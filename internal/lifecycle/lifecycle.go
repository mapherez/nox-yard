package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/composeorder"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"slices"
)

type Action string

const (
	Start   Action = "start"
	Stop    Action = "stop"
	Restart Action = "restart"
)

var ErrNotFound = errors.New("target not found")
var ErrChanged = errors.New("Docker removal plan changed")
var ErrProtected = errors.New("Yard cannot be stopped from its own web process")
var ErrInvalidAction = errors.New("invalid lifecycle action")
var ErrConflict = errors.New("a Yard maintenance helper is already running")

// Failure preserves the domain cause for machine clients without changing the
// existing browser response's counters or human-readable errors.
type Failure struct {
	Target string
	Cause  error
}

type Result struct {
	Action    Action    `json:"action"`
	Succeeded int       `json:"succeeded"`
	Skipped   int       `json:"skipped"`
	Failed    int       `json:"failed"`
	Queued    int       `json:"queued"`
	Errors    []string  `json:"errors,omitempty"`
	Failures  []Failure `json:"-"`
	Warnings  []string  `json:"warnings,omitempty"`
}

type Controller interface {
	Container(context.Context, string, Action) (Result, error)
	Project(context.Context, string, Action) (Result, error)
	PullContainer(context.Context, string) (MaintenanceResult, error)
	PullProject(context.Context, string) (MaintenanceResult, error)
	PreviewRemoveContainer(context.Context, string) (RemovalPlan, error)
	PreviewRemoveProject(context.Context, string) (RemovalPlan, error)
	RemoveContainer(context.Context, string, string) (RemovalReport, error)
	RemoveProject(context.Context, string, string) (RemovalReport, error)
}

// Manager serializes lifecycle requests so a group action cannot race another
// operation submitted through this application. Docker remains the source of truth.
type Manager struct {
	client        *client.Client
	mu            contextMutex
	data          *store.Store
	removalSource func(context.Context, string) (RemovalSource, error)
}

func (m *Manager) SetStore(data *store.Store) { m.data = data }

func New() (*Manager, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return nil, err
	}
	return &Manager{client: cli}, nil
}

func (m *Manager) Close() error {
	return m.client.Close()
}

func (m *Manager) Container(ctx context.Context, id string, action Action) (Result, error) {
	if !validAction(action) {
		return Result{}, ErrInvalidAction
	}
	if err := m.mu.Lock(ctx); err != nil {
		return Result{}, err
	}
	defer m.mu.Unlock()
	return m.container(ctx, id, action)
}

func (m *Manager) Project(ctx context.Context, id string, action Action) (Result, error) {
	if !validAction(action) {
		return Result{}, ErrInvalidAction
	}
	if err := m.mu.Lock(ctx); err != nil {
		return Result{}, err
	}
	defer m.mu.Unlock()
	if strings.HasPrefix(id, "container:") {
		return m.container(ctx, strings.TrimPrefix(id, "container:"), action)
	}
	name, ok := strings.CutPrefix(id, "compose:")
	if !ok || name == "" {
		return Result{}, ErrNotFound
	}
	targets, err := m.projectTargets(ctx, name)
	if err != nil {
		return Result{}, err
	}
	// Preflight the whole group before touching any container.
	if action == Stop || action == Restart {
		for _, item := range targets {
			if (action == Stop && isSelf(item.ID, item.Labels)) || isHelper(item.Labels) {
				return Result{}, ErrProtected
			}
		}
	}
	if action == Stop {
		slices.Reverse(targets)
	}
	result := Result{Action: action, Warnings: []string{"Compose source unavailable or protected; using observed container dependencies only."}}
	selfID := ""
	for _, item := range targets {
		if action == Start && (isSelf(item.ID, item.Labels) || isHelper(item.Labels)) {
			result.Skipped++
			continue
		}
		if action == Restart && isSelf(item.ID, item.Labels) {
			selfID = item.ID
			continue
		}
		if !shouldRun(string(item.State), action) {
			result.Skipped++
			continue
		}
		if action == Start || action == Restart {
			if err := m.waitObservedDependencies(ctx, item.Labels, targets); err != nil {
				result.Failed++
				result.Errors = append(result.Errors, err.Error())
				result.Failures = append(result.Failures, Failure{Target: item.ID, Cause: err})
				continue
			}
		}
		if err := m.apply(ctx, item.ID, action); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", containerName(item), err))
			result.Failures = append(result.Failures, Failure{Target: item.ID, Cause: err})
		} else {
			result.Succeeded++
		}
	}
	if selfID != "" {
		if err := m.launchSelfRestart(ctx, selfID, result.Failures...); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, "Yard: "+err.Error())
			result.Failures = append(result.Failures, Failure{Target: selfID, Cause: err})
		} else {
			result.Queued++
		}
	}
	return result, nil
}

