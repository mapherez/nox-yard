package managed

import (
	"encoding/json"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func TestAutomaticRuntimeRequiresRunningHealthyServices(t *testing.T) {
	preview := Preview{model: composeModel{Services: map[string]modelService{}}}
	// A running service without a healthcheck is permitted; stopped normal
	// services and unhealthy checks are not automatic update candidates.
	item := container.InspectResponse{Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": "web"}}, State: &container.State{Running: true, Status: "running"}}
	if !automaticRuntime(preview, []container.InspectResponse{item}) {
		t.Fatal("running service refused")
	}
	for _, state := range []*container.State{{Status: "exited"}, {Running: true, Paused: true}, {Running: true, Health: &container.Health{Status: "unhealthy"}}, {Running: true, OOMKilled: true}} {
		item.State = state
		if automaticRuntime(preview, []container.InspectResponse{item}) {
			t.Fatal("unsafe state admitted", state)
		}
	}
	item.State = &container.State{Running: true}
	item.Config.Labels["com.docker.compose.service"] = "nox-yard"
	if automaticRuntime(preview, []container.InspectResponse{item}) {
		t.Fatal("Yard automatically updated")
	}
}

func TestAutomaticRuntimeAllowsCompletedDeclaredDependencyWithoutStartingStoppedServices(t *testing.T) {
	var preview Preview
	if err := json.Unmarshal([]byte(`{"services":{"web":{"image":"app","depends_on":{"init":{"condition":"service_completed_successfully"}}},"init":{"image":"init"}}}`), &preview.model); err != nil {
		t.Fatal(err)
	}
	runtime := []container.InspectResponse{
		{Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": "web"}}, State: &container.State{Running: true, Status: "running"}},
		{Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": "init"}}, State: &container.State{Status: "exited", ExitCode: 0}},
	}
	if !automaticRuntime(preview, runtime) {
		t.Fatal("declared completed dependency refused")
	}
	runtime[1].State.ExitCode = 1
	if automaticRuntime(preview, runtime) {
		t.Fatal("failed one-shot admitted")
	}
	runtime[1].State.ExitCode = 0
	runtime[0].State.Running = false
	runtime[0].State.Status = "exited"
	if automaticRuntime(preview, runtime) {
		t.Fatal("stopped long-running service admitted")
	}
}
