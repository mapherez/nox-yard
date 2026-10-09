package recreate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func TestRollbackVerifiesRestoredOriginalDespiteDockerEmptyCollections(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		change    func(*container.InspectResponse)
		wantError bool
	}{
		{"unchanged", func(*container.InspectResponse) {}, false},
		{"environment changed", func(item *container.InspectResponse) { item.Config.Env = []string{"TOKEN=changed-private-value"} }, true},
		{"resource limit changed", func(item *container.InspectResponse) { item.HostConfig.Memory++ }, true},
		{"mount source changed", func(item *container.InspectResponse) { item.Mounts[0].Source = "/other" }, true},
		{"static address changed", func(item *container.InspectResponse) {
			item.NetworkSettings.Networks["app"].IPAMConfig.IPv4Address = netip.MustParseAddr("172.20.0.11")
		}, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			data, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			old := assessed("a", "web")
			old.Config.Env = []string{"TOKEN=original-private-value"}
			old.State.StartedAt = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
			old.Mounts = []container.MountPoint{{Type: mount.TypeBind, Source: "/original", Destination: "/data", RW: true}}
			old.NetworkSettings = &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{"app": {IPAMConfig: &network.EndpointIPAMConfig{IPv4Address: netip.MustParseAddr("172.20.0.10")}}}}
			actual := clone(old)
			actual.Config.ExposedPorts = map[network.Port]struct{}{}
			actual.HostConfig.Binds = []string{}
			actual.HostConfig.OomKillDisable = new(bool)
			actual.NetworkSettings.Networks["app"].DriverOpts = map[string]string{}
			actual.NetworkSettings.Networks["app"].Aliases = []string{}
			scenario.change(&actual)
			backup := clone(old)
			backup.Name = "/retained-original"
			backup.State.Running = false
			backup.NetworkSettings.Networks = map[string]*network.EndpointSettings{}
			current := backup
			var removed, renamed, connected, started bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/candidate"):
					removed = true
					w.WriteHeader(http.StatusNoContent)
				case strings.HasSuffix(r.URL.Path, "/containers/"+old.ID+"/json"):
					_ = json.NewEncoder(w).Encode(current)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+old.ID+"/rename"):
					renamed = true
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/networks/app/connect"):
					connected = true
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/"+old.ID+"/start"):
					started = true
					current = actual
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected Docker API request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			cli, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.56"))
			if err != nil {
				t.Fatal(err)
			}
			defer cli.Close()
			job, err := store.NewJob("container:"+old.ID, "engine", "update", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := data.CreateJob(job); err != nil {
				t.Fatal(err)
			}
			state := &journal{Request: Request{ID: job.TargetID}, Order: []int{0}, Mutated: true, Entries: []*entry{{Old: old, Backup: "retained-original", NewID: "candidate", Touched: true}}}
			manager := &Manager{data: data, client: cli}
			err = manager.rollback(context.Background(), job, state)
			if (err != nil) != scenario.wantError {
				t.Fatalf("rollback error = %v; wantError = %v", err, scenario.wantError)
			}
			if !removed || !renamed || !connected || !started {
				t.Fatal("rollback skipped candidate removal or original restoration")
			}
		})
	}
}
