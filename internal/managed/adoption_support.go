package managed

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
)

var ErrUnsupportedComparison = errors.New("Yard does not yet support comparing this Compose configuration")

// Adoption has a deliberately bounded comparison contract. Deployment can
// still use broader Compose definitions, but ownership must not silently omit
// runtime settings that this comparison cannot verify.
func assessAdoption(service modelService, item container.InspectResponse) error {
	labels := item.Config.Labels
	number, err := strconv.Atoi(labels["com.docker.compose.container-number"])
	if labels["com.docker.compose.config-hash"] == "" || labels["com.docker.compose.oneoff"] != "False" || err != nil || number < 1 {
		return fmt.Errorf("%w: existing Compose identity labels are incomplete; this service cannot be safely adopted", ErrInvalidSource)
	}
	allowed := map[string]bool{}
	for _, key := range strings.Fields("image command entrypoint environment labels user working_dir profiles scale depends_on ports volumes networks healthcheck restart stop_grace_period stop_signal hostname domainname init logging tmpfs") {
		allowed[key] = true
	}
	for _, key := range slices.Sorted(maps.Keys(service.raw)) {
		value := service.raw[key]
		if key == "deploy" {
			var deploy map[string]json.RawMessage
			if err := json.Unmarshal(value, &deploy); err != nil {
				return fmt.Errorf("%w: invalid deployment settings", ErrInvalidSource)
			}
			for field := range deploy {
				if field != "replicas" {
					return fmt.Errorf("%w: deploy.%s", ErrUnsupportedComparison, field)
				}
			}
			continue
		}
		if !allowed[key] {
			return fmt.Errorf("%w: %s", ErrUnsupportedComparison, key)
		}
		if key == "logging" {
			var logging map[string]json.RawMessage
			if err := json.Unmarshal(value, &logging); err != nil {
				return fmt.Errorf("%w: invalid logging settings", ErrInvalidSource)
			}
			for _, field := range slices.Sorted(maps.Keys(logging)) {
				if field != "driver" && field != "options" {
					return fmt.Errorf("%w: logging.%s", ErrUnsupportedComparison, field)
				}
			}
		}
		if key == "networks" {
			var networks map[string]map[string]json.RawMessage
			if err := json.Unmarshal(value, &networks); err != nil {
				return fmt.Errorf("%w: invalid network settings", ErrInvalidSource)
			}
			for _, options := range networks {
				for field := range options {
					if field != "aliases" {
						return fmt.Errorf("%w: network %s", ErrUnsupportedComparison, field)
					}
				}
			}
		}
		if key == "volumes" {
			var volumes []map[string]json.RawMessage
			if err := json.Unmarshal(value, &volumes); err != nil {
				return fmt.Errorf("%w: invalid mount settings", ErrInvalidSource)
			}
			for _, options := range volumes {
				for field := range options {
					if field != "type" && field != "source" && field != "target" && field != "read_only" && field != "bind" && field != "volume" && field != "tmpfs" {
						return fmt.Errorf("%w: mount %s", ErrUnsupportedComparison, field)
					}
				}
				if raw := options["tmpfs"]; raw != nil {
					var tmpfs map[string]json.RawMessage
					if json.Unmarshal(raw, &tmpfs) != nil {
						return fmt.Errorf("%w: invalid tmpfs settings", ErrInvalidSource)
					}
					for _, field := range slices.Sorted(maps.Keys(tmpfs)) {
						if field != "size" && field != "mode" {
							return fmt.Errorf("%w: tmpfs.%s", ErrUnsupportedComparison, field)
						}
					}
				}
				if raw := options["volume"]; raw != nil {
					var volume map[string]json.RawMessage
					if json.Unmarshal(raw, &volume) != nil {
						return fmt.Errorf("%w: invalid volume settings", ErrInvalidSource)
					}
					for field, value := range volume {
						if field == "nocopy" && string(value) == "false" {
							continue
						}
						if field == "subpath" && string(value) == `""` {
							continue
						}
						return fmt.Errorf("%w: volume %s", ErrUnsupportedComparison, field)
					}
				}
				if raw := options["bind"]; raw != nil {
					var bind map[string]json.RawMessage
					if err := json.Unmarshal(raw, &bind); err != nil {
						return fmt.Errorf("%w: invalid bind settings", ErrInvalidSource)
					}
					for field := range bind {
						if field != "propagation" && field != "create_host_path" {
							return fmt.Errorf("%w: bind %s", ErrUnsupportedComparison, field)
						}
					}
				}
			}
		}
	}
	// Docker fills many default fields. Reject observable non-default settings
	// whose meaning cannot be recovered from the supported source fields.
	host := reflect.ValueOf(*item.HostConfig)
	for _, field := range strings.Fields("Privileged ReadonlyRootfs CapAdd CapDrop SecurityOpt Devices DeviceRequests DeviceCgroupRules ExtraHosts DNS DNSOptions DNSSearch GroupAdd VolumesFrom Links Ulimits Memory MemorySwap MemoryReservation NanoCPUs CPUPeriod CPUQuota CPURealtimePeriod CPURealtimeRuntime CPUShares CpusetCpus CpusetMems PidsLimit BlkioWeight BlkioWeightDevice BlkioDeviceReadBps BlkioDeviceWriteBps BlkioDeviceReadIOps BlkioDeviceWriteIOps ShmSize Sysctls AutoRemove ContainerIDFile Annotations OomScoreAdj PidMode PublishAllPorts StorageOpt UTSMode UsernsMode CgroupParent Umask") {
		value := host.FieldByName(field)
		if field == "ShmSize" && value.IsValid() && value.Int() == 64*1024*1024 {
			continue
		}
		if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Map) && value.Len() == 0 {
			continue
		}
		if value.IsValid() && !value.IsZero() {
			return fmt.Errorf("%w: existing %s settings", ErrUnsupportedComparison, field)
		}
	}
	for _, endpoint := range item.NetworkSettings.Networks {
		if endpoint != nil && (endpoint.IPAMConfig != nil || len(endpoint.DriverOpts) > 0 || len(endpoint.Links) > 0) {
			return fmt.Errorf("%w: existing static network options", ErrUnsupportedComparison)
		}
	}
	for _, mount := range item.HostConfig.Mounts {
		if mount.Type == "tmpfs" && mount.TmpfsOptions != nil && len(mount.TmpfsOptions.Options) > 0 {
			return fmt.Errorf("%w: existing advanced tmpfs options", ErrUnsupportedComparison)
		}
		if mount.VolumeOptions != nil && (mount.VolumeOptions.NoCopy || mount.VolumeOptions.Subpath != "" || mount.VolumeOptions.DriverConfig != nil) {
			return fmt.Errorf("%w: existing volume options", ErrUnsupportedComparison)
		}
	}
	for _, mount := range item.Mounts {
		if mount.Type == "tmpfs" {
			// Configuration is assessed through HostConfig, not runtime mount flags.
			continue
		}
		for _, option := range strings.Split(mount.Mode, ",") {
			if option != "" && option != "rw" && option != "ro" {
				return fmt.Errorf("%w: existing mount mode", ErrUnsupportedComparison)
			}
		}
	}
	if item.HostConfig.NetworkMode.IsHost() || item.HostConfig.NetworkMode.IsContainer() || item.HostConfig.NetworkMode.IsNone() {
		return fmt.Errorf("%w: nonstandard attached networks", ErrUnsupportedComparison)
	}
	if item.Config.Tty || item.Config.OpenStdin || item.Config.NetworkDisabled {
		return fmt.Errorf("%w: existing terminal or disabled-network settings", ErrUnsupportedComparison)
	}
	return nil
}

