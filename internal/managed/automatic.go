package managed

import (
	"context"
	"fmt"

	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
)

func automaticRuntime(preview Preview, runtime []container.InspectResponse) bool {
	if len(runtime) == 0 || len(runtime) > 16 {
		return false
	}
	running := false
	for _, item := range runtime {
		if item.Config == nil || item.State == nil || lifecycle.Protected(item.ID, item.Config.Labels) || item.Config.Labels["com.docker.compose.service"] == "nox-yard" {
			return false
		}
		state := item.State
		if state.Paused || state.Restarting || state.Dead || state.OOMKilled {
			return false
		}
		if state.Running {
			running = true
			if state.Health != nil && state.Health.Status != "healthy" {
				return false
			}
		} else if !expectedServices(preview.model)[item.Config.Labels["com.docker.compose.service"]].oneshot || state.Status != "exited" || state.ExitCode != 0 {
			return false
		}
	}
	return running
}
func (m *Manager) AssessAutomatic(ctx context.Context, name string) error {
	project, found, err := m.data.ManagedProject(name)
	if err != nil {
		return err
	}
	if !found || name == "nox-yard" {
		return fmt.Errorf("%w: project is absent or protected", ErrConflict)
	}
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return err
	}
	snapshot, err := m.snapshotDeployment(ctx, project, runtime)
	if err != nil {
		return err
	}
	variables, files, err := projectEnvironment(project)
	if err != nil {
		return err
	}
	preview, err := Validate(ctx, name, Source{YAML: project.YAML}, variables, files, project.ProjectDir)
	if err != nil {
		return err
	}
	if !automaticRuntime(preview, snapshot.Runtime) {
		return fmt.Errorf("%w: automatic updates require healthy running services, at most 16 containers and cannot target Yard/helpers", ErrConflict)
	}
	return nil
}
func (m *Manager) ScheduledUpdate(ctx context.Context, name string) (store.Job, error) {
	if err := m.AssessAutomatic(ctx, name); err != nil {
		return store.Job{}, err
	}
	project, found, err := m.data.ManagedProject(name)
	if err != nil {
		return store.Job{}, err
	}
	if !found {
		return store.Job{}, ErrConflict
	}
	return m.enqueue(ctx, project, "update", workerPayload{Project: project})
}
