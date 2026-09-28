package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type Action string

const (
	Start   Action = "start"
	Stop    Action = "stop"
	Restart Action = "restart"
)

var ErrNotFound = errors.New("target not found")
var ErrChanged = errors.New("project containers changed")
var ErrProtected = errors.New("NoX Yard cannot be stopped from its own web process")
var ErrInvalidAction = errors.New("invalid lifecycle action")

type Result struct {
	Action    Action   `json:"action"`
	Succeeded int      `json:"succeeded"`
	Skipped   int      `json:"skipped"`
	Failed    int      `json:"failed"`
	Queued    int      `json:"queued"`
	Errors    []string `json:"errors,omitempty"`
}

type Controller interface {
	Container(context.Context, string, Action) (Result, error)
	Project(context.Context, string, Action) (Result, error)
	PullContainer(context.Context, string) (MaintenanceResult, error)
	PullProject(context.Context, string) (MaintenanceResult, error)
	RemoveContainer(context.Context, string) (MaintenanceResult, error)
	RemoveProject(context.Context, string, []string) (MaintenanceResult, error)
}

// Manager serializes lifecycle requests so a group action cannot race another
// operation submitted through this application. Docker remains the source of truth.
type Manager struct {
	client *client.Client
	mu     sync.Mutex
}

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
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.container(ctx, id, action)
}

func (m *Manager) Project(ctx context.Context, id string, action Action) (Result, error) {
	if !validAction(action) {
		return Result{}, ErrInvalidAction
	}
	m.mu.Lock()
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
	result := Result{Action: action}
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
		if err := m.apply(ctx, item.ID, action); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", containerName(item), err))
		} else {
			result.Succeeded++
		}
	}
	if selfID != "" {
		if err := m.launchSelfRestart(ctx, selfID); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, "NoX Yard: "+err.Error())
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
	return targets, nil
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
	return state == "running"
}

func isSelf(id string, labels map[string]string) bool {
	hostname, _ := os.Hostname()
	return (len(hostname) >= 12 && len(hostname) <= len(id) && strings.HasPrefix(id, hostname)) ||
		(labels["com.docker.compose.project"] == "nox-yard" && labels["com.docker.compose.service"] == "nox-yard")
}

func isHelper(labels map[string]string) bool {
	return labels["nox-yard.role"] == "self-update-worker" || labels["nox-yard.role"] == "self-restart-worker"
}

func containerName(item container.Summary) string {
	if len(item.Names) > 0 {
		return strings.TrimPrefix(item.Names[0], "/")
	}
	return item.ID[:12]
}
