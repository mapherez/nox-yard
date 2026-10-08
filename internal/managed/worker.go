package managed

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
)

type workerPayload struct {
	Project            store.ManagedProject
	RuntimeFingerprint string
	FilesFingerprint   string
	RemoveVolumes      bool
}

func (m *Manager) enqueue(ctx context.Context, project store.ManagedProject, operation string, payload workerPayload) (store.ManagedJob, error) {
	job, err := store.NewJob("compose:"+project.Name, "managed", operation, nil)
	if err != nil {
		return job, err
	}
	job.ProjectName = project.Name
	encoded, err := json.Marshal(payload)
	if err != nil {
		return job, err
	}
	job.Payload = string(encoded)
	// Directory-less legacy mutations fail locally, without launching a worker.
	invalidDir := project.ProjectDir == "" && (operation == "start" || operation == "restart" || operation == "update")
	if !invalidDir {
		runtime, err := m.runtime(ctx, project.Name)
		if err != nil {
			return job, err
		}
		job.SourceImages = []store.ImageIdentity{}
		for _, item := range runtime {
			job.Resources = append(job.Resources, "container:"+item.ID)
			identity := store.ImageIdentity{ContainerID: item.ID, ImageID: item.Image}
			if item.Config != nil {
				identity.Service = item.Config.Labels["com.docker.compose.service"]
				if identity.Service == "nox-yard" {
					job.Resources = append(job.Resources, "yard:self")
				}
			}
			if item.State != nil {
				identity.StartedAt = item.State.StartedAt
			}
			job.SourceImages = append(job.SourceImages, identity)
		}
		if operation == "sync" && payload.RuntimeFingerprint != "" && adoptionRuntimeFingerprint(runtime) != payload.RuntimeFingerprint {
			return store.Job{}, ErrConflict
		}
	}
	if err := m.data.CreateJob(job); err != nil {
		return store.ManagedJob{}, err
	}
	if m.changes != nil {
		m.changes.Notify(inventory.Change{Inventory: true})
	}
	if invalidDir {
		_ = m.data.FinishJob(job.ID, job.Owner, "failed", "failed", "Saved host project directory is missing; directory recovery is required before this operation.", "")
		current, _, err := m.data.Job(job.ID)
		return current, err
	}
	if operation == "new" || operation == "copy" {
		if err := m.data.SaveManagedProject(project); err != nil {
			_ = m.data.FinishJob(job.ID, job.Owner, "failed", "failed", "Project configuration could not be saved.", "")
			return store.ManagedJob{}, err
		}
	}
	if err := m.launch(ctx, m.data, job, "managed-worker"); err != nil {
		// A transport timeout can happen after Docker created or started the worker.
		// Preserve ownership for observation instead of allowing a duplicate launch.
		if !errors.Is(err, jobs.ErrLaunchUncertain) {
			_ = m.data.FinishJob(job.ID, job.Owner, "failed", "failed", "Independent worker could not be launched. Check the Yard state/socket mounts.", "")
		}
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
	m := &Manager{data: data, runtime: readProjectRuntime, imageDefaults: readImageDefaults, adoptionFiles: adoptionFiles}
	return m.execute(ctx, job)
}

func (m *Manager) execute(ctx context.Context, job store.Job) error {
	fail := func(message string) error {
		outcome := "failed"
		inspect, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		state, stateErr := jobs.State(inspect, job)
		cancel()
		if ctx.Err() != nil || stateErr != nil || state.HelpersRunning {
			outcome = "recovery_required"
			message += " Operation completion is uncertain. Inspect retained resources and acknowledge recovery before retrying."
		}
		rollback := ""
		if job.Operation == "update" || job.Operation == "sync" {
			message += " Automatic rollback was not performed."
			rollback = "not_performed"
		}
		return m.data.FinishJob(job.ID, job.Owner, "failed", outcome, message, rollback)
	}
	progress := func(stage string) error { return m.data.JobProgress(job.ID, job.Owner, stage, nil, nil) }
	var payload workerPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return fail("Saved operation payload could not be read.")
	}
	project := payload.Project
	variables, envFiles := map[string]string{}, map[string]string{}
	if json.Unmarshal([]byte(project.VariablesJSON), &variables) != nil || json.Unmarshal([]byte(project.EnvFilesJSON), &envFiles) != nil {
		return fail("Saved project environment could not be read.")
	}
	if job.Operation == "adopt" {
		current, err := m.runtime(ctx, project.Name)
		if err != nil || adoptionRuntimeFingerprint(current) != payload.RuntimeFingerprint {
			return fail("Existing project changed before adoption; review again.")
		}
		if err := progress("writing_files"); err != nil {
			return err
		}
		if _, err := m.adoptionFiles(ctx, project.ProjectDir, project.YAML, envFiles, payload.FilesFingerprint, true); err != nil {
			return fail("Project files could not be adopted without overwriting existing files; review again.")
		}
		if err := progress("committing"); err != nil {
			return err
		}
		if err := m.data.CommitManagedJob(job, &project, false, "verified"); err != nil {
			return fail("Adopted files were preserved, but project metadata could not be saved.")
		}
		return nil
	}
	needsVerification := job.Operation == "new" || job.Operation == "copy" || job.Operation == "sync" || job.Operation == "start" || job.Operation == "restart" || job.Operation == "update"
	var preview Preview
	if needsVerification || job.Operation == "pull" {
		var err error
		preview, err = Validate(ctx, project.Name, Source{YAML: project.YAML}, variables, envFiles, project.ProjectDir)
		if err != nil {
			return fail("Saved Compose configuration could not be validated.")
		}
	}
	if job.Operation == "new" || job.Operation == "copy" || job.Operation == "sync" {
		if err := progress("writing_files"); err != nil {
			return err
		}
		if err := writeHostCompose(ctx, project.ProjectDir, project.YAML, envFiles, job.Operation != "sync"); err != nil {
			return fail("Host project files could not be written. Inspect the source directory before retrying.")
		}
	}
	if job.Operation == "new" || job.Operation == "copy" || job.Operation == "sync" || job.Operation == "update" || job.Operation == "pull" {
		if err := progress("pulling"); err != nil {
			return err
		}
		if err := composeCommand(ctx, project.Name, project.YAML, variables, envFiles, project.ProjectDir, "pull"); err != nil {
			return fail("Image pull failed.")
		}
	}
	if needsVerification || job.Operation == "pull" {
		images := []store.ImageIdentity{}
		for name := range expectedServices(preview.model) {
			service := preview.model.Services[name]
			if job.Operation == "restart" {
				for _, prior := range job.SourceImages {
					if prior.Service == name {
						images = append(images, prior)
					}
				}
				continue
			}
			value, err := m.imageDefaults(ctx, service.Image)
			if err != nil {
				return fail("Target image identity could not be inspected.")
			}
			images = append(images, store.ImageIdentity{Service: name, ImageID: value.ID})
		}
		if err := m.data.JobProgress(job.ID, job.Owner, "replacing", nil, images); err != nil {
			return err
		}
		if job.Operation == "pull" {
			return m.data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", "")
		}
	} else if err := progress("executing"); err != nil {
		return err
	}
	args := []string{"up", "-d", "--no-build"}
	switch job.Operation {
	case "restart":
		args = []string{"restart"}
	case "stop":
		args = []string{"stop"}
	case "remove":
		args = []string{"down"}
		if payload.RemoveVolumes {
			args = append(args, "--volumes")
		}
	case "update":
		args = append(args, "--force-recreate")
	}
	if err := composeCommand(ctx, project.Name, project.YAML, variables, envFiles, project.ProjectDir, args...); err != nil {
		return fail("Docker Compose could not complete the operation.")
	}
	if err := progress("verifying"); err != nil {
		return err
	}
	if needsVerification {
		if err := m.verify(ctx, project.Name, preview); err != nil {
			return fail("Verification failed: " + err.Error() + ".")
		}
	} else {
		runtime, err := m.runtime(ctx, project.Name)
		if err != nil {
			return fail("Docker outcome could not be inspected.")
		}
		for _, item := range runtime {
			if job.Operation == "remove" || (item.State != nil && item.State.Running) {
				return fail("Containers remain active after the operation.")
			}
		}
	}
	if err := progress("committing"); err != nil {
		return err
	}
	if job.Operation == "remove" {
		if err := m.data.CommitManagedJob(job, nil, true, "verified"); err != nil {
			return fail("Containers were removed, but project metadata could not be deleted.")
		}
		return nil
	} else if job.Operation == "new" || job.Operation == "copy" || job.Operation == "sync" {
		if err := m.data.CommitManagedJob(job, &project, false, "verified"); err != nil {
			return fail("Containers were verified, but project metadata could not be saved.")
		}
		return nil
	}
	return m.data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", "")
}

