package mcpapi_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/store"
)

type configurationController struct {
	*managed.Manager
	view  managed.FileConfigurationView
	name  string
	edit  managed.ConfigurationEdit
	saves int
	jobID string
	err   error
}

func (c *configurationController) Configuration(ctx context.Context, name string) (managed.FileConfigurationView, error) {
	c.name = name
	return c.view, ctx.Err()
}
func (c *configurationController) SaveConfiguration(ctx context.Context, name string, edit managed.ConfigurationEdit) (managed.FileConfigurationView, error) {
	c.saves++
	c.name, c.edit = name, edit
	if job, ok := jobs.FromContext(ctx); ok {
		c.jobID = job.ID
	}
	if c.err != nil {
		return managed.FileConfigurationView{}, c.err
	}
	if edit.Fingerprint != c.view.Fingerprint {
		return managed.FileConfigurationView{}, managed.ErrConflict
	}
	c.view.Fingerprint += "-next"
	c.view.Variables[0].Pending = true
	return c.view, nil
}

func TestConfigurationToolsUseSharedFilesAndProtectValues(t *testing.T) {
	app, api, _ := fixture(t)
	c := &configurationController{view: managed.FileConfigurationView{
		Available: true, State: "ready", Directory: "/srv/demo", Fingerprint: "current",
		Files:     []managed.ConfigurationFile{{Path: "compose.yml", Content: "private-compose-value", Compose: true}, {Path: "nested/app.env", Content: "TOKEN=private-env-value"}},
		Variables: []managed.ConfigurationVariable{{Service: "web", Name: "TOKEN", Value: "private-env-value"}},
	}}
	app.Managed = c
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	read := func(reveal bool) string {
		t.Helper()
		result := call(t, session, "yard_project_configuration_read", map[string]any{"id": "compose:demo", "reveal": reveal})
		if result.IsError {
			t.Fatal(text(result))
		}
		return text(result)
	}
	if result := read(false); strings.Contains(result, "private-") || !strings.Contains(result, "nested/app.env") || !strings.Contains(result, "current") {
		t.Fatal("default read must return metadata only", result)
	}
	if result := read(true); !strings.Contains(result, "private-compose-value") || !strings.Contains(result, "private-env-value") {
		t.Fatal("redacting earlier read modified the shared configuration", result)
	}
	if c.name != "demo" {
		t.Fatal("wrong project", c.name)
	}
	result := call(t, session, "yard_project_configuration_save", map[string]any{"id": "compose:demo", "fingerprint": "current", "path": "nested/app.env", "content": "TOKEN=new-private-value\n"})
	if result.IsError || strings.Contains(text(result), "private-") || !strings.Contains(text(result), `"pending":true`) {
		t.Fatal("save response must hide sensitive contents and show pending state", text(result))
	}
	if c.edit.Path != "nested/app.env" || c.edit.Content != "TOKEN=new-private-value\n" || c.jobID == "" {
		t.Fatalf("save bypassed the shared operation: %+v, job=%s", c.edit, c.jobID)
	}
	job, found, err := app.Store.Job(c.jobID)
	if err != nil || !found || job.Status != "succeeded" || job.Operation != "edit_configuration" {
		t.Fatalf("configuration operation not recorded: %+v %v", job, err)
	}
	result = call(t, session, "yard_project_variable_set", map[string]any{"id": "compose:demo", "fingerprint": c.view.Fingerprint, "service": "web", "name": "TOKEN", "value": ""})
	if result.IsError || c.edit.Variable != "TOKEN" || c.edit.Service != "web" || c.edit.Content != "" || c.edit.Path != "" {
		t.Fatal("empty variable value or shared variable edit lost", text(result), c.edit)
	}
	result = call(t, session, "yard_project_variable_set", map[string]any{"id": "compose:demo", "fingerprint": "current", "service": "web", "name": "TOKEN", "value": "stale-private-value"})
	if !result.IsError || !strings.Contains(text(result), "OPERATION_CONFLICT") || strings.Contains(text(result), "private-") {
		t.Fatal("stale configuration accepted or leaked", text(result))
	}
	history, err := app.Store.Jobs(context.Background(), "compose:demo", false)
	if err != nil || len(history) != 3 {
		t.Fatal("missing shared history", history, err)
	}
	encoded, _ := json.Marshal(history)
	if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "TOKEN=") {
		t.Fatal("sensitive values leaked to history", string(encoded))
	}
	// The same shared reservation used by browser/Docker operations blocks edits.
	busy, _ := store.NewJob("compose:demo", "engine", "recreate", []string{"compose:demo"})
	if err := app.Store.CreateJob(busy); err != nil {
		t.Fatal(err)
	}
	before := c.saves
	result = call(t, session, "yard_project_variable_set", map[string]any{"id": "compose:demo", "fingerprint": c.view.Fingerprint, "service": "web", "name": "TOKEN", "value": "new"})
	if !result.IsError || c.saves != before {
		t.Fatal("edit bypassed active shared reservation", text(result))
	}
}

func TestConfigurationToolInputAndManagerErrors(t *testing.T) {
	app, api, _ := fixture(t)
	c := &configurationController{view: managed.FileConfigurationView{State: "ready"}, err: managed.ErrConflict}
	app.Managed = c
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"yard_project_configuration_read", map[string]any{"id": "container:" + id}},
		{"yard_project_configuration_read", map[string]any{"id": "compose:../bad"}},
		{"yard_project_configuration_save", map[string]any{"id": "compose:demo", "fingerprint": "f", "content": "x"}},
		{"yard_project_configuration_save", map[string]any{"id": "compose:demo", "fingerprint": "", "path": "compose.yml", "content": "x"}},
		{"yard_project_variable_set", map[string]any{"id": "compose:demo", "fingerprint": "f", "service": "web", "name": "TOKEN"}},
		{"yard_project_variable_set", map[string]any{"id": "compose:demo", "fingerprint": "f", "service": "", "name": "TOKEN", "value": ""}},
	} {
		if result := call(t, session, test.name, test.args); !result.IsError {
			t.Fatal("invalid input accepted", test.name, text(result))
		}
	}
	if c.saves != 0 {
		t.Fatal("invalid input reached manager")
	}
	result := call(t, session, "yard_project_configuration_save", map[string]any{"id": "compose:nox-yard", "fingerprint": "f", "path": "compose.yml", "content": "private-source"})
	if !result.IsError || !strings.Contains(text(result), "OPERATION_CONFLICT") || c.name != "nox-yard" || c.saves != 1 {
		t.Fatal("Yard target or manager protection lost", text(result))
	}
}
