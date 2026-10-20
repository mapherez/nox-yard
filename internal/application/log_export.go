package application

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/logexport"
	"github.com/mapherez/nox-yard/internal/managed"
)

func (s *Service) ProjectLogExport(ctx context.Context, id, containerID string, content *string) (logexport.Status, error) {
	if !ValidTarget(id, false) {
		return logexport.Status{}, fmt.Errorf("%w: invalid project ID", managed.ErrInvalidSource)
	}
	if content != nil && (!containerIDPattern.MatchString(containerID) || len(*content) == 0 || len(*content) > logexport.MaxBytes || !utf8.ValidString(*content)) {
		return logexport.Status{}, fmt.Errorf("%w: invalid or oversized log snapshot", managed.ErrInvalidSource)
	}
	if s.Inventory == nil {
		return logexport.Status{}, ErrDockerUnavailable
	}
	snapshot, err := s.Inventory.Snapshot(ctx)
	if err != nil {
		return logexport.Status{}, err
	}
	var selected *inventory.Project
	for _, project := range snapshot.Projects {
		if project.ID == id {
			selected = &project
			break
		}
	}
	if selected == nil {
		return logexport.Status{}, inventory.ErrContainerNotFound
	}
	if content != nil {
		member := false
		for _, container := range selected.Containers {
			if container.ID == containerID {
				member = true
				break
			}
		}
		if !member {
			return logexport.Status{}, inventory.ErrContainerNotFound
		}
	}
	controller, ok := s.Managed.(interface {
		ProjectLogExport(context.Context, string, *string) (logexport.Status, error)
	})
	if !ok {
		return logexport.Status{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return controller.ProjectLogExport(bounded, selected.Name, content)
}
