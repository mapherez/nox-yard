package managed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"gopkg.in/yaml.v3"
)

// ComposeContext records intent which Engine labels cannot reconstruct. A nil
// Profiles honors the project's environment; an explicit empty list disables it.
type ComposeContext struct {
	Profiles       *[]string `json:"profiles,omitempty"`
	EnvFiles       *[]string `json:"interpolationFiles,omitempty"`
	ObservedLabels string    `json:"observedLabels,omitempty"`
	Revision       uint64    `json:"revision,omitempty"`
	Services       []string  `json:"services,omitempty"`
}

// ComposeDiagnostic deliberately never includes Compose stderr or env values.
type ComposeDiagnostic struct {
	Operation string `json:"operation"`
	Service   string `json:"service,omitempty"`
	File      string `json:"file,omitempty"`
	Field     string `json:"field,omitempty"`
	Reason    string `json:"reason"`
}

func (e *ComposeDiagnostic) Error() string { return fmt.Sprintf("%s: %s", e.Operation, e.Reason) }
func (e *ComposeDiagnostic) Unwrap() error { return ErrInvalidSource }

var missingComposeVariable = regexp.MustCompile(`(?:required variable|variable) ([A-Za-z_][A-Za-z0-9_]*) (?:is missing|is not set|is required)`)

func composeFailure(operation string, output []byte) error {
	d := &ComposeDiagnostic{Operation: operation, Reason: "Compose could not resolve this configuration; check its syntax, dependencies and required variables"}
	if m := missingComposeVariable.FindSubmatch(output); len(m) > 1 {
		d.Field = string(m[1])
		d.Reason = "provide required interpolation variable " + d.Field
	}
	return d
}

func composeEnvironment() []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "COMPOSE_REMOVE_ORPHANS=0"}
}

func validateComposeControls(content string) error {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			key, _, ok = strings.Cut(line, ":")
		}
		key = strings.Trim(strings.TrimSpace(key), "\"'")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "DOCKER_") || key == "COMPOSE_REMOVE_ORPHANS" || key == "COMPOSE_EXPERIMENTAL" {
			return &ComposeDiagnostic{Operation: "configuration", Field: key, Reason: "project environment cannot change the Docker destination or enable implicit destructive/remote operations"}
		}
	}
	return nil
}

func sourceLabelSignature(items []container.InspectResponse) string {
	var labels []string
	for _, item := range items {
		if item.Config == nil {
			continue
		}
		l := item.Config.Labels
		if l["com.docker.compose.project.config_files"] == "" || l["com.docker.compose.project.config_files"] == "-" {
			continue
		}
		labels = append(labels, item.ID+"\x00"+l["com.docker.compose.project.working_dir"]+"\x00"+l["com.docker.compose.project.config_files"])
	}
	sort.Strings(labels)
	return fingerprint([]byte(strings.Join(labels, "\n")))
}

func configurationLocation(s FileSnapshot) store.FileConfiguration {
	files, _ := json.Marshal(s.ComposeFiles)
	ctx, _ := json.Marshal(s.Context)
	return store.FileConfiguration{Name: s.Name, Directory: s.Directory, ComposeFiles: string(files), Context: string(ctx), State: "ready"}
}

func (m *Manager) markObservedSource(ctx context.Context, name string) error {
	location, found, err := m.data.FileConfiguration(name)
	if err != nil || !found {
		return err
	}
	items, err := m.runtime(ctx, name)
	if err != nil {
		return err
	}
	var c ComposeContext
	if json.Unmarshal([]byte(location.Context), &c) != nil {
		return ErrInvalidSource
	}
	c.ObservedLabels = sourceLabelSignature(items)
	b, _ := json.Marshal(c)
	location.Context = string(b)
	return m.data.SetFileConfiguration(location)
}

type composeDependency struct {
	name, base        string
	required, compose bool
	include           bool
	projectDirectory  string
	includeEnvFiles   []string
	includeFirst      string
}

