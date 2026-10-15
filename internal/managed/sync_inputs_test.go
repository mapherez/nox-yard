package managed

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
)

func TestSyncPreservesSavedInputsAndAcceptsExplicitReplacements(t *testing.T) {
	source := `services:
  app:
    image: 'example:${TAG:-latest}'
    ports: ['${APP_PORT:-8080}:8080']
    environment: {TOKEN: '${TOKEN}'}
    env_file: [config/app.env]
`
	previous := store.ManagedProject{
		VariablesJSON: `{"APP_PORT":"2303","TAG":"old","TOKEN":"private-saved","REMOVED":"obsolete"}`,
		EnvFilesJSON:  `{".env":"EXTRA=saved\n","config/app.env":"PASSWORD=private-saved\n","removed.env":"OLD=value\n"}`,
	}
	for _, test := range []struct {
		name                                       string
		variables, files, wantVariables, wantFiles map[string]string
	}{
		{"omitted", nil, nil,
			map[string]string{"APP_PORT": "2303", "TAG": "old", "TOKEN": "private-saved"},
			map[string]string{".env": "EXTRA=saved\n", "config/app.env": "PASSWORD=private-saved\n"}},
		{"explicit empty and new value", map[string]string{"APP_PORT": "", "TAG": "new"}, map[string]string{"config/app.env": ""},
			map[string]string{"APP_PORT": "", "TAG": "new", "TOKEN": "private-saved"},
			map[string]string{".env": "EXTRA=saved\n", "config/app.env": ""}},
		{"new root env overrides inherited variable", nil, map[string]string{".env": "APP_PORT=2400\n"},
			map[string]string{"TAG": "old", "TOKEN": "private-saved"},
			map[string]string{".env": "APP_PORT=2400\n", "config/app.env": "PASSWORD=private-saved\n"}},
		{"explicit variable wins over root env", map[string]string{"APP_PORT": "2500"}, map[string]string{".env": "APP_PORT=2400\n"},
			map[string]string{"APP_PORT": "2500", "TAG": "old", "TOKEN": "private-saved"},
			map[string]string{".env": "APP_PORT=2400\n", "config/app.env": "PASSWORD=private-saved\n"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			variables, files, err := syncInputs(previous, source, test.variables, test.files)
			if err != nil || !reflect.DeepEqual(variables, test.wantVariables) || !reflect.DeepEqual(files, test.wantFiles) {
				t.Fatalf("effective inputs differ from expected preservation/override semantics: %v", err)
			}
			encoded, err := json.Marshal(ProjectPreview{variables: variables, envFiles: files})
			if err != nil || strings.Contains(string(encoded), "private-saved") {
				t.Fatal("saved secrets leaked through public preview")
			}
		})
	}
}

func TestSyncRejectsUnreadableSavedEnvironment(t *testing.T) {
	_, _, err := syncInputs(store.ManagedProject{VariablesJSON: "broken private-value", EnvFilesJSON: "{}"}, "services: {}", nil, nil)
	if err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("invalid private environment was not safely rejected")
	}
}
