package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/yardrelease"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type fileDeployment struct {
	Self           bool
	Service        string
	Source         FileSnapshot
	Runtime        []container.InspectResponse
	Targets        map[string]string
	Definition     string
	Mutation       bool
	DatabaseBackup string
	VerifiedYard   *yardrelease.Candidate
}

func (m *Manager) FilePreview(ctx context.Context, name, operation string) (recreate.Preview, error) {
	return m.ServiceFilePreview(ctx, name, operation, "")
}

func (m *Manager) ServiceFilePreview(ctx context.Context, name, operation, service string) (recreate.Preview, error) {
	if service != "" && operation != "recreate" {
		return recreate.Preview{}, ErrInvalidSource
	}
	if operation != "recreate" && operation != "update" && operation != "pull" {
		return recreate.Preview{}, ErrInvalidSource
	}
	source, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return recreate.Preview{}, err
	}
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return recreate.Preview{}, err
	}
	selected, err := fileServiceRuntime(source, runtime, service)
	if err != nil {
		return recreate.Preview{}, err
	}
	if service != "" {
		if err := recreate.CheckServiceIsolation(ctx, selected); err != nil {
			return recreate.Preview{}, err
		}
	}
	view := recreate.Preview{Items: []recreate.Item{}, Order: []string{}}
	for _, item := range selected {
		if item.Config == nil || item.State == nil || item.HostConfig == nil || item.State.Paused || item.State.Restarting {
			return view, fmt.Errorf("%w: stabilize the existing containers before recreation", ErrConflict)
		}
		if operation != "pull" && (strings.HasPrefix(string(item.HostConfig.NetworkMode), "container:") || strings.HasPrefix(string(item.HostConfig.PidMode), "container:") || strings.HasPrefix(string(item.HostConfig.IpcMode), "container:") || len(item.HostConfig.Links) > 0 || item.HostConfig.AutoRemove) {
			return view, fmt.Errorf("%w: shared container namespaces, legacy links or automatic removal cannot be safely recovered", ErrInvalidSource)
		}
		keys := []string{}
		for _, env := range item.Config.Env {
			key, _, _ := strings.Cut(env, "=")
			keys = append(keys, key)
		}
		sort.Strings(keys)
		view.Items = append(view.Items, recreate.Item{ID: item.ID, Name: strings.TrimPrefix(item.Name, "/"), Service: item.Config.Labels["com.docker.compose.service"], Image: item.Config.Image, ImageID: item.Image, Running: item.State.Running, Environment: keys, Preserved: []string{"host Compose files", "environment files", "volumes", "bind data", "running/stopped state"}})
		view.Order = append(view.Order, item.Config.Labels["com.docker.compose.service"])
	}
	preview := scopedFilePreview(source, service)
	if operation != "pull" {
		if err := m.assessServicePorts(ctx, name, service, preview.model); err != nil {
			return view, err
		}
	}
	view.Fingerprint = fileOperationFingerprint(source, runtime, operation, service)
	return view, nil
}

func (m *Manager) FileOperation(ctx context.Context, name, operation, confirmedFingerprint string) (store.Job, error) {
	return m.ServiceFileOperation(ctx, name, operation, "", confirmedFingerprint)
}

