package managed

import (
	"context"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
)

// Run directly in the disposable Linux runner to verify migration without
// rebuilding registry fixtures or replacing application containers.
func TestConfigurationMigrationDockerAcceptance(t *testing.T) {
	if os.Getenv("NOX_MANAGED_ACCEPTANCE_BASE") == "" {
		t.Skip("requires isolated Linux Docker acceptance")
	}
	prefix := os.Getenv("NOX_MANAGED_ACCEPTANCE_NAME")
	if !strings.HasPrefix(prefix, "configuration-migration-") {
		t.Skip("requires a generated configuration-migration fixture identity")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	manager, err := NewManager(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	docker := func(parts ...string) string {
		t.Helper()
		output, err := exec.CommandContext(ctx, "docker", parts...).CombinedOutput()
		if err != nil {
			t.Fatalf("migration fixture command: %s: %v", output, err)
		}
		return strings.TrimSpace(string(output))
	}
	testConfigurationMigrationDocker(t, ctx, manager, data, prefix, docker)
}

func testConfigurationMigrationDocker(t *testing.T, ctx context.Context, m *Manager, data *store.Store, prefix string, docker func(...string) string) {
	t.Helper()
	// Permission checks use a Linux volume so UID ownership is meaningful even
	// when the acceptance harness runs on Docker Desktop/Windows.
	volume := prefix + "-permissions"
	docker("volume", "create", "--label", "nox-yard.acceptance="+prefix, volume)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(cleanup, "docker", "volume", "rm", volume).CombinedOutput(); err != nil {
			t.Errorf("permission fixture cleanup: %s: %v", output, err)
		}
	})
	docker("run", "--rm", "--mount", "type=volume,source="+volume+",target=/fixture", "--entrypoint", "sh", os.Getenv("NOX_HELPER_IMAGE"), "-c", "chown 1001:1001 /fixture; mkdir /fixture/project")
	volumePath := docker("volume", "inspect", "--format", "{{.Mountpoint}}", volume)
	permissionProject := store.ManagedProject{Name: prefix + "-permissions", ProjectDir: path.Join(volumePath, "project"), YAML: "services:\n  app:\n    image: alpine:3.23\n    environment: {TOKEN: '${TOKEN}'}\n", VariablesJSON: `{"TOKEN":"fixture-value"}`, EnvFilesJSON: "{}"}
	if err := data.SaveManagedProject(permissionProject); err != nil {
		t.Fatal(err)
	}
	if err := m.migrateConfiguration(ctx, permissionProject, false); err != nil {
		t.Fatal(err)
	}
	if value := docker("run", "--rm", "--user", "1001:1001", "--mount", "type=volume,source="+volume+",target=/fixture", "--entrypoint", "sh", os.Getenv("NOX_HELPER_IMAGE"), "-c", "cd /fixture/project; test -r compose.yml; test -r .env; test -w compose.yml; test -w .env; test -w .; echo accessible"); value != "accessible" {
		t.Fatal("non-root owner cannot access exported files")
	}
	t.Log("PASS: migrated project directory, Compose and .env are readable/writable by the host owner without root")
	// Resume a partial export, then preserve a conflicting manual Compose.
	for _, mode := range []string{"resume", "conflict"} {
		candidate := permissionProject
		candidate.Name = prefix + "-" + mode
		candidate.ProjectDir = path.Join(volumePath, mode)
		docker("run", "--rm", "--mount", "type=volume,source="+volume+",target=/fixture", "--entrypoint", "mkdir", os.Getenv("NOX_HELPER_IMAGE"), "/fixture/"+mode)
		if err := data.SaveManagedProject(candidate); err != nil {
			t.Fatal(err)
		}
		partial := FileSnapshot{Name: candidate.Name, Directory: candidate.ProjectDir, ComposeFiles: []string{"compose.yml"}, Files: map[string]string{}}
		written := partial
		written.Files = map[string]string{"compose.yml": candidate.YAML}
		if mode == "conflict" {
			written.Files["compose.yml"] = "services:\n  app:\n    image: alpine:3.23\n    environment: {TOKEN: manual-value}\n"
		}
		if err := saveConfigurationFiles(ctx, partial, written); err != nil {
			t.Fatal(err)
		}
		err := m.migrateConfiguration(ctx, candidate, false)
		if mode == "conflict" {
			if err == nil {
				t.Fatal("migration overwrote manual conflict")
			}
			retained, _, _ := data.ManagedProject(candidate.Name)
			if retained.YAML != candidate.YAML {
				t.Fatal("conflict deleted active DB source")
			}
			if err := m.migrateConfiguration(ctx, candidate, true); err != nil {
				t.Fatal("explicit current-file resolution", err)
			}
			current, err := m.readFileSnapshot(ctx, candidate.Name)
			if err != nil || current.Files["compose.yml"] != written.Files["compose.yml"] {
				t.Fatal("manual conflict was not preserved")
			}
		} else if err != nil {
			t.Fatal("partial export could not resume", err)
		}
		state, found, _ := data.FileConfiguration(candidate.Name)
		if !found || state.State != "ready" {
			t.Fatal("verified migration did not become ready")
		}
	}
	t.Log("PASS: interrupted export resumes; conflicting manual files and DB source survive until explicit current-file resolution")
	// Invalid manual YAML remains readable and repairable in the file editor.
	snapshot, err := m.readFileSnapshot(ctx, permissionProject.Name)
	if err != nil {
		t.Fatal(err)
	}
	invalid := snapshot
	invalid.Files = map[string]string{}
	for file, content := range snapshot.Files {
		invalid.Files[file] = content
	}
	invalid.Files["compose.yml"] = "services: [invalid"
	if err := saveConfigurationFiles(ctx, snapshot, invalid); err != nil {
		t.Fatal(err)
	}
	view, err := m.Configuration(ctx, permissionProject.Name)
	if err != nil || !view.Available || view.Error == "" {
		t.Fatal("invalid manual YAML cannot be read for repair", err)
	}
	if _, err := m.FilePreview(ctx, permissionProject.Name, "recreate"); err == nil {
		t.Fatal("invalid host configuration fell back silently")
	}
	if _, err := m.SaveConfiguration(ctx, permissionProject.Name, ConfigurationEdit{Fingerprint: view.Fingerprint, Path: "compose.yml", Content: permissionProject.YAML}); err != nil {
		t.Fatal("manual invalid YAML could not be repaired", err)
	}
	t.Log("PASS: invalid manual Compose is editable, blocks Recreate and can be repaired without restarting containers")
	legacy := permissionProject
	legacy.Name = prefix + "-legacy-path"
	legacy.ProjectDir = ""
	directory := path.Join(volumePath, "legacy-path")
	docker("run", "--rm", "--mount", "type=volume,source="+volume+",target=/fixture", "--entrypoint", "mkdir", os.Getenv("NOX_HELPER_IMAGE"), "/fixture/legacy-path")
	missing := FileSnapshot{Name: legacy.Name, Directory: directory, ComposeFiles: []string{"compose.yaml"}, Files: map[string]string{}}
	existing := missing
	existing.Files = map[string]string{"compose.yaml": legacy.YAML}
	if err := saveConfigurationFiles(ctx, missing, existing); err != nil {
		t.Fatal(err)
	}
	if err := data.SaveManagedProject(legacy); err != nil {
		t.Fatal(err)
	}
	originalRuntime := m.runtime
	m.runtime = func(ctx context.Context, name string) ([]container.InspectResponse, error) {
		if name == legacy.Name {
			return []container.InspectResponse{{Config: &container.Config{Labels: map[string]string{"com.docker.compose.project.working_dir": directory, "com.docker.compose.project.config_files": path.Join(directory, "compose.yaml")}}}}, nil
		}
		return originalRuntime(ctx, name)
	}
	defer func() { m.runtime = originalRuntime }()
	if err := m.migrateConfiguration(ctx, legacy, false); err != nil {
		t.Fatal("legacy label migration", err)
	}
	materialized, found, err := data.ManagedProject(legacy.Name)
	if err != nil || !found || materialized.ProjectDir != directory || materialized.YAML != "" {
		t.Fatal("legacy host directory was not recovered", err)
	}
	actualFiles, err := readConfigurationFiles(ctx, directory, []string{"compose.yml", "compose.yaml", ".env"})
	if err != nil {
		t.Fatal(err)
	}
	if _, duplicate := actualFiles["compose.yml"]; duplicate || actualFiles["compose.yaml"] != legacy.YAML || actualFiles[".env"] == "" {
		t.Fatal("legacy migration changed original names or lost DB-only env")
	}
	t.Log("PASS: legacy missing directory is recovered from Docker labels; compose.yaml remains the original source without a duplicate compose.yml")
	// The same source can be split across original Compose files without
	// forcing an unnecessary conflict or rewriting them into one file.
	multi := legacy
	multi.Name = prefix + "-multi-migration"
	multi.ProjectDir = path.Join(volumePath, "multi")
	docker("run", "--rm", "--mount", "type=volume,source="+volume+",target=/fixture", "--entrypoint", "mkdir", os.Getenv("NOX_HELPER_IMAGE"), "/fixture/multi")
	base := FileSnapshot{Name: multi.Name, Directory: multi.ProjectDir, ComposeFiles: []string{"compose.yaml", "override.yaml"}, Files: map[string]string{}}
	merged := base
	merged.Files = map[string]string{"compose.yaml": multi.YAML, "override.yaml": "services: {}\n"}
	if err := saveConfigurationFiles(ctx, base, merged); err != nil {
		t.Fatal(err)
	}
	if err := data.SaveManagedProject(multi); err != nil {
		t.Fatal(err)
	}
	withLegacy := m.runtime
	m.runtime = func(ctx context.Context, name string) ([]container.InspectResponse, error) {
		if name == multi.Name {
			return []container.InspectResponse{{Config: &container.Config{Labels: map[string]string{"com.docker.compose.project.working_dir": multi.ProjectDir, "com.docker.compose.project.config_files": path.Join(multi.ProjectDir, "compose.yaml") + "," + path.Join(multi.ProjectDir, "override.yaml")}}}}, nil
		}
		return withLegacy(ctx, name)
	}
	if err := m.migrateConfiguration(ctx, multi, false); err != nil {
		t.Fatal("equivalent multi-file migration", err)
	}
	current, err := m.readFileSnapshot(ctx, multi.Name)
	if err != nil || len(current.ComposeFiles) != 2 || current.ComposeFiles[0] != "compose.yaml" || current.Files["override.yaml"] != "services: {}\n" {
		t.Fatal("multi-file migration changed paths or merge order", err)
	}
	t.Log("PASS: equivalent multi-file Compose migrates automatically while preserving original files and merge order")
}
