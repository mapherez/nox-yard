package mcpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/httpapi"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/mcpapi"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type reader struct {
	reveal bool
	calls  int
}

func (r *reader) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	r.calls++
	return inventory.Snapshot{CollectedAt: time.Now(), Projects: []inventory.Project{{ID: "container:" + id, Kind: "standalone", Name: "fixture", State: "running", Health: "none", Containers: []inventory.Container{{ID: id, State: "running", Health: "none"}}}}}, ctx.Err()
}
func (r *reader) InspectContainer(ctx context.Context, target string, reveal bool) (inventory.ContainerInspection, error) {
	r.reveal = reveal
	env := inventory.EnvironmentVariable{Name: "SECRET"}
	if reveal {
		v := "explicit-value"
		env.Value = &v
	}
	return inventory.ContainerInspection{ID: target, Ports: []inventory.PortInfo{}, Mounts: []inventory.MountInfo{}, Networks: []inventory.NetworkInfo{}, Environment: []inventory.EnvironmentVariable{env}}, ctx.Err()
}
func (r *reader) CachedMetrics() map[string]inventory.Metrics { return map[string]inventory.Metrics{} }
func (r *reader) OpenLogs(ctx context.Context, id string) (inventory.LogStream, error) {
	return r.OpenLogsWithOptions(ctx, id, inventory.LogOptions{Follow: true, Tail: 200})
}
func (r *reader) OpenLogsWithOptions(ctx context.Context, id string, options inventory.LogOptions) (inventory.LogStream, error) {
	if (!options.Follow && options.Tail != 20) || (options.Follow && options.Tail != 200) {
		return inventory.LogStream{}, errors.New("wrong snapshot options")
	}
	var text strings.Builder
	for i := 0; i < 25; i++ {
		text.WriteString("line\n")
	}
	return inventory.LogStream{TTY: true, Reader: io.NopCloser(strings.NewReader(text.String()))}, nil
}

type controller struct {
	lifecycle.Controller
	entered chan struct{}
	release chan struct{}
	result  lifecycle.Result
	err     error
	calls   int
}

func (c *controller) Container(ctx context.Context, id string, action lifecycle.Action) (lifecycle.Result, error) {
	c.calls++
	if c.entered != nil {
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return lifecycle.Result{}, ctx.Err()
		}
	}
	out := c.result
	out.Action = action
	return out, c.err
}
func (c *controller) Project(ctx context.Context, id string, action lifecycle.Action) (lifecycle.Result, error) {
	return c.Container(ctx, id, action)
}
func (c *controller) PullContainer(ctx context.Context, id string) (lifecycle.MaintenanceResult, error) {
	c.calls++
	return lifecycle.MaintenanceResult{Succeeded: 1, Images: []string{"fixture:latest"}}, c.err
}
func (c *controller) PullProject(ctx context.Context, id string) (lifecycle.MaintenanceResult, error) {
	return c.PullContainer(ctx, id)
}
func (c *controller) PreviewRemoveContainer(ctx context.Context, id string) (lifecycle.RemovalPlan, error) {
	c.calls++
	return lifecycle.RemovalPlan{Fingerprint: strings.Repeat("a", 64), Items: []lifecycle.RemovalItem{}}, c.err
}
func (c *controller) PreviewRemoveProject(ctx context.Context, id string) (lifecycle.RemovalPlan, error) {
	return c.PreviewRemoveContainer(ctx, id)
}
func (c *controller) RemoveContainer(ctx context.Context, id, fingerprint string) (lifecycle.RemovalReport, error) {
	c.calls++
	return lifecycle.RemovalReport{Items: []lifecycle.RemovalOutcome{}}, c.err
}
func (c *controller) RemoveProject(ctx context.Context, id, fingerprint string) (lifecycle.RemovalReport, error) {
	return c.RemoveContainer(ctx, id, fingerprint)
}

type updater struct {
	automatic bool
	checks    int
}