func (m *Manager) ServiceFileOperation(ctx context.Context, name, operation, service, confirmedFingerprint string) (store.Job, error) {
	if service != "" && operation != "recreate" {
		return store.Job{}, ErrInvalidSource
	}
	if operation != "pull" && operation != "recreate" && operation != "update" {
		return store.Job{}, ErrInvalidSource
	}
	preview, err := m.ServiceFilePreview(ctx, name, operation, service)
	if err != nil {
		return store.Job{}, err
	}
	if confirmedFingerprint != "" && confirmedFingerprint != preview.Fingerprint {
		return store.Job{}, fmt.Errorf("%w: configuration changed; review again", ErrConflict)
	}
	source, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return store.Job{}, err
	}
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return store.Job{}, err
	}
	if fileOperationFingerprint(source, runtime, operation, service) != preview.Fingerprint {
		return store.Job{}, fmt.Errorf("%w: project changed during confirmation; review again", ErrConflict)
	}
	location, _ := json.Marshal(source.ComposeFiles)
	if err := m.data.SetFileConfiguration(store.FileConfiguration{Name: name, Directory: source.Directory, ComposeFiles: string(location), State: "ready"}); err != nil {
		return store.Job{}, err
	}
	project := store.ManagedProject{Name: name, ProjectDir: source.Directory, YAML: string(source.Resolved), VariablesJSON: "{}", EnvFilesJSON: "{}"}
	hostname, _ := os.Hostname()
	self := false
	selected, err := fileServiceRuntime(source, runtime, service)
	if err != nil {
		return store.Job{}, err
	}
	for _, item := range selected {
		if strings.HasPrefix(item.ID, hostname) && item.Config != nil && item.Config.Labels["com.docker.compose.service"] == "nox-yard" {
			self = true
		}
	}
	if self && operation != "pull" {
		if err := validateSelfFileConfiguration(filePreview(source), runtime, m.data.Directory()); err != nil {
			return store.Job{}, err
		}
	}
	work := &fileDeployment{Self: self, Service: service, Source: source, Runtime: runtime}
	if self && operation == "update" {
		verificationErr := m.prepareSignedYardUpdate(ctx, work, scopedFilePreview(source, service))
		message := ""
		if verificationErr != nil {
			message = verificationErr.Error()
		}
		if err := m.data.RecordSelfUpdateCheck(message); err != nil {
			return store.Job{}, err
		}
		if verificationErr != nil {
			return store.Job{}, fmt.Errorf("%w: %v", ErrInvalidSource, verificationErr)
		}
	}
	return m.enqueue(ctx, project, operation, workerPayload{Project: project, FileJob: work, RuntimeFingerprint: adoptionRuntimeFingerprint(runtime)})
}

func validateSelfFileConfiguration(preview Preview, runtime []container.InspectResponse, directory string) error {
	service, found := preview.model.Services["nox-yard"]
	if !found {
		return fmt.Errorf("%w: Yard's service must remain present for independent recovery", ErrInvalidSource)
	}
	if value := service.Environment["NOX_DATA_DIR"]; value != nil && *value != directory {
		return fmt.Errorf("%w: moving Yard's data directory requires a separate host migration", ErrInvalidSource)
	}
	for _, item := range runtime {
		if item.Config == nil || item.Config.Labels["com.docker.compose.service"] != "nox-yard" {
			continue
		}
		for _, destination := range []string{"/data", "/var/run/docker.sock"} {
			preserved := false
			for _, old := range item.Mounts {
				if old.Destination != destination {
					continue
				}
				for _, next := range service.Volumes {
					if next.Target == destination && next.Type == "bind" && next.Source == old.Source && !next.ReadOnly {
						preserved = true
					}
				}
			}
			if !preserved {
				return fmt.Errorf("%w: Yard's persistent data and Docker socket mounts must be preserved for independent recovery", ErrInvalidSource)
			}
		}
	}
	return nil
}

func filePreview(source FileSnapshot) Preview {
	var model composeModel
	decoded, _ := decodedComposeModel(source.Resolved)
	_ = json.Unmarshal(decoded, &model)
	return Preview{Name: source.Name, model: model, resolved: source.Resolved}
}

