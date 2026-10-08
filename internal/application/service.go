// Package application is the transport-independent orchestration facade.
// Existing managers remain the implementation of all Docker and storage work.
package application

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/schedule"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
)

var ErrUnavailable = errors.New("The requested capability is unavailable")
var ErrInvalidRemoval = errors.New("Review and confirm a current removal preview first.")
var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ManagedController and Updater describe existing managers, without introducing
// a second implementation or changing their asynchronous job lifetimes.
type ManagedController interface {
	ProjectsBase() (string, error)
	SetProjectsBase(context.Context, string) (string, error)
	Preview(context.Context, managed.Request) (managed.ProjectPreview, managed.Source, error)
	Submit(context.Context, managed.Request) (store.ManagedJob, error)
	Operation(string, string, bool) (store.ManagedJob, error)
	Job(string) (store.ManagedJob, bool, error)
}
type Updater interface {
	Status() (selfupdate.Status, error)
	SetSettings(bool, int) error
	CheckNow() error
}
type Service struct {
	Store     *store.Store
	Inventory inventory.Reader
	Logs      inventory.LogReader
	Lifecycle lifecycle.Controller
	Managed   ManagedController
	Recreator interface {
		Preview(context.Context, recreate.Request) (recreate.Preview, error)
		Submit(context.Context, recreate.Request) (store.Job, error)
	}
	Updates        Updater
	Changes        *inventory.Notifier
	JobObserver    *jobs.Observer
	Scheduler      *schedule.Manager
	ResourceKeys   func(context.Context, string) ([]string, error)
	ResourceImages func(context.Context, []string, bool) ([]store.ImageIdentity, error)
	instance       string
}

func New(data *store.Store, changes *inventory.Notifier) *Service {
	if changes == nil {
		panic("application requires the shared inventory notifier")
	}
	identity, err := store.NewJob("instance", "engine", "", nil)
	if err != nil {
		panic(err)
	}
	return &Service{Store: data, Changes: changes, instance: identity.ID}
}
func ValidTarget(id string, container bool) bool {
	if container {
		return containerIDPattern.MatchString(id)
	}
	if suffix, ok := strings.CutPrefix(id, "container:"); ok {
		return containerIDPattern.MatchString(suffix)
	}
	name, ok := strings.CutPrefix(id, "compose:")
	return ok && name != "" && !strings.ContainsAny(name, "/\\") && !strings.ContainsFunc(name, unicode.IsControl)
}
func ValidRemoval(confirm bool, fingerprint string) bool { return confirm && len(fingerprint) == 64 }
func (s *Service) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Store.PingContext(ctx)
}
func (s *Service) Inspect(ctx context.Context, id string, reveal bool) (inventory.ContainerInspection, error) {
	if err := ctx.Err(); err != nil {
		return inventory.ContainerInspection{}, err
	}
	if s.Inventory == nil {
		return inventory.ContainerInspection{}, ErrDockerUnavailable
	}
	return s.Inventory.InspectContainer(ctx, id, reveal)
}
func (s *Service) Metrics(ctx context.Context) (map[string]inventory.Metrics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, ok := s.Inventory.(interface {
		CachedMetrics() map[string]inventory.Metrics
	})
	if !ok {
		return nil, ErrUnavailable
	}
	return reader.CachedMetrics(), nil
}
func (s *Service) PreviewRemove(ctx context.Context, id string, container bool) (lifecycle.RemovalPlan, error) {
	return s.PreviewRemoveWithOptions(ctx, id, container, false)
}
func (s *Service) PreviewRemoveWithOptions(ctx context.Context, id string, container, removeVolumes bool) (lifecycle.RemovalPlan, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RemovalPlan{}, err
	}
	if s.Lifecycle == nil {
		return lifecycle.RemovalPlan{}, ErrDockerUnavailable
	}
	if controller, ok := s.Lifecycle.(interface {
		PreviewRemoval(context.Context, string, bool) (lifecycle.RemovalPlan, error)
	}); ok {
		target := id
		if container {
			target = "container:" + id
		}
		return controller.PreviewRemoval(ctx, target, removeVolumes)
	}
	if removeVolumes {
		return lifecycle.RemovalPlan{}, ErrUnavailable
	}
	if container {
		return s.Lifecycle.PreviewRemoveContainer(ctx, id)
	}
	return s.Lifecycle.PreviewRemoveProject(ctx, id)
}
func (s *Service) Remove(ctx context.Context, id string, container, confirm bool, fingerprint string) (lifecycle.RemovalReport, error) {
	return s.RemoveWithOptions(ctx, id, container, confirm, fingerprint, false)
}
func (s *Service) RemoveWithOptions(ctx context.Context, id string, container, confirm bool, fingerprint string, removeVolumes bool) (lifecycle.RemovalReport, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.RemovalReport{}, err
	}
	if !ValidRemoval(confirm, fingerprint) {
		return lifecycle.RemovalReport{}, ErrInvalidRemoval
	}
	if s.Lifecycle == nil {
		return lifecycle.RemovalReport{}, ErrDockerUnavailable
	}
	target := id
	if container {
		target = "container:" + id
	}
	job, err := s.beginOperation(ctx, target, "remove")
	if err != nil {
		return lifecycle.RemovalReport{}, err
	}
	finish := s.Changes.Begin(target, "remove")
	defer finish()
	var result lifecycle.RemovalReport
	if controller, ok := s.Lifecycle.(interface {
		RemoveWithOptions(context.Context, string, string, bool) (lifecycle.RemovalReport, error)
	}); ok {
		result, err = controller.RemoveWithOptions(ctx, target, fingerprint, removeVolumes)
	} else if removeVolumes {
		err = ErrUnavailable
	} else if container {
		result, err = s.Lifecycle.RemoveContainer(ctx, id, fingerprint)
	} else {
		result, err = s.Lifecycle.RemoveProject(ctx, id, fingerprint)
	}
	failed := false
	for _, item := range result.Items {
		if item.Status == "failed" {
			failed = true
		}
	}
	s.finishOperation(job, failed, errors.Join(err, ctx.Err()))
	return result, err
}

