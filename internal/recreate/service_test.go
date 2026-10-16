package recreate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestServicePreviewAndJobKeepDependenciesOutsideReplacement(t *testing.T) {
	app, db, init := assessed("a", "web"), assessed("b", "db"), assessed("c", "init")
	items := []container.InspectResponse{app, db, init}
	for i := range items {
		items[i].Config.Labels["com.docker.compose.project"] = "sample"
		items[i].Config.Labels["com.docker.compose.container-number"] = "1"
	}
	items[0].Config.Labels["com.docker.compose.depends_on"] = "db:service_started:false,init:service_completed_successfully:false"
	items[2].State = &container.State{Status: "exited", ExitCode: 0}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			summaries := []container.Summary{}
			for _, item := range items {
				summaries = append(summaries, container.Summary{ID: item.ID, Labels: item.Config.Labels})
			}
			_ = json.NewEncoder(w).Encode(summaries)
		case strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{"Id":"sha256:old","Os":"linux","Architecture":"amd64"}`))
		default:
			for _, item := range items {
				if strings.Contains(r.URL.Path, "/containers/"+item.ID+"/") {
					_ = json.NewEncoder(w).Encode(item)
					return
				}
			}
			http.Error(w, "unexpected mutation", 500)
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
	manager := &Manager{client: cli, data: data, launch: func(context.Context, *store.Store, store.Job, string) error { return nil }}
	input := Request{ID: "compose:sample", Operation: "recreate", Service: "web"}
	preview, err := manager.Preview(t.Context(), input)
	if err != nil || len(preview.Items) != 1 || preview.Items[0].Service != "web" {
		t.Fatal("wrong service preview", preview, err)
	}
	project, err := manager.Preview(t.Context(), Request{ID: input.ID, Operation: "recreate"})
	if err != nil || project.Fingerprint == preview.Fingerprint || len(project.Items) != 3 {
		t.Fatal("scope not fingerprinted", project, err)
	}
	input.Confirm, input.Fingerprint = true, preview.Fingerprint
	job, err := manager.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	var work journal
	if json.Unmarshal([]byte(job.Payload), &work) != nil || len(work.Entries) != 1 || len(work.Dependencies) != 2 || len(job.SourceImages) != 1 {
		t.Fatal("dependency included in worker mutations", work)
	}
	for _, bad := range []Request{{ID: input.ID, Operation: "recreate", Service: "absent"}, {ID: input.ID, Operation: "update", Service: "web"}, {ID: "container:" + app.ID, Operation: "recreate", Service: "web"}} {
		if _, err := manager.Preview(t.Context(), bad); err == nil {
			t.Fatal("invalid scope accepted", bad)
		}
	}
}
