package recreate

import (
	"context"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

// Align only proven aliases for comparison. Creation/recovery inputs are not
// rewritten. Every other mount property retains its exact existing checks.
func comparisonBindSources(ctx context.Context, actual container.InspectResponse, previous []container.MountPoint, wanted *container.HostConfig, same func(context.Context, string, string) bool) (container.InspectResponse, *container.HostConfig) {
	actual = clone(actual)
	wanted = clone(wanted)
	if actual.HostConfig == nil || wanted == nil {
		return actual, wanted
	}
	sources := map[string]string{}
	for _, entry := range previous {
		if entry.Type == mount.TypeBind {
			sources[entry.Destination] = entry.Source
		}
	}
	proven := map[string]bool{}
	align := func(target string, source *string) {
		expected, found := sources[target]
		if !found || *source == expected {
			return
		}
		key := *source + "\x00" + expected
		match, checked := proven[key]
		if !checked {
			match = same(ctx, *source, expected)
			proven[key] = match
		}
		if match {
			*source = expected
		}
	}
	for i := range actual.Mounts {
		if actual.Mounts[i].Type == mount.TypeBind {
			align(actual.Mounts[i].Destination, &actual.Mounts[i].Source)
		}
	}
	for _, host := range []*container.HostConfig{actual.HostConfig, wanted} {
		for i := range host.Mounts {
			if host.Mounts[i].Type == mount.TypeBind {
				align(host.Mounts[i].Target, &host.Mounts[i].Source)
			}
		}
	}
	return actual, wanted
}
