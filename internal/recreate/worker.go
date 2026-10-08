package recreate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func (m *Manager) Submit(ctx context.Context, input Request) (store.Job, error) {
	if !input.Confirm || len(input.Fingerprint) != 64 {
		return store.Job{}, ErrInvalid
	}
	state, preview, err := m.snapshot(ctx, input)
	if err != nil {
		return store.Job{}, err
	}
	if preview.Fingerprint != input.Fingerprint {
		return store.Job{}, lifecycle.ErrChanged
	}
	resources := []string{input.ID}
	for _, target := range state.Entries {
		resources = append(resources, "container:"+target.Old.ID)
		if root := target.Old.Config.Labels[TargetLabel]; strings.HasPrefix(root, "container:") {
			resources = append(resources, root)
		}
	}
	job, err := store.NewJob(input.ID, "engine", input.Operation, resources)
	if err != nil {
		return job, err
	}
	job = store.ScheduledJob(ctx, job)
	for _, target := range state.Entries {
		target.Backup = "nox-yard-retained-" + job.ID + "-" + target.Old.ID[:12]
		job.SourceImages = append(job.SourceImages, store.ImageIdentity{ContainerID: target.Old.ID, ImageID: target.Old.Image, Service: target.Old.Config.Labels["com.docker.compose.service"], Platform: target.Platform, StartedAt: target.Old.State.StartedAt})
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return job, err
	}
	job.Payload = string(encoded)
	if err := m.data.CreateJob(job); err != nil {
		return job, err
	}
	launch := m.launch
	if launch == nil {
		launch = jobs.Launch
	}
	if err := launch(ctx, m.data, job, "recreate-worker"); err != nil {
		outcome, message := "failed", "Independent replacement worker could not be launched; no replacement was requested."
		if errors.Is(err, jobs.ErrLaunchUncertain) {
			outcome, message = "recovery_required", "Worker launch is uncertain. Inspect retained resources before acknowledging recovery; no replacement was replayed."
		}
		_ = m.data.FinishJob(job.ID, job.Owner, "failed", outcome, message, "")
	}
	current, _, err := m.data.Job(job.ID)
	return current, err
}

func RunWorker(data *store.Store, id, owner string) error {
	job, found, err := data.Job(id)
	if err != nil {
		return err
	}
	if !found {
		return store.ErrJobChanged
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(job.DeadlineAt, 0))
	defer cancel()
	if err := jobs.ValidateWorker(ctx, job, owner); err != nil {
		return err
	}
	if err := data.ClaimJob(id, owner); err != nil {
		return err
	}
	m, err := New(data)
	if err != nil {
		return err
	}
	defer m.Close()
	return m.execute(ctx, job)
}

func (m *Manager) save(job store.Job, state *journal, stage string, results []store.ImageIdentity) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := m.data.JobPayload(job.ID, job.Owner, string(encoded)); err != nil {
		return err
	}
	return m.data.JobProgress(job.ID, job.Owner, stage, nil, results)
}

func clone[T any](value T) T {
	encoded, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(encoded, &result)
	return result
}

