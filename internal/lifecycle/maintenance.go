package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Pull changes only the host's local image cache.
type MaintenanceResult struct {
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Images    []string  `json:"images,omitempty"`
	Errors    []string  `json:"errors,omitempty"`
	Failures  []Failure `json:"-"`
}

func (m *Manager) PullContainer(ctx context.Context, id string) (MaintenanceResult, error) {
	image, err := m.containerImage(ctx, id)
	if err != nil {
		return MaintenanceResult{}, err
	}
	return m.pullImages(ctx, []string{image}), nil
}

func (m *Manager) PullProject(ctx context.Context, id string) (MaintenanceResult, error) {
	if containerID, ok := strings.CutPrefix(id, "container:"); ok {
		return m.PullContainer(ctx, containerID)
	}
	name, ok := strings.CutPrefix(id, "compose:")
	if !ok || name == "" {
		return MaintenanceResult{}, ErrNotFound
	}
	targets, err := m.projectTargets(ctx, name)
	if err != nil {
		return MaintenanceResult{}, err
	}
	unique := make(map[string]struct{})
	for _, item := range targets {
		image, err := m.containerImage(ctx, item.ID)
		if err != nil {
			return MaintenanceResult{}, err
		}
		unique[image] = struct{}{}
	}
	images := make([]string, 0, len(unique))
	for image := range unique {
		images = append(images, image)
	}
	sort.Strings(images)
	return m.pullImages(ctx, images), nil
}

func (m *Manager) pullImages(ctx context.Context, images []string) MaintenanceResult {
	result := MaintenanceResult{}
	for _, image := range images {
		if ctx.Err() != nil {
			result.Failures = append(result.Failures, Failure{Target: image, Cause: ctx.Err()})
			break
		}
		response, err := m.client.ImagePull(ctx, image, client.ImagePullOptions{})
		if err == nil {
			err = response.Wait(ctx)
			_ = response.Close()
		}
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", image, err))
			result.Failures = append(result.Failures, Failure{Target: image, Cause: err})
			continue
		}
		result.Succeeded++
		result.Images = append(result.Images, image)
	}
	return result
}

func (m *Manager) containerImage(ctx context.Context, id string) (string, error) {
	item, err := m.inspectContainer(ctx, id)
	if err != nil {
		return "", err
	}
	if item.Config.Image == "" {
		return "", ErrNotFound
	}
	if isHelper(item.Config.Labels) || isSelf(item.ID, item.Config.Labels) {
		return "", ErrProtected
	}
	return imageidentity.Reference(item.Config.Image, item.Config.Labels), nil
}

func (m *Manager) inspectContainer(ctx context.Context, id string) (container.InspectResponse, error) {
	inspected, err := m.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return container.InspectResponse{}, ErrNotFound
	}
	if err != nil {
		return container.InspectResponse{}, err
	}
	item := inspected.Container
	if item.ID != id || item.Config == nil {
		return container.InspectResponse{}, ErrNotFound
	}
	return item, nil
}
