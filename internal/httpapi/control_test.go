package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
)

const controlTestKey = "independent-machine-test-key"

type controlReaderFake struct {
	snapshot   inventory.Snapshot
	err        error
	inspectErr error
	calls      int
	inspects   int
	reveal     bool
	deadline   time.Time
}

func (f *controlReaderFake) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	f.calls++
	f.deadline, _ = ctx.Deadline()
	return f.snapshot, f.err
}
func (f *controlReaderFake) InspectContainer(_ context.Context, id string, reveal bool) (inventory.ContainerInspection, error) {
	f.inspects++
	f.reveal = reveal
	secret := "must-never-leak"
	return inventory.ContainerInspection{ID: id, Ports: []inventory.PortInfo{{ContainerPort: "80/tcp", HostPort: "8090"}}, Mounts: []inventory.MountInfo{{Type: "volume", Source: "app_data", Destination: "/data", ReadOnly: true}}, Networks: []inventory.NetworkInfo{{Name: "app_default", IPv4: "172.18.0.2"}}, Environment: []inventory.EnvironmentVariable{{Name: "API_KEY", Value: &secret}}}, f.inspectErr
}

type controlLifecycleFake struct {
	lifecycle.Controller
	calls     int
	id        string
	container bool
	action    lifecycle.Action
	result    lifecycle.Result
	pull      lifecycle.MaintenanceResult
	err       error
}

func (f *controlLifecycleFake) Container(_ context.Context, id string, action lifecycle.Action) (lifecycle.Result, error) {
	f.calls++
	f.id, f.action, f.container = id, action, true
	return f.result, f.err
}
func (f *controlLifecycleFake) Project(_ context.Context, id string, action lifecycle.Action) (lifecycle.Result, error) {
	f.calls++
	f.id, f.action, f.container = id, action, false
	return f.result, f.err
}
func (f *controlLifecycleFake) PullContainer(_ context.Context, id string) (lifecycle.MaintenanceResult, error) {
	f.calls++
	f.id, f.container = id, true
	return f.pull, f.err
}
func (f *controlLifecycleFake) PullProject(_ context.Context, id string) (lifecycle.MaintenanceResult, error) {
	f.calls++
	f.id, f.container = id, false
	return f.pull, f.err
}

