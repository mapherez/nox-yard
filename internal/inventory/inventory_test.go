package inventory

import (
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
)

func TestGroupComposeAndStandalone(t *testing.T) {
	items := []container.Summary{
		{Labels: map[string]string{composeProjectLabel: "media", composeServiceLabel: "api"}},
		{Labels: nil},
		{Labels: map[string]string{composeProjectLabel: "media", composeServiceLabel: "db"}},
	}
	cpuA, cpuB := 4.5, 2.5
	memoryA, memoryB := uint64(1024), uint64(2048)
	rx, tx := uint64(10), uint64(20)
	uptimeA, uptimeB := int64(300), int64(100)
	descriptions := []Container{
		{ID: "a", Name: "media-api", State: "running", Health: "healthy", CPUPercent: &cpuA, MemoryBytes: &memoryA, NetworkRxBytes: &rx, NetworkTxBytes: &tx, UptimeSeconds: &uptimeA},
		{ID: "standalone", Name: "redis", State: "exited", Health: "none"},
		{ID: "b", Name: "media-db", State: "running", Health: "unhealthy", CPUPercent: &cpuB, MemoryBytes: &memoryB, NetworkRxBytes: &rx, NetworkTxBytes: &tx, UptimeSeconds: &uptimeB},
	}
	snapshot := group(items, descriptions, time.Unix(1000, 0))
	if len(snapshot.Projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(snapshot.Projects))
	}
	compose := snapshot.Projects[0]
	if compose.ID != "compose:media" || compose.Kind != "external-compose" || len(compose.Containers) != 2 {
		t.Fatalf("unexpected Compose project: %+v", compose)
	}
	if compose.State != "running" || compose.Health != "unhealthy" {
		t.Fatalf("unexpected Compose state: %s, %s", compose.State, compose.Health)
	}
	if compose.CPUPercent == nil || *compose.CPUPercent != 7 || compose.MemoryBytes == nil || *compose.MemoryBytes != 3072 {
		t.Fatalf("incorrect aggregate resource usage: %+v", compose)
	}
	if compose.NetworkRxBytes == nil || *compose.NetworkRxBytes != 20 || compose.UptimeSeconds == nil || *compose.UptimeSeconds != 100 {
		t.Fatalf("incorrect network or uptime summary: %+v", compose)
	}
	standalone := snapshot.Projects[1]
	if standalone.ID != "container:standalone" || standalone.Kind != "standalone" || standalone.State != "stopped" {
		t.Fatalf("unexpected standalone project: %+v", standalone)
	}
}

func TestGroupDoesNotReportIncompleteMetricsAsTotals(t *testing.T) {
	items := []container.Summary{
		{Labels: map[string]string{composeProjectLabel: "apps"}},
		{Labels: map[string]string{composeProjectLabel: "apps"}},
	}
	cpu := 1.0
	memory, network := uint64(100), uint64(0)
	descriptions := []Container{
		{ID: "a", State: "running", CPUPercent: &cpu, MemoryBytes: &memory, NetworkRxBytes: &network, NetworkTxBytes: &network},
		{ID: "b", State: "running"},
	}
	project := group(items, descriptions, time.Now()).Projects[0]
	if project.CPUPercent != nil || project.MemoryBytes != nil || project.NetworkRxBytes != nil {
		t.Fatalf("partial metrics should not be reported as complete totals: %+v", project)
	}
}
