package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestManagedEnvironmentMigrationPreservesProject(t *testing.T) {
	directory := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(directory, "nox-yard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE managed_projects (name TEXT PRIMARY KEY, source_kind TEXT NOT NULL, source_url TEXT NOT NULL, filename TEXT NOT NULL, yaml TEXT NOT NULL, variables_json TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`,
		`INSERT INTO managed_projects VALUES ('bot', 'paste', '', '', 'services: {}', '{}', 1, 1)`,
		`PRAGMA user_version = 4`,
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
	project, found, err := data.ManagedProject("bot")
	if err != nil || !found || project.EnvFilesJSON != "{}" {
		t.Fatalf("migrated project = %+v, %v, %v", project, found, err)
	}
}
