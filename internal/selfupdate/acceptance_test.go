//go:build linux

package selfupdate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/auth"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/client"
)

// The harness builds an otherwise identical fixture binary with only the
// restore-tag constant redirected to a private local fixture tag. Never run
// this opt-in test against the production reference or an existing Yard.
func TestSelfUpdateDockerAcceptance(t *testing.T) {
	reference := os.Getenv("NOX_SELF_FIXTURE_REFERENCE")
	if reference == "" {
		t.Skip("requires scripts/smoke-self-update.py")
	}
	if imageReference != reference || !strings.HasPrefix(reference, "self-update-smoke-") {
		t.Fatal("fixture binary must use an isolated restore tag")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	current := os.Getenv("NOX_SELF_FIXTURE_CONTAINER")
	label := os.Getenv("NOX_SELF_FIXTURE_NAME")
	original, err := cli.ContainerInspect(ctx, current, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if original.Container.Config.Labels["nox-yard.acceptance"] != label {
		t.Fatal("target is not our fixture")
	}
	data, err := store.Open("/data")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("private-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := data.CreateAdmin("owner", hash); err != nil {
		t.Fatal(err)
	}
	token := sha256.Sum256([]byte("private-fixture-session"))
	tokenHash := base64.RawURLEncoding.EncodeToString(token[:])
	if err := data.CreateSession(tokenHash, "fixture-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	data.Close()
	for _, variant := range []string{"GOOD", "BAD"} {
		old, err := cli.ContainerInspect(ctx, current, client.ContainerInspectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		target, err := cli.ImageInspect(ctx, os.Getenv("NOX_SELF_FIXTURE_"+variant))
		if err != nil {
			t.Fatal(err)
		}
		id, err := randomID()
		if err != nil {
			t.Fatal(err)
		}
		data, err = store.Open("/data")
		if err != nil {
			t.Fatal(err)
		}
		job := store.SelfUpdateJob{ID: id, OldImageID: old.Container.Image, TargetImageID: target.ID, TargetDigest: target.ID, ContainerID: current, ProjectName: label}
		if err := data.CreateSelfUpdateJob(job); err != nil {
			t.Fatal(err)
		}
		if err := launchWorker(ctx, cli, old.Container, id, data); err != nil {
			t.Fatal(err)
		}
		operation, found, err := data.Job(id)
		if err != nil || !found {
			t.Fatal("worker registration missing")
		}
		ledger, err := os.OpenFile("/fixture/worker-ids", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			t.Fatal(err)
		}
		ledger.WriteString(operation.WorkerID + "\n")
		ledger.Close()
		data.Close()
		var finished store.Job
		for {
			if ctx.Err() != nil {
				t.Fatal("self-update did not finish", ctx.Err())
			}
			snapshot, err := store.Open("/data")
			if err == nil {
				item, found, readErr := snapshot.Job(id)
				snapshot.Close()
				if readErr == nil && found && item.Status != "running" {
					finished = item
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		replacement, err := cli.ContainerInspect(ctx, strings.TrimPrefix(old.Container.Name, "/"), client.ContainerInspectOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if replacement.Container.State == nil || !replacement.Container.State.Running || replacement.Container.State.Health == nil || replacement.Container.State.Health.Status != "healthy" {
			t.Fatal("Yard not healthy after worker outcome")
		}
		if variant == "GOOD" {
			if finished.Status != "succeeded" || finished.Outcome != "verified" || replacement.Container.Image != target.ID || replacement.Container.ID == current {
				t.Fatalf("unverified replacement: %+v", finished)
			}
			_, err := cli.ContainerInspect(ctx, current, client.ContainerInspectOptions{})
			if !errdefs.IsNotFound(err) {
				t.Fatal("successful update retained original container")
			}
			current = replacement.Container.ID
		} else {
			if finished.Status != "failed" || finished.Outcome != "failed" || finished.Rollback != "restored" || replacement.Container.ID != current || replacement.Container.Image != old.Container.Image {
				t.Fatalf("failed replacement not restored: %+v", finished)
			}
			marker, err := os.ReadFile("/data/.fixture-bad-ran")
			if err != nil || string(marker) != "migration committed" {
				t.Fatal("bad replacement did not execute its committed migration")
			}
			restoredTag, err := cli.ImageInspect(ctx, reference)
			if err != nil || restoredTag.ID != old.Container.Image {
				t.Fatal("fixture restore tag does not reference previous image")
			}
			db, err := sql.Open("sqlite", "/data/nox-yard.sqlite")
			if err != nil {
				t.Fatal(err)
			}
			var count int
			err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='fixture_bad_migration'").Scan(&count)
			db.Close()
			if err != nil || count != 0 {
				t.Fatal("replacement database migration survived rollback")
			}
		}
		snapshot, err := store.Open("/data")
		if err != nil {
			t.Fatal(err)
		}
		admin, err := snapshot.Admin()
		if err != nil || admin.Username != "owner" || !auth.VerifyPassword(admin.PasswordHash, "private-fixture-password") {
			t.Fatal("update lost administrator")
		}
		_, valid, err := snapshot.Session(tokenHash)
		if err != nil || !valid {
			t.Fatal("update lost session")
		}
		settings, err := snapshot.SelfUpdateSettings()
		if err != nil {
			t.Fatal(err)
		}
		if variant == "BAD" && settings.FailedDigest != target.ID {
			t.Fatal("failed target suppression not persisted")
		}
		snapshot.Close()
		t.Log("PASS:", variant, "independent self-update worker, health, image identity, account/session and SQLite recovery")
	}
}