func controlFixture(t *testing.T, enabled bool) (*store.Store, *Server, *controlReaderFake, *controlLifecycleFake) {
	t.Helper()
	data, api := newTestServer(t, t.TempDir(), "")
	t.Cleanup(func() { _ = data.Close() })
	if err := api.ConfigureControlAPI(ControlConfig{Enabled: enabled, Key: controlTestKey, Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	reader := &controlReaderFake{snapshot: inventory.Snapshot{CollectedAt: time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC), Projects: []inventory.Project{{ID: "compose:app", Name: "app", Kind: "external-compose", State: "running", Health: "none", Containers: []inventory.Container{{ID: strings.Repeat("a", 64), Name: "app-1", Image: "app:latest", State: "running", Health: "none"}}}}}}
	controller := &controlLifecycleFake{result: lifecycle.Result{Succeeded: 1}, pull: lifecycle.MaintenanceResult{Succeeded: 1, Images: []string{"app:latest"}}}
	api.SetInventory(reader)
	api.SetLifecycle(controller)
	return data, api, reader, controller
}

func controlRequest(handler http.Handler, method, path, body, authorization string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://yard.test"+path, strings.NewReader(body))
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func controlCode(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var problem controlError
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("non-JSON response: %s", response.Body.String())
	}
	if response.Code != status || problem.Code != code {
		t.Fatalf("got %d %s, want %d %s", response.Code, response.Body.String(), status, code)
	}
	if strings.Contains(response.Body.String(), controlTestKey) || strings.Contains(response.Body.String(), "must-never-leak") {
		t.Fatal("response leaked a secret")
	}
}

func TestControlPublicAndProtectedRoutes(t *testing.T) {
	id := strings.Repeat("a", 64)
	routes := []struct{ method, path, body string }{
		{"GET", "/v1/status", ""}, {"GET", "/v1/projects", ""}, {"GET", "/v1/containers/" + id, ""},
		{"POST", "/v1/containers/" + id + "/actions", `{"action":"start"}`}, {"POST", "/v1/projects/compose:app/actions", `{"action":"start"}`},
		{"POST", "/v1/containers/" + id + "/pull", ""}, {"POST", "/v1/projects/compose:app/pull", ""},
	}
	for _, enabled := range []bool{false, true} {
		_, api, reader, controller := controlFixture(t, enabled)
		handler := api.Handler()
		for _, path := range []string{"/v1/health", "/v1/info"} {
			w := controlRequest(handler, "GET", path, "", "")
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("public route %s: %d", path, w.Code)
			}
		}
		for _, route := range routes {
			for _, authorization := range []string{"", "Bearer wrong", "Bearer " + controlTestKey} {
				w := controlRequest(handler, route.method, route.path, route.body, authorization)
				if !enabled {
					controlCode(t, w, 503, "API_DISABLED")
				} else if authorization == "" {
					controlCode(t, w, 401, "AUTH_REQUIRED")
				} else if authorization == "Bearer wrong" {
					controlCode(t, w, 401, "AUTH_INVALID")
				} else if w.Code != 200 {
					t.Fatalf("authorized route %s failed: %s", route.path, w.Body.String())
				}
			}
		}
		if !enabled && reader.calls+reader.inspects+controller.calls != 0 {
			t.Fatal("disabled API reached backend")
		}
	}
}

func TestControlBearerIsolationAndMalformedCredentials(t *testing.T) {
	_, api, reader, controller := controlFixture(t, true)
	handler := api.Handler()
	for _, values := range [][]string{{""}, {"Basic " + controlTestKey}, {"Bearer"}, {"Bearer "}, {"Bearer  " + controlTestKey}, {"Bearer " + controlTestKey + ", Bearer wrong"}, {"Bearer " + controlTestKey, "Bearer " + controlTestKey}} {
		r := httptest.NewRequest("GET", "http://yard.test/v1/projects?api_key="+controlTestKey, nil)
		r.Header["Authorization"] = values
		r.AddCookie(&http.Cookie{Name: "nox_session", Value: controlTestKey})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		controlCode(t, w, 401, "AUTH_INVALID")
		if w.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatal("missing auth challenge")
		}
	}
	for _, suffix := range []string{"", "?key=" + controlTestKey, "?api_key=" + controlTestKey} {
		controlCode(t, controlRequest(handler, "GET", "/v1/projects"+suffix, "", ""), 401, "AUTH_REQUIRED")
	}
	if reader.calls+reader.inspects+controller.calls != 0 {
		t.Fatal("invalid auth reached backend")
	}
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	r := httptest.NewRequest("GET", "http://yard.test/v1/projects", nil)
	r.AddCookie(setup.Result().Cookies()[0])
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	controlCode(t, w, 401, "AUTH_REQUIRED")
	controlCode(t, controlRequest(handler, "GET", "/v1/projects", "", "Bearer "+testPassword), 401, "AUTH_INVALID")
	w = controlRequest(handler, "GET", "/api/projects", "", "Bearer "+controlTestKey)
	if w.Code != 401 || !strings.Contains(w.Body.String(), `"error"`) || strings.Contains(w.Body.String(), `"code"`) {
		t.Fatal("Bearer changed browser authentication")
	}
	r = httptest.NewRequest("POST", "http://yard.test/v1/projects/compose:app/actions", strings.NewReader(`{"action":"start"}`))
	r.Header.Set("Authorization", "Bearer "+controlTestKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://unrelated.test")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("Bearer required browser Origin/CSRF: %s", w.Body.String())
	}
}

