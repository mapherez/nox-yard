package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRootWorkerRetainsHostOwnerOnDatabaseReopen(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires a root Linux worker fixture")
	}
	directory := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(directory, 1001, 1001); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		data, err := Open(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"nox-yard.sqlite", "nox-yard.sqlite-wal", "nox-yard.sqlite-shm"} {
			info, err := os.Stat(filepath.Join(directory, name))
			if err != nil {
				data.Close()
				t.Fatal(err)
			}
			owner := info.Sys().(*syscall.Stat_t)
			if owner.Uid != 1001 || owner.Gid != 1001 {
				data.Close()
				t.Fatalf("root worker made %s inaccessible to host owner", name)
			}
		}
		if err := data.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
