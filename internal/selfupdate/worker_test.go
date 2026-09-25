package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
)

func TestReplacementPreservesComposeRuntime(t *testing.T) {
	old := container.InspectResponse{
		Config: &container.Config{
			Image:       imageReference,
			Hostname:    "old-container-id",
			Cmd:         []string{"nox-yard"},
			Healthcheck: &container.HealthConfig{Test: []string{"CMD-SHELL", "wget http://127.0.0.1:8080/healthz"}},
			Labels: map[string]string{
				"com.docker.compose.project": "nox-yard",
				"com.docker.compose.image":   "sha256:old",
			},
		},
		HostConfig: &container.HostConfig{Binds: []string{"/host/data:/data", "/var/run/docker.sock:/var/run/docker.sock"}},
		Mounts: []container.MountPoint{
			{Type: mount.TypeBind, Source: "/host/data", Destination: "/data", RW: true},
			{Type: mount.TypeBind, Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true},
		},
		NetworkSettings: &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{
			"nox-yard_default": {Aliases: []string{"nox-yard"}},
		}},
	}
	target := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	config, host, networks, err := replacementConfig(old, target)
	if err != nil {
		t.Fatal(err)
	}
	if config.Image != target || config.Hostname != "" || config.Labels["com.docker.compose.image"] != target {
		t.Fatalf("replacement image or identity was not updated: %#v", config)
	}
	if old.Config.Labels["com.docker.compose.image"] != "sha256:old" {
		t.Fatal("the original container configuration was mutated")
	}
	if len(host.Binds) != 2 || networks.EndpointsConfig["nox-yard_default"].Aliases[0] != "nox-yard" {
		t.Fatal("Compose mounts or network aliases were lost")
	}
}

func TestWorkerMatchesContainerIDAndImageIDSeparately(t *testing.T) {
	containerID := "container-id"
	imageID := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	job := store.SelfUpdateJob{OldImageID: imageID}
	old := container.InspectResponse{
		ID:         containerID,
		Image:      imageID,
		State:      &container.State{Running: true},
		Config:     &container.Config{Labels: map[string]string{"com.docker.compose.service": "nox-yard"}},
		HostConfig: &container.HostConfig{},
	}
	if err := validateWorkerContainer(job, containerID, old); err != nil {
		t.Fatalf("a distinct container ID and image ID must be accepted: %v", err)
	}
	old.Image = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := validateWorkerContainer(job, containerID, old); err == nil {
		t.Fatal("a changed container image must be rejected")
	}
}

func TestSQLiteSnapshotRestoresUpdateState(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("container rollback runs only on Linux")
	}
	directory := t.TempDir()
	data, err := store.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.SetSelfUpdateSettings(true, 15); err != nil {
		t.Fatal(err)
	}
	jobID := "aaaaaaaaaaaaaaaaaaaaaaaa"
	if err := data.CreateSelfUpdateJob(store.SelfUpdateJob{
		ID: jobID, OldImageID: "sha256:old", TargetImageID: "sha256:new", TargetDigest: "sha256:manifest",
	}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(directory, ".self-update-"+jobID+".sqlite")
	if err := data.BackupForSelfUpdate(backup); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishSelfUpdateJob(jobID, "failed", "new version failed"); err != nil {
		t.Fatal(err)
	}
	if err := data.Close(); err != nil {
		t.Fatal(err)
	}
	if err := restoreDatabase(directory, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("snapshot was not consumed: %v", err)
	}
	restored, err := store.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	job, err := restored.SelfUpdateJob(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "updating" {
		t.Fatalf("snapshot did not restore the pre-update job: %s", job.Status)
	}
}
