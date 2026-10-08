package selfupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

func TestLegacyWorkerFailureRequiresRecovery(t *testing.T) {
	directory := t.TempDir()
	data, err := store.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	id := "aaaaaaaaaaaaaaaaaaaaaaaa"
	if err := data.CreateSelfUpdateJob(store.SelfUpdateJob{ID: id, OldImageID: "old", TargetImageID: "new"}); err != nil {
		t.Fatal(err)
	}
	// Pre-C2 workers only write their dedicated table; rollback is unconfirmed.
	db, err := sql.Open("sqlite", filepath.Join(directory, "nox-yard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE self_update_jobs SET status='failed',error='legacy failure' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	New(data, "test").reconcileInterruptedJob(context.Background())
	job, found, err := data.Job(id)
	if err != nil || !found || job.Outcome != "recovery_required" {
		t.Fatalf("legacy failure released ownership: %+v %v", job, err)
	}
	retry, _ := store.NewJob("container:yard", "engine", "restart", []string{"yard:self"})
	if err := data.CreateJob(retry); !errors.Is(err, store.ErrOperationConflict) {
		t.Fatal("legacy failure allowed retry", err)
	}
}

func TestDedicatedWorkerRegistrationAndUncertainStart(t *testing.T) {
	for _, failStart := range []bool{false, true} {
		t.Run(map[bool]string{false: "started", true: "uncertain"}[failStart], func(t *testing.T) {
			data, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			id := "aaaaaaaaaaaaaaaaaaaaaaaa"
			if err := data.CreateSelfUpdateJob(store.SelfUpdateJob{ID: id, OldImageID: "old", TargetImageID: "new"}); err != nil {
				t.Fatal(err)
			}
			var config container.Config
			removed := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/containers/create"):
					if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
						t.Error(err)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(201)
					_, _ = w.Write([]byte(`{"Id":"worker-id","Warnings":[]}`))
				case strings.HasSuffix(r.URL.Path, "/start"):
					if failStart {
						http.Error(w, `{"message":"response lost"}`, 500)
					} else {
						w.WriteHeader(204)
					}
				case r.Method == http.MethodDelete:
					removed = true
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected Docker call %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			cli, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.52"))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			self := container.InspectResponse{ID: "old-container", Image: "old", Mounts: []container.MountPoint{{Type: mount.TypeBind, Source: "/state", Destination: "/data", RW: true}, {Type: mount.TypeBind, Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true}}}
			err = launchWorker(context.Background(), cli, self, id, data)
			if failStart && !errors.Is(err, jobs.ErrLaunchUncertain) {
				t.Fatalf("lost uncertain launch: %v", err)
			}
			if !failStart && err != nil {
				t.Fatal(err)
			}
			job, _, err := data.Job(id)
			if err != nil || job.Status != "running" || job.WorkerID != "worker-id" || job.Stage != "launching" || config.Labels["nox-yard.owner"] != job.Owner || config.Labels["nox-yard.job"] != id || removed {
				t.Fatalf("worker ownership lost: %+v %v", job, err)
			}
			if err := data.ClaimJob(id, job.Owner); err != nil {
				t.Fatal(err)
			}
			if err := data.ClaimJob(id, job.Owner); !errors.Is(err, store.ErrJobChanged) {
				t.Fatal("duplicate self-update admitted", err)
			}
		})
	}
}
