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
	"sync/atomic"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var ErrUpdateInProgress = errors.New("self-update is in progress")
var ErrCheckInProgress = errors.New("self-update check is in progress")
var ErrInvalidInterval = errors.New("check interval must be 5, 15, 30, 60, or 360 minutes")

type Manager struct {
	store         *store.Store
	buildSHA      string
	trigger       chan struct{}
	manual        chan struct{}
	mu            sync.Mutex
	checking      atomic.Bool
	manualPending atomic.Bool
}

type Status struct {
	Automatic       bool   `json:"automatic"`
	IntervalMinutes int    `json:"intervalMinutes"`
	Status          string `json:"status"`
	LastChecked     string `json:"lastChecked,omitempty"`
	CurrentBuildSHA string `json:"currentBuildSHA,omitempty"`
	Error           string `json:"error,omitempty"`
}

func New(data *store.Store, buildSHA string) *Manager {
	return &Manager{store: data, buildSHA: buildSHA, trigger: make(chan struct{}, 1), manual: make(chan struct{}, 1)}
}

func (m *Manager) Start(ctx context.Context) {
	go func() {
		m.reconcileInterruptedJob(ctx)
		m.checkIfDue(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reconcileInterruptedJob(ctx)
				m.checkIfDue(ctx)
			case <-m.manual:
				if err := m.check(ctx, true); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("Manual self-update check failed: %v", err)
				}
				m.manualPending.Store(false)
			case <-m.trigger:
				settings, err := m.store.SelfUpdateSettings()
				if err == nil && settings.Automatic {
					if err := m.check(ctx, false); err != nil && !errors.Is(err, context.Canceled) {
						log.Printf("Self-update check failed: %v", err)
					}
				}
			}
		}
	}()
}

func (m *Manager) CheckNow() error {
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil {
		return err
	}
	if exists && job.Status == "updating" {
		return ErrUpdateInProgress
	}
	if m.checking.Load() || !m.manualPending.CompareAndSwap(false, true) {
		return ErrCheckInProgress
	}
	m.manual <- struct{}{}
	return nil
}

func (m *Manager) SetSettings(enabled bool, intervalMinutes int) error {
	if !validInterval(intervalMinutes) {
		return ErrInvalidInterval
	}
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil {
		return err
	}
	if exists && job.Status == "updating" {
		return ErrUpdateInProgress
	}
	if err := m.store.SetSelfUpdateSettings(enabled, intervalMinutes); err != nil {
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
	result := Status{
		Automatic: settings.Automatic, IntervalMinutes: settings.CheckIntervalMinutes,
		CurrentBuildSHA: m.buildSHA, Status: "not_checked",
	}
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
	if result.Status != "updating" && (m.checking.Load() || m.manualPending.Load()) {
		result.Status = "checking"
		result.Error = ""
	}
	return result, nil
}

func (m *Manager) checkIfDue(ctx context.Context) {
	settings, err := m.store.SelfUpdateSettings()
	if err != nil || !settings.Automatic ||
		(settings.LastCheckedAt > 0 && time.Since(time.Unix(settings.LastCheckedAt, 0)) <
			time.Duration(settings.CheckIntervalMinutes)*time.Minute) {
		return
	}
	if err := m.check(ctx, false); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("Self-update check failed: %v", err)
	}
}

func validInterval(minutes int) bool {
	switch minutes {
	case 5, 15, 30, 60, 360:
		return true
	default:
		return false
	}
}

func (m *Manager) check(ctx context.Context, manual bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checking.Store(true)
	defer m.checking.Store(false)
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil {
		return err
	}
	if exists && job.Status == "updating" {
		return ErrUpdateInProgress
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
	if !settings.Automatic && !manual {
		return nil
	}
	if settings.FailedDigest == available.ManifestDigest && !manual {
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

// Reconcile a job left behind by a worker that exited before recording an outcome.
// A live worker owns the job; the web service only resolves it after that worker is gone.
func (m *Manager) reconcileInterruptedJob(ctx context.Context) {
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil || !exists || job.Status != "updating" {
		return
	}
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		log.Printf("Cannot inspect interrupted self-update: %v", err)
		return
	}
	defer cli.Close()
	inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	worker, err := cli.ContainerInspect(inspectCtx, "nox-yard-update-"+job.ID, client.ContainerInspectOptions{})
	if err == nil && worker.Container.State != nil && worker.Container.State.Running {
		return
	}
	if err != nil && !errdefs.IsNotFound(err) {
		log.Printf("Cannot inspect self-update worker: %v", err)
		return
	}
	current, err := ownContainer(inspectCtx, cli)
	if err != nil {
		log.Printf("Cannot reconcile interrupted self-update: %v", err)
		return
	}
	if current.Image == job.TargetImageID && current.State.Health != nil && current.State.Health.Status == "healthy" {
		if err := m.store.FinishSelfUpdateJob(job.ID, "succeeded", ""); err != nil {
			log.Printf("Cannot record completed self-update: %v", err)
		}
		return
	}
	if current.Image == job.TargetImageID && current.State.Health != nil && current.State.Health.Status == "starting" {
		return
	}
	message := "Update worker exited before reporting a result; NoX Yard is still running the previous image."
	if current.Image != job.OldImageID {
		message = "Update worker exited before reporting a result; inspect the running container and rollback resources on the host."
	}
	if current.Image == job.OldImageID {
		if _, tagErr := cli.ImageTag(inspectCtx, client.ImageTagOptions{Source: job.OldImageID, Target: imageReference}); tagErr != nil {
			message += " Restoring the previous latest tag failed: " + tagErr.Error()
		}
	}
	if err := m.store.FinishSelfUpdateJob(job.ID, "failed", message); err != nil {
		log.Printf("Cannot record interrupted self-update: %v", err)
	}
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