func creation(target *entry, root string) client.ContainerCreateOptions {
	config, host := clone(target.Old.Config), clone(target.Old.HostConfig)
	config.Image = target.TargetImage
	if config.Hostname != "" && strings.HasPrefix(target.Old.ID, config.Hostname) {
		config.Hostname = ""
	}
	if config.Labels == nil {
		config.Labels = map[string]string{}
	}
	config.Labels[imageidentity.ReferenceLabel] = target.Reference
	if strings.HasPrefix(root, "container:") {
		config.Labels[TargetLabel] = root
	}
	covered := map[string]bool{}
	binds := []string{}
	for _, binding := range host.Binds {
		parts := strings.Split(binding, ":")
		if len(parts) >= 2 {
			covered[parts[1]] = true
			converted := false
			for _, actual := range target.Old.Mounts {
				if actual.Destination == parts[1] && actual.Type == mount.TypeBind {
					mounted := mount.Mount{Type: mount.TypeBind, Source: actual.Source, Target: actual.Destination, ReadOnly: !actual.RW, BindOptions: &mount.BindOptions{Propagation: actual.Propagation}}
					if len(parts) == 3 {
						for _, option := range strings.Split(parts[2], ",") {
							if option == "consistent" || option == "cached" || option == "delegated" {
								mounted.Consistency = mount.Consistency(option)
							}
						}
					}
					host.Mounts = append(host.Mounts, mounted)
					converted = true
				}
			}
			if !converted {
				binds = append(binds, binding)
			}
		}
	}
	host.Binds = binds
	for i, mounted := range host.Mounts {
		covered[mounted.Target] = true
		if mounted.Type == mount.TypeBind && mounted.BindOptions != nil {
			host.Mounts[i].BindOptions.CreateMountpoint = false
		}
		if mounted.Type == mount.TypeVolume && mounted.Source == "" {
			for _, actual := range target.Old.Mounts {
				if actual.Destination == mounted.Target {
					host.Mounts[i].Source = actual.Name
				}
			}
		}
	}
	for _, mounted := range target.Old.Mounts {
		if mounted.Type == mount.TypeVolume && !covered[mounted.Destination] {
			host.Mounts = append(host.Mounts, mount.Mount{Type: mount.TypeVolume, Source: mounted.Name, Target: mounted.Destination, ReadOnly: !mounted.RW, VolumeOptions: &mount.VolumeOptions{NoCopy: true}})
		}
	}
	parts := strings.Split(target.Platform, "/")
	platform := &ocispec.Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) > 2 {
		platform.Variant = parts[2]
	}
	return client.ContainerCreateOptions{Name: strings.TrimPrefix(target.Old.Name, "/"), Config: config, HostConfig: host, NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: endpoints(target.Old)}, Platform: platform}
}

func results(state *journal, outcome string) []store.ImageIdentity {
	images := []store.ImageIdentity{}
	for _, target := range state.Entries {
		id, image := target.NewID, target.TargetImage
		if id == "" || outcome == "rolled_back" || outcome == "unchanged" {
			id, image = target.Old.ID, target.Old.Image
		}
		status := "stopped"
		if target.Old.State.Running {
			status = "running"
		} else if target.OneShot {
			status = "completed"
		}
		if outcome != "verified" && outcome != "reconciled" && outcome != "unchanged" && outcome != "rolled_back" {
			status = "unknown"
		}
		images = append(images, store.ImageIdentity{ContainerID: id, PreviousContainerID: target.Old.ID, ImageID: image, Service: target.Old.Config.Labels["com.docker.compose.service"], Platform: target.Platform, Outcome: outcome, State: status})
	}
	return images
}

