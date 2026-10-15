package managed

import (
	"context"
	"encoding/json"
	"github.com/moby/moby/api/types/container"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"testing"
)

func TestConfigurationEditsTargetSourceFiles(t *testing.T) {
	for _, test := range []struct {
		name, source string
		files        map[string]string
		want         string
	}{
		{"interpolation", "services:\n  app:\n    image: alpine\n    environment: {TOKEN: '${SECRET}'}\n", map[string]string{".env": "SECRET=old\nOTHER=keep\n"}, ".env"},
		{"env file", "services:\n  app:\n    image: alpine\n    env_file: [app.env]\n", map[string]string{"app.env": "TOKEN=old\nOTHER=keep\n"}, "app.env"},
		{"raw env file", "services:\n  app:\n    image: alpine\n    env_file: [{path: app.env, format: raw}]\n", map[string]string{"app.env": "TOKEN=old\nOTHER=keep\n"}, "app.env"},
		{"inline", "services:\n  app:\n    image: alpine\n    environment: {TOKEN: old, OTHER: keep}\n", nil, "compose.yml"},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := FileSnapshot{ComposeFiles: []string{"compose.yml"}, Files: map[string]string{"compose.yml": test.source}}
			for name, value := range test.files {
				snapshot.Files[name] = value
			}
			if err := editConfigurationVariable(&snapshot, "app", "TOKEN", "new$literal"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(snapshot.Files[test.want], "new$") {
				t.Fatal("value not written to its source")
			}
			if test.want != "compose.yml" && snapshot.Files["compose.yml"] != test.source {
				t.Fatal("Compose rewritten instead of environment source")
			}
			if !strings.Contains(snapshot.Files[test.want], "keep") {
				t.Fatal("unrelated variable lost")
			}
		})
	}
}

func TestSelfFileConfigurationPreservesRecoverableData(t *testing.T) {
	service := modelService{Volumes: []modelVolume{{Type: "bind", Source: "/srv/yard/data", Target: "/data"}, {Type: "bind", Source: "/var/run/docker.sock", Target: "/var/run/docker.sock"}}, Environment: map[string]*string{}}
	preview := Preview{model: composeModel{Services: map[string]modelService{"nox-yard": service}}}
	runtime := []container.InspectResponse{{Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": "nox-yard"}}, Mounts: []container.MountPoint{{Source: "/srv/yard/data", Destination: "/data"}, {Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"}}}}
	if err := validateSelfFileConfiguration(preview, runtime, "/data"); err != nil {
		t.Fatal(err)
	}
	value := "/temporary-state"
	service.Environment["NOX_DATA_DIR"] = &value
	if err := validateSelfFileConfiguration(preview, runtime, "/data"); err == nil {
		t.Fatal("self-recreation could silently start a new empty database")
	}
	delete(service.Environment, "NOX_DATA_DIR")
	service.Volumes[0].Source = "/unrelated-state"
	if err := validateSelfFileConfiguration(preview, runtime, "/data"); err == nil {
		t.Fatal("self-recreation discarded the recovery data mount")
	}
}

func TestVariableEditPreservesInheritedAndBareListValues(t *testing.T) {
	snapshot := FileSnapshot{ComposeFiles: []string{"compose.yml"}, Files: map[string]string{"compose.yml": "x-env: &common {TOKEN: old, OTHER: keep}\nservices:\n  app:\n    image: alpine\n    environment: *common\n"}}
	if err := editConfigurationVariable(&snapshot, "app", "TOKEN", "new"); err != nil {
		t.Fatal(err)
	}
	var model struct {
		Services map[string]struct {
			Environment map[string]*string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(snapshot.Files["compose.yml"]), &model); err != nil {
		t.Fatal(err)
	}
	if *model.Services["app"].Environment["TOKEN"] != "new" || *model.Services["app"].Environment["OTHER"] != "keep" {
		t.Fatal("alias edit changed unrelated values")
	}
	snapshot.Files["compose.yml"] = "services:\n  app:\n    image: alpine\n    environment: [TOKEN=old, OTHER]\n"
	if err := editConfigurationVariable(&snapshot, "app", "TOKEN", "new"); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte(snapshot.Files["compose.yml"]), &model); err != nil {
		t.Fatal(err)
	}
	if model.Services["app"].Environment["OTHER"] != nil {
		t.Fatal("bare inherited value became empty string")
	}
	snapshot.Files["compose.yml"] = ""
	if err := editConfigurationVariable(&snapshot, "app", "TOKEN", "new"); err == nil {
		t.Fatal("empty invalid YAML accepted")
	}
}

func TestConfigurationPathsCannotEscapeProject(t *testing.T) {
	for _, name := range []string{"../env", "/env", "a/../../env", "a\\env", ".", "a/../env", "x\x00y"} {
		if safeProjectFile(name) {
			t.Errorf("accepted unsafe path %q", name)
		}
	}
}

func TestDotenvEditReplacesEffectiveAssignment(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{"TOKEN=first\nOTHER=keep\nTOKEN=effective\n", "TOKEN=first\nOTHER=keep\nTOKEN=\"new\"\n"},
		{"export TOKEN: 'line1\nline2' # note\nOTHER=keep\n", "TOKEN=\"new\"\nOTHER=keep\n"},
		{"OTHER='line1\nTOKEN=embedded\nline3'\nTOKEN=old\n", "OTHER='line1\nTOKEN=embedded\nline3'\nTOKEN=\"new\"\n"},
	} {
		if got := setDotenvValue(test.source, "TOKEN", "new"); got != test.want {
			t.Fatalf("wrong definition edited: %q", got)
		}
	}
}

func TestMaterializedEnvironmentDocker(t *testing.T) {
	if os.Getenv("NOX_MANAGED_ACCEPTANCE_BASE") == "" {
		t.Skip("run scripts/smoke-managed.py for real Compose integration")
	}
	values := map[string]string{"DOLLAR": "private$literal${IGNORED}", "QUOTE": "it's private", "MULTILINE": "line1\nline2", "BACKSLASH": `C:\private\`, "EMPTY": ""}
	files := materializedEnvironment(values, nil)
	source := FileSnapshot{Name: "configuration-env-test", Directory: "/fixture", ComposeFiles: []string{"compose.yml"}, Files: files}
	source.Files["compose.yml"] = "services:\n  app:\n    image: alpine:3.23\n    environment:\n      DOLLAR: '${DOLLAR}'\n      QUOTE: '${QUOTE}'\n      MULTILINE: '${MULTILINE}'\n      BACKSLASH: '${BACKSLASH}'\n      EMPTY: '${EMPTY}'\n"
	if err := resolveFileSnapshot(context.Background(), &source); err != nil {
		t.Fatal(err)
	}
	decoded, _ := decodedComposeModel(source.Resolved)
	var model composeModel
	if err := json.Unmarshal(decoded, &model); err != nil {
		t.Fatal(err)
	}
	for name, want := range values {
		got := model.Services["app"].Environment[name]
		if got == nil || *got != want {
			t.Errorf("%s did not survive file materialization", name)
		}
	}
}
