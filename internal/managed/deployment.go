package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// This record is private (never serialized by job/history adapters). Keep full
// inspect data and resolved definitions for diagnosis after uncertain exit.
type deploymentSnapshot struct {
	Previous       *store.ManagedProject
	Runtime        []container.InspectResponse
	Definition     string
	Candidate      string
	RetainedImages []string
	Mutation       bool
	FileCommit     bool
}

func projectEnvironment(project store.ManagedProject) (map[string]string, map[string]string, error) {
	variables, files := map[string]string{}, map[string]string{}
	if json.Unmarshal([]byte(project.VariablesJSON), &variables) != nil || json.Unmarshal([]byte(project.EnvFilesJSON), &files) != nil {
		return nil, nil, fmt.Errorf("saved project environment could not be read")
	}
	return variables, files, nil
}

func resolvedDefinition(preview Preview, directory string, images map[string]string) (string, error) {
	var model map[string]any
	if err := json.Unmarshal(preview.resolved, &model); err != nil {
		return "", err
	}
	services, ok := model["services"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing services")
	}
	for name, value := range services {
		service := value.(map[string]any)
		if image := images[name]; image != "" {
			labels, _ := service["labels"].(map[string]any)
			if labels == nil {
				labels = map[string]any{}
			}
			labels[imageidentity.ReferenceLabel] = service["image"]
			service["labels"] = labels
			service["image"] = image
			service["pull_policy"] = "never"
		}
		for _, value := range asArray(service["volumes"]) {
			mount, ok := value.(map[string]any)
			if !ok || mount["type"] != "bind" {
				continue
			}
			source, _ := mount["source"].(string)
			if !path.IsAbs(source) {
				mount["source"] = path.Join(directory, source)
			}
		}
	}
	encoded, err := json.Marshal(model)
	return string(encoded), err
}

func asArray(value any) []any { result, _ := value.([]any); return result }

func definitionPlatforms(definition string, platforms map[string]string) (string, error) {
	var model map[string]any
	if err := json.Unmarshal([]byte(definition), &model); err != nil {
		return "", err
	}
	services := model["services"].(map[string]any)
	for name, platform := range platforms {
		if platform != "" {
			services[name].(map[string]any)["platform"] = platform
		}
	}
	encoded, err := json.Marshal(model)
	return string(encoded), err
}

func imageTargets(preview Preview, defaults map[string]imageDefaults) ([]store.ImageIdentity, map[string]string) {
	images, byService := []store.ImageIdentity{}, map[string]string{}
	for service := range expectedServices(preview.model) {
		id := defaults[preview.model.Services[service].Image].ID
		byService[service] = id
		images = append(images, store.ImageIdentity{Service: service, ImageID: id, Platform: defaults[preview.model.Services[service].Image].Platform})
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Service < images[j].Service })
	return images, byService
}

func unchangedImages(model composeModel, runtime []container.InspectResponse, targets map[string]string) bool {
	counts := map[string]int{}
	for _, item := range runtime {
		if item.Config == nil || targets[item.Config.Labels["com.docker.compose.service"]] != item.Image {
			return false
		}
		counts[item.Config.Labels["com.docker.compose.service"]]++
	}
	for service, expected := range expectedServices(model) {
		if counts[service] != expected.replicas || targets[service] == "" {
			return false
		}
	}
	return len(counts) == len(targets) && len(targets) > 0
}

