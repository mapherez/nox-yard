package recreate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"errors"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/client"
)

func TestRecreateDockerAcceptance(t *testing.T) {
	base, name, reference := os.Getenv("NOX_RECREATE_BASE"), os.Getenv("NOX_RECREATE_NAME"), os.Getenv("NOX_RECREATE_IMAGE")
	if base == "" {
		t.Skip("run scripts/smoke-recreate.py with an isolated fixture")
	}
	if !path.IsAbs(base) || !strings.HasPrefix(name, "recreate-smoke-") {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	m, err := New(data)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.launch = func(context.Context, *store.Store, store.Job, string) error { return nil }
	docker := func(parts ...string) string {
		t.Helper()
		value, err := exec.CommandContext(ctx, "docker", parts...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture command: %s: %v", value, err)
		}
		return strings.TrimSpace(string(value))
	}
	recordVolumes := func() {
		t.Helper()
		file, err := os.OpenFile(path.Join(base, "anonymous-volumes"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		for _, fixtureID := range strings.Fields(docker("ps", "-aq", "--no-trunc", "--filter", "label=nox-yard.acceptance="+name)) {
			inspected, err := m.client.ContainerInspect(ctx, fixtureID, client.ContainerInspectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, mounted := range inspected.Container.Mounts {
				if mounted.Type == "volume" && len(mounted.Name) == 64 {
					if _, err := fmt.Fprintln(file, mounted.Name); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	publish := func(version, tag string) {
		t.Helper()
		docker("tag", os.Getenv("NOX_RECREATE_"+version), reference+":"+tag)
		docker("push", reference+":"+tag)
	}
	run := func(target, operation, want string) store.Job {
		t.Helper()
		input := Request{ID: target, Operation: operation, Confirm: true}
		preview, err := m.Preview(ctx, input)
		if err != nil {
			t.Fatal("preview", err)
		}
		input.Fingerprint = preview.Fingerprint
		job, err := m.Submit(ctx, input)
		if err != nil {
			t.Fatal("submit", err)
		}
		if err := m.execute(ctx, job); err != nil {
			t.Fatal("execute", err)
		}
		result, _, err := data.Job(job.ID)
		if err != nil || result.Outcome != want {
			t.Fatalf("wrong result: %s/%s (%s), %v", result.Status, result.Outcome, result.Error, err)
		}
		if result.Status == "succeeded" {
			if err := m.Cleanup(ctx, result); err != nil {
				t.Fatal("cleanup", err)
			}
		}
		return result
	}
	if err := os.MkdirAll(path.Join(base, "bind"), 0700); err != nil {
		t.Fatal(err)
	}
	docker("network", "create", "--label", "nox-yard.acceptance="+name, name+"-net")
	// Let Docker allocate a free subnet, then declare it explicitly for static IPs.
	subnet := docker("network", "inspect", "--format", "{{(index .IPAM.Config 0).Subnet}}", name+"-net")
	prefix, err := netip.ParsePrefix(subnet)
	if err != nil || !prefix.Addr().Is4() {
		t.Fatalf("fixture network did not allocate an IPv4 subnet: %q (%v)", subnet, err)
	}
	gateway := prefix.Masked().Addr().Next()
	staticIP := gateway
	for range 9 {
		staticIP = staticIP.Next()
	}
	if !prefix.Contains(staticIP) {
		t.Fatalf("fixture subnet cannot hold its static address: %s", prefix)
	}
	docker("network", "rm", name+"-net")
	// Engines may not report a gateway until the first endpoint is attached.
	docker("network", "create", "--subnet", subnet, "--gateway", gateway.String(), "--label", "nox-yard.acceptance="+name, name+"-net")
	docker("volume", "create", "--label", "nox-yard.acceptance="+name, name+"-data")
	id := docker("run", "-d", "--name", name+"-single", "--label", "nox-yard.acceptance="+name, "--network", name+"-net", "--network-alias", "app-alias",
		"--mount", "type=volume,source="+name+"-data,target=/data", "--mount", "type=bind,source="+base+"/bind,target=/bind",
		"--ip", staticIP.String(), "--mac-address", "02:42:ac:11:00:10", "--publish", "127.0.0.1:"+os.Getenv("NOX_RECREATE_PORT")+":8080",
		"--env", "TOKEN=private$literal", "--workdir", "/data", "--memory", "64m", "--cpus", "0.4", "--pids-limit", "64", "--read-only", "--tmpfs", "/tmp", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--restart", "unless-stopped", reference+":app")
	old, _ := m.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err := m.ready(ctx, &entry{Old: old.Container}, id); err != nil {
		t.Fatal(err)
	}
	docker("exec", id, "sh", "-c", "echo fixture >/data/keep; echo fixture >/bind/keep; echo fixture >/anonymous/keep")
	unchanged := run("container:"+id, "update", "unchanged")
	if unchanged.TargetImages[0].ContainerID != id {
		t.Fatal("unchanged target recreated")
	}
	t.Log("PASS: unchanged standalone image preserves container identity")
	publish("V2", "app")
	updated := run("container:"+id, "update", "verified")
	newID := updated.TargetImages[0].ContainerID
	if newID == id || updated.TargetID != "container:"+newID {
		t.Fatal("replacement identity not tracked")
	}
	if docker("exec", newID, "cat", "/data/keep", "/bind/keep", "/anonymous/keep") != "fixture\nfixture\nfixture" {
		t.Fatal("data lost")
	}
	history, err := data.Jobs(ctx, "container:"+newID, false)
	if err != nil || len(history) == 0 || history[0].ID != updated.ID {
		t.Fatal("new target lost operation history")
	}
	t.Log("PASS: changed standalone preserves mounts/env/networks/resource/security configuration and retargets history")
	publish("EXTRA", "app")
	rejected := run("container:"+newID, "update", "failed")
	if rejected.Rollback != "not_needed" || !strings.Contains(rejected.Error, "additional volumes") {
		t.Fatal("unassessed image volume mutated target")
	}
	t.Log("PASS: additional image volumes rejected before replacement")
	publish("BAD", "app")
	rolled := run("container:"+newID, "update", "rolled_back")
	if rolled.Status != "failed" || rolled.Rollback != "restored" || rolled.TargetID != "container:"+newID {
		t.Fatal("rollback outcome not truthful")
	}
	if docker("exec", newID, "cat", "/data/keep", "/bind/keep") != "fixture\nfixture" {
		t.Fatal("rollback lost data")
	}
	t.Log("PASS: failed standalone replacement restores original container identity/configuration/data")
	docker("stop", os.Getenv("NOX_RECREATE_REGISTRY"))
	failed := run("container:"+newID, "update", "failed")
	if failed.Rollback != "not_needed" {
		t.Fatal("failed pull mutated target")
	}
	docker("start", os.Getenv("NOX_RECREATE_REGISTRY"))
	t.Log("PASS: failed registry pull retains originals")
	input := Request{ID: "container:" + newID, Operation: "recreate", Confirm: true}
	preview, err := m.Preview(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	input.Fingerprint = preview.Fingerprint
	docker("stop", newID)
	if _, err := m.Submit(ctx, input); err == nil {
		t.Fatal("stale runtime preview accepted")
	}
	recreated := run("container:"+newID, "recreate", "verified")
	stoppedID := recreated.TargetImages[0].ContainerID
	if docker("inspect", "--format", "{{.State.Running}}", stoppedID) != "false" {
		t.Fatal("stopped container started")
	}
	t.Log("PASS: stale preview aborts; explicit same-image recreation retains stopped state")
	project := name + "-group"
	// The registry uses tmpfs: a restart intentionally erased its manifests.
	publish("V1", "db")
	publish("V1", "web")
	yaml := fmt.Sprintf("services:\n  db:\n    image: %s:db\n    labels: {nox-yard.acceptance: '%s'}\n  web:\n    image: %s:web\n    labels: {nox-yard.acceptance: '%s'}\n    depends_on:\n      db: {condition: service_healthy}\n    volumes: [data:/data, './bind:/bind']\n  idle:\n    image: %s:db\n    labels: {nox-yard.acceptance: '%s'}\nvolumes:\n  data:\n    labels: {nox-yard.acceptance: '%s'}\nnetworks:\n  default:\n    labels: {nox-yard.acceptance: '%s'}\n", reference, name, reference, name, reference, name, name, name)
	composeFile := path.Join(base, "compose.yml")
	if err := os.WriteFile(composeFile, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	docker("compose", "-p", project, "-f", composeFile, "up", "-d", "--wait")
	docker("compose", "-p", project, "-f", composeFile, "stop", "idle")
	before := docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project="+project)
	publish("V2", "web")
	group := run("compose:"+project, "update", "verified")
	if len(group.TargetImages) != 3 {
		t.Fatal("partial group identity missing")
	}
	publish("BAD", "web")
	groupBefore := docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project="+project)
	groupFailed := run("compose:"+project, "update", "rolled_back")
	if groupFailed.Rollback != "restored" || docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project="+project) != groupBefore {
		t.Fatal("failed group did not restore originals")
	}
	if before == groupBefore {
		t.Fatal("changed group not replaced")
	}
	if bytes, err := os.ReadFile(composeFile); err != nil || string(bytes) != yaml {
		t.Fatal("external source changed")
	}
	t.Log("PASS: external Compose dependency order, partial-group rollback, stopped services and untouched source")
	// Unchecked real Engine removal retains exclusive volumes as well as binds
	// and source. Keep the fixture images referenced during cleanup.
	docker("create", "--label", "nox-yard.acceptance="+name, os.Getenv("NOX_RECREATE_V1"), "true")
	engine, err := lifecycle.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	recordVolumes()
	plan, err := engine.PreviewRemoval(ctx, "compose:"+project, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.Items {
		if item.Kind == "volume" && item.Action != "keep" {
			t.Fatal("unselected volume eligible")
		}
	}
	report, err := engine.RemoveWithOptions(ctx, "compose:"+project, plan.Fingerprint, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range report.Items {
		if item.Status == "failed" {
			t.Fatal("fixture removal", item)
		}
	}
	docker("volume", "inspect", project+"_data")
	if bytes, err := os.ReadFile(composeFile); err != nil || string(bytes) != yaml {
		t.Fatal("removal changed source")
	}
	t.Log("PASS: unchecked real removal retains exclusive volumes, binds and source")

	// Simulate a competing host administrator deleting only this fixture's
	// retained original. A failed replacement must not claim restored/success
	// or release reservations when restoration is impossible.
	publish("V2", "recovery")
	recoveryID := docker("run", "-d", "--label", "nox-yard.acceptance="+name, "--name", name+"-recovery", reference+":recovery")
	recoveryOld, _ := m.client.ContainerInspect(ctx, recoveryID, client.ContainerInspectOptions{})
	recordVolumes()
	if err := m.ready(ctx, &entry{Old: recoveryOld.Container}, recoveryID); err != nil {
		t.Fatal(err)
	}
	publish("BAD", "recovery")
	recoveryInput := Request{ID: "container:" + recoveryID, Operation: "update", Confirm: true}
	recoveryPreview, err := m.Preview(ctx, recoveryInput)
	if err != nil {
		t.Fatal(err)
	}
	recoveryInput.Fingerprint = recoveryPreview.Fingerprint
	recoveryJob, err := m.Submit(ctx, recoveryInput)
	if err != nil {
		t.Fatal(err)
	}
	interference := make(chan error, 1)
	go func() {
		for {
			current, _, err := data.Job(recoveryJob.ID)
			if err != nil {
				interference <- err
				return
			}
			var private journal
			if json.Unmarshal([]byte(current.Payload), &private) == nil && len(private.Entries) == 1 && private.Entries[0].NewID != "" {
				_, err := m.client.ContainerRemove(ctx, recoveryID, client.ContainerRemoveOptions{Force: true})
				interference <- err
				return
			}
			select {
			case <-ctx.Done():
				interference <- ctx.Err()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	if err := m.execute(ctx, recoveryJob); err != nil {
		t.Fatal(err)
	}
	if err := <-interference; err != nil {
		t.Fatal(err)
	}
	recoveryResult, _, err := data.Job(recoveryJob.ID)
	if err != nil || recoveryResult.Status != "failed" || recoveryResult.Outcome != "recovery_required" || recoveryResult.Rollback != "failed" || recoveryResult.TargetImages[0].State != "unknown" {
		t.Fatal("failed restoration not truthful", recoveryResult, err)
	}
	conflict, _ := store.NewJob(recoveryInput.ID, "engine", "stop", nil)
	if err := data.CreateJob(conflict); !errors.Is(err, store.ErrOperationConflict) {
		t.Fatal("failed restoration released reservation", err)
	}
	if err := m.Cleanup(ctx, recoveryResult); err != nil {
		t.Fatal(err)
	}
	t.Log("PASS: failed restoration reports recovery-required/unknown state and retains ownership")
	public, _ := json.Marshal(groupFailed)
	if strings.Contains(string(public), "private$") || strings.Contains(string(public), "Payload") {
		t.Fatal("private snapshot leaked")
	}
}
