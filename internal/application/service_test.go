package application

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
)

type contextController struct {
	lifecycle.Controller
	ctx     context.Context
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (c *contextController) Container(ctx context.Context, id string, action lifecycle.Action) (lifecycle.Result, error) {
	c.calls++
	c.ctx = ctx
	if c.entered != nil {
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return lifecycle.Result{}, ctx.Err()
		}
	}
	return lifecycle.Result{Succeeded: 1}, nil
}
func TestContextPropagationAndPendingCleanup(t *testing.T) {
	changes := inventory.NewNotifier()
	app := New(nil, changes)
	c := &contextController{entered: make(chan struct{})}
	app.Lifecycle = c
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), struct{}{}, "marker"))
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := app.Action(ctx, "target", true, lifecycle.Restart); done <- err }()
	<-c.entered
	if c.ctx != ctx {
		t.Fatal("facade replaced adapter context")
	}
	if _, ok := c.ctx.Deadline(); ok {
		t.Fatal("facade introduced a deadline")
	}
	snapshot := inventory.Snapshot{Projects: []inventory.Project{{ID: "container:target", Containers: []inventory.Container{{ID: "target"}}}}}
	changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "restarting" {
		t.Fatal("missing pending state")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	changes.Apply(&snapshot)
	if snapshot.Projects[0].Operation != "" {
		t.Fatal("pending state survived cancellation")
	}
}
func TestPreviouslyCancelledContextDoesNotDispatch(t *testing.T) {
	app := New(nil, inventory.NewNotifier())
	c := &contextController{}
	app.Lifecycle = c
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checks := []func() error{
		func() error { _, e := app.Action(ctx, "target", true, lifecycle.Start); return e },
		func() error { _, e := app.Pull(ctx, "target", true); return e },
		func() error { _, e := app.Projects(ctx); return e },
		func() error { _, e := app.Inspect(ctx, "target", true); return e },
		func() error { _, e := app.Metrics(ctx); return e },
		func() error { _, e := app.PreviewRemove(ctx, "target", true); return e },
		func() error { _, e := app.Remove(ctx, "target", true, true, strings.Repeat("a", 64)); return e },
		func() error { _, e := app.PrepareSource(ctx, managed.SourceInput{}); return e },
		func() error { _, e := app.ComposePreview(ctx, managed.Request{}); return e },
		func() error { _, e := app.ComposeSubmit(ctx, managed.Request{}); return e },
		func() error { _, e := app.ComposeOperation(ctx, "target", "start", false); return e },
		func() error { _, _, e := app.ComposeJob(ctx, "job"); return e },
		func() error { _, e := app.ProjectsBase(ctx); return e },
		func() error { _, e := app.SetProjectsBase(ctx, "path"); return e },
		func() error { _, e := app.UpdateStatus(ctx); return e },
		func() error { _, e := app.SetUpdateSettings(ctx, true, 60); return e },
		func() error { _, e := app.CheckAndUpdate(ctx); return e },
		func() error { _, e := app.LogsSnapshot(ctx, "target"); return e },
		func() error { _, e := app.OpenLogs(ctx, "target"); return e },
		func() error { return app.Health(ctx) },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, context.Canceled) {
			t.Fatalf("operation %d: %v", i, err)
		}
	}
	if c.calls != 0 {
		t.Fatal("cancelled request reached manager")
	}
}

type deadlineReader struct{ ctx context.Context }

func (r *deadlineReader) Snapshot(ctx context.Context) (inventory.Snapshot, error) {
	r.ctx = ctx
	return inventory.Snapshot{Projects: []inventory.Project{}}, nil
}
func (r *deadlineReader) InspectContainer(ctx context.Context, id string, reveal bool) (inventory.ContainerInspection, error) {
	r.ctx = ctx
	return inventory.ContainerInspection{}, nil
}
func TestProjectsPreservesCallerDeadlineAndManagedOverlay(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	if err := data.SaveManagedProject(store.ManagedProject{Name: "demo", YAML: "services: {}", VariablesJSON: "{}", EnvFilesJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	app := New(data, inventory.NewNotifier())
	r := &deadlineReader{}
	app.Inventory = r
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	out, err := app.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.ctx != ctx {
		t.Fatal("request context replaced")
	}
	if len(out.Projects) != 1 || out.Projects[0].Kind != "managed-compose" {
		t.Fatalf("overlay: %+v", out)
	}
}

type blockedLogs struct {
	inventory.LogReader
	reader  *io.PipeReader
	entered chan struct{}
}

func (l *blockedLogs) OpenLogsWithOptions(ctx context.Context, id string, o inventory.LogOptions) (inventory.LogStream, error) {
	close(l.entered)
	return inventory.LogStream{TTY: true, Reader: l.reader}, nil
}
func TestLogsCancellationClosesBlockedReader(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	logs := &blockedLogs{reader: r, entered: make(chan struct{})}
	app := New(nil, inventory.NewNotifier())
	app.Logs = logs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := app.LogsSnapshot(ctx, "target"); done <- err }()
	<-logs.entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled log read leaked")
	}
}

type failedStatus struct{}

func (failedStatus) Status() (selfupdate.Status, error) {
	return selfupdate.Status{}, errors.New("storage failed")
}
func (failedStatus) SetSettings(bool, int) error { return nil }
func (failedStatus) CheckNow() error             { return nil }
func TestAcceptedUpdateWithFailedStatusIsDistinguished(t *testing.T) {
	app := New(nil, inventory.NewNotifier())
	app.Updates = failedStatus{}
	for _, op := range []func(context.Context) (selfupdate.Status, error){app.CheckAndUpdate, func(ctx context.Context) (selfupdate.Status, error) { return app.SetUpdateSettings(ctx, true, 60) }} {
		_, err := op(context.Background())
		var read UpdateStatusError
		if !errors.As(err, &read) {
			t.Fatal("lost accepted mutation/status distinction")
		}
	}
}
