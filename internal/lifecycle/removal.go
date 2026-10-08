package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// RemovalItem describes one Docker resource or host path before deletion.
// Only items with action=remove are ever sent to a Docker delete endpoint.
type RemovalItem struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

type RemovalPlan struct {
	Fingerprint string        `json:"fingerprint"`
	Items       []RemovalItem `json:"items"`
}

type RemovalOutcome struct {
	RemovalItem
	Status string `json:"status"`
}

type RemovalReport struct {
	Items []RemovalOutcome `json:"items"`
}

func (m *Manager) PreviewRemoveContainer(ctx context.Context, id string) (RemovalPlan, error) {
	if err := m.mu.Lock(ctx); err != nil {
		return RemovalPlan{}, err
	}
	defer m.mu.Unlock()
	return m.removalPlan(ctx, "container:"+id)
}

func (m *Manager) PreviewRemoveProject(ctx context.Context, id string) (RemovalPlan, error) {
	if err := m.mu.Lock(ctx); err != nil {
		return RemovalPlan{}, err
	}
	defer m.mu.Unlock()
	return m.removalPlan(ctx, id)
}

func (m *Manager) RemoveContainer(ctx context.Context, id, fingerprint string) (RemovalReport, error) {
	return m.remove(ctx, "container:"+id, fingerprint)
}

func (m *Manager) RemoveProject(ctx context.Context, id, fingerprint string) (RemovalReport, error) {
	return m.remove(ctx, id, fingerprint)
}

func (m *Manager) remove(ctx context.Context, id, fingerprint string) (RemovalReport, error) {
	return m.RemoveWithOptions(ctx, id, fingerprint, false)
}

func (m *Manager) RemoveWithOptions(ctx context.Context, id, fingerprint string, removeVolumes bool) (RemovalReport, error) {
	if err := m.mu.Lock(ctx); err != nil {
		return RemovalReport{}, err
	}
	defer m.mu.Unlock()
	plan, err := m.removalPlanWithOptions(ctx, id, removeVolumes)
	if err != nil {
		return RemovalReport{}, err
	}
	if fingerprint == "" || fingerprint != plan.Fingerprint {
		return RemovalReport{}, ErrChanged
	}
	report := RemovalReport{Items: make([]RemovalOutcome, 0, len(plan.Items))}
	for _, item := range plan.Items {
		outcome := RemovalOutcome{RemovalItem: item, Status: "retained"}
		if item.Action == "remove" {
			if ctx.Err() != nil {
				outcome.Status, outcome.Reason = "failed", ctx.Err().Error()
			} else {
				var operationErr error
				switch item.Kind {
				case "container":
					// Docker applies a bounded SIGTERM grace period before a forced stop.
					_, stopErr := m.client.ContainerStop(ctx, item.ID, client.ContainerStopOptions{})
					if stopErr == nil || errdefs.IsNotModified(stopErr) {
						_, operationErr = m.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{})
					} else {
						operationErr = stopErr
					}
					if operationErr != nil && ctx.Err() == nil {
						// A restart policy or a failed graceful stop can race the
						// ordinary remove. The user confirmed removal of this ID.
						previous := operationErr
						_, operationErr = m.client.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true})
						if operationErr == nil {
							outcome.Reason = "Graceful stop/remove failed; Docker force-removed the container: " + previous.Error()
						} else {
							operationErr = fmt.Errorf("graceful stop/remove: %v; forced removal: %w", previous, operationErr)
						}
					}
					if operationErr == nil {
						outcome.Status = "removed"
					}
				case "volume", "network", "image":
					operationErr = m.removeUnusedResource(ctx, item)
					if operationErr == nil {
						outcome.Status = "removed"
					}
				default:
					operationErr = fmt.Errorf("unsupported resource kind %q", item.Kind)
				}
				if operationErr != nil {
					outcome.Status, outcome.Reason = "failed", operationErr.Error()
				}
			}
		}
		report.Items = append(report.Items, outcome)
	}
	return report, nil
}

