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
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/schedule"
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
	Files         *managed.Manager
	Target        string
	location      *time.Location
	store         *store.Store
	buildSHA      string
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
	return &Manager{store: data, buildSHA: buildSHA, manual: make(chan struct{}, 1)}
}

func (m *Manager) Start(ctx context.Context) {
	go func() {
		m.reconcileInterruptedJob(ctx)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.reconcileInterruptedJob(ctx)
			case <-m.manual:
				if err := m.check(ctx, true); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("Manual self-update check failed: %v", err)
				}
				m.manualPending.Store(false)
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
	if row, found, _ := m.store.ProjectSchedule(context.Background(), m.scheduleTarget()); found {
		next := int64(0)
		if enabled {
			hour, minute, parseErr := schedule.ParseTime(row.Time)
			if parseErr != nil {
				return parseErr
			}
			location := m.location
			if location == nil {
				location = time.Local
			}
			next = schedule.NextAt(time.Now(), location, hour, minute).Unix()
		}
		_, err = m.store.SetProjectScheduleAt(context.Background(), row.Key, row.TargetID, enabled, next, row.Time)
		return err
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
	if row, found, _ := m.store.ProjectSchedule(context.Background(), m.scheduleTarget()); found {
		result.Automatic = row.Enabled
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
	if active, err := m.store.Jobs(context.Background(), m.scheduleTarget(), true); err == nil && len(active) > 0 {
		result.Status = "updating"
	}
	if result.Status != "updating" && result.Status != "checking" {
		if history, err := m.store.Jobs(context.Background(), m.scheduleTarget(), false); err == nil {
			for _, latest := range history {
				if latest.Operation != "update" && latest.Operation != "recreate" {
					continue
				}
				if latest.Domain == "managed" && latest.Status == "failed" && latest.CompletedAt >= settings.LastCheckedAt {
					result.Status = "update_failed"
					result.Error = latest.Error
				}
				break
			}
		}
	}
	return result, nil
}

func (m *Manager) scheduleTarget() string {
	if m.Target != "" {
		return m.Target
	}
	return "compose:nox-yard"
}

func (m *Manager) InitializeDaily(ctx context.Context, location *time.Location) error {
	m.Target = "compose:nox-yard"
	if cli, err := client.New(client.WithHost("unix:///var/run/docker.sock")); err == nil {
		if current, err := ownContainer(ctx, cli); err == nil && current.Config != nil {
			if name := current.Config.Labels["com.docker.compose.project"]; name != "" {
				m.Target = "compose:" + name
			}
		}
		_ = cli.Close()
	}
	return m.initializeDaily(ctx, location)
}

func (m *Manager) initializeDaily(ctx context.Context, location *time.Location) error {
	if location == nil {
		location = time.Local
	}
	m.location = location
	if _, found, err := m.store.ProjectSchedule(ctx, m.Target); err != nil || found {
		return err
	}
	settings, err := m.store.SelfUpdateSettings()
	if err != nil {
		return err
	}
	next := int64(0)
	if settings.Automatic {
		next = schedule.NextAt(time.Now(), location, 3, 0).Unix()
	}
	_, err = m.store.SetProjectScheduleAt(ctx, m.Target, m.Target, settings.Automatic, next, "03:00")
	return err
}
func (m *Manager) Scheduled(ctx context.Context, row store.ProjectSchedule, occurrence store.ScheduleOccurrence) error {
	ctx = store.WithSchedule(ctx, occurrence)
	if err := m.check(ctx, false); err != nil {
		if ctx.Err() != nil {
			return err
		}
		return m.store.SkipSchedule(ctx, row, occurrence, "Yard update check failed or the project is busy; review its status before retrying.")
	}
	current, _, err := m.store.ProjectSchedule(ctx, row.TargetID)
	if err != nil {
		return err
	}
	if current.NextAt == row.NextAt {
		return m.store.SkipSchedule(ctx, row, occurrence, "No new image was applied; current or previously failed image retained.")
	}
	return nil
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
	if available.ImageID != current.Image && m.Files == nil {
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
	if m.Files != nil {
		name := current.Config.Labels["com.docker.compose.project"]
		_, err := m.Files.FileOperation(ctx, name, "update", "")
		if !errors.Is(err, managed.ErrFilesMissing) {
			return err
		}
	}
	jobID, err := randomID()
	if err != nil {
		return err
	}
	job = store.SelfUpdateJob{
		ID: jobID, OldImageID: current.Image,
		TargetImageID: available.ImageID, TargetDigest: available.ManifestDigest,
		ProjectName: current.Config.Labels["com.docker.compose.project"], ContainerID: current.ID,
	}
	if err := m.store.CreateSelfUpdateJobContext(ctx, job); err != nil {
		return err
	}
	if err := pullAndVerify(ctx, cli, available.ImageID); err != nil {
		return m.failJobWithTag(cli, jobID, current.Image, err)
	}
	if err := launchWorker(ctx, cli, current, jobID, m.store); err != nil {
		if errors.Is(err, jobs.ErrLaunchUncertain) {
			return err
		}
		return m.failJobWithTag(cli, jobID, current.Image, err)
	}
	return nil
}

// Reconcile a job left behind by a worker that exited before recording an outcome.
// A live worker owns the job; the web service only resolves it after that worker is gone.
func (m *Manager) reconcileInterruptedJob(ctx context.Context) {
	job, exists, err := m.store.LatestSelfUpdateJob()
	if err != nil || !exists {
		return
	}
	if job.Status != "updating" {
		// A pre-C2 worker only finalizes its dedicated table. Mirror that durable
		// result after an upgrade without rerunning its replacement protocol.
		operation, found, _ := m.store.Job(job.ID)
		if found && operation.Status == "running" {
			outcome, message := "verified", ""
			if job.Status == "failed" {
				outcome, message = "recovery_required", "Previous self-update failed during upgrade. Inspect its dedicated status and retained resources, then acknowledge recovery before retrying."
			}
			_ = m.store.FinishJob(job.ID, operation.Owner, job.Status, outcome, message, "")
		}
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
	recoveryRequired := current.Image != job.OldImageID
	if err := m.store.FinishSelfUpdateOutcome(job.ID, "failed", message, recoveryRequired, ""); err != nil {
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

func launchWorker(ctx context.Context, cli *client.Client, self container.InspectResponse, jobID string, data *store.Store) error {
	mounts, err := requiredMounts(self)
	if err != nil {
		return err
	}
	operation, found, err := data.Job(jobID)
	if err != nil {
		return err
	}
	if !found {
		return store.ErrJobChanged
	}
	if err := data.SetJobWorker(jobID, operation.Owner, "nox-yard-update-"+jobID); err != nil {
		return err
	}
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "nox-yard-update-" + jobID,
		Config: &container.Config{
			Image:  self.Image,
			Cmd:    []string{"nox-yard", "update-worker", jobID, self.ID},
			Env:    []string{"NOX_DATA_DIR=/data"},
			Labels: map[string]string{"nox-yard.role": "self-update-worker", "nox-yard.job": jobID, "nox-yard.owner": operation.Owner},
		},
		HostConfig: &container.HostConfig{
			AutoRemove:  true,
			NetworkMode: "none",
			Mounts:      mounts,
			SecurityOpt: []string{"no-new-privileges:true"},
		},
	})
	if err != nil {
		return fmt.Errorf("%w: create update worker", jobs.ErrLaunchUncertain)
	}
	if err := data.SetJobWorker(jobID, operation.Owner, created.ID); err != nil {
		return fmt.Errorf("%w: register update worker", jobs.ErrLaunchUncertain)
	}
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("%w: start update worker", jobs.ErrLaunchUncertain)
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