func TestControlHealthInfoVersionAndStorage(t *testing.T) {
	data, api, reader, _ := controlFixture(t, false)
	handler := api.Handler()
	var health controlHealth
	var info controlInfo
	_ = json.Unmarshal(controlRequest(handler, "GET", "/v1/health", "", "").Body.Bytes(), &health)
	_ = json.Unmarshal(controlRequest(handler, "GET", "/v1/info", "", "").Body.Bytes(), &info)
	if health.Version != "v1.2.3" || health.Version != info.Version || !health.Ready || !health.Storage.Available || info.APIEnabled || info.Service != "nox-yard" || info.APIVersion != "v1" || len(info.Capabilities) != 7 {
		t.Fatalf("bad metadata: %+v %+v", health, info)
	}
	w := controlRequest(handler, "GET", "/healthz", "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"status":"ok"}` || reader.calls != 0 {
		t.Fatal("healthz behavior changed or health called Docker")
	}
	_ = data.Close()
	w = controlRequest(handler, "GET", "/v1/health", "", "")
	controlCode(t, w, 503, "STORAGE_UNAVAILABLE")
	_ = json.Unmarshal(w.Body.Bytes(), &health)
	if health.Ready || health.Storage.Available || health.Version != info.Version {
		t.Fatal("unavailable health lost metadata")
	}
	w = controlRequest(handler, "GET", "/healthz", "", "")
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"error":"Storage is unavailable."`) {
		t.Fatal("healthz storage failure changed")
	}
}

func TestControlStatusAndProjectInventory(t *testing.T) {
	data, api, reader, _ := controlFixture(t, true)
	for _, name := range []string{"app", "empty"} {
		if err := data.SaveManagedProject(store.ManagedProject{Name: name, SourceKind: "paste", YAML: "must-never-leak", VariablesJSON: `{"SECRET":"must-never-leak"}`}); err != nil {
			t.Fatal(err)
		}
	}
	reader.snapshot.Projects = append(reader.snapshot.Projects, inventory.Project{ID: "container:" + strings.Repeat("b", 64), Name: "single", Kind: "standalone", State: "stopped", Health: "new-health", Containers: []inventory.Container{{ID: strings.Repeat("b", 64), State: "new-state"}}})
	handler := api.Handler()
	w := controlRequest(handler, "GET", "/v1/status", "", "Bearer "+controlTestKey)
	var status controlStatus
	_ = json.Unmarshal(w.Body.Bytes(), &status)
	if w.Code != 200 || !status.Docker.Available || status.Inventory == nil || status.Inventory.Projects != 3 || status.Inventory.Containers != 2 || status.Inventory.RunningContainers != 1 || reader.calls != 1 || reader.inspects != 0 || time.Until(reader.deadline) > 5*time.Second {
		t.Fatalf("bad lightweight status: %s", w.Body.String())
	}
	w = controlRequest(handler, "GET", "/v1/projects", "", "Bearer "+controlTestKey)
	var projects controlProjects
	_ = json.Unmarshal(w.Body.Bytes(), &projects)
	if w.Code != 200 || len(projects.Projects) != 3 || projects.Projects[0].Kind != "managed-compose" || projects.Projects[1].Health != "unknown" || projects.Projects[1].Containers[0].State != "unknown" || projects.Projects[2].Containers == nil {
		t.Fatalf("bad projects: %s", w.Body.String())
	}
	for _, excluded := range []string{"must-never-leak", "cpuPercent", "terminalAvailable", "variables", "projectDir"} {
		if strings.Contains(w.Body.String(), excluded) {
			t.Fatalf("inventory leaked %s", excluded)
		}
	}
	for _, failure := range []struct {
		err  error
		code string
	}{{errdefs.ErrUnavailable, "DOCKER_UNAVAILABLE"}, {context.DeadlineExceeded, "OPERATION_TIMEOUT"}, {errdefs.ErrInternal, "OPERATION_FAILED"}} {
		reader.err = failure.err
		w = controlRequest(handler, "GET", "/v1/status", "", "Bearer "+controlTestKey)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"inventory":null`) || !strings.Contains(w.Body.String(), `"available":false`) || !strings.Contains(w.Body.String(), failure.code) {
			t.Fatalf("unavailable Docker: %s", w.Body.String())
		}
	}
	reader.err = errors.New("must-never-leak")
	controlCode(t, controlRequest(handler, "GET", "/v1/status", "", "Bearer "+controlTestKey), 500, "INTERNAL_ERROR")
	reader.err = nil
	_ = data.Close()
	controlCode(t, controlRequest(handler, "GET", "/v1/status", "", "Bearer "+controlTestKey), 500, "INTERNAL_ERROR")
}