func (m *Manager) removeUnusedResource(ctx context.Context, item RemovalItem) error {
	// A fresh host-wide check catches containers attached after confirmation.
	listed, err := m.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	for _, other := range listed.Items {
		switch item.Kind {
		case "image":
			if other.ImageID == item.ID {
				return fmt.Errorf("still used by container %s", containerName(other))
			}
		case "volume":
			for _, mount := range other.Mounts {
				if mount.Name == item.ID {
					return fmt.Errorf("still used by container %s", containerName(other))
				}
			}
		case "network":
			if other.NetworkSettings != nil {
				for _, endpoint := range other.NetworkSettings.Networks {
					if endpoint != nil && endpoint.NetworkID == item.ID {
						return fmt.Errorf("still used by container %s", containerName(other))
					}
				}
			}
		}
	}
	switch item.Kind {
	case "volume":
		_, err = m.client.VolumeRemove(ctx, item.ID, client.VolumeRemoveOptions{})
	case "network":
		_, err = m.client.NetworkRemove(ctx, item.ID, client.NetworkRemoveOptions{})
	case "image":
		_, err = m.client.ImageRemove(ctx, item.ID, client.ImageRemoveOptions{})
		if err == nil {
			_, inspectErr := m.client.ImageInspect(ctx, item.ID)
			if inspectErr == nil {
				return fmt.Errorf("image ID remains on the host, possibly referenced by another tag")
			}
			if !errdefs.IsNotFound(inspectErr) {
				return inspectErr
			}
		}
	}
	return err
}

func (m *Manager) removalPlan(ctx context.Context, id string) (RemovalPlan, error) {
	return m.removalPlanWithOptions(ctx, id, false)
}

func (m *Manager) PreviewRemoval(ctx context.Context, id string, removeVolumes bool) (RemovalPlan, error) {
	if err := m.mu.Lock(ctx); err != nil {
		return RemovalPlan{}, err
	}
	defer m.mu.Unlock()
	return m.removalPlanWithOptions(ctx, id, removeVolumes)
}

