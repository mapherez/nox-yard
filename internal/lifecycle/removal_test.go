package lifecycle

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/moby/moby/client"
)

type dockerRoundTrip func(*http.Request) (*http.Response, error)

func (fn dockerRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func dockerResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestRemovalReportsEveryResourceAndNeverDeletesUnreviewedResources(t *testing.T) {
	id := strings.Repeat("a", 64)
	imageID := "sha256:" + strings.Repeat("b", 64)
	labels := `"com.docker.compose.project":"sample","com.docker.compose.service":"app","com.docker.compose.project.config_files":"/srv/sample/compose.yaml"`
	mounts := `[{"Type":"volume","Name":"sample_data"},{"Type":"bind","Source":"/srv/sample/data"}]`
	networkSettings := `{"Networks":{"sample_default":{"NetworkID":"network-1"}}}`
	listed := fmt.Sprintf(`[{"Id":%q,"Names":["/sample-app-1"],"Labels":{%s},"ImageID":%q,"Mounts":%s,"NetworkSettings":%s}]`, id, labels, imageID, mounts, networkSettings)
	inspected := fmt.Sprintf(`{"Id":%q,"Name":"/sample-app-1","Image":%q,"Config":{"Image":"example/app:latest","Labels":{%s}},"Mounts":%s,"NetworkSettings":%s}`, id, imageID, labels, mounts, networkSettings)
	volumes := `{"Volumes":[{"Name":"sample_data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/sample_data","Labels":{"com.docker.compose.project":"sample"}}]}`
	networks := `[{"Id":"network-1","Name":"sample_default","Labels":{"com.docker.compose.project":"sample"}}]`
	containerExists := true
	imageExists := true
	var deletes []string
	transport := dockerRoundTrip(func(request *http.Request) (*http.Response, error) {
		path := strings.TrimPrefix(request.URL.Path, "/v1.56")
		switch {
		case request.Method == http.MethodGet && path == "/containers/json":
			if containerExists {
				return dockerResponse(200, listed), nil
			}
			return dockerResponse(200, `[]`), nil
		case request.Method == http.MethodGet && path == "/containers/"+id+"/json":
			return dockerResponse(200, inspected), nil
		case request.Method == http.MethodGet && path == "/volumes":
			return dockerResponse(200, volumes), nil
		case request.Method == http.MethodGet && path == "/networks":
			return dockerResponse(200, networks), nil
		case request.Method == http.MethodPost && path == "/containers/"+id+"/stop":
			return dockerResponse(204, ""), nil
		case request.Method == http.MethodDelete && path == "/containers/"+id:
			deletes = append(deletes, "container")
			containerExists = false
			return dockerResponse(204, ""), nil
		case request.Method == http.MethodDelete && path == "/volumes/sample_data":
			deletes = append(deletes, "volume")
			return dockerResponse(204, ""), nil
		case request.Method == http.MethodDelete && path == "/networks/network-1":
			deletes = append(deletes, "network")
			return dockerResponse(409, `{"message":"network still has an endpoint"}`), nil
		case request.Method == http.MethodDelete && path == "/images/"+imageID:
			deletes = append(deletes, "image")
			imageExists = false
			return dockerResponse(200, `[{"Deleted":"`+imageID+`"}]`), nil
		case request.Method == http.MethodGet && path == "/images/"+imageID+"/json" && !imageExists:
			return dockerResponse(404, `{"message":"No such image"}`), nil
		default:
			t.Fatalf("unexpected Docker request: %s %s", request.Method, request.URL.Path)
			return nil, nil
		}
	})
	docker, err := client.New(client.WithHTTPClient(&http.Client{Transport: transport}), client.WithAPIVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{client: docker}
	plan, err := m.PreviewRemoveProject(context.Background(), "compose:sample")
	if err != nil {
		t.Fatal(err)
	}
	if len(deletes) != 0 {
		t.Fatal("preview made a destructive Docker call")
	}
	if _, err := m.RemoveProject(context.Background(), "compose:sample", "stale"); err != ErrChanged || len(deletes) != 0 {
		t.Fatalf("stale confirmation must not delete anything: %v, %v", err, deletes)
	}
	report, err := m.RemoveProject(context.Background(), "compose:sample", plan.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, item := range report.Items {
		statuses[item.Kind] = item.Status
		if item.Status != "removed" && item.Reason == "" {
			t.Fatalf("leftover %s has no reason", item.Kind)
		}
	}
	for kind, want := range map[string]string{"container": "removed", "volume": "removed", "network": "failed", "image": "removed", "bind mount": "retained", "Compose file": "retained"} {
		if statuses[kind] != want {
			t.Errorf("%s: got %q, want %q", kind, statuses[kind], want)
		}
	}
	if strings.Join(deletes, ",") != "container,volume,network,image" {
		t.Fatalf("unexpected deletion order or scope: %v", deletes)
	}
}

func TestRemovalKeepsResourcesUsedByAnotherContainer(t *testing.T) {
	targetID, otherID := strings.Repeat("c", 64), strings.Repeat("d", 64)
	imageID := "sha256:" + strings.Repeat("e", 64)
	containerExists := true
	var deleted []string
	transport := dockerRoundTrip(func(request *http.Request) (*http.Response, error) {
		path := strings.TrimPrefix(request.URL.Path, "/v1.56")
		switch {
		case request.Method == http.MethodGet && path == "/containers/json":
			target := fmt.Sprintf(`{"Id":%q,"Names":["/sample-app"],"Labels":{"com.docker.compose.project":"sample"},"ImageID":%q,"Mounts":[{"Type":"volume","Name":"sample_data"}],"NetworkSettings":{"Networks":{"sample_default":{"NetworkID":"network-1"}}}}`, targetID, imageID)
			other := fmt.Sprintf(`{"Id":%q,"Names":["/other-app"],"Labels":{"com.docker.compose.project":"other"},"ImageID":%q,"Mounts":[{"Type":"volume","Name":"sample_data"}],"NetworkSettings":{"Networks":{"sample_default":{"NetworkID":"network-1"}}}}`, otherID, imageID)
			if containerExists {
				return dockerResponse(200, "["+target+","+other+"]"), nil
			}
			return dockerResponse(200, "["+other+"]"), nil
		case request.Method == http.MethodGet && path == "/containers/"+targetID+"/json":
			body := fmt.Sprintf(`{"Id":%q,"Name":"/sample-app","Image":%q,"Config":{"Image":"example/app:latest","Labels":{"com.docker.compose.project":"sample"}},"Mounts":[{"Type":"volume","Name":"sample_data"}],"NetworkSettings":{"Networks":{"sample_default":{"NetworkID":"network-1"}}}}`, targetID, imageID)
			return dockerResponse(200, body), nil
		case request.Method == http.MethodGet && path == "/volumes":
			return dockerResponse(200, `{"Volumes":[{"Name":"sample_data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/sample_data","Labels":{"com.docker.compose.project":"sample"}}]}`), nil
		case request.Method == http.MethodGet && path == "/networks":
			return dockerResponse(200, `[{"Id":"network-1","Name":"sample_default","Labels":{"com.docker.compose.project":"sample"}}]`), nil
		case request.Method == http.MethodPost && path == "/containers/"+targetID+"/stop":
			return dockerResponse(204, ""), nil
		case request.Method == http.MethodDelete && path == "/containers/"+targetID:
			deleted = append(deleted, "container")
			containerExists = false
			return dockerResponse(204, ""), nil
		case request.Method == http.MethodDelete:
			deleted = append(deleted, path)
			return dockerResponse(204, ""), nil
		default:
			t.Fatalf("unexpected Docker request: %s %s", request.Method, request.URL.Path)
			return nil, nil
		}
	})
	docker, err := client.New(client.WithHTTPClient(&http.Client{Transport: transport}), client.WithAPIVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{client: docker}
	plan, err := m.PreviewRemoveProject(context.Background(), "compose:sample")
	if err != nil {
		t.Fatal(err)
	}
	report, err := m.RemoveProject(context.Background(), "compose:sample", plan.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deleted, ",") != "container" {
		t.Fatalf("shared resource was deleted: %v", deleted)
	}
	for _, item := range report.Items {
		if item.Kind != "container" && (item.Status != "retained" || !strings.Contains(item.Reason, "other-app")) {
			t.Errorf("shared %s was not explained: %+v", item.Kind, item)
		}
	}
}
