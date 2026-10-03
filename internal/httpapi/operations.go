package httpapi

import (
	"context"
	"errors"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

var errDockerUnavailable = errors.New("Docker management is unavailable")

type inventoryReadError struct{ error }

func (e inventoryReadError) Unwrap() error { return e.error }

type managedReadError struct{ error }

func (e managedReadError) Unwrap() error { return e.error }

// readProjects shares the browser's live inventory and stored project overlay.
// The wrappers retain the failing dependency without parsing error messages.
func (s *Server) readProjects(ctx context.Context) (inventory.Snapshot, error) {
	if s.inventory == nil {
		return inventory.Snapshot{}, inventoryReadError{errDockerUnavailable}
	}
	snapshot, err := s.inventory.Snapshot(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return inventory.Snapshot{}, inventoryReadError{err}
	}
	projects, err := s.store.ManagedProjectsContext(ctx)
	if err != nil {
		return inventory.Snapshot{}, managedReadError{err}
	}
	seen := map[string]bool{}
	for i := range snapshot.Projects {
		seen[snapshot.Projects[i].ID] = true
		for _, item := range projects {
			if snapshot.Projects[i].ID == "compose:"+item.Name {
				snapshot.Projects[i].Kind = "managed-compose"
			}
		}
	}
	for _, item := range projects {
		if !seen["compose:"+item.Name] {
			snapshot.Projects = append(snapshot.Projects, inventory.Project{ID: "compose:" + item.Name, Name: item.Name, Kind: "managed-compose", State: "stopped", Health: "none", Containers: []inventory.Container{}})
		}
	}
	s.changes.Apply(&snapshot)
	return snapshot, nil
}

func (s *Server) runAction(ctx context.Context, id string, container bool, action lifecycle.Action) (lifecycle.Result, error) {
	if s.lifecycle == nil {
		return lifecycle.Result{}, errDockerUnavailable
	}
	target := id
	if container {
		target = "container:" + id
	}
	finish := s.changes.Begin(target, string(action))
	defer finish()
	if container {
		return s.lifecycle.Container(ctx, id, action)
	}
	return s.lifecycle.Project(ctx, id, action)
}

func (s *Server) runPull(ctx context.Context, id string, container bool) (lifecycle.MaintenanceResult, error) {
	if s.lifecycle == nil {
		return lifecycle.MaintenanceResult{}, errDockerUnavailable
	}
	if container {
		return s.lifecycle.PullContainer(ctx, id)
	}
	return s.lifecycle.PullProject(ctx, id)
}

// Keep storage failures separate from errors reported by the Docker reader.
func projectReadMessage(err error) string {
	var storage managedReadError
	if errors.As(err, &storage) {
		return "Unable to read managed projects."
	}
	return "Cannot reach the local Docker Engine. Check the Docker socket mount and access permissions."
}
