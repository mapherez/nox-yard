// Package hosthelper runs bounded, labelled helpers on Yard's Docker host.
package hosthelper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

func Image(ctx context.Context) (string, error) {
	if image := os.Getenv("NOX_HELPER_IMAGE"); image != "" {
		return image, nil
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Image}}", hostname)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("cannot identify Yard image: %w", err)
	}
	image := strings.TrimSpace(string(output))
	if image == "" {
		return "", fmt.Errorf("cannot identify Yard image")
	}
	return image, nil
}

func Args(args ...string) []string {
	result := []string{"run", "--rm", "--label", "nox-yard.role=managed-helper"}
	if id := os.Getenv("NOX_JOB_ID"); id != "" {
		result = append(result, "--label", "nox-yard.job="+id)
	}
	return append(result, args...)
}

// Engines may report the canonical VM path after recreation (Docker Desktop,
// for example). Accept aliases only when the daemon mounts the very same inode;
// textual prefixes or a list of special-case paths cannot establish identity.
const sourceIdentityCommand = `previous_identity=$(stat -c '%d:%i:%F' /previous) || exit 1
next_identity=$(stat -c '%d:%i:%F' /next) || exit 1
[ -n "$previous_identity" ] && [ "$previous_identity" = "$next_identity" ]`

func SameSource(ctx context.Context, previous, next string) bool {
	if previous == next {
		return true
	}
	if !path.IsAbs(previous) || !path.IsAbs(next) || strings.ContainsAny(previous+next, ",\x00\n\r") {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	image, err := Image(ctx)
	if err != nil {
		return false
	}
	args := Args("--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--mount", "type=bind,source="+previous+",target=/previous,readonly", "--mount", "type=bind,source="+next+",target=/next,readonly", "--entrypoint", "sh", image, "-c", sourceIdentityCommand)
	return exec.CommandContext(ctx, "docker", args...).Run() == nil
}
