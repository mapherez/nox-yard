package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
)

func serviceRuntime(runtime []container.InspectResponse, service string) []container.InspectResponse {
	if service == "" {
		return runtime
	}
	selected := []container.InspectResponse{}
	for _, item := range runtime {
		if item.Config != nil && item.Config.Labels["com.docker.compose.service"] == service {
			selected = append(selected, item)
		}
	}
	return selected
}

func otherServiceRuntime(runtime []container.InspectResponse, service string) []container.InspectResponse {
	other := []container.InspectResponse{}
	for _, item := range runtime {
		if item.Config == nil || item.Config.Labels["com.docker.compose.service"] != service {
			other = append(other, item)
		}
	}
	return other
}

func fileOperationFingerprint(source FileSnapshot, runtime []container.InspectResponse, operation, service string) string {
	type observedState struct {
		Running   bool
		StartedAt string
		Restarts  int
	}
	states := map[string]observedState{}
	for _, item := range runtime {
		if item.State != nil {
			states[item.ID] = observedState{item.State.Running, item.State.StartedAt, item.RestartCount}
		}
	}
	encoded, _ := json.Marshal(struct {
		Files, Runtime, Operation, Service string
		Running                            map[string]observedState
	}{source.Fingerprint, adoptionRuntimeFingerprint(runtime), operation, service, states})
	return fingerprint(encoded)
}

func fileServiceRuntime(source FileSnapshot, runtime []container.InspectResponse, service string) ([]container.InspectResponse, error) {
	if service == "" {
		return selectedFileRuntime(source, runtime, service), nil
	}
	if _, exists := filePreview(source).model.Services[service]; !exists {
		return nil, fmt.Errorf("%w: the selected service is absent from the current Compose configuration", ErrInvalidSource)
	}
	selected := serviceRuntime(runtime, service)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: the selected service has no existing containers", ErrConflict)
	}
	if _, active := expectedServices(scopedFilePreview(source, service).model)[service]; !active {
		return nil, fmt.Errorf("%w: the selected service must have at least one configured replica", ErrInvalidSource)
	}
	for _, item := range selected {
		if item.State == nil || selected[0].State == nil || item.State.Running != selected[0].State.Running {
			return nil, fmt.Errorf("%w: selected service replicas must share their running/stopped state", ErrConflict)
		}
	}
	for _, item := range otherServiceRuntime(runtime, service) {
		if item.HostConfig == nil {
			return nil, ErrConflict
		}
		refs := append([]string{string(item.HostConfig.NetworkMode), string(item.HostConfig.IpcMode), string(item.HostConfig.PidMode)}, item.HostConfig.VolumesFrom...)
		refs = append(refs, item.HostConfig.Links...)
		for _, ref := range refs {
			ref = strings.TrimPrefix(ref, "container:")
			ref, _, _ = strings.Cut(ref, ":")
			ref = strings.TrimPrefix(ref, "/")
			for _, target := range selected {
				if ref == target.ID || ref == strings.TrimPrefix(target.Name, "/") || (len(ref) >= 12 && strings.HasPrefix(target.ID, ref)) {
					return nil, fmt.Errorf("%w: another service shares the selected service's namespace, links or volumes-from", ErrInvalidSource)
				}
			}
		}
	}
	return selected, nil
}

func scopedFilePreview(source FileSnapshot, service string) Preview {
	preview := filePreview(source)
	if service != "" {
		definition := preview.model.Services[service]
		if expectedServices(preview.model)[service].oneshot {
			if definition.Labels == nil {
				definition.Labels = map[string]string{}
			}
			definition.Labels["nox-yard.lifecycle"] = "oneshot"
		}
		definition.Profiles = nil // Explicit service selection activates that service.
		preview.model.Services = map[string]modelService{service: definition}
		preview.model.ActiveServices = []string{service}
	}
	return preview
}

func fileCreateArgs(operation, service string) []string {
	args := []string{"up", "--no-start", "--no-build", "--pull", "never"}
	if service != "" {
		args = append(args, "--no-deps")
	}
	if operation == "recreate" {
		args = append(args, "--force-recreate")
	}
	if service != "" {
		args = append(args, service)
	}
	return args
}

func fileServiceVerification(preview Preview, previous []container.InspectResponse, service string) Preview {
	if service != "" && len(previous) > 0 && previous[0].State != nil && !previous[0].State.Running {
		definition := preview.model.Services[service]
		zero := 0
		definition.Scale, definition.Deploy.Replicas = &zero, nil
		preview.model.Services[service] = definition
		return preview
	}
	return fileVerification(preview, previous)
}

func (m *Manager) verifyFileDeployment(ctx context.Context, name string, model composeModel, targets map[string]string, work *fileDeployment) error {
	if work.Service == "" {
		current, err := m.runtime(ctx, name)
		if err != nil {
			return err
		}
		if !unchangedImages(model, selectedFileRuntime(work.Source, current, ""), targets) {
			return store.ErrJobChanged
		}
		selected := map[string]bool{}
		for _, item := range selectedFileRuntime(work.Source, work.Runtime, "") {
			selected[item.ID] = true
		}
		beforePeers, afterPeers := []container.InspectResponse{}, []container.InspectResponse{}
		for _, item := range work.Runtime {
			if !selected[item.ID] {
				beforePeers = append(beforePeers, item)
			}
		}
		for _, item := range current {
			if item.Config != nil && !slices.Contains(work.Source.ActiveServices, item.Config.Labels["com.docker.compose.service"]) {
				afterPeers = append(afterPeers, item)
			}
		}
		if work.Source.ActiveServices != nil && (adoptionRuntimeFingerprint(beforePeers) != adoptionRuntimeFingerprint(afterPeers) || !sameRunningState(beforePeers, afterPeers)) {
			return store.ErrJobChanged
		}
		return nil
	}
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return err
	}
	if !unchangedImages(model, serviceRuntime(runtime, work.Service), targets) {
		return store.ErrJobChanged
	}
	expected := expectedServices(scopedFilePreview(work.Source, work.Service).model)[work.Service]
	if len(serviceRuntime(runtime, work.Service)) != expected.replicas {
		return store.ErrJobChanged
	}
	before, after := otherServiceRuntime(work.Runtime, work.Service), otherServiceRuntime(runtime, work.Service)
	if adoptionRuntimeFingerprint(before) != adoptionRuntimeFingerprint(after) || !sameRunningState(before, after) {
		return store.ErrJobChanged
	}
	old := serviceRuntime(work.Runtime, work.Service)
	if len(old) > 0 && !old[0].State.Running {
		for _, item := range serviceRuntime(runtime, work.Service) {
			if item.State == nil || item.State.Running {
				return store.ErrJobChanged
			}
		}
	}
	return nil
}

func selectedFileRuntime(source FileSnapshot, runtime []container.InspectResponse, service string) []container.InspectResponse {
	if service != "" {
		return serviceRuntime(runtime, service)
	}
	if source.ActiveServices == nil {
		return runtime
	}
	selected := []container.InspectResponse{}
	for _, item := range runtime {
		if item.Config != nil && slices.Contains(source.ActiveServices, item.Config.Labels["com.docker.compose.service"]) {
			selected = append(selected, item)
		}
	}
	return selected
}
