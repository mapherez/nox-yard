package inventory

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const (
	composeProjectLabel = "com.docker.compose.project"
	composeServiceLabel = "com.docker.compose.service"
	maxWorkers          = 6
)

// Reader keeps Docker SDK details out of the HTTP layer.
type Reader interface {
	Snapshot(context.Context) (Snapshot, error)
}

type Snapshot struct {
	CollectedAt time.Time `json:"collectedAt"`
	Projects    []Project `json:"projects"`
}

type Project struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Kind           string      `json:"kind"`
	State          string      `json:"state"`
	Health         string      `json:"health"`
	CPUPercent     *float64    `json:"cpuPercent"`
	MemoryBytes    *uint64     `json:"memoryBytes"`
	NetworkRxBytes *uint64     `json:"networkRxBytes"`
	NetworkTxBytes *uint64     `json:"networkTxBytes"`
	UptimeSeconds  *int64      `json:"uptimeSeconds"`
	Containers     []Container `json:"containers"`
}

type Container struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Service        string   `json:"service,omitempty"`
	Image          string   `json:"image"`
	State          string   `json:"state"`
	Health         string   `json:"health"`
	CPUPercent     *float64 `json:"cpuPercent"`
	MemoryBytes    *uint64  `json:"memoryBytes"`
	NetworkRxBytes *uint64  `json:"networkRxBytes"`
	NetworkTxBytes *uint64  `json:"networkTxBytes"`
	UptimeSeconds  *int64   `json:"uptimeSeconds"`
}

type DockerReader struct {
	client *client.Client
}

func NewDockerReader() (*DockerReader, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return nil, err
	}
	return &DockerReader{client: cli}, nil
}

func (r *DockerReader) Close() error {
	return r.client.Close()
}

func (r *DockerReader) Snapshot(ctx context.Context) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	listed, err := r.client.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return Snapshot{}, err
	}
	collectedAt := time.Now().UTC()
	containers := make([]Container, len(listed.Items))
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(maxWorkers, len(listed.Items)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				containers[index] = r.describe(ctx, listed.Items[index], collectedAt)
			}
		}()
	}
	for index := range listed.Items {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return group(listed.Items, containers, collectedAt), nil
}

func group(items []container.Summary, containers []Container, collectedAt time.Time) Snapshot {
	projects := make(map[string]*Project)
	for index, summary := range items {
		item := containers[index]
		name := strings.TrimSpace(summary.Labels[composeProjectLabel])
		key, kind := "container:"+item.ID, "standalone"
		if name != "" {
			key, kind = "compose:"+name, "external-compose"
		} else {
			name = item.Name
		}
		project := projects[key]
		if project == nil {
			project = &Project{ID: key, Name: name, Kind: kind, Containers: []Container{}}
			projects[key] = project
		}
		project.Containers = append(project.Containers, item)
	}

	result := Snapshot{CollectedAt: collectedAt, Projects: make([]Project, 0, len(projects))}
	for _, project := range projects {
		sort.Slice(project.Containers, func(i, j int) bool {
			return project.Containers[i].Name < project.Containers[j].Name
		})
		summarize(project)
		result.Projects = append(result.Projects, *project)
	}
	sort.Slice(result.Projects, func(i, j int) bool {
		if result.Projects[i].Kind != result.Projects[j].Kind {
			return result.Projects[i].Kind == "external-compose"
		}
		return strings.ToLower(result.Projects[i].Name) < strings.ToLower(result.Projects[j].Name)
	})
	return result
}

func (r *DockerReader) describe(ctx context.Context, summary container.Summary, now time.Time) Container {
	name := strings.TrimPrefix(firstName(summary.Names), "/")
	if name == "" {
		name = summary.ID[:min(12, len(summary.ID))]
	}
	item := Container{
		ID: summary.ID, Name: name, Service: summary.Labels[composeServiceLabel],
		Image: summary.Image, State: string(summary.State), Health: "none",
	}
	if summary.Health != nil {
		item.Health = string(summary.Health.Status)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	inspected, err := r.client.ContainerInspect(requestCtx, summary.ID, client.ContainerInspectOptions{})
	if err == nil && inspected.Container.State != nil {
		state := inspected.Container.State
		if state.Health != nil {
			item.Health = string(state.Health.Status)
		}
		if state.Running {
			if started, parseErr := time.Parse(time.RFC3339Nano, state.StartedAt); parseErr == nil {
				seconds := max(int64(0), int64(now.Sub(started).Seconds()))
				item.UptimeSeconds = &seconds
			}
		}
	}
	if item.State != "running" || requestCtx.Err() != nil {
		return item
	}
	stats, err := r.client.ContainerStats(requestCtx, summary.ID, client.ContainerStatsOptions{
		Stream: false, IncludePreviousSample: true,
	})
	if err != nil {
		return item
	}
	defer stats.Body.Close()
	var sample container.StatsResponse
	if json.NewDecoder(stats.Body).Decode(&sample) != nil {
		return item
	}
	if sample.CPUStats.CPUUsage.TotalUsage >= sample.PreCPUStats.CPUUsage.TotalUsage &&
		sample.CPUStats.SystemUsage > sample.PreCPUStats.SystemUsage {
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
	for _, network := range sample.Networks {
		rx += network.RxBytes
		tx += network.TxBytes
	}
	item.NetworkRxBytes, item.NetworkTxBytes = &rx, &tx
	return item
}

func summarize(project *Project) {
	running := 0
	transitional := false
	health := "none"
	var cpu float64
	var memory uint64
	var networkRx, networkTx uint64
	metricsComplete := true
	for _, item := range project.Containers {
		if item.State == "running" {
			running++
			if item.UptimeSeconds != nil && (project.UptimeSeconds == nil || *item.UptimeSeconds < *project.UptimeSeconds) {
				project.UptimeSeconds = item.UptimeSeconds
			}
		}
		if item.State != "running" && item.State != "created" && item.State != "exited" && item.State != "dead" {
			transitional = true
		}
		switch item.Health {
		case "unhealthy":
			health = "unhealthy"
		case "starting":
			if health != "unhealthy" {
				health = "starting"
			}
		case "healthy":
			if health == "none" {
				health = "healthy"
			}
		}
		if item.CPUPercent != nil {
			cpu += *item.CPUPercent
		}
		if item.MemoryBytes != nil {
			memory += *item.MemoryBytes
		}
		if item.NetworkRxBytes != nil && item.NetworkTxBytes != nil {
			networkRx += *item.NetworkRxBytes
			networkTx += *item.NetworkTxBytes
		}
		if item.State == "running" && (item.CPUPercent == nil || item.MemoryBytes == nil || item.NetworkRxBytes == nil || item.NetworkTxBytes == nil) {
			metricsComplete = false
		}
	}
	project.Health = health
	if running == len(project.Containers) {
		project.State = "running"
	} else if running == 0 && !transitional {
		project.State = "stopped"
	} else {
		project.State = "partial"
	}
	if running > 0 && metricsComplete {
		project.CPUPercent = &cpu
		project.MemoryBytes = &memory
		project.NetworkRxBytes = &networkRx
		project.NetworkTxBytes = &networkTx
	}
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

var _ Reader = (*DockerReader)(nil)
