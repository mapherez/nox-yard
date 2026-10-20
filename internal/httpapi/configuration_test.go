package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/managed"
)

type configurationStub struct {
	*managed.Manager
	reads, saves int
}

func (s *configurationStub) Configuration(context.Context, string) (managed.FileConfigurationView, error) {
	s.reads++
	return managed.FileConfigurationView{Available: true, State: "ready", Fingerprint: strings.Repeat("a", 64), Files: []managed.ConfigurationFile{{Path: "compose.yaml", Content: "private-file-value", Compose: true}}, Variables: []managed.ConfigurationVariable{{Name: "TOKEN", Value: "private-env-value", Service: "web"}}}, nil
}
func (s *configurationStub) SaveConfiguration(_ context.Context, _ string, in managed.ConfigurationEdit) (managed.FileConfigurationView, error) {
	s.saves++
	if in.Fingerprint != strings.Repeat("a", 64) {
		return managed.FileConfigurationView{}, fmt.Errorf("%w: configuration changed; reload before saving", managed.ErrConflict)
	}
	return s.Configuration(context.Background(), "")
}

func TestConfigurationRoutesProtectSecretsAndConcurrentWrites(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	fake := &configurationStub{}
	api.application.Managed = fake
	handler := api.Handler()
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	var session bootstrapResponse
	_ = json.Unmarshal(setup.Body.Bytes(), &session)
	cookie := setup.Result().Cookies()[0]
	request := func(operation, origin, csrf, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://yard.test/api/projects/compose:sample/configuration/"+operation, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, operation := range []string{"read", "save"} {
		if w := request(operation, "http://yard.test", session.CSRFToken, `{}`, false); w.Code != 401 {
			t.Fatal("anonymous configuration access", operation, w.Code)
		}
		for _, headers := range [][2]string{{"http://evil.test", session.CSRFToken}, {"http://yard.test", ""}} {
			if w := request(operation, headers[0], headers[1], `{}`, true); w.Code != 403 {
				t.Fatal("unprotected configuration access", operation, w.Code)
			}
		}
	}
	if fake.reads+fake.saves != 0 {
		t.Fatal("rejected requests reached configuration manager")
	}
	for _, reveal := range []bool{false, true} {
		w := request("read", "http://yard.test", session.CSRFToken, fmt.Sprintf(`{"reveal":%t}`, reveal), true)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-") != reveal {
			t.Fatal("incorrect secret reveal policy", w.Body.String())
		}
	}
	w := request("save", "http://yard.test", session.CSRFToken, `{"fingerprint":"stale","service":"web","variable":"TOKEN","content":"private-new-value"}`, true)
	if w.Code != 409 || strings.Contains(w.Body.String(), "private-") {
		t.Fatal("stale write lost conflict or leaked value", w.Code, w.Body.String())
	}
	history, err := data.Jobs(context.Background(), "compose:sample", false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(history)
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("configuration edit leaked into public job history")
	}
	for _, path := range []string{"/api/configuration-migration", "/api/projects/compose:sample/configuration/retry-migration"} {
		r := httptest.NewRequest(http.MethodGet, "http://yard.test"+path, nil)
		r.AddCookie(cookie)
		if strings.Contains(path, "retry-migration") {
			r.Method = http.MethodPost
			r.Header.Set("Origin", "http://yard.test")
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("retired endpoint %s returned %d", path, w.Code)
		}
	}
}
