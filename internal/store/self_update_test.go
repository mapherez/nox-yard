package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestSelfUpdateSettingsMigrationPreservesExistingState(t *testing.T) {
	directory := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(directory, "nox-yard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE self_update_settings (
			id INTEGER PRIMARY KEY, automatic INTEGER NOT NULL,
			last_checked_at INTEGER NOT NULL, last_check_error TEXT NOT NULL,
			failed_digest TEXT NOT NULL)`,
		`INSERT INTO self_update_settings VALUES (1, 1, 1234, '', 'sha256:failed')`,
		`PRAGMA user_version = 2`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	settings, err := data.SelfUpdateSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Automatic || settings.CheckIntervalMinutes != 15 ||
		settings.LastCheckedAt != 1234 || settings.FailedDigest != "sha256:failed" {
		t.Fatalf("migration lost update settings: %+v", settings)
	}
	if err := data.SetSelfUpdateSettings(true, 5); err != nil {
		t.Fatal(err)
	}
	if err := data.SetSelfUpdateSettings(true, 7); err == nil {
		t.Fatal("unsupported interval was saved")
	}
	settings, err = data.SelfUpdateSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.CheckIntervalMinutes != 5 || settings.FailedDigest != "sha256:failed" {
		t.Fatalf("settings were not preserved after validation: %+v", settings)
	}
}

func TestSelfUpdateDockerTargetIsJournaledAtomicallyBeforeLaunch(t *testing.T) {
	for _, stage := range []string{"pulling", "replacing"} {
		t.Run(stage, func(t *testing.T) {
			data, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			id := "aaaaaaaaaaaaaaaaaaaaaaaa"
			if err := data.CreateSelfUpdateJob(SelfUpdateJob{ID: id, OldImageID: "old", TargetImageID: "config", TargetDigest: "signed-platform"}); err != nil {
				t.Fatal(err)
			}
			if _, err := data.db.Exec("UPDATE operation_jobs SET stage=? WHERE id=?", stage, id); err != nil {
				t.Fatal(err)
			}
			err = data.SetSelfUpdateTarget(id, "config", "verified-index")
			if stage == "replacing" {
				if !errors.Is(err, ErrJobChanged) {
					t.Fatal("late target change accepted", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			job, err := data.SelfUpdateJob(id)
			if err != nil {
				t.Fatal(err)
			}
			operation, found, err := data.Job(id)
			if err != nil || !found {
				t.Fatal(err)
			}
			want := "config"
			if stage == "pulling" {
				want = "verified-index"
			}
			if job.TargetImageID != want || len(operation.TargetImages) != 1 || operation.TargetImages[0].ImageID != want || job.TargetDigest != "signed-platform" {
				t.Fatal("target journals diverged", job, operation)
			}
			if err := data.SetSelfUpdateTarget(id, "stale", "other"); !errors.Is(err, ErrJobChanged) {
				t.Fatal("stale target changed", err)
			}
		})
	}
}
