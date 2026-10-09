package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func scheduledFixture(t *testing.T, key, target, date string, due int64) Job {
	t.Helper()
	job, err := NewJob(target, "engine", "update", nil)
	if err != nil {
		t.Fatal(err)
	}
	return ScheduledJob(WithSchedule(t.Context(), ScheduleOccurrence{Key: key, DueAt: due, Date: date, NextAt: due + 86400}), job)
}
func scheduleStore(t *testing.T) *Store {
	t.Helper()
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	return data
}
func TestSchedulesDefaultOffAndAtomicAdmissionAcrossConnections(t *testing.T) {
	ctx := t.Context()
	data := scheduleStore(t)
	row, found, err := data.ProjectSchedule(ctx, "compose:sample")
	if err != nil || found || row.Enabled || row.NextAt != 0 {
		t.Fatal("schedule enabled by default", row, err)
	}
	if _, err := data.SetProjectSchedule(ctx, row.Key, row.TargetID, true, 100); err != nil {
		t.Fatal(err)
	}
	other, err := Open(data.Directory())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	jobs := []Job{scheduledFixture(t, row.Key, row.TargetID, "2026-10-08", 100), scheduledFixture(t, row.Key, row.TargetID, "2026-10-08", 100)}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, db := range []*Store{data, other} {
		wg.Add(1)
		go func(db *Store, job Job) { defer wg.Done(); <-start; results <- db.CreateJob(job) }(db, jobs[i])
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrScheduleChanged) && !errors.Is(err, ErrOperationConflict) {
			t.Fatal(err)
		}
	}
	history, err := data.Jobs(ctx, row.TargetID, false)
	if err != nil || success != 1 || len(history) != 1 {
		t.Fatal("duplicate admission", success, history, err)
	}
	row, _, _ = data.ProjectSchedule(ctx, row.TargetID)
	if row.NextAt != 86500 || row.LastJobID != history[0].ID {
		t.Fatal("claim not committed with job", row)
	}
	for _, key := range []string{AutomaticResource, row.TargetID} {
		conflict, _ := NewJob(key, "engine", "stop", nil)
		if err := other.CreateJob(conflict); !errors.Is(err, ErrOperationConflict) {
			t.Fatal("missing atomic reservation", key, err)
		}
	}
}
func TestScheduleConflictRollbackSkipAndDisableFences(t *testing.T) {
	data := scheduleStore(t)
	ctx := t.Context()
	key := "compose:sample"
	data.SetProjectSchedule(ctx, key, key, true, 100)
	manual, _ := NewJob(key, "engine", "stop", nil)
	if err := data.CreateJob(manual); err != nil {
		t.Fatal(err)
	}
	auto := scheduledFixture(t, key, key, "2026-10-08", 100)
	if err := data.CreateJob(auto); !errors.Is(err, ErrOperationConflict) {
		t.Fatal(err)
	}
	row, _, _ := data.ProjectSchedule(ctx, key)
	if row.NextAt != 100 || row.LastJobID != "" {
		t.Fatal("failed admission consumed occurrence", row)
	}
	if err := data.SkipSchedule(ctx, row, ScheduleOccurrence{Key: key, DueAt: 100, Date: "2026-10-08", NextAt: 86500}, "Project is busy."); err != nil {
		t.Fatal(err)
	}
	busy, err := data.AutomaticBusy(ctx)
	if err != nil || busy {
		t.Fatal("skip reserved automatic worker", err)
	}
	row, _, _ = data.ProjectSchedule(ctx, key)
	if row.LastOutcome != "skipped" || row.NextAt != 86500 {
		t.Fatal(row)
	}
	data.FinishJob(manual.ID, manual.Owner, "succeeded", "completed", "", "")
	data.SetProjectSchedule(ctx, key, key, false, 0)
	if err := data.CreateJob(scheduledFixture(t, key, key, "2026-10-09", 86500)); !errors.Is(err, ErrScheduleChanged) {
		t.Fatal("disabled schedule admitted", err)
	}
	history, _ := data.Jobs(ctx, key, false)
	if len(history) != 2 {
		t.Fatal("rejected admission left ghost job", history)
	}
}
func TestScheduleFailedImagesManualRetryRetargetAndRemoval(t *testing.T) {
	data := scheduleStore(t)
	ctx := t.Context()
	key := "container:root"
	target := "container:old"
	data.SetProjectSchedule(ctx, key, target, true, 100)
	auto := scheduledFixture(t, key, target, "2026-10-08", 100)
	if err := data.CreateJob(auto); err != nil {
		t.Fatal(err)
	}
	images := []ImageIdentity{{ContainerID: "ephemeral", Service: "web", Platform: "linux/amd64", ImageID: "sha256:bad"}}
	if reason, err := data.UpdateCandidate(auto, images); err != nil || reason != "" {
		t.Fatal(reason, err)
	}
	if err := data.FinishJob(auto.ID, auto.Owner, "failed", "failed", "Unsafe image volume.", "not_needed"); err != nil {
		t.Fatal(err)
	}
	images[0].ContainerID = "different"
	next := scheduledFixture(t, key, target, "2026-10-09", 86500)
	if err := data.CreateJob(next); err != nil {
		t.Fatal(err)
	}
	if reason, err := data.UpdateCandidate(next, images); err != nil || !strings.Contains(reason, "previously failed") {
		t.Fatal("failed candidate not suppressed", reason, err)
	}
	if reason, err := data.UpdateCandidate(next, []ImageIdentity{{Service: "web", Platform: "linux/amd64", ImageID: "sha256:new"}}); err != nil || reason != "" {
		t.Fatal("new candidate suppressed", reason, err)
	}
	data.SetProjectSchedule(ctx, key, target, false, 0)
	if reason, err := data.UpdateCandidate(next, images); err != nil || !strings.Contains(reason, "disabled") {
		t.Fatal(reason, err)
	}
	data.FinishJob(next.ID, next.Owner, "succeeded", "skipped", "Disabled.", "")
	manual, _ := NewJob(target, "engine", "update", []string{key})
	data.CreateJob(manual)
	if reason, err := data.UpdateCandidate(manual, images); err != nil || reason != "" {
		t.Fatal("manual retry fenced", reason, err)
	}
	if err := data.RetargetJob(manual.ID, manual.Owner, "container:new", nil); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishJob(manual.ID, manual.Owner, "succeeded", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	row, _, _ := data.ProjectSchedule(ctx, key)
	if row.TargetID != "container:new" || row.FailedFingerprint != "" {
		t.Fatal("lineage or manual success lost", row)
	}
	data.SetProjectSchedule(ctx, key, row.TargetID, true, 200000)
	remove, _ := NewJob(row.TargetID, "engine", "remove", []string{key})
	data.CreateJob(remove)
	data.FinishJob(remove.ID, remove.Owner, "succeeded", "removed", "", "")
	row, _, _ = data.ProjectSchedule(ctx, key)
	if row.Enabled || row.NextAt != 0 {
		t.Fatal("removed project remains scheduled", row)
	}
	encoded, _ := json.Marshal(auto)
	if strings.Contains(string(encoded), "schedule_key") || strings.Contains(string(encoded), auto.Owner) || strings.Contains(string(encoded), key) {
		t.Fatal("private schedule metadata leaked", string(encoded))
	}
}
func TestScheduleSchemaSevenMigrationPreservesHistory(t *testing.T) {
	dir := t.TempDir()
	data, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := NewJob("compose:legacy", "engine", "pull", nil)
	data.CreateJob(job)
	data.FinishJob(job.ID, job.Owner, "succeeded", "cached", "", "")
	for _, statement := range []string{"DROP TABLE schedule_occurrences", "DROP TABLE project_schedules", "DROP INDEX scheduled_workers_cleanup", "ALTER TABLE operation_jobs DROP COLUMN schedule_key", "ALTER TABLE operation_jobs DROP COLUMN scheduled_for", "ALTER TABLE operation_jobs DROP COLUMN candidate_fingerprint", "PRAGMA user_version=7"} {
		if _, err := data.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	data.Close()
	data, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	got, found, err := data.Job(job.ID)
	if err != nil || !found || got.Outcome != "cached" || got.ScheduledFor != 0 {
		t.Fatal("legacy job changed", got, err)
	}
	row, found, err := data.ProjectSchedule(context.Background(), job.TargetID)
	if err != nil || found || row.Enabled {
		t.Fatal("migration opted in project", row, err)
	}
}

func TestScheduleTimesMigrateAndPersistIndependently(t *testing.T) {
	dir := t.TempDir()
	data, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	ctx := t.Context()
	if _, err := data.SetProjectSchedule(ctx, "compose:old", "compose:old", true, 100); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"ALTER TABLE project_schedules DROP COLUMN daily_time", "PRAGMA user_version=8"} {
		if _, err := data.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	data.Close()
	data, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	row, found, err := data.ProjectSchedule(ctx, "compose:old")
	if err != nil || !found || !row.Enabled || row.Time != "03:00" || row.NextAt != 100 {
		t.Fatal("migration changed existing schedule", row, err)
	}
	for _, value := range []struct {
		key, at string
		next    int64
	}{{"compose:old", "06:45", 200}, {"compose:other", "22:10", 300}} {
		row, err := data.SetProjectScheduleAt(ctx, value.key, value.key, true, value.next, value.at)
		if err != nil || row.Time != value.at || row.NextAt != value.next {
			t.Fatal("custom time was not saved", row, err)
		}
	}
	row, err = data.SetProjectScheduleAt(ctx, "compose:old", "compose:old", true, 999, "06:45")
	if err != nil || row.NextAt != 200 {
		t.Fatal("unchanged save moved next check", row, err)
	}
	if _, err := data.SetProjectScheduleAt(ctx, "compose:old", "compose:old", true, 400, "24:00"); err == nil {
		t.Fatal("invalid time accepted")
	}
	data.Close()
	data, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct {
		key, at string
		next    int64
	}{{"compose:old", "06:45", 200}, {"compose:other", "22:10", 300}} {
		row, _, err := data.ProjectSchedule(ctx, value.key)
		if err != nil || row.Time != value.at || row.NextAt != value.next {
			t.Fatal("restart lost custom time", row, err)
		}
	}
}

func TestAutomaticAdmissionWaitsForFinishedWorkerCleanupAtomically(t *testing.T) {
	data := scheduleStore(t)
	ctx := t.Context()
	first := "compose:a"
	second := "compose:b"
	for _, key := range []string{first, second} {
		data.SetProjectSchedule(ctx, key, key, true, 100)
	}
	job := scheduledFixture(t, first, first, "2026-10-08", 100)
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if err := data.SetJobWorker(job.ID, job.Owner, "finished-worker"); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishJob(job.ID, job.Owner, "succeeded", "unchanged", "", ""); err != nil {
		t.Fatal(err)
	}
	busy, err := data.AutomaticBusy(ctx)
	if err != nil || !busy {
		t.Fatal("finished worker omitted from bound", err)
	}
	next := scheduledFixture(t, second, second, "2026-10-08", 100)
	if err := data.CreateJob(next); !errors.Is(err, ErrOperationConflict) {
		t.Fatal("admission bypassed pending worker cleanup", err)
	}
	row, _, _ := data.ProjectSchedule(ctx, second)
	if row.LastJobID != "" || row.NextAt != 100 {
		t.Fatal("pending cleanup consumed occurrence", row)
	}
	if err := data.WorkerCleaned(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.CreateJob(next); err != nil {
		t.Fatal("cleaned worker still blocks", err)
	}
}
