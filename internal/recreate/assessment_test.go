package recreate

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func assessed(id, service string) container.InspectResponse {
	return container.InspectResponse{ID: strings.Repeat(id, 64), Name: "/" + service, Image: "sha256:old", Config: &container.Config{Image: "app:latest", Labels: map[string]string{"com.docker.compose.service": service}}, HostConfig: &container.HostConfig{}, State: &container.State{Status: "running", Running: true}}
}

func TestPreviewHandlesIncomingDependenciesAndUnrelatedChurn(t *testing.T) {
	item := assessed("a", "web")
	var missingTarget atomic.Bool
	var linkedPeer atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			_ = json.NewEncoder(w).Encode([]container.Summary{{ID: item.ID}, {ID: strings.Repeat("b", 64)}})
		case strings.Contains(r.URL.Path, "/containers/"+item.ID+"/") && !missingTarget.Load():
			_ = json.NewEncoder(w).Encode(item)
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"sha256:old","Os":"linux","Architecture":"amd64"}`))
		case strings.Contains(r.URL.Path, "/containers/"+strings.Repeat("b", 64)+"/") && linkedPeer.Load():
			peer := assessed("b", "caller")
			peer.HostConfig.Links = []string{"/web:/caller/web"}
			_ = json.NewEncoder(w).Encode(peer)
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"message":"No such container"}`))
		}
	}))
	defer server.Close()
	cli, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	m := &Manager{data: data, client: cli}
	request := Request{ID: "container:" + item.ID, Operation: "update"}
	if _, err := m.Preview(context.Background(), request); err != nil {
		t.Fatal("unrelated churn broke preview", err)
	}
	linkedPeer.Store(true)
	if _, err := m.Preview(context.Background(), request); !errors.Is(err, ErrUnsupported) {
		t.Fatal("incoming legacy name link accepted", err)
	}
	linkedPeer.Store(false)
	missingTarget.Store(true)
	if _, err := m.Preview(context.Background(), request); err == nil {
		t.Fatal("missing selected target accepted")
	}
}

func TestLegacyBindConversionNeverCreatesMissingSource(t *testing.T) {
	item := assessed("a", "web")
	item.HostConfig.Binds = []string{"/existing:/data:ro,rprivate"}
	item.Mounts = []container.MountPoint{{Type: mount.TypeBind, Source: "/existing", Destination: "/data", RW: false, Propagation: mount.PropagationRPrivate}}
	if err := assess(item); err != nil {
		t.Fatal(err)
	}
	created := creation(&entry{Old: item, Platform: "linux/amd64"}, "")
	if len(created.HostConfig.Binds) != 0 || len(created.HostConfig.Mounts) != 1 || created.HostConfig.Mounts[0].BindOptions.CreateMountpoint || !created.HostConfig.Mounts[0].ReadOnly {
		t.Fatal("legacy bind may create a missing source or lose access mode")
	}
	item.HostConfig.Binds[0] = "/existing:/data:Z"
	if err := assess(item); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unrepresentable SELinux bind accepted", err)
	}
}

func TestRestartInvalidatesConfirmation(t *testing.T) {
	state := &journal{Request: Request{ID: "container:test", Operation: "update"}, Entries: []*entry{{Old: assessed("a", "web")}}}
	before := fingerprint(state)
	state.Entries[0].Old.State.StartedAt = "2026-10-08T00:00:00Z"
	if fingerprint(state) == before {
		t.Fatal("external restart ignored")
	}
}

func TestFingerprintIgnoresMountOrderButDetectsMountChanges(t *testing.T) {
	item := assessed("a", "web")
	item.Mounts = []container.MountPoint{
		{Type: mount.TypeBind, Source: "/source", Destination: "/bind", RW: true},
		{Type: mount.TypeVolume, Name: "volume", Destination: "/data", RW: true},
	}
	state := &journal{Request: Request{ID: "container:test", Operation: "update"}, Entries: []*entry{{Old: item}}}
	before := fingerprint(state)
	item.Mounts[0], item.Mounts[1] = item.Mounts[1], item.Mounts[0]
	if before != fingerprint(state) {
		t.Fatal("Docker mount ordering invalidated unchanged runtime")
	}
	if item.Mounts[0].Destination != "/data" {
		t.Fatal("fingerprinting mutated the runtime snapshot")
	}
	item.Mounts[0].Name = "different-volume"
	if before == fingerprint(state) {
		t.Fatal("real mount change ignored")
	}
}
func TestAssessmentRejectsUnpreservableSettingsAndState(t *testing.T) {
	for _, change := range []func(*container.InspectResponse){
		func(item *container.InspectResponse) { item.HostConfig.AutoRemove = true },
		func(item *container.InspectResponse) { item.HostConfig.VolumesFrom = []string{"peer"} },
		func(item *container.InspectResponse) { item.HostConfig.Links = []string{"peer:peer"} },
		func(item *container.InspectResponse) { item.HostConfig.NetworkMode = "container:peer" },
		func(item *container.InspectResponse) { item.HostConfig.PublishAllPorts = true },
		func(item *container.InspectResponse) { item.State.Paused = true },
		func(item *container.InspectResponse) { item.Config.ArgsEscaped = true },
	} {
		item := assessed("a", "web")
		change(&item)
		if err := assess(item); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unsafe configuration accepted: %v", err)
		}
	}
}
func TestDependencyOrderingAndMissingCyclicDependencies(t *testing.T) {
	web, db := &entry{Old: assessed("a", "web")}, &entry{Old: assessed("b", "db")}
	web.Old.Config.Labels["com.docker.compose.depends_on"] = "db:service_started:false"
	order, err := dependencyOrder([]*entry{web, db})
	if err != nil || len(order) != 2 || order[0] != 1 {
		t.Fatal("dependency not ordered first", order, err)
	}
	for _, spec := range []string{"absent:service_started:false", "db:unknown:false", "db:service_started:maybe"} {
		web.Old.Config.Labels["com.docker.compose.depends_on"] = spec
		if _, err := dependencyOrder([]*entry{web, db}); !errors.Is(err, ErrUnsupported) {
			t.Fatal("unsupported dependency accepted")
		}
	}
	web.Old.Config.Labels["com.docker.compose.depends_on"] = "db:service_started:false"
	db.Old.Config.Labels["com.docker.compose.depends_on"] = "web:service_started:false"
	if _, err := dependencyOrder([]*entry{web, db}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("cycle accepted")
	}
}
func TestFingerprintIncludesConfigStateAndOperationWithoutVolatileHealth(t *testing.T) {
	state := &journal{Request: Request{ID: "compose:test", Operation: "update"}, Entries: []*entry{{Old: assessed("a", "web")}}}
	before := fingerprint(state)
	state.Entries[0].Old.State.Health = &container.Health{Status: "healthy"}
	if fingerprint(state) != before {
		t.Fatal("volatile health invalidated the preview")
	}
	state.Entries[0].Old.Config.Env = []string{"TOKEN=private"}
	if fingerprint(state) == before {
		t.Fatal("configuration drift ignored")
	}
	state.Request.Operation = "recreate"
	if fingerprint(state) == before {
		t.Fatal("operation change ignored")
	}
}