// Rollback is bounded to the same assessed source/runtime contract as adoption.
// Reject drift and mixed replica state before replacing anything. Anonymous
// volumes and unsupported settings must never be silently discarded.
func (m *Manager) snapshotDeployment(ctx context.Context, project store.ManagedProject, runtime []container.InspectResponse) (*deploymentSnapshot, error) {
	variables, files, err := projectEnvironment(project)
	if err != nil {
		return nil, err
	}
	preview, err := Validate(ctx, project.Name, Source{YAML: project.YAML}, variables, files, project.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("previous Compose configuration cannot be validated; restore its saved source before updating")
	}
	defaults, images, states := map[string]imageDefaults{}, map[string]string{}, map[string]bool{}
	platforms := map[string]string{}
	// Compare against each actually deployed image, even if its mutable tag has
	// moved or a prior Yard operation stored an immutable image in Config.Image.
	for _, item := range runtime {
		if item.Config == nil || item.State == nil || item.State.Paused || item.State.Restarting {
			return nil, fmt.Errorf("previous container state is incomplete, paused or restarting; stabilize the project first")
		}
		name := item.Config.Labels["com.docker.compose.service"]
		service, found := preview.model.Services[name]
		if !found {
			return nil, fmt.Errorf("runtime contains services absent from the saved source; reconcile the project before updating")
		}
		if image, found := images[name]; found && (image != item.Image || states[name] != item.State.Running) {
			return nil, fmt.Errorf("replicas have mixed images or running states; reconcile the service before updating")
		}
		images[name], states[name] = item.Image, item.State.Running
		value, err := m.imageDefaults(ctx, item.Image)
		if err != nil {
			return nil, fmt.Errorf("previous images are unavailable; rollback cannot be guaranteed")
		}
		if item.Config.Labels[imageidentity.ReferenceLabel] != "" {
			if service.Labels == nil {
				service.Labels = map[string]string{}
			}
			service.Labels[imageidentity.ReferenceLabel] = service.Image
		}
		service.Image = item.Config.Image
		preview.model.Services[name] = service
		defaults[service.Image] = value
		platforms[name] = value.Platform
		if item.ImageManifestDescriptor != nil && item.ImageManifestDescriptor.Platform != nil {
			platform := item.ImageManifestDescriptor.Platform
			platforms[name] = platform.OS + "/" + platform.Architecture
			if platform.Variant != "" {
				platforms[name] += "/" + platform.Variant
			}
		}
	}
	if len(runtime) > 0 {
		changes, err := compareAdoption(project.Name, project.ProjectDir, preview.model, runtime, defaults)
		if err != nil {
			return nil, fmt.Errorf("rollback assessment failed: %w", err)
		}
		if len(changes) > 0 {
			return nil, fmt.Errorf("runtime differs from saved source (%s); reconcile the project before updating", strings.Join(changes, "; "))
		}
	}
	definition, err := resolvedDefinition(preview, project.ProjectDir, images)
	if err == nil {
		definition, err = definitionPlatforms(definition, platforms)
	}
	return &deploymentSnapshot{Previous: &project, Runtime: runtime, Definition: definition}, err
}

func (m *Manager) saveDeployment(job store.Job, payload workerPayload, stage string) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := m.data.JobPayload(job.ID, job.Owner, string(encoded)); err != nil {
		return err
	}
	return m.data.JobProgress(job.ID, job.Owner, stage, nil, nil)
}

