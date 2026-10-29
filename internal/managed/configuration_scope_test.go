package managed

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
)

func scopeFixture() (FileSnapshot, []container.InspectResponse) {
	source := FileSnapshot{Name: "sample", Fingerprint: "files", Resolved: json.RawMessage(`{"services":{"app":{"image":"app:v2","deploy":{"replicas":2},"depends_on":{"init":{"condition":"service_completed_successfully"}}},"init":{"image":"init:v1"},"db":{"image":"db:v1"}}}`)}
	runtime := []container.InspectResponse{}
	for _, name := range []string{"app", "app", "init", "db"} {
		id := strings.Repeat(string(rune('a'+len(runtime))), 64)
		runtime = append(runtime, container.InspectResponse{ID: id, Name: "/" + name + id[:1], Image: "old:" + name, Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": name}}, HostConfig: &container.HostConfig{}, State: &container.State{Running: name != "init"}})
	}
	return source, runtime
}

func TestServiceScopeSelectionAndConfirmation(t *testing.T) {
	source, runtime := scopeFixture()
	selected, err := fileServiceRuntime(source, runtime, "app")
	if err != nil || len(selected) != 2 {
		t.Fatal("service replicas not selected", selected, err)
	}
	preview := scopedFilePreview(source, "app")
	if len(preview.model.Services) != 1 || len(expectedServices(preview.model)) != 1 {
		t.Fatal("dependency entered selected model", preview)
	}
	if _, err := fileServiceRuntime(source, runtime, "missing"); err == nil {
		t.Fatal("unknown service widened to project")
	}
	if fileOperationFingerprint(source, runtime, "recreate", "app") == fileOperationFingerprint(source, runtime, "recreate", "") {
		t.Fatal("service/project confirmations share fingerprint")
	}
	before := fileOperationFingerprint(source, runtime, "recreate", "app")
	runtime[0].State.Running = false
	if before == fileOperationFingerprint(source, runtime, "recreate", "app") {
		t.Fatal("stale running-state confirmation accepted")
	}
	if _, err := fileServiceRuntime(source, runtime, "app"); err == nil {
		t.Fatal("mixed replica states accepted")
	}
	runtime[0].State.Running = true
	runtime[3].HostConfig.NetworkMode = container.NetworkMode("container:" + runtime[0].ID)
	if _, err := fileServiceRuntime(source, runtime, "app"); err == nil {
		t.Fatal("incoming shared namespace accepted")
	}
}

func TestServiceCreateExcludesDependenciesAndOrphanRemoval(t *testing.T) {
	args := strings.Join(fileCreateArgs("recreate", "app"), " ")
	if !strings.Contains(args, "--no-deps") || strings.Contains(args, "--remove-orphans") || !strings.HasSuffix(args, " app") || !strings.Contains(args, "--no-build --pull never") {
		t.Fatal("unsafe service create", args)
	}
	if args := strings.Join(fileCreateArgs("recreate", ""), " "); strings.Contains(args, "--remove-orphans") || strings.Contains(args, "--no-deps") {
		t.Fatal("project scope changed", args)
	}
}

func TestServiceVerificationIgnoresUnselectedImagesButDetectsPeerChanges(t *testing.T) {
	source, runtime := scopeFixture()
	manager := &Manager{runtime: func(context.Context, string) ([]container.InspectResponse, error) { return runtime, nil }}
	preview := scopedFilePreview(source, "app")
	previous := append([]container.InspectResponse{}, runtime...)
	for i := range runtime[:2] {
		runtime[i].Image = "new:app"
	}
	work := &fileDeployment{Source: source, Service: "app", Runtime: previous}
	if err := manager.verifyFileDeployment(t.Context(), "sample", preview.model, map[string]string{"app": "new:app"}, work); err != nil {
		t.Fatal("unselected images incorrectly verified", err)
	}
	runtime[3].ID = "replacement-db"
	if err := manager.verifyFileDeployment(t.Context(), "sample", preview.model, map[string]string{"app": "new:app"}, work); err == nil {
		t.Fatal("dependency replacement was missed")
	}
}

func TestStoppedInitializerServiceDoesNotRunItsCommand(t *testing.T) {
	source, runtime := scopeFixture()
	preview := scopedFilePreview(source, "init")
	if !expectedServices(preview.model)["init"].oneshot {
		t.Fatal("one-shot identity lost outside parent scope")
	}
	verification := fileServiceVerification(preview, serviceRuntime(runtime, "init"), "init")
	if len(expectedServices(verification.model)) != 0 {
		t.Fatal("stopped initializer required execution")
	}
}

func TestServicePortsDoNotIgnoreSiblingBindings(t *testing.T) {
	claims := []hostPortClaim{{first: 8080, last: 8080, protocol: "tcp", owner: "app"}}
	peer := container.Summary{ID: "peer", State: "running", Labels: map[string]string{"com.docker.compose.project": "sample", "com.docker.compose.service": "db"}, Ports: []container.PortSummary{{IP: netip.MustParseAddr("0.0.0.0"), PrivatePort: 8080, PublicPort: 8080, Type: "tcp"}}}
	if err := compareServiceHostPorts("sample", "app", claims, []container.Summary{peer}); err == nil {
		t.Fatal("sibling port conflict ignored")
	}
	peer.Labels["com.docker.compose.service"] = "app"
	if err := compareServiceHostPorts("sample", "app", claims, []container.Summary{peer}); err != nil {
		t.Fatal("selected service cannot reuse its port", err)
	}
}

func TestFileOperationFingerprintIncludesExternalRestartRevision(t *testing.T) {
	first := []container.InspectResponse{{ID: "same-id", State: &container.State{Running: true, StartedAt: "2026-10-29T00:00:00Z"}}}
	next := []container.InspectResponse{{ID: "same-id", State: &container.State{Running: true, StartedAt: "2026-10-29T00:01:00Z"}}}
	if fileOperationFingerprint(FileSnapshot{}, first, "recreate", "") == fileOperationFingerprint(FileSnapshot{}, next, "recreate", "") {
		t.Fatal("external restart did not invalidate confirmation")
	}
	if sameRunningState(first, next) {
		t.Fatal("external restart was accepted before mutation")
	}
}
