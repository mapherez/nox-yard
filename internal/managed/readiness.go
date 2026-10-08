package managed

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

type runtimeReader func(context.Context, string) ([]container.InspectResponse, error)

// Reading the runtime is separate from inventory's cached metrics and public
// inspection DTOs. Raw configuration stays within managed operations.
func readProjectRuntime(ctx context.Context, name string) ([]container.InspectResponse, error) {
	cli, err := client.New(client.WithHost("unix:///var/run/docker.sock"))
	if err != nil {
		return nil, err
	}
	defer cli.Close()
	listed, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", "com.docker.compose.project="+name)})
	if err != nil {
		return nil, err
	}
	result := make([]container.InspectResponse, 0, len(listed.Items))
	for _, item := range listed.Items {
		if strings.EqualFold(item.Labels["com.docker.compose.oneoff"], "true") || item.Labels["com.docker.compose.hook"] != "" {
			continue
		}
		value, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, err
		}
		result = append(result, value.Container)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

type serviceExpectation struct {
	replicas int
	oneshot  bool
}

func expectedServices(model composeModel) map[string]serviceExpectation {
	result := map[string]serviceExpectation{}
	oneshot := map[string]bool{}
	for _, service := range model.Services {
		for name, dependency := range service.DependsOn {
			if dependency.Condition == "service_completed_successfully" {
				oneshot[name] = true
			}
		}
	}
	for name, service := range model.Services {
		if len(service.Profiles) > 0 {
			continue
		}
		count := 1
		if service.Scale != nil {
			count = *service.Scale
		}
		if service.Deploy.Replicas != nil {
			count = *service.Deploy.Replicas
		}
		if count <= 0 {
			continue
		}
		result[name] = serviceExpectation{count, oneshot[name] || service.Labels["nox-yard.lifecycle"] == "oneshot"}
	}
	return result
}

func verifyReadiness(ctx context.Context, name string, model composeModel, read runtimeReader, stableFor, poll time.Duration) error {
	expected := expectedServices(model)
	if len(expected) == 0 {
		return fmt.Errorf("no active services to verify")
	}
	stable := map[string]time.Time{}
	identity := map[string]string{}
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	sort.Strings(names)
	lastReason := "services have not appeared"
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("verification did not complete: %s", lastReason)
		}
		states, err := read(ctx, name)
		if err != nil {
			return fmt.Errorf("Docker runtime could not be inspected during verification")
		}
		grouped := map[string][]container.InspectResponse{}
		for _, item := range states {
			if item.Config != nil {
				grouped[item.Config.Labels["com.docker.compose.service"]] = append(grouped[item.Config.Labels["com.docker.compose.service"]], item)
			}
		}
		ready := true
		now := time.Now()
		seen := map[string]bool{}
		for _, service := range names {
			want := expected[service]
			items := grouped[service]
			if len(items) != want.replicas {
				ready = false
				lastReason = fmt.Sprintf("service %s has %d of %d expected containers", service, len(items), want.replicas)
			}
			for _, item := range items {
				seen[item.ID] = true
				s := item.State
				if s == nil {
					return fmt.Errorf("service %s has no runtime state", service)
				}
				if s.OOMKilled || s.Dead || s.Status == "dead" {
					return fmt.Errorf("service %s failed", service)
				}
				if s.Status == "exited" {
					if want.oneshot && s.ExitCode == 0 {
						continue
					}
					return fmt.Errorf("service %s exited unexpectedly (code %d)", service, s.ExitCode)
				}
				if s.Health != nil && s.Health.Status == container.Unhealthy {
					return fmt.Errorf("service %s is unhealthy", service)
				}
				hasHealthcheck := item.Config.Healthcheck != nil && len(item.Config.Healthcheck.Test) > 0 && item.Config.Healthcheck.Test[0] != "NONE"
				isReady := !want.oneshot && s.Running && !s.Paused && !s.Restarting &&
					((!hasHealthcheck && s.Health == nil) || (s.Health != nil && s.Health.Status == container.Healthy))
				if !isReady {
					ready = false
					delete(stable, item.ID)
					lastReason = fmt.Sprintf("service %s is not ready", service)
					continue
				}
				currentIdentity := fmt.Sprintf("%s|%d", s.StartedAt, item.RestartCount)
				if identity[item.ID] != currentIdentity || stable[item.ID].IsZero() {
					identity[item.ID] = currentIdentity
					stable[item.ID] = now
				}
				if now.Sub(stable[item.ID]) < stableFor {
					ready = false
					lastReason = fmt.Sprintf("service %s is still stabilizing", service)
				}
			}
		}
		for id := range stable {
			if !seen[id] {
				delete(stable, id)
				delete(identity, id)
			}
		}
		if ready {
			return nil
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("verification did not complete: %s", lastReason)
		case <-timer.C:
		}
	}
}

func (m *Manager) verify(ctx context.Context, name string, preview Preview) error {
	verification, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return verifyReadiness(verification, name, preview.model, m.runtime, 5*time.Second, 500*time.Millisecond)
}
