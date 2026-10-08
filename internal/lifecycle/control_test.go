package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

func testControlManager(t *testing.T, transport dockerRoundTrip) *Manager {
	t.Helper()
	cli, err := client.New(client.WithHTTPClient(&http.Client{Transport: transport}), client.WithAPIVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return &Manager{client: cli}
}

func TestLifecycleWaitingHonorsDeadlineAndReleasesLock(t *testing.T) {
	m := testControlManager(t, dockerRoundTrip(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("canceled waiter reached Docker")
		return nil, nil
	}))
	if err := m.mu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := m.Project(ctx, "compose:app", Start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait ignored context: %v", err)
	}
	m.mu.Unlock()
	if err := m.mu.Lock(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.mu.Unlock()
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if _, err := m.Container(ctx, strings.Repeat("a", 64), Start); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled action acquired lock")
	}
}

func TestLifecycleFailuresRetainCauseAndBrowserJSON(t *testing.T) {
	id := strings.Repeat("a", 64)
	m := testControlManager(t, dockerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/json") {
			return dockerResponse(200, fmt.Sprintf(`{"Id":%q,"Config":{"Image":"app:latest"},"State":{"Status":"running"}}`, id)), nil
		}
		return dockerResponse(409, `{"message":"already changing"}`), nil
	}))
	result, err := m.Container(context.Background(), id, Restart)
	if err != nil || result.Failed != 1 || len(result.Failures) != 1 || result.Failures[0].Target != id || !errdefs.IsConflict(result.Failures[0].Cause) {
		t.Fatalf("cause lost: %+v %v", result, err)
	}
	body, _ := json.Marshal(result)
	if strings.Contains(string(body), "Failures") || strings.Contains(string(body), "failures") || !strings.Contains(string(body), `"errors"`) {
		t.Fatal("changed browser result contract")
	}
}

func TestProjectProtectionPreflightsEntireGroup(t *testing.T) {
	id, self := strings.Repeat("a", 64), strings.Repeat("b", 64)
	mutations := 0
	m := testControlManager(t, dockerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			mutations++
			t.Fatal("protected group mutated")
			return nil, nil
		}
		return dockerResponse(200, fmt.Sprintf(`[{"Id":%q,"Names":["/app"],"State":"running","Labels":{"com.docker.compose.project":"nox-yard"}},{"Id":%q,"Names":["/nox-yard"],"State":"running","Labels":{"com.docker.compose.project":"nox-yard","com.docker.compose.service":"nox-yard"}}]`, id, self)), nil
	}))
	if _, err := m.Project(context.Background(), "compose:nox-yard", Stop); !errors.Is(err, ErrProtected) || mutations != 0 {
		t.Fatal("protected group was not rejected before mutation")
	}
}

func TestManagedHelpersAreProtectedFromLifecycleAndPull(t *testing.T) {
	id := strings.Repeat("a", 64)
	m := testControlManager(t, dockerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("helper mutated")
		}
		return dockerResponse(200, fmt.Sprintf(`{"Id":%q,"Config":{"Image":"yard:test","Labels":{"nox-yard.role":"managed-helper"}},"State":{"Status":"running"}}`, id)), nil
	}))
	for _, action := range []Action{Start, Stop, Restart} {
		if _, err := m.Container(t.Context(), id, action); !errors.Is(err, ErrProtected) {
			t.Fatalf("helper action %s accepted: %v", action, err)
		}
	}
	if _, err := m.PullContainer(t.Context(), id); !errors.Is(err, ErrProtected) {
		t.Fatalf("helper pull accepted: %v", err)
	}
}

func TestPullDeduplicatesImagesAndPreservesTypedFailure(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	pulls := 0
	m := testControlManager(t, dockerRoundTrip(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			return dockerResponse(200, fmt.Sprintf(`[{"Id":%q,"Labels":{"com.docker.compose.project":"app"}},{"Id":%q,"Labels":{"com.docker.compose.project":"app"}}]`, a, b)), nil
		case strings.Contains(r.URL.Path, "/containers/"):
			id := a
			if strings.Contains(r.URL.Path, b) {
				id = b
			}
			return dockerResponse(200, fmt.Sprintf(`{"Id":%q,"Config":{"Image":"sha256:pinned","Labels":{"nox-yard.image-reference":"app:latest"}}}`, id)), nil
		case strings.HasSuffix(r.URL.Path, "/images/create"):
			if r.URL.Query().Get("fromImage") != "docker.io/library/app" || r.URL.Query().Get("tag") != "latest" {
				t.Fatalf("pull lost mutable source reference: %s", r.URL.RawQuery)
			}
			pulls++
			return dockerResponse(503, `{"message":"registry unavailable"}`), nil
		default:
			t.Fatalf("unexpected Docker request: %s", r.URL.Path)
			return nil, nil
		}
	}))
	result, err := m.PullProject(context.Background(), "compose:app")
	if err != nil || pulls != 1 || result.Failed != 1 || len(result.Failures) != 1 || !errdefs.IsUnavailable(result.Failures[0].Cause) {
		t.Fatalf("wrong pull result: %+v %v (%d pulls)", result, err, pulls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = m.pullImages(ctx, []string{"unattempted:latest"})
	if len(result.Failures) != 1 || !errors.Is(result.Failures[0].Cause, context.Canceled) || pulls != 1 {
		t.Fatal("canceled pull silently reported success")
	}
}

func TestSelfTargetPullAndStopAreProtected(t *testing.T) {
	id := strings.Repeat("a", 64)
	m := testControlManager(t, dockerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("protected self target mutated")
		}
		return dockerResponse(200, fmt.Sprintf(`{"Id":%q,"Config":{"Image":"yard:latest","Labels":{"com.docker.compose.project":"nox-yard","com.docker.compose.service":"nox-yard"}},"State":{"Status":"running"}}`, id)), nil
	}))
	if _, err := m.Container(context.Background(), id, Stop); !errors.Is(err, ErrProtected) {
		t.Fatal("self stop allowed")
	}
	if _, err := m.PullContainer(context.Background(), id); !errors.Is(err, ErrProtected) {
		t.Fatal("self pull allowed")
	}
}

func TestSelfRestartConflictRetainsTypedCause(t *testing.T) {
	id := strings.Repeat("a", 64)
	m := testControlManager(t, dockerRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("conflicting restart created a helper")
		}
		if strings.HasSuffix(r.URL.Path, "/containers/json") {
			return dockerResponse(200, `[{"Id":"worker","State":"running","Labels":{"nox-yard.role":"self-restart-worker"}}]`), nil
		}
		return dockerResponse(200, fmt.Sprintf(`{"Id":%q,"Config":{"Labels":{"com.docker.compose.project":"nox-yard","com.docker.compose.service":"nox-yard"}},"State":{"Status":"running"}}`, id)), nil
	}))
	result, err := m.Container(context.Background(), id, Restart)
	if err != nil || result.Failed != 1 || len(result.Failures) != 1 || !errors.Is(result.Failures[0].Cause, ErrConflict) || result.Errors[0] != "a NoX Yard maintenance helper is already running" {
		t.Fatalf("restart conflict lost its domain or browser message: %+v %v", result, err)
	}
}
