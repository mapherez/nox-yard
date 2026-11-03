package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

type inventoryStub struct {
	calls        int
	inspectCalls int
}

func TestCSPAllowsTerminalStylesWithoutInlineScripts(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()

	result := httptest.NewRecorder()
	api.Handler().ServeHTTP(result, httptest.NewRequest(http.MethodGet, "http://yard.test/api/bootstrap", nil))
	policy := result.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("terminal runtime styles are blocked by CSP: %q", policy)
	}
	if !strings.Contains(policy, "script-src 'self';") {
		t.Fatalf("CSP must continue to restrict scripts to this origin: %q", policy)
	}
}

func (s *inventoryStub) Snapshot(context.Context) (inventory.Snapshot, error) {
	s.calls++
	return inventory.Snapshot{Projects: []inventory.Project{{ID: "compose:yard", Name: "yard"}}}, nil
}

func (s *inventoryStub) InspectContainer(_ context.Context, id string, reveal bool) (inventory.ContainerInspection, error) {
	s.inspectCalls++
	variable := inventory.EnvironmentVariable{Name: "TOKEN"}
	if reveal {
		value := "secret-value"
		variable.Value = &value
	}
	return inventory.ContainerInspection{ID: id, Environment: []inventory.EnvironmentVariable{variable}}, nil
}

func TestContainerInspectionRequiresSessionAndExplicitReveal(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	reader := &inventoryStub{}
	api.SetInventory(reader)
	handler := api.Handler()
	id := strings.Repeat("a", 64)
	path := "/api/containers/" + id

	request := httptest.NewRequest(http.MethodGet, "http://yard.test"+path, nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusUnauthorized || reader.inspectCalls != 0 {
		t.Fatal("unauthenticated request reached Docker inspect")
	}

	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup returned %d", setup.Code)
	}
	var session bootstrapResponse
	if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookie := setup.Result().Cookies()[0]
	request.AddCookie(cookie)
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK || strings.Contains(result.Body.String(), "secret-value") {
		t.Fatalf("default inspection leaked a value: %d %s", result.Code, result.Body.String())
	}

	reveal := httptest.NewRequest(http.MethodPost, "http://yard.test"+path+"/environment", nil)
	reveal.Header.Set("Origin", "http://yard.test")
	reveal.AddCookie(cookie)
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, reveal)
	if result.Code != http.StatusForbidden {
		t.Fatalf("reveal without CSRF token returned %d", result.Code)
	}
	reveal.Header.Set("X-CSRF-Token", session.CSRFToken)
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, reveal)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "secret-value") || reader.inspectCalls != 2 {
		t.Fatalf("explicit reveal returned %d: %s", result.Code, result.Body.String())
	}
}

func TestProjectsRequiresSession(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	reader := &inventoryStub{}
	api.SetInventory(reader)
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "http://yard.test/api/projects", nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusUnauthorized || reader.calls != 0 {
		t.Fatalf("unauthenticated inventory request returned %d with %d Docker reads", result.Code, reader.calls)
	}

	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup returned %d", setup.Code)
	}
	request.AddCookie(setup.Result().Cookies()[0])
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK || reader.calls != 1 || !strings.Contains(result.Body.String(), `"id":"compose:yard"`) {
		t.Fatalf("authenticated inventory request returned %d: %s", result.Code, result.Body.String())
	}
}

const testPassword = "a-long-test-password"

func newTestServer(t *testing.T, dataDir, publicURL string) (*store.Store, *Server) {
	t.Helper()
	data, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	api, err := New(data, "", publicURL)
	if err != nil {
		data.Close()
		t.Fatal(err)
	}
	configureLoginFixture(t, data, api, publicURL)
	return data, api
}

// postCredentials seeds a verified Auth fixture session for existing domain tests.
// It does not call the removed password routes; login security is tested through
// the real OIDC browser/code flow in nox_login_test.go.
func postCredentials(handler http.Handler, path, origin, username, _ string) *httptest.ResponseRecorder {
	result := httptest.NewRecorder()
	api := handler.(yardHandler).server
	request := httptest.NewRequest(http.MethodGet, "http://yard.test/", nil)
	request.Header.Set("Origin", origin)
	if !api.checkOrigin(result, request) {
		return result
	}
	fixture := loginFixtures.Load(api)
	credentials := fixture.Issue("nox-yard", username+"@example.test")
	if err := api.issueNoXSession(result, credentials); err != nil {
		panic(err)
	}
	cookie := result.Result().Cookies()[0]
	_, _, sessionFound, err := api.store.NoXSession(hashToken(cookie.Value))
	if err != nil || !sessionFound {
		panic("fixture session was not saved")
	}
	session, _, _, _ := api.store.NoXSession(hashToken(cookie.Value))
	status := http.StatusOK
	if path == "/api/setup" {
		status = http.StatusCreated
	}
	writeJSON(result, status, bootstrapResponse{Authenticated: true, Username: session.Username, CSRFToken: session.CSRFToken, LoginAvailable: true})
	return result
}

func TestHTTPSOriginUsesSecureHostCookie(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "https://yard.test")
	defer data.Close()
	response := postCredentials(api.Handler(), "/api/setup", "https://yard.test", "owner", testPassword)
	if response.Code != http.StatusCreated {
		t.Fatalf("setup returned %d: %s", response.Code, response.Body.String())
	}
	cookie := response.Result().Cookies()[0]
	if cookie.Name != "__Host-nox_session" || !cookie.Secure || cookie.Path != "/" {
		t.Fatalf("unexpected secure cookie: %+v", cookie)
	}
}
