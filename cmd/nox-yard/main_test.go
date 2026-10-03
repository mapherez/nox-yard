package main

import (
	"os"
	"strings"
	"testing"
)

func TestResolvedVersion(t *testing.T) {
	for _, test := range []struct{ runtime, built, want string }{{" v2.0.0 ", "git-abc", "v2.0.0"}, {"", "v1.0.0", "v1.0.0"}, {" \t", " git-abc ", "git-abc"}, {"", "", "dev"}} {
		if got := resolvedVersion(test.runtime, test.built); got != test.want {
			t.Fatalf("resolved %q, want %q", got, test.want)
		}
	}
}

func TestControlEnvironmentConfiguration(t *testing.T) {
	for _, value := range []string{"", "false", "true", "0", "1", "invalid"} {
		env := map[string]string{"NOX_YARD_API_ENABLED": value, "NOX_YARD_API_KEY": "independent-key", "NOX_YARD_VERSION": " runtime-version "}
		config, err := controlConfiguration(func(key string) string { return env[key] })
		if value == "invalid" {
			if err == nil || strings.Contains(err.Error(), "independent-key") {
				t.Fatal("invalid boolean accepted or key leaked")
			}
			continue
		}
		if err != nil || config.Enabled != (value == "true" || value == "1") || config.Version != "runtime-version" {
			t.Fatalf("bad configuration: %+v %v", config, err)
		}
	}
}

func TestAuxiliaryCommandDoesNotValidateMachineConfiguration(t *testing.T) {
	t.Setenv("NOX_YARD_API_ENABLED", "invalid")
	t.Setenv("NOX_DATA_DIR", t.TempDir())
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	os.Args = []string{"nox-yard", "unknown-command"}
	err := run()
	if err == nil || err.Error() != "unknown command: unknown-command" {
		t.Fatalf("machine config changed auxiliary dispatch: %v", err)
	}
}
