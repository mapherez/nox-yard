package inventory

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
)

func initializerFixture() []container.Summary {
	var items []container.Summary
	for _, service := range []string{"bot", "db", "state-init", "volume-init"} {
		state, status := container.ContainerState("running"), "Up 2 hours"
		if strings.HasSuffix(service, "init") {
			state, status = "exited", "Exited (0) 2 hours ago"
		}
		items = append(items, container.Summary{ID: service, Names: []string{"/" + service}, State: state, Status: status,
			Labels: map[string]string{composeProjectLabel: "bot", composeServiceLabel: service, "com.docker.compose.oneoff": "False"}})
	}
	items[0].Labels["com.docker.compose.depends_on"] = "db:service_started:false,state-init:service_completed_successfully:false"
	items[1].Labels["com.docker.compose.depends_on"] = "volume-init:service_completed_successfully:false"
	items[2].Labels["com.docker.compose.depends_on"] = "db:service_started:false"
	return items
}

func TestProjectStateRecognizesOnlySuccessfulInitializers(t *testing.T) {
	for _, test := range []struct {
		name      string
		change    func([]container.Summary)
		state     string
		completed int
	}{
		{"two completed initializers", func([]container.Summary) {}, "running", 2},
		{"failed initializer", func(items []container.Summary) { items[2].Status = "Exited (1) 1 minute ago" }, "partial", 1},
		{"killed initializer", func(items []container.Summary) { items[2].Status = "Exited (137) 1 minute ago" }, "partial", 1},
		{"unknown exit code", func(items []container.Summary) { items[2].Status = "Exited" }, "partial", 1},
		{"unstarted initializer", func(items []container.Summary) { items[2].State = "created" }, "partial", 1},
		{"dead initializer", func(items []container.Summary) { items[2].State = "dead" }, "partial", 1},
		{"stopped application exited zero", func(items []container.Summary) {
			items[0].State = "exited"
			items[0].Status = "Exited (0) 1 minute ago"
		}, "partial", 2},
		{"stopped database", func(items []container.Summary) {
			items[1].State = "exited"
			items[1].Status = "Exited (0) 1 minute ago"
		}, "partial", 2},
		{"paused application", func(items []container.Summary) { items[0].State = "paused" }, "partial", 2},
		{"fully stopped project", func(items []container.Summary) { items[0].State = "exited"; items[1].State = "exited" }, "stopped", 2},
		{"service name alone is insufficient", func(items []container.Summary) { delete(items[0].Labels, "com.docker.compose.depends_on") }, "partial", 1},
		{"started dependency is not initializer", func(items []container.Summary) {
			items[0].Labels["com.docker.compose.depends_on"] = "state-init:service_started:false"
		}, "partial", 1},
		{"explicit oneshot label", func(items []container.Summary) {
			delete(items[0].Labels, "com.docker.compose.depends_on")
			items[2].Labels["nox-yard.lifecycle"] = "oneshot"
		}, "running", 2},
		{"old dependency label without restart flag", func(items []container.Summary) {
			items[0].Labels["com.docker.compose.depends_on"] = "state-init:service_completed_successfully"
		}, "running", 2},
		{"compose run is not service completion", func(items []container.Summary) { items[2].Labels["com.docker.compose.oneoff"] = "True" }, "partial", 1},
		{"hook is not service completion", func(items []container.Summary) { items[2].Labels["com.docker.compose.hook"] = "post_start" }, "partial", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := initializerFixture()
			test.change(items)
			containers := make([]Container, len(items))
			for i, item := range items {
				containers[i] = describe(item)
			}
			snapshot := group(items, containers, time.Now())
			if len(snapshot.Projects) != 1 || snapshot.Projects[0].State != test.state {
				t.Fatalf("wrong project state: %+v", snapshot.Projects)
			}
			completed := 0
			for _, item := range snapshot.Projects[0].Containers {
				if item.Completed {
					completed++
					if item.State != "exited" || (item.Service != "state-init" && item.Service != "volume-init") {
						t.Fatal("raw state changed or ordinary service marked completed", item)
					}
				}
			}
			if completed != test.completed {
				t.Fatalf("completed=%d want %d", completed, test.completed)
			}
		})
	}
}

func TestInitializerDeclarationsStayWithinProject(t *testing.T) {
	items := initializerFixture()
	// An unrelated project's dependency must not make a normal stopped service
	// in this project count as completed, even when service names match.
	delete(items[0].Labels, "com.docker.compose.depends_on")
	other := container.Summary{ID: "other", Names: []string{"/other"}, State: "running", Labels: map[string]string{
		composeProjectLabel: "other", composeServiceLabel: "app", "com.docker.compose.depends_on": "state-init:service_completed_successfully:false",
	}}
	items = append(items, other)
	containers := make([]Container, len(items))
	for i, item := range items {
		containers[i] = describe(item)
	}
	snapshot := group(items, containers, time.Now())
	for _, project := range snapshot.Projects {
		if project.ID == "compose:bot" && project.State != "partial" {
			t.Fatal("dependency crossed project boundary", project)
		}
	}
}

func TestCompletedOnlyProjectRemainsStopped(t *testing.T) {
	item := container.Summary{ID: "once", Names: []string{"/once"}, State: "exited", Status: "Exited (0)", Labels: map[string]string{"nox-yard.lifecycle": "oneshot"}}
	project := group([]container.Summary{item}, []Container{describe(item)}, time.Now()).Projects[0]
	if project.State != "stopped" || !project.Containers[0].Completed {
		t.Fatal("completion must not imply a running process", project)
	}
}

func TestInitializerInventoryStillUsesOnlyContainerList(t *testing.T) {
	var lists, forbidden atomic.Int32
	var current atomic.Value
	current.Store(initializerFixture())
	reader := dockerTestReader(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			lists.Add(1)
			_ = json.NewEncoder(w).Encode(current.Load())
			return
		}
		forbidden.Add(1)
		http.Error(w, "unexpected probe", 500)
	})
	snapshot, err := reader.Snapshot(t.Context())
	if err != nil || len(snapshot.Projects) != 1 || snapshot.Projects[0].State != "running" || lists.Load() != 1 || forbidden.Load() != 0 {
		t.Fatal("initializer inventory introduced probes or lost state", snapshot, err)
	}
	// A Docker event/refresh after a failed rerun must not retain completion.
	items := initializerFixture()
	items[2].Status = "Exited (2) 1 second ago"
	current.Store(items)
	snapshot, err = reader.Snapshot(t.Context())
	if err != nil || snapshot.Projects[0].State != "partial" || lists.Load() != 2 || forbidden.Load() != 0 {
		t.Fatal("stale successful initializer state", snapshot, err)
	}
}