type PreparedSource struct {
	managed.Source
	Variables []managed.Variable `json:"variables"`
	managed.SourceInfo
}

func (s *Service) PrepareSource(ctx context.Context, input managed.SourceInput) (PreparedSource, error) {
	if err := ctx.Err(); err != nil {
		return PreparedSource{}, err
	}
	source, err := managed.LoadSource(ctx, input)
	if err != nil {
		return PreparedSource{}, err
	}
	if err := ctx.Err(); err != nil {
		return PreparedSource{}, err
	}
	info, err := managed.InspectSource(source.YAML)
	if err != nil {
		return PreparedSource{}, err
	}
	return PreparedSource{source, managed.Variables(source.YAML), info}, nil
}
func (s *Service) ComposePreview(ctx context.Context, input managed.Request) (managed.ProjectPreview, error) {
	if err := ctx.Err(); err != nil {
		return managed.ProjectPreview{}, err
	}
	if s.Managed == nil {
		return managed.ProjectPreview{}, ErrUnavailable
	}
	preview, _, err := s.Managed.Preview(ctx, input)
	return preview, err
}
func (s *Service) ComposeSubmit(ctx context.Context, input managed.Request) (store.ManagedJob, error) {
	if err := ctx.Err(); err != nil {
		return store.ManagedJob{}, err
	}
	if s.Managed == nil {
		return store.ManagedJob{}, ErrUnavailable
	}
	return s.Managed.Submit(ctx, input)
}
func (s *Service) ComposeOperation(ctx context.Context, name, operation string, removeVolumes bool) (store.ManagedJob, error) {
	return s.ComposeOperationWithPreview(ctx, name, operation, removeVolumes, "")
}
func (s *Service) ComposeOperationWithPreview(ctx context.Context, name, operation string, removeVolumes bool, fingerprint string) (store.ManagedJob, error) {
	if err := ctx.Err(); err != nil {
		return store.ManagedJob{}, err
	}
	if s.Managed == nil {
		return store.ManagedJob{}, ErrUnavailable
	}
	if fingerprint != "" {
		if operation != "remove" || len(fingerprint) != 64 {
			return store.ManagedJob{}, ErrInvalidRemoval
		}
		controller, ok := s.Managed.(interface {
			OperationWithPreview(string, string, bool, string) (store.ManagedJob, error)
		})
		if !ok {
			return store.ManagedJob{}, ErrUnavailable
		}
		return controller.OperationWithPreview(name, operation, removeVolumes, fingerprint)
	}
	return s.Managed.Operation(name, operation, removeVolumes)
}
func (s *Service) ComposeJob(ctx context.Context, id string) (store.ManagedJob, bool, error) {
	if err := ctx.Err(); err != nil {
		return store.ManagedJob{}, false, err
	}
	if s.Managed == nil {
		return store.ManagedJob{}, false, ErrUnavailable
	}
	return s.Managed.Job(id)
}
func (s *Service) ProjectsBase(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Managed == nil {
		return "", ErrUnavailable
	}
	return s.Managed.ProjectsBase()
}
func (s *Service) SetProjectsBase(ctx context.Context, base string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Managed == nil {
		return "", ErrUnavailable
	}
	return s.Managed.SetProjectsBase(ctx, base)
}
func (s *Service) UpdateStatus(ctx context.Context) (selfupdate.Status, error) {
	if err := ctx.Err(); err != nil {
		return selfupdate.Status{}, err
	}
	if s.Updates == nil {
		return selfupdate.Status{}, ErrUnavailable
	}
	return s.Updates.Status()
}
func (s *Service) SetUpdateSettings(ctx context.Context, automatic bool, minutes int) (selfupdate.Status, error) {
	if err := ctx.Err(); err != nil {
		return selfupdate.Status{}, err
	}
	if s.Updates == nil {
		return selfupdate.Status{}, ErrUnavailable
	}
	if err := s.Updates.SetSettings(automatic, minutes); err != nil {
		return selfupdate.Status{}, err
	}
	status, err := s.UpdateStatus(ctx)
	if err != nil {
		return status, UpdateStatusError{err}
	}
	return status, nil
}
func (s *Service) CheckAndUpdate(ctx context.Context) (selfupdate.Status, error) {
	if err := ctx.Err(); err != nil {
		return selfupdate.Status{}, err
	}
	if s.Updates == nil {
		return selfupdate.Status{}, ErrUnavailable
	}
	if err := s.Updates.CheckNow(); err != nil {
		return selfupdate.Status{}, err
	}
	status, err := s.UpdateStatus(ctx)
	if err != nil {
		return status, UpdateStatusError{err}
	}
	return status, nil
}

