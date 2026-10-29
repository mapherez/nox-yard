package store

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenWaitsForDatabaseLockBeforeConfiguringJournal(t *testing.T) {
	for _, mode := range []string{"WAL", "DELETE"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			initial, err := Open(directory)
			if err != nil {
				t.Fatal(err)
			}
			initial.Close()
			writer, err := sql.Open("sqlite", filepath.Join(directory, "nox-yard.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			writer.SetMaxOpenConns(1)
			for _, statement := range []string{"PRAGMA journal_mode=" + mode, "PRAGMA locking_mode=EXCLUSIVE", "BEGIN EXCLUSIVE", "CREATE TABLE startup_lock(value TEXT)"} {
				if _, err := writer.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			type result struct {
				data *Store
				err  error
			}
			started, done := make(chan struct{}), make(chan result, 1)
			go func() {
				close(started)
				data, err := Open(directory)
				done <- result{data, err}
			}()
			<-started
			select {
			case opened := <-done:
				if opened.data != nil {
					opened.data.Close()
				}
				t.Fatalf("Open returned before the SQLite lock was released: %v", opened.err)
			case <-time.After(100 * time.Millisecond):
			}
			// Closing this exclusive writer rolls back the disposable DDL and
			// releases its lock; a starting worker must then open normally.
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case opened := <-done:
				if opened.err != nil {
					t.Fatal(opened.err)
				}
				defer opened.data.Close()
				for pragma, want := range map[string]string{"journal_mode": "wal", "busy_timeout": "5000", "foreign_keys": "1", "integrity_check": "ok"} {
					var got string
					if err := opened.data.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
						t.Fatalf("%s: got %q, want %q: %v", pragma, got, want, err)
					}
				}
			case <-time.After(6 * time.Second):
				t.Fatal("Open did not resume after releasing the lock")
			}
		})
	}
}

func TestConcurrentWorkerDatabaseOpen(t *testing.T) {
	directory := t.TempDir()
	initial, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	initial.Close()
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			data, err := Open(directory)
			if err != nil {
				t.Errorf("concurrent worker startup: %v", err)
				return
			}
			data.Close()
		}()
	}
	close(start)
	workers.Wait()
}
