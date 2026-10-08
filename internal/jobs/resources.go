package jobs

import (
	"context"
	"strings"

	"github.com/moby/moby/client"
)

func targetResources(ctx context.Context, target string) ([]string, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return nil, err
	}
	defer cli.Close()
	resources := []string{target}
	if id, ok := strings.CutPrefix(target, "container:"); ok {
		inspected, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return nil, err
		}
		if config := inspected.Container.Config; config != nil {
			if name := config.Labels["com.docker.compose.project"]; name != "" {
				resources = append(resources, "compose:"+name)
			}
			if config.Labels["com.docker.compose.service"] == "nox-yard" {
				resources = append(resources, "yard:self")
			}
		}
	} else if name, ok := strings.CutPrefix(target, "compose:"); ok {
		listed, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{"label": {"com.docker.compose.project=" + name: true}}})
		if err != nil {
			return nil, err
		}
		for _, item := range listed.Items {
			resources = append(resources, "container:"+item.ID)
			if item.Labels["com.docker.compose.service"] == "nox-yard" {
				resources = append(resources, "yard:self")
			}
		}
	}
	return resources, nil
}