func (m *Manager) execute(ctx context.Context, job store.Job) error {
	var state journal
	if json.Unmarshal([]byte(job.Payload), &state) != nil || len(state.Entries) == 0 {
		return store.ErrJobChanged
	}
	fail := func(message string, cause error) error {
		outcome, rollback := "failed", "not_needed"
		if ctx.Err() != nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) || errdefs.IsDeadlineExceeded(cause) {
			outcome, rollback = "recovery_required", "not_performed"
			message += " Completion is uncertain; inspect retained containers and the private journal before acknowledging recovery."
		} else if state.Mutated {
			_ = m.save(job, &state, "rolling_back", results(&state, "restoring"))
			recovery, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			err := m.rollback(recovery, job, &state)
			cancel()
			if err != nil {
				outcome, rollback = "recovery_required", "failed"
				message += " Original containers could not be fully restored; inspect retained originals and replacements before acknowledging recovery."
			} else {
				outcome, rollback = "rolled_back", "restored"
				message += " Original containers, names, configuration and running/stopped state were restored. Application data writes were not undone."
			}
		}
		if err := m.save(job, &state, "committing", results(&state, outcome)); err != nil {
			return err
		}
		return m.data.FinishJob(job.ID, job.Owner, "failed", outcome, message, rollback)
	}
	if err := m.save(job, &state, "preparing", nil); err != nil {
		return err
	}
	_, fresh, err := m.snapshot(ctx, state.Request)
	if err != nil || fresh.Fingerprint != state.Request.Fingerprint {
		return fail("Target configuration changed before execution; review a fresh preview.", err)
	}
	if job.ScheduleKey != "" {
		enabled, err := m.data.ScheduleEnabled(job)
		if err != nil {
			return err
		}
		if !enabled || !automaticRuntime(&state) {
			return m.data.FinishJob(job.ID, job.Owner, "succeeded", "skipped", "Automatic update skipped because the project is disabled, stopped or protected.", "")
		}
	}
	additionalVolumes := false
	if state.Request.Operation == "update" {
		if err := m.save(job, &state, "pulling", nil); err != nil {
			return err
		}
		pulled := map[string]bool{}
		for _, target := range state.Entries {
			key := target.Reference + "|" + target.Platform
			if !pulled[key] {
				options := creation(target, "")
				response, err := m.client.ImagePull(ctx, target.Reference, client.ImagePullOptions{Platforms: []ocispec.Platform{*options.Platform}})
				if err == nil {
					err = response.Wait(ctx)
					_ = response.Close()
				}
				if err != nil {
					return fail("Image pull failed; check registry access, credentials and availability. Existing containers were retained.", err)
				}
				pulled[key] = true
			}
			image, err := m.client.ImageInspect(ctx, target.Reference)
			if err != nil {
				return fail("The pulled image identity could not be inspected.", err)
			}
			actual := image.Os + "/" + image.Architecture
			if image.Variant != "" {
				actual += "/" + image.Variant
			}
			if actual != target.Platform {
				return fail("The pulled image does not match the assessed host platform.", nil)
			}
			// Extra image-declared anonymous volumes cannot be preserved from the
			// confirmed runtime snapshot. Refuse before stopping any original.
			if image.Config != nil {
				for destination := range image.Config.Volumes {
					if _, present := target.Old.Config.Volumes[destination]; !present {
						additionalVolumes = true
					}
				}
			}
			target.TargetImage = image.ID
		}
	} else {
		for _, target := range state.Entries {
			target.TargetImage = target.Old.Image
		}
	}
	_, fresh, err = m.snapshot(ctx, state.Request)
	if err != nil || fresh.Fingerprint != state.Request.Fingerprint {
		return fail("Target configuration changed during pull; review again.", err)
	}
	images := []store.ImageIdentity{}
	for _, target := range state.Entries {
		images = append(images, store.ImageIdentity{Service: target.Old.Config.Labels["com.docker.compose.service"], ImageID: target.TargetImage, Platform: target.Platform})
	}
	reason, err := m.data.UpdateCandidate(job, images)
	if err != nil {
		return err
	}
	if reason != "" {
		if err := m.save(job, &state, "committing", results(&state, "skipped")); err != nil {
			return err
		}
		return m.data.FinishJob(job.ID, job.Owner, "succeeded", "skipped", reason, "")
	}
	if additionalVolumes {
		return fail("The new image declares additional volumes; review the deployment source before replacement.", nil)
	}
	unchanged := state.Request.Operation == "update"
	for _, target := range state.Entries {
		if target.TargetImage != target.Old.Image {
			unchanged = false
		}
	}
	if unchanged {
		if err := m.save(job, &state, "committing", results(&state, "unchanged")); err != nil {
			return err
		}
		return m.data.FinishJob(job.ID, job.Owner, "succeeded", "unchanged", "", "")
	}
	if job.ScheduleKey != "" {
		reason, err := m.data.UpdateCandidate(job, images)
		if err != nil {
			return err
		}
		if reason != "" {
			return m.data.FinishJob(job.ID, job.Owner, "succeeded", "skipped", reason, "")
		}
	}
	state.Mutated = true
	if err := m.save(job, &state, "replacing", results(&state, "pending")); err != nil {
		return err
	}
	// Stop dependants before dependencies, retaining originals and their images.
	for i := len(state.Order) - 1; i >= 0; i-- {
		target := state.Entries[state.Order[i]]
		target.Touched = true
		if err := m.save(job, &state, "replacing", nil); err != nil {
			return err
		}
		if target.Old.State.Running {
			if _, err := m.client.ContainerStop(ctx, target.Old.ID, client.ContainerStopOptions{}); err != nil {
				return fail("An original container could not be stopped.", err)
			}
		}
		if _, err := m.client.ContainerRename(ctx, target.Old.ID, client.ContainerRenameOptions{NewName: target.Backup}); err != nil {
			return fail("An original container could not be retained under its recovery name.", err)
		}
		for name := range endpoints(target.Old) {
			if name == "none" {
				continue
			}
			if _, err := m.client.NetworkDisconnect(ctx, name, client.NetworkDisconnectOptions{Container: target.Old.ID}); err != nil {
				return fail("An original network endpoint could not be retained safely.", err)
			}
		}
	}
	root := state.Request.ID
	if len(state.Entries) == 1 && state.Entries[0].Old.Config.Labels[TargetLabel] != "" {
		root = state.Entries[0].Old.Config.Labels[TargetLabel]
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for _, index := range state.Order {
		target := state.Entries[index]
		created, err := m.client.ContainerCreate(ctx, creation(target, root))
		if err != nil {
			return fail("A replacement could not be created; inspect image availability, ports and mounts.", err)
		}
		target.NewID = created.ID
		if err := m.save(job, &state, "replacing", results(&state, "pending")); err != nil {
			return err
		}
		if err := m.data.RetargetJob(job.ID, job.Owner, job.TargetID, []string{"container:" + target.NewID}); err != nil {
			return fail("Replacement ownership could not be reserved.", err)
		}
		if target.Old.State.Running || target.OneShot {
			if _, err := m.client.ContainerStart(ctx, target.NewID, client.ContainerStartOptions{}); err != nil {
				return fail("A replacement could not be started; inspect host ports and mounts.", err)
			}
		}
		if err := m.ready(verifyCtx, target, target.NewID); err != nil {
			return fail("Replacement readiness failed for "+strings.TrimPrefix(target.Old.Name, "/")+".", nil)
		}
	}
	if err := m.save(job, &state, "verifying", results(&state, "verifying")); err != nil {
		return err
	}
	if err := m.verify(verifyCtx, &state, root); err != nil {
		message := "Replacement configuration, images or running/stopped state could not be verified."
		var preserved preservationError
		if errors.As(err, &preserved) {
			message += " " + preserved.Error()
		}
		return fail(message, nil)
	}
	if err := m.complete(job, &state, "verified"); err != nil {
		return err
	}
	return nil
}