func (m *Manager) retainDeploymentImages(ctx context.Context, job store.Job, payload workerPayload) error {
	snapshot := payload.Deployment
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	seen := map[string]bool{}
	for _, item := range snapshot.Runtime {
		if seen[item.Image] {
			continue
		}
		seen[item.Image] = true
		// Two references also prevent Yard's non-forced Engine image-ID removal
		// from deleting a now-unused rollback image between replacement and verify.
		for reference := 0; reference < 2; reference++ {
			tag := "nox-yard-rollback/" + job.ID + ":image-" + fmt.Sprint(len(snapshot.RetainedImages))
			// Journal each reference before creation, including a lost tag response.
			snapshot.RetainedImages = append(snapshot.RetainedImages, tag)
			if err := m.saveDeployment(job, payload, "preparing"); err != nil {
				return err
			}
			if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: item.Image, Target: tag}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) cleanupDeployment(job store.Job, snapshot *deploymentSnapshot) {
	if snapshot == nil || len(snapshot.RetainedImages) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := m.CleanupDeployment(ctx, store.Job{Payload: mustPayload(snapshot), Outcome: "verified"})
	if err != nil {
		_ = m.data.JobCleanupError(job.ID, "Result saved; temporary rollback image references remain. Inspect nox-yard-rollback/"+job.ID+" on the Docker host.")
	}
}

func mustPayload(snapshot *deploymentSnapshot) string {
	encoded, _ := json.Marshal(workerPayload{Deployment: snapshot})
	return string(encoded)
}

// CleanupDeployment is retried by the observer after a terminal worker exits.
// Recovery-required snapshots/images remain available until host review.
func (m *Manager) CleanupDeployment(ctx context.Context, job store.Job) error {
	if job.Outcome == "recovery_required" {
		return nil
	}
	var payload workerPayload
	if json.Unmarshal([]byte(job.Payload), &payload) != nil {
		return store.ErrJobChanged
	}
	if payload.Deployment == nil || len(payload.Deployment.RetainedImages) == 0 {
		return nil
	}
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err == nil {
		defer cli.Close()
		for _, tag := range payload.Deployment.RetainedImages {
			if _, removeErr := cli.ImageRemove(ctx, tag, client.ImageRemoveOptions{}); removeErr != nil && !strings.Contains(strings.ToLower(removeErr.Error()), "no such image") {
				err = removeErr
			}
		}
	}
	return err
}

func (m *Manager) deploy(ctx context.Context, job store.Job, payload workerPayload) error {
	project := payload.Project
	initial := job.Operation == "new" || job.Operation == "copy"
	snapshot := &deploymentSnapshot{}
	payload.Deployment = snapshot
	finishFailure := func(message string) error {
		outcome, rollback := "failed", "not_needed"
		inspect, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr := jobs.State(inspect, job)
		cancel()
		if stateErr != nil || state.HelpersRunning {
			outcome, rollback = "recovery_required", "not_performed"
			message += " Completion is uncertain; inspect retained resources and the private job snapshot before acknowledging recovery."
		} else if !initial && (snapshot.Mutation || snapshot.FileCommit) {
			_ = m.data.JobProgress(job.ID, job.Owner, "rolling_back", nil, nil)
			recovery, stop := context.WithTimeout(context.Background(), 3*time.Minute)
			err := m.rollbackDeployment(recovery, project, snapshot)
			stop()
			if err != nil {
				outcome, rollback = "recovery_required", "failed"
				message += " Rollback failed: " + err.Error() + ". Inspect the retained snapshot/images before acknowledging recovery."
			} else {
				outcome, rollback = "rolled_back", "restored"
				message += " Previous images, configuration and running/stopped state were restored. Application data writes were not undone."
			}
		} else if initial && snapshot.Mutation {
			message += " Initial deployment has partial resources; inspect the project, then use Start to retry or Remove without deleting volumes to clean up."
		}
		err := m.data.FinishJob(job.ID, job.Owner, "failed", outcome, message, rollback)
		if err == nil && outcome != "recovery_required" {
			m.cleanupDeployment(job, snapshot)
		}
		return err
	}
	if err := m.saveDeployment(job, payload, "preparing"); err != nil {
		return err
	}
	variables, envFiles, err := projectEnvironment(project)
	if err != nil {
		return finishFailure("Saved project environment could not be read.")
	}
	preview, err := Validate(ctx, project.Name, Source{YAML: project.YAML}, variables, envFiles, project.ProjectDir)
	if err != nil {
		return finishFailure("Saved Compose configuration could not be validated.")
	}
	if job.Operation != "pull" {
		if initial {
			if err := hostProjectFiles(ctx, project.ProjectDir, nil, project, "prepare"); err != nil {
				return finishFailure("New project files already exist or cannot be prepared; review the host directory.")
			}
		} else {
			previous, found, err := m.data.ManagedProject(project.Name)
			if err != nil || !found {
				return finishFailure("Previous project metadata is unavailable.")
			}
			if err := hostProjectFiles(ctx, project.ProjectDir, &previous, project, "check"); err != nil {
				return finishFailure("Host project files changed or are missing; restore matching saved source/environment files and review again.")
			}
			runtime, err := m.runtime(ctx, project.Name)
			if err != nil || !matchesSourceImages(runtime, job.SourceImages) || (payload.RuntimeFingerprint != "" && adoptionRuntimeFingerprint(runtime) != payload.RuntimeFingerprint) {
				return finishFailure("Existing containers changed before execution; review again.")
			}
			prepared, err := m.snapshotDeployment(ctx, previous, runtime)
			if err != nil {
				return finishFailure("Cannot safely replace this project: " + err.Error() + ".")
			}
			snapshot = prepared
			payload.Deployment = snapshot
			if err := m.saveDeployment(job, payload, "preparing"); err != nil {
				return err
			}
			if err := m.retainDeploymentImages(ctx, job, payload); err != nil {
				return finishFailure("Previous images could not be retained for rollback.")
			}
		}
	}
	pullDefinition, err := resolvedDefinition(preview, project.ProjectDir, nil)
	if err != nil {
		return finishFailure("Validated Compose definition could not be prepared.")
	}
	if err := m.saveDeployment(job, payload, "pulling"); err != nil {
		return err
	}
	if project.ProjectDir == "" && job.Operation == "pull" {
		err = composeCommand(ctx, project.Name, project.YAML, variables, envFiles, "", "pull")
	} else {
		err = runResolvedCompose(ctx, project.Name, project.ProjectDir, pullDefinition, "pull")
	}
	if err != nil {
		message := err.Error()
		if project.ProjectDir == "" {
			message = safeDockerFailure(message)
		}
		return finishFailure("Image pull failed: " + message)
	}
	defaults := map[string]imageDefaults{}
	for name := range expectedServices(preview.model) {
		service := preview.model.Services[name]
		value, err := m.imageDefaults(ctx, service.Image)
		if err != nil || value.ID == "" {
			return finishFailure("Target image identity could not be inspected for the current Docker platform.")
		}
		defaults[service.Image] = value
	}
	images, targets := imageTargets(preview, defaults)
	if err := m.data.JobProgress(job.ID, job.Owner, "preparing", nil, images); err != nil {
		return err
	}
	if job.Operation == "pull" {
		return m.data.FinishJob(job.ID, job.Owner, "succeeded", "cached", "", "")
	}
	if !initial {
		current, err := m.runtime(ctx, project.Name)
		if err != nil || adoptionRuntimeFingerprint(current) != adoptionRuntimeFingerprint(snapshot.Runtime) || !sameRunningState(current, snapshot.Runtime) {
			return finishFailure("Existing containers changed during pull; review again.")
		}
		if err := hostProjectFiles(ctx, project.ProjectDir, snapshot.Previous, project, "check"); err != nil {
			return finishFailure("Host project files changed during pull; review again.")
		}
		if job.Operation == "update" && unchangedImages(preview.model, current, targets) {
			if err := m.data.FinishJob(job.ID, job.Owner, "succeeded", "unchanged", "", ""); err != nil {
				return err
			}
			m.cleanupDeployment(job, snapshot)
			return nil
		}
	}
	snapshot.Candidate, err = resolvedDefinition(preview, project.ProjectDir, targets)
	if err == nil {
		platforms := map[string]string{}
		for name := range targets {
			platforms[name] = defaults[preview.model.Services[name].Image].Platform
		}
		snapshot.Candidate, err = definitionPlatforms(snapshot.Candidate, platforms)
	}
	if err != nil {
		return finishFailure("Immutable target definition could not be prepared.")
	}
	if initial {
		snapshot.FileCommit = true
		if err := m.saveDeployment(job, payload, "writing_files"); err != nil {
			return err
		}
		if err := hostProjectFiles(ctx, project.ProjectDir, nil, project, "commit"); err != nil {
			// No previous application exists, but a partial file write must retain
			// ownership rather than pretending it is a clean deployment failure.
			return m.data.FinishJob(job.ID, job.Owner, "failed", "recovery_required", "Initial project file commit was interrupted; inspect the saved journal and host source before retrying.", "not_performed")
		}
	}
	snapshot.Mutation = true
	if err := m.saveDeployment(job, payload, "replacing"); err != nil {
		return err
	}
	if err := runResolvedCompose(ctx, project.Name, project.ProjectDir, snapshot.Candidate, "up", "-d", "--no-build", "--pull", "never", "--remove-orphans"); err != nil {
		return finishFailure("Replacement failed: " + err.Error())
	}
	if err := m.saveDeployment(job, payload, "verifying"); err != nil {
		return err
	}
	if err := m.verify(ctx, project.Name, preview); err != nil {
		return finishFailure("Verification failed: " + err.Error() + ".")
	}
	if err := m.verifyDeploymentImages(ctx, project.Name, preview.model, targets); err != nil {
		return finishFailure("Replacement image identities did not match the verified targets.")
	}
	if job.Operation == "sync" {
		snapshot.FileCommit = true
		if err := m.saveDeployment(job, payload, "committing_files"); err != nil {
			return err
		}
		if err := hostProjectFiles(ctx, project.ProjectDir, snapshot.Previous, project, "commit"); err != nil {
			return finishFailure("Verified source could not be committed to the host files.")
		}
	}
	if err := m.saveDeployment(job, payload, "committing"); err != nil {
		return err
	}
	if err := m.data.CommitManagedJob(job, &project, false, "verified"); err != nil {
		return finishFailure("Verified project metadata could not be committed.")
	}
	m.cleanupDeployment(job, snapshot)
	return nil
}

func matchesSourceImages(runtime []container.InspectResponse, images []store.ImageIdentity) bool {
	if len(runtime) != len(images) {
		return false
	}
	byID := map[string]string{}
	for _, image := range images {
		byID[image.ContainerID] = image.ImageID
	}
	for _, item := range runtime {
		image, found := byID[item.ID]
		if !found || image == "" || image != item.Image {
			return false
		}
	}
	return true
}

func sameRunningState(a, b []container.InspectResponse) bool {
	states := map[string]bool{}
	for _, item := range b {
		if item.State == nil {
			return false
		}
		states[item.ID] = item.State.Running
	}
	for _, item := range a {
		value, found := states[item.ID]
		if !found || item.State == nil || value != item.State.Running {
			return false
		}
	}
	return len(a) == len(b)
}

func (m *Manager) verifyDeploymentImages(ctx context.Context, name string, model composeModel, targets map[string]string) error {
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return err
	}
	if !unchangedImages(model, runtime, targets) {
		return store.ErrJobChanged
	}
	return nil
}

