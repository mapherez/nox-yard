package recreate

import (
	"context"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"testing"
)

func TestBindSourceAliasesRequireIdentityProof(t *testing.T) {
	previous := []container.MountPoint{{Type: mount.TypeBind, Source: "/original", Destination: "/data", RW: true}}
	wanted := &container.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: "/original", Target: "/data"}}}
	original := container.InspectResponse{HostConfig: &container.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: "/alias", Target: "/data"}}}, Mounts: []container.MountPoint{{Type: mount.TypeBind, Source: "/alias", Destination: "/data", RW: true}}}
	for _, proof := range []bool{false, true} {
		calls := 0
		actual, expected := comparisonBindSources(context.Background(), original, previous, wanted, func(_ context.Context, a, b string) bool {
			calls++
			if a != "/alias" || b != "/original" {
				t.Fatal("unexpected identity comparison")
			}
			return proof
		})
		if equivalent(mountIdentities(actual), mountIdentities(container.InspectResponse{Mounts: previous})) != proof || equivalent(actual.HostConfig, expected) != proof {
			t.Fatal("source accepted without matching proof or proven alias rejected")
		}
		if calls != 1 {
			t.Fatal("identity proof should be reused for the same pair")
		}
		if original.Mounts[0].Source != "/alias" || original.HostConfig.Mounts[0].Source != "/alias" || wanted.Mounts[0].Source != "/original" {
			t.Fatal("comparison mutated recovery inputs")
		}
	}
}
func TestSourceProofDoesNotRelaxMountProperties(t *testing.T) {
	for _, changed := range []string{"readonly", "destination", "volume"} {
		t.Run(changed, func(t *testing.T) {
			old := []container.MountPoint{{Type: mount.TypeBind, Source: "/original", Destination: "/data", RW: true}}
			actual := container.InspectResponse{HostConfig: &container.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: "/alias", Target: "/data"}}}, Mounts: []container.MountPoint{{Type: mount.TypeBind, Source: "/alias", Destination: "/data", RW: true}}}
			wanted := &container.HostConfig{Mounts: []mount.Mount{{Type: mount.TypeBind, Source: "/original", Target: "/data"}}}
			switch changed {
			case "readonly":
				actual.Mounts[0].RW = false
				actual.HostConfig.Mounts[0].ReadOnly = true
			case "destination":
				actual.Mounts[0].Destination = "/other"
				actual.HostConfig.Mounts[0].Target = "/other"
			case "volume":
				actual.Mounts[0].Type = mount.TypeVolume
				actual.HostConfig.Mounts[0].Type = mount.TypeVolume
			}
			got, expected := comparisonBindSources(context.Background(), actual, old, wanted, func(context.Context, string, string) bool { return true })
			if equivalent(got.HostConfig, expected) || equivalent(mountIdentities(got), mountIdentities(container.InspectResponse{Mounts: old})) {
				t.Fatal("identity proof relaxed another mount property")
			}
		})
	}
}
