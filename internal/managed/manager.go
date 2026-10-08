package managed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
)

var ErrConflict = errors.New("managed project conflict")

type Request struct {
	Name        string            `json:"name"`
	Source      SourceInput       `json:"source"`
	Variables   map[string]string `json:"variables"`
	EnvFiles    map[string]string `json:"envFiles"`
	Mode        string            `json:"mode"`
	Fingerprint string            `json:"fingerprint,omitempty"`
	ProjectDir  string            `json:"projectDir,omitempty"`
}

type ProjectPreview struct {
	Preview
	ProjectDir         string   `json:"projectDir"`
	SourceKind         string   `json:"sourceKind"`
	SourceURL          string   `json:"sourceURL,omitempty"`
	Duplicates         []string `json:"duplicates"`
	ExternalMatch      bool     `json:"externalMatch"`
	Changes            []string `json:"changes"`
	Mode               string   `json:"mode"`
	AdoptionDir        string   `json:"adoptionDir,omitempty"`
	filesFingerprint   string
	runtimeFingerprint string
}

type Manager struct {
	data          *store.Store
	inventory     inventory.Reader
	changes       *inventory.Notifier
	runtime       runtimeReader
	imageDefaults func(context.Context, string) (imageDefaults, error)
	adoptionFiles func(context.Context, string, string, map[string]string, string, bool) (string, error)
	launch        func(context.Context, *store.Store, store.Job, string) error
}

func (m *Manager) SetNotifier(changes *inventory.Notifier) { m.changes = changes }

func NewManager(data *store.Store, reader inventory.Reader) (*Manager, error) {
	return &Manager{data: data, inventory: reader, runtime: readProjectRuntime, imageDefaults: readImageDefaults, adoptionFiles: adoptionFiles, launch: jobs.Launch}, nil
}

func (m *Manager) ProjectsBase() (string, error) { return m.data.ProjectsBase() }

func (m *Manager) SetProjectsBase(ctx context.Context, value string) (string, error) {
	base, err := NormalizeProjectsBase(value)
	if err != nil {
		return "", err
	}
	if err := checkHostBase(ctx, base); err != nil {
		return "", err
	}
	return base, m.data.SetProjectsBase(base)
}

