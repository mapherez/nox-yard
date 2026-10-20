package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/machineauth"
	"github.com/mapherez/nox-yard/internal/store"
)

func TestMachineHTTPBoundariesScopesAndRotation(t *testing.T) {
	data, api, _, controller := controlFixture(t, true)
	network := store.MachineNetwork{Yard: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8095}, MCP: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8096}}
	if err := data.InitializeMachine(network, false, ""); err != nil {
		t.Fatal(err)
	}
	api.ConfigureMachineAccess(nil, nil)
	api.SetMCPListening(true)
	calls := 0
	api.SetMCPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		p, ok := machineauth.FromContext(r.Context())
		if !ok {
			t.Error("principal missing")
		}
		writeJSON(w, 200, p)
	}))
	mcp := api.MCPHandler()
	main := api.Handler()
	if response := controlRequest(mcp, "POST", "/mcp", "{}", ""); response.Code != 503 || calls != 0 {
		t.Fatal("MCP not opt-in")
	}
	if response := controlRequest(main, "POST", "/mcp", "{}", ""); response.Code != 404 {
		t.Fatal("MCP exposed on UI port")
	}
	if response := controlRequest(mcp, "GET", "/api/bootstrap", "", ""); response.Code != 404 {
		t.Fatal("UI on MCP port")
	}
	token, err := data.RotateMachineToken("owner", false)
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := data.MachineSettings()
	settings.MCPEnabled, settings.APIEnabled = true, true
	if _, err := data.SaveMachineSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, auth := range []string{"", "Bearer invalid", "Basic " + token, "Bearer  " + token} {
		if response := controlRequest(mcp, "POST", "/mcp", "{}", auth); response.Code != 401 {
			t.Fatalf("credential accepted: %q %d", auth, response.Code)
		}
	}
	if response := controlRequest(mcp, "POST", "/mcp?api_key="+token, "{}", ""); response.Code != 401 {
		t.Fatal("query authenticated")
	}
	if response := controlRequest(mcp, "POST", "/mcp", "{}", "Bearer "+token); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if response := controlRequest(main, "GET", "/v1/projects", "", "Bearer "+token); response.Code != 200 {
		t.Fatal("shared token rejected by Control API")
	}
	if response := controlRequest(main, "HEAD", "/v1/projects", "", "Bearer "+token); response.Code != 405 {
		t.Fatal("read-only grants changed method validation")
	}
	if response := controlRequest(main, "POST", "/v1/projects/compose:app/actions", `{"action":"restart"}`, "Bearer "+token); response.Code != 403 || controller.calls != 0 {
		t.Fatal("read token mutated Docker")
	}
	settings, _ = data.MachineSettings()
	settings.AuthMode = "anonymous"
	if _, err := data.SaveMachineSettings(settings); err != nil {
		t.Fatal(err)
	}
	if response := controlRequest(mcp, "POST", "/mcp", "{}", ""); response.Code != 200 {
		t.Fatal("anonymous mode failed")
	}
	if response := controlRequest(mcp, "POST", "/mcp", "{}", "Bearer invalid"); response.Code != 401 {
		t.Fatal("invalid credential downgraded")
	}
	if response := controlRequest(main, "GET", "/v1/projects", "", ""); response.Code != 401 {
		t.Fatal("anonymous MCP exposed Control API")
	}
	settings, _ = data.MachineSettings()
	settings.AuthMode = "token"
	if _, err := data.SaveMachineSettings(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := data.RotateMachineToken("owner", false); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		h    http.Handler
		path string
	}{{mcp, "/mcp"}, {main, "/v1/projects"}} {
		if response := controlRequest(route.h, "GET", route.path, "", "Bearer "+token); response.Code != 401 {
			t.Fatal("old token accepted")
		}
	}
}

func TestMachineAutomaticApplyPersistsBeforeLaunchingAndReportsFailure(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	network := store.MachineNetwork{Yard: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8095}, MCP: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8096}}
	if err := data.InitializeMachine(network, false, ""); err != nil {
		t.Fatal(err)
	}
	calls := 0
	api.ConfigureMachineAccess(nil, func(_ context.Context, revision int64) (store.Job, error) {
		calls++
		saved, err := data.MachineSettings()
		if err != nil || saved.Revision != revision || saved.Network.MCP.Port != 18096 {
			t.Error("worker launched before desired configuration was durable")
		}
		return store.Job{}, errors.New("fixture unavailable")
	})
	handler := api.Handler()
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	var session bootstrapResponse
	_ = json.Unmarshal(setup.Body.Bytes(), &session)
	save := func(settings store.MachineSettings) *httptest.ResponseRecorder {
		body, _ := json.Marshal(settings)
		r := httptest.NewRequest("PUT", "http://yard.test/api/machine", strings.NewReader(string(body)))
		r.AddCookie(setup.Result().Cookies()[0])
		r.Header.Set("Origin", "http://yard.test")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	settings, _ := data.MachineSettings()
	settings.AutoApply = true
	if response := save(settings); response.Code != 200 || calls != 0 {
		t.Fatal("unrelated save launched recreation")
	}
	settings, _ = data.MachineSettings()
	settings.Network.MCP.Port = 18096
	if response := save(settings); response.Code != 200 || calls != 1 || !strings.Contains(response.Body.String(), "applyError") {
		t.Fatal("saved pending settings or apply failure was hidden")
	}
	if response := save(settings); response.Code != 409 || calls != 1 {
		t.Fatal("stale settings launched recreation")
	}
}

func TestMachineSettingsSessionCSRFAndSecretResponses(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	if err := data.InitializeMachine(store.MachineNetwork{Yard: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8095}, MCP: store.NetworkEndpoint{Bind: "127.0.0.1", Port: 8096}}, false, ""); err != nil {
		t.Fatal(err)
	}
	api.ConfigureMachineAccess(nil, nil)
	handler := api.Handler()
	for _, path := range []string{"/api/machine", "/api/machine/token/history"} {
		if response := controlRequest(handler, "GET", path, "", ""); response.Code != 401 {
			t.Fatal("session not required")
		}
	}
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	var session bootstrapResponse
	if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookie := setup.Result().Cookies()[0]
	invoke := func(path, csrf, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://yard.test"+path, strings.NewReader(`{"confirm":true}`))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if response := invoke("/api/machine/token", "", "http://yard.test"); response.Code != 403 {
		t.Fatal("CSRF not required")
	}
	if response := invoke("/api/machine/token", session.CSRFToken, "http://evil.test"); response.Code != 403 {
		t.Fatal("Origin not required")
	}
	response := invoke("/api/machine/token", session.CSRFToken, "http://yard.test")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var issued struct{ Token string }
	_ = json.Unmarshal(response.Body.Bytes(), &issued)
	if issued.Token == "" {
		t.Fatal("no token")
	}
	for _, path := range []string{"/api/machine", "/api/machine/token/history"} {
		r := httptest.NewRequest("GET", "http://yard.test"+path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), issued.Token) || strings.Contains(w.Body.String(), "token_digest") {
			t.Fatal("secret in settings/history")
		}
	}
}
