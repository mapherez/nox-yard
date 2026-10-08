package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/store"
)

// A final result can retain a resource cleanup issue requiring host review.
var ErrCleanupReview = errors.New("resource cleanup requires host review")

type Observer struct {
	Data         *store.Store
	Changes      *inventory.Notifier
	Verify       func(context.Context, store.Job) error
	CleanupFinal func(context.Context, store.Job) error
}

func (o *Observer) Start(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		previous := ""
		for ctx.Err() == nil {
			o.Reconcile(ctx)
			jobs, err := o.Data.Jobs(ctx, "", true)
			if err == nil {
				current := ""
				for _, job := range jobs {
					current += fmt.Sprintf("%s:%s:%s:%d;", job.ID, job.Status, job.Stage, job.UpdatedAt)
				}
				if current != previous {
					o.Changes.Notify(inventory.Change{Inventory: true})
					previous = current
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (o *Observer) Reconcile(ctx context.Context) {
	current, err := o.Data.Jobs(ctx, "", true)
	if err != nil {
		return
	}
	for _, job := range current {
		if ctx.Err() != nil {
			return
		}
		if job.Domain == "self-update" {
			continue
		} // Dedicated snapshot/rollback protocol owns its outcome.
		if job.Domain == "engine" && job.WorkerID == "" {
			// Direct Engine calls belong to the current web process until its cancellation
			// or deadline. Startup marks only jobs owned by a previous process uncertain.
			continue
		}
		observed, cancel := context.WithTimeout(ctx, 15*time.Second)
		_ = Observe(observed, o.Data, job, o.Verify)
		cancel()
	}
	// Final workers are retained until their result is durable and they exit.
	recent, err := o.Data.WorkersForCleanup(ctx)
	if err != nil {
		return
	}
	for _, job := range recent {
		if job.Status == "running" || job.Domain == "self-update" || job.WorkerID == "" {
			continue
		}
		cleanup, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := Cleanup(cleanup, job); err != nil {
			if time.Now().Unix()-job.CompletedAt > 30 {
				_ = o.Data.JobCleanupError(job.ID, "Operation result was saved, but worker cleanup is pending; inspect the worker on the host.")
			}
		} else if o.CleanupFinal != nil {
			if err := o.CleanupFinal(cleanup, job); errors.Is(err, ErrCleanupReview) {
				_ = o.Data.WorkerCleaned(job.ID, false)
			} else if err != nil {
				_ = o.Data.JobCleanupError(job.ID, "Operation result was saved, but retained snapshot/image cleanup is pending; inspect the job's rollback image references on the host.")
			} else {
				_ = o.Data.WorkerCleaned(job.ID)
			}
		} else {
			_ = o.Data.WorkerCleaned(job.ID)
		}
		cancel()
	}
}

func (o *Observer) Acknowledge(ctx context.Context, id string, updated int64) error {
	job, found, err := o.Data.Job(id)
	if err != nil {
		return err
	}
	if !found || job.Outcome != "recovery_required" {
		return store.ErrJobChanged
	}
	state, err := State(ctx, job)
	if err != nil {
		return err
	}
	if state.Running || state.HelpersRunning {
		return store.ErrOperationConflict
	}
	if err := o.Data.AcknowledgeRecovery(id, updated); err != nil {
		return err
	}
	if o.CleanupFinal != nil {
		job.Outcome = "recovery_acknowledged"
		if err := o.CleanupFinal(ctx, job); err != nil {
			_ = o.Data.JobCleanupError(job.ID, "Recovery acknowledged; retained snapshot/image cleanup is pending on the host.")
		}
	}
	return nil
}

func TargetResources(ctx context.Context, target string) ([]string, error) {
	// Fresh Docker labels, rather than a cached inventory snapshot, identify the
	// Compose lock for a container-level action during a project replacement.
	return targetResources(ctx, target)
}