func (m *Manager) Preview(ctx context.Context, input Request) (ProjectPreview, Source, error) {
	if !projectNamePattern.MatchString(input.Name) {
		return ProjectPreview{}, Source{}, fmt.Errorf("%w: invalid project name", ErrInvalidSource)
	}
	if input.ProjectDir != "" && input.Mode != "adopt" {
		return ProjectPreview{}, Source{}, fmt.Errorf("%w: an explicit project directory is only supported for adoption", ErrInvalidSource)
	}
	snapshot, err := m.inventory.Snapshot(ctx)
	if err != nil {
		return ProjectPreview{}, Source{}, fmt.Errorf("cannot inspect existing Docker projects: %w", err)
	}
	storedProject, isManaged, err := m.data.ManagedProject(input.Name)
	if err != nil {
		return ProjectPreview{}, Source{}, err
	}
	external := false
	for _, project := range snapshot.Projects {
		if project.ID == "compose:"+input.Name && !isManaged {
			external = true
		}
	}
	var runtime []container.InspectResponse
	adoptDir, runtimeSignature := "", ""
	if external {
		runtime, err = m.runtime(ctx, input.Name)
		if err != nil || len(runtime) == 0 {
			return ProjectPreview{}, Source{}, fmt.Errorf("%w: existing project could not be inspected", ErrInvalidSource)
		}
		adoptDir, err = adoptionDirectory(runtime, input.ProjectDir)
		if err != nil && input.Mode == "adopt" {
			return ProjectPreview{}, Source{}, err
		}
		runtimeSignature = adoptionRuntimeFingerprint(runtime)
	}
	projectDir := ""
	if input.Mode == "new" || input.Mode == "copy" {
		base, err := m.data.ProjectsBase()
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		if base == "" && !external {
			return ProjectPreview{}, Source{}, fmt.Errorf("%w: set the projects directory in Settings before creating a project", ErrInvalidSource)
		}
		if base != "" {
			projectDir = path.Join(base, input.Name)
		}
	} else if input.Mode == "sync" {
		old, found, err := m.data.ManagedProject(input.Name)
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		if found {
			if old.ProjectDir == "" {
				return ProjectPreview{}, Source{}, fmt.Errorf("%w: saved host project directory is missing; directory recovery is required before sync", ErrInvalidSource)
			}
			projectDir = old.ProjectDir
		}
	} else if input.Mode == "adopt" {
		if !external {
			return ProjectPreview{}, Source{}, fmt.Errorf("%w: no external project is available for adoption", ErrConflict)
		}
		projectDir = adoptDir
	}
	if input.Mode == "sync" && isManaged {
		runtime, err = m.runtime(ctx, input.Name)
		if err != nil {
			return ProjectPreview{}, Source{}, fmt.Errorf("%w: existing project could not be inspected before sync", ErrInvalidSource)
		}
		runtimeSignature = adoptionRuntimeFingerprint(runtime)
	}
	source, err := LoadSource(ctx, input.Source)
	if err != nil {
		return ProjectPreview{}, Source{}, err
	}
	preview, err := Validate(ctx, input.Name, source, input.Variables, input.EnvFiles, projectDir)
	if err != nil {
		return ProjectPreview{}, Source{}, err
	}
	result := ProjectPreview{Preview: preview, ProjectDir: projectDir, SourceKind: source.Kind, SourceURL: source.URL, Duplicates: []string{}, Changes: []string{}, Mode: input.Mode, ExternalMatch: external, AdoptionDir: adoptDir, runtimeFingerprint: runtimeSignature}
	if source.URL != "" {
		matches, err := m.data.ManagedByURL(source.URL)
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		for _, match := range matches {
			result.Duplicates = append(result.Duplicates, match.Name)
			if match.Name == input.Name && input.Mode == "sync" {
				result.Changes = append(result.Changes, describeSourceChanges(ctx, input.Name, match, source, input.Variables, input.EnvFiles, projectDir, preview)...)
			}
		}
	}
	imageSignature := ""
	if external {
		defaults := map[string]imageDefaults{}
		for _, service := range preview.model.Services {
			if _, found := defaults[service.Image]; found {
				continue
			}
			value, err := m.imageDefaults(ctx, service.Image)
			if err != nil {
				return ProjectPreview{}, Source{}, fmt.Errorf("%w: image defaults are unavailable; pull the proposed images explicitly before adoption", ErrInvalidSource)
			}
			defaults[service.Image] = value
		}
		comparisonDir := adoptDir
		if comparisonDir == "" {
			comparisonDir = projectDir
		}
		changes, err := compareAdoption(input.Name, comparisonDir, preview.model, runtime, defaults)
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		result.Changes = append(result.Changes, changes...)
		encoded, _ := json.Marshal(defaults)
		imageSignature = fingerprint(encoded)
		if input.Mode == "adopt" {
			if err := verifyAdoptionBinds(projectDir, preview.model, runtime); err != nil {
				return ProjectPreview{}, Source{}, err
			}
			result.filesFingerprint, err = m.adoptionFiles(ctx, projectDir, source.YAML, input.EnvFiles, "", false)
			if err != nil {
				return ProjectPreview{}, Source{}, err
			}
		}
	}
	sort.Strings(result.Changes)
	managedSignature := ""
	if isManaged {
		encoded, _ := json.Marshal(storedProject)
		managedSignature = fingerprint(encoded)
	}
	signature, _ := json.Marshal(struct {
		Fingerprint string
		Mode        string
		Changes     []string
		External    bool
		Runtime     string
		Images      string
		Files       string
		Duplicates  []string
		Managed     string
	}{preview.Fingerprint, input.Mode, result.Changes, external, runtimeSignature, imageSignature, result.filesFingerprint, result.Duplicates, managedSignature})
	result.Fingerprint = fingerprint(signature)
	return result, source, nil
}

