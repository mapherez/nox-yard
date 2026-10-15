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

func TestRollbackDefinitionPinsEffectiveInitAndLogging(t *testing.T) {
	raw := `{"services":{"web":{"image":"sha256:old","init":false,"logging":{"driver":"json-file","options":{"max-size":"10m"}}},"idle":{"image":"sha256:idle"}}}`
	_, web, _ := adoptionSample(t)
	web.HostConfig.Init = new(true)
	web.HostConfig.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3", "tag": "private${TOKEN}$$literal"}}
	idle := container.InspectResponse{Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": "idle"}}, HostConfig: &container.HostConfig{Init: new(false), LogConfig: container.LogConfig{Type: "none"}}}
	definition, err := definitionRuntimeSettings(raw, []container.InspectResponse{web, idle})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodedComposeModel([]byte(definition))
	if err != nil {
		t.Fatal(err)
	}
	var model composeModel
	if err := json.Unmarshal(decoded, &model); err != nil {
		t.Fatal(err)
	}
	for _, item := range []container.InspectResponse{web, idle} {
		service := model.Services[item.Config.Labels["com.docker.compose.service"]]
		if service.Init == nil || *service.Init != *item.HostConfig.Init || service.Logging == nil || service.Logging.Driver != item.HostConfig.LogConfig.Type || len(service.Logging.Options) != len(item.HostConfig.LogConfig.Config) {
			t.Fatal("effective init/logging was not pinned")
		}
		for key, value := range item.HostConfig.LogConfig.Config {
			if service.Logging.Options[key] != value {
				t.Fatal("logging option changed during Compose serialization")
			}
		}
	}
	if web.HostConfig.LogConfig.Config["tag"] != "private${TOKEN}$$literal" || web.HostConfig.LogConfig.Config["max-file"] != "3" {
		t.Fatal("snapshot mutated original inspection")
	}
}

func TestRollbackRejectsMixedReplicaInitAndLogging(t *testing.T) {
	for _, field := range []string{"init", "driver", "options"} {
		t.Run(field, func(t *testing.T) {
			_, first, _ := adoptionSample(t)
			_, second, _ := adoptionSample(t)
			first.HostConfig.Init, second.HostConfig.Init = new(true), new(true)
			first.HostConfig.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m"}}
			second.HostConfig.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m"}}
			if _, err := definitionRuntimeSettings(`{"services":{"web":{}}}`, []container.InspectResponse{first, second}); err != nil {
				t.Fatal("identical replicas rejected", err)
			}
			switch field {
			case "init":
				second.HostConfig.Init = new(false)
			case "driver":
				second.HostConfig.LogConfig.Type = "local"
			case "options":
				second.HostConfig.LogConfig.Config["max-file"] = "secret-value"
			}
			if _, err := definitionRuntimeSettings(`{"services":{"web":{}}}`, []container.InspectResponse{first, second}); err == nil || !strings.Contains(err.Error(), "mixed init or logging") || strings.Contains(err.Error(), "secret-value") {
				t.Fatalf("mixed replicas accepted or secrets exposed: %v", err)
			}
		})
	}
}

func TestRollbackVerifiesEffectiveSettingsIncludingInheritedOptions(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*container.HostConfig)
		wantOK bool
	}{
		{"same", func(*container.HostConfig) {}, true},
		{"init", func(h *container.HostConfig) { h.Init = new(false) }, false},
		{"driver", func(h *container.HostConfig) { h.LogConfig.Type = "local" }, false},
		{"changed option", func(h *container.HostConfig) { h.LogConfig.Config["max-size"] = "secret-value" }, false},
		{"missing option", func(h *container.HostConfig) { delete(h.LogConfig.Config, "max-size") }, false},
		{"added default", func(h *container.HostConfig) { h.LogConfig.Config["max-file"] = "3" }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, before, _ := adoptionSample(t)
			_, after, _ := adoptionSample(t)
			before.HostConfig.Init, after.HostConfig.Init = new(true), new(true)
			before.HostConfig.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m"}}
			after.HostConfig.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m"}}
			test.change(after.HostConfig)
			err := verifyRestoredRuntimeSettings([]container.InspectResponse{before}, []container.InspectResponse{after})
			if (err == nil) != test.wantOK || (err != nil && strings.Contains(err.Error(), "secret-value")) {
				t.Fatalf("unexpected restored-settings result: %v", err)
			}
		})
	}
	// Docker represents empty options both ways.
	_, before, _ := adoptionSample(t)
	_, after, _ := adoptionSample(t)
	after.HostConfig.Init = new(false)
	after.HostConfig.LogConfig.Config = map[string]string{}
	if err := verifyRestoredRuntimeSettings([]container.InspectResponse{before}, []container.InspectResponse{after}); err != nil {
		t.Fatal("equivalent default settings rejected", err)
	}
}
