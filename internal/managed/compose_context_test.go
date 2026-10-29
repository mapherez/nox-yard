package managed

import (
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func requireCompose(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("native Compose CLI required")
	}
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("native Compose CLI required")
	}
}

func TestNativeComposeInterpolationAndBuildMetadata(t *testing.T) {
	requireCompose(t)
	source := Source{YAML: `services:
  app:
    image: alpine
    build: ./unavailable-development-context
    environment:
      OPTIONAL: ${OPTIONAL}
      SHORT: $SHORT
      DEFAULT: ${UNSET:-fallback}
      EMPTY: ${EMPTY-default}
      NESTED: ${OUTER:-${INNER:-nested}}
      LITERAL: $$HOME
# ${COMMENT_ONLY:?not-an-input}
`}
	preview, err := Validate(t.Context(), "parity", source, nil, map[string]string{".env": "EMPTY=\n"}, "/srv/parity")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"OPTIONAL": "", "SHORT": "", "DEFAULT": "fallback", "EMPTY": "", "NESTED": "nested", "LITERAL": "$HOME"} {
		v := preview.model.Services["app"].Environment[key]
		if v == nil || *v != want {
			t.Fatalf("%s: got %v want %q", key, v, want)
		}
	}
	definition, err := resolvedDefinition(preview, "/srv/parity", nil)
	if err != nil || strings.Contains(definition, `"build"`) {
		t.Fatal("private execution must use prebuilt image", definition, err)
	}
	variables, err := nativeVariables(t.Context(), source.YAML)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range variables {
		if v.Name == "COMMENT_ONLY" || v.Name == "HOME" || v.Required {
			t.Fatal("native optional/escaped/comment discovery changed", variables)
		}
	}
	if _, err := Validate(t.Context(), "parity", Source{YAML: "services:\n  app:\n    image: alpine\n    environment: {VALUE: '${REQUIRED:?provide-it}'}\n"}, nil, nil, "/srv/parity"); err == nil {
		t.Fatal("required variable accepted")
	}
}

func TestNativeComposeProfilesSelectionAndVerification(t *testing.T) {
	requireCompose(t)
	s := FileSnapshot{Name: "parity", Directory: "/srv/parity", ComposeFiles: []string{"compose.yaml"}, Files: map[string]string{"compose.yaml": `services:
  app: {image: alpine}
  debug: {image: alpine, profiles: [debug]}
  companion: {image: alpine, profiles: [debug]}
`}}
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.ActiveServices, []string{"app"}) {
		t.Fatal("inactive profiles activated", s.ActiveServices)
	}
	s.Context.Services = []string{"debug"}
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.ActiveServices, []string{"app", "debug"}) {
		t.Fatal("runtime selection activated profile companion", s.ActiveServices)
	}
	preview := filePreview(s)
	if len(expectedServices(preview.model)) != 2 {
		t.Fatal("active profiled service omitted from verification")
	}
	definition, err := resolvedDefinition(preview, s.Directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc composeModel
	_ = json.Unmarshal([]byte(definition), &doc)
	if len(doc.Services) != 3 || len(doc.Services["debug"].Profiles) != 0 || len(doc.Services["companion"].Profiles) == 0 {
		t.Fatal("private execution selection changed", definition)
	}
	profiles := []string{"debug"}
	s.Context.Profiles = &profiles
	s.Context.Services = nil
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.ActiveServices) != 3 {
		t.Fatal("explicit profile did not activate all its services", s.ActiveServices)
	}
}

func TestNativeComposeLocalDependenciesAndOriginalPaths(t *testing.T) {
	requireCompose(t)
	s := FileSnapshot{Name: "parity", Directory: "/srv/parity", ComposeFiles: []string{"compose.yaml", "extra.yaml"}, Files: map[string]string{
		"compose.yaml": `include: [parts/compose.yaml]
services:
  app:
    image: alpine
    env_file: [{path: missing.env, required: false}, {path: app.env, format: raw}]
    label_file: labels.txt
    configs: [settings]
    secrets: [token]
    volumes: ['./data:/data']
configs: {settings: {file: settings.txt}}
secrets: {token: {file: token.txt}}
`,
		"extra.yaml":         "services:\n  app:\n    environment: {OVERRIDE: selected}\n",
		"parts/compose.yaml": "services:\n  sidecar:\n    image: alpine\n    volumes: ['./side-data:/data']\n",
		"app.env":            "RAW=$KEEP\n", "labels.txt": "example.label=kept\n", "settings.txt": "configuration\n", "token.txt": "private-fixture-value\n",
	}}
	if err := snapshotDependencies(t.Context(), &s, false); err != nil {
		t.Fatal(err)
	}
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	preview := filePreview(s)
	if *preview.model.Services["app"].Environment["RAW"] != "$KEEP" {
		t.Fatal("raw env_file interpolated")
	}
	for _, want := range []string{"/srv/parity/data", "/srv/parity/parts/side-data", "/srv/parity/settings.txt", "/srv/parity/token.txt", `"OVERRIDE":"selected"`} {
		if !strings.Contains(string(s.Resolved), want) {
			t.Fatalf("resolved context lost %s: %s", want, s.Resolved)
		}
	}
}

