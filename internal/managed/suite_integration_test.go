package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/suite"
	suitebackend "github.com/mapherez/nox-yard/internal/suite/backend"
	"github.com/mapherez/nox-yard/internal/suite/local"
)

// The script first enrolls the real Yard image. This runner then opens that same
// isolated state and runs its real managed worker/controller against real Auth
// and Backend. No production route or user session is bypassed by a test route.
func TestSuiteDeploymentDockerAcceptance(t *testing.T) {
	base := os.Getenv("NOX_SUITE_ACCEPTANCE_BASE")
	if base == "" {
		t.Skip("run scripts/smoke-suite.py against disposable services")
	}
	name, image := os.Getenv("NOX_SUITE_ACCEPTANCE_NAME"), os.Getenv("NOX_SUITE_ACCEPTANCE_PRODUCT_IMAGE")
	if !strings.HasPrefix(name, "nox-yard-suite-") || image == "" {
		t.Fatal("invalid isolated acceptance configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Minute)
	defer cancel()
	data, err := store.Open("/data")
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	if err = data.SetProjectsBase(base); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(data, previewInventory{})
	if err != nil {
		t.Fatal(err)
	}
	manager.ConfigureSuiteVolume(os.Getenv("NOX_SUITE_LOCAL_VOLUME_NAME"))
	// Execute the same worker synchronously so failures remain attributable to the
	// accepted job, without requiring a second Yard runtime to own the socket.
	manager.launch = func(context.Context, *store.Store, store.Job, string) error { return nil }
	controller, err := suite.New(suite.Config{Directory: "/run/nox-suite"}, data, os.Getenv("NOX_SUITE_ACCEPTANCE_YARD_VERSION"), manager)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	backend, err := local.NewClient("/run/nox-suite")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	snapshot := func() noxmcp.SuiteRegistrySnapshot {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://nox-backend/api/nox/products", nil)
		req.Header.Set("Authorization", "Bearer "+os.Getenv("NOX_SUITE_ACCEPTANCE_ADMIN_KEY"))
		res, err := backend.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		value, err := noxmcp.LoadSuiteRegistrySnapshot(raw)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("snapshot HTTP %d: %v", res.StatusCode, err)
		}
		return value
	}
	docker := func(parts ...string) string {
		t.Helper()
		raw, err := exec.CommandContext(ctx, "docker", parts...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture command failed: %v: %s", err, raw)
		}
		return strings.TrimSpace(string(raw))
	}
	execute := func(job store.Job, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}

		t.Setenv("NOX_JOB_ID", job.ID)
		if err = manager.execute(ctx, job); err != nil {
			t.Fatal(err)
		}
		actual, _, err := data.Job(job.ID)
		if err != nil || actual.Status != "succeeded" {
			t.Fatalf("job %s: %s / %s / %s / %v", job.Operation, actual.Status, actual.Outcome, actual.Error, err)
		}
	}
	input := Request{Name: name, Mode: "new", Source: SourceInput{Kind: "paste", YAML: fmt.Sprintf(`services:
  product:
    image: %s
    pull_policy: never
    command: [python3, /fixture.py]
    environment: {FIXTURE_PRODUCT_ID: lifecycle-fixture}
    labels: {io.nox.suite.product-id: lifecycle-fixture, nox-yard.acceptance: %s}
    volumes: [%q]
    stop_grace_period: 1s
  companion:
    image: %s
    pull_policy: never
    command: [sleep, '600']
    labels: {nox-yard.acceptance: %s}
    stop_grace_period: 1s
`, image, name, os.Getenv("NOX_SUITE_ACCEPTANCE_PRODUCT_SCRIPT")+":/fixture.py:ro", image, name)}}
	preview, _, err := manager.Preview(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Fingerprint = preview.Fingerprint
	execute(manager.Submit(ctx, input))
	var binding string
	wait := func(want, version string) noxmcp.SuiteRegistrationRecord {
		t.Helper()
		deadline := time.Now().Add(100 * time.Second)
		var last string
		for time.Now().Before(deadline) {
			if err = controller.Reconcile(ctx); err != nil {
				var failure *suitebackend.Failure
				if errors.As(err, &failure) && failure.Retryable {
					time.Sleep(time.Second)
					continue
				}
				t.Fatalf("controller reconciliation failed: %v", err)
			}
			state := snapshot()
			for _, record := range state.Products {
				if record.ProductID != "lifecycle-fixture" {
					continue
				}
				if want == "removed" {
					break
				}
				if record.ManagedBy == nil || record.ManagedBy.ProductID != "nox-yard" {
					t.Fatal("missing installer ownership")
				}
				if binding != "" && record.ManagedBy.DeploymentID != binding {
					t.Fatal("deployment identity changed")
				}
				binding = record.ManagedBy.DeploymentID
				live, readErr := data.SuiteBindings(ctx)
				if readErr != nil {
					t.Fatal(readErr)
				}
				settled := false
				for _, b := range live {
					if b.ID == binding && b.State == "registered" && b.Message == "" {
						settled = true
					}
				}
				if !settled {
					continue
				}
				if version != "" && record.Declaration.ProductVersion != version {
					continue
				}
				for _, envelope := range state.Observations {
					if envelope.Observation.ProductID == record.ProductID && envelope.Observation.Source == "yard" && envelope.Observation.State == want {
						return record
					}
				}
			}
			if want == "removed" {
				found := false
				for _, record := range state.Products {
					if record.ProductID == "lifecycle-fixture" {
						found = true
					}
				}
				bindings, _ := data.SuiteBindings(ctx)
				pending := false
				for _, b := range bindings {
					if b.ProductID == "lifecycle-fixture" && b.State != "removed" {
						pending = true
					}
				}
				if !found && !pending {
					return noxmcp.SuiteRegistrationRecord{}
				}
			}
			bindings, _ := data.SuiteBindings(ctx)
			raw, _ := json.Marshal(bindings)
			last = string(raw) + " " + last
			time.Sleep(time.Second)
		}
		t.Fatalf("Suite did not reach %s / %s: %s", want, version, last)
		return noxmcp.SuiteRegistrationRecord{}
	}
	first := wait("running", "fixture-1")
	// Recreate the persisted journal from just before a lost registration
	// response. Backend has committed; the original mutation key remains.
	bindings, err := data.SuiteBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bindings {
		if b.ID != binding {
			continue
		}
		var journal map[string]any
		if err = json.Unmarshal(b.Private, &journal); err != nil {
			t.Fatal(err)
		}
		delete(journal, "record")
		journal["fingerprint"] = ""
		journal["epoch"] = ""
		journal["epochKey"] = ""
		journal["sequence"] = "0"
		b.Private, _ = json.Marshal(journal)
		b.State = "integration_pending"
		if err = data.SaveSuiteBinding(b); err != nil {
			t.Fatal(err)
		}
	}
	recoveredResponse := wait("running", "fixture-1")
	if recoveredResponse.AssociationID != first.AssociationID || recoveredResponse.Revision != first.Revision {
		t.Fatal("lost response created a duplicate association")
	}
	t.Log("PASS: committed registration recovered from its pre-response journal without mismatched idempotency replay")
	items, err := manager.runtime(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	var participant string
	for _, item := range items {
		mounted := false
		for _, mount := range item.Mounts {
			if mount.Destination == "/run/nox-suite" {
				mounted = true
			}
		}
		if item.Config.Labels["com.docker.compose.service"] == "product" {
			if !mounted {
				t.Fatal("channel not prepared")
			}
			participant = item.ID
		} else if mounted {
			t.Fatal("ordinary companion received Suite channel")
		}
	}
	t.Log("PASS: real managed deployment automatically prepared, paired, ACKed, registered and observed; companion isolated")
	execute(manager.Operation(name, "stop", false))
	wait("stopped", "")
	execute(manager.Operation(name, "start", false))
	restarted := wait("running", "fixture-1")
	if restarted.AssociationID != first.AssociationID {
		t.Fatal("stop/start changed association")
	}
	t.Log("PASS: stopped product reported without an active participant socket; start retains identity")
	docker("exec", participant, "sh", "-c", "echo fixture-2 > /fixture-state/version")
	updated := wait("running", "fixture-2")
	if updated.Revision == first.Revision {
		t.Fatal("declaration update kept old registry revision")
	}
	t.Log("PASS: changed declaration re-paired and refreshed registration without reinstalling Docker deployment")
	controller.Close()
	controller, err = suite.New(suite.Config{Directory: "/run/nox-suite"}, data, os.Getenv("NOX_SUITE_ACCEPTANCE_YARD_VERSION"), manager)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	recovered := wait("running", "fixture-2")
	if recovered.AssociationID != updated.AssociationID {
		t.Fatal("recovery duplicated registration")
	}
	t.Log("PASS: restarted controller recovers durable registration without replaying Docker job")
	execute(manager.Operation(name, "remove", false))
	wait("removed", "")
	binding = ""
	// Removal intentionally retains host files; reinstall into a new project
	// directory and prove the same product can receive a fresh association.
	name += "-reinstalled"
	input.Name = name
	backendContainer := os.Getenv("NOX_SUITE_ACCEPTANCE_BACKEND_CONTAINER")
	docker("stop", backendContainer)
	preview, _, err = manager.Preview(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Fingerprint = preview.Fingerprint
	execute(manager.Submit(ctx, input))
	if err = controller.Reconcile(ctx); err == nil || controller.Ready() {
		t.Fatal("Backend outage did not mark integration unavailable")
	}
	beforeJobs, err := data.Jobs(ctx, "compose:"+name, false)
	if err != nil || len(beforeJobs) != 1 || beforeJobs[0].Status != "succeeded" {
		t.Fatal("Backend outage changed Docker result", err)
	}
	docker("start", backendContainer)
	reinstalled := wait("running", "fixture-1")
	if reinstalled.AssociationID == first.AssociationID || reinstalled.ManagedBy.DeploymentID == first.ManagedBy.DeploymentID {
		t.Fatal("reinstallation reused retired association")
	}
	afterJobs, err := data.Jobs(ctx, "compose:"+name, false)
	if err != nil || len(afterJobs) != len(beforeJobs) || afterJobs[0].ID != beforeJobs[0].ID {
		t.Fatal("integration recovery replayed Docker deployment", err)
	}
	t.Log("PASS: Docker deployment succeeds while Backend is unavailable; recovery integrates it without replaying its job")
	execute(manager.Operation(name, "remove", false))
	wait("removed", "")
	t.Log("PASS: removal retires registry/grant and reinstall establishes a fresh scoped association")
}
