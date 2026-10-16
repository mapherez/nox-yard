package recreate

import (
	"context"
	"fmt"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// CheckServiceIsolation protects incoming references from containers outside
// the Compose project as well. DNS/network membership and shared named volumes
// are fine; explicit namespace, legacy link and volumes-from references are not.
func CheckServiceIsolation(ctx context.Context, selected []container.InspectResponse) error {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	listed, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	for _, item := range listed.Items {
		target := false
		for _, old := range selected {
			if item.ID == old.ID {
				target = true
				break
			}
		}
		if target {
			continue
		}
		inspected, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := checkIncomingReferences(inspected.Container.HostConfig, selected); err != nil {
			return err
		}
	}
	return nil
}

func checkIncomingReferences(host *container.HostConfig, selected []container.InspectResponse) error {
	if host == nil {
		return fmt.Errorf("%w: incoming dependencies cannot be assessed", ErrUnsupported)
	}
	refs := append([]string{string(host.NetworkMode), string(host.IpcMode), string(host.PidMode)}, host.VolumesFrom...)
	refs = append(refs, host.Links...)
	for _, ref := range refs {
		ref = strings.TrimPrefix(ref, "container:")
		ref, _, _ = strings.Cut(ref, ":")
		ref = strings.TrimPrefix(ref, "/")
		for _, target := range selected {
			if ref == target.ID || ref == strings.TrimPrefix(target.Name, "/") || (len(ref) >= 12 && strings.HasPrefix(target.ID, ref)) {
				return fmt.Errorf("%w: another container depends on the selected service's namespace, link or volumes-from", ErrUnsupported)
			}
		}
	}
	return nil
}