func (m *Manager) removalPlanWithOptions(ctx context.Context, id string, removeVolumes bool) (RemovalPlan, error) {
	all, err := m.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return RemovalPlan{}, err
	}
	name, compose := strings.CutPrefix(id, "compose:")
	containerID, single := strings.CutPrefix(id, "container:")
	if (!compose || name == "") && (!single || containerID == "") {
		return RemovalPlan{}, ErrNotFound
	}
	var targets []container.Summary
	for _, item := range all.Items {
		if (compose && item.Labels["com.docker.compose.project"] == name) || (single && item.ID == containerID) {
			targets = append(targets, item)
		}
	}
	if len(targets) == 0 {
		managed := false
		if compose && m.data != nil {
			_, managed, err = m.data.ManagedProject(name)
			if err != nil {
				return RemovalPlan{}, err
			}
		}
		if !managed {
			return RemovalPlan{}, ErrNotFound
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	plan := RemovalPlan{Items: []RemovalItem{}}
	targetIDs := make(map[string]bool, len(targets))
	volumes := map[string]bool{}
	images := map[string]string{}
	networks := map[string]string{}
	unique := map[string]bool{}
	add := func(item RemovalItem) {
		key := item.Kind + "\x00" + item.ID
		if !unique[key] {
			unique[key] = true
			plan.Items = append(plan.Items, item)
		}
	}
	if compose && m.data != nil {
		project, found, err := m.data.ManagedProject(name)
		if err != nil {
			return RemovalPlan{}, err
		}
		if found && project.ProjectDir != "" {
			file := path.Join(project.ProjectDir, "compose.yml")
			add(RemovalItem{Kind: "Compose file", ID: file, Name: file, Action: "keep", Reason: "Host source files remain after project removal."})
		}
	}
	for _, target := range targets {
		targetIDs[target.ID] = true
		inspected, err := m.inspectContainer(ctx, target.ID)
		if err != nil {
			return RemovalPlan{}, err
		}
		if isSelf(inspected.ID, inspected.Config.Labels) || isHelper(inspected.Config.Labels) {
			return RemovalPlan{}, ErrProtected
		}
		add(RemovalItem{Kind: "container", ID: inspected.ID, Name: strings.TrimPrefix(inspected.Name, "/"), Action: "remove"})
		if inspected.Image != "" {
			images[inspected.Image] = inspected.Config.Image
		}
		for _, mount := range inspected.Mounts {
			switch string(mount.Type) {
			case "volume":
				if mount.Name != "" {
					volumes[mount.Name] = true
				}
			case "bind":
				add(RemovalItem{Kind: "bind mount", ID: mount.Source, Name: mount.Source, Action: "keep", Reason: "Host files are outside Docker volume management; remove manually if no longer needed."})
			}
		}
		if inspected.NetworkSettings != nil {
			for networkName, endpoint := range inspected.NetworkSettings.Networks {
				if endpoint != nil && endpoint.NetworkID != "" {
					networks[endpoint.NetworkID] = networkName
				}
			}
		}
		if compose {
			for _, file := range strings.Split(inspected.Config.Labels["com.docker.compose.project.config_files"], ",") {
				file = strings.TrimSpace(file)
				if file != "" {
					add(RemovalItem{Kind: "Compose file", ID: file, Name: file, Action: "keep", Reason: "Host source file is not managed by the Docker Engine; remove manually if no longer needed."})
				}
			}
		}
	}
	volumeList, err := m.client.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return RemovalPlan{}, err
	}
	if len(volumeList.Warnings) > 0 {
		return RemovalPlan{}, fmt.Errorf("Docker could not list all volumes: %s", strings.Join(volumeList.Warnings, "; "))
	}
	for _, volume := range volumeList.Items {
		if compose && volume.Labels["com.docker.compose.project"] == name {
			volumes[volume.Name] = true
		}
	}
	volumeByName := map[string]map[string]string{}
	for _, volume := range volumeList.Items {
		volumeByName[volume.Name] = volume.Labels
	}
	for volumeName := range volumes {
		labels, exists := volumeByName[volumeName]
		if !exists {
			return RemovalPlan{}, fmt.Errorf("volume %s disappeared during preview", volumeName)
		}
		item := RemovalItem{Kind: "volume", ID: volumeName, Name: volumeName, Action: "remove"}
		if compose && labels["com.docker.compose.project"] == name {
			// Compose owns this volume, including unattached project volumes.
		} else if labels["com.docker.compose.project"] != "" || !anonymousVolumeName(volumeName) {
			item.Action, item.Reason = "keep", "External or unowned named volume; ownership cannot be established safely."
		}
		for _, other := range all.Items {
			if targetIDs[other.ID] {
				continue
			}
			for _, mount := range other.Mounts {
				if mount.Name == volumeName {
					item.Action, item.Reason = "keep", "Also used by container "+containerName(other)+"."
				}
			}
		}
		if !removeVolumes && item.Action == "remove" {
			item.Action, item.Reason = "keep", "Volume deletion was not selected."
		}
		add(item)
	}
	networkList, err := m.client.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return RemovalPlan{}, err
	}
	for _, network := range networkList.Items {
		if compose && network.Labels["com.docker.compose.project"] == name {
			networks[network.ID] = network.Name
		}
	}
	for _, network := range networkList.Items {
		if _, used := networks[network.ID]; !used {
			continue
		}
		item := RemovalItem{Kind: "network", ID: network.ID, Name: network.Name, Action: "remove"}
		if !compose || network.Labels["com.docker.compose.project"] != name {
			item.Action, item.Reason = "keep", "External or shared network; not owned by this Compose project."
		}
		for _, other := range all.Items {
			if targetIDs[other.ID] || other.NetworkSettings == nil {
				continue
			}
			for _, endpoint := range other.NetworkSettings.Networks {
				if endpoint != nil && endpoint.NetworkID == network.ID {
					item.Action, item.Reason = "keep", "Also used by container "+containerName(other)+"."
				}
			}
		}
		add(item)
	}
	for imageID, imageName := range images {
		item := RemovalItem{Kind: "image", ID: imageID, Name: imageName, Action: "remove"}
		if imageName == "" {
			item.Name = imageID
		}
		for _, other := range all.Items {
			if !targetIDs[other.ID] && other.ImageID == imageID {
				item.Action, item.Reason = "keep", "Also used by container "+containerName(other)+"."
			}
		}
		add(item)
	}
	// Execute containers first, then storage, networks, and images. Keep items
	// last so the report reads as completed work followed by leftovers.
	order := map[string]int{"container": 0, "volume": 1, "network": 2, "image": 3, "bind mount": 4, "Compose file": 5}
	sort.Slice(plan.Items, func(i, j int) bool {
		a, b := plan.Items[i], plan.Items[j]
		if order[a.Kind] != order[b.Kind] {
			return order[a.Kind] < order[b.Kind]
		}
		return a.ID < b.ID
	})
	encoded, _ := json.Marshal(struct {
		Target        string
		RemoveVolumes bool
		Items         []RemovalItem
	}{id, removeVolumes, plan.Items})
	sum := sha256.Sum256(encoded)
	plan.Fingerprint = hex.EncodeToString(sum[:])
	return plan, nil
}

func anonymousVolumeName(name string) bool {
	if len(name) != 64 {
		return false
	}
	_, err := hex.DecodeString(name)
	return err == nil
}