func (m *Manager) projectTargets(ctx context.Context, name string) ([]container.Summary, error) {
	listed, err := m.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	var targets []container.Summary
	for _, item := range listed.Items {
		if item.Labels["com.docker.compose.project"] == name {
			targets = append(targets, item)
		}
	}
	if len(targets) == 0 {
		return nil, ErrNotFound
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	labels := make([]map[string]string, len(targets))
	for i, item := range targets {
		labels[i] = item.Labels
	}
	order, err := composeorder.Labels(labels)
	if err != nil {
		return nil, err
	}
	sorted := make([]container.Summary, 0, len(targets))
	for _, i := range order {
		sorted = append(sorted, targets[i])
	}
	return sorted, nil
}

func (m *Manager) container(ctx context.Context, id string, action Action) (Result, error) {
	inspected, err := m.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	item := inspected.Container
	if item.ID != id {
		return Result{}, ErrNotFound
	}
	labels := map[string]string{}
	if item.Config != nil {
		labels = item.Config.Labels
	}
	if isHelper(labels) || (action == Stop && isSelf(item.ID, labels)) {
		return Result{}, ErrProtected
	}
	result := Result{Action: action}
	if isSelf(item.ID, labels) && action == Restart {
		if err := m.launchSelfRestart(ctx, id); err != nil {
			result.Failed = 1
			result.Errors = []string{err.Error()}
			result.Failures = []Failure{{Target: id, Cause: err}}
		} else {
			result.Queued = 1
		}
		return result, nil
	}
	state := ""
	if item.State != nil {
		state = string(item.State.Status)
	}
	if !shouldRun(state, action) {
		result.Skipped = 1
		return result, nil
	}
	if err := m.apply(ctx, id, action); err != nil {
		result.Failed = 1
		result.Errors = []string{err.Error()}
		result.Failures = []Failure{{Target: id, Cause: err}}
	} else {
		result.Succeeded = 1
	}
	return result, nil
}

func (m *Manager) apply(ctx context.Context, id string, action Action) error {
	switch action {
	case Start:
		_, err := m.client.ContainerStart(ctx, id, client.ContainerStartOptions{})
		return err
	case Stop:
		_, err := m.client.ContainerStop(ctx, id, client.ContainerStopOptions{})
		return err
	case Restart:
		_, err := m.client.ContainerRestart(ctx, id, client.ContainerRestartOptions{})
		return err
	default:
		return ErrInvalidAction
	}
}

func validAction(action Action) bool {
	return action == Start || action == Stop || action == Restart
}

func shouldRun(state string, action Action) bool {
	if action == Start {
		return state != "running" && state != "paused" && state != "restarting"
	}
	if action == Restart {
		return state == "running" || state == "exited" || state == "created"
	}
	return state == "running" || state == "paused" || state == "restarting"
}

func isSelf(id string, labels map[string]string) bool {
	hostname, _ := os.Hostname()
	return (len(hostname) >= 12 && len(hostname) <= len(id) && strings.HasPrefix(id, hostname)) ||
		(labels["com.docker.compose.project"] == "nox-yard" && labels["com.docker.compose.service"] == "nox-yard")
}

func isHelper(labels map[string]string) bool {
	return labels["nox-yard.role"] == "self-update-worker" || labels["nox-yard.role"] == "self-restart-worker" || labels["nox-yard.role"] == "managed-helper" || labels["nox-yard.role"] == "operation-worker"
}

// Protected is shared by assessment before any cache or runtime mutation.
func Protected(id string, labels map[string]string) bool {
	return isSelf(id, labels) || isHelper(labels)
}

func containerName(item container.Summary) string {
	if len(item.Names) > 0 {
		return strings.TrimPrefix(item.Names[0], "/")
	}
	return item.ID[:12]
}

func (m *Manager) waitObservedDependencies(ctx context.Context, labels map[string]string, targets []container.Summary) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, value := range strings.Split(labels["com.docker.compose.depends_on"], ",") {
		if value == "" {
			continue
		}
		parts := strings.Split(value, ":")
		if len(parts) != 3 {
			return ErrInvalidAction
		}
		for _, dependency := range targets {
			if dependency.Labels["com.docker.compose.service"] != parts[0] {
				continue
			}
			for {
				item, err := m.client.ContainerInspect(ctx, dependency.ID, client.ContainerInspectOptions{})
				if err != nil {
					return fmt.Errorf("observed dependency %s could not be inspected", parts[0])
				}
				state := item.Container.State
				if state == nil {
					return ErrInvalidAction
				}
				ready := state.Running
				if parts[1] == "service_healthy" {
					ready = state.Running && state.Health != nil && state.Health.Status == "healthy"
				}
				if parts[1] == "service_completed_successfully" {
					ready = state.Status == "exited" && state.ExitCode == 0
				}
				if ready {
					break
				}
				if state.Dead || state.OOMKilled || (state.Status == "exited" && state.ExitCode != 0) {
					return fmt.Errorf("observed dependency %s failed", parts[0])
				}
				timer := time.NewTimer(250 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return fmt.Errorf("observed dependency %s did not become ready", parts[0])
				case <-timer.C:
				}
			}
		}
	}
	return nil
}

func (m *Manager) SetRemovalSource(source func(context.Context, string) (RemovalSource, error)) {
	m.removalSource = source
}