func describeSourceChanges(ctx context.Context, name string, old store.ManagedProject, source Source, variables, envFiles map[string]string, projectDir string, next Preview) []string {
	changes := []string{}
	if old.YAML != source.YAML {
		changes = append(changes, "Compose YAML changed")
	}
	encoded, _ := json.Marshal(variables)
	if old.VariablesJSON != string(encoded) {
		changes = append(changes, "Interpolation variables changed")
	}
	encodedEnv, _ := json.Marshal(envFiles)
	if old.EnvFilesJSON != string(encodedEnv) {
		changes = append(changes, "Environment files changed")
	}
	if old.ProjectDir != projectDir {
		changes = append(changes, "Project directory changed")
	}
	previousVariables := map[string]string{}
	previousEnvFiles := map[string]string{}
	if json.Unmarshal([]byte(old.VariablesJSON), &previousVariables) != nil || json.Unmarshal([]byte(old.EnvFilesJSON), &previousEnvFiles) != nil {
		return changes
	}
	previous, err := Validate(ctx, name, Source{YAML: old.YAML}, previousVariables, previousEnvFiles, old.ProjectDir)
	if err != nil {
		return append(changes, "Previous configuration could not be compared")
	}
	oldServices := map[string]Service{}
	for _, service := range previous.Services {
		oldServices[service.Name] = service
	}
	for _, service := range next.Services {
		prior, found := oldServices[service.Name]
		if !found {
			changes = append(changes, "Service added: "+service.Name)
		} else {
			if prior.Image != service.Image {
				changes = append(changes, "Image changed for "+service.Name)
			}
			if !sameStrings(prior.Ports, service.Ports) {
				changes = append(changes, "Ports changed for "+service.Name)
			}
			if !sameStrings(prior.Volumes, service.Volumes) {
				changes = append(changes, "Mounts changed for "+service.Name)
			}
			if !sameStrings(prior.Networks, service.Networks) {
				changes = append(changes, "Networks changed for "+service.Name)
			}
		}
		delete(oldServices, service.Name)
	}
	for name := range oldServices {
		changes = append(changes, "Service removed: "+name)
	}
	if !sameStrings(previous.Volumes, next.Volumes) {
		changes = append(changes, "Named volumes changed")
	}
	if !sameStrings(previous.Networks, next.Networks) {
		changes = append(changes, "Project networks changed")
	}
	return changes
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *Manager) Submit(ctx context.Context, input Request) (store.ManagedJob, error) {
	if input.Name == "nox-yard" {
		return store.ManagedJob{}, fmt.Errorf("%w: NoX Yard cannot manage itself", ErrConflict)
	}
	preview, source, err := m.Preview(ctx, input)
	if err != nil {
		return store.ManagedJob{}, err
	}
	if input.Fingerprint == "" || input.Fingerprint != preview.Fingerprint {
		return store.ManagedJob{}, fmt.Errorf("%w: preview changed; review again", ErrConflict)
	}
	existing, managed, err := m.data.ManagedProject(input.Name)
	if err != nil {
		return store.ManagedJob{}, err
	}
	switch input.Mode {
	case "new":
		if managed || preview.ExternalMatch || len(preview.Duplicates) > 0 {
			return store.ManagedJob{}, fmt.Errorf("%w: choose copy, sync, or adoption after reviewing the existing source", ErrConflict)
		}
	case "copy":
		if managed || preview.ExternalMatch || source.URL == "" || len(preview.Duplicates) == 0 {
			return store.ManagedJob{}, fmt.Errorf("%w: a separate copy needs a new name and a known URL", ErrConflict)
		}
	case "sync":
		if !managed || source.URL == "" || existing.SourceURL != source.URL {
			return store.ManagedJob{}, fmt.Errorf("%w: sync requires the matching managed URL", ErrConflict)
		}
	case "adopt":
		if managed || !preview.ExternalMatch {
			return store.ManagedJob{}, fmt.Errorf("%w: no external project is available for adoption", ErrConflict)
		}
	default:
		return store.ManagedJob{}, fmt.Errorf("%w: choose a project action", ErrInvalidSource)
	}
	variablesJSON, _ := json.Marshal(input.Variables)
	envFilesJSON, _ := json.Marshal(input.EnvFiles)
	projectRecord := store.ManagedProject{Name: input.Name, SourceKind: source.Kind, SourceURL: source.URL, Filename: source.Filename,
		YAML: source.YAML, VariablesJSON: string(variablesJSON), EnvFilesJSON: string(envFilesJSON), ProjectDir: preview.ProjectDir}
	return m.enqueue(ctx, projectRecord, input.Mode, workerPayload{Project: projectRecord, RuntimeFingerprint: preview.runtimeFingerprint, FilesFingerprint: preview.filesFingerprint})
}

func composeCommand(ctx context.Context, name, content string, variables, envFiles map[string]string, hostProjectDir string, args ...string) error {
	if hostProjectDir != "" {
		return runHostCompose(ctx, name, hostProjectDir, variables, args...)
	}
	composePath, cleanup, err := prepareCompose(content, envFiles)
	if err != nil {
		return err
	}
	defer cleanup()
	commandArgs := append([]string{"compose", "--ansi", "never", "-p", name, "-f", composePath}, args...)
	command := exec.CommandContext(ctx, "docker", commandArgs...)
	command.Dir = filepath.Dir(composePath)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	for key, value := range variables {
		command.Env = append(command.Env, key+"="+value)
	}
	return command.Run()
}

func (m *Manager) Job(id string) (store.ManagedJob, bool, error) {
	return m.data.ManagedJob(id)
}

func (m *Manager) Operation(name, operation string, removeVolumes bool) (store.ManagedJob, error) {
	if name == "nox-yard" {
		return store.ManagedJob{}, fmt.Errorf("%w: NoX Yard cannot manage itself", ErrConflict)
	}
	allowed := map[string]bool{"start": true, "stop": true, "restart": true, "pull": true, "update": true, "remove": true}
	if !allowed[operation] {
		return store.ManagedJob{}, fmt.Errorf("%w: invalid operation", ErrInvalidSource)
	}
	project, found, err := m.data.ManagedProject(name)
	if err != nil {
		return store.ManagedJob{}, err
	}
	if !found {
		return store.ManagedJob{}, fmt.Errorf("%w: managed project not found", ErrConflict)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return m.enqueue(ctx, project, operation, workerPayload{Project: project, RemoveVolumes: removeVolumes})
}

func fingerprint(data []byte) string {
	// A fingerprint is a confirmation token, not a secret.
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
