package mcpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/machineauth"
	"github.com/mapherez/nox-yard/internal/mcpapi"
)

func TestInspectionCannotExecuteWritesOrRevealSecrets(t *testing.T) {
	app, _, runtime := fixture(t)
	c := &controller{}
	app.Lifecycle = c
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"yard_container_action", map[string]any{"id": id, "action": "restart"}},
		{"yard_project_pull", map[string]any{"id": "compose:fixture"}},
		{"yard_projects_settings_set", map[string]any{"projectsBase": "/tmp"}},
		{"yard_job_recovery_acknowledge", map[string]any{"id": "a", "updatedAt": 1, "confirm": true}},
		{"yard_container_environment", map[string]any{"id": id}},
		{"yard_container_logs", map[string]any{"id": id}},
		{"yard_project_configuration_read", map[string]any{"id": "compose:fixture", "reveal": true}},
	} {
		_, err := runtime.Execute(machineauth.WithPolicy(context.Background(), machineauth.Policy{}), test.name, test.args, nil)
		var public *noxmcp.Error
		if !errors.As(err, &public) || public.Code != "FORBIDDEN" {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
	if c.calls != 0 {
		t.Fatal("denied operation reached Docker")
	}
	_, err := runtime.Execute(context.Background(), "yard_projects", map[string]any{}, nil)
	var public *noxmcp.Error
	if !errors.As(err, &public) || public.Code != "AUTH_REQUIRED" {
		t.Fatalf("missing identity: %v", err)
	}
}
func TestInspectionCatalogOmitsWritesAndSensitiveTools(t *testing.T) {
	app, _, _ := fixture(t)
	handler, err := mcpapi.NewHandler(app, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(machineauth.WithPolicy(r.Context(), machineauth.Policy{})))
	}))
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("empty inspection catalog")
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.Name == "yard_container_environment" || tool.Name == "yard_container_logs" {
			t.Fatalf("unexpected tool: %s", tool.Name)
		}
	}
}
