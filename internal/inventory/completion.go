package inventory

import (
	"strings"

	"github.com/moby/moby/api/types/container"
)

// Compose stores each deployed service's dependency conditions in Docker's
// list labels. Use these rather than guessing from names or reading host files
// on every inventory request. Explicit oneshot labels match managed readiness.
func completedInitializers(items []container.Summary) map[string]bool {
	oneshots := map[string]map[string]bool{}
	for _, item := range items {
		project := item.Labels[composeProjectLabel]
		if project == "" || !lifecycleContainer(item) {
			continue
		}
		for _, dependency := range strings.Split(item.Labels["com.docker.compose.depends_on"], ",") {
			parts := strings.SplitN(dependency, ":", 3)
			if len(parts) < 2 || parts[0] == "" || parts[1] != "service_completed_successfully" {
				continue
			}
			if oneshots[project] == nil {
				oneshots[project] = map[string]bool{}
			}
			oneshots[project][parts[0]] = true
		}
	}
	completed := map[string]bool{}
	for _, item := range items {
		if item.State != "exited" || !lifecycleContainer(item) {
			continue
		}
		if item.Labels["nox-yard.lifecycle"] != "oneshot" && !oneshots[item.Labels[composeProjectLabel]][item.Labels[composeServiceLabel]] {
			continue
		}
		// ContainerList has no numeric exit-code field. Docker's status includes
		// it; only an explicit zero confirms completion. Unknown status remains
		// stopped/partial instead of adding blocking inspect calls to inventory.
		completed[item.ID] = item.Status == "Exited (0)" || strings.HasPrefix(item.Status, "Exited (0) ")
	}
	return completed
}

func lifecycleContainer(item container.Summary) bool {
	role := item.Labels["nox-yard.role"]
	return role != "managed-helper" && role != "operation-worker" &&
		!strings.EqualFold(item.Labels["com.docker.compose.oneoff"], "true") && item.Labels["com.docker.compose.hook"] == ""
}
