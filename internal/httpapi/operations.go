package httpapi

import (
	"context"
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

var errDockerUnavailable = application.ErrDockerUnavailable

type inventoryReadError = application.InventoryReadError
type managedReadError = application.ManagedReadError

func (s *Server) readProjects(ctx context.Context) (inventory.Snapshot, error) {
	return s.application.Projects(ctx)
}
func (s *Server) runAction(ctx context.Context, id string, container bool, action lifecycle.Action) (lifecycle.Result, error) {
	return s.application.Action(ctx, id, container, action)
}
func (s *Server) runPull(ctx context.Context, id string, container bool) (lifecycle.MaintenanceResult, error) {
	return s.application.Pull(ctx, id, container)
}
func projectReadMessage(err error) string { return application.ProjectReadMessage(err) }
