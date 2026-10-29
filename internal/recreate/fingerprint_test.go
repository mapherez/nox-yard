package recreate

import (
	"reflect"
	"slices"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func fingerprintFixture(item container.InspectResponse, dependency bool) *journal {
	state := &journal{Request: Request{ID: "compose:test", Operation: "update"}}
	if dependency {
		state.Entries = []*entry{{Old: assessed("b", "selected")}}
		state.Dependencies = []container.InspectResponse{item}
	} else {
		state.Entries = []*entry{{Old: item}}
	}
	return state
}

func fingerprintMounts() []container.MountPoint {
	return []container.MountPoint{
		{Type: mount.TypeBind, Source: "/host/bind", Destination: "/bind", Mode: "rw", RW: true, Propagation: mount.PropagationRPrivate},
		{Type: mount.TypeVolume, Name: "fixture-data", Source: "/var/lib/docker/volumes/fixture-data/_data", Destination: "/data", Driver: "local", RW: true},
		{Type: mount.TypeVolume, Name: "anonymous-fixture", Source: "/var/lib/docker/volumes/anonymous-fixture/_data", Destination: "/anonymous", Driver: "local", RW: true},
		{Type: mount.TypeTmpfs, Destination: "/tmp", RW: true},
	}
}

func TestFingerprintIgnoresMountInspectionOrder(t *testing.T) {
	for _, dependency := range []bool{false, true} {
		original := assessed("a", "web")
		original.Mounts = fingerprintMounts()
		before := slices.Clone(original.Mounts)
		want := fingerprint(fingerprintFixture(original, dependency))
		// Docker inspections may enumerate the same mounts in any order. Check
		// every permutation, for targets and for unselected dependencies.
		var permute func(int, []container.MountPoint)
		permute = func(index int, mounts []container.MountPoint) {
			if index == len(mounts) {
				observed := original
				observed.Mounts = slices.Clone(mounts)
				untouched := slices.Clone(observed.Mounts)
				if got := fingerprint(fingerprintFixture(observed, dependency)); got != want {
					t.Fatalf("mount order changed fingerprint (dependency=%t): %+v", dependency, mounts)
				}
				if !reflect.DeepEqual(observed.Mounts, untouched) {
					t.Fatal("fingerprinting reordered the original inspection")
				}
				return
			}
			for i := index; i < len(mounts); i++ {
				mounts[index], mounts[i] = mounts[i], mounts[index]
				permute(index+1, mounts)
				mounts[index], mounts[i] = mounts[i], mounts[index]
			}
		}
		permute(0, slices.Clone(original.Mounts))
		if !reflect.DeepEqual(original.Mounts, before) {
			t.Fatal("fingerprinting mutated its original snapshot")
		}
	}
}

func TestFingerprintStillDetectsEveryMountField(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*container.MountPoint)
	}{
		{"source", func(m *container.MountPoint) { m.Source = "/different" }},
		{"destination", func(m *container.MountPoint) { m.Destination = "/different" }},
		{"name", func(m *container.MountPoint) { m.Name = "different-volume" }},
		{"type", func(m *container.MountPoint) { m.Type = mount.TypeVolume }},
		{"driver", func(m *container.MountPoint) { m.Driver = "different-driver" }},
		{"mode", func(m *container.MountPoint) { m.Mode = "ro" }},
		{"access", func(m *container.MountPoint) { m.RW = false }},
		{"propagation", func(m *container.MountPoint) { m.Propagation = mount.PropagationRShared }},
	} {
		t.Run(change.name, func(t *testing.T) {
			for _, dependency := range []bool{false, true} {
				original := assessed("a", "web")
				original.Mounts = fingerprintMounts()
				changed := original
				changed.Mounts = slices.Clone(original.Mounts)
				change.apply(&changed.Mounts[0])
				if fingerprint(fingerprintFixture(original, dependency)) == fingerprint(fingerprintFixture(changed, dependency)) {
					t.Fatalf("real mount change ignored (dependency=%t)", dependency)
				}
			}
		})
	}
}
