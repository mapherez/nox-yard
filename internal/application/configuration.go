package application

import (
	"context"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/store"
	"strings"
)

type fileController interface {
	Configuration(context.Context, string) (managed.FileConfigurationView, error)
	SaveConfiguration(context.Context, string, managed.ConfigurationEdit) (managed.FileConfigurationView, error)
	RetryConfigurationMigration(context.Context, string, bool) error
	FilePreview(context.Context, string, string) (recreate.Preview, error)
	FileOperation(context.Context, string, string, string) (store.Job, error)
}

func (s *Service) ProjectConfiguration(ctx context.Context, id string) (managed.FileConfigurationView, error) {
	name, ok := strings.CutPrefix(id, "compose:")
	if !ok || !ValidTarget(id, false) {
		return managed.FileConfigurationView{State: "ready", Files: []managed.ConfigurationFile{}, Variables: []managed.ConfigurationVariable{}}, nil
	}
	controller, ok := s.Managed.(fileController)
	if !ok {
		return managed.FileConfigurationView{}, ErrUnavailable
	}
	return controller.Configuration(ctx, name)
}
func (s *Service) SaveProjectConfiguration(ctx context.Context, id string, input managed.ConfigurationEdit) (managed.FileConfigurationView, error) {
	name, ok := strings.CutPrefix(id, "compose:")
	if !ok || !ValidTarget(id, false) {
		return managed.FileConfigurationView{}, ErrInvalidRemoval
	}
	controller, ok := s.Managed.(fileController)
	if !ok {
		return managed.FileConfigurationView{}, ErrUnavailable
	}
	job, err := s.beginOperation(ctx, id, "edit_configuration")
	if err != nil {
		return managed.FileConfigurationView{}, err
	}
	view, err := controller.SaveConfiguration(jobs.WithJob(ctx, job), name, input)
	s.finishOperation(job, err != nil, err)
	return view, err
}
func (s *Service) RetryProjectMigration(ctx context.Context, id string, useCurrent bool) error {
	name, ok := strings.CutPrefix(id, "compose:")
	if !ok || !ValidTarget(id, false) {
		return ErrInvalidRemoval
	}
	controller, ok := s.Managed.(fileController)
	if !ok {
		return ErrUnavailable
	}
	return controller.RetryConfigurationMigration(ctx, name, useCurrent)
}
