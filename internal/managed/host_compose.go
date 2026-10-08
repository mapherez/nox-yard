package managed

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
)

func helperImage(ctx context.Context) (string, error) {
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
		return "", fmt.Errorf("cannot identify NoX Yard image: %w", err)
	}
	image := strings.TrimSpace(string(output))
	if image == "" {
		return "", fmt.Errorf("cannot identify NoX Yard image")
	}
	return image, nil
}

func checkHostBase(ctx context.Context, base string) error {
	image, err := helperImage(ctx)
	if err != nil {
		return err
	}
	mount := "type=bind,source=" + base + ",target=" + base
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "--label", "nox-yard.role=managed-helper", "--network", "none", "--mount", mount, "--entrypoint", "test", image, "-d", base)
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: directory does not exist or Docker cannot access it on the host", ErrInvalidSource)
	}
	return nil
}

func writeHostCompose(ctx context.Context, projectDir, content string, envFiles map[string]string, createOnly bool) error {
	if projectDir == "" {
		return fmt.Errorf("project directory is missing")
	}
	base := path.Dir(projectDir)
	image, err := helperImage(ctx)
	if err != nil {
		return err
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	files := map[string]string{"compose.yml": content}
	for name, value := range envFiles {
		files[name] = value
	}
	directories := map[string]bool{}
	for name := range files {
		for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
			directories[directory] = true
		}
	}
	orderedDirectories := make([]string, 0, len(directories))
	for directory := range directories {
		orderedDirectories = append(orderedDirectories, directory)
	}
	sort.Strings(orderedDirectories)
	for _, directory := range orderedDirectories {
		if err := writer.WriteHeader(&tar.Header{Name: directory + "/", Typeflag: tar.TypeDir, Mode: 0o700}); err != nil {
			return err
		}
	}
	for name, value := range files {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(value))}); err != nil {
			return err
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	script := `set -eu; if [ "$2" = create ] && [ -e "$1/compose.yml" ]; then echo 'Project compose.yml already exists' >&2; exit 73; fi; mkdir -p "$1"; tar -x -C "$1"`
	mode := "update"
	if createOnly {
		mode = "create"
	}
	mount := "type=bind,source=" + base + ",target=" + base
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "-i", "--label", "nox-yard.role=managed-helper", "--network", "none", "--mount", mount, "--entrypoint", "sh", image, "-c", script, "sh", projectDir, mode)
	command.Stdin = &archive
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("cannot write host project files: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func runHostCompose(ctx context.Context, name, projectDir string, variables map[string]string, args ...string) error {
	image, err := helperImage(ctx)
	if err != nil {
		return err
	}
	base := path.Dir(projectDir)
	mount := "type=bind,source=" + base + ",target=" + base
	commandArgs := []string{"run", "--rm", "--label", "nox-yard.role=managed-helper", "--mount", mount, "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", "--workdir", projectDir}
	if home := os.Getenv("NOX_HOST_HOME"); home != "" {
		commandArgs = append(commandArgs, "--env", "HOME="+home)
	}
	for key, value := range variables {
		commandArgs = append(commandArgs, "--env", key+"="+value)
	}
	commandArgs = append(commandArgs, "--entrypoint", "docker", image, "compose", "--ansi", "never", "-p", name, "-f", path.Join(projectDir, "compose.yml"))
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("Docker Compose failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}