func (m *Manager) executeFileDeployment(ctx context.Context, job store.Job, payload workerPayload) error {
	work := payload.FileJob
	preview := scopedFilePreview(work.Source, work.Service)
	selected := serviceRuntime(work.Runtime, work.Service)
	fail := func(message string) error {
		outcome, rollback := "failed", "not_needed"
		inspection, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		state, stateErr := jobs.State(inspection, job)
		cancel()
		if stateErr != nil || state.HelpersRunning {
			outcome, rollback = "recovery_required", "not_performed"
		} else if work.Mutation {
			_ = m.data.JobProgress(job.ID, job.Owner, "rolling_back", nil, nil)
			recovery, stop := context.WithTimeout(context.Background(), 3*time.Minute)
			var restoreErr error
			if work.DatabaseBackup != "" {
				if _, err := os.Stat(work.DatabaseBackup); err == nil {
					cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
					if err != nil {
						restoreErr = err
					} else {
						current, err := m.runtime(recovery, job.ProjectName)
						if err != nil {
							restoreErr = err
						}
						for _, item := range serviceRuntime(current, work.Service) {
							if _, err := cli.ContainerStop(recovery, item.ID, client.ContainerStopOptions{}); err != nil {
								restoreErr = err
							}
						}
						_ = cli.Close()
					}
					if restoreErr == nil {
						directory := m.data.Directory()
						if err := m.data.Close(); err != nil {
							restoreErr = err
						} else {
							restoreErr = store.RestoreRollbackSnapshot(directory, work.DatabaseBackup)
							reopened, openErr := store.Open(directory)
							if openErr != nil {
								stop()
								return fmt.Errorf("Yard database could not be reopened after rollback; inspect the retained snapshot")
							}
							m.data = reopened
						}
					}
				}
			}
			if restoreErr == nil {
				restoreErr = recreate.RestoreServiceRuntime(recovery, job.ProjectName, work.Service, work.Runtime)
			}
			if restoreErr != nil {
				outcome, rollback = "recovery_required", "failed"
				message += " Previous containers could not be restored; inspect the private recovery snapshot."
			} else {
				outcome, rollback = "rolled_back", "restored"
				message += " Previous images and container configuration were restored. Application data writes were not undone."
			}
			stop()
		}
		if err := m.data.FinishJob(job.ID, job.Owner, "failed", outcome, message, rollback); err != nil {
			return err
		}
		if outcome != "recovery_required" {
			m.cleanupDeployment(job, payload.Deployment)
		}
		return nil
	}
	current, err := m.readFileSnapshot(ctx, job.ProjectName)
	if err != nil || current.Fingerprint != work.Source.Fingerprint {
		return fail("Host configuration changed before execution; review the current files.")
	}
	runtime, err := m.runtime(ctx, job.ProjectName)
	if err != nil || adoptionRuntimeFingerprint(runtime) != adoptionRuntimeFingerprint(work.Runtime) || !sameRunningState(runtime, work.Runtime) {
		return fail("Existing containers changed before execution; review again.")
	}
	pullDefinition := string(work.Source.Resolved)
	if work.Self && job.Operation == "update" {
		pullDefinition, err = verifiedYardPull(work, preview)
		if err != nil {
			return fail(err.Error())
		}
	}
	if job.Operation != "pull" {
		payload.Deployment = &deploymentSnapshot{Runtime: selected}
		if err := m.retainDeploymentImages(ctx, job, payload); err != nil {
			return fail("Previous images could not be retained for recovery.")
		}
	}
	if job.Operation == "pull" || job.Operation == "update" {
		if err := m.saveDeployment(job, payload, "pulling"); err != nil {
			return err
		}
		if err := runResolvedCompose(ctx, job.ProjectName, work.Source.Directory, pullDefinition, "pull"); err != nil {
			return fail("Image pull failed: " + err.Error())
		}
	}
	defaults, err := m.fileImageDefaults(ctx, preview, work.VerifiedYard)
	if err != nil {
		return fail(err.Error())
	}
	images, targets := imageTargets(preview, defaults)
	work.Targets = targets
	if err := m.data.JobProgress(job.ID, job.Owner, "preparing", nil, images); err != nil {
		return err
	}
	if job.Operation == "pull" {
		return m.data.FinishJob(job.ID, job.Owner, "succeeded", "cached", "", "")
	}
	if job.ScheduleKey != "" {
		if !fileAutomaticRuntime(job.ProjectName, preview, runtime) {
			return m.data.FinishJob(job.ID, job.Owner, "succeeded", "skipped", "Project is stopped, unstable or protected.", "")
		}
		if reason, err := m.data.UpdateCandidate(job, images); err != nil {
			return err
		} else if reason != "" {
			return m.data.FinishJob(job.ID, job.Owner, "succeeded", "skipped", reason, "")
		}
	}
	if job.Operation == "update" && unchangedImages(preview.model, runtime, targets) {
		if err := m.data.FinishJob(job.ID, job.Owner, "succeeded", "unchanged", "", ""); err != nil {
			return err
		}
		m.cleanupDeployment(job, payload.Deployment)
		return nil
	}
	// Explicit Recreate applies saved configuration even with identical images.
	work.Definition, err = resolvedDefinition(preview, work.Source.Directory, targets)
	if err == nil && work.VerifiedYard != nil {
		work.Definition, err = definitionPlatforms(work.Definition, map[string]string{"nox-yard": "linux/" + work.VerifiedYard.Architecture})
	}
	if err != nil {
		return fail("Resolved configuration could not be prepared.")
	}
	if err := m.assessServicePorts(ctx, job.ProjectName, work.Service, preview.model); err != nil {
		return fail(err.Error())
	}
	current, err = m.readFileSnapshot(ctx, job.ProjectName)
	if err != nil || current.Fingerprint != work.Source.Fingerprint {
		return fail("Host configuration changed during preparation; review again.")
	}
	if work.Service != "" {
		if err := recreate.CheckServiceIsolation(ctx, selected); err != nil {
			return fail("Service dependencies changed before recreation; review again.")
		}
	}
	if job.ScheduleKey != "" {
		runtime, err = m.runtime(ctx, job.ProjectName)
		if err != nil || !fileAutomaticRuntime(job.ProjectName, preview, runtime) {
			return fail("Project stopped or became unstable before automatic replacement.")
		}
		if reason, err := m.data.UpdateCandidate(job, images); err != nil {
			return err
		} else if reason != "" {
			return m.data.FinishJob(job.ID, job.Owner, "succeeded", "skipped", reason, "")
		}
	}
	work.Mutation = true
	if err := m.saveDeployment(job, payload, "replacing"); err != nil {
		return err
	}
	// The independent managed worker survives replacement of the Yard itself.
	// A database snapshot protects an unsuccessful Yard image/schema upgrade.
	if work.Self {
		work.DatabaseBackup = filepath.Join(m.data.Directory(), ".configuration-"+job.ID+".sqlite")
		if err := m.saveDeployment(job, payload, "replacing"); err != nil {
			return err
		}
		cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
		if err != nil {
			return fail("Docker is unavailable.")
		}
		for _, item := range selected {
			if _, err = cli.ContainerStop(ctx, item.ID, client.ContainerStopOptions{}); err != nil {
				break
			}
		}
		_ = cli.Close()
		if err != nil {
			return fail("The Yard could not be stopped for independent recreation.")
		}
		if err := m.data.BackupForSelfUpdate(work.DatabaseBackup); err != nil {
			return fail("The Yard database snapshot could not be created.")
		}
	}
	args := fileCreateArgs(job.Operation, work.Service)
	if err := runResolvedCompose(ctx, job.ProjectName, work.Source.Directory, work.Definition, args...); err != nil {
		return fail("Recreate failed: " + err.Error())
	}
	wanted, existed := map[string]bool{}, map[string]bool{}
	for _, item := range selected {
		service := item.Config.Labels["com.docker.compose.service"]
		existed[service] = true
		if item.State.Running {
			wanted[service] = true
		}
	}
	for service := range expectedServices(preview.model) {
		if !existed[service] {
			wanted[service] = true
		}
	}
	services := []string{}
	for service := range wanted {
		if _, exists := preview.model.Services[service]; exists {
			services = append(services, service)
		}
	}
	sort.Strings(services)
	if len(services) > 0 {
		args = []string{"up", "-d", "--no-recreate", "--no-build", "--pull", "never"}
		if work.Service != "" {
			args = append(args, "--no-deps")
		}
		args = append(args, services...)
		if err := runResolvedCompose(ctx, job.ProjectName, work.Source.Directory, work.Definition, args...); err != nil {
			return fail("Starting recreated services failed: " + err.Error())
		}
	}
	if err := m.saveDeployment(job, payload, "verifying"); err != nil {
		return err
	}
	verification := fileServiceVerification(preview, selected, work.Service)
	if len(expectedServices(verification.model)) > 0 {
		if err := m.verify(ctx, job.ProjectName, verification); err != nil {
			return fail("Recreate verification failed: " + err.Error())
		}
	}
	if err := m.verifyFileDeployment(ctx, job.ProjectName, verification.model, targets, work); err != nil {
		return fail("Recreated images did not match the prepared local images.")
	}
	if err := m.data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
		return err
	}
	if work.DatabaseBackup != "" {
		_ = os.Remove(work.DatabaseBackup)
	}
	m.cleanupDeployment(job, payload.Deployment)
	if m.changes != nil {
		m.changes.Notify(inventory.Change{Inventory: true})
	}
	return nil
}

