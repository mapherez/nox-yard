package managed

import (
	"reflect"
	"slices"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func TestRuntimeFingerprintIgnoresMountInspectionOrder(t *testing.T) {
	_, original, _ := adoptionSample(t)
	before := slices.Clone(original.Mounts)
	reordered := original
	reordered.Mounts = slices.Clone(original.Mounts)
	slices.Reverse(reordered.Mounts)
	reorderedBefore := slices.Clone(reordered.Mounts)
	if adoptionRuntimeFingerprint([]container.InspectResponse{original}) != adoptionRuntimeFingerprint([]container.InspectResponse{reordered}) {
		t.Fatal("identical Docker mounts in another inspection order invalidate the reviewed runtime")
	}
	if !reflect.DeepEqual(original.Mounts, before) || !reflect.DeepEqual(reordered.Mounts, reorderedBefore) {
		t.Fatal("fingerprinting mutated the runtime inspection")
	}
}

func TestRuntimeFingerprintStillDetectsMountChanges(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*container.MountPoint)
	}{
		{"source", func(m *container.MountPoint) { m.Source = "/another/source" }},
		{"destination", func(m *container.MountPoint) { m.Destination = "/another/destination" }},
		{"type", func(m *container.MountPoint) { m.Type = mount.TypeVolume }},
		{"access", func(m *container.MountPoint) { m.RW = !m.RW }},
		{"name", func(m *container.MountPoint) { m.Name = "another-volume" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			_, original, _ := adoptionSample(t)
			changed := original
			changed.Mounts = slices.Clone(original.Mounts)
			change.apply(&changed.Mounts[0])
			if adoptionRuntimeFingerprint([]container.InspectResponse{original}) == adoptionRuntimeFingerprint([]container.InspectResponse{changed}) {
				t.Fatal("a real mount change no longer invalidates the reviewed runtime")
			}
		})
	}
}
