package suite

import (
	"context"
	"testing"

	noxmcp "github.com/mapherez/nox-mcp/go"
	"github.com/mapherez/nox-yard/internal/store"
)

type runtimeResult struct{ value Deployment }

func (r runtimeResult) Inspect(context.Context, string, string, string) (Deployment, error) {
	return r.value, nil
}

func TestActiveDockerReplacementDoesNotRetireMissingParticipant(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	job, err := store.NewJob("compose:project", "managed", "recreate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	controller, err := New(Config{}, data, "test", runtimeResult{})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	binding := store.SuiteBinding{ProjectID: job.TargetID, Service: "api", ProductID: "future-product", State: "registered"}
	record := &noxmcp.SuiteRegistrationRecord{ProductID: binding.ProductID}
	journal := recovery{Record: record}
	if err = controller.reconcileBinding(t.Context(), &binding, &journal); err != nil {
		t.Fatal(err)
	}
	if binding.State != "registered" || journal.Record != record {
		t.Fatal("active replacement retired an existing registration")
	}
}

func TestStoppedUnregisteredParticipantDoesNotRequireActiveSocket(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	controller, err := New(Config{}, data, "test", runtimeResult{Deployment{Present: true, State: "stopped"}})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	binding := store.SuiteBinding{ProjectID: "compose:project", ProductID: "future-product", State: "pending"}
	if err = controller.reconcileBinding(t.Context(), &binding, &recovery{}); err != nil {
		t.Fatal("stopped participant should wait for normal startup", err)
	}
	if binding.State != "pending" {
		t.Fatal("stopped product lost pending integration")
	}
}

func TestActiveUncommittedVersionWaitsForDockerOutcome(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	job, err := store.NewJob("compose:project", "managed", "update", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	controller, err := New(Config{}, data, "test", runtimeResult{Deployment{Present: true, State: "running"}})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	binding := store.SuiteBinding{ProjectID: job.TargetID, ProductID: "future-product", State: "pending"}
	if err = controller.reconcileBinding(t.Context(), &binding, &recovery{}); err != nil {
		t.Fatal("uncommitted version attempted pairing before job completion", err)
	}
	if binding.State != "pending" {
		t.Fatal("uncommitted deployment changed membership")
	}
}
