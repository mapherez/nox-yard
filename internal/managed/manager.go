package managed

import (
	"context"
	"crypto/rand"
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
	"strings"
	"sync"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

var ErrConflict = errors.New("managed project conflict")

type Request struct {
	Name        string            `json:"name"`
	Source      SourceInput       `json:"source"`
	Variables   map[string]string `json:"variables"`
	EnvFiles    map[string]string `json:"envFiles"`
	Mode        string            `json:"mode"`
	Fingerprint string            `json:"fingerprint,omitempty"`
}

type ProjectPreview struct {
	Preview
	ProjectDir    string   `json:"projectDir"`
	SourceKind    string   `json:"sourceKind"`
	SourceURL     string   `json:"sourceURL,omitempty"`
	Duplicates    []string `json:"duplicates"`
	ExternalMatch bool     `json:"externalMatch"`
	Changes       []string `json:"changes"`
	Mode          string   `json:"mode"`
}

type Manager struct {
	data      *store.Store
	inventory inventory.Reader
	mu        sync.Mutex
	active    map[string]bool
	changes   *inventory.Notifier
}

func (m *Manager) SetNotifier(changes *inventory.Notifier) { m.changes = changes }

func (m *Manager) beginChange(name, operation string) func() {
	if m.changes == nil {
		return func() {}
	}
	return m.changes.Begin("compose:"+name, operation)
}

func NewManager(data *store.Store, reader inventory.Reader) (*Manager, error) {
	if err := data.InterruptManagedJobs(); err != nil {
		return nil, err
	}
	return &Manager{data: data, inventory: reader, active: make(map[string]bool)}, nil
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
	projectDir := ""
	if input.Mode == "new" || input.Mode == "copy" {
		base, err := m.data.ProjectsBase()
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		if base == "" {
			return ProjectPreview{}, Source{}, fmt.Errorf("%w: set the projects directory in Settings before creating a project", ErrInvalidSource)
		}
		projectDir = path.Join(base, input.Name)
	} else if input.Mode == "sync" {
		old, found, err := m.data.ManagedProject(input.Name)
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		if found {
			projectDir = old.ProjectDir
		}
	}
	source, err := LoadSource(ctx, input.Source)
	if err != nil {
		return ProjectPreview{}, Source{}, err
	}
	preview, err := Validate(ctx, input.Name, source, input.Variables, input.EnvFiles, projectDir)
	if err != nil {
		return ProjectPreview{}, Source{}, err
	}
	result := ProjectPreview{Preview: preview, ProjectDir: projectDir, SourceKind: source.Kind, SourceURL: source.URL, Duplicates: []string{}, Changes: []string{}, Mode: input.Mode}
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
	snapshot, err := m.inventory.Snapshot(ctx)
	if err != nil {
		return ProjectPreview{}, Source{}, fmt.Errorf("cannot inspect existing Docker projects: %w", err)
	}
	for _, project := range snapshot.Projects {
		if project.ID != "compose:"+input.Name {
			continue
		}
		_, managed, err := m.data.ManagedProject(input.Name)
		if err != nil {
			return ProjectPreview{}, Source{}, err
		}
		result.ExternalMatch = !managed
		if result.ExternalMatch {
			changes, err := compareExternal(ctx, m.inventory, project, preview)
			if err != nil {
				return ProjectPreview{}, Source{}, fmt.Errorf("cannot compare existing project: %w", err)
			}
			result.Changes = append(result.Changes, changes...)
		}
	}
	sort.Strings(result.Changes)
	signature, _ := json.Marshal(struct {
		Fingerprint string
		Mode        string
		Changes     []string
	}{preview.Fingerprint, input.Mode, result.Changes})
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

func compareExternal(ctx context.Context, reader inventory.Reader, project inventory.Project, preview Preview) ([]string, error) {
	oldServices := map[string]inventory.Container{}
	for _, container := range project.Containers {
		if container.Service != "" {
			oldServices[container.Service] = container
		}
	}
	changes := []string{}
	for _, service := range preview.Services {
		container, exists := oldServices[service.Name]
		if !exists {
			changes = append(changes, "New service: "+service.Name)
		} else {
			if container.Image != service.Image {
				changes = append(changes, "Image differs for "+service.Name)
			}
			inspection, err := reader.InspectContainer(ctx, container.ID, false)
			if err != nil {
				return nil, err
			}
			for _, port := range service.Ports {
				matched := false
				for _, current := range inspection.Ports {
					if current.HostPort+":"+current.ContainerPort == port {
						matched = true
					}
				}
				if !matched {
					changes = append(changes, "Published port differs for "+service.Name+": "+port)
				}
			}
			for _, volume := range service.Volumes {
				source, target, _ := strings.Cut(volume, ":")
				matched := false
				for _, current := range inspection.Mounts {
					if current.Destination == target && (current.Source == source || current.Source == project.Name+"_"+source) {
						matched = true
					}
				}
				if !matched {
					changes = append(changes, "Mount differs for "+service.Name+": "+volume)
				}
			}
			for _, network := range service.Networks {
				matched := false
				for _, current := range inspection.Networks {
					if current.Name == network || current.Name == project.Name+"_"+network {
						matched = true
					}
				}
				if !matched {
					changes = append(changes, "Network differs for "+service.Name+": "+network)
				}
			}
		}
		delete(oldServices, service.Name)
	}
	for service := range oldServices {
		changes = append(changes, "Existing service absent from source: "+service)
	}
	return changes, nil
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
	m.mu.Lock()
	if m.active[input.Name] {
		m.mu.Unlock()
		return store.ManagedJob{}, fmt.Errorf("%w: project operation already running", ErrConflict)
	}
	m.active[input.Name] = true
	m.mu.Unlock()
	release := func() {
		m.mu.Lock()
		delete(m.active, input.Name)
		m.mu.Unlock()
	}
	variablesJSON, _ := json.Marshal(input.Variables)
	envFilesJSON, _ := json.Marshal(input.EnvFiles)
	projectRecord := store.ManagedProject{
		Name: input.Name, SourceKind: source.Kind, SourceURL: source.URL, Filename: source.Filename,
		YAML: source.YAML, VariablesJSON: string(variablesJSON), EnvFilesJSON: string(envFilesJSON), ProjectDir: preview.ProjectDir,
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		release()
		return store.ManagedJob{}, err
	}
	job := store.ManagedJob{ID: hex.EncodeToString(bytes[:]), ProjectName: input.Name, Operation: input.Mode, Status: "running", CreatedAt: time.Now().Unix()}
	if err := m.data.CreateManagedJob(job); err != nil {
		release()
		return store.ManagedJob{}, err
	}
	if input.Mode == "new" || input.Mode == "copy" {
		if err := m.data.SaveManagedProject(projectRecord); err != nil {
			_ = m.data.FinishManagedJob(job.ID, "failed", "Project configuration could not be saved.")
			release()
			return store.ManagedJob{}, err
		}
	}
	finish := m.beginChange(input.Name, input.Mode)
	go func() {
		defer finish()
		defer release()
		if input.Mode == "adopt" {
			m.finishWithProject(job.ID, projectRecord, nil)
			return
		}
		jobCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if projectRecord.ProjectDir != "" {
			if err := writeHostCompose(jobCtx, projectRecord.ProjectDir, source.YAML, input.EnvFiles, input.Mode != "sync"); err != nil {
				_ = m.data.FinishManagedJob(job.ID, "failed", err.Error())
				return
			}
		}
		if err := composeCommand(jobCtx, input.Name, source.YAML, input.Variables, input.EnvFiles, preview.ProjectDir, "pull"); err == nil {
			err = composeCommand(jobCtx, input.Name, source.YAML, input.Variables, input.EnvFiles, preview.ProjectDir, "up", "-d", "--no-build")
			if err == nil {
				m.finishWithProject(job.ID, projectRecord, nil)
				return
			}
		}
		_ = m.data.FinishManagedJob(job.ID, "failed", "Docker Compose could not complete the operation. Inspect the host Docker logs and retry.")
	}()
	return job, nil
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
	m.mu.Lock()
	if m.active[name] {
		m.mu.Unlock()
		return store.ManagedJob{}, fmt.Errorf("%w: project operation already running", ErrConflict)
	}
	m.active[name] = true
	m.mu.Unlock()
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		m.release(name)
		return store.ManagedJob{}, err
	}
	job := store.ManagedJob{ID: hex.EncodeToString(id[:]), ProjectName: name, Operation: operation, Status: "running", CreatedAt: time.Now().Unix()}
	if err := m.data.CreateManagedJob(job); err != nil {
		m.release(name)
		return store.ManagedJob{}, err
	}
	finish := m.beginChange(name, operation)
	go func() {
		defer finish()
		defer m.release(name)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		variables := map[string]string{}
		if err := json.Unmarshal([]byte(project.VariablesJSON), &variables); err != nil {
			_ = m.data.FinishManagedJob(job.ID, "failed", "Saved variables could not be read.")
			return
		}
		envFiles := map[string]string{}
		if err := json.Unmarshal([]byte(project.EnvFilesJSON), &envFiles); err != nil {
			_ = m.data.FinishManagedJob(job.ID, "failed", "Saved environment files could not be read.")
			return
		}
		args := []string{}
		switch operation {
		case "start":
			args = []string{"up", "-d", "--no-build"}
		case "stop":
			args = []string{"stop"}
		case "restart":
			args = []string{"restart"}
		case "pull":
			args = []string{"pull"}
		case "remove":
			args = []string{"down"}
			if removeVolumes {
				args = append(args, "--volumes")
			}
		case "update":
			if err := composeCommand(ctx, name, project.YAML, variables, envFiles, project.ProjectDir, "pull"); err != nil {
				_ = m.data.FinishManagedJob(job.ID, "failed", "Image pull failed.")
				return
			}
			args = []string{"up", "-d", "--no-build", "--force-recreate"}
		}
		if err := composeCommand(ctx, name, project.YAML, variables, envFiles, project.ProjectDir, args...); err != nil {
			_ = m.data.FinishManagedJob(job.ID, "failed", "Docker Compose could not complete the operation.")
			return
		}
		if operation == "remove" {
			if err := m.data.DeleteManagedProject(name); err != nil {
				_ = m.data.FinishManagedJob(job.ID, "failed", "Containers were removed, but project metadata could not be deleted.")
				return
			}
		}
		_ = m.data.FinishManagedJob(job.ID, "succeeded", "")
	}()
	return job, nil
}

func (m *Manager) release(name string) {
	m.mu.Lock()
	delete(m.active, name)
	m.mu.Unlock()
}

func fingerprint(data []byte) string {
	// A fingerprint is a confirmation token, not a secret.
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (m *Manager) finishWithProject(id string, project store.ManagedProject, operationErr error) {
	if operationErr == nil {
		operationErr = m.data.SaveManagedProject(project)
	}
	if operationErr != nil {
		_ = m.data.FinishManagedJob(id, "failed", operationErr.Error())
		return
	}
	_ = m.data.FinishManagedJob(id, "succeeded", "")
}