func TestControlInspectionAndEmptyCollections(t *testing.T) {
	_, api, reader, _ := controlFixture(t, true)
	handler := api.Handler()
	id := strings.Repeat("a", 64)
	w := controlRequest(handler, "GET", "/v1/containers/"+id, "", "Bearer "+controlTestKey)
	if w.Code != 200 || reader.reveal || reader.calls != 0 || reader.inspects != 1 || strings.Contains(w.Body.String(), "environment") || strings.Contains(w.Body.String(), "must-never-leak") {
		t.Fatalf("unsafe inspection: %s", w.Body.String())
	}
	var detail controlInspection
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	if len(detail.Ports) != 1 || len(detail.Mounts) != 1 || len(detail.Networks) != 1 {
		t.Fatal("missing inspection fields")
	}
	reader.inspectErr = inventory.ErrContainerNotFound
	controlCode(t, controlRequest(handler, "GET", "/v1/containers/"+id, "", "Bearer "+controlTestKey), 404, "TARGET_NOT_FOUND")
	reader.snapshot.Projects = nil
	w = controlRequest(handler, "GET", "/v1/projects", "", "Bearer "+controlTestKey)
	if !strings.Contains(w.Body.String(), `"projects":[]`) {
		t.Fatal("empty inventory must be an array")
	}
}

