package managed

import (
	"context"
	"errors"

	"github.com/mapherez/nox-yard/internal/lifecycle"
)

// RemovalSource supplements observed Engine ownership with Compose's external
// declarations. A volume can retain an old project label after being declared
// external; the label alone is not permission to delete it.
func (m *Manager) RemovalSource(ctx context.Context, name string) (lifecycle.RemovalSource, error) {
	source := lifecycle.RemovalSource{ExternalVolumes: map[string]bool{}, ExternalNetworks: map[string]bool{}}
	snapshot, err := m.readFileSnapshotMode(ctx, name, true)
	if errors.Is(err, ErrFilesMissing) {
		source.Fingerprint = "source-unavailable"
		return source, nil
	}
	if err != nil {
		source.Fingerprint = "source-inaccessible"
		return source, nil
	}
	source.Fingerprint = snapshot.Fingerprint
	if snapshot.Problem != "" {
		return source, nil
	}
	source.Known = true
	model := filePreview(snapshot).model
	for key, resource := range model.Volumes {
		if resource.External {
			source.ExternalVolumes[key] = true
			source.ExternalVolumes[resource.Name] = true
		}
	}
	for key, resource := range model.Networks {
		if resource.External {
			source.ExternalNetworks[key] = true
			source.ExternalNetworks[resource.Name] = true
		}
	}
	return source, nil
}
