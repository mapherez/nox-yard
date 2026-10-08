package recreate

import (
	"context"
	"fmt"

	"github.com/mapherez/nox-yard/internal/lifecycle"
)

func automaticRuntime(state *journal) bool {
	running := false
	for _, target := range state.Entries {
		if lifecycle.Protected(target.Old.ID, target.Old.Config.Labels) || target.Old.Config.Labels["com.docker.compose.service"] == "nox-yard" {
			return false
		}
		if target.Old.State.Running {
			running = true
		} else if !target.OneShot {
			return false
		}
	}
	return running
}
func (m *Manager) AutomaticPreview(ctx context.Context, input Request) (Preview, error) {
	state, preview, err := m.snapshot(ctx, input)
	if err != nil {
		return Preview{}, err
	}
	if !automaticRuntime(state) {
		return Preview{}, fmt.Errorf("%w: automatic updates require running services and cannot target Yard/helpers", ErrUnsupported)
	}
	return preview, nil
}
