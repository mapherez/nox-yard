package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
)

func TestJobRoutesProtectHistoryAndRecovery(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	job, _ := store.NewJob("compose:sample", "managed", "update", nil)
	job.Payload = "private-input-secret"
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishJob(job.ID, job.Owner, "failed", "recovery_required", "Review the host.", ""); err != nil {
		t.Fatal(err)
	}
	handler := api.Handler()
	paths := []string{"/api/jobs?target=compose:sample", "/api/jobs/" + job.ID}
	for _, path := range paths {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://yard.test"+path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous history: %d", w.Code)
		}
	}
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	if setup.Code != http.StatusCreated {
		t.Fatalf("setup: %d", setup.Code)
	}
	var session bootstrapResponse
	if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookie := setup.Result().Cookies()[0]
	for _, path := range paths {
		r := httptest.NewRequest(http.MethodGet, "http://yard.test"+path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || strings.Contains(w.Body.String(), job.Payload) || strings.Contains(w.Body.String(), job.Owner) {
			t.Fatalf("history response leaked private data or failed: %d %s", w.Code, w.Body.String())
		}
	}
	for _, test := range []struct{ origin, csrf string }{{"http://yard.test", ""}, {"http://evil.test", session.CSRFToken}} {
		r := httptest.NewRequest(http.MethodPost, "http://yard.test/api/jobs/"+job.ID+"/recovery", strings.NewReader(`{"confirm":true,"updatedAt":1}`))
		r.AddCookie(cookie)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-CSRF-Token", test.csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("unprotected recovery: %d %s", w.Code, w.Body.String())
		}
	}
	current, _, _ := data.Job(job.ID)
	if current.Outcome != "recovery_required" {
		t.Fatal("rejected request released ownership")
	}
}
