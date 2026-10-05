package application

import (
	"context"
	"errors"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

var ErrDockerUnavailable = errors.New("Docker management is unavailable")

type InventoryReadError struct{ error }

func (e InventoryReadError) Unwrap() error { return e.error }

type ManagedReadError struct{ error }

func (e ManagedReadError) Unwrap() error { return e.error }

// Projects shares the browser's live inventory and stored project overlay.
// The wrappers retain the failing dependency without parsing error messages.
func (s *Service) Projects(ctx context.Context) (inventory.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return inventory.Snapshot{}, err
	}
	if s.Inventory == nil {
		return inventory.Snapshot{}, InventoryReadError{ErrDockerUnavailable}
	}
	snapshot, err := s.Inventory.Snapshot(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return inventory.Snapshot{}, InventoryReadError{err}
	}
	projects, err := s.Store.ManagedProjectsContext(ctx)
	if err != nil {
		return inventory.Snapshot{}, ManagedReadError{err}
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
	s.Changes.Apply(&snapshot)
	return snapshot, nil
}

func (s *Service) Action(ctx context.Context, id string, container bool, action lifecycle.Action) (lifecycle.Result, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Result{}, err
	}
	if s.Lifecycle == nil {
		return lifecycle.Result{}, ErrDockerUnavailable
	}
	target := id
	if container {
		target = "container:" + id
	}
	finish := s.Changes.Begin(target, string(action))
	defer finish()
	if container {
		return s.Lifecycle.Container(ctx, id, action)
	}
	return s.Lifecycle.Project(ctx, id, action)
}

func (s *Service) Pull(ctx context.Context, id string, container bool) (lifecycle.MaintenanceResult, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.MaintenanceResult{}, err
	}
	if s.Lifecycle == nil {
		return lifecycle.MaintenanceResult{}, ErrDockerUnavailable
	}
	if container {
		return s.Lifecycle.PullContainer(ctx, id)
	}
	return s.Lifecycle.PullProject(ctx, id)
}

// Keep storage failures separate from errors reported by the Docker reader.
func ProjectReadMessage(err error) string {
	var storage ManagedReadError
	if errors.As(err, &storage) {
		return "Unable to read managed projects."
	}
	return "Cannot reach the local Docker Engine. Check the Docker socket mount and access permissions."
}
