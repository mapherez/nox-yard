package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

func materializedEnvironment(variables, files map[string]string) map[string]string {
	result := map[string]string{}
	for file, content := range files {
		result[file] = content
	}
	if len(variables) == 0 {
		return result
	}
	content := result[".env"]
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	keys := []string{}
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		content += key + "=" + dotenvLiteral(variables[key]) + "\n"
	}
	result[".env"] = content
	return result
}

func dotenvLiteral(value string) string { return strconv.Quote(strings.ReplaceAll(value, "$", "$$")) }

// StartConfigurationMigration is deliberately separate from store.Open: workers
// must be able to open the DB while the web instance migrates other projects.
// A live old worker keeps its original source until it has finished.
func (m *Manager) StartConfigurationMigration(ctx context.Context) {
	m.filesEnabled = true
	projects, err := m.data.ManagedProjects()
	if err != nil {
		return
	}
	for _, project := range projects {
		if _, found, _ := m.data.FileConfiguration(project.Name); !found {
			_ = m.data.SetFileConfiguration(store.FileConfiguration{Name: project.Name, Directory: project.ProjectDir, ComposeFiles: `["compose.yml"]`, State: "migrating"})
		}
	}
	go func() {
		m.repairDataOwnership(ctx)
		m.migrateConfigurations(ctx)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.migrateConfigurations(ctx)
			}
		}
	}()
}

func (m *Manager) migrateConfigurations(ctx context.Context) {
	m.migrationMu.Lock()
	defer m.migrationMu.Unlock()
	projects, err := m.data.ManagedProjects()
	if err != nil {
		return
	}
	for _, project := range projects {
		if ctx.Err() != nil {
			return
		}
		state, found, _ := m.data.FileConfiguration(project.Name)
		if found && (state.State == "ready" || state.State == "needs_attention") {
			continue
		}
		if !found {
			state = store.FileConfiguration{Name: project.Name, Directory: project.ProjectDir, ComposeFiles: `["compose.yml"]`, State: "migrating"}
			if err := m.data.SetFileConfiguration(state); err != nil {
				continue
			}
		}
		jobs, err := m.data.Jobs(ctx, "compose:"+project.Name, true)
		if err != nil || len(jobs) > 0 {
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 45*time.Second)
		err = m.migrateConfiguration(attempt, project, false)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			state.Name, state.Directory, state.ComposeFiles, state.State, state.Error = project.Name, project.ProjectDir, `["compose.yml"]`, "needs_attention", err.Error()
			_ = m.data.SetFileConfiguration(state)
		}
		if m.changes != nil {
			m.changes.Notify(inventory.Change{Inventory: true})
		}
	}
}

func (m *Manager) RetryConfigurationMigration(ctx context.Context, name string, useCurrentFiles bool) error {
	m.migrationMu.Lock()
	defer m.migrationMu.Unlock()
	if state, found, err := m.data.FileConfiguration(name); err != nil {
		return err
	} else if found && state.State == "ready" {
		return nil
	}
	project, found, err := m.data.ManagedProject(name)
	if err != nil {
		return err
	}
	if !found {
		return ErrInvalidSource
	}
	jobs, err := m.data.Jobs(ctx, "compose:"+name, true)
	if err != nil {
		return err
	}
	if len(jobs) > 0 {
		return store.ErrOperationConflict
	}
	err = m.migrateConfiguration(ctx, project, useCurrentFiles)
	if err != nil {
		_ = m.data.SetFileConfiguration(store.FileConfiguration{Name: name, Directory: project.ProjectDir, ComposeFiles: `["compose.yml"]`, State: "needs_attention", Error: err.Error()})
	}
	return err
}

