package managed

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
)

func TestUnchangedRequiresAllServiceImagesAndReplicaCounts(t *testing.T) {
	two := 2
	model := composeModel{Services: map[string]modelService{"web": {Scale: &two}, "idle": {}}}
	item := func(id, service, image string) container.InspectResponse {
		return container.InspectResponse{ID: id, Image: image, Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": service}}}
	}
	runtime := []container.InspectResponse{item("a", "web", "old"), item("b", "web", "old"), item("c", "idle", "old")}
	if !unchangedImages(model, runtime, map[string]string{"web": "old", "idle": "old"}) {
		t.Fatal("same image replicas should be unchanged")
	}
	for _, targets := range []map[string]string{{"web": "new", "idle": "old"}, {"web": "old"}, {"web": "", "idle": "old"}} {
		if unchangedImages(model, runtime, targets) {
			t.Fatal("changed/incomplete target reported unchanged")
		}
	}
	if unchangedImages(model, runtime[:2], map[string]string{"web": "old", "idle": "old"}) {
		t.Fatal("missing service reported unchanged")
	}
	if unchangedImages(model, append(runtime, item("d", "unknown", "old")), map[string]string{"web": "old", "idle": "old"}) {
		t.Fatal("orphan service reported unchanged")
	}
}

func TestResolvedDefinitionPreservesSettingsAndOriginalBindBase(t *testing.T) {
	raw := `{"services":{"web":{"image":"moving:tag","environment":{"TOKEN":"private$$literal"},"cpus":0.75,"security_opt":["no-new-privileges:true"],"volumes":[{"type":"bind","source":"./data","target":"/data","read_only":true}]}},"networks":{"default":{"name":"original_network"}}}`
	definition, err := resolvedDefinition(Preview{resolved: json.RawMessage(raw)}, "/host/original", map[string]string{"web": "sha256:target"})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"image":"sha256:target"`, `"nox-yard.image-reference":"moving:tag"`, `"pull_policy":"never"`, `"source":"/host/original/data"`, `"cpus":0.75`, `"security_opt"`, `private$$literal`, `original_network`} {
		if !strings.Contains(definition, value) {
			t.Fatalf("setting missing: %s", value)
		}
	}
	decoded, err := decodedComposeModel([]byte(definition))
	if err != nil || !strings.Contains(string(decoded), "private$literal") || strings.Contains(string(decoded), "private$$literal") {
		t.Fatal("runtime comparisons must decode Compose's literal-dollar escape")
	}
}

func TestDockerErrorsNeverReturnRawOutput(t *testing.T) {
	for _, output := range []string{"private-secret: denied access", "manifest unknown private-secret", "address already in use private-secret", "arbitrary private-secret output"} {
		result := safeDockerFailure(output)
		if result == "" || strings.Contains(result, "private-secret") {
			t.Fatal("raw daemon output escaped")
		}
	}
}

func TestRunningStateComparisonRequiresSameContainerIdentities(t *testing.T) {
	before := []container.InspectResponse{{ID: "one", State: &container.State{Running: false}}}
	if !sameRunningState(before, before) {
		t.Fatal("identical state rejected")
	}
	for _, current := range [][]container.InspectResponse{{{ID: "one", State: &container.State{Running: true}}}, {{ID: "two", State: &container.State{Running: false}}}, nil} {
		if sameRunningState(current, before) {
			t.Fatal("state/identity drift ignored")
		}
	}
	if matchesSourceImages(before, []store.ImageIdentity{{ContainerID: "other"}}) {
		t.Fatal("source identity drift ignored")
	}
}