func (m *Manager) rollbackDeployment(ctx context.Context, project store.ManagedProject, snapshot *deploymentSnapshot) error {
	if snapshot.Previous == nil {
		return fmt.Errorf("previous source snapshot is missing")
	}
	variables, files, err := projectEnvironment(*snapshot.Previous)
	if err != nil {
		return err
	}
	preview, err := Validate(ctx, project.Name, Source{YAML: snapshot.Previous.YAML}, variables, files, project.ProjectDir)
	if err != nil {
		return fmt.Errorf("previous source could not be verified")
	}
	wanted, counts, images := map[string]bool{}, map[string]int{}, map[string]string{}
	for _, item := range snapshot.Runtime {
		name := item.Config.Labels["com.docker.compose.service"]
		wanted[name] = item.State.Running
		counts[name]++
		images[name] = item.Image
	}
	if snapshot.FileCommit {
		if err := hostProjectFiles(ctx, project.ProjectDir, snapshot.Previous, project, "restore"); err != nil {
			return fmt.Errorf("host source files could not be restored safely")
		}
	}
	if snapshot.Mutation {
		if len(snapshot.Runtime) == 0 {
			if err := runResolvedCompose(ctx, project.Name, project.ProjectDir, snapshot.Candidate, "down", "--remove-orphans"); err != nil {
				return err
			}
		} else {
			// Create the old containers without starting stopped services or running
			// dependencies accidentally. Then start only originally running services.
			if err := runResolvedCompose(ctx, project.Name, project.ProjectDir, snapshot.Definition, "up", "--no-start", "--no-build", "--pull", "never", "--remove-orphans"); err != nil {
				return err
			}
			running := map[string]bool{}
			for _, item := range snapshot.Runtime {
				if item.State.Running {
					running[item.Config.Labels["com.docker.compose.service"]] = true
				}
			}
			current, err := m.runtime(ctx, project.Name)
			if err != nil {
				return fmt.Errorf("restored containers could not be inspected")
			}
			cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
			if err != nil {
				return fmt.Errorf("Docker could not restore stopped services")
			}
			defer cli.Close()
			for _, item := range current {
				if item.Config == nil || item.State == nil {
					return fmt.Errorf("restored container state is incomplete")
				}
				name := item.Config.Labels["com.docker.compose.service"]
				if wanted[name] {
					continue
				}
				if item.State.Running {
					if _, err := cli.ContainerStop(ctx, item.ID, client.ContainerStopOptions{}); err != nil {
						return fmt.Errorf("previous stopped state could not be restored")
					}
				}
				if expectedServices(preview.model)[name].oneshot && (item.State.Status != "exited" || item.State.ExitCode != 0) {
					running[name] = true
				}
			}
			if len(running) > 0 {
				args := []string{"start"}
				for service := range running {
					args = append(args, service)
				}
				if err := runResolvedCompose(ctx, project.Name, project.ProjectDir, snapshot.Definition, args...); err != nil {
					return err
				}
			}
		}
	}
	for name := range counts {
		if !wanted[name] && !expectedServices(preview.model)[name].oneshot {
			service := preview.model.Services[name]
			zero := 0
			service.Scale = &zero
			service.Deploy.Replicas = nil
			preview.model.Services[name] = service
		}
	}
	if len(expectedServices(preview.model)) > 0 {
		if err := m.verify(ctx, project.Name, preview); err != nil {
			return err
		}
	}
	current, err := m.runtime(ctx, project.Name)
	if err != nil {
		return fmt.Errorf("restored runtime could not be inspected")
	}
	if len(snapshot.Runtime) == 0 {
		if len(current) != 0 {
			return fmt.Errorf("new containers remain after rollback")
		}
		return nil
	}
	for _, item := range current {
		if item.Config == nil || item.State == nil {
			return fmt.Errorf("restored state is incomplete")
		}
		name := item.Config.Labels["com.docker.compose.service"]
		if item.Image != images[name] || item.State.Running != wanted[name] {
			return fmt.Errorf("previous image or running/stopped state was not restored")
		}
		counts[name]--
	}
	for _, count := range counts {
		if count != 0 {
			return fmt.Errorf("previous replica count was not restored")
		}
	}
	if _, err := m.snapshotDeployment(ctx, *snapshot.Previous, current); err != nil {
		return fmt.Errorf("restored runtime configuration does not match the previous source: %w", err)
	}
	return hostProjectFiles(ctx, project.ProjectDir, snapshot.Previous, *snapshot.Previous, "verified")
}