// Verify before journaling/launch: independent workers intentionally have no
// network. The job binds this candidate to the assessed source/runtime snapshot.
func (m *Manager) prepareSignedYardUpdate(ctx context.Context, work *fileDeployment, preview Preview) error {
	service, ok := preview.model.Services["nox-yard"]
	if !ok {
		return fmt.Errorf("Yard update blocked: Yard service is missing")
	}
	var platform string
	if raw := service.raw["platform"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &platform)
	}
	if platform != "" && platform != "linux/"+runtime.GOARCH && !(runtime.GOARCH == "arm64" && platform == "linux/arm64/v8") {
		return fmt.Errorf("Yard update blocked: configured platform differs from the running architecture")
	}
	verify := m.verifyYardRelease
	if verify == nil {
		verify = yardrelease.Trusted
	}
	candidate, err := verify(ctx, service.Image, runtime.GOARCH)
	if err != nil {
		return err
	}
	work.VerifiedYard = &candidate
	return nil
}

// Pin only the private pull definition. Host files and source labels retain the
// selected channel/version; actual creation later uses the verified local ID.
func verifiedYardPull(work *fileDeployment, preview Preview) (string, error) {
	candidate := work.VerifiedYard
	if candidate == nil || candidate.Source != preview.model.Services["nox-yard"].Image || candidate.Architecture != runtime.GOARCH || !yardrelease.ValidDigest(candidate.IndexDigest) || !yardrelease.ValidDigest(candidate.ManifestDigest) || !yardrelease.ValidDigest(candidate.ImageID) {
		return "", fmt.Errorf("Yard update blocked: the job has no matching verified release")
	}
	var model map[string]any
	if err := json.Unmarshal(preview.resolved, &model); err != nil {
		return "", err
	}
	services, ok := model["services"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("Yard update blocked: invalid resolved services")
	}
	yard, ok := services["nox-yard"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("Yard update blocked: invalid resolved Yard service")
	}
	yard["image"] = candidate.Reference()
	yard["platform"] = "linux/" + runtime.GOARCH
	delete(yard, "pull_policy")
	definition, err := json.Marshal(model)
	if err != nil {
		return "", err
	}
	return string(definition), nil
}

