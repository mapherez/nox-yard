package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/logexport"
	"github.com/mapherez/nox-yard/internal/managed"
)

type exportInventory struct{ inventoryStub }

func (r *exportInventory) Snapshot(context.Context) (inventory.Snapshot, error) {
	return inventory.Snapshot{Projects: []inventory.Project{{ID: "compose:sample", Name: "sample", Containers: []inventory.Container{{ID: strings.Repeat("a", 64)}}}, {ID: "container:" + strings.Repeat("b", 64), Name: "standalone", Containers: []inventory.Container{{ID: strings.Repeat("b", 64)}}}}}, nil
}

type exportManager struct {
	*managed.Manager
	reads, writes int
	name, content string
}

func (m *exportManager) ProjectLogExport(_ context.Context, name string, content *string) (logexport.Status, error) {
	m.name = name
	if content == nil {
		m.reads++
	} else {
		m.writes++
		m.content = *content
	}
	return logexport.Status{Available: true, Directory: "/srv/projects/" + name + "/logs", Filename: "2026-10-20-11-37.txt", ExportedAt: "2026-10-20T11:37:00Z"}, nil
}

func TestLogExportRequiresSessionConfirmationAndProjectMembership(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	fake := &exportManager{}
	api.application.Managed = fake
	api.SetInventory(&exportInventory{})
	handler := api.Handler()
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	var session bootstrapResponse
	_ = json.Unmarshal(setup.Body.Bytes(), &session)
	cookie := setup.Result().Cookies()[0]
	request := func(method, id, origin, csrf, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://yard.test/api/projects/"+id+"/logs/export", strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	valid := `{"confirm":true,"containerID":"` + strings.Repeat("a", 64) + `","content":"fixture log\n"}`
	for _, method := range []string{"GET", "POST"} {
		if w := request(method, "compose:sample", "http://yard.test", session.CSRFToken, valid, false); w.Code != 401 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
	for _, headers := range [][2]string{{"http://evil.test", session.CSRFToken}, {"http://yard.test", ""}} {
		if w := request("POST", "compose:sample", headers[0], headers[1], valid, true); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	for _, body := range []string{strings.Replace(valid, `"confirm":true`, `"confirm":false`, 1), strings.TrimSuffix(valid, "}") + `,"path":"/elsewhere"}`, valid + `{}`} {
		if w := request("POST", "compose:sample", "http://yard.test", session.CSRFToken, body, true); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request("POST", "compose:sample", "http://yard.test", session.CSRFToken, strings.ReplaceAll(valid, strings.Repeat("a", 64), strings.Repeat("b", 64)), true); w.Code != 404 {
		t.Fatal("cross-project export", w.Code, w.Body.String())
	}
	if fake.reads+fake.writes != 0 {
		t.Fatal("rejected request reached filesystem controller")
	}
	if w := request("GET", "compose:sample", "http://yard.test", "", "", true); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "compose:sample", "http://yard.test", session.CSRFToken, valid, true); w.Code != 201 || fake.name != "sample" || fake.content != "fixture log\n" {
		t.Fatal(w.Code, fake.name, fake.content)
	}
	standalone := strings.ReplaceAll(valid, strings.Repeat("a", 64), strings.Repeat("b", 64))
	if w := request("POST", "container:"+strings.Repeat("b", 64), "http://yard.test", session.CSRFToken, standalone, true); w.Code != 201 || fake.name != "standalone" {
		t.Fatal(w.Code, fake.name)
	}
}