func (m *Manager) reconcileDeployment(ctx context.Context, job store.Job, payload workerPayload) error {
	snapshot, project := payload.Deployment, payload.Project
	if snapshot.Candidate == "" || !snapshot.Mutation {
		return store.ErrJobChanged
	}
	variables, files, err := projectEnvironment(project)
	if err != nil {
		return err
	}
	preview, err := Validate(ctx, project.Name, Source{YAML: project.YAML}, variables, files, project.ProjectDir)
	if err != nil {
		return err
	}
	targets := map[string]string{}
	for _, image := range job.TargetImages {
		targets[image.Service] = image.ImageID
	}
	if err := m.verifyDeploymentImages(ctx, project.Name, preview.model, targets); err != nil {
		return err
	}
	if err := m.verify(ctx, project.Name, preview); err != nil {
		return err
	}
	if err := hostProjectFiles(ctx, project.ProjectDir, snapshot.Previous, project, "verified"); err != nil {
		if job.Operation != "sync" || snapshot.FileCommit {
			return store.ErrJobChanged
		}
		// Healthy replacement completed before worker loss. Finish a still-pristine
		// file commit; never replay a partially written journal or the replacement.
		snapshot.FileCommit = true
		if err := m.saveDeployment(job, payload, "committing_files"); err != nil {
			return err
		}
		if err := hostProjectFiles(ctx, project.ProjectDir, snapshot.Previous, project, "commit"); err != nil {
			return err
		}
	}
	if err := m.data.CommitManagedJob(job, &project, false, "reconciled"); err != nil {
		return err
	}
	m.cleanupDeployment(job, snapshot)
	return nil
}
