package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestManagedFinalizationFencesLateMetadataAndRemoval(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	old, _ := NewJob("compose:sample", "managed", "sync", nil)
	old.ProjectName = "sample"
	if err := data.CreateJob(old); err != nil {
		t.Fatal(err)
	}
	first := ManagedProject{Name: "sample", YAML: "first", VariablesJSON: "{}"}
	if err := data.CommitManagedJob(old, &first, false, "verified"); err != nil {
		t.Fatal(err)
	}
	next, _ := NewJob(old.TargetID, "managed", "sync", nil)
	next.ProjectName = "sample"
	if err := data.CreateJob(next); err != nil {
		t.Fatal(err)
	}
	second := first
	second.YAML = "second"
	if err := data.CommitManagedJob(next, &second, false, "verified"); err != nil {
		t.Fatal(err)
	}
	for _, remove := range []bool{false, true} {
		if err := data.CommitManagedJob(old, &first, remove, "reconciled"); !errors.Is(err, ErrJobChanged) {
			t.Fatal("late observer changed metadata", err)
		}
	}
	current, found, err := data.ManagedProject("sample")
	if err != nil || !found || current.YAML != "second" {
		t.Fatalf("late finalization overwrote newer source: %+v %v", current, err)
	}
	job, _, _ := data.Job(old.ID)
	if job.Outcome != "verified" {
		t.Fatal("late finalization changed outcome", job)
	}
}

func TestJobOwnershipAcrossConnectionsAndRecovery(t *testing.T) {
	directory := t.TempDir()
	first, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	a, _ := NewJob("compose:sample", "managed", "update", []string{"container:target"})
	b, _ := NewJob("container:target", "engine", "restart", []string{"compose:sample"})
	stores := []*Store{first, second}
	candidates := []Job{a, b}
	results := make([]error, 2)
	var group sync.WaitGroup
	for i := range stores {
		group.Go(func() { results[i] = stores[i].CreateJob(candidates[i]) })
	}
	group.Wait()
	winner := 0
	if results[0] != nil {
		winner = 1
	}
	if results[winner] != nil || !errors.Is(results[1-winner], ErrOperationConflict) {
		t.Fatalf("overlapping ownership admitted: %v", results)
	}
	job := candidates[winner]
	if err := first.SetJobWorker(job.ID, job.Owner, "worker"); err != nil {
		t.Fatal(err)
	}
	if err := second.ClaimJob(job.ID, "wrong-owner"); !errors.Is(err, ErrJobChanged) {
		t.Fatal(err)
	}
	if err := first.ClaimJob(job.ID, job.Owner); err != nil {
		t.Fatal(err)
	}
	if err := second.ClaimJob(job.ID, job.Owner); !errors.Is(err, ErrJobChanged) {
		t.Fatalf("duplicate worker claimed execution: %v", err)
	}
	images := []ImageIdentity{{Service: "web", ImageID: "sha256:image", ContainerID: "target"}}
	if err := first.JobProgress(job.ID, job.Owner, "verifying", images, images); err != nil {
		t.Fatal(err)
	}
	if err := second.FinishJob(job.ID, job.Owner, "failed", "recovery_required", "Inspect the target.", "not_performed"); err != nil {
		t.Fatal(err)
	}
	current, found, err := first.Job(job.ID)
	if err != nil || !found || current.Stage != "awaiting_recovery" || len(current.TargetImages) != 1 {
		t.Fatalf("lost result: %+v %v", current, err)
	}
	retry, _ := NewJob("compose:sample", "managed", "update", nil)
	if err := first.CreateJob(retry); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("uncertain job released ownership: %v", err)
	}
	if err := first.AcknowledgeRecovery(job.ID, current.UpdatedAt-1); !errors.Is(err, ErrJobChanged) {
		t.Fatal(err)
	}
	if err := first.AcknowledgeRecovery(job.ID, current.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := first.CreateJob(retry); err != nil {
		t.Fatal(err)
	}
	if err := second.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); !errors.Is(err, ErrJobChanged) {
		t.Fatalf("late result overwrote recovery: %v", err)
	}
}

func TestJobHistorySurvivesReopenAndRedactsPayload(t *testing.T) {
	directory := t.TempDir()
	data, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := NewJob("compose:sample", "managed", "new", nil)
	job.Payload = "SECRET_ENV_VALUE"
	job.ProjectName = "sample"
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if err := data.FinishJob(job.ID, job.Owner, "succeeded", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	data.Close()
	data, err = Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	history, err := data.Jobs(context.Background(), "compose:sample", false)
	if err != nil || len(history) != 1 || history[0].Status != "succeeded" {
		t.Fatalf("history missing: %+v %v", history, err)
	}
	public, _ := json.Marshal(history)
	if strings.Contains(string(public), "SECRET_ENV_VALUE") || strings.Contains(string(public), job.Owner) {
		t.Fatalf("private job state exposed: %s", public)
	}
}

func TestOperationMigrationPreservesManagedAndSelfUpdateRecords(t *testing.T) {
	directory := t.TempDir()
	data, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`DROP TABLE operation_locks`, `DROP TABLE operation_jobs`,
		`INSERT INTO managed_jobs VALUES ('legacy','sample','update','running','',10,0)`,
		`INSERT INTO managed_jobs VALUES ('legacy-failed','sample','update','failed','private-legacy-secret',9,10)`,
		`INSERT INTO self_update_jobs VALUES ('old-self','updating','old-image','new-image','digest','',11,0)`,
		`PRAGMA user_version=6`,
	} {
		if _, err := data.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	data.Close()
	data, err = Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	managed, found, err := data.Job("legacy")
	if err != nil || !found || managed.Status != "running" || managed.Stage != "legacy_reconciliation" {
		t.Fatalf("legacy result falsely assumed: %+v %v", managed, err)
	}
	failed, found, err := data.Job("legacy-failed")
	if err != nil || !found || strings.Contains(failed.Error, "private-legacy-secret") {
		t.Fatalf("legacy diagnostic leaked: %+v %v", failed, err)
	}
	self, found, err := data.Job("old-self")
	if err != nil || !found || len(self.SourceImages) != 1 || self.SourceImages[0].ImageID != "old-image" {
		t.Fatalf("self-update lost: %+v %v", self, err)
	}
	retry, _ := NewJob("container:yard", "engine", "restart", []string{"yard:self"})
	if err := data.CreateJob(retry); !errors.Is(err, ErrOperationConflict) {
		t.Fatal("legacy self-update is not protected", err)
	}
}
