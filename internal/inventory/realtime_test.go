package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

func dockerTestReader(t *testing.T, handler http.HandlerFunc) *DockerReader {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cli, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	reader := &DockerReader{client: cli}
	t.Cleanup(func() { _ = reader.Close() })
	return reader
}

func TestSnapshotUsesOnlyListEvenWhileStatsAreBlocked(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var forbidden atomic.Int32
	reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_ = json.NewEncoder(w).Encode([]container.Summary{{ID: "a", Names: []string{"/app"}, State: "running", Status: "Up 2 hours (healthy)"}})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprint(w, `{"cpu_stats":{"cpu_usage":{"total_usage":200},"system_cpu_usage":2000,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},"memory_stats":{"usage":1000,"stats":{"inactive_file":100}},"networks":{"eth0":{"rx_bytes":10,"tx_bytes":20}}}`)
		default:
			forbidden.Add(1)
			http.Error(w, "unexpected Docker call", 500)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan struct{})
	go func() { reader.collectMetrics(ctx); close(finished) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("stats never started")
	}
	// The inventory must complete before the blocked metrics request is released.
	quick, stop := context.WithTimeout(ctx, 500*time.Millisecond)
	snapshot, err := reader.Snapshot(quick)
	stop()
	close(release)
	<-finished
	if err != nil {
		t.Fatalf("inventory blocked on stats: %v", err)
	}
	if len(snapshot.Projects) != 1 || snapshot.Projects[0].Health != "healthy" || snapshot.Projects[0].CPUPercent != nil {
		t.Fatalf("bad lightweight snapshot: %+v", snapshot)
	}
	cached := reader.CachedMetrics()["a"]
	if cached.MemoryBytes == nil || *cached.MemoryBytes != 900 || cached.CPUPercent == nil || *cached.CPUPercent != 20 {
		t.Fatalf("background metrics missing: %+v", cached)
	}
	snapshot, err = reader.Snapshot(ctx)
	if err != nil || snapshot.Projects[0].CPUPercent == nil {
		t.Fatalf("cached metrics not joined: %v %+v", err, snapshot)
	}
	if forbidden.Load() != 0 {
		t.Fatal("inventory invoked inspect or stat path")
	}
}

func TestMetricsExpireAndLifecycleInvalidatesSamples(t *testing.T) {
	value := uint64(1)
	reader := &DockerReader{metrics: map[string]Metrics{"a": {MemoryBytes: &value, CollectedAt: time.Now().Add(-time.Minute)}}}
	if reader.CachedMetrics()["a"].MemoryBytes != nil {
		t.Fatal("expired value presented as current")
	}
	now := time.Now().UnixNano()
	for _, action := range []string{"create", "start", "restart", "pause", "unpause", "health_status: unhealthy", "stop", "die", "destroy", "rename", "update", "kill", "oom"} {
		if !reader.handleEvent(events.Message{Type: events.ContainerEventType, Action: events.Action(action), Actor: events.Actor{ID: "a"}, TimeNano: now}) {
			t.Fatalf("missed event %s", action)
		}
	}
	if _, ok := reader.CachedMetrics()["a"]; ok {
		t.Fatal("destroy retained sample")
	}
	if reader.handleEvent(events.Message{Type: events.ContainerEventType, Action: events.Action("exec_start")}) {
		t.Fatal("terminal noise caused refresh")
	}
}

func TestLifecycleDiscardsInflightMetricsAndInspection(t *testing.T) {
	for _, kind := range []string{"stats", "inspect", "uptime"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/containers/json") {
					_, _ = fmt.Fprint(w, `[{"Id":"a","State":"running"}]`)
					return
				}
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if kind == "stats" {
					_, _ = fmt.Fprint(w, `{"memory_stats":{"usage":1000}}`)
				} else {
					_, _ = fmt.Fprint(w, `{"Id":"a","State":{"Running":true,"StartedAt":"2026-01-01T00:00:00Z"}}`)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				if kind == "stats" {
					reader.collectMetrics(ctx)
				} else if kind == "inspect" {
					_, _ = reader.InspectContainer(ctx, "a", false)
				} else {
					reader.collectUptimes(ctx, uptimeBatch{ids: []string{"a"}}, func(Change) {})
				}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("Docker call never started")
			}
			reader.handleEvent(events.Message{Type: events.ContainerEventType, Action: events.ActionDestroy, Actor: events.Actor{ID: "a"}})
			close(release)
			<-done
			if _, exists := reader.CachedMetrics()["a"]; exists {
				t.Fatal("response predating destroy restored stale data")
			}
		})
	}
}

