package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mapherez/nox-yard/internal/auth"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

type inventoryStub struct {
	calls int
}

func (s *inventoryStub) Snapshot(context.Context) (inventory.Snapshot, error) {
	s.calls++
	return inventory.Snapshot{Projects: []inventory.Project{{ID: "compose:yard", Name: "yard"}}}, nil
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
	return data, api
}

func postCredentials(handler http.Handler, path, origin, username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(credentials{Username: username, Password: password})
	request := httptest.NewRequest(http.MethodPost, "http://yard.test"+path, strings.NewReader(string(body)))
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	return result
}

func TestFirstRunPersistsAndRequiresLoginAfterLogout(t *testing.T) {
	dataDir := t.TempDir()
	data, api := newTestServer(t, dataDir, "")
	handler := api.Handler()

	request := httptest.NewRequest(http.MethodGet, "http://yard.test/api/bootstrap", nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"needsSetup":true`) {
		t.Fatalf("expected first-run setup, got %d %s", result.Code, result.Body.String())
	}

	badOrigin := postCredentials(handler, "/api/setup", "http://evil.test", "owner", testPassword)
	if badOrigin.Code != http.StatusForbidden {
		t.Fatalf("cross-origin setup returned %d", badOrigin.Code)
	}

	created := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	if created.Code != http.StatusCreated {
		t.Fatalf("setup returned %d: %s", created.Code, created.Body.String())
	}
	if len(created.Result().Cookies()) != 1 || !created.Result().Cookies()[0].HttpOnly || created.Result().Cookies()[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie does not have expected protection")
	}
	cookie := created.Result().Cookies()[0]
	var session bootstrapResponse
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if !session.Authenticated || session.CSRFToken == "" || session.Username != "owner" {
		t.Fatal("setup did not establish a session")
	}

	again := postCredentials(handler, "/api/setup", "http://yard.test", "other", testPassword)
	if again.Code != http.StatusConflict {
		t.Fatalf("repeat setup returned %d", again.Code)
	}
	if err := data.Close(); err != nil {
		t.Fatal(err)
	}

	data, api = newTestServer(t, dataDir, "")
	defer data.Close()
	handler = api.Handler()
	request = httptest.NewRequest(http.MethodGet, "http://yard.test/api/bootstrap", nil)
	request.AddCookie(cookie)
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"authenticated":true`) {
		t.Fatalf("persisted session unavailable: %d %s", result.Code, result.Body.String())
	}

	logout := httptest.NewRequest(http.MethodPost, "http://yard.test/api/logout", nil)
	logout.Header.Set("Origin", "http://yard.test")
	logout.AddCookie(cookie)
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, logout)
	if result.Code != http.StatusForbidden {
		t.Fatalf("logout without CSRF token returned %d", result.Code)
	}
	logout.Header.Set("X-CSRF-Token", session.CSRFToken)
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, logout)
	if result.Code != http.StatusNoContent {
		t.Fatalf("logout returned %d: %s", result.Code, result.Body.String())
	}

	wrong := postCredentials(handler, "/api/login", "http://yard.test", "owner", "incorrect-password")
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password returned %d", wrong.Code)
	}
	login := postCredentials(handler, "/api/login", "http://yard.test", "owner", testPassword)
	if login.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", login.Code, login.Body.String())
	}
	newHash, err := auth.HashPassword("another-long-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := data.ResetAdminPassword(newHash); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "http://yard.test/api/bootstrap", nil)
	request.AddCookie(login.Result().Cookies()[0])
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if !strings.Contains(result.Body.String(), `"authenticated":false`) {
		t.Fatal("password reset did not revoke sessions")
	}
}

func TestOnlyOneConcurrentFirstRunSetupSucceeds(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	handler := api.Handler()

	var group sync.WaitGroup
	results := make(chan int, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword).Code
		}()
	}
	group.Wait()
	close(results)
	created, conflict := 0, 0
	for code := range results {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected setup response %d", code)
		}
	}
	if created != 1 || conflict != 1 {
		t.Fatalf("expected one setup and one conflict, got %d and %d", created, conflict)
	}
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
