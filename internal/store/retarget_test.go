package store

import (
	"context"
	"errors"
	"testing"
)

func TestRetargetReservesOldNewIdentitiesAndFencesFinalizedJobs(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	job, _ := NewJob("container:old", "engine", "update", []string{"container:old"})
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if err := data.RetargetJob(job.ID, job.Owner, "container:new", []string{"container:new"}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"container:old", "container:new"} {
		conflict, _ := NewJob(target, "engine", "stop", nil)
		if err := data.CreateJob(conflict); !errors.Is(err, ErrOperationConflict) {
			t.Fatal("replacement identity not reserved", err)
		}
		history, err := data.Jobs(context.Background(), target, false)
		if err != nil || len(history) != 1 || history[0].ID != job.ID {
			t.Fatal("replacement history lost", err)
		}
	}
	if err := data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := data.RetargetJob(job.ID, job.Owner, "container:late", nil); !errors.Is(err, ErrJobChanged) {
		t.Fatal("late worker retargeted finalized job")
	}
	conflict, _ := NewJob("container:new", "engine", "stop", nil)
	if err := data.CreateJob(conflict); err != nil {
		t.Fatal("replacement reservation not released", err)
	}
}

func TestHistoryFollowsRepeatedStandaloneReplacementWithoutCrossingGroups(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	for _, pair := range [][2]string{{"old", "middle"}, {"middle", "new"}} {
		job, _ := NewJob("container:"+pair[0], "engine", "recreate", nil)
		if err := data.CreateJob(job); err != nil {
			t.Fatal(err)
		}
		if err := data.RetargetJob(job.ID, job.Owner, "container:"+pair[1], nil); err != nil {
			t.Fatal(err)
		}
		if err := data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	unrelated, _ := NewJob("compose:other", "engine", "update", []string{"container:new", "container:unrelated"})
	if err := data.CreateJob(unrelated); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishJob(unrelated.ID, unrelated.Owner, "failed", "failed", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"old", "middle", "new"} {
		history, err := data.Jobs(t.Context(), "container:"+target, false)
		if err != nil || len(history) != 3 {
			t.Fatal("lineage history lost", target, history, err)
		}
	}
	history, err := data.Jobs(t.Context(), "container:unrelated", false)
	if err != nil || len(history) != 1 {
		t.Fatal("history crossed a Compose group", history, err)
	}
}

func TestWorkerCleanupPreservesApplicationCleanupFailure(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	job, _ := NewJob("compose:removed", "managed", "remove", nil)
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishJob(job.ID, job.Owner, "succeeded", "removed", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := data.JobCleanupError(job.ID, "An exclusive volume could not be removed; inspect it on the host."); err != nil {
		t.Fatal(err)
	}
	if err := data.WorkerCleaned(job.ID, false); err != nil {
		t.Fatal(err)
	}
	result, _, err := data.Job(job.ID)
	var cleaned int64
	if err := data.db.QueryRow("SELECT worker_cleaned_at FROM operation_jobs WHERE id=?", job.ID).Scan(&cleaned); err != nil {
		t.Fatal(err)
	}
	if err != nil || cleaned == 0 || result.CleanupError == "" {
		t.Fatal("helper cleanup hid application cleanup failure", result, err)
	}
}
