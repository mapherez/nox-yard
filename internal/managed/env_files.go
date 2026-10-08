package managed

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

var globalControlVariable = regexp.MustCompile(`(?m)^[\t ]*(?:export[\t ]+)?["']?(?:COMPOSE|DOCKER)_[A-Za-z0-9_]*["']?[\t ]*[=:]`)

func envFileDefines(content, key string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		name, _, hasValue := strings.Cut(line, "=")
		if !hasValue {
			name, _, hasValue = strings.Cut(line, ":")
		}
		if hasValue && strings.TrimSpace(name) == key {
			return true
		}
	}
	return false
}

func prepareCompose(content string, envFiles map[string]string) (string, func(), error) {
	info, err := InspectSource(content)
	if err != nil {
		return "", nil, err
	}
	provided := map[string]string{}
	total := 0
	for name, value := range envFiles {
		if name == ".env" && globalControlVariable.MatchString(value) {
			return "", nil, fmt.Errorf("%w: global .env cannot set COMPOSE_ or DOCKER_ controls; use a separate service env file for application values", ErrInvalidSource)
		}
		if !validEnvPath(name) {
			return "", nil, fmt.Errorf("%w: invalid env_file path", ErrInvalidSource)
		}
		canonical := filepath.ToSlash(filepath.Clean(name))
		if canonical != name {
			return "", nil, fmt.Errorf("%w: env_file path must match the Compose source", ErrInvalidSource)
		}
		if !utf8.ValidString(value) || len(value) > MaxSourceBytes || total+len(value) > MaxSourceBytes {
			return "", nil, fmt.Errorf("%w: environment files must contain up to 1 MiB of UTF-8 text", ErrInvalidSource)
		}
		total += len(value)
		provided[name] = value
	}
	allowed := map[string]bool{}
	for _, file := range info.EnvFiles {
		allowed[file.Path] = true
		if _, exists := provided[file.Path]; file.Required && !exists {
			return "", nil, fmt.Errorf("%w: provide env_file %s before validation", ErrInvalidSource, file.Path)
		}
	}
	for name := range provided {
		if !allowed[name] {
			return "", nil, fmt.Errorf("%w: unexpected env_file %s", ErrInvalidSource, name)
		}
	}
	dir, err := os.MkdirTemp("", "nox-compose-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	composePath := filepath.Join(dir, "__nox_compose.yaml")
	if err := os.WriteFile(composePath, []byte(content), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	for name, value := range provided {
		filePath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
			cleanup()
			return "", nil, err
		}
		if err := os.WriteFile(filePath, []byte(value), 0o600); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return composePath, cleanup, nil
}