func (u *updater) Status() (selfupdate.Status, error) {
	return selfupdate.Status{Automatic: u.automatic, IntervalMinutes: 60, Status: "checking"}, nil
}
func (u *updater) SetSettings(automatic bool, minutes int) error { u.automatic = automatic; return nil }
func (u *updater) CheckNow() error                               { u.checks++; return nil }
func fixture(t *testing.T) (*application.Service, *httpapi.Server, *noxmcp.Runtime) {
	t.Helper()
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	app := application.New(data, inventory.NewNotifier())
	api, err := httpapi.New(data, t.TempDir(), "", app)
	if err != nil {
		t.Fatal(err)
	}
	r := &reader{}
	api.SetInventory(r)
	api.SetLogs(r)
	runtime, err := mcpapi.New(app, "fixture-version")
	if err != nil {
		t.Fatal(err)
	}
	api.SetMCPHandler(runtime.HTTPHandler())
	return app, api, runtime
}
func connect(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "yard-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}
func call(t *testing.T, session *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func text(result *mcp.CallToolResult) string {
	b, _ := json.Marshal(result.StructuredContent)
	return string(b)
}
func TestHTTPCatalogSchemasAndAnnotations(t *testing.T) {
	app, api, _ := fixture(t)
	api.SetLifecycle(&controller{result: lifecycle.Result{Succeeded: 1}})
	app.Updates = &updater{}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 25 {
		t.Fatalf("tools=%d", len(list.Tools))
	}
	expectedCLIPaths := map[string]string{
		"yard_health":                       "health",
		"yard_status":                       "status",
		"yard_projects":                     "project list",
		"yard_metrics":                      "metrics",
		"yard_container_inspect":            "container inspect",
		"yard_container_environment":        "container env",
		"yard_container_logs":               "container logs",
		"yard_container_action":             "container action",
		"yard_project_action":               "project action",
		"yard_container_pull":               "container pull",
		"yard_project_pull":                 "project pull",
		"yard_container_remove_preview":     "container remove preview",
		"yard_project_remove_preview":       "project remove preview",
		"yard_container_remove":             "container remove",
		"yard_project_remove":               "project remove",
		"yard_compose_source":               "compose source",
		"yard_compose_preview":              "compose preview",
		"yard_compose_submit":               "compose submit",
		"yard_compose_operation":            "compose operation",
		"yard_compose_job":                  "compose job",
		"yard_projects_settings_get":        "settings projects get",
		"yard_projects_settings_set":        "settings projects set",
		"yard_self_update_status":           "update status",
		"yard_self_update_settings":         "update settings",
		"yard_self_update_check_and_update": "update now",
	}
	seenCLIPaths := map[string]string{}
	writes := map[string]bool{}
	for _, kind := range []string{"container", "project"} {
		for _, op := range []string{"action", "pull", "remove"} {
			writes["yard_"+kind+"_"+op] = true
		}
	}
	for _, name := range []string{"yard_compose_submit", "yard_compose_operation", "yard_projects_settings_set", "yard_self_update_settings", "yard_self_update_check_and_update"} {
		writes[name] = true
	}
	for _, tool := range list.Tools {
		if tool.Meta == nil {
			t.Fatalf("missing _meta for %s", tool.Name)
		}
		cli, ok := tool.Meta["cli"].(string)
		if !ok || strings.TrimSpace(cli) == "" {
			t.Fatalf("missing or invalid _meta.cli for %s: %#v", tool.Name, tool.Meta["cli"])
		}
		expected, ok := expectedCLIPaths[tool.Name]
		if !ok || cli != expected {
			t.Fatalf("wrong _meta.cli for %s: got %q, want %q", tool.Name, cli, expected)
		}
		if strings.HasPrefix(cli, "yard ") {
			t.Fatalf("_meta.cli includes app prefix for %s: %q", tool.Name, cli)
		}
		if previous, ok := seenCLIPaths[cli]; ok {
			t.Fatalf("duplicate _meta.cli %q for %s and %s", cli, previous, tool.Name)
		}
		seenCLIPaths[cli] = tool.Name
		a := tool.Annotations
		if a == nil || a.DestructiveHint == nil || tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Fatalf("missing metadata for %s", tool.Name)
		}
		mutation := writes[tool.Name]
		if a.ReadOnlyHint == mutation || *a.DestructiveHint != mutation || a.IdempotentHint != (!mutation || tool.Name == "yard_projects_settings_set") {
			t.Fatalf("wrong annotations %s: %+v", tool.Name, a)
		}
		if tool.Name == "yard_self_update_check_and_update" && (!strings.Contains(tool.Description, "even when automatic updates are disabled") || tool.Title != "Check and update NoX Yard") {
			t.Fatal("misleading self-update tool")
		}
	}
	for _, name := range []string{"yard_health", "yard_status", "yard_projects", "yard_metrics", "yard_self_update_status"} {
		result := call(t, session, name, map[string]any{})
		if result.IsError {
			t.Fatalf("%s: %s", name, text(result))
		}
	}
	result := call(t, session, "yard_container_inspect", map[string]any{"id": id})
	if result.IsError || strings.Contains(text(result), "explicit-value") {
		t.Fatalf("normal inspection: %s", text(result))
	}
	result = call(t, session, "yard_container_environment", map[string]any{"id": id})
	if result.IsError || !strings.Contains(text(result), "explicit-value") {
		t.Fatalf("explicit environment: %s", text(result))
	}
	result = call(t, session, "yard_container_logs", map[string]any{"id": id})
	if result.IsError {
		t.Fatal(text(result))
	}
	var logs struct{ Lines []inventory.LogLine }
	b, _ := json.Marshal(result.StructuredContent)
	json.Unmarshal(b, &logs)
	if len(logs.Lines) != 20 {
		t.Fatalf("logs=%d", len(logs.Lines))
	}
	result = call(t, session, "yard_container_action", map[string]any{"id": id, "action": "restart"})
	if result.IsError || !strings.Contains(text(result), `"succeeded":1`) {
		t.Fatal(text(result))
	}
	result = call(t, session, "yard_container_action", map[string]any{"id": id, "action": "start", "unexpected": true})
	if !result.IsError {
		t.Fatal("unknown field accepted")
	}
	result = call(t, session, "yard_container_inspect", map[string]any{"id": "short"})
	if !result.IsError || !strings.Contains(text(result), "INVALID_TARGET_ID") {
		t.Fatal(text(result))
	}
	for _, path := range []string{"/mcp/unknown", "/mcp/", "/v1/unknown"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 404 || strings.Contains(string(body), "<!") {
			t.Fatalf("SPA fallback: %s", path)
		}
	}
	for _, path := range []string{"/api/projects", "/v1/projects"} {
		response, _ := server.Client().Get(server.URL + path)
		response.Body.Close()
		expected := 401
		if path == "/v1/projects" {
			expected = 503
		}
		if response.StatusCode != expected {
			t.Fatalf("auth changed: %s %d", path, response.StatusCode)
		}
	}
	req, _ := http.NewRequest("POST", server.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://other.test")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("invalid origin accepted")
	}
}
func TestMCPPartialFailuresProtectionRemovalAndSelfUpdate(t *testing.T) {
	app, api, _ := fixture(t)
	c := &controller{result: lifecycle.Result{Succeeded: 1, Failed: 1, Failures: []lifecycle.Failure{{Target: id, Cause: lifecycle.ErrProtected}}}}
	api.SetLifecycle(c)
	u := &updater{}
	app.Updates = u
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	result := call(t, session, "yard_container_action", map[string]any{"id": id, "action": "restart"})
	if !result.IsError || !strings.Contains(text(result), "OPERATION_PARTIAL_FAILURE") || !strings.Contains(text(result), `"succeeded":1`) {
		t.Fatal(text(result))
	}
	c.result = lifecycle.Result{}
	c.err = lifecycle.ErrProtected
	result = call(t, session, "yard_container_action", map[string]any{"id": id, "action": "stop"})
	if !result.IsError || !strings.Contains(text(result), "TARGET_PROTECTED") {
		t.Fatal(text(result))
	}
	before := c.calls
	result = call(t, session, "yard_container_remove", map[string]any{"id": id, "confirm": false, "fingerprint": strings.Repeat("a", 64)})
	if !result.IsError || c.calls != before {
		t.Fatal("unconfirmed removal reached manager")
	}
	c.err = lifecycle.ErrChanged
	result = call(t, session, "yard_container_remove", map[string]any{"id": id, "confirm": true, "fingerprint": strings.Repeat("a", 64)})
	if !result.IsError || !strings.Contains(text(result), "OPERATION_CONFLICT") {
		t.Fatal(text(result))
	}
	result = call(t, session, "yard_self_update_check_and_update", map[string]any{})
	if result.IsError || u.checks != 1 || u.automatic {
		t.Fatalf("manual check changed behavior: %s", text(result))
	}
}
func TestMCPUsesHTTPNotifierAndController(t *testing.T) {
	app, api, _ := fixture(t)
	c := &controller{entered: make(chan struct{}), release: make(chan struct{}), result: lifecycle.Result{Succeeded: 1}}
	api.SetLifecycle(c)
	handler := api.Handler()
	setup := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "http://yard.test/api/setup", strings.NewReader(`{"username":"owner","password":"correct-horse-battery-staple"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://yard.test")
	handler.ServeHTTP(setup, request)
	if setup.Code != 201 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	server := httptest.NewServer(handler)
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/projects/events", nil)
	req.AddCookie(cookie)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	events := bufio.NewReader(response.Body)
	readEvent := func() string {
		var b strings.Builder
		for {
			line, err := events.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			b.WriteString(line)
			if line == "\n" {
				return b.String()
			}
		}
	}
	readEvent()
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "yard_container_action", Arguments: map[string]any{"id": id, "action": "restart"}})
		done <- err
	}()
	select {
	case <-c.entered:
	case <-ctx.Done():
		t.Fatal("action not dispatched")
	}
	if event := readEvent(); !strings.Contains(event, `"inventory":true`) {
		t.Fatal(event)
	}
	snapshot, err := app.Projects(ctx)
	if err != nil || snapshot.Projects[0].Operation != "restarting" {
		t.Fatalf("pending state: %+v %v", snapshot, err)
	}
	close(c.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	readEvent()
	snapshot, err = app.Projects(ctx)
	if err != nil || snapshot.Projects[0].Operation != "" || c.calls != 1 {
		t.Fatal("completion/controller not shared")
	}
	// The same manager is used by the Bearer adapter; no internal HTTP bridge.
	c.entered = nil
	if err := api.ConfigureControlAPI(httpapi.ControlConfig{Enabled: true, Key: "test-key"}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("POST", "http://yard.test/v1/containers/"+id+"/actions", strings.NewReader(`{"action":"restart"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != 200 || c.calls != 2 {
		t.Fatalf("REST path: %d %s", w.Code, w.Body.String())
	}
}

// Fake asynchronous work retains the existing manager-owned notification lifetime.
type asyncManaged struct {
	application.ManagedController
	changes *inventory.Notifier
	mu      sync.Mutex
	job     store.ManagedJob
	finish  func()
}

func (m *asyncManaged) Operation(name, op string, volumes bool) (store.ManagedJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.job = store.ManagedJob{ID: "job", ProjectName: name, Operation: op, Status: "running"}
	m.finish = m.changes.Begin("compose:"+name, op)
	return m.job, nil
}
func (m *asyncManaged) Job(id string) (store.ManagedJob, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.job, true, nil
}
func TestComposeAcceptanceAndNotifierLifetime(t *testing.T) {
	app, api, _ := fixture(t)
	m := &asyncManaged{changes: app.Changes}
	app.Managed = m
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	result := call(t, session, "yard_compose_operation", map[string]any{"name": "demo", "operation": "start", "removeVolumes": false})
	if result.IsError || !strings.Contains(text(result), `"status":"running"`) {
		t.Fatal(text(result))
	}
	snapshot := inventory.Snapshot{Projects: []inventory.Project{{ID: "compose:demo"}}}
	app.Changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "starting" {
		t.Fatal("job lost pending state after response")
	}
	result = call(t, session, "yard_compose_job", map[string]any{"id": "job"})
	if result.IsError {
		t.Fatal(text(result))
	}
	m.mu.Lock()
	m.job.Status = "succeeded"
	m.finish()
	m.mu.Unlock()
	app.Changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "" {
		t.Fatal("job pending state not released")
	}
	result = call(t, session, "yard_compose_job", map[string]any{"id": "job"})
	if result.IsError || !strings.Contains(text(result), `"status":"succeeded"`) {
		t.Fatal(text(result))
	}
}

type timeoutController struct {
	lifecycle.Controller
	seen     chan context.Context
	release  chan struct{}
	finished chan struct{}
	calls    int
}

func (c *timeoutController) Container(ctx context.Context, id string, action lifecycle.Action) (lifecycle.Result, error) {
	c.calls++
	c.seen <- ctx
	<-c.release
	close(c.finished)
	return lifecycle.Result{}, ctx.Err()
}
func TestRuntimeTimeoutContextAndUnknownOutcome(t *testing.T) {
	app, _, runtime := fixture(t)
	c := &timeoutController{seen: make(chan context.Context, 1), release: make(chan struct{}), finished: make(chan struct{})}
	app.Lifecycle = c
	args := map[string]any{"id": id, "action": "restart"}
	done := make(chan error, 1)
	go func() {
		_, err := runtime.Execute(context.Background(), "yard_container_action", args, nil)
		done <- err
	}()
	ctx := <-c.seen
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 60*time.Second || time.Until(deadline) < 59*time.Second {
		t.Fatalf("runtime deadline: %v", deadline)
	}
	close(c.release)
	<-c.finished
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// A shorter adapter deadline must also reach the manager unchanged in effect.
	c = &timeoutController{seen: make(chan context.Context, 1), release: make(chan struct{}), finished: make(chan struct{})}
	app.Lifecycle = c
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() { _, err := runtime.Execute(parent, "yard_container_action", args, nil); done <- err }()
	child := <-c.seen
	parentDeadline, _ := parent.Deadline()
	childDeadline, _ := child.Deadline()
	if !childDeadline.Equal(parentDeadline) {
		t.Fatal("parent deadline replaced")
	}
	err := <-done
	var public *noxmcp.Error
	if !errors.As(err, &public) || public.Code != "TIMEOUT" || public.Retryable || public.Details["outcome"] != "unknown" {
		t.Fatalf("timeout result: %+v", err)
	}
	close(c.release)
	<-c.finished
	if c.calls != 1 {
		t.Fatal("timed-out mutation replayed")
	}
}

func TestComposeSourceSettingsSchemasAndBrowserLogRegression(t *testing.T) {
	app, api, _ := fixture(t)
	app.Managed = &schemaManaged{}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	session := connect(t, server.URL+"/mcp")
	source := map[string]any{"kind": "paste", "yaml": "services:\n  demo:\n    image: alpine:3.23\n"}
	result := call(t, session, "yard_compose_source", source)
	if result.IsError {
		t.Fatal(text(result))
	}
	request := map[string]any{"name": "demo", "source": source, "variables": map[string]any{}, "envFiles": map[string]any{}, "mode": "new"}
	result = call(t, session, "yard_compose_preview", request)
	if result.IsError {
		t.Fatal(text(result))
	}
	request["fingerprint"] = strings.Repeat("a", 64)
	result = call(t, session, "yard_compose_submit", request)
	if result.IsError {
		t.Fatal(text(result))
	}
	for _, kind := range []string{"container", "project"} {
		target := id
		if kind == "project" {
			target = "container:" + id
		}
		api.SetLifecycle(&controller{})
		for _, op := range []string{"pull", "remove_preview"} {
			result := call(t, session, "yard_"+kind+"_"+op, map[string]any{"id": target})
			if result.IsError {
				t.Fatalf("%s: %s", op, text(result))
			}
		}
		result = call(t, session, "yard_"+kind+"_remove", map[string]any{"id": target, "confirm": true, "fingerprint": strings.Repeat("a", 64)})
		if result.IsError {
			t.Fatal(text(result))
		}
	}
	for _, op := range []struct {
		name string
		args any
	}{{"yard_projects_settings_get", map[string]any{}}, {"yard_projects_settings_set", map[string]any{"projectsBase": "/projects"}}} {
		if result := call(t, session, op.name, op.args); result.IsError {
			t.Fatal(text(result))
		}
	}
	req, _ := http.NewRequest("POST", server.URL+"/api/setup", strings.NewReader(`{"username":"owner","password":"correct-horse-battery-staple"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", server.URL)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cookie := response.Cookies()[0]
	response.Body.Close()
	req, _ = http.NewRequest("GET", server.URL+"/api/containers/"+id+"/logs", nil)
	req.AddCookie(cookie)
	response, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !strings.Contains(string(body), "event: end") || strings.Count(string(body), "event: log") != 25 {
		t.Fatalf("browser SSE regression: %d %s", response.StatusCode, body)
	}
}

type schemaManaged struct{ application.ManagedController }

func (*schemaManaged) Preview(ctx context.Context, in managed.Request) (managed.ProjectPreview, managed.Source, error) {
	return managed.ProjectPreview{Preview: managed.Preview{Fingerprint: strings.Repeat("a", 64)}, Duplicates: []string{}, Changes: []string{}}, managed.Source{}, nil
}
func (*schemaManaged) Submit(ctx context.Context, in managed.Request) (store.ManagedJob, error) {
	return store.ManagedJob{ID: "job", ProjectName: in.Name, Operation: in.Mode, Status: "running"}, nil
}
func (*schemaManaged) ProjectsBase() (string, error) { return "/projects", nil }
func (*schemaManaged) SetProjectsBase(ctx context.Context, base string) (string, error) {
	return base, ctx.Err()
}
