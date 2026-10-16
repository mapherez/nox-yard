package application

import (
	"context"
	"errors"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/store"
	"strings"
)

func (s *Service) RecreatePreview(ctx context.Context, input recreate.Request) (recreate.Preview, error) {
	if err := ctx.Err(); err != nil {
		return recreate.Preview{}, err
	}
	if !ValidTarget(input.ID, false) {
		return recreate.Preview{}, recreate.ErrInvalid
	}
	if input.Service != "" && (!strings.HasPrefix(input.ID, "compose:") || input.Operation != "recreate") {
		return recreate.Preview{}, recreate.ErrInvalid
	}
	if name, compose := strings.CutPrefix(input.ID, "compose:"); compose {
		if controller, ok := s.Managed.(fileController); ok {
			var preview recreate.Preview
			var err error
			if input.Service != "" {
				scoped, ok := s.Managed.(interface {
					ServiceFilePreview(context.Context, string, string, string) (recreate.Preview, error)
				})
				if !ok {
					return preview, ErrUnavailable
				}
				preview, err = scoped.ServiceFilePreview(ctx, name, input.Operation, input.Service)
			} else {
				preview, err = controller.FilePreview(ctx, name, input.Operation)
			}
			if !errors.Is(err, managed.ErrFilesMissing) {
				return preview, err
			}
		}
	}
	if s.Recreator == nil {
		return recreate.Preview{}, ErrUnavailable
	}
	return s.Recreator.Preview(ctx, input)
}
func (s *Service) RecreateSubmit(ctx context.Context, input recreate.Request) (store.Job, error) {
	if err := ctx.Err(); err != nil {
		return store.Job{}, err
	}
	if !ValidTarget(input.ID, false) {
		return store.Job{}, recreate.ErrInvalid
	}
	if input.Service != "" && (!strings.HasPrefix(input.ID, "compose:") || input.Operation != "recreate") {
		return store.Job{}, recreate.ErrInvalid
	}
	if name, compose := strings.CutPrefix(input.ID, "compose:"); compose {
		if controller, ok := s.Managed.(fileController); ok {
			if !ValidRemoval(input.Confirm, input.Fingerprint) {
				return store.Job{}, recreate.ErrInvalid
			}
			var job store.Job
			var err error
			if input.Service != "" {
				scoped, ok := s.Managed.(interface {
					ServiceFileOperation(context.Context, string, string, string, string) (store.Job, error)
				})
				if !ok {
					return job, ErrUnavailable
				}
				job, err = scoped.ServiceFileOperation(ctx, name, input.Operation, input.Service, input.Fingerprint)
			} else {
				job, err = controller.FileOperation(ctx, name, input.Operation, input.Fingerprint)
			}
			if !errors.Is(err, managed.ErrFilesMissing) {
				s.Changes.Notify(inventory.Change{Inventory: true})
				return job, err
			}
		}
	}
	if s.Recreator == nil {
		return store.Job{}, ErrUnavailable
	}
	job, err := s.Recreator.Submit(ctx, input)
	s.Changes.Notify(inventory.Change{Inventory: true})
	return job, err
}
