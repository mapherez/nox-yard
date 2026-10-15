package application

import (
	"context"
	"errors"
	"testing"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/store"
)

type filePullController struct {
	*managed.Manager
	project, service string
	calls            int
	err              error
	job              bool
}

func (c *filePullController) PullFiles(ctx context.Context, name string) (lifecycle.MaintenanceResult, error) {
	c.calls++
	c.project = name
	_, c.job = jobs.FromContext(ctx)
	return lifecycle.MaintenanceResult{Succeeded: 1, Images: []string{"current-file-image:v2"}}, c.err
}
func (c *filePullController) PullServiceFiles(ctx context.Context, name, service string) (lifecycle.MaintenanceResult, error) {
	c.service = service
	return c.PullFiles(ctx, name)
}

type runtimePullController struct {
	lifecycle.Controller
	calls int
}

func (c *runtimePullController) PullContainer(context.Context, string) (lifecycle.MaintenanceResult, error) {
	c.calls++
	return lifecycle.MaintenanceResult{Succeeded: 1, Images: []string{"runtime-image:v1"}}, nil
}
func (c *runtimePullController) PullProject(ctx context.Context, id string) (lifecycle.MaintenanceResult, error) {
	return c.PullContainer(ctx, id)
}

func TestPullUsesCurrentComposeServiceAndOnlyFallsBackForMissingFiles(t *testing.T) {
	for _, test := range []struct {
		name                  string
		project               string
		service               string
		container             bool
		fileErr               error
		wantErr               error
		wantFile, wantRuntime int
	}{
		{"Compose service", "compose:demo", "web", true, nil, nil, 1, 0},
		{"Compose project", "compose:demo", "web", false, nil, nil, 1, 0},
		{"missing files", "compose:demo", "web", true, managed.ErrFilesMissing, nil, 1, 1},
		{"project missing files", "compose:demo", "web", false, managed.ErrFilesMissing, nil, 1, 1},
		{"invalid current source", "compose:demo", "web", true, managed.ErrInvalidSource, managed.ErrInvalidSource, 1, 0},
		{"busy configuration", "compose:demo", "web", true, managed.ErrConflict, managed.ErrConflict, 1, 0},
		{"missing service label", "compose:demo", "", true, nil, managed.ErrInvalidSource, 0, 0},
		{"standalone", "container:member", "", true, nil, nil, 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer data.Close()
			app := New(data, inventory.NewNotifier())
			app.Inventory = scheduleReader{projects: []inventory.Project{{ID: test.project, Containers: []inventory.Container{{ID: "member", Service: test.service}, {ID: "other", Service: "redis"}}}}}
			files := &filePullController{err: test.fileErr}
			runtime := &runtimePullController{}
			app.Managed, app.Lifecycle = files, runtime
			id := test.project
			if test.container {
				id = "member"
			}
			result, err := app.Pull(t.Context(), id, test.container)
			if !errors.Is(err, test.wantErr) || files.calls != test.wantFile || runtime.calls != test.wantRuntime {
				t.Fatal("wrong pull source/fallback", result, err, files.calls, runtime.calls)
			}
			if files.calls > 0 && (files.project != "demo" || !files.job || (test.container && files.service != "web") || (!test.container && files.service != "")) {
				t.Fatal("scope or shared job context lost", files)
			}
			if err == nil && test.wantRuntime == 0 && result.Images[0] != "current-file-image:v2" {
				t.Fatal("runtime image used instead of current file", result)
			}
			target := id
			if test.container {
				target = "container:" + id
			}
			history, err := data.Jobs(t.Context(), target, false)
			if err != nil || len(history) != 1 || history[0].Status == "running" || (test.wantErr != nil && history[0].Status != "failed") {
				t.Fatal("pull job not finalized", history, err)
			}
		})
	}
}

func TestContainerPullMissingInventoryDoesNotUseOldReference(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	app := New(data, inventory.NewNotifier())
	app.Managed = &filePullController{}
	runtime := &runtimePullController{}
	app.Lifecycle = runtime
	app.Inventory = scheduleReader{}
	_, err = app.Pull(t.Context(), "removed", true)
	if !errors.Is(err, inventory.ErrContainerNotFound) || runtime.calls != 0 {
		t.Fatal("unresolved target pulled stale runtime image", err)
	}
}
