package selfupdate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const checkInterval = 6 * time.Hour

var ErrUpdateInProgress = errors.New("self-update is in progress")

type Manager struct {
	store    *store.Store
	buildSHA string
	trigger  chan struct{}
	mu       sync.Mutex
}

type Status struct {
	Automatic       bool   `json:"automatic"`
	Status          string `json:"status"`
	LastChecked     string `json:"lastChecked,omitempty"`
	CurrentBuildSHA string `json:"currentBuildSHA,omitempty"`
	Error           string `json:"error,omitempty"`
}

func New(data *store.Store, buildSHA string) *Manager {
	return &Manager{store: data, buildSHA: buildSHA, trigger: make(chan struct{}, 1)}
}

func (m *Manager) Start(ctx context.Context) {
	go func() {
		m.checkIfDue(ctx)
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.checkIfDue(ctx)
			case <-m.trigger:
				settings, err := m.store.SelfUpdateSettings()
				if err == nil && settings.Automatic {
					if err := m.Check(ctx); err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("Self-update check failed: %v", err)
					}
				}
			}
		}
	}()
}

func (m *Manager) SetAutomatic(enabled bool) error {
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil {
		return err
	}
	if exists && job.Status == "updating" {
		return ErrUpdateInProgress
	}
	if err := m.store.SetAutomaticUpdates(enabled); err != nil {
		return err
	}
	if enabled {
		select {
		case m.trigger <- struct{}{}:
		default:
		}
	}
	return nil
}

func (m *Manager) Status() (Status, error) {
	settings, err := m.store.SelfUpdateSettings()
	if err != nil {
		return Status{}, err
	}
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil {
		return Status{}, err
	}
	result := Status{Automatic: settings.Automatic, CurrentBuildSHA: m.buildSHA, Status: "not_checked"}
	if settings.LastCheckedAt > 0 {
		result.LastChecked = time.Unix(settings.LastCheckedAt, 0).UTC().Format(time.RFC3339)
		result.Status = "up_to_date"
	}
	if exists {
		switch job.Status {
		case "updating":
			result.Status = "updating"
		case "failed":
			if settings.FailedDigest == "" {
				break
			}
			result.Status = "update_failed"
			result.Error = job.Error
		}
	}
	if settings.LastCheckError != "" && result.Status != "updating" {
		result.Status = "update_failed"
		result.Error = settings.LastCheckError
	}
	return result, nil
}

func (m *Manager) checkIfDue(ctx context.Context) {
	settings, err := m.store.SelfUpdateSettings()
	if err != nil || !settings.Automatic ||
		(settings.LastCheckedAt > 0 && time.Since(time.Unix(settings.LastCheckedAt, 0)) < checkInterval) {
		return
	}
	if err := m.Check(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("Self-update check failed: %v", err)
	}
}

func (m *Manager) Check(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil {
		return err
	}
	if exists && job.Status == "updating" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return m.checkError(err)
	}
	defer cli.Close()
	current, err := ownContainer(ctx, cli)
	if err != nil {
		return m.checkError(err)
	}
	available, err := latestRelease(ctx)
	if err != nil {
		return m.checkError(err)
	}
	if available.ImageID != current.Image {
		if _, _, _, err := replacementConfig(current, available.ImageID); err != nil {
			return m.checkError(err)
		}
	}
	if err := m.store.RecordSelfUpdateCheck(""); err != nil {
		return err
	}
	if current.Image == available.ImageID {
		return m.store.ClearSelfUpdateFailure()
	}
	if available.ManifestDigest == "" {
		return nil
	}
	settings, err := m.store.SelfUpdateSettings()
	if err != nil {
		return err
	}
	if !settings.Automatic {
		return nil
	}
	if settings.FailedDigest == available.ManifestDigest {
		return nil // Do not retry a version that already failed and was rolled back.
	}
	jobID, err := randomID()
	if err != nil {
		return err
	}
	job = store.SelfUpdateJob{
		ID: jobID, OldImageID: current.Image,
		TargetImageID: available.ImageID, TargetDigest: available.ManifestDigest,
	}
	if err := m.store.CreateSelfUpdateJob(job); err != nil {
		return err
	}
	if err := pullAndVerify(ctx, cli, available.ImageID); err != nil {
		return m.failJobWithTag(cli, jobID, current.Image, err)
	}
	if err := launchWorker(ctx, cli, current, jobID); err != nil {
		return m.failJobWithTag(cli, jobID, current.Image, err)
	}
	return nil
}