// Status uses the same live snapshot as HTTP, without extra Docker probes.
type Availability struct {
	Available bool `json:"available"`
}
type DockerStatus struct {
	Available bool     `json:"available"`
	Error     *Problem `json:"error,omitempty"`
}
type Counts struct {
	CollectedAt       time.Time `json:"collectedAt"`
	Projects          int       `json:"projects"`
	Containers        int       `json:"containers"`
	RunningContainers int       `json:"runningContainers"`
}
type Status struct {
	Service    string       `json:"service"`
	Version    string       `json:"version"`
	APIVersion string       `json:"apiVersion"`
	Docker     DockerStatus `json:"docker"`
	Inventory  *Counts      `json:"inventory"`
}

func (s *Service) Status(ctx context.Context, version string) (Status, error) {
	result := Status{Service: "nox-yard", Version: version, APIVersion: "v1"}
	snapshot, err := s.Projects(ctx)
	if err != nil {
		var read InventoryReadError
		status, p := ClassifyError(err, false)
		if !errors.As(err, &read) || (status != 502 && status != 503 && status != 504) || errors.Is(err, context.Canceled) {
			return result, err
		}
		result.Docker.Error = &p
		return result, nil
	}
	result.Docker.Available = true
	counts := Counts{CollectedAt: snapshot.CollectedAt.UTC(), Projects: len(snapshot.Projects)}
	for _, project := range snapshot.Projects {
		counts.Containers += len(project.Containers)
		for _, c := range project.Containers {
			if c.State == "running" {
				counts.RunningContainers++
			}
		}
	}
	result.Inventory = &counts
	return result, nil
}

// UpdateStatusError distinguishes accepted mutations from a failed status read.
type UpdateStatusError struct{ error }

func (e UpdateStatusError) Unwrap() error { return e.error }

func (s *Service) LogsSnapshot(ctx context.Context, id string) ([]inventory.LogLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, ok := s.Logs.(inventory.SnapshotLogReader)
	if !ok {
		return nil, ErrUnavailable
	}
	stream, err := reader.OpenLogsWithOptions(ctx, id, inventory.LogOptions{Tail: 20, Follow: false})
	if err != nil {
		return nil, err
	}
	defer stream.Reader.Close()
	// Interrupt a blocked finite read when the adapter context ends.
	stop := context.AfterFunc(ctx, func() { _ = stream.Reader.Close() })
	defer stop()
	lines := make([]inventory.LogLine, 0, 20)
	err = inventory.DecodeLogs(ctx, stream, func(line inventory.LogLine) error {
		if len(lines) == 20 {
			copy(lines, lines[1:])
			lines = lines[:19]
		}
		lines = append(lines, line)
		return nil
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return lines, err
}

func (s *Service) OpenLogs(ctx context.Context, id string) (inventory.LogStream, error) {
	if err := ctx.Err(); err != nil {
		return inventory.LogStream{}, err
	}
	if s.Logs == nil {
		return inventory.LogStream{}, ErrUnavailable
	}
	return s.Logs.OpenLogs(ctx, id)
}
