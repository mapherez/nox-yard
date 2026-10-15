package managed

import (
	"context"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

// PullFiles participates in the application's existing synchronous pull job.
func (m *Manager) PullFiles(ctx context.Context, name string) (lifecycle.MaintenanceResult, error) {
	result := lifecycle.MaintenanceResult{}
	if err := m.configurationReady(name); err != nil {
		return result, err
	}
	source, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return result, err
	}
	if err := runResolvedCompose(ctx, name, source.Directory, string(source.Resolved), "pull"); err != nil {
		return result, err
	}
	preview := filePreview(source)
	seen := map[string]bool{}
	for service := range expectedServices(preview.model) {
		reference := preview.model.Services[service].Image
		if !seen[reference] {
			seen[reference] = true
			result.Images = append(result.Images, reference)
			result.Succeeded++
		}
	}
	return result, nil
}
