package store

import (
	"os"
	"path/filepath"
	"syscall"
)

// Workers run as root to access Docker. Keep the Yard's exact SQLite files
// owned by the host owner already established for its data directory.
func inheritDatabaseOwner(directory string) {
	info, err := os.Stat(directory)
	if err != nil {
		return
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid == 0 {
		return
	}
	for _, name := range []string{"nox-yard.sqlite", "nox-yard.sqlite-wal", "nox-yard.sqlite-shm"} {
		target := filepath.Join(directory, name)
		if info, err := os.Lstat(target); err == nil && info.Mode().IsRegular() {
			_ = os.Chown(target, int(owner.Uid), int(owner.Gid))
		}
	}
}