func TestComposeDependenciesCannotEscapeSnapshot(t *testing.T) {
	for _, source := range []string{"include: [https://example.com/compose.yaml]", "include: [../outside.yaml]", "services: {app: {image: alpine, env_file: /etc/passwd}}"} {
		s := FileSnapshot{Directory: "/srv/project", ComposeFiles: []string{"compose.yaml"}, Files: map[string]string{"compose.yaml": source}}
		if err := snapshotDependencies(t.Context(), &s, false); err == nil {
			t.Fatal("external dependency accepted", source)
		}
	}
}

func TestNativeIncludeProjectDirectoryAndEnvironment(t *testing.T) {
	requireCompose(t)
	s := FileSnapshot{Name: "parity", Directory: "/srv/parity", ComposeFiles: []string{"compose.yaml"}, Files: map[string]string{
		"compose.yaml":         "include:\n  - path: parts/compose.yaml\n    project_directory: settings\n    env_file: parts/defaults.env\nservices: {main: {image: alpine}}\n",
		"parts/compose.yaml":   "services:\n  included:\n    image: alpine\n    env_file: '${ENV_PATH}'\n    volumes: ['./data:/data']\n",
		"parts/defaults.env":   "ENV_PATH=runtime.env\n",
		"settings/runtime.env": "VALUE=included-file\n",
	}}
	if err := snapshotDependencies(t.Context(), &s, false); err != nil {
		t.Fatal(err)
	}
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	if value := filePreview(s).model.Services["included"].Environment["VALUE"]; value == nil || *value != "included-file" {
		t.Fatal("included env_file base changed")
	}
	if !strings.Contains(string(s.Resolved), "/srv/parity/settings/data") {
		t.Fatal("included bind base changed", string(s.Resolved))
	}
}

func TestExplicitInterpolationFilePrecedenceAndFingerprint(t *testing.T) {
	requireCompose(t)
	files := []string{"one.env", "two.env"}
	s := FileSnapshot{Name: "parity", Directory: "/srv/parity", ComposeFiles: []string{"compose.yaml"}, Context: ComposeContext{EnvFiles: &files}, Files: map[string]string{
		"compose.yaml": "services: {app: {image: alpine, environment: {VALUE: '${VALUE-default}'}}}\n",
		".env":         "VALUE=implicit\n", "one.env": "VALUE=one\n", "two.env": "VALUE=two\n",
	}}
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	if *filePreview(s).model.Services["app"].Environment["VALUE"] != "two" {
		t.Fatal("explicit environment precedence changed")
	}
	previous := s.Fingerprint
	empty := []string{}
	s.Context.EnvFiles = &empty
	if err := resolveSnapshot(t.Context(), &s); err != nil {
		t.Fatal(err)
	}
	if *filePreview(s).model.Services["app"].Environment["VALUE"] != "default" || s.Fingerprint == previous {
		t.Fatal("disabled .env or context fingerprint changed")
	}
}

func TestInterpolatedDependencyDiagnosticDoesNotRevealValues(t *testing.T) {
	requireCompose(t)
	s := FileSnapshot{Name: "parity", Directory: "/srv/parity", ComposeFiles: []string{"compose.yaml"}, Files: map[string]string{"compose.yaml": "include: ['${PRIVATE_PATH}.yaml']\nservices: {app: {image: alpine}}\n", ".env": "PRIVATE_PATH=private-value-must-not-leak\n"}}
	err := snapshotDependencies(t.Context(), &s, false)
	if err == nil {
		t.Fatal("missing interpolated input accepted")
	}
	encoded, _ := json.Marshal(err)
	if strings.Contains(string(encoded), "private-value-must-not-leak") || strings.Contains(err.Error(), "private-value-must-not-leak") {
		t.Fatal("interpolated value leaked into diagnostic")
	}
}
