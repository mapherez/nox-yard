// Package jobs owns independent worker transport and interruption observation.
// It never replays Docker mutations on behalf of an interrupted operation.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

var ErrLaunchUncertain = errors.New("worker launch needs reconciliation")

// Launch uses the running Yard image and only its state and Docker socket mounts.
// Source/env payloads stay in SQLite, never in container arguments or labels.
func Launch(ctx context.Context, data *store.Store, job store.Job, command string) error {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	inspected, err := cli.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return errors.New("independent operations require the Yard container and its persistent state mount")
	}
	self := inspected.Container
	if !strings.HasPrefix(self.ID, hostname) || self.Config == nil {
		return errors.New("cannot identify the current Yard container")
	}
	directory, err := filepath.Abs(data.Directory())
	if err != nil {
		return err
	}
	var mounts []mount.Mount
	for _, destination := range []string{directory, "/var/run/docker.sock"} {
		found := false
		for _, item := range self.Mounts {
			if item.Destination != destination || !item.RW || (item.Type != mount.TypeBind && item.Type != mount.TypeVolume) {
				continue
			}
			source := item.Source
			if item.Type == mount.TypeVolume {
				source = item.Name
			}
			target := "/data"
			if destination == "/var/run/docker.sock" {
				target = destination
			}
			mounts = append(mounts, mount.Mount{Type: item.Type, Source: source, Target: target})
			found = true
			break
		}
		if !found {
			return errors.New("independent operations require writable persistent state and Docker socket mounts")
		}
	}
	env := []string{"NOX_DATA_DIR=/data", "NOX_HELPER_IMAGE=" + self.Image, "NOX_JOB_ID=" + job.ID}
	if home := os.Getenv("NOX_HOST_HOME"); home != "" {
		env = append(env, "NOX_HOST_HOME="+home)
	}
	name := "nox-yard-job-" + job.ID
	if err := data.SetJobWorker(job.ID, job.Owner, name); err != nil {
		return err
	}
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{Name: name,
		Config: &container.Config{Image: self.Image, Entrypoint: []string{"nox-yard"}, Cmd: []string{command, job.ID, job.Owner}, Env: env,
			Labels: map[string]string{"nox-yard.role": "operation-worker", "nox-yard.job": job.ID, "nox-yard.owner": job.Owner}},
		HostConfig: &container.HostConfig{NetworkMode: "none", Mounts: mounts, SecurityOpt: []string{"no-new-privileges:true"}},
	})
	if err != nil {
		return fmt.Errorf("%w: create worker", ErrLaunchUncertain)
	}
	if err := data.SetJobWorker(job.ID, job.Owner, created.ID); err != nil {
		return fmt.Errorf("%w: register worker", ErrLaunchUncertain)
	}
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("%w: start worker", ErrLaunchUncertain)
	}
	return nil
}

func ValidateWorker(ctx context.Context, job store.Job, owner string) error {
	if job.Status != "running" || owner == "" || job.Owner != owner || job.WorkerID == "" {
		return store.ErrJobChanged
	}
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	inspected, err := cli.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	item := inspected.Container
	if item.Config == nil || item.ID != job.WorkerID || item.Config.Labels["nox-yard.job"] != job.ID || item.Config.Labels["nox-yard.owner"] != owner {
		return store.ErrJobChanged
	}
	return nil
}

type WorkerState struct {
	Running        bool
	Exists         bool
	HelpersRunning bool
}

// State checks identities and child helpers too: killing the parent does not
// imply that a detached Compose operation has stopped touching the target.
func State(ctx context.Context, job store.Job) (WorkerState, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return WorkerState{}, err
	}
	defer cli.Close()
	result := WorkerState{}
	if job.WorkerID != "" {
		inspected, err := cli.ContainerInspect(ctx, job.WorkerID, client.ContainerInspectOptions{})
		if err != nil && !errdefs.IsNotFound(err) {
			return result, err
		}
		if err == nil {
			item := inspected.Container
			legacyUpdate := job.Domain == "self-update" && job.Owner == "" && item.Config != nil && item.Config.Labels["nox-yard.role"] == "self-update-worker" && strings.TrimPrefix(item.Name, "/") == "nox-yard-update-"+job.ID
			if !legacyUpdate && (item.Config == nil || item.Config.Labels["nox-yard.job"] != job.ID || item.Config.Labels["nox-yard.owner"] != job.Owner) {
				return result, store.ErrJobChanged
			}
			result.Exists = true
			result.Running = item.State != nil && (item.State.Running || item.State.Restarting)
		}
	}
	listed, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: client.Filters{"label": {"nox-yard.job=" + job.ID: true}}})
	if err != nil {
		return result, err
	}
	for _, item := range listed.Items {
		if item.ID != job.WorkerID && item.State == container.StateRunning {
			result.HelpersRunning = true
		}
	}
	return result, nil
}

func Cleanup(ctx context.Context, job store.Job) error {
	if job.WorkerID == "" {
		return nil
	}
	state, err := State(ctx, job)
	if err != nil {
		return err
	}
	if state.Running || state.HelpersRunning {
		return store.ErrOperationConflict
	}
	if !state.Exists {
		return nil
	}
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ContainerRemove(ctx, job.WorkerID, client.ContainerRemoveOptions{})
	if errdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// Observe never starts workers or reissues commands. A late final database write
// wins over an earlier snapshot; CAS protects reconciliation from that race.
func Observe(ctx context.Context, data *store.Store, job store.Job, verify func(context.Context, store.Job) error) error {
	state, err := State(ctx, job)
	if err != nil {
		return err
	}
	return observeState(ctx, data, job, state, verify)
}

func observeState(ctx context.Context, data *store.Store, job store.Job, state WorkerState, verify func(context.Context, store.Job) error) error {
	if state.Running || state.HelpersRunning {
		return nil
	}
	if job.Status != "running" {
		return nil
	}
	// Allow a submitting web process time to finish launch registration.
	if job.StartedAt == 0 && time.Now().Unix()-job.CreatedAt < 30 {
		return nil
	}
	if verify != nil && (job.Stage == "verifying" || job.Stage == "committing") {
		if err := verify(ctx, job); err == nil {
			current, found, err := data.Job(job.ID)
			if err != nil {
				return err
			}
			if !found {
				return store.ErrJobChanged
			}
			if current.Status != "running" {
				return nil
			}
			return data.FinishJob(job.ID, job.Owner, "succeeded", "reconciled", "", "")
		}
	}
	return data.FinishJob(job.ID, job.Owner, "failed", "recovery_required",
		"Worker exited before recording a verified result. Inspect the target, source files and retained resources on the host, then acknowledge recovery before retrying. No operation was replayed.", job.Rollback)
}