func (m *Manager) checkError(err error) error {
	message := err.Error()
	if recordErr := m.store.RecordSelfUpdateCheck(message); recordErr != nil {
		return fmt.Errorf("%v; save check error: %w", err, recordErr)
	}
	return err
}

func (m *Manager) failJobWithTag(cli *client.Client, id, oldImageID string, err error) error {
	recovery, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, tagErr := cli.ImageTag(recovery, client.ImageTagOptions{Source: oldImageID, Target: imageReference}); tagErr != nil {
		err = fmt.Errorf("%v; restore previous latest tag: %w", err, tagErr)
	}
	if recordErr := m.store.FinishSelfUpdateJob(id, "failed", err.Error()); recordErr != nil {
		return fmt.Errorf("%v; save failure: %w", err, recordErr)
	}
	return err
}

func ownContainer(ctx context.Context, cli *client.Client) (container.InspectResponse, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return container.InspectResponse{}, err
	}
	inspected, err := cli.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf("identify this Docker container: %w", err)
	}
	self := inspected.Container
	if !strings.HasPrefix(self.ID, hostname) || self.Config == nil || self.HostConfig == nil ||
		self.State == nil || !self.State.Running ||
		self.Config.Labels["com.docker.compose.service"] != "nox-yard" ||
		self.Config.Labels["com.docker.compose.project"] == "" {
		return container.InspectResponse{}, errors.New("self-update requires the standard running NoX Yard Compose container")
	}
	if !hasExpectedHealthcheck(self.Config.Healthcheck) {
		return container.InspectResponse{}, errors.New("self-update requires the /healthz Docker healthcheck")
	}
	if _, err := requiredMounts(self); err != nil {
		return container.InspectResponse{}, err
	}
	return self, nil
}

func requiredMounts(self container.InspectResponse) ([]mount.Mount, error) {
	var result []mount.Mount
	for _, destination := range []string{"/data", "/var/run/docker.sock"} {
		found := false
		for _, source := range self.Mounts {
			if source.Destination != destination || !source.RW || source.Type != mount.TypeBind {
				continue
			}
			if source.Source == "" {
				return nil, fmt.Errorf("self-update mount %s has no source", destination)
			}
			result = append(result, mount.Mount{Type: mount.TypeBind, Source: source.Source, Target: destination})
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("self-update requires a writable %s bind mount", destination)
		}
	}
	return result, nil
}

func pullAndVerify(ctx context.Context, cli *client.Client, expectedID string) error {
	stream, err := cli.ImagePull(ctx, imageReference, client.ImagePullOptions{
		Platforms: []ocispec.Platform{{OS: "linux", Architecture: runtime.GOARCH}},
	})
	if err != nil {
		return fmt.Errorf("pull latest image: %w", err)
	}
	defer stream.Close()
	if err := stream.Wait(ctx); err != nil {
		return fmt.Errorf("pull latest image: %w", err)
	}
	inspected, err := cli.ImageInspect(ctx, imageReference)
	if err != nil {
		return fmt.Errorf("inspect pulled image: %w", err)
	}
	if inspected.ID != expectedID {
		return errors.New("latest changed while it was being pulled; update aborted")
	}
	return nil
}

func launchWorker(ctx context.Context, cli *client.Client, self container.InspectResponse, jobID string) error {
	mounts, err := requiredMounts(self)
	if err != nil {
		return err
	}
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "nox-yard-update-" + jobID,
		Config: &container.Config{
			Image:  self.Image,
			Cmd:    []string{"nox-yard", "update-worker", jobID, self.ID},
			Env:    []string{"NOX_DATA_DIR=/data"},
			Labels: map[string]string{"nox-yard.role": "self-update-worker"},
		},
		HostConfig: &container.HostConfig{
			AutoRemove:  true,
			NetworkMode: "none",
			Mounts:      mounts,
			SecurityOpt: []string{"no-new-privileges:true"},
		},
	})
	if err != nil {
		return fmt.Errorf("create update worker: %w", err)
	}
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = cli.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start update worker: %w", err)
	}
	return nil
}

func randomID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
