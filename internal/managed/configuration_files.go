package managed

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
	"gopkg.in/yaml.v3"
)

var ErrFilesMissing = errors.New("project Compose files were not found")

// FileSnapshot is private execution data. Only FileConfigurationView is public.
type FileSnapshot struct {
	Name         string
	Directory    string
	ComposeFiles []string
	Files        map[string]string
	Resolved     json.RawMessage
	Fingerprint  string
	Problem      string
}
type ConfigurationFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Compose bool   `json:"compose"`
}
type ConfigurationVariable struct {
	Name    string `json:"name"`
	Value   string `json:"value,omitempty"`
	Service string `json:"service"`
	Pending bool   `json:"pending"`
}
type FileConfigurationView struct {
	Available   bool                    `json:"available"`
	State       string                  `json:"state"`
	Error       string                  `json:"error,omitempty"`
	Directory   string                  `json:"directory,omitempty"`
	Fingerprint string                  `json:"fingerprint,omitempty"`
	Files       []ConfigurationFile     `json:"files"`
	Variables   []ConfigurationVariable `json:"variables"`
}
type ConfigurationEdit struct {
	Fingerprint string `json:"fingerprint"`
	Path        string `json:"path,omitempty"`
	Content     string `json:"content"`
	Service     string `json:"service,omitempty"`
	Variable    string `json:"variable,omitempty"`
}

func safeProjectFile(name string) bool {
	return name != "" && name != "." && !path.IsAbs(name) && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00\n\r")
}

// Each path is a validated argument, never interpolated shell source. No file
// values or stderr are included in errors. Refuse symlinks at every level.
const readConfigurationScript = `set -eu
[ -d "/host/$1" ] || exit 72
[ ! -L "/host/$1" ] || exit 73
cd "/host/$1"; shift
for file do
 p="$file"
 while [ "$p" != . ]; do [ ! -L "$p" ] || exit 73; p=$(dirname "$p"); done
 if [ -e "$file" ]; then [ -f "$file" ] || exit 73; tar -c -- "$file"; fi
done`

func readConfigurationFiles(ctx context.Context, directory string, names []string) (map[string]string, error) {
	if !path.IsAbs(directory) || directory == "/" {
		return nil, ErrFilesMissing
	}
	for _, name := range names {
		if !safeProjectFile(name) {
			return nil, fmt.Errorf("%w: configuration path must stay in its project directory", ErrInvalidSource)
		}
	}
	image, err := helperImage(ctx)
	if err != nil {
		return nil, err
	}
	args := helperCommandArgs("--network", "none", "--mount", "type=bind,source="+path.Dir(directory)+",target=/host,readonly", "--entrypoint", "sh", image, "-c", readConfigurationScript, "sh", path.Base(directory))
	args = append(args, names...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	// Concatenated tar streams are decoded separately (one file per stream).
	var output bytes.Buffer
	cmd.Stdout = &boundedConfigurationOutput{buffer: &output}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if (errors.As(err, &exit) && exit.ExitCode() == 72) || strings.Contains(stderr.String(), "bind source path does not exist") {
			return nil, ErrFilesMissing
		}
		return nil, fmt.Errorf("%w: original project files are inaccessible, too large or contain symbolic links", ErrConflict)
	}
	result := map[string]string{}
	reader := bytes.NewReader(output.Bytes())
	for reader.Len() > 0 {
		before := reader.Len()
		tr := tar.NewReader(reader)
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, ErrInvalidSource
			}
			if header.Typeflag != tar.TypeReg || !slices.Contains(names, header.Name) || header.Size > MaxSourceBytes {
				return nil, ErrInvalidSource
			}
			value, err := io.ReadAll(io.LimitReader(tr, MaxSourceBytes+1))
			if err != nil || len(value) > MaxSourceBytes || !utf8.Valid(value) {
				return nil, ErrInvalidSource
			}
			result[header.Name] = string(value)
		}
		// BusyBox tar pads the end of each archive to a 512-byte boundary, with
		// two zero blocks. archive/tar consumes those blocks before returning EOF.
		if reader.Len() == before {
			break
		}
	}
	return result, nil
}

type boundedConfigurationOutput struct{ buffer *bytes.Buffer }

func (w *boundedConfigurationOutput) Write(value []byte) (int, error) {
	if w.buffer.Len()+len(value) > 8*MaxSourceBytes {
		return 0, ErrInvalidSource
	}
	return w.buffer.Write(value)
}

