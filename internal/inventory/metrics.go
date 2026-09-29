package inventory

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const metricsLifetime = 30 * time.Second

type Metrics struct {
	CPUPercent     *float64  `json:"cpuPercent"`
	MemoryBytes    *uint64   `json:"memoryBytes"`
	NetworkRxBytes *uint64   `json:"networkRxBytes"`
	NetworkTxBytes *uint64   `json:"networkTxBytes"`
	UptimeSeconds  *int64    `json:"uptimeSeconds"`
	CollectedAt    time.Time `json:"collectedAt"`
}

func applyMetrics(item *Container, sample Metrics) {
	item.CPUPercent, item.MemoryBytes = sample.CPUPercent, sample.MemoryBytes
	item.NetworkRxBytes, item.NetworkTxBytes = sample.NetworkRxBytes, sample.NetworkTxBytes
	item.UptimeSeconds = sample.UptimeSeconds
}

// CachedMetrics never performs Docker I/O or waits for an in-flight sample.
func (r *DockerReader) CachedMetrics() map[string]Metrics {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]Metrics, len(r.metrics))
	now := time.Now()
	for id, sample := range r.metrics {
		if now.Sub(sample.CollectedAt) > metricsLifetime {
			sample = Metrics{}
		}
		result[id] = sample
	}
	for id, started := range r.started {
		sample := result[id]
		seconds := max(int64(0), int64(now.Sub(started).Seconds()))
		sample.UptimeSeconds = &seconds
		result[id] = sample
	}
	return result
}

func (r *DockerReader) collectMetrics(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r.mu.RLock()
	generation := r.generation
	r.mu.RUnlock()
	listed, err := r.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return
	}
	live := make(map[string]bool)
	for _, item := range listed.Items {
		if item.State == "running" {
			live[item.ID] = true
		}
	}
	r.mu.Lock()
	if r.metrics == nil {
		r.metrics = make(map[string]Metrics)
	}
	if generation == r.generation {
		for id := range r.metrics {
			if !live[id] {
				delete(r.metrics, id)
			}
		}
		for id := range r.started {
			if !live[id] {
				delete(r.started, id)
			}
		}
		for id, entry := range r.shells {
			if time.Now().After(entry.expires) {
				delete(r.shells, id)
			}
		}
	}
	r.mu.Unlock()
	jobs := make(chan string)
	var workers sync.WaitGroup
	for range min(maxWorkers, len(live)) {
		workers.Go(func() {
			for id := range jobs {
				sample, err := r.readMetrics(ctx, id)
				if err != nil {
					continue
				}
				r.mu.Lock()
				// A stop/restart/destroy received during sampling invalidates the old data.
				if generation == r.generation {
					r.metrics[id] = sample
				}
				r.mu.Unlock()
			}
		})
	}
	for id := range live {
		select {
		case jobs <- id:
		case <-ctx.Done():
		}
	}
	close(jobs)
	workers.Wait()
}

func (r *DockerReader) readMetrics(ctx context.Context, id string) (Metrics, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	stats, err := r.client.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		return Metrics{}, err
	}
	defer stats.Body.Close()
	var sample container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&sample); err != nil {
		return Metrics{}, err
	}
	item := Metrics{CollectedAt: time.Now().UTC()}
	if sample.CPUStats.CPUUsage.TotalUsage >= sample.PreCPUStats.CPUUsage.TotalUsage && sample.CPUStats.SystemUsage > sample.PreCPUStats.SystemUsage {
		cpuDelta := sample.CPUStats.CPUUsage.TotalUsage - sample.PreCPUStats.CPUUsage.TotalUsage
		systemDelta := sample.CPUStats.SystemUsage - sample.PreCPUStats.SystemUsage
		cores := sample.CPUStats.OnlineCPUs
		if cores == 0 {
			cores = uint32(len(sample.CPUStats.CPUUsage.PercpuUsage))
		}
		if cores > 0 {
			value := float64(cpuDelta) / float64(systemDelta) * float64(cores) * 100
			item.CPUPercent = &value
		}
	}
	usage := sample.MemoryStats.Usage
	if inactive, ok := sample.MemoryStats.Stats["inactive_file"]; ok && inactive <= usage {
		usage -= inactive
	} else if inactive, ok := sample.MemoryStats.Stats["total_inactive_file"]; ok && inactive <= usage {
		usage -= inactive
	}
	item.MemoryBytes = &usage
	var rx, tx uint64
	for _, n := range sample.Networks {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	item.NetworkRxBytes, item.NetworkTxBytes = &rx, &tx
	return item, nil
}
