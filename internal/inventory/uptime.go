package inventory

import (
	"context"
	"sync"
	"time"

	"github.com/moby/moby/client"
)

type uptimeBatch struct {
	generation uint64
	ids        []string
}

// Only unknown start times are inspected. The two-worker limit and deadlines
// keep cold-start backfill bounded; normal inventory/metrics reads use the cache.
func (r *DockerReader) collectUptimes(ctx context.Context, batch uptimeBatch, notify func(Change)) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	jobs := make(chan string)
	var workers sync.WaitGroup
	for range min(2, len(batch.ids)) {
		workers.Go(func() {
			for id := range jobs {
				r.mu.RLock()
				_, known := r.started[id]
				current := batch.generation == r.generation
				r.mu.RUnlock()
				if known || !current || ctx.Err() != nil {
					continue
				}
				if r.readUptime(ctx, id, batch.generation) {
					notify(Change{Metrics: true})
				}
			}
		})
	}
	for _, id := range batch.ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
}

func (r *DockerReader) readUptime(ctx context.Context, id string, generation uint64) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err := r.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil || result.Container.State == nil || !result.Container.State.Running {
		return false
	}
	started, err := time.Parse(time.RFC3339Nano, result.Container.State.StartedAt)
	if err != nil || started.IsZero() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.generation {
		return false
	}
	if r.started == nil {
		r.started = make(map[string]time.Time)
	}
	if _, known := r.started[id]; known {
		return false
	}
	r.started[id] = started
	return true
}