// Only enumerate file dependencies here. Compose itself interprets all YAML
// merges, interpolation, profiles and path bases. No host data mounts are read.
func dependencyNames(content, base string) ([]composeDependency, error) {
	var root map[string]any
	if yaml.Unmarshal([]byte(content), &root) != nil {
		return nil, ErrInvalidSource
	}
	var result []composeDependency
	var add func(any, string, bool, bool) error
	add = func(raw any, at string, required, compose bool) error {
		switch v := raw.(type) {
		case nil:
			return nil
		case string:
			result = append(result, composeDependency{name: v, base: at, required: required, compose: compose})
		case []any:
			for _, child := range v {
				if err := add(child, at, required, compose); err != nil {
					return err
				}
			}
		case map[string]any:
			if value, ok := v["required"].(bool); ok {
				required = value
			}

			return add(v["path"], at, required, compose)
		default:
			return ErrInvalidSource
		}
		return nil
	}
	includeValues, err := composeStringsOrEntries(root["include"])
	if err != nil {
		return nil, err
	}
	for _, raw := range includeValues {
		value := raw
		directory := ""
		var environment []string
		if entry, ok := raw.(map[string]any); ok {
			value = entry["path"]
			directory, _ = entry["project_directory"].(string)
			if env, exists := entry["env_file"]; exists {
				values, err := composeStringsOrEntries(env)
				if err != nil {
					return nil, err
				}
				environment = []string{}
				for _, v := range values {
					p, ok := v.(string)
					if !ok {
						return nil, ErrInvalidSource
					}
					environment = append(environment, p)
				}
			}
		}
		paths, err := composeStringsOrEntries(value)
		if err != nil || len(paths) == 0 {
			return nil, ErrInvalidSource
		}
		first, ok := paths[0].(string)
		if !ok {
			return nil, ErrInvalidSource
		}
		for _, v := range paths {
			p, ok := v.(string)
			if !ok {
				return nil, ErrInvalidSource
			}
			result = append(result, composeDependency{name: p, base: base, required: true, compose: true, include: true, projectDirectory: directory, includeEnvFiles: environment, includeFirst: first})
		}
	}

	for _, kind := range []string{"configs", "secrets"} {
		resources, _ := root[kind].(map[string]any)
		for _, raw := range resources {
			if value, ok := raw.(map[string]any); ok {
				if err := add(value["file"], base, true, false); err != nil {
					return nil, err
				}
			}
		}
	}
	services, _ := root["services"].(map[string]any)
	for _, raw := range services {
		service, _ := raw.(map[string]any)
		for _, field := range []string{"env_file", "label_file"} {
			if err := add(service[field], base, true, false); err != nil {
				return nil, err
			}
		}
		if ext, ok := service["extends"].(map[string]any); ok {
			if err := add(ext["file"], base, true, true); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func composeStringsOrEntries(value any) ([]any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []any{v}, nil
	case map[string]any:
		return []any{v}, nil
	case []any:
		return v, nil
	default:
		return nil, ErrInvalidSource
	}
}

func writeSnapshotDirectory(files map[string]string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "nox-compose-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	total := 0
	for name, content := range files {
		total += len(content)
		if !safeProjectFile(name) || len(content) > MaxSourceBytes || total > 8*MaxSourceBytes {
			cleanup()
			return "", func() {}, ErrInvalidSource
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			cleanup()
			return "", func() {}, err
		}
		if err := os.WriteFile(dest, []byte(content), 0600); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dir, cleanup, nil
}

func interpolateDependency(ctx context.Context, name string, files map[string]string, contexts ...ComposeContext) (string, error) {
	if !strings.Contains(name, "$") {
		return name, nil
	}
	// A synthetic Compose value delegates interpolation to the native parser.
	probe := map[string]any{"services": map[string]any{"path": map[string]any{"image": "scratch", "environment": map[string]string{"VALUE": name}}}}
	b, _ := json.Marshal(probe)
	staged := map[string]string{"path.json": string(b)}
	if env, ok := files[".env"]; ok {
		staged[".env"] = env
	}
	var selection *[]string
	if len(contexts) > 0 {
		selection = contexts[0].EnvFiles
	}
	if selection != nil {
		for _, name := range *selection {
			if value, ok := files[name]; ok {
				staged[name] = value
			}
		}
	}
	dir, cleanup, err := writeSnapshotDirectory(staged)
	if err != nil {
		return "", err
	}
	defer cleanup()
	args := []string{"compose", "-p", "yard-path", "-f", filepath.Join(dir, "path.json")}
	if selection != nil {
		if len(*selection) == 0 {
			args = append(args, "--env-file", "/dev/null")
		} else {
			for _, name := range *selection {
				args = append(args, "--env-file", filepath.Join(dir, name))
			}
		}
	}
	args = append(args, "config", "--format", "json")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir, cmd.Env = dir, composeEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return "", composeFailure("path resolution", nil)
	}
	var model composeModel
	decoded, err := decodedComposeModel(out)
	if err != nil || json.Unmarshal(decoded, &model) != nil {
		return "", ErrInvalidSource
	}
	v := model.Services["path"].Environment["VALUE"]
	if v == nil {
		return "", ErrInvalidSource
	}
	return *v, nil
}

func snapshotDependencies(ctx context.Context, s *FileSnapshot, fetch bool) error {
	if err := validateComposeControls(s.Files[".env"]); err != nil {
		return err
	}
	normalize := func(value, base string) (string, error) {
		if strings.Contains(value, "://") {
			return "", fmt.Errorf("%w: remote Compose dependencies are not fetched", ErrInvalidSource)
		}
		if path.IsAbs(value) {
			if !strings.HasPrefix(value, s.Directory+"/") {
				return "", fmt.Errorf("%w: configuration dependency must stay in its project directory", ErrInvalidSource)
			}
			value = strings.TrimPrefix(value, s.Directory+"/")
			base = "."
		}
		name := path.Clean(path.Join(base, value))
		if name != "." && !safeProjectFile(name) {
			return "", fmt.Errorf("%w: configuration dependency must stay in its project directory", ErrInvalidSource)
		}
		return name, nil
	}
	load := func(name string, required bool) error {
		if !safeProjectFile(name) {
			return ErrInvalidSource
		}
		if _, exists := s.Files[name]; !exists && fetch {
			files, err := readConfigurationFiles(ctx, s.Directory, []string{name})
			if err != nil {
				return err
			}
			for key, value := range files {
				s.Files[key] = value
			}
		}
		if _, exists := s.Files[name]; !exists && required {
			return &ComposeDiagnostic{Operation: "configuration", File: s.ComposeFiles[0], Field: "dependency", Reason: "provide the required local dependency referenced by this Compose selection"}
		}
		return nil
	}
	if s.Context.EnvFiles != nil {
		for _, name := range *s.Context.EnvFiles {
			if err := load(name, true); err != nil {
				return err
			}
			if err := validateComposeControls(s.Files[name]); err != nil {
				return err
			}
		}
	}
	type sourceFile struct {
		name, base    string
		configuration ComposeContext
	}
	queue := []sourceFile{}
	for _, name := range s.ComposeFiles {
		queue = append(queue, sourceFile{name, ".", s.Context})
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		source := queue[0]
		queue = queue[1:]
		key := source.name + "\x00" + source.base
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(seen) > 64 || len(s.Files) > 128 {
			return fmt.Errorf("%w: configuration dependency graph exceeds supported bounds", ErrInvalidSource)
		}
		content, exists := s.Files[source.name]
		if !exists {
			return &ComposeDiagnostic{Operation: "configuration", File: s.ComposeFiles[0], Field: "dependency", Reason: "a required local Compose dependency is missing"}
		}
		deps, err := dependencyNames(content, source.base)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			value, err := interpolateDependency(ctx, dep.name, s.Files, source.configuration)
			if err != nil {
				return err
			}
			name, err := normalize(value, dep.base)
			if err != nil {
				return err
			}
			if err := load(name, dep.required); err != nil {
				return err
			}
			if !dep.compose {
				continue
			}
			includedBase := path.Dir(name)
			configuration := source.configuration
			if dep.include {
				first, err := interpolateDependency(ctx, dep.includeFirst, s.Files, source.configuration)
				if err != nil {
					return err
				}
				first, err = normalize(first, dep.base)
				if err != nil {
					return err
				}
				includedBase = path.Dir(first)
				if dep.projectDirectory != "" {
					value, err := interpolateDependency(ctx, dep.projectDirectory, s.Files, source.configuration)
					if err != nil {
						return err
					}
					includedBase, err = normalize(value, dep.base)
					if err != nil {
						return err
					}
				}
				local := []string{}
				if dep.includeEnvFiles == nil {
					file := path.Join(includedBase, ".env")
					if err := load(file, false); err != nil {
						return err
					}
					if _, exists := s.Files[file]; exists {
						local = append(local, file)
					}
				} else {
					for _, raw := range dep.includeEnvFiles {
						value, err := interpolateDependency(ctx, raw, s.Files, source.configuration)
						if err != nil {
							return err
						}
						file, err := normalize(value, dep.base)
						if err != nil {
							return err
						}
						if err := load(file, true); err != nil {
							return err
						}
						local = append(local, file)
					}
				}
				if source.configuration.EnvFiles != nil {
					local = append(local, (*source.configuration.EnvFiles)...)
				} else if _, exists := s.Files[".env"]; exists {
					local = append(local, ".env")
				}
				configuration.EnvFiles = &local
			}
			queue = append(queue, sourceFile{name, includedBase, configuration})
		}
	}
	return nil
}

func resolveSnapshot(ctx context.Context, s *FileSnapshot) error {
	if err := validateComposeControls(s.Files[".env"]); err != nil {
		return err
	}
	dir, cleanup, err := writeSnapshotDirectory(s.Files)
	if err != nil {
		return err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	base := []string{"compose", "--ansi", "never", "-p", s.Name, "--project-directory", dir}
	if s.Context.EnvFiles != nil {
		if len(*s.Context.EnvFiles) == 0 {
			base = append(base, "--env-file", "/dev/null")
		} else {
			for _, file := range *s.Context.EnvFiles {
				if !safeProjectFile(file) {
					return ErrInvalidSource
				}
				if _, exists := s.Files[file]; !exists {
					return &ComposeDiagnostic{Operation: "configuration", File: file, Reason: "selected interpolation file is missing"}
				}
				base = append(base, "--env-file", filepath.Join(dir, file))
			}
		}
	}
	for _, file := range s.ComposeFiles {
		base = append(base, "-f", filepath.Join(dir, filepath.FromSlash(file)))
	}
	run := func(all bool, services []string) ([]byte, error) {
		args := slices.Clone(base)
		if all {
			args = append(args, "--profile", "*")
		} else if s.Context.Profiles != nil {
			for _, p := range *s.Context.Profiles {
				args = append(args, "--profile", p)
			}
		}
		args = append(args, "config", "--format", "json", "--no-path-resolution")
		args = append(args, services...)
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Dir = dir
		cmd.Env = composeEnvironment()
		if s.Context.Profiles != nil {
			cmd.Env = append(cmd.Env, "COMPOSE_PROFILES="+strings.Join(*s.Context.Profiles, ","))
		}
		out, err := cmd.Output()
		if err != nil {
			var stderr []byte
			if e, ok := err.(*exec.ExitError); ok {
				stderr = e.Stderr
			}
			return nil, composeFailure("Compose resolution", stderr)
		}
		return out, nil
	}
	all, err := run(true, nil)
	if err != nil {
		return err
	}
	var model map[string]any
	if json.Unmarshal(all, &model) != nil {
		return ErrInvalidSource
	}
	services, ok := model["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return ErrInvalidSource
	}
	s.AvailableProfiles = []string{}
	for name, raw := range services {
		service, ok := raw.(map[string]any)
		if !ok {
			return ErrInvalidSource
		}
		if image, _ := service["image"].(string); image == "" {
			return &ComposeDiagnostic{Operation: "configuration", Service: name, Field: "image", Reason: "an explicit prebuilt image is required; builds are not performed"}
		}
		labels, _ := service["labels"].(map[string]any)
		for _, reserved := range []string{"nox-yard.operation", "nox-yard.image-reference"} {
			if _, exists := labels[reserved]; exists {
				return &ComposeDiagnostic{Operation: "configuration", Service: name, Field: "labels", Reason: "service uses the reserved label " + reserved}
			}
		}
		delete(service, "build")
		for _, v := range asArray(service["profiles"]) {
			if p, ok := v.(string); ok && !slices.Contains(s.AvailableProfiles, p) {
				s.AvailableProfiles = append(s.AvailableProfiles, p)
			}
		}
		for _, raw := range asArray(service["volumes"]) {
			if mount, ok := raw.(map[string]any); ok && mount["type"] == "bind" {
				if value, ok := mount["source"].(string); ok {
					mount["source"] = originalComposePath(value, dir, s.Directory)
				}
			}
		}
	}
	for _, kind := range []string{"configs", "secrets"} {
		resources, _ := model[kind].(map[string]any)
		for _, raw := range resources {
			if resource, ok := raw.(map[string]any); ok {
				if file, ok := resource["file"].(string); ok {
					resource["file"] = originalComposePath(file, dir, s.Directory)
				}
			}
		}
	}
	active, err := run(false, nil)
	if err != nil {
		return err
	}
	var selected composeModel
	if json.Unmarshal(active, &selected) != nil {
		return ErrInvalidSource
	}
	s.ActiveServices = []string{}
	for name := range selected.Services {
		s.ActiveServices = append(s.ActiveServices, name)
	}
	for _, name := range s.Context.Services {
		if _, exists := services[name]; exists && !slices.Contains(s.ActiveServices, name) {
			s.ActiveServices = append(s.ActiveServices, name)
		}
	}
	s.EffectiveProfiles = []string{}
	if s.Context.Profiles != nil {
		s.EffectiveProfiles = slices.Clone(*s.Context.Profiles)
	} else {
		value, err := interpolateDependency(ctx, "${COMPOSE_PROFILES-}", s.Files, s.Context)
		if err != nil {
			return err
		}
		for _, profile := range strings.Split(value, ",") {
			if profile = strings.TrimSpace(profile); profile != "" {
				s.EffectiveProfiles = append(s.EffectiveProfiles, profile)
			}
		}
	}
	sort.Strings(s.ActiveServices)
	sort.Strings(s.AvailableProfiles)
	if len(s.ActiveServices) > 0 {
		if _, err := run(false, s.ActiveServices); err != nil {
			return err
		}
	}
	s.Resolved, err = json.Marshal(model)
	s.Fingerprint = fileFingerprint(*s)
	return err
}

func originalComposePath(value, staged, original string) string {
	if strings.HasPrefix(value, staged+"/") {
		return path.Join(original, strings.TrimPrefix(value, staged+"/"))
	}
	if !path.IsAbs(value) {
		return path.Join(original, value)
	}
	return value
}

func nativeVariables(ctx context.Context, content string) ([]Variable, error) {
	dir, cleanup, err := writeSnapshotDirectory(map[string]string{"compose.yaml": content})
	if err != nil {
		return nil, err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", "yard-variables", "-f", filepath.Join(dir, "compose.yaml"), "config", "--variables", "--format", "json")
	cmd.Dir, cmd.Env = dir, composeEnvironment()
	out, err := cmd.Output()
	if err != nil {
		return nil, composeFailure("variable discovery", nil)
	}
	var values map[string]struct {
		Name         string
		Required     bool
		DefaultValue string
	}
	if json.Unmarshal(out, &values) != nil {
		return nil, fmt.Errorf("%w: installed Compose must support JSON variable discovery", ErrInvalidSource)
	}
	result := []Variable{}
	for name, v := range values {
		result = append(result, Variable{Name: name, Required: v.Required, Default: v.DefaultValue})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}
