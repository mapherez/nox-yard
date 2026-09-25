package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// RunWorker executes in a separate container made from the exact previous image ID.
func RunWorker(data *store.Store, dataDir, jobID, oldID string) (result error) {
	if len(jobID) != 24 {
		return errors.New("invalid self-update job ID")
	}
	for _, character := range jobID {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return errors.New("invalid self-update job ID")
		}
	}
	job, err := data.SelfUpdateJob(jobID)
	if err != nil {
		return err
	}
	if job.Status != "updating" || job.OldImageID != oldID {
		return errors.New("self-update job does not match the requested container")
	}
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		_ = data.FinishSelfUpdateJob(jobID, "failed", err.Error())
		return err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	oldResult, err := cli.ContainerInspect(ctx, oldID, client.ContainerInspectOptions{})
	if err != nil {
		return failBeforeReplacement(data, cli, job, err)
	}
	old := oldResult.Container
	if old.Image != job.OldImageID || old.State == nil || !old.State.Running ||
		old.Config == nil || old.HostConfig == nil || old.Config.Labels["com.docker.compose.service"] != "nox-yard" {
		err = errors.New("original NoX Yard container changed before update worker started")
		return failBeforeReplacement(data, cli, job, err)
	}
	config, host, networking, err := replacementConfig(old, job.TargetImageID)
	if err != nil {
		return failBeforeReplacement(data, cli, job, err)
	}
	name := strings.TrimPrefix(old.Name, "/")
	backupName := name + "-rollback-" + jobID
	backupPath := filepath.Join(dataDir, ".self-update-"+jobID+".sqlite")
	var newID string
	backupCreated := false
	committed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			result = fmt.Errorf("self-update worker panicked: %v", recovered)
		}
		if committed {
			return
		}
		// Recovery must continue even if the update deadline expired.
		recovery, stop := context.WithTimeout(context.Background(), 3*time.Minute)
		defer stop()
		restore := func() error { return nil }
		if backupCreated {
			restore = func() error {
				if err := data.Close(); err != nil {
					return err
				}
				if err := restoreDatabase(dataDir, backupPath); err != nil {
					return err
				}
				var err error
				data, err = store.Open(dataDir)
				return err
			}
		}
		if err := rollback(recovery, cli, oldID, newID, name, restore); err != nil {
			log.Printf("Self-update rollback failed: %v", err)
			result = fmt.Errorf("%v; rollback failed: %w", result, err)
		}
		if data != nil {
			if err := data.FinishSelfUpdateJob(jobID, "failed", result.Error()); err != nil {
				result = fmt.Errorf("%v; save update failure: %w", result, err)
			}
		}
	}()

	if _, err = cli.ContainerRename(ctx, oldID, client.ContainerRenameOptions{NewName: backupName}); err != nil {
		return fmt.Errorf("reserve original container: %w", err)
	}
	if _, err = cli.ContainerStop(ctx, oldID, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("stop original container: %w", err)
	}
	if err := data.BackupForSelfUpdate(backupPath); err != nil {
		return err
	}
	backupCreated = true
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name, Config: config, HostConfig: host, NetworkingConfig: networking,
	})
	if err != nil {
		return fmt.Errorf("create replacement: %w", err)
	}
	newID = created.ID
	if _, err = cli.ContainerStart(ctx, newID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start replacement: %w", err)
	}
	if err = waitHealthy(ctx, cli, newID); err != nil {
		return fmt.Errorf("replacement healthcheck: %w", err)
	}
	if err = data.FinishSelfUpdateJob(jobID, "succeeded", ""); err != nil {
		return fmt.Errorf("replacement is healthy but saving the result failed: %w", err)
	}
	committed = true
	cleanup, stopCleanup := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCleanup()
	if _, err = cli.ContainerRemove(cleanup, oldID, client.ContainerRemoveOptions{}); err != nil {
		log.Printf("Self-update succeeded but original container cleanup failed: %v", err)
	}
	if err := os.Remove(backupPath); err != nil {
		log.Printf("Self-update succeeded but SQLite snapshot cleanup failed: %v", err)
	}
	return nil
}

func failBeforeReplacement(data *store.Store, cli *client.Client, job store.SelfUpdateJob, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: job.OldImageID, Target: imageReference}); err != nil {
		cause = fmt.Errorf("%v; restore previous latest tag: %w", cause, err)
	}
	if err := data.FinishSelfUpdateJob(job.ID, "failed", cause.Error()); err != nil {
		return fmt.Errorf("%v; save update failure: %w", cause, err)
	}
	return cause
}