func configurationEnvFiles(content string) ([]string, error) {
	var root struct {
		Services map[string]struct {
			EnvFile any `yaml:"env_file"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal([]byte(content), &root) != nil {
		return nil, fmt.Errorf("%w: invalid Compose YAML", ErrInvalidSource)
	}
	var result []string
	var add func(any) error
	add = func(value any) error {
		switch v := value.(type) {
		case nil:
			return nil
		case string:
			if !safeProjectFile(v) {
				return fmt.Errorf("%w: env_file must be a project-relative path", ErrInvalidSource)
			}
			result = append(result, v)
		case []any:
			for _, entry := range v {
				if err := add(entry); err != nil {
					return err
				}
			}
		case map[string]any:
			return add(v["path"])
		default:
			return ErrInvalidSource
		}
		return nil
	}
	for _, service := range root.Services {
		if err := add(service.EnvFile); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (m *Manager) readFileSnapshot(ctx context.Context, name string) (FileSnapshot, error) {
	return m.readFileSnapshotMode(ctx, name, false)
}

func (m *Manager) readFileSnapshotMode(ctx context.Context, name string, allowInvalid bool) (FileSnapshot, error) {
	snapshot := FileSnapshot{Name: name, Files: map[string]string{}}
	location, found, err := m.data.FileConfiguration(name)
	if err != nil {
		return snapshot, err
	}
	if found {
		snapshot.Directory = location.Directory
		_ = json.Unmarshal([]byte(location.ComposeFiles), &snapshot.ComposeFiles)
	}
	// A later terminal deployment may change the ordered -f list. Fresh real
	// labels take precedence; Yard's stdin label "-" falls back to the location.
	items, err := m.runtime(ctx, name)
	if err != nil {
		return snapshot, err
	}
	labelDirectory := ""
	var labelFiles []string
	for _, item := range items {
		if item.Config == nil {
			continue
		}
		directory, raw := item.Config.Labels["com.docker.compose.project.working_dir"], item.Config.Labels["com.docker.compose.project.config_files"]
		if directory == "" || raw == "" || raw == "-" {
			continue
		}
		directory = path.Clean(directory)
		files := []string{}
		for _, file := range strings.Split(raw, ",") {
			file = strings.TrimSpace(file)
			if path.IsAbs(file) {
				file = strings.TrimPrefix(file, directory+"/")
			}
			if !safeProjectFile(file) {
				return snapshot, ErrInvalidSource
			}
			files = append(files, file)
		}
		if labelDirectory != "" && (labelDirectory != directory || !slices.Equal(labelFiles, files)) {
			return snapshot, fmt.Errorf("%w: containers disagree on their Compose paths", ErrConflict)
		}
		labelDirectory, labelFiles = directory, files
	}
	if labelDirectory != "" {
		snapshot.Directory, snapshot.ComposeFiles = labelDirectory, labelFiles
	}
	if len(snapshot.ComposeFiles) == 0 {
		project, managed, err := m.data.ManagedProject(name)
		if err != nil {
			return snapshot, err
		}
		if managed && project.ProjectDir != "" {
			snapshot.Directory = project.ProjectDir
			snapshot.ComposeFiles = []string{"compose.yml"}
		}
	}
	if snapshot.Directory == "" || len(snapshot.ComposeFiles) == 0 {
		return snapshot, ErrFilesMissing
	}
	names := append(slices.Clone(snapshot.ComposeFiles), ".env")
	snapshot.Files, err = readConfigurationFiles(ctx, snapshot.Directory, names)
	if err != nil {
		return snapshot, err
	}
	for _, file := range snapshot.ComposeFiles {
		content, found := snapshot.Files[file]
		if !found {
			return snapshot, ErrFilesMissing
		}
		envFiles, err := configurationEnvFiles(content)
		if err != nil {
			if allowInvalid {
				continue
			}
			return snapshot, err
		}
		for _, envFile := range envFiles {
			if !slices.Contains(names, envFile) {
				names = append(names, envFile)
			}
		}
	}
	if len(names) > len(snapshot.ComposeFiles)+1 {
		snapshot.Files, err = readConfigurationFiles(ctx, snapshot.Directory, names)
		if err != nil {
			return snapshot, err
		}
	}
	snapshot.Fingerprint = fileFingerprint(snapshot)
	if err := resolveFileSnapshot(ctx, &snapshot); err != nil {
		if !allowInvalid {
			return snapshot, err
		}
		snapshot.Problem = err.Error()
	}
	return snapshot, nil
}

// Hydration is transient: persisted managed records contain locations only.
func (m *Manager) hydrateFileProject(ctx context.Context, project store.ManagedProject) (store.ManagedProject, error) {
	state, found, err := m.data.FileConfiguration(project.Name)
	if err != nil {
		return project, err
	}
	if !found || state.State != "ready" {
		return project, nil
	}
	snapshot, err := m.readFileSnapshot(ctx, project.Name)
	if err != nil {
		return project, err
	}
	if len(snapshot.ComposeFiles) != 1 {
		return project, fmt.Errorf("%w: edit a multi-file project through its Compose file editors", ErrConflict)
	}
	project.YAML = snapshot.Files[snapshot.ComposeFiles[0]]
	project.VariablesJSON = "{}"
	env := map[string]string{}
	for name, content := range snapshot.Files {
		if !slices.Contains(snapshot.ComposeFiles, name) {
			env[name] = content
		}
	}
	encoded, _ := json.Marshal(env)
	project.EnvFilesJSON = string(encoded)
	return project, nil
}

func resolveFileSnapshot(ctx context.Context, snapshot *FileSnapshot) error {
	dir, err := os.MkdirTemp("", "nox-config-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, content := range snapshot.Files {
		if !safeProjectFile(name) || !utf8.ValidString(content) || len(content) > MaxSourceBytes {
			return ErrInvalidSource
		}
		destination := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(destination, []byte(content), 0600); err != nil {
			return err
		}
	}
	args := []string{"compose", "--ansi", "never", "-p", snapshot.Name, "--project-directory", dir}
	for _, file := range snapshot.ComposeFiles {
		args = append(args, "-f", filepath.Join(dir, filepath.FromSlash(file)))
	}
	args = append(args, "config", "--format", "json", "--no-path-resolution")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	resolved, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("%w: Compose validation failed; check the YAML and environment file syntax", ErrInvalidSource)
	}
	var model map[string]any
	if json.Unmarshal(resolved, &model) != nil {
		return ErrInvalidSource
	}
	services, ok := model["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return ErrInvalidSource
	}
	for name, raw := range services {
		service := raw.(map[string]any)
		if image, _ := service["image"].(string); image == "" {
			return fmt.Errorf("%w: service %s needs an explicit prebuilt image; builds are not performed", ErrInvalidSource, name)
		}
		// A development build definition does not prevent using its prebuilt
		// image. Strip it only from the private execution model, never host files.
		delete(service, "build")
		if volumes, ok := service["volumes"].([]any); ok {
			for _, raw := range volumes {
				volume, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if volume["type"] == "bind" {
					source, _ := volume["source"].(string)
					if !path.IsAbs(source) {
						volume["source"] = path.Join(snapshot.Directory, source)
					}
				}
			}
		}
	}
	snapshot.Resolved, _ = json.Marshal(model)
	snapshot.Fingerprint = fileFingerprint(*snapshot)
	return nil
}

func fileFingerprint(snapshot FileSnapshot) string {
	encoded, _ := json.Marshal(struct {
		Directory    string
		ComposeFiles []string
		Files        map[string]string
	}{snapshot.Directory, snapshot.ComposeFiles, snapshot.Files})
	return fingerprint(encoded)
}

const saveConfigurationScript = `set -eu
umask 077
mkdir /tmp/config
tar -x -C /tmp/config
host="$1"; shift
owner=$(stat -c '%u:%g' "$host")
if [ "${owner%%:*}" = 0 ]; then owner=$(stat -c '%u:%g' "$(dirname "$host")"); fi
p="$host"; while [ "$p" != / ]; do [ ! -L "$p" ] || exit 73; p=$(dirname "$p"); done
for file do
 p="$file"; while [ "$p" != . ]; do [ ! -L "$host/$p" ] || exit 73; p=$(dirname "$p"); done
 if [ -f "/tmp/config/old/$file" ]; then cmp -s "$host/$file" "/tmp/config/old/$file" || exit 73
 else [ ! -e "$host/$file" ] || exit 73; fi
done
chown "$owner" "$host"
for file do
 if [ -f "/tmp/config/new/$file" ]; then
  directory="$host/$(dirname "$file")"; mkdir -p "$directory"
  p="$directory"; while [ "$p" != "$host" ]; do if [ "$(stat -c %u "$p")" = 0 ]; then chown "$owner" "$p"; fi; p=$(dirname "$p"); done
  temporary=$(mktemp "$directory/.nox-config-XXXXXX")
  cat "/tmp/config/new/$file" > "$temporary"
  if [ -f "$host/$file" ]; then chmod "$(stat -c %a "$host/$file")" "$temporary"; fileowner=$(stat -c '%u:%g' "$host/$file"); if [ "${fileowner%%:*}" = 0 ]; then fileowner="$owner"; fi; chown "$fileowner" "$temporary"
  else chown "$owner" "$temporary"; fi
  mv -f "$temporary" "$host/$file"
 fi
done`

func saveConfigurationFiles(ctx context.Context, before, after FileSnapshot) error {
	if before.Directory != after.Directory {
		return ErrInvalidSource
	}
	names := []string{}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, files := range []map[string]string{before.Files, after.Files} {
		for name := range files {
			if !safeProjectFile(name) {
				return ErrInvalidSource
			}
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	for version, files := range map[string]map[string]string{"old": before.Files, "new": after.Files} {
		for name, content := range files {
			if err := tw.WriteHeader(&tar.Header{Name: version + "/" + name, Mode: 0600, Size: int64(len(content))}); err != nil {
				return err
			}
			if _, err := tw.Write([]byte(content)); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	image, err := helperImage(ctx)
	if err != nil {
		return err
	}
	args := helperCommandArgs("-i", "--network", "none", "--mount", "type=bind,source="+path.Dir(before.Directory)+",target="+path.Dir(before.Directory), "--entrypoint", "sh", image, "-c", saveConfigurationScript, "sh", before.Directory)
	args = append(args, names...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = &archive
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: files changed or cannot be safely written; reload the current files", ErrConflict)
	}
	return nil
}

func (m *Manager) configurationReady(name string) error {
	state, found, err := m.data.FileConfiguration(name)
	if err != nil {
		return err
	}
	if found && state.State != "ready" {
		return fmt.Errorf("%w: project configuration is %s; finish migration before editing or recreating", ErrConflict, state.State)
	}
	return nil
}

func (m *Manager) Configuration(ctx context.Context, name string) (FileConfigurationView, error) {
	view := FileConfigurationView{State: "ready", Files: []ConfigurationFile{}, Variables: []ConfigurationVariable{}}
	if state, found, err := m.data.FileConfiguration(name); err != nil {
		return view, err
	} else if found && state.State != "ready" {
		view.State, view.Error = state.State, state.Error
		return view, nil
	}
	if project, legacy, err := m.data.ManagedProject(name); err != nil {
		return view, err
	} else if legacy && project.YAML != "" {
		view.State = "migrating"
		_ = m.data.SetFileConfiguration(store.FileConfiguration{Name: name, Directory: project.ProjectDir, ComposeFiles: `["compose.yml"]`, State: "migrating"})
		return view, nil
	}
	snapshot, err := m.readFileSnapshotMode(ctx, name, true)
	if errors.Is(err, ErrFilesMissing) {
		return view, nil
	}
	if err != nil {
		return view, err
	}
	view.Available = true
	view.Directory = snapshot.Directory
	view.Fingerprint = snapshot.Fingerprint
	view.Error = snapshot.Problem
	names := []string{}
	for file := range snapshot.Files {
		names = append(names, file)
	}
	sort.Strings(names)
	for _, file := range names {
		view.Files = append(view.Files, ConfigurationFile{file, snapshot.Files[file], slices.Contains(snapshot.ComposeFiles, file)})
	}
	decoded, _ := decodedComposeModel(snapshot.Resolved)
	var model composeModel
	_ = json.Unmarshal(decoded, &model)
	runtime, err := m.runtime(ctx, name)
	if err != nil {
		return view, err
	}
	actual := map[string]map[string]string{}
	for _, item := range runtime {
		if item.Config == nil {
			continue
		}
		service := item.Config.Labels["com.docker.compose.service"]
		actual[service] = map[string]string{}
		for _, env := range item.Config.Env {
			k, v, _ := strings.Cut(env, "=")
			actual[service][k] = v
		}
	}
	for service, definition := range model.Services {
		for key, value := range definition.Environment {
			v := ""
			if value != nil {
				v = *value
			}
			runningValue, present := actual[service][key]
			view.Variables = append(view.Variables, ConfigurationVariable{key, v, service, !present || runningValue != v})
		}
	}
	sort.Slice(view.Variables, func(i, j int) bool {
		a, b := view.Variables[i], view.Variables[j]
		if a.Service == b.Service {
			return a.Name < b.Name
		}
		return a.Service < b.Service
	})
	// Persist only the discovered location, so stdin-generated Compose labels
	// after recreation do not lose the original filenames.
	encoded, _ := json.Marshal(snapshot.ComposeFiles)
	_ = m.data.SetFileConfiguration(store.FileConfiguration{Name: name, Directory: snapshot.Directory, ComposeFiles: string(encoded), State: "ready"})
	return view, nil
}

func (m *Manager) SaveConfiguration(ctx context.Context, name string, input ConfigurationEdit) (FileConfigurationView, error) {
	if err := m.configurationReady(name); err != nil {
		return FileConfigurationView{}, err
	}
	active, err := m.data.Jobs(ctx, "compose:"+name, true)
	if err != nil {
		return FileConfigurationView{}, err
	}
	owned, _ := jobs.FromContext(ctx)
	for _, job := range active {
		if job.ID != owned.ID {
			return FileConfigurationView{}, store.ErrOperationConflict
		}
	}
	before, err := m.readFileSnapshotMode(ctx, name, true)
	if err != nil {
		return FileConfigurationView{}, err
	}
	if before.Fingerprint != input.Fingerprint {
		return FileConfigurationView{}, fmt.Errorf("%w: configuration changed; reload before saving", ErrConflict)
	}
	after := before
	after.Files = map[string]string{}
	for file, content := range before.Files {
		after.Files[file] = content
	}
	if input.Variable != "" {
		if err := editConfigurationVariable(&after, input.Service, input.Variable, input.Content); err != nil {
			return FileConfigurationView{}, err
		}
	} else {
		if _, found := after.Files[input.Path]; !found {
			return FileConfigurationView{}, ErrInvalidSource
		}
		after.Files[input.Path] = input.Content
	}
	// A Compose edit may introduce another existing env_file. Include its
	// current bytes in the compare-before-write check as well.
	for _, file := range after.ComposeFiles {
		names, err := configurationEnvFiles(after.Files[file])
		if err != nil {
			return FileConfigurationView{}, err
		}
		for _, name := range names {
			if _, found := after.Files[name]; !found {
				files, err := readConfigurationFiles(ctx, after.Directory, []string{name})
				if err != nil {
					return FileConfigurationView{}, err
				}
				if content, exists := files[name]; exists {
					before.Files[name] = content
					after.Files[name] = content
				}
			}
		}
	}
	if err := resolveFileSnapshot(ctx, &after); err != nil {
		return FileConfigurationView{}, err
	}
	if input.Variable != "" {
		definition := filePreview(after).model.Services[input.Service]
		value := definition.Environment[input.Variable]
		if value == nil || *value != input.Content {
			return FileConfigurationView{}, fmt.Errorf("%w: this variable definition could not preserve the requested value; edit its file directly", ErrInvalidSource)
		}
	}
	if err := saveConfigurationFiles(ctx, before, after); err != nil {
		return FileConfigurationView{}, err
	}
	if m.changes != nil {
		m.changes.Notify(inventory.Change{Inventory: true})
	}
	return m.Configuration(ctx, name)
}

func editConfigurationVariable(snapshot *FileSnapshot, service, name, value string) error {
	if !variableNamePattern.MatchString(name) {
		return ErrInvalidSource
	}
	// A service-specific override is unambiguous even when several services
	// share an env_file or use a compound interpolation expression.
	for i := len(snapshot.ComposeFiles) - 1; i >= 0; i-- {
		file := snapshot.ComposeFiles[i]
		var root yaml.Node
		if yaml.Unmarshal([]byte(snapshot.Files[file]), &root) != nil || len(root.Content) == 0 {
			return ErrInvalidSource
		}
		mapping := root.Content[0]
		services := yamlMappingValue(mapping, "services")
		definition := yamlMappingValue(services, service)
		if definition == nil {
			continue
		}
		env := yamlMappingValue(definition, "environment")
		inherited := env != nil && (env.Kind == yaml.AliasNode || yamlMappingValue(env, "<<") != nil)
		if env != nil && env.Kind == yaml.AliasNode {
			alias := *env
			*env = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!merge", Value: "<<"}, &alias}}
		}
		if env != nil && env.Kind == yaml.SequenceNode {
			next := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			for _, entry := range env.Content {
				k, v, present := strings.Cut(entry.Value, "=")
				tag := "!!str"
				if !present {
					tag = "!!null"
				}
				next.Content = append(next.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: v})
			}
			*env = *next
		}
		if existing := yamlMappingValue(env, name); existing != nil && existing.Kind == yaml.ScalarNode {
			matches := interpolationPattern.FindStringSubmatch(existing.Value)
			if len(matches) > 1 && matches[0] == existing.Value {
				snapshot.Files[".env"] = setDotenvValue(snapshot.Files[".env"], matches[1], value)
				return nil
			}
		}
		if yamlMappingValue(env, name) == nil && !inherited {
			var raw any
			if files := yamlMappingValue(definition, "env_file"); files != nil && files.Decode(&raw) == nil {
				paths := []string{}
				rawPaths := map[string]bool{}
				switch v := raw.(type) {
				case string:
					paths = append(paths, v)
				case []any:
					for _, entry := range v {
						switch v := entry.(type) {
						case string:
							paths = append(paths, v)
						case map[string]any:
							if p, ok := v["path"].(string); ok {
								paths = append(paths, p)
								rawPaths[p] = v["format"] == "raw"
							}
						}
					}
				}
				for i := len(paths) - 1; i >= 0; i-- {
					file := paths[i]
					if envFileDefines(snapshot.Files[file], name) {
						if rawPaths[file] {
							if strings.ContainsAny(value, "\r\n") {
								break
							}
							lines := strings.Split(snapshot.Files[file], "\n")
							for j := len(lines) - 1; j >= 0; j-- {
								key, _, found := strings.Cut(lines[j], "=")
								if found && strings.TrimSpace(key) == name {
									lines[j] = name + "=" + value
									break
								}
							}
							snapshot.Files[file] = strings.Join(lines, "\n")
							return nil
						}
						snapshot.Files[file] = setDotenvValue(snapshot.Files[file], name, value)
						return nil
					}
				}
			}
		}
		if env == nil {
			env = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			definition.Content = append(definition.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "environment"}, env)
		}
		if env.Kind != yaml.MappingNode {
			return ErrInvalidSource
		}
		v := yamlMappingValue(env, name)
		if v == nil {
			v = &yaml.Node{}
			env.Content = append(env.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, v)
		}
		*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: strings.ReplaceAll(value, "$", "$$"), Style: yaml.DoubleQuotedStyle}
		var encoded bytes.Buffer
		encoder := yaml.NewEncoder(&encoded)
		encoder.SetIndent(2)
		if err := encoder.Encode(&root); err != nil {
			return err
		}
		_ = encoder.Close()
		snapshot.Files[file] = encoded.String()
		return nil
	}
	return fmt.Errorf("%w: service was not found in the Compose files", ErrInvalidSource)
}

func setDotenvValue(content, key, value string) string {
	encoded := key + "=" + dotenvLiteral(value)
	lines := strings.Split(content, "\n")
	start, end := dotenvDefinition(lines, key)
	if start >= 0 {
		lines = append(append(lines[:start:start], encoded), lines[end+1:]...)
		return strings.Join(lines, "\n")
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + encoded + "\n"
}

// Locate the effective (last) assignment, skipping quoted multiline values.
// Earlier duplicate assignments remain untouched, as do unrelated comments.
func dotenvDefinition(lines []string, key string) (int, int) {
	start, end := -1, -1
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, rest, found := strings.Cut(line, "=")
		if !found {
			name, rest, found = strings.Cut(line, ":")
		}
		if !found {
			continue
		}
		first := i
		rest = strings.TrimSpace(rest)
		if len(rest) > 0 && (rest[0] == '\'' || rest[0] == '"') {
			quote, escaped, closed := rest[0], false, false
			for part := rest[1:]; ; part = lines[i] {
				for j := 0; j < len(part); j++ {
					if escaped {
						escaped = false
						continue
					}
					if part[j] == '\\' {
						escaped = true
						continue
					}
					if part[j] == quote {
						closed = true
						break
					}
				}
				if closed || i+1 >= len(lines) {
					break
				}
				i++
				escaped = false
			}
		}
		if strings.TrimSpace(name) == key {
			start, end = first, i
		}
	}
	return start, end
}
func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
