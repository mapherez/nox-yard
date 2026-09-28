package managed

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectSourceSuggestsContainerNameAndEnvFiles(t *testing.T) {
	content := "services:\n  bot:\n    image: alpine\n    container_name: nox-discord-bot\n    env_file:\n      - .env\n      - path: optional.env\n        required: false\n"
	info, err := InspectSource(content)
	if err != nil {
		t.Fatal(err)
	}
	if info.SuggestedName != "nox-discord-bot" {
		t.Fatalf("suggested name = %q", info.SuggestedName)
	}
	if len(info.EnvFiles) != 2 || info.EnvFiles[0].Path != ".env" || !info.EnvFiles[0].Required || info.EnvFiles[1].Required {
		t.Fatalf("environment files = %+v", info.EnvFiles)
	}
}

func TestPrepareComposeRequiresReferencedEnvFile(t *testing.T) {
	content := "services:\n  bot:\n    image: alpine\n    env_file: .env\n"
	if _, _, err := prepareCompose(content, nil); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("missing env_file error = %v", err)
	}
	path, cleanup, err := prepareCompose(content, map[string]string{".env": "TOKEN='hidden'\n"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), ".env"))
	if err != nil || string(data) != "TOKEN='hidden'\n" {
		t.Fatalf("materialized env_file = %q, %v", data, err)
	}
	if _, err := InspectSource(strings.Replace(content, ".env", "../escape.env", 1)); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("unsafe env_file error = %v", err)
	}
}
