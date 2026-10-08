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
)

// Existing files are only read/compared. Write mode creates missing files using
// noclobber after verifying the same identities as the confirmation preview.
const adoptionFileScript = `set -eu
umask 077
mkdir /tmp/nox-adoption
tar -x -C /tmp/nox-adoption
expected="$1"; write="$2"; shift 2
: > /tmp/nox-identities
for file do
  directory=$(dirname "$file")
  parent="$directory"
  while [ "$parent" != . ]; do
    if [ -L "/host/$parent" ]; then echo 'Adoption refuses symbolic-link directories' >&2; exit 73; fi
    parent=$(dirname "$parent")
  done
  if [ -L "/host/$file" ]; then echo 'Adoption refuses symbolic-link files' >&2; exit 73; fi
  if [ -e "/host/$file" ]; then
    if [ ! -f "/tmp/nox-adoption/$file" ] || [ ! -f "/host/$file" ] || ! cmp -s "/tmp/nox-adoption/$file" "/host/$file"; then
      echo 'Existing project files differ; provide matching source and environment files' >&2; exit 73
    fi
    printf '%s\000present\000' "$file" >> /tmp/nox-identities
  else
    printf '%s\000missing\000' "$file" >> /tmp/nox-identities
  fi
done
identity=$(sha256sum /tmp/nox-identities); identity=${identity%% *}
if [ -n "$expected" ] && [ "$expected" != "$identity" ]; then echo 'Adoption files changed; review again' >&2; exit 73; fi
if [ "$write" = yes ]; then
  for file do
    if [ -f "/tmp/nox-adoption/$file" ] && [ ! -e "/host/$file" ]; then
      directory=$(dirname "$file")
      mkdir -p "/host/$directory"
      (set -C; cat "/tmp/nox-adoption/$file" > "/host/$file")
    fi
  done
fi
printf '%s' "$identity"
`

func adoptionFiles(ctx context.Context, directory, content string, envFiles map[string]string, expected string, write bool) (string, error) {
	if directory == "" {
		return "", fmt.Errorf("%w: adoption requires a host directory", ErrInvalidSource)
	}
	// Validate archive paths and dependencies even when called outside Preview.
	_, cleanup, err := prepareCompose(content, envFiles)
	if err != nil {
		return "", err
	}
	defer cleanup()
	image, err := helperImage(ctx)
	if err != nil {
		return "", err
	}
	files := map[string]string{"compose.yml": content}
	for name, value := range envFiles {
		files[name] = value
	}
	names := make([]string, 0, len(files))
	info, err := InspectSource(content)
	if err != nil {
		return "", err
	}
	for _, file := range info.EnvFiles {
		if _, supplied := files[file.Path]; !supplied {
			names = append(names, file.Path)
		}
	}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var archive bytes.Buffer
	tarWriter := tar.NewWriter(&archive)
	directories := map[string]bool{}
	for _, name := range names {
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
		if err := tarWriter.WriteHeader(&tar.Header{Name: directory + "/", Typeflag: tar.TypeDir, Mode: 0o700}); err != nil {
			return "", err
		}
	}
	for _, name := range names {
		value, supplied := files[name]
		if !supplied {
			continue
		}
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(value))}); err != nil {
			return "", err
		}
		if _, err := tarWriter.Write([]byte(value)); err != nil {
			return "", err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return "", err
	}
	mount := "type=bind,source=" + directory + ",target=/host"
	mode := "no"
	if write {
		mode = "yes"
	} else {
		mount += ",readonly"
	}
	arguments := helperCommandArgs("-i", "--network", "none", "--mount", mount, "--entrypoint", "sh", image, "-c", adoptionFileScript, "sh", expected, mode)
	arguments = append(arguments, names...)
	command := exec.CommandContext(ctx, "docker", arguments...)
	command.Stdin = &archive
	output, err := command.Output()
	if err != nil {
		// Never include subprocess stderr: a daemon error can contain file values.
		return "", fmt.Errorf("%w: provide matching source and all existing declared environment files in the original host directory; files may have changed since preview", ErrConflict)
	}
	return strings.TrimSpace(string(output)), nil
}
