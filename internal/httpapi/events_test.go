package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

type liveInventoryStub struct{ inventoryStub }

func (*liveInventoryStub) CachedMetrics() map[string]inventory.Metrics {
	return map[string]inventory.Metrics{}
}

func TestInventoryStreamAndMetricsRequireSession(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	api.SetInventory(&liveInventoryStub{})
	handler := api.Handler()
	for _, path := range []string{"/api/projects/events", "/api/metrics"} {
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, httptest.NewRequest("GET", "http://yard.test"+path, nil))
		if result.Code != http.StatusUnauthorized {
			t.Fatalf("%s accepted anonymous request: %d", path, result.Code)
		}
	}
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	cookie := setup.Result().Cookies()[0]
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/projects/events", nil)
	request.AddCookie(cookie)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Header.Get("X-Accel-Buffering") != "no" || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal("missing stream headers")
	}
	reader := bufio.NewReader(response.Body)
	readEvent := func() string {
		t.Helper()
		var event strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			event.WriteString(line)
			if line == "\n" {
				return event.String()
			}
		}
	}
	if event := readEvent(); !strings.Contains(event, `"inventory":true`) || !strings.Contains(event, `"metrics":true`) {
		t.Fatalf("missing initial reconciliation: %s", event)
	}
	api.Notify(inventory.Change{Metrics: true})
	if event := readEvent(); !strings.Contains(event, `"inventory":false,"metrics":true`) {
		t.Fatalf("metrics forced inventory refresh: %s", event)
	}
	api.Notify(inventory.Change{Inventory: true})
	if event := readEvent(); !strings.Contains(event, `"inventory":true`) {
		t.Fatalf("missing live update: %s", event)
	}
	req := httptest.NewRequest("GET", "http://yard.test/api/metrics", nil)
	req.AddCookie(cookie)
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != 200 || strings.TrimSpace(result.Body.String()) != "{}" {
		t.Fatalf("cached metrics: %d %s", result.Code, result.Body.String())
	}
}

type pendingController struct {
	lifecycle.Controller
	entered chan struct{}
	release chan struct{}
}

func (c *pendingController) Project(ctx context.Context, _ string, action lifecycle.Action) (lifecycle.Result, error) {
	close(c.entered)
	select {
	case <-ctx.Done():
		return lifecycle.Result{}, ctx.Err()
	case <-c.release:
	}
	return lifecycle.Result{Action: action, Succeeded: 1}, nil
}

func TestActionPublishesPendingStateAndCompletion(t *testing.T) {
	data, api := newTestServer(t, t.TempDir(), "")
	defer data.Close()
	api.SetInventory(&inventoryStub{})
	controller := &pendingController{entered: make(chan struct{}), release: make(chan struct{})}
	api.SetLifecycle(controller)
	handler := api.Handler()
	setup := postCredentials(handler, "/api/setup", "http://yard.test", "owner", testPassword)
	cookie := setup.Result().Cookies()[0]
	var session bootstrapResponse
	_ = json.Unmarshal(setup.Body.Bytes(), &session)
	changes, unsubscribe := api.changes.Subscribe()
	defer unsubscribe()
	request := httptest.NewRequest("POST", "http://yard.test/api/projects/compose:yard/actions", strings.NewReader(`{"action":"restart"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://yard.test")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { handler.ServeHTTP(response, request); close(done) }()
	<-controller.entered
	if change := <-changes; !change.Inventory {
		t.Fatal("pending action not published")
	}
	snapshot := inventory.Snapshot{Projects: []inventory.Project{{ID: "compose:yard"}}}
	api.changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "restarting" {
		t.Fatal("pending state missing")
	}
	close(controller.release)
	<-done
	if response.Code != 200 {
		t.Fatalf("action failed: %s", response.Body.String())
	}
	if change := <-changes; !change.Inventory {
		t.Fatal("completed action not published")
	}
	api.changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "" {
		t.Fatal("completed operation retained")
	}
}
