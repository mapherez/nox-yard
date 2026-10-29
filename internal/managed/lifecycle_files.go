package managed

import (
	"context"
	"fmt"
	"slices"

	"github.com/mapherez/nox-yard/internal/lifecycle"
)

// LifecycleFiles operates existing containers through Compose; unlike up it
// never applies edited service configuration or creates missing containers.
func (m *Manager) LifecycleFiles(ctx context.Context, name string, action lifecycle.Action) (lifecycle.Result, error) {
	result := lifecycle.Result{Action: action}
	if action != lifecycle.Start && action != lifecycle.Stop && action != lifecycle.Restart {
		return result, lifecycle.ErrInvalidAction
	}
	source, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return result, err
	}
	before, err := m.runtime(ctx, name)
	if err != nil {
		return result, err
	}
	for _, item := range before {
		if item.Config != nil && lifecycle.Protected(item.ID, item.Config.Labels) {
			if action == lifecycle.Stop {
				return result, lifecycle.ErrProtected
			}
			// The existing self-restart helper must survive the web process.
			return result, ErrFilesMissing
		}
	}
	if action == lifecycle.Restart {
		for _, item := range before {
			if item.Config != nil && item.State != nil && !item.State.Running && slices.Contains(source.ActiveServices, item.Config.Labels["com.docker.compose.service"]) {
				result.Warnings = append(result.Warnings, "Restart includes selected stopped services; completed initializers may execute again.")
				break
			}
		}
	}
	definition, err := resolvedDefinition(filePreview(source), source.Directory, nil)
	if err != nil {
		return result, err
	}
	current, err := m.readFileSnapshot(ctx, name)
	if err != nil || current.Fingerprint != source.Fingerprint {
		return result, ErrConflict
	}
	if err := runResolvedCompose(ctx, name, source.Directory, definition, string(action)); err != nil {
		return result, err
	}
	after, err := m.runtime(ctx, name)
	if err != nil {
		return result, err
	}
	states := map[string]bool{}
	for _, item := range after {
		if item.State != nil {
			states[item.ID] = item.State.Running
		}
	}
	for _, item := range before {
		if item.Config == nil || item.State == nil {
			continue
		}
		if !slices.Contains(source.ActiveServices, item.Config.Labels["com.docker.compose.service"]) {
			result.Skipped++
			continue
		}
		wanted := action != lifecycle.Stop
		if state, found := states[item.ID]; !found || state != wanted {
			// Completed one-shot services may finish before the post-read.
			if action != lifecycle.Stop && expectedServices(filePreview(source).model)[item.Config.Labels["com.docker.compose.service"]].oneshot {
				completed := false
				for _, actual := range after {
					if actual.ID == item.ID && actual.State != nil && !actual.State.Running && actual.State.ExitCode == 0 {
						completed = true
					}
				}
				if !completed {
					result.Failed++
					result.Errors = append(result.Errors, "initializer did not complete successfully")
					continue
				}
				result.Succeeded++
				continue
			}
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("service %s did not reach the requested state", item.Config.Labels["com.docker.compose.service"]))
		} else {
			result.Succeeded++
		}
	}
	return result, nil
}
