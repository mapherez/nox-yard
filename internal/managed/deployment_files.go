package managed

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/mapherez/nox-yard/internal/store"
)

func projectFiles(project store.ManagedProject) (map[string]string, error) {
	_, env, err := projectEnvironment(project)
	if err != nil {
		return nil, err
	}
	files := map[string]string{"compose.yml": project.YAML}
	for name, value := range env {
		files[name] = value
	}
	return files, nil
}

// The private job payload is the durable file journal. Each rename is atomic;
// a crash between renames retains the reservation for explicit recovery. No
// unrelated file, bind directory or application data is part of this journal.
const deploymentFilesScript = `set -eu
umask 077
mkdir /tmp/journal
tar -x -C /tmp/journal
host="$1"; mode="$2"; shift 2
parent="$host"
while [ "$parent" != / ]; do
  [ ! -L "$parent" ] || exit 73
  parent=$(dirname "$parent")
done
for file do
  parent=$(dirname "$file")
  while [ "$parent" != . ]; do
    [ ! -L "$host/$parent" ] || exit 73
    parent=$(dirname "$parent")
  done
  [ ! -L "$host/$file" ] || exit 73
  if [ "$mode" = restore ]; then
    if [ -e "$host/$file" ]; then
      [ -f "$host/$file" ] || exit 73
      cmp -s "$host/$file" "/tmp/journal/old/$file" || cmp -s "$host/$file" "/tmp/journal/new/$file" || exit 73
    fi
  elif [ "$mode" = verified ]; then
    if [ -f "/tmp/journal/new/$file" ]; then
      cmp -s "$host/$file" "/tmp/journal/new/$file" || exit 73
    else
      [ ! -e "$host/$file" ] || exit 73
    fi
  elif [ -f "/tmp/journal/old/$file" ]; then
    cmp -s "$host/$file" "/tmp/journal/old/$file" || exit 73
  else
    [ ! -e "$host/$file" ] || exit 73
  fi
done
if [ "$mode" = check ] || [ "$mode" = verified ]; then exit 0; fi
if [ "$mode" = prepare ]; then mkdir -p "$host"; exit 0; fi
version=new
if [ "$mode" = restore ]; then version=old; fi
for file do
  if [ -f "/tmp/journal/$version/$file" ]; then
    mkdir -p "$host/$(dirname "$file")"
    # Same-filesystem temporary file plus rename; never truncate the active file.
    temporary=$(mktemp "$host/$(dirname "$file")/.nox-write-XXXXXX")
    cat "/tmp/journal/$version/$file" > "$temporary"
    mv -f "$temporary" "$host/$file"
  else
    rm -f "$host/$file"
  fi
done
`

func hostProjectFiles(ctx context.Context, directory string, previous *store.ManagedProject, next store.ManagedProject, mode string) error {
	oldFiles := map[string]string{}
	if previous != nil {
		var err error
		oldFiles, err = projectFiles(*previous)
		if err != nil {
			return err
		}
	}
	nextFiles, err := projectFiles(next)
	if err != nil {
		return err
	}
	names := map[string]bool{"compose.yml": true, ".env": true}
	for _, project := range []*store.ManagedProject{previous, &next} {
		if project == nil {
			continue
		}
		info, err := InspectSource(project.YAML)
		if err != nil {
			return err
		}
		for _, file := range info.EnvFiles {
			names[file.Path] = true
		}
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for version, files := range map[string]map[string]string{"old": oldFiles, "new": nextFiles} {
		for name, content := range files {
			if name != "compose.yml" && !validEnvPath(name) {
				return fmt.Errorf("invalid environment path")
			}
			if err := writer.WriteHeader(&tar.Header{Name: version + "/" + name, Mode: 0o600, Size: int64(len(content))}); err != nil {
				return err
			}
			if _, err := writer.Write([]byte(content)); err != nil {
				return err
			}
			names[name] = true
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	image, err := helperImage(ctx)
	if err != nil {
		return err
	}
	mount := "type=bind,source=" + path.Dir(directory) + ",target=" + path.Dir(directory)
	if mode == "check" || mode == "verified" {
		mount += ",readonly"
	}
	args := helperCommandArgs("-i", "--network", "none", "--mount", mount, "--entrypoint", "sh", image, "-c", deploymentFilesScript, "sh", directory, mode)
	args = append(args, ordered...)
	command := exec.CommandContext(ctx, "docker", args...)
	command.Stdin = &archive
	if err := command.Run(); err != nil {
		return fmt.Errorf("project files differ, are missing, or cannot be safely accessed; review the original host directory")
	}
	return nil
}

// Use the complete validated model, not a hand-built subset. Relative binds
// are resolved against the original project directory before serialization.
// Compose config already escapes literal dollars for a subsequent Compose read.
func runResolvedCompose(ctx context.Context, name, directory, definition string, args ...string) error {
	image, err := helperImage(ctx)
	if err != nil {
		return err
	}
	base := path.Dir(directory)
	workdir := directory
	if len(args) > 0 && args[0] == "pull" {
		workdir = base
	}
	commandArgs := helperCommandArgs("-i", "--mount", "type=bind,source="+base+",target="+base+",readonly",
		"--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", "--workdir", workdir,
		"--entrypoint", "docker", image, "compose", "--ansi", "never", "--env-file", "/dev/null", "--project-directory", directory, "-p", name, "-f", "-")
	commandArgs = append(commandArgs, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	command.Stdin = strings.NewReader(definition)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s", safeDockerFailure(string(output)))
	}
	return nil
}

func safeDockerFailure(message string) string {
	message = strings.ToLower(message)
	for _, item := range []struct{ match, result string }{
		{"unauthorized", "Registry authentication failed; check Docker host credentials."},
		{"denied", "Registry access was denied; check image permissions and Docker host credentials."},
		{"manifest unknown", "The requested image tag or digest is unavailable in the registry."},
		{"no matching manifest", "The requested image does not support this Docker host platform."},
		{"no space left", "The Docker host has insufficient free disk space."},
		{"port is already allocated", "A requested host port is already in use."},
		{"address already in use", "A requested host port is already in use."},
		{"invalid mount", "A host mount is unavailable or invalid; check the declared bind paths."},
		{"cannot connect", "The Docker daemon is unavailable; check the socket mount and host daemon."},
	} {
		if strings.Contains(message, item.match) {
			return item.result
		}
	}
	return "Docker Compose failed; check registry connectivity, image availability, host mounts and Docker daemon health."
}
