package managed

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
)

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
	for _, key := range strings.Fields("image command entrypoint environment labels user working_dir profiles scale depends_on ports volumes networks healthcheck restart stop_grace_period stop_signal hostname domainname") {
		allowed[key] = true
	}
	for key, value := range service.raw {
		if key == "deploy" {
			var deploy map[string]json.RawMessage
			if err := json.Unmarshal(value, &deploy); err != nil {
				return fmt.Errorf("%w: invalid deployment settings", ErrInvalidSource)
			}
			for field := range deploy {
				if field != "replicas" {
					return fmt.Errorf("%w: adoption cannot compare deploy.%s", ErrInvalidSource, field)
				}
			}
			continue
		}
		if !allowed[key] {
			return fmt.Errorf("%w: adoption cannot compare %s", ErrInvalidSource, key)
		}
		if key == "networks" {
			var networks map[string]map[string]json.RawMessage
			if err := json.Unmarshal(value, &networks); err != nil {
				return fmt.Errorf("%w: adoption cannot compare network settings", ErrInvalidSource)
			}
			for _, options := range networks {
				for field := range options {
					if field != "aliases" {
						return fmt.Errorf("%w: adoption cannot compare network %s", ErrInvalidSource, field)
					}
				}
			}
		}
		if key == "volumes" {
			var volumes []map[string]json.RawMessage
			if err := json.Unmarshal(value, &volumes); err != nil {
				return fmt.Errorf("%w: adoption cannot compare mount settings", ErrInvalidSource)
			}
			for _, options := range volumes {
				for field := range options {
					if field != "type" && field != "source" && field != "target" && field != "read_only" && field != "bind" {
						return fmt.Errorf("%w: adoption cannot compare mount %s", ErrInvalidSource, field)
					}
				}
				if raw := options["bind"]; raw != nil {
					var bind map[string]json.RawMessage
					if err := json.Unmarshal(raw, &bind); err != nil {
						return fmt.Errorf("%w: invalid bind settings", ErrInvalidSource)
					}
					for field := range bind {
						if field != "propagation" && field != "create_host_path" {
							return fmt.Errorf("%w: adoption cannot compare bind %s", ErrInvalidSource, field)
						}
					}
				}
			}
		}
	}
	// Docker fills many default fields. Reject observable non-default settings
	// whose meaning cannot be recovered from the supported source fields.
	host := reflect.ValueOf(*item.HostConfig)
	for _, field := range strings.Fields("Privileged ReadonlyRootfs CapAdd CapDrop SecurityOpt Devices DeviceRequests DeviceCgroupRules ExtraHosts DNS DNSOptions DNSSearch GroupAdd VolumesFrom Links Tmpfs Init Ulimits Memory MemorySwap MemoryReservation NanoCPUs CPUPeriod CPUQuota CPURealtimePeriod CPURealtimeRuntime CPUShares CpusetCpus CpusetMems PidsLimit BlkioWeight BlkioWeightDevice BlkioDeviceReadBps BlkioDeviceWriteBps BlkioDeviceReadIOps BlkioDeviceWriteIOps ShmSize Sysctls AutoRemove ContainerIDFile Annotations OomScoreAdj PidMode PublishAllPorts StorageOpt UTSMode UsernsMode CgroupParent Umask") {
		value := host.FieldByName(field)
		if field == "ShmSize" && value.IsValid() && value.Int() == 64*1024*1024 {
			continue
		}
		if value.IsValid() && (value.Kind() == reflect.Slice || value.Kind() == reflect.Map) && value.Len() == 0 {
			continue
		}
		if value.IsValid() && !value.IsZero() {
			return fmt.Errorf("%w: adoption cannot compare existing %s settings", ErrInvalidSource, field)
		}
	}
	if len(item.HostConfig.LogConfig.Config) > 0 {
		return fmt.Errorf("%w: adoption cannot compare existing logging options", ErrInvalidSource)
	}
	for _, endpoint := range item.NetworkSettings.Networks {
		if endpoint != nil && (endpoint.IPAMConfig != nil || len(endpoint.DriverOpts) > 0 || len(endpoint.Links) > 0) {
			return fmt.Errorf("%w: adoption cannot compare existing static network options", ErrInvalidSource)
		}
	}
	for _, mount := range item.HostConfig.Mounts {
		if mount.VolumeOptions != nil && (mount.VolumeOptions.NoCopy || mount.VolumeOptions.Subpath != "" || mount.VolumeOptions.DriverConfig != nil) {
			return fmt.Errorf("%w: adoption cannot compare existing volume options", ErrInvalidSource)
		}
	}
	for _, mount := range item.Mounts {
		for _, option := range strings.Split(mount.Mode, ",") {
			if option != "" && option != "rw" && option != "ro" {
				return fmt.Errorf("%w: adoption cannot compare existing mount mode", ErrInvalidSource)
			}
		}
	}
	if item.HostConfig.NetworkMode.IsHost() || item.HostConfig.NetworkMode.IsContainer() || item.HostConfig.NetworkMode.IsNone() {
		return fmt.Errorf("%w: adoption needs standard attached networks", ErrInvalidSource)
	}
	if item.Config.Tty || item.Config.OpenStdin || item.Config.NetworkDisabled {
		return fmt.Errorf("%w: adoption cannot compare existing terminal or disabled-network settings", ErrInvalidSource)
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
