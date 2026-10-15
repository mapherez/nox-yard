package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RestoreRollbackSnapshot requires all web processes using this database to be
// stopped and its Store closed. Only a generated same-directory snapshot can
// replace the database; application bind/volume files are never traversed.
func RestoreRollbackSnapshot(directory, snapshot string) error {
	base, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	source, err := filepath.Abs(snapshot)
	if err != nil {
		return err
	}
	if filepath.Dir(source) != base || !strings.HasPrefix(filepath.Base(source), ".configuration-") || !strings.HasSuffix(source, ".sqlite") {
		return fmt.Errorf("invalid configuration rollback snapshot path")
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("configuration rollback snapshot is unavailable")
	}
	destination := filepath.Join(base, "nox-yard.sqlite")
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(destination + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(source, destination)
}
