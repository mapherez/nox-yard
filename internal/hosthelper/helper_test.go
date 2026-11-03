package hosthelper

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise the actual shell expression with failing stat commands. Failed or
// empty identities must never compare equal merely because both are empty.
func TestSourceIdentityCommandRejectsMissingProof(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	directory := t.TempDir()
	fake := filepath.Join(directory, "stat")
	script := `#!/bin/sh
case "$3" in
  /previous) [ "$PREVIOUS_FAILURE" = 0 ] || exit 1; printf '%s\n' "$PREVIOUS_ID" ;;
  /next) [ "$NEXT_FAILURE" = 0 ] || exit 1; printf '%s\n' "$NEXT_ID" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, left, right, leftFailure, rightFailure string
		allowed                                      bool
	}{
		{"same inode", "1:2:directory", "1:2:directory", "0", "0", true},
		{"different inode", "1:2:directory", "1:3:directory", "0", "0", false},
		{"both stats fail", "", "", "1", "1", false},
		{"previous fails", "", "1:2:directory", "1", "0", false},
		{"next fails", "1:2:directory", "", "0", "1", false},
		{"empty proof", "", "", "0", "0", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(shell, "-c", sourceIdentityCommand)
			command.Env = []string{"PATH=" + directory, "PREVIOUS_ID=" + test.left, "NEXT_ID=" + test.right, "PREVIOUS_FAILURE=" + test.leftFailure, "NEXT_FAILURE=" + test.rightFailure}
			if success := command.Run() == nil; success != test.allowed {
				t.Fatalf("identity comparison allowed=%t, expected %t", success, test.allowed)
			}
		})
	}
}
