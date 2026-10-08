package httpapi

import (
	"context"
	"encoding/json"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recreateStub struct {
	calls int
	err   error
}

func (s *recreateStub) Preview(ctx context.Context, input recreate.Request) (recreate.Preview, error) {
	s.calls++
	return recreate.Preview{Fingerprint: strings.Repeat("a", 64), Items: []recreate.Item{}}, s.err
}
func (s *recreateStub) Submit(ctx context.Context, input recreate.Request) (store.Job, error) {
	s.calls++
	if !input.Confirm {
		return store.Job{}, recreate.ErrInvalid
	}
	return store.Job{ID: "accepted", Status: "running", Payload: "private-source-secret", Owner: "private-owner-secret"}, s.err
}

func TestRecreationRoutesRequireAuthOriginCSRFFreshConfirmationAndHidePayload(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	fake := &recreateStub{}
	api.application.Recreator = fake
	handler := api.Handler()
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	var session bootstrapResponse
	_ = json.Unmarshal(setup.Body.Bytes(), &session)
	cookie := setup.Result().Cookies()[0]
	request := func(path, origin, csrf, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://yard.test"+path, strings.NewReader(body))
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
	body := `{"id":"compose:sample","operation":"update"}`
	for _, path := range []string{"/api/recreate/preview", "/api/recreate/submit"} {
		if w := request(path, "http://yard.test", session.CSRFToken, body, false); w.Code != 401 {
			t.Fatal("anonymous replacement", w.Code)
		}
		for _, headers := range [][2]string{{"http://evil.test", session.CSRFToken}, {"http://yard.test", ""}} {
			if w := request(path, headers[0], headers[1], body, true); w.Code != 403 {
				t.Fatal("unprotected replacement", w.Code)
			}
		}
	}
	if fake.calls != 0 {
		t.Fatal("rejected requests reached replacement manager")
	}
	if w := request("/api/recreate/preview", "http://yard.test", session.CSRFToken, body, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("/api/recreate/submit", "http://yard.test", session.CSRFToken, body, true); w.Code != 400 {
		t.Fatal("unconfirmed replacement accepted", w.Code)
	}
	body = `{"id":"compose:sample","operation":"update","confirm":true,"fingerprint":"` + strings.Repeat("a", 64) + `"}`
	if w := request("/api/recreate/submit", "http://yard.test", session.CSRFToken, body, true); w.Code != 202 || strings.Contains(w.Body.String(), "private-") {
		t.Fatal("bad acceptance/private payload", w.Code, w.Body.String())
	}
	fake.err = recreate.ErrUnsupported
	if w := request("/api/recreate/preview", "http://yard.test", session.CSRFToken, body, true); w.Code != 400 || !strings.Contains(w.Body.String(), "safely recreated") {
		t.Fatal("unsupported configuration lost its reason", w.Code, w.Body.String())
	}
}
