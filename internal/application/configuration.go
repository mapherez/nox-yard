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
	FilePreview(context.Context, string, string) (recreate.Preview, error)
	FileOperation(context.Context, string, string, string) (store.Job, error)
}

func (s *Service) ProjectConfiguration(ctx context.Context, id string) (managed.FileConfigurationView, error) {
	if err := ctx.Err(); err != nil {
		return managed.FileConfigurationView{}, err
	}
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

// ReadProjectConfiguration shares the disclosure policy between transports.
// File contents can contain credentials, including inside Compose itself.
func (s *Service) ReadProjectConfiguration(ctx context.Context, id string, reveal bool) (managed.FileConfigurationView, error) {
	view, err := s.ProjectConfiguration(ctx, id)
	if !reveal {
		view = RedactProjectConfiguration(view)
	}
	return view, err
}

// RedactProjectConfiguration returns metadata without modifying the source view.
func RedactProjectConfiguration(view managed.FileConfigurationView) managed.FileConfigurationView {
	view.Files = append([]managed.ConfigurationFile{}, view.Files...)
	view.Variables = append([]managed.ConfigurationVariable{}, view.Variables...)
	for i := range view.Files {
		view.Files[i].Content = ""
	}
	for i := range view.Variables {
		view.Variables[i].Value = ""
	}
	return view
}

func (s *Service) SaveProjectConfiguration(ctx context.Context, id string, input managed.ConfigurationEdit) (managed.FileConfigurationView, error) {
	if err := ctx.Err(); err != nil {
		return managed.FileConfigurationView{}, err
	}
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
