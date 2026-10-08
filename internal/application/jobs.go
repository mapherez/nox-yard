package application

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

func (s *Service) Job(ctx context.Context, id string) (store.Job, bool, error) {
	if err := ctx.Err(); err != nil {
		return store.Job{}, false, err
	}
	return s.Store.Job(id)
}
func (s *Service) JobHistory(ctx context.Context, target string) ([]store.Job, error) {
	if !ValidTarget(target, false) {
		return nil, ErrInvalidRemoval
	}
	return s.Store.Jobs(ctx, target, false)
}
func (s *Service) AcknowledgeRecovery(ctx context.Context, id string, updated int64, confirm bool) (store.Job, error) {
	if !confirm {
		return store.Job{}, ErrInvalidRemoval
	}
	if s.JobObserver == nil {
		return store.Job{}, ErrUnavailable
	}
	if err := s.JobObserver.Acknowledge(ctx, id, updated); err != nil {
		return store.Job{}, err
	}
	s.Changes.Notify(inventory.Change{Inventory: true})
	job, _, err := s.Store.Job(id)
	return job, err
}

func (s *Service) beginOperation(ctx context.Context, target, operation string) (store.Job, error) {
	resources := []string{target}
	if s.ResourceKeys != nil {
		var err error
		resources, err = s.ResourceKeys(ctx, target)
		if err != nil {
			return store.Job{}, err
		}
	}
	job, err := store.NewJob(target, "engine", operation, resources)
	if err != nil {
		return job, err
	}
	if s.ResourceImages != nil {
		job.SourceImages, err = s.ResourceImages(ctx, resources, false)
		if err != nil {
			return store.Job{}, err
		}
	}
	payload, _ := json.Marshal(map[string]string{"process": s.instance})
	job.Payload = string(payload)
	if err := s.Store.CreateJob(job); err != nil {
		return store.Job{}, err
	}
	if err := s.Store.JobProgress(job.ID, job.Owner, "executing", nil, nil); err != nil {
		return store.Job{}, err
	}
	return job, nil
}
func (s *Service) finishOperation(job store.Job, failed bool, err error) {
	status, outcome, message := "succeeded", "verified", ""
	if failed || err != nil {
		status, outcome, message = "failed", "failed", "Docker could not complete every requested action. Inspect the target before retrying."
	}
	var timeout net.Error
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		outcome = "recovery_required"
		message = "Operation response was interrupted. Inspect the target and retained resources, then acknowledge recovery before retrying."
	}
	if s.ResourceImages != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		images, imageErr := s.ResourceImages(ctx, job.Resources, job.Operation == "pull")
		cancel()
		if imageErr == nil {
			_ = s.Store.JobProgress(job.ID, job.Owner, "verifying", nil, images)
		}
	}
	_ = s.Store.FinishJob(job.ID, job.Owner, status, outcome, message, "")
	s.Changes.Notify(inventory.Change{Inventory: true})
}

// Interrupted synchronous Engine requests are never replayed. They keep their
// resource locks until a host review acknowledges the uncertain outcome.
func (s *Service) ReconcileDirectJobs(ctx context.Context) error {
	current, err := s.Store.Jobs(ctx, "", true)
	if err != nil {
		return err
	}
	for _, job := range current {
		if job.Domain != "engine" || job.Status != "running" || job.WorkerID != "" {
			continue
		}
		payload := map[string]string{}
		_ = json.Unmarshal([]byte(job.Payload), &payload)
		if payload["process"] == s.instance && time.Now().Unix() < job.DeadlineAt {
			continue
		}
		if err := s.Store.FinishJob(job.ID, job.Owner, "failed", "recovery_required", "Web process exited before recording the Engine result. Inspect the target and acknowledge recovery before retrying. No action was replayed.", ""); err != nil {
			return err
		}
	}
	return nil
}