func (m *Manager) migrateConfiguration(ctx context.Context, project store.ManagedProject, useCurrentFiles bool) error {
	discovered, err := m.readFileSnapshotMode(ctx, project.Name, true)
	if err != nil && !errors.Is(err, ErrFilesMissing) {
		return err
	}
	directory, composeFiles := discovered.Directory, discovered.ComposeFiles
	if directory == "" || len(composeFiles) == 0 {
		return fmt.Errorf("Original project directory is missing; locate the host Compose before migration.")
	}
	variables, env, err := projectEnvironment(project)
	if err != nil {
		return err
	}
	files := map[string]string{}
	if len(composeFiles) == 1 {
		files[composeFiles[0]] = project.YAML
	} else {
		for _, name := range composeFiles {
			content, exists := discovered.Files[name]
			if !exists {
				return fmt.Errorf("Original Compose files are missing; restore their files before migration.")
			}
			files[name] = content
		}
	}
	for name, value := range env {
		files[name] = value
	}
	names := append([]string{".env"}, composeFiles...)
	for name := range discovered.Files {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	for name := range files {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	actual, err := readConfigurationFiles(ctx, directory, names)
	if err != nil {
		return err
	}
	before := FileSnapshot{Name: project.Name, Directory: directory, ComposeFiles: composeFiles, Files: actual}
	after := before
	after.Files = map[string]string{}
	for name, value := range actual {
		after.Files[name] = value
	}
	materialized := materializedEnvironment(variables, env)
	if !useCurrentFiles {
		for name, value := range files {
			if observed, exists := actual[name]; exists && observed != value && observed != materialized[name] {
				return fmt.Errorf("Host files differ from the saved project. Preserve your edits and choose Use current files, or restore matching files and retry.")
			}
			after.Files[name] = value
		}
		for name, value := range materialized {
			if observed, exists := actual[name]; exists && observed != env[name] && observed != value {
				return fmt.Errorf("Host environment differs from the saved project. Review the existing files before migration.")
			}
			after.Files[name] = value
		}
	} else {
		// Choosing local files resolves conflicts in their favor, while values
		// that exist only in the DB still become directly accessible on disk.
		for name, content := range env {
			if _, exists := after.Files[name]; !exists && slices.Contains(names, name) {
				after.Files[name] = content
			}
		}
		missing := map[string]string{}
		for key, value := range variables {
			if !envFileDefines(after.Files[".env"], key) {
				missing[key] = value
			}
		}
		if len(missing) > 0 {
			after.Files[".env"] = materializedEnvironment(missing, map[string]string{".env": after.Files[".env"]})[".env"]
		}
	}
	if err := resolveFileSnapshot(ctx, &after); err != nil {
		return err
	}
	// Compare legacy interpolation with the exported .env before allowing cleanup.
	if !useCurrentFiles && project.YAML != "" {
		// Validate is authoritative for the pre-migration interpolation contract.
		old, err := Validate(ctx, project.Name, Source{YAML: project.YAML}, variables, env, directory)
		if err != nil {
			return fmt.Errorf("Saved configuration could not be validated; no database contents were removed.")
		}
		if len(composeFiles) > 1 {
			previousDefinition, err := resolvedDefinition(old, directory, nil)
			if err != nil {
				return err
			}
			var previous, current any
			_ = json.Unmarshal([]byte(previousDefinition), &previous)
			_ = json.Unmarshal(after.Resolved, &current)
			if !reflect.DeepEqual(previous, current) {
				return fmt.Errorf("Host Compose files differ from the saved configuration. Review them and choose Use current files to preserve the original files and merge order.")
			}
		}
		decoded, _ := decodedComposeModel(after.Resolved)
		var current composeModel
		_ = json.Unmarshal(decoded, &current)
		for service, want := range old.model.Services {
			got, found := current.Services[service]
			if !found || !sameEnvironment(want.Environment, got.Environment) || !sameModelPorts(want.Ports, got.Ports) {
				return fmt.Errorf("Exported environment differs from the saved configuration; no database contents were removed.")
			}
		}
	}
	if err := saveConfigurationFiles(ctx, before, after); err != nil {
		return err
	}
	verified, err := m.readFileSnapshot(ctx, project.Name)
	if err != nil {
		return err
	}
	// Exported legacy files may no longer be referenced by the chosen Compose.
	// Verify those bytes too before removing their old active DB copy.
	exportedNames := make([]string, 0, len(after.Files))
	for name := range after.Files {
		exportedNames = append(exportedNames, name)
	}
	verified.Files, err = readConfigurationFiles(ctx, verified.Directory, exportedNames)
	if err != nil {
		return err
	}
	verified.Fingerprint = fileFingerprint(verified)
	if verified.Fingerprint != after.Fingerprint {
		return fmt.Errorf("Export verification failed; no database contents were removed.")
	}
	// Keep a durable native recovery snapshot before marking the files ready.
	backup, err := store.NewJob("compose:"+project.Name, "managed", "migrate_configuration", nil)
	if err != nil {
		return err
	}
	backup.ProjectName = project.Name
	encoded, _ := json.Marshal(struct {
		Previous store.ManagedProject
		Exported FileSnapshot
	}{project, verified})
	backup.Payload = string(encoded)
	if err := m.data.CreateJob(backup); err != nil {
		return err
	}
	if err := m.data.FinishJob(backup.ID, backup.Owner, "succeeded", "verified", "", ""); err != nil {
		return err
	}
	location, _ := json.Marshal(verified.ComposeFiles)
	return m.data.CompleteFileMigration(project, store.FileConfiguration{Name: project.Name, Directory: directory, ComposeFiles: string(location), State: "ready"}, backup.ID, verified.Fingerprint)
}
func sameEnvironment(a, b map[string]*string) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func sameModelPorts(a, b []modelPort) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (m *Manager) repairDataOwnership(ctx context.Context) {
	// The daemon's bind source identifies the host owner. The container UID is
	// not used as the intended owner of the user's files.
	hostname, _ := os.Hostname()
	image, err := helperImage(ctx)
	if err != nil {
		return
	}
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{range .Mounts}}{{if eq .Destination "/data"}}{{.Source}}{{end}}{{end}}`, hostname)
	output, err := cmd.Output()
	if err != nil {
		return
	}
	source := strings.TrimSpace(string(output))
	if !path.IsAbs(source) {
		return
	}
	ownerCmd := exec.CommandContext(ctx, "docker", helperCommandArgs("--network", "none", "--mount", "type=bind,source="+path.Dir(source)+",target=/host,readonly", "--entrypoint", "stat", image, "-c", "%u:%g", "/host")...)
	owner, err := ownerCmd.Output()
	if err != nil {
		return
	}
	var uid, gid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(owner)), "%d:%d", &uid, &gid); err != nil || uid == 0 {
		return
	}
	_ = os.Chown(m.data.Directory(), uid, gid)
	entries, _ := os.ReadDir(m.data.Directory())
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if entry.Name() == "nox-yard.sqlite" || strings.HasPrefix(entry.Name(), "nox-yard.sqlite-") {
			_ = os.Chown(path.Join(m.data.Directory(), entry.Name()), uid, gid)
		}
	}
}
