package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/schedule"
	"github.com/mapherez/nox-yard/internal/store"
)

type ScheduleStatus struct {
	store.ProjectSchedule
	Timezone string `json:"timezone"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}

var ErrScheduleUnsupported = errors.New("automatic updates require a supported, healthy running project; review its update preview")

func (s *Service) scheduleProject(ctx context.Context, target string) (inventory.Project, error) {
	snapshot, err := s.Projects(ctx)
	if err != nil {
		return inventory.Project{}, err
	}
	for _, project := range snapshot.Projects {
		if project.ID == target {
			return project, nil
		}
	}
	return inventory.Project{}, ErrUnavailable
}
func protectedSchedule(project inventory.Project) bool {
	if project.Name == "nox-yard" {
		return true
	}
	for _, item := range project.Containers {
		if item.Service == "nox-yard" || strings.HasPrefix(item.Name, "nox-yard-update-") {
			return true
		}
	}
	return false
}
func (s *Service) ProjectSchedule(ctx context.Context, target string) (ScheduleStatus, error) {
	if !ValidTarget(target, false) {
		return ScheduleStatus{}, ErrInvalidRemoval
	}
	if s.Scheduler == nil {
		return ScheduleStatus{}, ErrUnavailable
	}
	row, _, err := s.Store.ProjectSchedule(ctx, target)
	if err != nil {
		return ScheduleStatus{}, err
	}
	result := ScheduleStatus{ProjectSchedule: row, Timezone: s.Scheduler.Location.String(), Eligible: true}
	project, err := s.scheduleProject(ctx, target)
	if err != nil {
		result.Eligible = false
		result.Reason = "Project availability could not be verified."
		return result, nil
	}
	if protectedSchedule(project) {
		result.Eligible = false
		result.Reason = "Yard and helpers use their dedicated update path."
	}
	return result, nil
}
func (s *Service) SetProjectSchedule(ctx context.Context, target string, enabled bool) (ScheduleStatus, error) {
	return s.setProjectSchedule(ctx, target, enabled, nil)
}
func (s *Service) SetProjectScheduleTime(ctx context.Context, target string, enabled bool, at string) (ScheduleStatus, error) {
	return s.setProjectSchedule(ctx, target, enabled, &at)
}
func (s *Service) setProjectSchedule(ctx context.Context, target string, enabled bool, at *string) (ScheduleStatus, error) {
	if !ValidTarget(target, false) {
		return ScheduleStatus{}, ErrInvalidRemoval
	}
	if s.Scheduler == nil {
		return ScheduleStatus{}, ErrUnavailable
	}
	row, found, err := s.Store.ProjectSchedule(ctx, target)
	if err != nil {
		return ScheduleStatus{}, err
	}
	checkTime := row.Time
	if at != nil {
		checkTime = *at
	}
	hour, minute, err := schedule.ParseTime(checkTime)
	if err != nil {
		return ScheduleStatus{}, err
	}
	key := target
	if found {
		key = row.Key
		if !enabled {
			target = row.TargetID
		}
	}
	if enabled {
		project, err := s.scheduleProject(ctx, target)
		if err != nil {
			return ScheduleStatus{}, err
		}
		if protectedSchedule(project) {
			return ScheduleStatus{}, ErrScheduleUnsupported
		}
		if err := s.assessAutomatic(ctx, project); err != nil {
			if ctx.Err() != nil {
				return ScheduleStatus{}, ctx.Err()
			}
			return ScheduleStatus{}, ErrScheduleUnsupported
		}
		if !found && s.ResourceKeys != nil {
			resources, err := s.ResourceKeys(ctx, target)
			if err != nil {
				return ScheduleStatus{}, err
			}
			for _, resource := range resources {
				if resource != target && strings.HasPrefix(target, "container:") && ValidTarget(resource, false) && strings.HasPrefix(resource, "container:") {
					key = resource
					break
				}
			}
		}
	}
	next := int64(0)
	if enabled {
		next = schedule.NextAt(time.Now(), s.Scheduler.Location, hour, minute).Unix()
	}
	if _, err := s.Store.SetProjectScheduleAt(ctx, key, target, enabled, next, checkTime); err != nil {
		return ScheduleStatus{}, err
	}
	s.Changes.Notify(inventory.Change{Inventory: true})
	return s.ProjectSchedule(ctx, target)
}
func (s *Service) assessAutomatic(ctx context.Context, project inventory.Project) error {
	if project.Kind == "managed-compose" {
		manager, ok := s.Managed.(interface {
			AssessAutomatic(context.Context, string) error
		})
		if !ok {
			return ErrUnavailable
		}
		return manager.AssessAutomatic(ctx, project.Name)
	}
	manager, ok := s.Recreator.(interface {
		AutomaticPreview(context.Context, recreate.Request) (recreate.Preview, error)
	})
	if !ok {
		return ErrUnavailable
	}
	_, err := manager.AutomaticPreview(ctx, recreate.Request{ID: project.ID, Operation: "update"})
	return err
}
func (s *Service) RunScheduledUpdate(ctx context.Context, row store.ProjectSchedule, occurrence store.ScheduleOccurrence) error {
	skip := func(reason string) error { return s.Store.SkipSchedule(ctx, row, occurrence, reason) }
	project, err := s.scheduleProject(ctx, row.TargetID)
	if err != nil {
		return skip("Project is absent or Docker is unavailable; no automatic replacement was requested.")
	}
	if protectedSchedule(project) {
		return skip("Yard/helpers are protected from project auto-update.")
	}
	if project.Operation != "" {
		return skip("Project is busy; automatic update skipped for this occurrence.")
	}
	ctx = store.WithSchedule(ctx, occurrence)
	if project.Kind == "managed-compose" {
		manager, ok := s.Managed.(interface {
			ScheduledUpdate(context.Context, string) (store.Job, error)
		})
		if !ok {
			return skip("Managed automatic updates are unavailable.")
		}
		_, err = manager.ScheduledUpdate(ctx, project.Name)
	} else {
		manager, ok := s.Recreator.(interface {
			AutomaticPreview(context.Context, recreate.Request) (recreate.Preview, error)
		})
		if !ok {
			return skip("External automatic updates are unavailable.")
		}
		input := recreate.Request{ID: project.ID, Operation: "update", Confirm: true}
		preview, previewErr := manager.AutomaticPreview(ctx, input)
		if previewErr != nil {
			err = previewErr
		} else {
			input.Fingerprint = preview.Fingerprint
			_, err = s.Recreator.Submit(ctx, input)
		}
	}
	if errors.Is(err, store.ErrScheduleChanged) {
		return err
	}
	if errors.Is(err, store.ErrOperationConflict) {
		return skip("Project is busy or awaiting recovery; automatic update skipped for this occurrence.")
	}
	if err != nil {
		return skip("Automatic update assessment failed: stopped, unhealthy, unsupported or changed configuration. Review the manual update preview.")
	}
	return nil
}
