package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenWaitsForTransientDatabaseLock(t *testing.T) {
	dir := t.TempDir()
	initial, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	initial.Close()
	locker, err := sql.Open("sqlite", filepath.Join(dir, "nox-yard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	locker.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA locking_mode = EXCLUSIVE", "BEGIN EXCLUSIVE", "SELECT COUNT(*) FROM operation_jobs", "COMMIT"} {
		if _, err := locker.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// SQLite can briefly hold an exclusive lock during WAL recovery/startup.
	// Release it while another process is opening the existing database.
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(100 * time.Millisecond)
		locker.Close()
	}()
	reopened, err := Open(dir)
	<-released
	if err != nil {
		t.Fatalf("transient lock aborted database startup: %v", err)
	}
	defer reopened.Close()
	if _, err := reopened.Jobs(t.Context(), "", false); err != nil {
		t.Fatal(err)
	}
}