func (m *Manager) fileImageDefaults(ctx context.Context, preview Preview, verified *yardrelease.Candidate) (map[string]imageDefaults, error) {
	defaults := map[string]imageDefaults{}
	for service := range expectedServices(preview.model) {
		reference := preview.model.Services[service].Image
		if _, exists := defaults[reference]; exists {
			continue
		}
		inspectionReference := reference
		if verified != nil && reference == preview.model.Services["nox-yard"].Image {
			inspectionReference = verified.Reference()
		}
		value, err := m.imageDefaults(ctx, inspectionReference)
		if err != nil || value.ID == "" {
			return nil, fmt.Errorf("A required local image is unavailable. Run Pull before Recreate.")
		}
		if verified != nil && reference == preview.model.Services["nox-yard"].Image {
			platform := "linux/" + verified.Architecture
			if !verified.MatchesImageID(value.ID) || (value.Platform != platform && !(verified.Architecture == "arm64" && value.Platform == platform+"/v8")) {
				return nil, fmt.Errorf("Yard update blocked: pulled image does not match the verified release or platform.")
			}
		}
		defaults[reference] = value
	}
	return defaults, nil
}

func fileVerification(preview Preview, previous []container.InspectResponse) Preview {
	existed, wanted := map[string]bool{}, map[string]bool{}
	for _, item := range previous {
		if item.Config != nil && item.State != nil {
			service := item.Config.Labels["com.docker.compose.service"]
			existed[service] = true
			wanted[service] = wanted[service] || item.State.Running
		}
	}
	for service, definition := range preview.model.Services {
		if existed[service] && !wanted[service] && !expectedServices(preview.model)[service].oneshot {
			zero := 0
			definition.Scale = &zero
			definition.Deploy.Replicas = nil
			preview.model.Services[service] = definition
		}
	}
	return preview
}

func fileAutomaticRuntime(name string, preview Preview, runtime []container.InspectResponse) bool {
	self := name == "nox-yard"
	for _, item := range runtime {
		if item.Config != nil && item.Config.Labels["com.docker.compose.service"] == "nox-yard" {
			self = true
		}
	}
	if !self {
		return automaticRuntime(preview, runtime)
	}
	return len(runtime) == 1 && runtime[0].State != nil && runtime[0].State.Running && !runtime[0].State.Restarting && !runtime[0].State.Paused && runtime[0].State.Health != nil && runtime[0].State.Health.Status == "healthy"
}
func (m *Manager) AssessFileAutomatic(ctx context.Context, name string) error {
	source, err := m.readFileSnapshot(ctx, name)
	if err != nil {
		return err
	}
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return err
	}
	if !fileAutomaticRuntime(name, filePreview(source), runtime) {
		return ErrConflict
	}
	return nil
}
func (m *Manager) ScheduledFileUpdate(ctx context.Context, name string) (store.Job, error) {
	if err := m.AssessFileAutomatic(ctx, name); err != nil {
		return store.Job{}, err
	}
	return m.FileOperation(ctx, name, "update", "")
}
