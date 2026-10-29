package recreate

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// AssessRuntimeRecovery checks the old settings before a file-backed operation
// mutates Docker. Broader Compose input support must not promise a rollback
// which Engine reconstruction cannot preserve.
func AssessRuntimeRecovery(previous []container.InspectResponse) error {
	for _, item := range previous {
		if err := assess(item); err != nil {
			return fmt.Errorf("previous runtime cannot be safely restored: %w", err)
		}
	}
	return nil
}

// RestoreRuntime is used by file-backed Compose jobs after a failed up. It
// restores exact Engine inspections rather than rereading edited host files.
// No data volumes are removed. It is never called to apply normal operations.
func RestoreRuntime(ctx context.Context, name string, previous []container.InspectResponse) error {
	return RestoreServiceRuntime(ctx, name, "", previous)
}

func RestoreServiceRuntime(ctx context.Context, name, service string, previous []container.InspectResponse) error {
	return restoreRuntime(ctx, name, service, previous, nil, "")
}
func RestoreOperationRuntime(ctx context.Context, name, service string, previous []container.InspectResponse, active []string, job string) error {
	return restoreRuntime(ctx, name, service, previous, active, job)
}
func restoreRuntime(ctx context.Context, name, service string, previous []container.InspectResponse, active []string, job string) error {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	wanted := map[string]bool{}
	for _, item := range previous {
		wanted[item.ID] = true
	}
	listed, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", "com.docker.compose.project="+name)})
	if err != nil {
		return err
	}
	for _, item := range listed.Items {
		if (service != "" && item.Labels["com.docker.compose.service"] != service) || (service == "" && active != nil && !slices.Contains(active, item.Labels["com.docker.compose.service"])) {
			continue
		}
		if wanted[item.ID] {
			continue
		}
		if job != "" && item.Labels["nox-yard.operation"] != job {
			return fmt.Errorf("unrecognized container appeared during recovery; manual review is required")
		}
		_, _ = cli.ContainerStop(ctx, item.ID, client.ContainerStopOptions{})
		if _, err := cli.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{}); err != nil {
			return err
		}
	}
	entries := []*entry{}
	dependencies := []container.InspectResponse{}
	for _, old := range previous {
		if old.Config == nil || old.HostConfig == nil || old.State == nil {
			return fmt.Errorf("previous container inspection is incomplete")
		}
		if (service != "" && old.Config.Labels["com.docker.compose.service"] != service) || (service == "" && active != nil && !slices.Contains(active, old.Config.Labels["com.docker.compose.service"])) {
			dependencies = append(dependencies, old)
			continue
		}
		image, err := cli.ImageInspect(ctx, old.Image)
		if err != nil {
			return fmt.Errorf("previous image is unavailable")
		}
		platform := image.Os + "/" + image.Architecture
		if image.Variant != "" {
			platform += "/" + image.Variant
		}
		entries = append(entries, &entry{Old: old, Reference: imageidentity.Reference(old.Config.Image, old.Config.Labels), TargetImage: old.Image, Platform: platform})
	}
	order, err := scopedDependencyOrder(entries, dependencies)
	if err != nil {
		return err
	}
	for _, i := range order {
		target := entries[i]
		inspection, err := cli.ContainerInspect(ctx, target.Old.ID, client.ContainerInspectOptions{})
		id := target.Old.ID
		if err != nil {
			options := creation(target, "")
			created, err := cli.ContainerCreate(ctx, options)
			if err != nil {
				return fmt.Errorf("previous container could not be recreated")
			}
			id = created.ID
		} else if strings.TrimPrefix(inspection.Container.Name, "/") != strings.TrimPrefix(target.Old.Name, "/") {
			return fmt.Errorf("previous container identity changed")
		}
		if target.Old.State.Running || target.OneShot {
			if _, err := cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
				return fmt.Errorf("previous container could not be started")
			}
		} else {
			if _, err := cli.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
				return fmt.Errorf("previous stopped state could not be restored")
			}
		}
		target.NewID = id
		manager := &Manager{client: cli}
		if err := manager.ready(ctx, target, id); err != nil {
			return fmt.Errorf("previous container readiness could not be verified")
		}
		observed, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		actual := observed.Container
		wanted := creation(target, "")
		if id == target.Old.ID {
			wanted.Config = normalizeConfig(target.Old)
			wanted.HostConfig = target.Old.HostConfig
		}
		if actual.Image != target.Old.Image || !equivalent(normalizeConfig(actual), wanted.Config) || !equivalent(normalizeHost(actual.HostConfig), normalizeHost(wanted.HostConfig)) || !equivalent(mountIdentities(actual), mountIdentities(target.Old)) {
			return fmt.Errorf("previous container settings could not be verified")
		}
	}
	return nil
}
