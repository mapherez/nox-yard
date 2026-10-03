package httpapi

import (
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
)

// These types are the v1 wire contract, independent of internal read models.
type controlError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Result  any    `json:"result,omitempty"`
}

type controlAvailability struct {
	Available bool `json:"available"`
}
type controlHealth struct {
	Service    string              `json:"service"`
	Version    string              `json:"version"`
	APIVersion string              `json:"apiVersion"`
	Ready      bool                `json:"ready"`
	Storage    controlAvailability `json:"storage"`
	Code       string              `json:"code,omitempty"`
	Message    string              `json:"message,omitempty"`
}
type controlInfo struct {
	Service      string   `json:"service"`
	Version      string   `json:"version"`
	APIVersion   string   `json:"apiVersion"`
	APIEnabled   bool     `json:"apiEnabled"`
	Capabilities []string `json:"capabilities"`
}
type controlDocker struct {
	Available bool          `json:"available"`
	Error     *controlError `json:"error,omitempty"`
}
type controlCounts struct {
	CollectedAt       time.Time `json:"collectedAt"`
	Projects          int       `json:"projects"`
	Containers        int       `json:"containers"`
	RunningContainers int       `json:"runningContainers"`
}
type controlStatus struct {
	Service    string         `json:"service"`
	Version    string         `json:"version"`
	APIVersion string         `json:"apiVersion"`
	Docker     controlDocker  `json:"docker"`
	Inventory  *controlCounts `json:"inventory"`
}
type controlProjects struct {
	CollectedAt time.Time        `json:"collectedAt"`
	Projects    []controlProject `json:"projects"`
}
type controlProject struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Kind       string             `json:"kind"`
	State      string             `json:"state"`
	Health     string             `json:"health"`
	Operation  string             `json:"operation,omitempty"`
	Containers []controlContainer `json:"containers"`
}
type controlContainer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Service   string `json:"service,omitempty"`
	Image     string `json:"image"`
	State     string `json:"state"`
	Health    string `json:"health"`
	Operation string `json:"operation,omitempty"`
}
type controlInspection struct {
	ID       string           `json:"id"`
	Ports    []controlPort    `json:"ports"`
	Mounts   []controlMount   `json:"mounts"`
	Networks []controlNetwork `json:"networks"`
}
type controlPort struct {
	ContainerPort string `json:"containerPort"`
	HostIP        string `json:"hostIP,omitempty"`
	HostPort      string `json:"hostPort,omitempty"`
}
type controlMount struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}
type controlNetwork struct {
	Name string `json:"name"`
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
}
type controlFailure struct {
	Target  string `json:"target"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type controlActionResult struct {
	Action    string           `json:"action"`
	Succeeded int              `json:"succeeded"`
	Skipped   int              `json:"skipped"`
	Failed    int              `json:"failed"`
	Queued    int              `json:"queued"`
	Failures  []controlFailure `json:"failures"`
}
type controlPullResult struct {
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Images    []string         `json:"images"`
	Failures  []controlFailure `json:"failures"`
}

func controlEnum(value string, allowed ...string) string {
	for _, item := range allowed {
		if item == value {
			return value
		}
	}
	return "unknown"
}

func projectsDTO(snapshot inventory.Snapshot) controlProjects {
	result := controlProjects{CollectedAt: snapshot.CollectedAt.UTC(), Projects: make([]controlProject, 0, len(snapshot.Projects))}
	for _, project := range snapshot.Projects {
		item := controlProject{ID: project.ID, Name: project.Name, Kind: controlEnum(project.Kind, "managed-compose", "external-compose", "standalone"), State: controlEnum(project.State, "running", "stopped", "partial"), Health: controlEnum(project.Health, "none", "healthy", "unhealthy", "starting"), Operation: project.Operation, Containers: make([]controlContainer, 0, len(project.Containers))}
		for _, c := range project.Containers {
			item.Containers = append(item.Containers, controlContainer{ID: c.ID, Name: c.Name, Service: c.Service, Image: c.Image, State: controlEnum(c.State, "created", "running", "paused", "restarting", "removing", "exited", "dead"), Health: controlEnum(c.Health, "none", "healthy", "unhealthy", "starting"), Operation: c.Operation})
		}
		result.Projects = append(result.Projects, item)
	}
	return result
}

func inspectionDTO(detail inventory.ContainerInspection) controlInspection {
	result := controlInspection{ID: detail.ID, Ports: []controlPort{}, Mounts: []controlMount{}, Networks: []controlNetwork{}}
	for _, p := range detail.Ports {
		result.Ports = append(result.Ports, controlPort{ContainerPort: p.ContainerPort, HostIP: p.HostIP, HostPort: p.HostPort})
	}
	for _, m := range detail.Mounts {
		result.Mounts = append(result.Mounts, controlMount{Type: m.Type, Source: m.Source, Destination: m.Destination, ReadOnly: m.ReadOnly})
	}
	for _, n := range detail.Networks {
		result.Networks = append(result.Networks, controlNetwork{Name: n.Name, IPv4: n.IPv4, IPv6: n.IPv6})
	}
	return result
}

func failuresDTO(failures []lifecycle.Failure) []controlFailure {
	result := make([]controlFailure, 0, len(failures))
	for _, f := range failures {
		_, problem := classifyControlError(f.Cause, true)
		result = append(result, controlFailure{Target: f.Target, Code: problem.Code, Message: problem.Message})
	}
	return result
}
