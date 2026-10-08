package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
)

func TestManagedOwnershipBlocksEngineActionsAcrossFacades(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	job, _ := store.NewJob("compose:sample", "managed", "update", []string{"container:target"})
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	app := New(data, inventory.NewNotifier())
	c := &contextController{}
	app.Lifecycle = c
	app.ResourceKeys = func(context.Context, string) ([]string, error) {
		return []string{"compose:sample", "container:target"}, nil
	}
	if _, err := app.Action(context.Background(), "target", true, lifecycle.Restart); !errors.Is(err, store.ErrOperationConflict) {
		t.Fatalf("conflicting action admitted: %v", err)
	}
	if c.calls != 0 {
		t.Fatal("conflicting operation reached Docker")
	}
	if err := data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Action(context.Background(), "target", true, lifecycle.Restart); err != nil {
		t.Fatal(err)
	}
	history, err := app.JobHistory(context.Background(), "compose:sample")
	if err != nil || len(history) != 2 {
		t.Fatalf("project history lost container actions: %+v %v", history, err)
	}
}

func TestRestartReconcilesOnlyPreviousDirectOwners(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	old := New(data, inventory.NewNotifier())
	job, err := old.beginOperation(context.Background(), "compose:sample", "restart")
	if err != nil {
		t.Fatal(err)
	}
	if err := old.ReconcileDirectJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, _, _ := data.Job(job.ID)
	if current.Status != "running" {
		t.Fatal("current process lost its operation")
	}
	replacement := New(data, inventory.NewNotifier())
	if err := replacement.ReconcileDirectJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, _, _ = data.Job(job.ID)
	if current.Outcome != "recovery_required" {
		t.Fatal("restart assumed a direct result", current)
	}
	if _, err := replacement.beginOperation(context.Background(), "compose:sample", "restart"); !errors.Is(err, store.ErrOperationConflict) {
		t.Fatal("uncertain direct mutation can be replayed", err)
	}
}

func TestInterruptedGroupFailureRetainsOwnership(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	app := New(data, inventory.NewNotifier())
	job, err := app.beginOperation(context.Background(), "compose:sample", "restart")
	if err != nil {
		t.Fatal(err)
	}
	app.finishOperation(job, true, operationFailure(nil, []lifecycle.Failure{{Target: "one", Cause: context.DeadlineExceeded}}))
	current, _, _ := data.Job(job.ID)
	if current.Outcome != "recovery_required" || current.StartedAt == 0 {
		t.Fatalf("interrupted group lost durable ownership: %+v", current)
	}
	if _, err := app.beginOperation(context.Background(), job.TargetID, "restart"); !errors.Is(err, store.ErrOperationConflict) {
		t.Fatal("interrupted group was replayable", err)
	}
}

type canceledRemoval struct {
	lifecycle.Controller
	cancel context.CancelFunc
}

func (c canceledRemoval) RemoveProject(context.Context, string, string) (lifecycle.RemovalReport, error) {
	c.cancel()
	return lifecycle.RemovalReport{Items: []lifecycle.RemovalOutcome{{Status: "failed"}}}, nil
}
func TestCanceledRemovalReportRetainsOwnership(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	app := New(data, inventory.NewNotifier())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.Lifecycle = canceledRemoval{cancel: cancel}
	if _, err := app.Remove(ctx, "compose:sample", false, true, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	history, err := data.Jobs(context.Background(), "compose:sample", false)
	if err != nil || len(history) != 1 || history[0].Outcome != "recovery_required" {
		t.Fatalf("canceled removal lost reservation: %+v %v", history, err)
	}
}
