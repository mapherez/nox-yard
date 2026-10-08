package jobs

import (
	"context"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/client"
)

func Images(ctx context.Context, resources []string, cached bool) ([]store.ImageIdentity, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return nil, err
	}
	defer cli.Close()
	result := []store.ImageIdentity{}
	for _, resource := range resources {
		id, ok := strings.CutPrefix(resource, "container:")
		if !ok {
			continue
		}
		inspected, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		item := inspected.Container
		image := store.ImageIdentity{ContainerID: item.ID, ImageID: item.Image}
		if item.Config != nil {
			image.Service = item.Config.Labels["com.docker.compose.service"]
		}
		if item.State != nil {
			image.StartedAt = item.State.StartedAt
		}
		if cached && item.Config != nil {
			local, err := cli.ImageInspect(ctx, item.Config.Image)
			if err != nil {
				return nil, err
			}
			image.ImageID = local.ID
		}
		result = append(result, image)
	}
	return result, nil
}
