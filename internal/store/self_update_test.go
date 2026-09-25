package store

import (
	"database/sql"
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
