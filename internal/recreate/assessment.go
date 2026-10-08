// Package recreate owns bounded Engine replacement. Runtime snapshots and
// execution journals are private; public previews never contain secret values.
package recreate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

var ErrUnsupported = errors.New("configuration cannot be safely recreated")
var ErrInvalid = errors.New("review and confirm a current update/recreate preview first")

const TargetLabel = "nox-yard.target"

type Request struct {
	ID          string `json:"id"`
	Operation   string `json:"operation"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Confirm     bool   `json:"confirm,omitempty"`
}
type Item struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Service     string   `json:"service,omitempty"`
	Image       string   `json:"image"`
	ImageID     string   `json:"imageID"`
	Running     bool     `json:"running"`
	Preserved   []string `json:"preserved"`
	Environment []string `json:"environment"`
}
type Preview struct {
	Fingerprint string   `json:"fingerprint"`
	Items       []Item   `json:"items"`
	Order       []string `json:"order"`
}
type entry struct {
	Old         container.InspectResponse
	Reference   string
	TargetImage string
	Platform    string
	Backup      string
	NewID       string
	Touched     bool
	OneShot     bool
}
type journal struct {
	Request Request
	Entries []*entry
	Order   []int
	Mutated bool
}
type Manager struct {
	data   *store.Store
	client *client.Client
	launch func(context.Context, *store.Store, store.Job, string) error
}

func New(data *store.Store) (*Manager, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return nil, err
	}
	return &Manager{data: data, client: cli}, nil
}
func (m *Manager) Close() error { return m.client.Close() }

// Reject unknown configured fields rather than silently losing them when a
// newer Engine/SDK adds settings outside this assessed contract.
func supportedFields(value any, names string) error {
	v := reflect.Indirect(reflect.ValueOf(value))
	allowed := map[string]bool{}
	for _, name := range strings.Fields(names) {
		allowed[name] = true
	}
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		empty := field.IsZero() || ((field.Kind() == reflect.Slice || field.Kind() == reflect.Map) && field.Len() == 0)
		if !allowed[v.Type().Field(i).Name] && !empty {
			return fmt.Errorf("%w: unsupported %s setting", ErrUnsupported, v.Type().Field(i).Name)
		}
	}
	return nil
}

func assess(item container.InspectResponse) error {
	if item.Config == nil || item.HostConfig == nil || item.State == nil || item.Image == "" || item.Name == "" {
		return fmt.Errorf("%w: incomplete container inspection", ErrUnsupported)
	}
	if lifecycle.Protected(item.ID, item.Config.Labels) {
		return lifecycle.ErrProtected
	}
	if item.Config.Labels["com.docker.swarm.service.id"] != "" {
		return fmt.Errorf("%w: Swarm tasks must be managed through their orchestrator", ErrUnsupported)
	}
	if item.State.Paused || item.State.Restarting || item.State.Dead || item.State.Status == "removing" || item.State.OOMKilled {
		return fmt.Errorf("%w: stabilize paused, restarting, dead or OOM-killed containers first", ErrUnsupported)
	}
	if item.State.Running && item.State.Health != nil && item.State.Health.Status != "healthy" {
		return fmt.Errorf("%w: the previous running container must be healthy before replacement", ErrUnsupported)
	}
	if item.Config.StopTimeout != nil && (*item.Config.StopTimeout < 0 || *item.Config.StopTimeout > 30) {
		return fmt.Errorf("%w: stop grace must be bounded to at most 30 seconds", ErrUnsupported)
	}
	if err := supportedFields(item.Config, "Hostname Domainname User AttachStdin AttachStdout AttachStderr ExposedPorts Tty OpenStdin StdinOnce Env Cmd Healthcheck Image Volumes WorkingDir Entrypoint NetworkDisabled Labels StopSignal StopTimeout Shell"); err != nil {
		return err
	}
	if err := supportedFields(item.HostConfig, "Binds LogConfig NetworkMode PortBindings RestartPolicy VolumeDriver ConsoleSize Annotations CapAdd CapDrop CgroupnsMode DNS DNSOptions DNSSearch ExtraHosts GroupAdd IpcMode Cgroup OomScoreAdj PidMode Privileged ReadonlyRootfs SecurityOpt StorageOpt Tmpfs UTSMode UsernsMode ShmSize Sysctls Runtime Umask Resources Mounts MaskedPaths ReadonlyPaths Init"); err != nil {
		return err
	}
	host := item.HostConfig
	if strings.HasPrefix(string(host.NetworkMode), "container:") || host.NetworkMode == "host" || strings.HasPrefix(string(host.IpcMode), "container:") || host.IpcMode == "shareable" || strings.HasPrefix(string(host.PidMode), "container:") {
		return fmt.Errorf("%w: shared container namespaces or host networking require manual management", ErrUnsupported)
	}
	if host.Runtime != "" && host.Runtime != "runc" {
		return fmt.Errorf("%w: custom runtimes require manual management", ErrUnsupported)
	}
	if err := supportedFields(host.Resources, "CPUShares Memory NanoCPUs CgroupParent BlkioWeight BlkioWeightDevice BlkioDeviceReadBps BlkioDeviceWriteBps BlkioDeviceReadIOps BlkioDeviceWriteIOps CPUPeriod CPUQuota CPURealtimePeriod CPURealtimeRuntime CpusetCpus CpusetMems KernelMemoryTCP MemoryReservation MemorySwap MemorySwappiness OomKillDisable PidsLimit Ulimits"); err != nil {
		return err
	}
	for _, bindings := range host.PortBindings {
		for _, binding := range bindings {
			if binding.HostPort == "" || binding.HostPort == "0" {
				return fmt.Errorf("%w: dynamically allocated host ports cannot be preserved", ErrUnsupported)
			}
		}
	}
	for _, mounted := range item.Mounts {
		if mounted.Type != mount.TypeBind && mounted.Type != mount.TypeVolume && mounted.Type != mount.TypeTmpfs {
			return fmt.Errorf("%w: unsupported mount type", ErrUnsupported)
		}
		if mounted.Type == mount.TypeVolume && (mounted.Name == "" || mounted.Driver != "local") {
			return fmt.Errorf("%w: volumes must have an identifiable local driver/name", ErrUnsupported)
		}
	}
	// Bind conversion uses --mount semantics so a missing source cannot be
	// silently replaced with a new empty directory. Reject legacy flags which
	// cannot be represented by the typed Engine mount API.
	for _, binding := range host.Binds {
		parts := strings.Split(binding, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return fmt.Errorf("%w: ambiguous legacy mount", ErrUnsupported)
		}
		actual := false
		for _, mounted := range item.Mounts {
			if mounted.Destination == parts[1] {
				actual = true
				if mounted.Type == mount.TypeBind && len(parts) == 3 {
					for _, option := range strings.Split(parts[2], ",") {
						switch option {
						case "ro", "rw", "private", "rprivate", "shared", "rshared", "slave", "rslave", "consistent", "cached", "delegated":
						default:
							return fmt.Errorf("%w: legacy bind flags require manual management", ErrUnsupported)
						}
					}
				}
			}
		}
		if !actual {
			return fmt.Errorf("%w: legacy mount source cannot be resolved", ErrUnsupported)
		}
	}
	if item.NetworkSettings != nil {
		for _, endpoint := range item.NetworkSettings.Networks {
			if endpoint == nil {
				return fmt.Errorf("%w: missing network endpoint", ErrUnsupported)
			}
			if len(endpoint.Links) > 0 {
				return fmt.Errorf("%w: legacy network links require manual management", ErrUnsupported)
			}
			if err := supportedFields(endpoint, "IPAMConfig Links Aliases DriverOpts GwPriority NetworkID EndpointID Gateway IPAddress MacAddress IPPrefixLen IPv6Gateway GlobalIPv6Address GlobalIPv6PrefixLen DNSNames"); err != nil {
				return err
			}
		}
	}
	if strings.EqualFold(item.Config.Labels["com.docker.compose.oneoff"], "true") {
		return fmt.Errorf("%w: Compose one-off containers require their original source", ErrUnsupported)
	}
	return nil
}

func (m *Manager) snapshot(ctx context.Context, input Request) (*journal, Preview, error) {
	if input.Operation != "update" && input.Operation != "recreate" {
		return nil, Preview{}, ErrInvalid
	}
	name, compose := strings.CutPrefix(input.ID, "compose:")
	id, single := strings.CutPrefix(input.ID, "container:")
	if (!compose || name == "") && (!single || id == "") {
		return nil, Preview{}, ErrInvalid
	}
	listed, err := m.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, Preview{}, err
	}
	state := &journal{Request: input}
	for _, summary := range listed.Items {
		if (compose && summary.Labels["com.docker.compose.project"] == name) || (single && summary.ID == id) {
			inspect, err := m.client.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
			if errdefs.IsNotFound(err) {
				return nil, Preview{}, lifecycle.ErrChanged
			}
			if err != nil {
				return nil, Preview{}, err
			}
			item := inspect.Container
			if err := assess(item); err != nil {
				return nil, Preview{}, err
			}
			for networkName := range endpoints(item) {
				inspected, err := m.client.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
				if err != nil {
					return nil, Preview{}, err
				}
				if inspected.Network.Scope != "local" || (inspected.Network.Driver != "bridge" && inspected.Network.Driver != "null") {
					return nil, Preview{}, fmt.Errorf("%w: only local bridge or disabled networking is assessed", ErrUnsupported)
				}
			}
			project := item.Config.Labels["com.docker.compose.project"]
			if single && project != "" {
				return nil, Preview{}, fmt.Errorf("%w: update the complete Compose project instead of one member", ErrUnsupported)
			}
			if project != "" {
				if _, found, err := m.data.ManagedProject(project); err != nil {
					return nil, Preview{}, err
				} else if found {
					return nil, Preview{}, fmt.Errorf("%w: use stored managed-project operations", ErrUnsupported)
				}
				if item.Config.Labels["com.docker.compose.service"] == "" || item.Config.Labels["com.docker.compose.container-number"] == "" {
					return nil, Preview{}, fmt.Errorf("%w: incomplete Compose service/replica identity", ErrUnsupported)
				}
			}
			image, err := m.client.ImageInspect(ctx, item.Image)
			if err != nil {
				return nil, Preview{}, err
			}
			if image.Os != "linux" {
				return nil, Preview{}, fmt.Errorf("%w: only Linux containers are assessed", ErrUnsupported)
			}
			platform := image.Os + "/" + image.Architecture
			if image.Variant != "" {
				platform += "/" + image.Variant
			}
			state.Entries = append(state.Entries, &entry{Old: item, Reference: imageidentity.Reference(item.Config.Image, item.Config.Labels), Platform: platform})
		}
	}
	if len(state.Entries) == 0 {
		return nil, Preview{}, lifecycle.ErrNotFound
	}
	if len(state.Entries) > 16 {
		return nil, Preview{}, fmt.Errorf("%w: replacement groups are limited to 16 containers", ErrUnsupported)
	}
	sort.Slice(state.Entries, func(i, j int) bool { return state.Entries[i].Old.ID < state.Entries[j].Old.ID })
	// Check observable incoming namespace/link/volume dependencies on the host.
	for _, other := range listed.Items {
		isTarget := false
		for _, target := range state.Entries {
			if other.ID == target.Old.ID {
				isTarget = true
			}
		}
		if isTarget {
			continue
		}
		inspected, err := m.client.ContainerInspect(ctx, other.ID, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			// An unrelated container disappearing cannot leave an incoming
			// dependency to preserve. Selected targets still require exact presence.
			continue
		}
		if err != nil {
			return nil, Preview{}, err
		}
		host := inspected.Container.HostConfig
		if host == nil {
			return nil, Preview{}, fmt.Errorf("%w: incoming dependencies cannot be assessed", ErrUnsupported)
		}
		for _, value := range append(append([]string{string(host.NetworkMode), string(host.IpcMode), string(host.PidMode)}, host.VolumesFrom...), host.Links...) {
			for _, target := range state.Entries {
				ref := strings.TrimPrefix(value, "container:")
				ref, _, _ = strings.Cut(ref, ":")
				ref = strings.TrimPrefix(ref, "/")
				if ref == target.Old.ID || ref == strings.TrimPrefix(target.Old.Name, "/") || (len(ref) >= 12 && strings.HasPrefix(target.Old.ID, ref)) {
					return nil, Preview{}, fmt.Errorf("%w: another container depends on the selected container namespace, link or volumes-from", ErrUnsupported)
				}
			}
		}
	}
	order, err := dependencyOrder(state.Entries)
	if err != nil {
		return nil, Preview{}, err
	}
	state.Order = order
	preview := Preview{Items: []Item{}, Order: []string{}}
	for _, target := range state.Entries {
		names := []string{}
		for _, value := range target.Old.Config.Env {
			key, _, _ := strings.Cut(value, "=")
			names = append(names, key)
		}
		sort.Strings(names)
		preview.Items = append(preview.Items, Item{ID: target.Old.ID, Name: strings.TrimPrefix(target.Old.Name, "/"), Service: target.Old.Config.Labels["com.docker.compose.service"], Image: target.Reference, ImageID: target.Old.Image, Running: target.Old.State.Running, Environment: names, Preserved: []string{"ports", "mounts and data", "networks and aliases", "environment and labels", "command and entrypoint", "user and working directory", "healthcheck and restart policy", "resource and security settings"}})
	}
	for _, i := range order {
		preview.Order = append(preview.Order, strings.TrimPrefix(state.Entries[i].Old.Name, "/"))
	}
	preview.Fingerprint = fingerprint(state)
	return state, preview, nil
}

func fingerprint(state *journal) string {
	values := []any{state.Request.ID, state.Request.Operation}
	for _, target := range state.Entries {
		item := target.Old
		networkIDs := map[string]string{}
		if item.NetworkSettings != nil {
			for name, endpoint := range item.NetworkSettings.Networks {
				networkIDs[name] = endpoint.NetworkID
			}
		}
		values = append(values, []any{item.ID, item.Name, item.Image, item.Config, item.HostConfig, item.Mounts, endpoints(item), item.State.Running, item.State.Status, item.State.ExitCode, item.State.Paused, item.State.Restarting, item.State.StartedAt})
		values = append(values, networkIDs)
	}
	encoded, _ := json.Marshal(values)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func endpoints(item container.InspectResponse) map[string]*network.EndpointSettings {
	result := map[string]*network.EndpointSettings{}
	if item.NetworkSettings == nil {
		return result
	}
	for name, endpoint := range item.NetworkSettings.Networks {
		result[name] = &network.EndpointSettings{IPAMConfig: endpoint.IPAMConfig.Copy(), Aliases: sorted(endpoint.Aliases), DriverOpts: endpoint.DriverOpts, GwPriority: endpoint.GwPriority, MacAddress: endpoint.MacAddress}
	}
	return result
}

func dependencyOrder(entries []*entry) ([]int, error) {
	services := map[string][]int{}
	for i, target := range entries {
		services[target.Old.Config.Labels["com.docker.compose.service"]] = append(services[target.Old.Config.Labels["com.docker.compose.service"]], i)
	}
	dependencies := map[int][]int{}
	for i, target := range entries {
		for _, value := range strings.Split(target.Old.Config.Labels["com.docker.compose.depends_on"], ",") {
			if value == "" {
				continue
			}
			parts := strings.Split(value, ":")
			if len(parts) != 3 || (parts[1] != "service_started" && parts[1] != "service_healthy" && parts[1] != "service_completed_successfully") || (parts[2] != "true" && parts[2] != "false") || len(services[parts[0]]) == 0 {
				return nil, fmt.Errorf("%w: unsupported or missing Compose dependency", ErrUnsupported)
			}
			for _, dep := range services[parts[0]] {
				if !entries[dep].Old.State.Running && parts[1] != "service_completed_successfully" && target.Old.State.Running {
					return nil, fmt.Errorf("%w: running service depends on a stopped service", ErrUnsupported)
				}
				if parts[1] == "service_healthy" && entries[dep].Old.Config.Healthcheck == nil {
					return nil, fmt.Errorf("%w: healthy dependency has no healthcheck", ErrUnsupported)
				}
				if parts[1] == "service_completed_successfully" {
					if entries[dep].Old.State.Running || entries[dep].Old.State.Status != "exited" || entries[dep].Old.State.ExitCode != 0 {
						return nil, fmt.Errorf("%w: one-shot dependency has not completed successfully", ErrUnsupported)
					}
					entries[dep].OneShot = true
				}
				dependencies[i] = append(dependencies[i], dep)
			}
		}
	}
	order, visiting, visited := []int{}, map[int]bool{}, map[int]bool{}
	var visit func(int) error
	visit = func(i int) error {
		if visiting[i] {
			return fmt.Errorf("%w: cyclic Compose dependencies", ErrUnsupported)
		}
		if visited[i] {
			return nil
		}
		visiting[i] = true
		for _, dep := range dependencies[i] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[i], visited[i] = false, true
		order = append(order, i)
		return nil
	}
	for i := range entries {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (m *Manager) Preview(ctx context.Context, input Request) (Preview, error) {
	_, preview, err := m.snapshot(ctx, input)
	return preview, err
}
