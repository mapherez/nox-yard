package managed

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EnvFile struct {
	Path     string `json:"path"`
	Required bool   `json:"required"`
}

type SourceInfo struct {
	SuggestedName string    `json:"suggestedName,omitempty"`
	EnvFiles      []EnvFile `json:"envFiles"`
}

var invalidProjectCharacter = regexp.MustCompile(`[^a-z0-9_-]+`)

func InspectSource(content string) (SourceInfo, error) {
	var model map[string]any
	if err := yaml.Unmarshal([]byte(content), &model); err != nil {
		return SourceInfo{}, fmt.Errorf("%w: invalid YAML; check the Compose syntax", ErrInvalidSource)
	}
	services, ok := model["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return SourceInfo{}, fmt.Errorf("%w: Compose file has no services", ErrInvalidSource)
	}
	info := SourceInfo{EnvFiles: []EnvFile{}}
	// Compose reads .env implicitly for interpolation and global controls.
	byPath := map[string]bool{".env": false}
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		service, ok := services[name].(map[string]any)
		if !ok {
			return SourceInfo{}, fmt.Errorf("%w: invalid service %s", ErrInvalidSource, name)
		}
		if info.SuggestedName == "" {
			if containerName, ok := service["container_name"].(string); ok && !strings.Contains(containerName, "$") {
				info.SuggestedName = normalizeProjectName(containerName)
			}
		}
		entries, err := envFileEntries(service["env_file"])
		if err != nil {
			return SourceInfo{}, fmt.Errorf("%w: service %s: %v", ErrInvalidSource, name, err)
		}
		for _, entry := range entries {
			byPath[entry.Path] = byPath[entry.Path] || entry.Required
		}
	}
	if info.SuggestedName == "" {
		if name, ok := model["name"].(string); ok && !strings.Contains(name, "$") {
			info.SuggestedName = normalizeProjectName(name)
		}
	}
	for name, required := range byPath {
		info.EnvFiles = append(info.EnvFiles, EnvFile{Path: name, Required: required})
	}
	sort.Slice(info.EnvFiles, func(i, j int) bool { return info.EnvFiles[i].Path < info.EnvFiles[j].Path })
	return info, nil
}

func normalizeProjectName(name string) string {
	name = invalidProjectCharacter.ReplaceAllString(strings.ToLower(name), "-")
	name = strings.TrimLeft(name, "-_")
	if len(name) > 63 {
		name = name[:63]
	}
	if projectNamePattern.MatchString(name) {
		return name
	}
	return ""
}

func envFileEntries(raw any) ([]EnvFile, error) {
	if raw == nil {
		return nil, nil
	}
	values := []any{raw}
	if list, ok := raw.([]any); ok {
		values = list
	}
	result := make([]EnvFile, 0, len(values))
	for _, value := range values {
		entry := EnvFile{Required: true}
		switch typed := value.(type) {
		case string:
			entry.Path = typed
		case map[string]any:
			if name, ok := typed["path"].(string); ok {
				entry.Path = name
			}
			if required, ok := typed["required"].(bool); ok {
				entry.Required = required
			}
		default:
			return nil, fmt.Errorf("unsupported env_file entry")
		}
		if !validEnvPath(entry.Path) {
			return nil, fmt.Errorf("env_file must use a relative path within the project")
		}
		clean := path.Clean(entry.Path)
		if entry.Path != clean && entry.Path != "./"+clean {
			return nil, fmt.Errorf("env_file path must not traverse directories")
		}
		entry.Path = path.Clean(entry.Path)
		result = append(result, entry)
	}
	return result, nil
}

func validEnvPath(name string) bool {
	clean := path.Clean(name)
	return name != "" && clean != "." && clean != "__nox_compose.yaml" && clean != "compose.yml" && !strings.HasPrefix(clean, "../") && clean != ".." && !strings.HasPrefix(name, "/") && !strings.ContainsAny(name, "\\\x00") && !strings.Contains(name, "$")
}
