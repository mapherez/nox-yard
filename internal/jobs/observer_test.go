package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/store"
)

func TestWorkerObservationDoesNotReplayOrAssumeOutcome(t *testing.T) {
	for _, scenario := range []struct {
		name, stage string
		state       WorkerState
		verify      bool
		expected    string
	}{
		{"live", "pulling", WorkerState{Exists: true, Running: true}, false, "running"},
		{"child still replacing", "replacing", WorkerState{HelpersRunning: true}, false, "running"},
		{"vanished during pull", "pulling", WorkerState{}, false, "recovery_required"},
		{"vanished during replacement", "replacing", WorkerState{}, false, "recovery_required"},
		{"verification proves completion", "verifying", WorkerState{}, true, "reconciled"},
		{"unverified completion", "verifying", WorkerState{}, false, "recovery_required"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			data, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			job, _ := store.NewJob("compose:sample", "managed", "update", nil)
			job.CreatedAt = time.Now().Add(-time.Minute).Unix()
			job.Stage = scenario.stage
			if err := data.CreateJob(job); err != nil {
				t.Fatal(err)
			}
			calls := 0
			verify := func(context.Context, store.Job) error {
				calls++
				if scenario.verify {
					return nil
				}
				return errors.New("not verified")
			}
			if err := observeState(context.Background(), data, job, scenario.state, verify); err != nil {
				t.Fatal(err)
			}
			current, _, err := data.Job(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			actual := current.Outcome
			if actual == "" {
				actual = current.Status
			}
			if actual != scenario.expected {
				t.Fatalf("outcome=%s want=%s", actual, scenario.expected)
			}
			if calls > 0 && scenario.stage != "verifying" {
				t.Fatal("observation repeated execution")
			}
		})
	}
}

func TestLateWorkerResultWinsObservationCAS(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	job, _ := store.NewJob("compose:sample", "managed", "update", nil)
	job.CreatedAt = time.Now().Add(-time.Minute).Unix()
	job.Stage = "verifying"
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	verify := func(context.Context, store.Job) error {
		return data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", "")
	}
	if err := observeState(context.Background(), data, job, WorkerState{}, verify); err != nil {
		t.Fatal(err)
	}
	current, _, _ := data.Job(job.ID)
	if current.Outcome != "verified" {
		t.Fatal("observer overwrote worker result")
	}
}
