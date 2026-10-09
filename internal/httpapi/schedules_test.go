package httpapi

import (
	"encoding/json"
	"github.com/mapherez/nox-yard/internal/schedule"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScheduleRoutesRequireSessionOriginCSRFAndExplicitFlag(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	api.SetInventory(&inventoryStub{})
	api.application.Scheduler = &schedule.Manager{Data: data, Location: time.UTC}
	handler := api.Handler()
	url := "http://yard.test/api/projects/compose:yard/schedule"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	var session bootstrapResponse
	json.Unmarshal(setup.Body.Bytes(), &session)
	cookie := setup.Result().Cookies()[0]
	for _, test := range []struct {
		method, body, origin, csrf string
		want                       int
	}{
		{"GET", "", "", "", 200},
		{"PUT", `{"enabled":false}`, "http://evil.test", session.CSRFToken, 403},
		{"PUT", `{"enabled":false}`, "http://yard.test", "", 403},
		{"PUT", `{}`, "http://yard.test", session.CSRFToken, 400},
		{"PUT", `{"enabled":false}`, "http://yard.test", session.CSRFToken, 200},
		{"PUT", `{"enabled":false,"time":"06:45"}`, "http://yard.test", session.CSRFToken, 200},
		{"PUT", `{"enabled":false,"time":"24:00"}`, "http://yard.test", session.CSRFToken, 400},
		{"PUT", `{"enabled":false,"time":""}`, "http://yard.test", session.CSRFToken, 400},
		{"PUT", `{"enabled":true}`, "http://yard.test", session.CSRFToken, 400},
	} {
		r := httptest.NewRequest(test.method, url, strings.NewReader(test.body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-CSRF-Token", test.csrf)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatal(test, w.Code, w.Body.String())
		}
		if test.method == http.MethodGet && (!strings.Contains(w.Body.String(), `"enabled":false`) || !strings.Contains(w.Body.String(), `"timezone":"UTC"`)) {
			t.Fatal("default setting missing explicit timezone", w.Body.String())
		}
	}
	row, _, _ := data.ProjectSchedule(t.Context(), "compose:yard")
	if row.Enabled || row.Time != "06:45" {
		t.Fatal("unsupported project enabled or saved time lost", row)
	}
}
