package application_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/recreate"
	"github.com/mapherez/nox-yard/internal/schedule"
	"github.com/mapherez/nox-yard/internal/store"
)

// Production workers run from the built Yard binary, with isolated persistent
// state. Only Tick's clock is supplied by this test; production has no clock override.
func TestScheduleDockerAcceptance(t *testing.T) {
	base, name, reference := os.Getenv("NOX_RECREATE_BASE"), os.Getenv("NOX_RECREATE_NAME"), os.Getenv("NOX_RECREATE_IMAGE")
	if base == "" {
		t.Skip("run scripts/smoke-schedules.py with an isolated fixture")
	}
	if !path.IsAbs(base) || !strings.HasPrefix(name, "recreate-smoke-") || os.Getenv("NOX_DATA_DIR") != "/data" {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	docker := func(parts ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", parts...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture: %s: %v", out, err)
		}
		return strings.TrimSpace(string(out))
	}
	var data *store.Store
	var app *application.Service
	var reader *inventory.DockerReader
	var engine *recreate.Manager
	var compose *managed.Manager
	open := func() {
		var err error
		data, err = store.Open("/data")
		if err != nil {
			t.Fatal(err)
		}
		reader, err = inventory.NewDockerReader()
		if err != nil {
			t.Fatal(err)
		}
		engine, err = recreate.New(data)
		if err != nil {
			t.Fatal(err)
		}
		compose, err = managed.NewManager(data, reader)
		if err != nil {
			t.Fatal(err)
		}
		app = application.New(data, inventory.NewNotifier())
		app.Inventory = reader
		app.Managed = compose
		app.Recreator = engine
		app.ResourceKeys = jobs.TargetResources
		observer := &jobs.Observer{Data: data, Changes: app.Changes, Verify: func(ctx context.Context, j store.Job) error {
			if j.Domain == "managed" {
				return compose.Reconcile(ctx, j)
			}
			return engine.Reconcile(ctx, j)
		}, CleanupFinal: func(ctx context.Context, j store.Job) error {
			if j.Domain == "managed" {
				return compose.CleanupDeployment(ctx, j)
			}
			return engine.Cleanup(ctx, j)
		}}
		app.Scheduler = &schedule.Manager{Data: data, Location: time.UTC, Run: app.RunScheduledUpdate, Reconcile: observer.Reconcile}
		app.JobObserver = observer
	}
	close := func() { engine.Close(); reader.Close(); data.Close() }
	open()
	defer func() { close() }()
	recordVolumes := func() {
		file, err := os.OpenFile(path.Join(base, "anonymous-volumes"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		ids := strings.Fields(docker("ps", "-aq", "--filter", "label=nox-yard.acceptance="+name))
		if len(ids) == 0 {
			return
		}
		var items []struct{ Mounts []struct{ Type, Name string } }
		if err := json.Unmarshal([]byte(docker(append([]string{"inspect"}, ids...)...)), &items); err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			for _, mount := range item.Mounts {
				if mount.Type == "volume" && len(mount.Name) == 64 {
					fmt.Fprintln(file, mount.Name)
				}
			}
		}
	}
	wait := func(id, want string) store.Job {
		t.Helper()
		deadline := time.Now().Add(100 * time.Second)
		for time.Now().Before(deadline) {
			j, found, err := data.Job(id)
			if err != nil || !found {
				t.Fatal(err)
			}
			if j.Status != "running" {
				if j.Outcome != want {
					t.Fatalf("wrong scheduled result %s/%s: %s", j.Status, j.Outcome, j.Error)
				}
				recordVolumes()
				for attempt := 0; attempt < 50; attempt++ {
					state, err := jobs.State(ctx, j)
					if err != nil {
						t.Fatal(err)
					}
					if !state.Running && !state.HelpersRunning {
						app.JobObserver.Reconcile(ctx)
						return j
					}
					time.Sleep(100 * time.Millisecond)
				}
				t.Fatal("worker did not exit")
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("job did not finish")
		return store.Job{}
	}
	publish := func(version string) {
		docker("tag", os.Getenv("NOX_RECREATE_"+version), reference+":app")
		docker("push", reference+":app")
	}
	now := time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)
	arm := func(target string) {
		t.Helper()
		row, found, err := data.ProjectSchedule(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		key := target
		if found {
			key = row.Key
		}
		data.SetProjectSchedule(ctx, key, target, false, 0)
		if _, err := data.SetProjectSchedule(ctx, key, target, true, now.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	tick := func(target, want string, restart bool) store.Job {
		t.Helper()
		arm(target)
		if err := app.Scheduler.Tick(ctx, now); err != nil {
			t.Fatal(err)
		}
		row, _, _ := data.ProjectSchedule(ctx, target)
		if row.LastJobID == "" {
			t.Fatal("no durable scheduled job")
		}
		if restart {
			close()
			open()
			if err := app.Scheduler.Tick(ctx, now.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		j := wait(row.LastJobID, want)
		if j.ScheduledFor != now.Unix() {
			t.Fatal("wrong occurrence")
		}
		if err := app.Scheduler.Tick(ctx, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		row, _, _ = data.ProjectSchedule(ctx, j.TargetID)
		if row.LastJobID != j.ID {
			t.Fatal("restart duplicated daily job")
		}
		data.SetProjectSchedule(ctx, row.Key, row.TargetID, false, 0)
		now = now.AddDate(0, 0, 1)
		return j
	}
	id := docker("run", "-d", "--name", name+"-single", "--label", "nox-yard.acceptance="+name, "--stop-timeout", "1", reference+":app")
	deadline := time.Now().Add(15 * time.Second)
	for docker("inspect", "--format", "{{.State.Health.Status}}", id) != "healthy" {
		if time.Now().After(deadline) {
			t.Fatal("fixture not healthy")
		}
		time.Sleep(200 * time.Millisecond)
	}
	target := "container:" + id
	status, err := app.ProjectSchedule(ctx, target)
	if err != nil || status.Enabled || status.Timezone != "UTC" {
		t.Fatal("default or timezone wrong", status, err)
	}
	if err := app.Scheduler.Tick(ctx, now); err != nil {
		t.Fatal(err)
	}
	history, _ := data.Jobs(ctx, target, false)
	if len(history) != 0 {
		t.Fatal("disabled project updated")
	}
	if _, err := app.SetProjectSchedule(ctx, target, true); err != nil {
		t.Fatal("enable", err)
	}
	j := tick(target, "unchanged", true)
	if j.TargetImages[0].ContainerID != id {
		t.Fatal("unchanged recreated")
	}
	t.Log("PASS: default-off, unchanged identity, real independent worker and reopen without duplicate")
	publish("V2")
	j = tick(target, "verified", true)
	target = j.TargetID
	id = strings.TrimPrefix(target, "container:")
	status, err = app.ProjectSchedule(ctx, target)
	if err != nil || status.LastOutcome != "verified" || status.TargetID != target {
		t.Fatal("retargeted schedule lost", status, err)
	}
	t.Log("PASS: changed scheduled image verified and standalone schedule follows new identity")
	publish("BAD")
	j = tick(target, "rolled_back", false)
	if j.Status != "failed" || j.Rollback != "restored" || j.TargetID != target {
		t.Fatal("rollback not truthful", j)
	}
	tick(target, "skipped", false)
	preview, err := engine.Preview(ctx, recreate.Request{ID: target, Operation: "update"})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := engine.Submit(ctx, recreate.Request{ID: target, Operation: "update", Confirm: true, Fingerprint: preview.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	wait(retry.ID, "rolled_back")
	t.Log("PASS: failed immutable image suppressed, manual retry bypasses fence and remains failed/restored")
	publish("V1")
	j = tick(target, "verified", false)
	target = j.TargetID
	id = strings.TrimPrefix(target, "container:")
	publish("EXTRA")
	tick(target, "failed", false)
	tick(target, "skipped", false)
	t.Log("PASS: new image clears failure fence; unsafe known candidate is suppressed before replacement")
	docker("stop", id)
	tick(target, "skipped", false)
	if docker("inspect", "--format", "{{.State.Running}}", id) != "false" {
		t.Fatal("automatic update started stopped project")
	}
	if _, err := app.SetProjectSchedule(ctx, target, true); err == nil {
		t.Fatal("stopped project enabled")
	}
	t.Log("PASS: stopped project never started or admitted for opt-in")
	publish("V1")
	docker("start", id)
	deadline = time.Now().Add(15 * time.Second)
	for docker("inspect", "--format", "{{.State.Health.Status}}", id) != "healthy" {
		if time.Now().After(deadline) {
			t.Fatal("not healthy")
		}
		time.Sleep(200 * time.Millisecond)
	}
	manual, _ := store.NewJob(target, "engine", "stop", nil)
	data.CreateJob(manual)
	tick(target, "skipped", false)
	data.FinishJob(manual.ID, manual.Owner, "succeeded", "completed", "", "")
	t.Log("PASS: manual reservation wins without duplicate execution")
	external := name + "-external"
	source := fmt.Sprintf("services:\n  web:\n    image: %s:app\n    stop_grace_period: 1s\n    labels: {nox-yard.acceptance: '%s'}\n    volumes: [anonymous:/anonymous]\nvolumes:\n  anonymous: {}\n", reference, name)
	externalFile := path.Join(base, "external.yaml")
	os.WriteFile(externalFile, []byte(source), 0600)
	docker("compose", "-p", external, "-f", externalFile, "up", "-d", "--wait")
	tick("compose:"+external, "unchanged", false)
	publish("V2")
	tick("compose:"+external, "verified", true)
	t.Log("PASS: external Compose schedule uses complete Engine update contract")
	managedName := name + "-managed"
	data.SetProjectsBase(base)
	request := managed.Request{Name: managedName, Mode: "new", Source: managed.SourceInput{Kind: "paste", YAML: source}}
	previewManaged, _, err := compose.Preview(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Fingerprint = previewManaged.Fingerprint
	deployed, err := compose.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	wait(deployed.ID, "verified")
	if err := compose.AssessAutomatic(ctx, managedName); err != nil {
		t.Fatal("managed assessment", err)
	}
	if _, err := app.SetProjectSchedule(ctx, "compose:"+managedName, true); err != nil {
		t.Fatal("managed opt-in", err)
	}
	tick("compose:"+managedName, "unchanged", false)
	publish("BAD")
	tick("compose:"+managedName, "rolled_back", false)
	tick("compose:"+managedName, "skipped", false)
	publish("V1")
	tick("compose:"+managedName, "verified", true)
	stored, found, err := data.ManagedProject(managedName)
	if err != nil || !found || stored.YAML != source {
		t.Fatal("automatic update changed source", err)
	}
	docker("compose", "-p", managedName, "-f", path.Join(stored.ProjectDir, "compose.yml"), "stop")
	tick("compose:"+managedName, "skipped", false)
	t.Log("PASS: managed opt-in shares immutable comparison, rollback, suppression and stopped-project policy; source preserved")
}
