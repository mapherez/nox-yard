package application

import (
	"context"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/store"
)

func (s *Service) RecreatePreview(ctx context.Context, input recreate.Request) (recreate.Preview, error) {
	if err := ctx.Err(); err != nil {
		return recreate.Preview{}, err
	}
	if !ValidTarget(input.ID, false) {
		return recreate.Preview{}, recreate.ErrInvalid
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
	if s.Recreator == nil {
		return store.Job{}, ErrUnavailable
	}
	job, err := s.Recreator.Submit(ctx, input)
	s.Changes.Notify(inventory.Change{Inventory: true})
	return job, err
}
