package managed

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
)

func readyContainer(service string) container.InspectResponse {
	return container.InspectResponse{ID: service + "-id", Config: &container.Config{Labels: map[string]string{"com.docker.compose.service": service}}, State: &container.State{Status: "running", Running: true, StartedAt: "first"}}
}

func TestReadinessHealthAndFailureOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*container.InspectResponse)
		want   string
	}{
		{"running without health", func(*container.InspectResponse) {}, ""},
		{"healthy", func(c *container.InspectResponse) { c.State.Health = &container.Health{Status: container.Healthy} }, ""},
		{"unhealthy", func(c *container.InspectResponse) { c.State.Health = &container.Health{Status: container.Unhealthy} }, "unhealthy"},
		{"health starting", func(c *container.InspectResponse) { c.State.Health = &container.Health{Status: container.Starting} }, "did not complete"},
		{"missing health result", func(c *container.InspectResponse) {
			c.Config.Healthcheck = &container.HealthConfig{Test: []string{"CMD", "true"}}
		}, "did not complete"},
		{"disabled health", func(c *container.InspectResponse) {
			c.Config.Healthcheck = &container.HealthConfig{Test: []string{"NONE"}}
		}, ""},
		{"unexpected exit zero", func(c *container.InspectResponse) { c.State = &container.State{Status: "exited"} }, "exited unexpectedly"},
		{"out of memory", func(c *container.InspectResponse) { c.State.OOMKilled = true }, "failed"},
		{"dead", func(c *container.InspectResponse) { c.State.Dead = true }, "failed"},
		{"paused", func(c *container.InspectResponse) { c.State.Paused = true }, "did not complete"},
		{"restarting", func(c *container.InspectResponse) { c.State.Restarting = true }, "did not complete"},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := readyContainer("web")
			test.change(&item)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			err := verifyReadiness(ctx, "sample", composeModel{Services: map[string]modelService{"web": {}}}, func(context.Context, string) ([]container.InspectResponse, error) {
				return []container.InspectResponse{item}, nil
			}, 0, time.Millisecond)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %s", err, test.want)
			}
		})
	}
}

func TestReadinessRequiresStableRuntimeAndEveryReplica(t *testing.T) {
	model := composeModel{Services: map[string]modelService{"web": {Scale: new(2)}}}
	first, second := readyContainer("web"), readyContainer("web")
	second.ID = "second"
	calls := 0
	var restartedAt time.Time
	err := verifyReadiness(t.Context(), "sample", model, func(context.Context, string) ([]container.InspectResponse, error) {
		calls++
		if calls == 1 {
			return []container.InspectResponse{first}, nil
		}
		if calls == 3 {
			restartedAt = time.Now()
			second.RestartCount++
			second.State.StartedAt = "restarted"
		}
		return []container.InspectResponse{first, second}, nil
	}, 8*time.Millisecond, 2*time.Millisecond)
	if err != nil || calls < 4 || restartedAt.IsZero() || time.Since(restartedAt) < 8*time.Millisecond {
		t.Fatalf("unstable or incomplete replica set passed: calls=%d err=%v", calls, err)
	}
}

func TestReadinessOneShotProfilesAndZeroReplicas(t *testing.T) {
	model := composeModel{Services: map[string]modelService{
		"web": {DependsOn: map[string]struct {
			Condition string `json:"condition"`
		}{"migrate": {Condition: "service_completed_successfully"}}},
		"migrate": {}, "task": {Labels: map[string]string{"nox-yard.lifecycle": "oneshot"}},
		"optional": {Profiles: []string{"optional"}}, "disabled": {Scale: new(0)},
	}}
	items := []container.InspectResponse{readyContainer("web"), readyContainer("migrate"), readyContainer("task")}
	items[1].State = &container.State{Status: "exited"}
	items[2].State = &container.State{Status: "exited"}
	read := func(context.Context, string) ([]container.InspectResponse, error) { return items, nil }
	if err := verifyReadiness(t.Context(), "sample", model, read, 0, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	items[2].State.ExitCode = 1
	if err := verifyReadiness(t.Context(), "sample", model, read, 0, time.Millisecond); err == nil {
		t.Fatal("failed one-shot accepted")
	}
	items[2] = readyContainer("task")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := verifyReadiness(ctx, "sample", model, read, 0, time.Millisecond); err == nil {
		t.Fatal("still-running one-shot accepted")
	}
}

func TestReadinessMissingAndDockerFailureDoNotLeakDetails(t *testing.T) {
	model := composeModel{Services: map[string]modelService{"web": {}}}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := verifyReadiness(ctx, "sample", model, func(context.Context, string) ([]container.InspectResponse, error) { return nil, nil }, 0, time.Millisecond); err == nil || !strings.Contains(err.Error(), "0 of 1") {
		t.Fatalf("missing service passed: %v", err)
	}
	err := verifyReadiness(t.Context(), "sample", model, func(context.Context, string) ([]container.InspectResponse, error) {
		return nil, errors.New("secret-value")
	}, 0, time.Millisecond)
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("runtime error exposed details")
	}
}
