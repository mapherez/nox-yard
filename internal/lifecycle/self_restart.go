package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

func (m *Manager) launchSelfRestart(ctx context.Context, id string, failures ...Failure) error {
	if job, ok := jobs.FromContext(ctx); ok && m.data != nil {
		inspected, err := m.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		self := inspected.Container
		if self.ID != id || self.State == nil || !self.State.Running || self.Config == nil || self.Config.Healthcheck == nil {
			return errors.New("NoX Yard must be running with a healthcheck to restart safely")
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
			return err
		}
		payload["selfID"] = id
		if len(failures) > 0 {
			payload["partialFailure"] = "true"
		}
		for _, failure := range failures {
			if errors.Is(failure.Cause, context.Canceled) || errors.Is(failure.Cause, context.DeadlineExceeded) {
				payload["interrupted"] = "true"
			}
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := m.data.JobPayload(job.ID, job.Owner, string(encoded)); err != nil {
			return err
		}
		return jobs.Launch(ctx, m.data, job, "restart-worker")
	}
	listed, err := m.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	for _, item := range listed.Items {
		if isHelper(item.Labels) && item.State == container.StateRunning {
			return ErrConflict
		}
	}
	inspected, err := m.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	self := inspected.Container
	if self.ID != id || self.State == nil || !self.State.Running || self.Config == nil || self.Config.Healthcheck == nil {
		return errors.New("NoX Yard must be running with a healthcheck to restart safely")
	}
	var socket *mount.Mount
	for _, source := range self.Mounts {
		if source.Destination == "/var/run/docker.sock" && source.Source != "" && source.RW && source.Type == mount.TypeBind {
			socket = &mount.Mount{Type: mount.TypeBind, Source: source.Source, Target: source.Destination}
			break
		}
	}
	if socket == nil {
		return errors.New("NoX Yard needs a writable Docker socket mount for its restart helper")
	}
	created, err := m.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:  self.Image,
			Cmd:    []string{"nox-yard", "restart-worker", id},
			Labels: map[string]string{"nox-yard.role": "self-restart-worker"},
		},
		HostConfig: &container.HostConfig{
			AutoRemove:  true,
			NetworkMode: "none",
			Mounts:      []mount.Mount{*socket},
			SecurityOpt: []string{"no-new-privileges:true"},
		},
	})
	if err != nil {
		return fmt.Errorf("create restart helper: %w", err)
	}
	if _, err := m.client.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = m.client.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start restart helper: %w", err)
	}
	return nil
}

func RunDurableRestartWorker(data *store.Store, id, owner string) error {
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
	payload := map[string]string{}
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return err
	}
	if payload["selfID"] == "" {
		return errors.New("restart target identity is missing")
	}
	if err := data.JobProgress(id, owner, "restarting", nil, nil); err != nil {
		return err
	}
	if err := RunRestartWorker(payload["selfID"]); err != nil {
		return data.FinishJob(id, owner, "failed", "recovery_required", "NoX Yard restart failed or its healthcheck did not become healthy. Inspect the container on the host and acknowledge recovery before retrying.", "")
	}
	images, imageErr := jobs.Images(ctx, job.Resources, false)
	if imageErr == nil {
		_ = data.JobProgress(id, owner, "verifying", nil, images)
	}
	status, outcome, message := restartOutcome(payload)
	return data.FinishJob(id, owner, status, outcome, message, "")
}

func restartOutcome(payload map[string]string) (string, string, string) {
	if payload["interrupted"] == "true" {
		return "failed", "recovery_required", "Yard restarted successfully, but another group action was interrupted. Inspect the other targets and acknowledge recovery before retrying."
	}
	if payload["partialFailure"] == "true" {
		return "failed", "failed", "Yard restarted successfully, but another requested group action failed. Inspect the other targets before retrying."
	}
	return "succeeded", "verified", ""
}

// RunRestartWorker runs independently of the web server, from the exact image
// currently used by NoX Yard. Durable helpers are cleaned after their saved result.
func RunRestartWorker(id string) error {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return err
	}
	defer cli.Close()
	// Give the API request time to return before the web server is restarted.
	time.Sleep(3 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{}); err != nil {
		return fmt.Errorf("restart NoX Yard: %w", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		inspected, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			return errors.New("NoX Yard disappeared during restart")
		}
		if err != nil {
			return err
		}
		state := inspected.Container.State
		if state == nil || !state.Running {
			return errors.New("NoX Yard stopped during restart")
		}
		if state.Health != nil && state.Health.Status == container.Healthy {
			return nil
		}
		if state.Health != nil && state.Health.Status == container.Unhealthy {
			return errors.New("NoX Yard is unhealthy after restart")
		}
		select {
		case <-ctx.Done():
			return errors.New("timed out waiting for NoX Yard healthcheck")
		case <-ticker.C:
		}
	}
}
