package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func adoptionDirectory(items []container.InspectResponse, supplied string) (string, error) {
	directory := ""
	for _, item := range items {
		if item.Config == nil {
			return "", fmt.Errorf("%w: existing container configuration is unavailable", ErrInvalidSource)
		}
		value := item.Config.Labels["com.docker.compose.project.working_dir"]
		if value == "" {
			continue
		}
		value, err := NormalizeProjectsBase(value)
		if err != nil || value == "/" {
			return "", fmt.Errorf("%w: existing Compose directory is invalid", ErrInvalidSource)
		}
		if directory != "" && directory != value {
			return "", fmt.Errorf("%w: existing containers disagree on the Compose directory", ErrInvalidSource)
		}
		directory = value
	}
	if supplied != "" {
		value, err := NormalizeProjectsBase(supplied)
		if err != nil || value == "/" {
			return "", fmt.Errorf("%w: provide an absolute host project directory", ErrInvalidSource)
		}
		if directory != "" && directory != value {
			return "", fmt.Errorf("%w: supplied directory differs from the existing Compose directory", ErrInvalidSource)
		}
		directory = value
	}
	if directory == "" {
		return "", fmt.Errorf("%w: provide the original host project directory before adoption", ErrInvalidSource)
	}
	return directory, nil
}

