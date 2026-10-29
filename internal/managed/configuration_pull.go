package managed

import (
	"context"
	"sort"

	"github.com/mapherez/nox-yard/internal/lifecycle"
)

// PullFiles participates in the application's existing synchronous pull job.
func (m *Manager) PullFiles(ctx context.Context, name string) (lifecycle.MaintenanceResult, error) {
	return m.pullFiles(ctx, name, "")
}

// PullServiceFiles uses the current source reference without pulling siblings.
func (m *Manager) PullServiceFiles(ctx context.Context, name, service string) (lifecycle.MaintenanceResult, error) {
	if service == "" {
		return lifecycle.MaintenanceResult{}, ErrInvalidSource
	}
	return m.pullFiles(ctx, name, service)
}

func (m *Manager) pullFiles(ctx context.Context, name, service string) (lifecycle.MaintenanceResult, error) {
	result := lifecycle.MaintenanceResult{}
	source, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return result, err
	}
	preview := scopedFilePreview(source, service)
	services := expectedServices(preview.model)
	args := []string{"pull"}
	if service != "" {
		if _, exists := preview.model.Services[service]; !exists {
			return result, ErrInvalidSource
		}
		services = map[string]serviceExpectation{service: {}}
		args = append(args, service)
	}
	definition, err := resolvedDefinition(preview, source.Directory, nil)
	if err != nil {
		return result, err
	}
	if err := runResolvedCompose(ctx, name, source.Directory, definition, args...); err != nil {
		return result, err
	}
	seen := map[string]bool{}
	for service := range services {
		reference := preview.model.Services[service].Image
		if !seen[reference] {
			seen[reference] = true
			result.Images = append(result.Images, reference)
			result.Succeeded++
		}
	}
	sort.Strings(result.Images)
	return result, nil
}
