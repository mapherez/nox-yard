package lifecycle

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Pull changes only the host's local image cache. Remove changes only
// containers; volumes, networks, and images are deliberately retained.
type MaintenanceResult struct {
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Images    []string `json:"images,omitempty"`
	Errors    []string `json:"errors,omitempty"`
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
			continue
		}
		result.Succeeded++
		result.Images = append(result.Images, image)
	}
	return result
}

func (m *Manager) RemoveContainer(ctx context.Context, id string) (MaintenanceResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, err := m.inspectContainer(ctx, id)
	if err != nil {
		return MaintenanceResult{}, err
	}
	if isSelf(item.ID, item.Config.Labels) || isHelper(item.Config.Labels) {
		return MaintenanceResult{}, ErrProtected
	}
	_, err = m.client.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	if err != nil {
		return MaintenanceResult{Failed: 1, Errors: []string{err.Error()}}, nil
	}
	return MaintenanceResult{Succeeded: 1}, nil
}

func (m *Manager) RemoveProject(ctx context.Context, id string, expectedIDs []string) (MaintenanceResult, error) {
	if containerID, ok := strings.CutPrefix(id, "container:"); ok {
		if len(expectedIDs) != 1 || expectedIDs[0] != containerID {
			return MaintenanceResult{}, ErrChanged
		}
		return m.RemoveContainer(ctx, containerID)
	}
	name, ok := strings.CutPrefix(id, "compose:")
	if !ok || name == "" {
		return MaintenanceResult{}, ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	targets, err := m.projectTargets(ctx, name)
	if err != nil {
		return MaintenanceResult{}, err
	}
	if len(targets) != len(expectedIDs) {
		return MaintenanceResult{}, ErrChanged
	}
	expected := append([]string(nil), expectedIDs...)
	sort.Strings(expected)
	for index, target := range targets {
		if target.ID != expected[index] {
			return MaintenanceResult{}, ErrChanged
		}
	}
	// Refuse the entire group before removing anything if NoX Yard or one of
	// its temporary maintenance workers belongs to it.
	for _, target := range targets {
		item, err := m.inspectContainer(ctx, target.ID)
		if err != nil {
			return MaintenanceResult{}, err
		}
		if item.Config.Labels["com.docker.compose.project"] != name {
			return MaintenanceResult{}, ErrNotFound
		}
		if isSelf(item.ID, item.Config.Labels) || isHelper(item.Config.Labels) {
			return MaintenanceResult{}, ErrProtected
		}
	}
	result := MaintenanceResult{}
	for _, item := range targets {
		if ctx.Err() != nil {
			break
		}
		_, err := m.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true})
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", containerName(item), err))
		} else {
			result.Succeeded++
		}
	}
	return result, nil
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
	return item.Config.Image, nil
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