// Fingerprint configuration, not transient health/metrics or Docker credentials.
func adoptionRuntimeFingerprint(items []container.InspectResponse) string {
	type state struct {
		ID         string
		Image      string
		Config     *container.Config
		HostConfig *container.HostConfig
		Mounts     []container.MountPoint
		Networks   any
	}
	values := make([]state, 0, len(items))
	for _, item := range items {
		value := state{ID: item.ID, Image: item.Image, Config: item.Config, HostConfig: item.HostConfig, Mounts: item.Mounts}
		if item.NetworkSettings != nil {
			value.Networks = item.NetworkSettings.Networks
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	encoded, _ := json.Marshal(values)
	return fingerprint(encoded)
}

type imageDefaults struct {
	ID     string
	Config container.Config
}

func readImageDefaults(ctx context.Context, reference string) (imageDefaults, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return imageDefaults{}, err
	}
	defer cli.Close()
	value, err := cli.ImageInspect(ctx, reference)
	if err != nil || value.Config == nil {
		return imageDefaults{}, fmt.Errorf("image defaults unavailable")
	}
	encoded, err := json.Marshal(value.Config)
	if err != nil {
		return imageDefaults{}, err
	}
	result := imageDefaults{ID: value.ID}
	err = json.Unmarshal(encoded, &result.Config)
	return result, err
}

func compareAdoption(name, directory string, model composeModel, items []container.InspectResponse, defaults map[string]imageDefaults) ([]string, error) {
	changes := map[string]bool{}
	seen := map[string]bool{}
	counts := map[string]int{}
	for _, item := range items {
		if item.Config == nil || item.HostConfig == nil || item.NetworkSettings == nil {
			return nil, fmt.Errorf("%w: existing runtime configuration is incomplete", ErrInvalidSource)
		}
		serviceName := item.Config.Labels["com.docker.compose.service"]
		if serviceName == "" {
			return nil, fmt.Errorf("%w: existing Compose service labels are incomplete", ErrInvalidSource)
		}
		service, found := model.Services[serviceName]
		if !found {
			changes["Existing service absent from source: "+serviceName] = true
			continue
		}
		seen[serviceName] = true
		counts[serviceName]++
		if err := assessAdoption(service, item); err != nil {
			return nil, err
		}
		mark := func(field string) { changes[field+" differs for "+serviceName] = true }
		if item.Config.Image != service.Image {
			mark("Image")
		}
		base, ok := defaults[service.Image]
		if !ok {
			return nil, fmt.Errorf("%w: image defaults must be available before adoption", ErrInvalidSource)
		}
		if item.Image != "" && base.ID != "" && item.Image != base.ID {
			mark("Image ID")
		}
		command, entrypoint, user, working := base.Config.Cmd, base.Config.Entrypoint, base.Config.User, base.Config.WorkingDir
		if service.Command != nil {
			command = *service.Command
		}
		if service.Entrypoint != nil {
			entrypoint = *service.Entrypoint
			if service.Command == nil {
				command = nil
			}
		}
		if !slices.Equal(command, item.Config.Cmd) {
			mark("Command")
		}
		if !slices.Equal(entrypoint, item.Config.Entrypoint) {
			mark("Entrypoint")
		}
		if service.User != "" {
			user = service.User
		}
		if user != item.Config.User {
			mark("User")
		}
		if service.WorkingDir != "" {
			working = service.WorkingDir
		}
		if working != item.Config.WorkingDir {
			mark("Working directory")
		}
		healthcheck, err := desiredHealthcheck(base.Config.Healthcheck, service.Healthcheck)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(healthcheck, item.Config.Healthcheck) {
			mark("Healthcheck")
		}
		restart, err := desiredRestart(service.Restart)
		if err != nil {
			return nil, err
		}
		actualRestart := item.HostConfig.RestartPolicy
		if actualRestart.Name == "" {
			actualRestart.Name = "no"
		}
		if restart != actualRestart {
			mark("Restart policy")
		}
		hostname := service.Hostname
		if hostname == "" {
			hostname = base.Config.Hostname
		}
		defaultHostname := service.Hostname == "" && item.Config.Hostname == item.ID[:min(12, len(item.ID))]
		if hostname != item.Config.Hostname && !defaultHostname {
			mark("Hostname")
		}
		domainname := service.Domainname
		if domainname == "" {
			domainname = base.Config.Domainname
		}
		if domainname != item.Config.Domainname {
			mark("Domain name")
		}
		stopSignal := base.Config.StopSignal
		if service.StopSignal != "" {
			stopSignal = service.StopSignal
		}
		if stopSignal != item.Config.StopSignal {
			mark("Stop signal")
		}
		stopTimeout := 10
		if service.StopGracePeriod != "" {
			duration, err := time.ParseDuration(service.StopGracePeriod)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid stop duration", ErrInvalidSource)
			}
			stopTimeout = int(duration.Seconds())
		}
		if item.Config.StopTimeout != nil && *item.Config.StopTimeout != stopTimeout {
			mark("Stop timeout")
		}
		environment := map[string]string{}
		for _, entry := range item.Config.Env {
			key, value, _ := strings.Cut(entry, "=")
			environment[key] = value
		}
		desiredEnvironment := map[string]string{}
		for _, entry := range base.Config.Env {
			key, value, _ := strings.Cut(entry, "=")
			desiredEnvironment[key] = value
		}
		for key, value := range service.Environment {
			if value == nil {
				return nil, fmt.Errorf("%w: provide all environment values before adoption", ErrInvalidSource)
			}
			desiredEnvironment[key] = *value
		}
		if !reflect.DeepEqual(environment, desiredEnvironment) {
			mark("Environment")
		}
		labels, desiredLabels := map[string]string{}, map[string]string{}
		for key, value := range item.Config.Labels {
			if !strings.HasPrefix(key, "com.docker.compose.") {
				labels[key] = value
			}
		}
		for key, value := range base.Config.Labels {
			desiredLabels[key] = value
		}
		for key, value := range service.Labels {
			desiredLabels[key] = value
		}
		if !reflect.DeepEqual(labels, desiredLabels) {
			mark("Labels")
		}
		ports := map[string]bool{}
		for port, bindings := range item.HostConfig.PortBindings {
			for _, binding := range bindings {
				ip := ""
				if binding.HostIP.IsValid() {
					ip = binding.HostIP.String()
				}
				if ip == "0.0.0.0" || ip == "::" {
					ip = ""
				}
				ports[ip+"|"+binding.HostPort+"|"+port.String()] = true
			}
		}
		desiredPorts := map[string]bool{}
		for _, port := range service.Ports {
			ip := port.HostIP
			if ip == "0.0.0.0" || ip == "::" {
				ip = ""
			}
			desiredPorts[fmt.Sprintf("%s|%s|%d/%s", ip, port.Published, port.Target, port.Protocol)] = true
		}
		if !reflect.DeepEqual(ports, desiredPorts) {
			mark("Published ports")
		}
		mounts := map[string]string{}
		for _, mount := range item.Mounts {
			source := mount.Source
			if mount.Type == "volume" {
				source = mount.Name
			}
			propagation := string(mount.Propagation)
			if propagation == "rprivate" {
				propagation = ""
			}
			mounts[mount.Destination] = fmt.Sprintf("%s|%s|%t|%s", mount.Type, source, !mount.RW, propagation)
		}
		desiredMounts := map[string]string{}
		for _, mount := range service.Volumes {
			source := mount.Source
			if mount.Type == "bind" && !path.IsAbs(source) {
				if directory == "" {
					return nil, fmt.Errorf("%w: a host directory is needed to compare relative binds", ErrInvalidSource)
				}
				source = path.Join(directory, source)
			}
			if mount.Type == "volume" && source != "" {
				resource := model.Volumes[source]
				if resource.Name != "" {
					source = resource.Name
				} else {
					source = name + "_" + source
				}
			}
			if mount.Type == "volume" && mount.Source == "" {
				// Anonymous volume identity cannot be promised during adoption.
				return nil, fmt.Errorf("%w: anonymous volumes cannot be safely adopted", ErrInvalidSource)
			}
			propagation := mount.Bind.Propagation
			if propagation == "rprivate" {
				propagation = ""
			}
			desiredMounts[mount.Target] = fmt.Sprintf("%s|%s|%t|%s", mount.Type, source, mount.ReadOnly, propagation)
		}
		if !reflect.DeepEqual(mounts, desiredMounts) {
			mark("Mounts")
		}
		networks := map[string]bool{}
		for network := range item.NetworkSettings.Networks {
			networks[network] = true
		}
		desiredNetworks := map[string]bool{}
		for network, options := range service.Networks {
			actual := model.Networks[network].Name
			if actual == "" {
				actual = name + "_" + network
			}
			desiredNetworks[actual] = true
			endpoint := item.NetworkSettings.Networks[actual]
			for _, alias := range options.Aliases {
				if endpoint == nil || !contains(endpoint.Aliases, alias) {
					mark("Network aliases")
				}
			}
		}
		if !reflect.DeepEqual(networks, desiredNetworks) {
			mark("Networks")
		}
	}
	for service, expected := range expectedServices(model) {
		if !seen[service] {
			changes["New service: "+service] = true
		} else if counts[service] != expected.replicas {
			changes["Replica count differs for "+service] = true
		}
	}
	result := make([]string, 0, len(changes))
	for change := range changes {
		result = append(result, change)
	}
	sort.Strings(result)
	return result, nil
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func verifyAdoptionBinds(directory string, model composeModel, items []container.InspectResponse) error {
	for _, item := range items {
		if item.Config == nil {
			return fmt.Errorf("%w: existing mount configuration is unavailable", ErrInvalidSource)
		}
		service := model.Services[item.Config.Labels["com.docker.compose.service"]]
		for _, desired := range service.Volumes {
			if desired.Type != "bind" || path.IsAbs(desired.Source) {
				continue
			}
			matched := false
			for _, actual := range item.Mounts {
				if actual.Type == "bind" && actual.Destination == desired.Target && path.Clean(actual.Source) == path.Join(directory, desired.Source) {
					matched = true
				}
			}
			if !matched {
				return fmt.Errorf("%w: original directory and source must match existing relative bind mounts before adoption", ErrInvalidSource)
			}
		}
	}
	return nil
}