func replacementConfig(old container.InspectResponse, imageID string) (*container.Config, *container.HostConfig, *network.NetworkingConfig, error) {
	if !validDigest(imageID) || old.Config == nil || old.HostConfig == nil || old.NetworkSettings == nil {
		return nil, nil, nil, errors.New("original container configuration is incomplete")
	}
	if !hasExpectedHealthcheck(old.Config.Healthcheck) {
		return nil, nil, nil, errors.New("original container has no /healthz healthcheck")
	}
	if _, err := requiredMounts(old); err != nil {
		return nil, nil, nil, err
	}
	if len(old.NetworkSettings.Networks) != 1 {
		return nil, nil, nil, errors.New("self-update requires exactly one Docker network")
	}
	config := *old.Config
	config.Image = imageID
	config.Hostname = "" // Allow Docker to assign the replacement's own container ID.
	config.Labels = make(map[string]string, len(old.Config.Labels))
	for key, value := range old.Config.Labels {
		config.Labels[key] = value
	}
	config.Labels["com.docker.compose.image"] = imageID
	host := *old.HostConfig
	host.AutoRemove = false
	networking := &network.NetworkingConfig{EndpointsConfig: make(map[string]*network.EndpointSettings)}
	for name, endpoint := range old.NetworkSettings.Networks {
		if endpoint == nil {
			return nil, nil, nil, errors.New("original Docker network settings are missing")
		}
		networking.EndpointsConfig[name] = &network.EndpointSettings{
			IPAMConfig: endpoint.IPAMConfig,
			Links:      endpoint.Links,
			Aliases:    endpoint.Aliases,
			DriverOpts: endpoint.DriverOpts,
			GwPriority: endpoint.GwPriority,
		}
	}
	return &config, &host, networking, nil
}

func hasExpectedHealthcheck(health *container.HealthConfig) bool {
	return health != nil && len(health.Test) == 2 && health.Test[0] == "CMD-SHELL" &&
		strings.Contains(health.Test[1], "http://127.0.0.1:8080/healthz")
}

func waitHealthy(ctx context.Context, cli *client.Client, id string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		inspected, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		state := inspected.Container.State
		if state == nil || !state.Running {
			if state != nil {
				return fmt.Errorf("container exited with code %d: %s", state.ExitCode, state.Error)
			}
			return errors.New("container did not start")
		}
		if state.Health == nil {
			return errors.New("container did not expose a Docker healthcheck")
		}
		switch state.Health.Status {
		case "healthy":
			return nil
		case "unhealthy":
			return errors.New("/healthz reported unhealthy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func rollback(ctx context.Context, cli *client.Client, oldID, newID, originalName string, restore func() error) error {
	var failures []string
	if newID == "" {
		if candidate, err := cli.ContainerInspect(ctx, originalName, client.ContainerInspectOptions{}); err == nil && candidate.Container.ID != oldID {
			newID = candidate.Container.ID // ContainerCreate may have succeeded before its response was lost.
		}
	}
	if newID != "" {
		if _, err := cli.ContainerRemove(ctx, newID, client.ContainerRemoveOptions{Force: true}); err != nil {
			failures = append(failures, "remove replacement: "+err.Error())
		}
	}
	if err := restore(); err != nil {
		failures = append(failures, "restore database: "+err.Error())
	}
	inspected, err := cli.ContainerInspect(ctx, oldID, client.ContainerInspectOptions{})
	if err != nil {
		failures = append(failures, "inspect original: "+err.Error())
	} else {
		if strings.TrimPrefix(inspected.Container.Name, "/") != originalName {
			if _, err := cli.ContainerRename(ctx, oldID, client.ContainerRenameOptions{NewName: originalName}); err != nil {
				failures = append(failures, "restore original name: "+err.Error())
			}
		}
		if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: inspected.Container.Image, Target: imageReference}); err != nil {
			failures = append(failures, "restore previous latest tag: "+err.Error())
		}
		if inspected.Container.State == nil || !inspected.Container.State.Running {
			if _, err := cli.ContainerStart(ctx, oldID, client.ContainerStartOptions{}); err != nil {
				failures = append(failures, "restart original: "+err.Error())
			} else if err := waitHealthy(ctx, cli, oldID); err != nil {
				failures = append(failures, "original healthcheck: "+err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func restoreDatabase(dataDir, backupPath string) error {
	path := filepath.Join(dataDir, "nox-yard.sqlite")
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(backupPath, path); err != nil {
		return fmt.Errorf("replace SQLite database with rollback snapshot: %w", err)
	}
	return nil
}