// Reconcile verifies a completed command without replaying it. Metadata writes
// are idempotent; source/host-file transactionality and rollback remain C3.
func (m *Manager) Reconcile(ctx context.Context, job store.Job) error {
	var payload workerPayload
	if json.Unmarshal([]byte(job.Payload), &payload) != nil {
		return store.ErrJobChanged
	}
	project := payload.Project
	if job.Operation == "adopt" {
		return store.ErrJobChanged
	} // Never repeat create-only file work after uncertain interruption.
	if job.Operation == "stop" || job.Operation == "remove" {
		runtime, err := m.runtime(ctx, project.Name)
		if err != nil {
			return err
		}
		for _, item := range runtime {
			if job.Operation == "remove" || item.State == nil || item.State.Running {
				return store.ErrJobChanged
			}
		}
		return m.data.CommitManagedJob(job, nil, job.Operation == "remove", "reconciled")
	}
	variables, envFiles := map[string]string{}, map[string]string{}
	if json.Unmarshal([]byte(project.VariablesJSON), &variables) != nil || json.Unmarshal([]byte(project.EnvFilesJSON), &envFiles) != nil {
		return store.ErrJobChanged
	}
	preview, err := Validate(ctx, project.Name, Source{YAML: project.YAML}, variables, envFiles, project.ProjectDir)
	if err != nil {
		return err
	}
	runtime, err := m.runtime(ctx, project.Name)
	if err != nil {
		return err
	}
	targets := map[string]string{}
	for _, image := range job.TargetImages {
		targets[image.Service] = image.ImageID
	}
	if len(targets) == 0 {
		return store.ErrJobChanged
	}
	for _, item := range runtime {
		if item.Config == nil || targets[item.Config.Labels["com.docker.compose.service"]] != item.Image {
			return store.ErrJobChanged
		}
	}
	if err := m.verify(ctx, project.Name, preview); err != nil {
		return err
	}
	if job.Operation == "new" || job.Operation == "copy" || job.Operation == "sync" {
		return m.data.CommitManagedJob(job, &project, false, "reconciled")
	}
	return nil
}