func TestControlActionsAndPullUseSharedControllers(t *testing.T) {
	_, api, _, controller := controlFixture(t, true)
	handler := api.Handler()
	id := strings.Repeat("a", 64)
	for _, target := range []struct {
		path, id  string
		container bool
	}{{"containers/" + id, id, true}, {"projects/compose:app", "compose:app", false}, {"projects/container:" + id, "container:" + id, false}} {
		for _, action := range []string{"start", "stop", "restart"} {
			w := controlRequest(handler, "POST", "/v1/"+target.path+"/actions", `{"action":"`+action+`"}`, "Bearer "+controlTestKey)
			if w.Code != 200 || controller.id != target.id || controller.container != target.container || string(controller.action) != action || !strings.Contains(w.Body.String(), `"failures":[]`) {
				t.Fatalf("wrong operation dispatch: %s", w.Body.String())
			}
		}
		w := controlRequest(handler, "POST", "/v1/"+target.path+"/pull", "", "Bearer "+controlTestKey)
		if w.Code != 200 || controller.id != target.id || controller.container != target.container || !strings.Contains(w.Body.String(), `"images":["app:latest"]`) {
			t.Fatal("wrong pull dispatch")
		}
	}
	controller.result = lifecycle.Result{Queued: 1}
	w := controlRequest(handler, "POST", "/v1/containers/"+id+"/actions", `{"action":"restart"}`, "Bearer "+controlTestKey)
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"queued":1`) {
		t.Fatal("queued restart reported completed")
	}
	controller.result = lifecycle.Result{Skipped: 1}
	w = controlRequest(handler, "POST", "/v1/containers/"+id+"/actions", `{"action":"start"}`, "Bearer "+controlTestKey)
	if w.Code != 200 {
		t.Fatal("skip is not success")
	}
}

func TestControlStableErrorsAndPartialResults(t *testing.T) {
	_, api, _, controller := controlFixture(t, true)
	handler := api.Handler()
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{lifecycle.ErrNotFound, 404, "TARGET_NOT_FOUND"}, {lifecycle.ErrProtected, 409, "TARGET_PROTECTED"}, {lifecycle.ErrChanged, 409, "OPERATION_CONFLICT"}, {lifecycle.ErrConflict, 409, "OPERATION_CONFLICT"}, {errdefs.ErrConflict, 409, "OPERATION_CONFLICT"}, {errdefs.ErrUnavailable, 503, "DOCKER_UNAVAILABLE"}, {context.DeadlineExceeded, 504, "OPERATION_TIMEOUT"}, {errors.New("must-never-leak"), 500, "INTERNAL_ERROR"},
	} {
		controller.err = test.err
		for _, operation := range []string{"actions", "pull"} {
			body := ""
			if operation == "actions" {
				body = `{"action":"stop"}`
			}
			controlCode(t, controlRequest(handler, "POST", "/v1/projects/compose:app/"+operation, body, "Bearer "+controlTestKey), test.status, test.code)
		}
	}
	controller.err = nil
	for _, test := range []struct {
		succeeded int
		cause     error
		status    int
		code      string
	}{
		{1, errors.New("must-never-leak"), 502, "OPERATION_PARTIAL_FAILURE"}, {0, errors.New("must-never-leak"), 502, "OPERATION_FAILED"}, {0, errdefs.ErrConflict, 409, "OPERATION_CONFLICT"}, {0, errdefs.ErrUnavailable, 503, "DOCKER_UNAVAILABLE"}, {1, context.DeadlineExceeded, 504, "OPERATION_TIMEOUT"}, {1, &net.DNSError{IsTimeout: true}, 504, "OPERATION_TIMEOUT"},
	} {
		failures := []lifecycle.Failure{{Target: "app:latest", Cause: test.cause}}
		controller.result = lifecycle.Result{Succeeded: test.succeeded, Failed: 1, Failures: failures, Errors: []string{"must-never-leak"}}
		controller.pull = lifecycle.MaintenanceResult{Succeeded: test.succeeded, Failed: 1, Failures: failures, Errors: []string{"must-never-leak"}}
		for _, operation := range []string{"actions", "pull"} {
			body := ""
			if operation == "actions" {
				body = `{"action":"restart"}`
			}
			w := controlRequest(handler, "POST", "/v1/projects/compose:app/"+operation, body, "Bearer "+controlTestKey)
			controlCode(t, w, test.status, test.code)
			if !strings.Contains(w.Body.String(), `"result"`) || !strings.Contains(w.Body.String(), `"target":"app:latest"`) {
				t.Fatal("lost partial result")
			}
		}
	}
}

func TestControlPayloadIDsMethodsAndUnknownRoutes(t *testing.T) {
	_, api, reader, controller := controlFixture(t, true)
	handler := api.Handler()
	auth := "Bearer " + controlTestKey
	for _, body := range []string{"", "null", "[]", `{}`, `{"action":"remove"}`, `{"action":"start","extra":1}`, `{"action":"stop"} {}`, `{"action":1}`, `{`, `{"action":"stop","action":"start"}`, `{"Action":"start"}`} {
		w := controlRequest(handler, "POST", "/v1/projects/compose:app/actions", body, auth)
		status, code := 400, "INVALID_PAYLOAD"
		if body == "" {
			status, code = 415, "UNSUPPORTED_MEDIA_TYPE"
		}
		controlCode(t, w, status, code)
	}
	controlCode(t, controlRequest(handler, "POST", "/v1/projects/compose:app/actions", `{"action":"start","extra":"`+strings.Repeat("x", 4096)+`"}`, auth), 413, "PAYLOAD_TOO_LARGE")
	controlCode(t, controlRequest(handler, "POST", "/v1/projects/compose:app/pull", `{}`, auth), 400, "INVALID_PAYLOAD")
	for _, id := range []string{"compose:", "compose:a/b", "compose:a\\b", "compose:a\n", "container:abc", "bad:app"} {
		controlCode(t, controlRequest(handler, "POST", "/v1/projects/"+url.PathEscape(id)+"/pull", "", auth), 400, "INVALID_TARGET_ID")
	}
	for _, id := range []string{"abc", strings.Repeat("A", 64), "app"} {
		controlCode(t, controlRequest(handler, "GET", "/v1/containers/"+id, "", auth), 400, "INVALID_TARGET_ID")
	}
	for _, method := range []string{"POST", "HEAD", "OPTIONS"} {
		w := controlRequest(handler, method, "/v1/projects", "", auth)
		controlCode(t, w, 405, "METHOD_NOT_ALLOWED")
		if w.Header().Get("Allow") != "GET" {
			t.Fatal("missing Allow")
		}
	}
	for _, path := range []string{"/v1", "/v1/", "/v1/unknown", "/v1/containers", "/v1/projects/events", "/v1//projects", "/v1/../api/projects"} {
		controlCode(t, controlRequest(handler, "GET", path, "", ""), 404, "ROUTE_NOT_FOUND")
	}
	if reader.calls+reader.inspects+controller.calls != 0 {
		t.Fatal("invalid request reached backend")
	}
	long := "compose:" + strings.Repeat("a", 100)
	w := controlRequest(handler, "POST", "/v1/projects/"+long+"/pull", "", auth)
	if w.Code != 200 || controller.id != long {
		t.Fatal("applied managed-create length limit to external ID")
	}
}

func TestControlConfiguration(t *testing.T) {
	_, api, _, _ := controlFixture(t, false)
	for _, key := range []string{"", " ", "abc def", "abc\tdef", "abc\ndef"} {
		if err := api.ConfigureControlAPI(ControlConfig{Enabled: true, Key: key}); err == nil || (strings.TrimSpace(key) != "" && strings.Contains(err.Error(), key)) {
			t.Fatal("invalid key accepted or leaked")
		}
	}
	if err := api.ConfigureControlAPI(ControlConfig{Key: "ignored key"}); err != nil || api.control.enabled {
		t.Fatal("key activated disabled API")
	}
	if api.controlVersion() != "dev" {
		t.Fatal("missing version fallback")
	}
}

func TestControlActionDeadlineAndNotifications(t *testing.T) {
	_, api, _, _ := controlFixture(t, true)
	controller := &pendingController{entered: make(chan struct{}), release: make(chan struct{})}
	api.SetLifecycle(controller)
	changes, unsubscribe := api.changes.Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("POST", "http://yard.test/v1/projects/compose:app/actions", strings.NewReader(`{"action":"restart"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+controlTestKey)
	w := httptest.NewRecorder()
	handler := api.Handler()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(w, r); close(done) }()
	<-controller.entered
	if change := <-changes; !change.Inventory {
		t.Fatal("machine operation did not publish pending state")
	}
	snapshot := inventory.Snapshot{Projects: []inventory.Project{{ID: "compose:app"}}}
	api.changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "restarting" {
		t.Fatal("browser inventory cannot see machine operation")
	}
	<-done
	controlCode(t, w, 504, "OPERATION_TIMEOUT")
	if change := <-changes; !change.Inventory {
		t.Fatal("timed-out operation did not publish completion")
	}
	api.changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "" {
		t.Fatal("timed-out operation retained pending state")
	}
	// Even a controller returning a nil error must not turn an expired request
	// into a success response with apparently successful counters.
	writer := httptest.NewRecorder()
	writeControlResult(writer, ctx, nil, controlPullResult{Succeeded: 1}, 0, 1, 0, 0, nil)
	controlCode(t, writer, 504, "OPERATION_TIMEOUT")
}
