package managed

import (
	"encoding/json"
	"maps"
	"strings"

	"github.com/moby/moby/api/types/container"
)

// Compose versions serialize resolved byte sizes as either numbers or decimal
// strings. Unit conversion remains Compose's responsibility during validation.
type modelTmpfsSize int64

func (s *modelTmpfsSize) UnmarshalJSON(data []byte) error {
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	value, err := number.Int64()
	if err == nil {
		*s = modelTmpfsSize(value)
	}
	return err
}

type tmpfsMountSettings struct {
	ReadOnly bool
	Size     int64
	Mode     uint32
}

// Compose's service-level tmpfs entries become HostConfig.Tmpfs, whereas
// volumes of type tmpfs become HostConfig.Mounts. Inspect.Mounts does not
// reliably include either form. Compare creation settings, retaining option
// strings verbatim (including order-dependent repeated flags).
func matchesTmpfs(service modelService, host *container.HostConfig) bool {
	desired := map[string]string{}
	for _, entry := range service.Tmpfs {
		target, options, _ := strings.Cut(entry, ":")
		desired[target] = options
	}
	if !maps.Equal(desired, host.Tmpfs) {
		return false
	}
	desiredMounts := map[string]tmpfsMountSettings{}
	for _, volume := range service.Volumes {
		if volume.Type == "tmpfs" {
			desiredMounts[volume.Target] = tmpfsMountSettings{ReadOnly: volume.ReadOnly, Size: int64(volume.Tmpfs.Size), Mode: volume.Tmpfs.Mode}
		}
	}
	actualMounts := map[string]tmpfsMountSettings{}
	for _, mounted := range host.Mounts {
		if mounted.Type == "tmpfs" {
			settings := tmpfsMountSettings{ReadOnly: mounted.ReadOnly}
			if mounted.TmpfsOptions != nil {
				settings.Size = mounted.TmpfsOptions.SizeBytes
				settings.Mode = uint32(mounted.TmpfsOptions.Mode)
			}
			actualMounts[mounted.Target] = settings
		}
	}
	return maps.Equal(desiredMounts, actualMounts)
}
