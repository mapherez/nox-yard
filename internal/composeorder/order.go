// Package composeorder orders observed Engine labels when Compose source is
// unavailable. It does not attempt to infer source-only relationships.
package composeorder

import (
	"fmt"
	"strings"
)

func Labels(labels []map[string]string) ([]int, error) {
	services := map[string][]int{}
	for i, l := range labels {
		services[l["com.docker.compose.service"]] = append(services[l["com.docker.compose.service"]], i)
	}
	visiting, visited := map[int]bool{}, map[int]bool{}
	order := []int{}
	var visit func(int) error
	visit = func(i int) error {
		if visiting[i] {
			return fmt.Errorf("cyclic observed Compose dependencies")
		}
		if visited[i] {
			return nil
		}
		visiting[i] = true
		for _, value := range strings.Split(labels[i]["com.docker.compose.depends_on"], ",") {
			if value == "" {
				continue
			}
			p := strings.Split(value, ":")
			if len(p) != 3 || (p[1] != "service_started" && p[1] != "service_healthy" && p[1] != "service_completed_successfully") || (p[2] != "true" && p[2] != "false") || len(services[p[0]]) == 0 {
				return fmt.Errorf("unsupported or missing observed Compose dependency")
			}
			for _, dep := range services[p[0]] {
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		visiting[i] = false
		visited[i] = true
		order = append(order, i)
		return nil
	}
	for i := range labels {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return order, nil
}