func desiredHealthcheck(base *container.HealthConfig, source *modelHealthcheck) (*container.HealthConfig, error) {
	if source == nil {
		return base, nil
	}
	result := container.HealthConfig{}
	if base != nil {
		result = *base
	}
	if source.Disable {
		result.Test = []string{"NONE"}
		return &result, nil
	}
	if source.Test != nil {
		result.Test = source.Test
	}
	for _, field := range []struct {
		text        string
		destination *time.Duration
	}{
		{source.Interval, &result.Interval}, {source.Timeout, &result.Timeout},
		{source.StartPeriod, &result.StartPeriod}, {source.StartInterval, &result.StartInterval},
	} {
		if field.text != "" {
			value, err := time.ParseDuration(field.text)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid healthcheck duration", ErrInvalidSource)
			}
			*field.destination = value
		}
	}
	if source.Retries != nil {
		result.Retries = *source.Retries
	}
	return &result, nil
}

func desiredRestart(source string) (container.RestartPolicy, error) {
	if source == "" {
		source = "no"
	}
	name, retries, _ := strings.Cut(source, ":")
	count := 0
	if retries != "" {
		value, err := strconv.Atoi(retries)
		if err != nil {
			return container.RestartPolicy{}, fmt.Errorf("%w: invalid restart policy", ErrInvalidSource)
		}
		count = value
	}
	return container.RestartPolicy{Name: container.RestartPolicyMode(name), MaximumRetryCount: count}, nil
}