func (m *Manager) ready(ctx context.Context, target *entry, id string) error {
	for {
		inspected, err := m.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		state := inspected.Container.State
		if state == nil || state.Paused || state.Restarting || state.OOMKilled {
			return fmt.Errorf("unstable container")
		}
		if target.OneShot {
			if state.Status == "exited" {
				if state.ExitCode == 0 {
					return nil
				}
				return fmt.Errorf("one-shot failed")
			}
		} else if !target.Old.State.Running {
			if state.Running {
				return fmt.Errorf("stopped container unexpectedly running")
			}
			return nil
		} else {
			if !state.Running {
				return fmt.Errorf("running container exited")
			}
			if state.Health != nil && state.Health.Status == "unhealthy" {
				return fmt.Errorf("unhealthy container")
			}
			if state.Health == nil || state.Health.Status == "healthy" {
				started, err := time.Parse(time.RFC3339Nano, state.StartedAt)
				if err == nil && time.Since(started) >= 5*time.Second {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func normalizeConfig(item container.InspectResponse) *container.Config {
	config := clone(item.Config)
	if config.Hostname != "" && strings.HasPrefix(item.ID, config.Hostname) {
		config.Hostname = ""
	}
	return config
}

func normalizeHost(host *container.HostConfig) *container.HostConfig {
	result := clone(host)
	// Engine initializes the unset false pointer only after first start.
	if result.OomKillDisable != nil && !*result.OomKillDisable {
		result.OomKillDisable = nil
	}
	return result
}

func (m *Manager) verify(ctx context.Context, state *journal, root string) error {
	for _, target := range state.Entries {
		if target.NewID == "" {
			return store.ErrJobChanged
		}
		original, err := m.client.ContainerInspect(ctx, target.Old.ID, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		old := original.Container
		if old.Name != "/"+target.Backup || old.State == nil || old.State.Running || old.Image != target.Old.Image || !equivalent(old.Config, target.Old.Config) || !equivalent(normalizeHost(old.HostConfig), normalizeHost(target.Old.HostConfig)) {
			return preservationError{"Retained original changed during replacement."}
		}
		inspected, err := m.client.ContainerInspect(ctx, target.NewID, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		actual := inspected.Container
		wanted := creation(target, root)
		if actual.Image != target.TargetImage || actual.Name != target.Old.Name || !equivalent(normalizeConfig(actual), wanted.Config) || !equivalent(normalizeHost(actual.HostConfig), normalizeHost(wanted.HostConfig)) {
			return preservationError{fmt.Sprintf("Preserved configuration differs (%s; %s).", differing(normalizeConfig(actual), wanted.Config), differing(normalizeHost(actual.HostConfig), normalizeHost(wanted.HostConfig)))}
		}
		if !reflect.DeepEqual(endpoints(actual), wanted.NetworkingConfig.EndpointsConfig) {
			return preservationError{"Preserved network endpoint settings differ."}
		}
		if !equivalent(mountIdentities(actual), mountIdentities(target.Old)) {
			return preservationError{"Preserved mount sources, access or propagation differ."}
		}
		if err := m.ready(ctx, target, target.NewID); err != nil {
			return err
		}
	}
	return nil
}

// Compare effective data sources, not Docker's legacy/typed encoding of modes.
func mountIdentities(item container.InspectResponse) []any {
	mounted := append([]container.MountPoint(nil), item.Mounts...)
	sort.Slice(mounted, func(i, j int) bool { return mounted[i].Destination < mounted[j].Destination })
	result := []any{}
	for _, value := range mounted {
		result = append(result, []any{value.Type, value.Name, value.Source, value.Destination, value.Driver, value.RW, value.Propagation})
	}
	return result
}

func (m *Manager) rollback(ctx context.Context, job store.Job, state *journal) error {
	// Remove candidates in reverse dependency order. Data volumes are untouched.
	for i := len(state.Order) - 1; i >= 0; i-- {
		target := state.Entries[state.Order[i]]
		if target.NewID != "" {
			if _, err := m.client.ContainerRemove(ctx, target.NewID, client.ContainerRemoveOptions{Force: true}); err != nil && !errdefs.IsNotFound(err) {
				return err
			}
		}
	}
	for _, index := range state.Order {
		target := state.Entries[index]
		if !target.Touched {
			continue
		}
		inspected, err := m.client.ContainerInspect(ctx, target.Old.ID, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		if inspected.Container.Name != target.Old.Name {
			if inspected.Container.Name != "/"+target.Backup {
				return store.ErrJobChanged
			}
			if _, err := m.client.ContainerRename(ctx, target.Old.ID, client.ContainerRenameOptions{NewName: strings.TrimPrefix(target.Old.Name, "/")}); err != nil {
				return err
			}
		}
		for name, endpoint := range endpoints(target.Old) {
			if inspected.Container.NetworkSettings != nil && inspected.Container.NetworkSettings.Networks[name] != nil {
				continue
			}
			if _, err := m.client.NetworkConnect(ctx, name, client.NetworkConnectOptions{Container: target.Old.ID, EndpointConfig: endpoint}); err != nil {
				return err
			}
		}
		if target.Old.State.Running && !inspected.Container.State.Running {
			if _, err := m.client.ContainerStart(ctx, target.Old.ID, client.ContainerStartOptions{}); err != nil {
				return err
			}
		}
		if !target.Old.State.Running && inspected.Container.State.Running {
			return store.ErrJobChanged
		}
		if err := m.ready(ctx, target, target.Old.ID); err != nil {
			return err
		}
		actual, err := m.client.ContainerInspect(ctx, target.Old.ID, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		if actual.Container.Image != target.Old.Image || !reflect.DeepEqual(actual.Container.Config, target.Old.Config) || !reflect.DeepEqual(actual.Container.HostConfig, target.Old.HostConfig) || !reflect.DeepEqual(endpoints(actual.Container), endpoints(target.Old)) {
			return store.ErrJobChanged
		}
	}
	return m.data.RetargetJob(job.ID, job.Owner, state.Request.ID, nil)
}

func (m *Manager) complete(job store.Job, state *journal, outcome string) error {
	target := state.Request.ID
	if strings.HasPrefix(target, "container:") {
		target = "container:" + state.Entries[0].NewID
	}
	if err := m.data.RetargetJob(job.ID, job.Owner, target, nil); err != nil {
		return err
	}
	if err := m.save(job, state, "committing", results(state, outcome)); err != nil {
		return err
	}
	return m.data.FinishJob(job.ID, job.Owner, "succeeded", outcome, "", "")
}

func (m *Manager) Reconcile(ctx context.Context, job store.Job) error {
	var state journal
	if json.Unmarshal([]byte(job.Payload), &state) != nil || !state.Mutated {
		return store.ErrJobChanged
	}
	root := state.Request.ID
	if len(state.Entries) == 1 && state.Entries[0].Old.Config.Labels[TargetLabel] != "" {
		root = state.Entries[0].Old.Config.Labels[TargetLabel]
	}
	if err := m.verify(ctx, &state, root); err != nil {
		return err
	}
	return m.complete(job, &state, "reconciled")
}

// Recovery acknowledgement deliberately retains originals/candidates for host
// review. Only verified success permits deleting retained originals.
func (m *Manager) Cleanup(ctx context.Context, job store.Job) error {
	if job.Status != "succeeded" || (job.Outcome != "verified" && job.Outcome != "reconciled") {
		return nil
	}
	var state journal
	if json.Unmarshal([]byte(job.Payload), &state) != nil {
		return store.ErrJobChanged
	}
	for _, target := range state.Entries {
		inspected, err := m.client.ContainerInspect(ctx, target.Old.ID, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		actual := inspected.Container
		if actual.Name != "/"+target.Backup || actual.State == nil || actual.State.Running || actual.Image != target.Old.Image {
			return store.ErrJobChanged
		}
		if _, err := m.client.ContainerRemove(ctx, target.Old.ID, client.ContainerRemoveOptions{}); err != nil {
			return err
		}
	}
	return nil
}

type preservationError struct{ message string }

func (e preservationError) Error() string { return e.message }

// Docker may encode unset lists as null or [] before/after first start. Treat
// only empty collections equivalently; configured values still compare exactly.
func equivalent(a, b any) bool {
	canonical := func(value any) any {
		encoded, _ := json.Marshal(value)
		var decoded any
		_ = json.Unmarshal(encoded, &decoded)
		var normalize func(any) any
		normalize = func(value any) any {
			switch typed := value.(type) {
			case []any:
				if len(typed) == 0 {
					return nil
				}
				for i, item := range typed {
					typed[i] = normalize(item)
				}
			case map[string]any:
				if len(typed) == 0 {
					return nil
				}
				for key, item := range typed {
					typed[key] = normalize(item)
				}
			}
			return value
		}
		return normalize(decoded)
	}
	return reflect.DeepEqual(canonical(a), canonical(b))
}

func differing(a, b any) string {
	left, right := reflect.Indirect(reflect.ValueOf(a)), reflect.Indirect(reflect.ValueOf(b))
	names := []string{}
	for i := 0; i < left.NumField(); i++ {
		if !equivalent(left.Field(i).Interface(), right.Field(i).Interface()) {
			name := left.Type().Field(i).Name
			if left.Field(i).Kind() == reflect.Struct {
				name += "." + differing(left.Field(i).Interface(), right.Field(i).Interface())
			}
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

// Keep deterministic test comparisons when Docker returns aliases reordered.
func sorted(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
