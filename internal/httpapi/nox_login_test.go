package httpapi

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	noxauth "github.com/mapherez/nox-auth/client"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/testauth"
)

type fixtureMap struct{ sync.Map }

func (m *fixtureMap) Load(s *Server) *testauth.Provider {
	v, ok := m.Map.Load(s)
	if !ok {
		panic("missing Auth fixture")
	}
	return v.(*testauth.Provider)
}

var loginFixtures fixtureMap

func configureLoginFixture(t *testing.T, data *store.Store, api *Server, origin string) {
	t.Helper()
	if origin == "" {
		origin = "http://yard.test"
	}
	directory, err := os.MkdirTemp("", "yl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socket := filepath.Join(directory, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := testauth.New()
	if err != nil {
		t.Fatal(err)
	}
	secret := noxauth.GenerateSecret()
	provider.Register("nox-yard", secret)
	if err = data.SaveNoXLoginConfiguration(store.NoXLoginConfig{Issuer: testauth.Issuer, Origin: origin, ClientID: "nox-yard", Secret: secret, SocketPath: socket}); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: provider.Handler()}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	loginFixtures.Store(api, provider)
	t.Cleanup(func() { loginFixtures.Delete(api) })
}
func TestNoXLoginRealOIDCRoundTripAndPersistence(t *testing.T) {
	dataDir := t.TempDir()
	data, api := newTestServer(t, dataDir, "")
	defer data.Close()
	yard := httptest.NewServer(api.Handler())
	defer yard.Close()
	cfg, _, _ := data.NoXLoginConfiguration()
	cfg.Origin = yard.URL
	if err := data.SaveNoXLoginConfiguration(cfg); err != nil {
		t.Fatal(err)
	}
	provider := loginFixtures.Load(api)
	issuer := httptest.NewServer(provider.Handler())
	defer issuer.Close()
	jar, _ := cookiejar.New(nil)
	browser := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	start, err := browser.Get(yard.URL + "/auth/nox/start?return_to=%2Fprojects%3Fview%3Dactive")
	if err != nil {
		t.Fatal(err)
	}
	start.Body.Close()
	if start.StatusCode != 302 {
		t.Fatal(start.StatusCode)
	}
	authURL, _ := url.Parse(start.Header.Get("Location"))
	authURL.Scheme = "http"
	authURL.Host = strings.TrimPrefix(issuer.URL, "http://")
	authorized, err := browser.Get(authURL.String())
	if err != nil {
		t.Fatal(err)
	}
	authorized.Body.Close()
	callback, err := browser.Get(authorized.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callback.Body.Close()
	if callback.StatusCode != 303 || callback.Header.Get("Location") != "/projects?view=active" {
		t.Fatal("incorrect login return", callback.StatusCode, callback.Header.Get("Location"))
	}
	response, err := browser.Get(yard.URL + "/api/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	var session bootstrapResponse
	err = json.NewDecoder(response.Body).Decode(&session)
	response.Body.Close()
	if err != nil || !session.Authenticated || session.Username != "owner@example.test" || session.CSRFToken == "" {
		t.Fatal("missing NoX identity", err, session)
	}
	initial := provider.Checks()
	response, err = browser.Get(yard.URL + "/api/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if provider.Checks() != initial {
		t.Fatal("admission was not reused")
	}
	// Legacy HTTP entry points must remain unavailable even for a valid Auth session.
	for _, path := range []string{"/api/setup", "/api/login"} {
		req, _ := http.NewRequest("POST", yard.URL+path, strings.NewReader(`{}`))
		req.Header.Set("Origin", yard.URL)
		resp, err := browser.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 && resp.StatusCode != 405 {
			t.Fatal("password endpoint remains", path, resp.StatusCode)
		}
	}
	// Callback replay cannot create another session.
	replay, err := browser.Get(authorized.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	replay.Body.Close()
	if replay.StatusCode != 400 {
		t.Fatal("callback replay accepted")
	}
	// The database can reopen without changing schema version or losing sessions.
	if err = data.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	fresh, err := New(reopened, "", yard.URL)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", yard.URL+"/api/bootstrap", nil)
	u, _ := url.Parse(yard.URL)
	for _, cookie := range jar.Cookies(u) {
		req.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	fresh.Handler().ServeHTTP(result, req)
	if !strings.Contains(result.Body.String(), `"authenticated":true`) {
		t.Fatal("session lost after restart", result.Body.String())
	}
}
func TestNoXLoginCallbackBindingAndUnsafeReturns(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	handler := api.Handler()
	for _, target := range []string{"https://attacker.test/", "//attacker.test/", "/%2F%2Fattacker.test/"} {
		request := httptest.NewRequest("GET", "http://yard.test/auth/nox/start?return_to="+url.QueryEscape(target), nil)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, request)
		if result.Code != 400 {
			t.Fatal("unsafe return accepted", target, result.Code)
		}
	}
	request := httptest.NewRequest("GET", "http://yard.test/auth/nox/callback?state="+noxauth.GenerateSecret()+"&code=forged", nil)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if result.Code != 400 {
		t.Fatal("unbound callback accepted")
	}
	oldHash := "legacy-session"
	if err := data.CreateAdmin("legacy", "preserved-password-hash"); err != nil {
		t.Fatal(err)
	}
	if err := data.CreateSession(hashToken(oldHash), "legacy-csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest("GET", "http://yard.test/api/bootstrap", nil)
	request.AddCookie(&http.Cookie{Name: "nox_session", Value: oldHash})
	result = httptest.NewRecorder()
	handler.ServeHTTP(result, request)
	if strings.Contains(result.Body.String(), `"authenticated":true`) {
		t.Fatal("legacy password session admitted")
	}
	old, err := data.Admin()
	if err != nil || old.PasswordHash != "preserved-password-hash" {
		t.Fatal("rollback administrator changed")
	}
}
func TestLocalLogoutAndAuthRevocation(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	handler := api.Handler()
	login := postCredentials(handler, "/api/setup", "http://yard.test", "owner", "")
	cookie := login.Result().Cookies()[0]
	var b bootstrapResponse
	_ = json.Unmarshal(login.Body.Bytes(), &b)
	req := httptest.NewRequest("POST", "http://yard.test/api/logout", nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", "http://yard.test")
	req.Header.Set("X-CSRF-Token", b.CSRFToken)
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if out.Code != 204 {
		t.Fatal("logout failed", out.Code)
	}
	req = httptest.NewRequest("GET", "http://yard.test/api/bootstrap", nil)
	req.AddCookie(cookie)
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if strings.Contains(out.Body.String(), `"authenticated":true`) {
		t.Fatal("local session survived logout")
	}
	login = postCredentials(handler, "/api/setup", "http://yard.test", "owner", "")
	req = httptest.NewRequest("GET", "http://yard.test/api/bootstrap", nil)
	req.AddCookie(login.Result().Cookies()[0])
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	loginFixtures.Load(api).Deny()
	api.noxLogin.mu.Lock()
	clear(api.noxLogin.sessions)
	api.noxLogin.mu.Unlock()
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	if strings.Contains(out.Body.String(), `"authenticated":true`) {
		t.Fatal("revoked identity stayed admitted")
	}
}

func TestLocalLogoutDuringAuthOutageRetainsOriginAndCSRF(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	handler := api.Handler()
	login := postCredentials(handler, "/api/setup", "http://yard.test", "owner", "")
	cookie := login.Result().Cookies()[0]
	var session bootstrapResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	config, _, err := data.NoXLoginConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	config.SocketPath = filepath.Join(t.TempDir(), "unavailable")
	if err := data.SaveNoXLoginConfiguration(config); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		origin, csrf string
		status       int
	}{
		{"http://other.test", session.CSRFToken, 403},
		{"http://yard.test", "", 403},
		{"http://yard.test", session.CSRFToken, 204},
	} {
		req := httptest.NewRequest("POST", "http://yard.test/api/logout", nil)
		req.AddCookie(cookie)
		req.Header.Set("Origin", test.origin)
		req.Header.Set("X-CSRF-Token", test.csrf)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != test.status {
			t.Fatalf("logout returned %d, expected %d", result.Code, test.status)
		}
	}
	if _, _, found, err := data.NoXSession(hashToken(cookie.Value)); err != nil || found {
		t.Fatal("Local session survived logout", err)
	}
}