func TestColdUptimeBackfillDoesNotWaitForStatsAndIsCached(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var inspections atomic.Int32
	started := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_, _ = fmt.Fprint(w, `[{"Id":"a","State":"running"}]`)
		case strings.HasSuffix(r.URL.Path, "/a/json"):
			inspections.Add(1)
			_, _ = fmt.Fprintf(w, `{"Id":"a","State":{"Running":true,"StartedAt":%q}}`, started)
		case strings.HasSuffix(r.URL.Path, "/stats"):
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = fmt.Fprint(w, `{"memory_stats":{"usage":1000}}`)
		default:
			t.Errorf("unexpected Docker request %s", r.URL.Path)
		}
	})
	reader.uptimeRequests = make(chan uptimeBatch, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { reader.collectMetrics(ctx); close(done) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("stats did not start")
	}
	var batch uptimeBatch
	select {
	case batch = <-reader.uptimeRequests:
	case <-ctx.Done():
		t.Fatal("missing cold-start batch")
	}
	notifications := 0
	reader.collectUptimes(ctx, batch, func(change Change) {
		if !change.Metrics || change.Inventory {
			t.Error("uptime requested a full inventory reload")
		}
		notifications++
	})
	quick, stop := context.WithTimeout(ctx, 500*time.Millisecond)
	snapshot, err := reader.Snapshot(quick)
	stop()
	close(release)
	<-done
	if err != nil || len(snapshot.Projects) != 1 {
		t.Fatal("inventory waited for stats", snapshot, err)
	}
	project := snapshot.Projects[0]
	if project.UptimeSeconds == nil || *project.UptimeSeconds < 7200 || *project.UptimeSeconds > 7203 || project.CPUPercent != nil {
		t.Fatal("uptime was not available while stats were blocked", project)
	}
	reader.collectUptimes(ctx, batch, func(Change) { t.Error("known uptime was reloaded") })
	reader.collectMetrics(ctx)
	select {
	case <-reader.uptimeRequests:
		t.Fatal("known start time queued again")
	default:
	}
	if inspections.Load() != 1 || notifications != 1 {
		t.Fatal("uptime was not cached", inspections.Load(), notifications)
	}
}

func TestStatsFailureRetainsRecentSampleAndPrunesStoppedContainers(t *testing.T) {
	reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			_, _ = fmt.Fprint(w, `[{"Id":"a","State":"running"},{"Id":"b","State":"exited"}]`)
			return
		}
		http.Error(w, "stats unavailable", 500)
	})
	value := uint64(123)
	reader.metrics = map[string]Metrics{"a": {MemoryBytes: &value, CollectedAt: time.Now()}, "b": {MemoryBytes: &value, CollectedAt: time.Now()}}
	reader.collectMetrics(context.Background())
	cached := reader.CachedMetrics()
	if cached["a"].MemoryBytes == nil || *cached["a"].MemoryBytes != value {
		t.Fatal("transient stats error erased recent sample")
	}
	if _, exists := cached["b"]; exists {
		t.Fatal("stopped container sample retained")
	}
}

func TestShellCapabilityIsLazyAndCached(t *testing.T) {
	var calls atomic.Int32
	reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/archive") || r.Method != "HEAD" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		calls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	})
	for range 2 {
		available, err := reader.shellAvailable(context.Background(), "a")
		if err != nil || available {
			t.Fatalf("missing shell: %v %v", available, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("shell result was not cached")
	}
	reader.handleEvent(events.Message{Type: events.ContainerEventType, Action: events.Action("start"), Actor: events.Actor{ID: "a"}, TimeNano: time.Now().UnixNano()})
	_, _ = reader.shellAvailable(context.Background(), "a")
	if calls.Load() != 2 {
		t.Fatal("start did not invalidate shell cache")
	}
}

func TestNotifierCoalescesAndTracksOperations(t *testing.T) {
	n := NewNotifier()
	updates, unsubscribe := n.Subscribe()
	defer unsubscribe()
	done := n.Begin("compose:app", "restart")
	n.Notify(Change{Metrics: true})
	change := <-updates
	if !change.Inventory || !change.Metrics {
		t.Fatal("coalescing dropped an update")
	}
	snapshot := Snapshot{Projects: []Project{{ID: "compose:app", Containers: []Container{{ID: "a"}}}}}
	n.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "restarting" || snapshot.Projects[0].Containers[0].Operation != "restarting" {
		t.Fatal("operation not reflected")
	}
	done()
	n.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "" || snapshot.Projects[0].Containers[0].Operation != "" {
		t.Fatal("completed operation retained")
	}
}

func TestEventsReconnectAndStopWithContext(t *testing.T) {
	var connections atomic.Int32
	reconnect := make(chan struct{}, 1)
	reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/events") {
			http.Error(w, "unexpected call", 500)
			return
		}
		if r.URL.Query().Get("since") == "" {
			t.Error("reconnect cursor missing")
		}
		w.Header().Set("Content-Type", "application/json")
		if connections.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(events.Message{Type: events.ContainerEventType, Action: events.Action("create"), Actor: events.Actor{ID: "a"}, TimeNano: time.Now().UnixNano()})
			return
		}
		w.(http.Flusher).Flush()
		reconnect <- struct{}{}
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	var notifications atomic.Int32
	go func() {
		reader.watchEvents(ctx, func(change Change) {
			if change.Inventory {
				notifications.Add(1)
			}
		})
		close(done)
	}()
	select {
	case <-reconnect:
	case <-time.After(4 * time.Second):
		t.Fatal("event stream did not reconnect")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event stream did not stop")
	}
	if notifications.Load() < 3 {
		t.Fatal("missing reconciliation notifications")
	}
}
