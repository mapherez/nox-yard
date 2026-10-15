package managed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/mapherez/nox-yard/internal/imageidentity"
	"github.com/mapherez/nox-yard/internal/jobs"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/moby/moby/api/types/container"
)

// Explicitly enabled by smoke-managed.py in an isolated Linux runner. URLs are
// supplied through the intake seam: business copy/sync/cancel behavior is real,
// without publishing fixture sources or bypassing production SSRF protection.
func TestDeploymentDockerAcceptance(t *testing.T) {
	base := os.Getenv("NOX_MANAGED_ACCEPTANCE_BASE")
	if base == "" {
		t.Skip("run scripts/smoke-managed.py with an isolated Docker fixture")
	}
	prefix, reference := os.Getenv("NOX_MANAGED_ACCEPTANCE_NAME"), os.Getenv("NOX_MANAGED_ACCEPTANCE_IMAGE")
	if !strings.HasPrefix(prefix, "managed-smoke-") || !path.IsAbs(base) || reference == "" {
		t.Fatal("invalid fixture identity")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	if err := data.SetProjectsBase(base); err != nil {
		t.Fatal(err)
	}
	m, _ := NewManager(data, previewInventory{})
	m.launch = func(context.Context, *store.Store, store.Job, string) error { return nil }
	docker := func(parts ...string) string {
		t.Helper()
		value, err := exec.CommandContext(ctx, "docker", parts...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture command failed: %s: %v", value, err)
		}
		return strings.TrimSpace(string(value))
	}
	// Docker itself allocates ports on its host, including when the test runner
	// runs in a separate network namespace. All owners use the fixture label.
	portOwner := prefix + "-port-owner"
	docker("run", "-d", "--name", portOwner, "--label", "nox-yard.acceptance="+prefix, "-p", "127.0.0.1::8080", os.Getenv("NOX_MANAGED_ACCEPTANCE_V1"))
	allocatedPort := func(name string) string {
		t.Helper()
		return docker("inspect", "--format", `{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}`, name)
	}
	blockedPort := allocatedPort(portOwner)
	freePort := func() string {
		t.Helper()
		name := prefix + "-port-reserver"
		docker("run", "-d", "--name", name, "--label", "nox-yard.acceptance="+prefix, "-p", "127.0.0.1::8080", os.Getenv("NOX_MANAGED_ACCEPTANCE_V1"))
		port := allocatedPort(name)
		docker("rm", "-f", name)
		return port
	}
	appPort := freePort()
	sourceURL := "https://example.test/fixture.yml"
	source := fmt.Sprintf(`services:
  web:
    image: %s
    ports: ['127.0.0.1:${APP_PORT:-%s}:8080']
    init: true
    logging:
      driver: json-file
      options:
        max-size: '10m'
        max-file: '3'
        tag: 'private$$literal'
    tmpfs:
      - /volatile
      - /scratch:size=8m,mode=1770
    stop_grace_period: 1s
    env_file: [config/app.env]
    environment: {LITERAL: '${LITERAL}'}
    labels: {nox-yard.acceptance: '%s'}
    volumes:
      - data:/data
      - './bind:/bind'
      - type: tmpfs
        target: /cache
        read_only: true
        tmpfs:
          size: 65536
          mode: 0700
  idle:
    image: %s
    stop_grace_period: 1s
    labels: {nox-yard.acceptance: '%s'}
volumes:
  data: {}
`, reference, blockedPort, prefix, reference, prefix)
	m.loadSource = func(ctx context.Context, input SourceInput) (Source, error) {
		if input.Kind == "url" && input.URL == sourceURL {
			return Source{Kind: "url", URL: sourceURL, Filename: "fixture.yml", YAML: source}, nil
		}
		return LoadSource(ctx, input)
	}
	variables := map[string]string{"LITERAL": "private$literal", "APP_PORT": appPort}
	env := map[string]string{"config/app.env": "TOKEN=private-fixture-secret\n"}
	request := Request{Name: prefix, Mode: "new", Source: SourceInput{Kind: "url", URL: sourceURL}, Variables: variables, EnvFiles: env}
	run := func(accepted store.Job, want string) store.Job {
		t.Helper()
		current, found, err := data.Job(accepted.ID)
		if err != nil || !found {
			t.Fatal(err)
		}
		var payload workerPayload
		if err := json.Unmarshal([]byte(current.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		t.Setenv("NOX_JOB_ID", current.ID)
		if err := m.execute(ctx, current); err != nil {
			t.Fatal(err)
		}
		result, _, err := data.Job(current.ID)
		if err != nil || result.Status != want {
			t.Fatalf("unexpected result: %s / %s: %s, %v", result.Status, result.Outcome, result.Error, err)
		}
		return result
	}
	submit := func(input Request, want string) store.Job {
		t.Helper()
		preview, _, err := m.Preview(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		input.Fingerprint = preview.Fingerprint
		job, err := m.Submit(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return run(job, want)
	}
	operation := func(operation, want string) store.Job {
		t.Helper()
		job, err := m.Operation(prefix, operation, false)
		if err != nil {
			t.Fatal(err)
		}
		return run(job, want)
	}
	runtime := func() []container.InspectResponse {
		t.Helper()
		items, err := m.runtime(ctx, prefix)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	publish := func(version string) {
		t.Helper()
		docker("tag", os.Getenv("NOX_MANAGED_ACCEPTANCE_"+version), reference)
		docker("push", reference)
	}
	web := func() container.InspectResponse {
		t.Helper()
		for _, item := range runtime() {
			if item.Config.Labels["com.docker.compose.service"] == "web" {
				return item
			}
		}
		t.Fatal("web missing")
		return container.InspectResponse{}
	}
	assertData := func() {
		t.Helper()
		if docker("exec", web().ID, "cat", "/data/keep", "/bind/keep") != "fixture\nfixture" {
			t.Fatal("shared data lost")
		}
	}
	assertSource := func(want store.ManagedProject) {
		t.Helper()
		actual, _, err := data.ManagedProject(prefix)
		if err != nil || actual.YAML != want.YAML || actual.VariablesJSON != want.VariablesJSON || actual.EnvFilesJSON != want.EnvFilesJSON {
			t.Fatal("SQL source differs from expected version")
		}
		if err := hostProjectFiles(ctx, want.ProjectDir, &want, want, "verified"); err != nil {
			t.Fatal("host source differs from SQL", err)
		}
	}
	submit(request, "succeeded")
	original := web()
	assertRuntimeSettings := func(item container.InspectResponse) {
		t.Helper()
		if allocatedPort(item.ID) != appPort {
			t.Fatal("saved published port changed")
		}
		if item.HostConfig == nil || item.HostConfig.Init == nil || !*item.HostConfig.Init || item.HostConfig.LogConfig.Type != "json-file" {
			t.Fatal("init or logging driver changed")
		}
		for key, value := range map[string]string{"max-size": "10m", "max-file": "3", "tag": "private$literal"} {
			if item.HostConfig.LogConfig.Config[key] != value {
				t.Fatal("logging option changed")
			}
		}
		if len(item.HostConfig.Tmpfs) != 2 || item.HostConfig.Tmpfs["/scratch"] != "size=8m,mode=1770" {
			t.Fatal("service tmpfs configuration changed")
		}
		if value, ok := item.HostConfig.Tmpfs["/volatile"]; !ok || value != "" {
			t.Fatal("plain tmpfs configuration changed")
		}
		found := false
		for _, mounted := range item.HostConfig.Mounts {
			if mounted.Type == "tmpfs" && mounted.Target == "/cache" {
				found = mounted.ReadOnly && mounted.TmpfsOptions != nil && mounted.TmpfsOptions.SizeBytes == 65536 && mounted.TmpfsOptions.Mode == 0700
			}
		}
		if !found {
			t.Fatal("long-syntax tmpfs configuration changed")
		}
	}
	assertRuntimeSettings(original)
	for _, value := range original.Config.Env {
		if strings.HasPrefix(value, "LITERAL=") && value != "LITERAL=private$literal" {
			t.Fatal("literal interpolation changed")
		}
	}
	docker("exec", original.ID, "sh", "-c", "echo fixture > /data/keep; echo fixture > /bind/keep")
	before := runtime()
	unchanged := operation("update", "succeeded")
	if unchanged.Outcome != "unchanged" || !matchesSourceImages(runtime(), unchanged.SourceImages) {
		t.Fatal("unchanged images recreated containers")
	}
	t.Log("PASS: URL intake, env files, literal dollars and unchanged container identities")
	publish("V2")
	engine, err := lifecycle.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	enginePull, err := engine.PullProject(ctx, "compose:"+prefix)
	if err != nil || enginePull.Failed != 0 || enginePull.Succeeded != 1 || !matchesSourceImages(runtime(), unchanged.SourceImages) {
		t.Fatalf("Engine pull lost source reference or changed pinned containers: %+v %v", enginePull, err)
	}
	if original.Config.Image != original.Image || original.Config.Labels[imageidentity.ReferenceLabel] != reference {
		t.Fatal("managed container must pin its ID and retain its original reference")
	}
	resources := make([]string, 0, len(before))
	for _, item := range before {
		resources = append(resources, "container:"+item.ID)
	}
	cached, err := jobs.Images(ctx, resources, true)
	if err != nil || len(cached) != len(before) {
		t.Fatalf("cached image history unavailable: %+v %v", cached, err)
	}
	for _, image := range cached {
		if image.ImageID == original.Image {
			t.Fatal("cached Engine history still reports the deployed image after pull")
		}
	}
	t.Log("PASS: Engine pull retains source references, caches new IDs and preserves pinned containers")
	pulled := operation("pull", "succeeded")
	if pulled.Outcome != "cached" || !matchesSourceImages(runtime(), pulled.SourceImages) {
		t.Fatal("pull mutated deployed containers")
	}
	updated := operation("update", "succeeded")
	if updated.Outcome != "verified" || web().ID == original.ID || web().Image == original.Image {
		t.Fatal("changed image was not replaced")
	}
	assertRuntimeSettings(web())
	assertData()
	t.Log("PASS: plain/optioned service tmpfs and long-syntax tmpfs survive image replacement")
	t.Log("PASS: real registry pull is cache-only; changed image update is immutable and preserves named-volume/bind data")
	// Simulate a pull failure by removing only this fixture registry container.
	registry := os.Getenv("NOX_MANAGED_ACCEPTANCE_REGISTRY")
	docker("stop", registry)
	before = runtime()
	failed := operation("update", "failed")
	if failed.Rollback != "not_needed" || !matchesSourceImages(runtime(), failed.SourceImages) {
		t.Fatal("failed pull altered the stack")
	}
	docker("start", registry)
	t.Log("PASS: failed registry pull leaves existing containers and source intact")
	for _, item := range before {
		if item.Config.Labels["com.docker.compose.service"] == "idle" {
			docker("stop", item.ID)
		}
	}
	prior, _, _ := data.ManagedProject(prefix)
	publish("BAD")
	failed = operation("update", "failed")
	if failed.Outcome != "rolled_back" || failed.Rollback != "restored" {
		t.Fatalf("rollback missing: %s: %s", failed.Outcome, failed.Error)
	}
	assertRuntimeSettings(web())
	t.Log("PASS: rollback restores tmpfs targets, options, size, mode and read-only state")
	for _, item := range runtime() {
		if item.Image != before[0].Image || (item.Config.Labels["com.docker.compose.service"] == "idle" && item.State.Running) {
			t.Fatal("previous image/stopped state not restored")
		}
	}
	assertSource(prior)
	assertData()
	t.Log("PASS: unhealthy changed image restores old image/configuration and stopped service; data preserved")
	// The registry/cache still points at BAD after rollback. A normal Stop/Start
	// must not silently reapply that failed version from a mutable cached tag.
	restoredID, restoredImage := web().ID, web().Image
	operation("stop", "succeeded")
	operation("start", "succeeded")
	if web().ID != restoredID || web().Image != restoredImage {
		t.Fatal("start silently reapplied the failed cached image after rollback")
	}
	assertData()
	t.Log("PASS: stop/start after rollback preserves restored containers despite the failed image remaining in cache")
	publish("V2")
	// Preview/cancel performs no host writes and no replacement.
	source = strings.Replace(source, "stop_grace_period: 1s", "stop_grace_period: 2s", 1)
	sync := request
	sync.Mode = "sync"
	sync.Variables = nil // Untouched fields must retain saved overrides, including APP_PORT.
	sync.EnvFiles = map[string]string{"config/app.env": "TOKEN=new-private-secret\n"}
	conflicting := sync
	conflicting.Variables = map[string]string{"APP_PORT": blockedPort}
	beforePortCheck := web().ID
	if _, _, err := m.Preview(ctx, conflicting); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), portOwner) {
		t.Fatalf("occupied explicit port not rejected with its owner: %v", err)
	}
	assertSource(prior)
	if web().ID != beforePortCheck {
		t.Fatal("port preview replaced running containers")
	}
	// A port can become occupied after confirmation. The worker rechecks before
	// replacing anything and leaves the original files, containers and SQL intact.
	lateOwner, latePort := prefix+"-late-port-owner", freePort()
	docker("create", "--name", lateOwner, "--label", "nox-yard.acceptance="+prefix, "-p", "127.0.0.1:"+latePort+":8080", os.Getenv("NOX_MANAGED_ACCEPTANCE_V1"))
	conflicting.Variables = map[string]string{"APP_PORT": latePort}
	latePreview, _, err := m.Preview(ctx, conflicting)
	if err != nil {
		t.Fatal(err)
	}
	conflicting.Fingerprint = latePreview.Fingerprint
	lateJob, err := m.Submit(ctx, conflicting)
	if err != nil {
		t.Fatal(err)
	}
	docker("start", lateOwner)
	portFailure := run(lateJob, "failed")
	if portFailure.Rollback != "not_needed" || !strings.Contains(portFailure.Error, lateOwner) || web().ID != beforePortCheck {
		t.Fatalf("late port conflict should fail before mutation: %s / %s", portFailure.Rollback, portFailure.Error)
	}
	assertSource(prior)
	docker("rm", "-f", lateOwner)
	t.Log("PASS: occupied ports fail in preview and are rechecked before replacement without rollback or container changes")
	preview, _, err := m.Preview(ctx, sync)
	if err != nil || len(preview.Changes) == 0 {
		t.Fatalf("sync preview missing changes: %v", err)
	}
	assertSource(prior)
	if _, err := m.Submit(ctx, sync); err == nil {
		t.Fatal("unconfirmed sync accepted")
	}
	source = strings.Replace(source, "stop_grace_period: 2s", "stop_grace_period: 2s\n    healthcheck: {test: [CMD, 'false'], interval: 1s, timeout: 1s, retries: 1}", 1)
	failed = submit(sync, "failed")
	if failed.Rollback != "restored" {
		t.Fatalf("sync rollback missing: %s: %s", failed.Outcome, failed.Error)
	}
	assertSource(prior)
	assertData()
	t.Log("PASS: explicit URL sync/cancel; unhealthy source sync restores host files, env and SQL consistently")
	source = strings.Replace(source, "    healthcheck: {test: [CMD, 'false'], interval: 1s, timeout: 1s, retries: 1}\n", "", 1)
	submit(sync, "succeeded")
	next, _, _ := data.ManagedProject(prefix)
	if next.YAML == prior.YAML || next.EnvFilesJSON == prior.EnvFilesJSON {
		t.Fatal("verified sync source was not committed")
	}
	assertSource(next)
	assertData()
	assertRuntimeSettings(web())
	if next.VariablesJSON != prior.VariablesJSON {
		t.Fatal("omitted sync variables lost saved overrides")
	}
	t.Log("PASS: URL sync with omitted variables retains the saved published port despite an occupied source default")
	// Exercise a partially committed source journal directly in the fixture.
	// SQL still describes next; the recovery path restores only owned files.
	journalCandidate := next
	journalCandidate.YAML = strings.Replace(next.YAML, "config/app.env", "config/new.env", 1)
	journalCandidate.EnvFilesJSON = `{"config/new.env":"TOKEN=journal-private-value\n"}`
	_, nextEnv, _ := projectEnvironment(next)
	if err := writeHostCompose(ctx, next.ProjectDir, journalCandidate.YAML, nextEnv, false); err != nil {
		t.Fatal("partial journal fixture could not be staged")
	}
	if err := hostProjectFiles(ctx, next.ProjectDir, &next, journalCandidate, "restore"); err != nil {
		t.Fatal("partial source journal could not be restored", err)
	}
	assertSource(next)
	assertData()
	if err := hostProjectFiles(ctx, next.ProjectDir, &next, journalCandidate, "commit"); err != nil {
		t.Fatal("source journal commit failed", err)
	}
	if err := hostProjectFiles(ctx, next.ProjectDir, &next, journalCandidate, "verified"); err != nil {
		t.Fatal("committed source journal did not match", err)
	}
	if err := hostProjectFiles(ctx, next.ProjectDir, &next, journalCandidate, "restore"); err != nil {
		t.Fatal("source/env journal rollback failed", err)
	}
	assertSource(next)
	assertData()
	t.Log("PASS: partial/full source journal restores old files and removed/added env paths without touching data")
	copy := sync
	copy.Name = prefix + "-copy"
	copy.Mode = "copy"
	copy.Variables = map[string]string{"APP_PORT": "0", "LITERAL": "copy-value"}
	submit(copy, "succeeded")
	copyProject, _, _ := data.ManagedProject(copy.Name)
	if copyProject.ProjectDir == next.ProjectDir {
		t.Fatal("URL copy reused original directory")
	}
	t.Log("PASS: successful source sync commits verified files/SQL; URL copy has a separate directory")
	for _, invalid := range []string{"include: [other.yml]\nservices: {}", "services:\n  web:\n    build: .\n"} {
		if _, err := Validate(ctx, prefix, Source{YAML: invalid}, nil, nil, next.ProjectDir); err == nil {
			t.Fatal("unsupported source accepted")
		}
	}
	history, err := data.Jobs(ctx, "compose:"+prefix, false)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(history)
	for _, secret := range []string{"private-fixture-secret", "new-private-secret", "private$literal", "\"Payload\"", "\"payload\""} {
		if strings.Contains(string(public), secret) {
			t.Fatal("private data escaped job history")
		}
	}
	t.Log("PASS: unsupported sources rejected before mutation; errors/history omit private source/env snapshots")
	t.Run("materialized environment", TestMaterializedEnvironmentDocker)
	if err := m.migrateConfiguration(ctx, next, false); err != nil {
		t.Fatal("file migration", err)
	}
	cleared, _, _ := data.ManagedProject(prefix)
	if cleared.YAML != "" || cleared.VariablesJSON != "{}" || cleared.EnvFilesJSON != "{}" {
		t.Fatal("migration retained active database configuration")
	}
	configuration, err := m.Configuration(ctx, prefix)
	if err != nil || !configuration.Available || configuration.State != "ready" {
		t.Fatalf("configuration unavailable: %v", err)
	}
	if _, exists := func() (string, bool) {
		for _, file := range configuration.Files {
			if file.Path == ".env" {
				return file.Content, true
			}
		}
		return "", false
	}(); !exists {
		t.Fatal("interpolation variables were not materialized")
	}
	beforeFileEdit := web()
	configuration, err = m.SaveConfiguration(ctx, prefix, ConfigurationEdit{Fingerprint: configuration.Fingerprint, Service: "web", Variable: "LITERAL", Content: "edited$private-value"})
	if err != nil {
		t.Fatal("inline variable edit", err)
	}
	if web().ID != beforeFileEdit.ID {
		t.Fatal("saving configuration changed containers")
	}
	if _, err := m.SaveConfiguration(ctx, prefix, ConfigurationEdit{Fingerprint: "stale", Service: "web", Variable: "LITERAL", Content: "must-not-write"}); err == nil {
		t.Fatal("stale file edit was accepted")
	}
	docker("stop", registry)
	fileJob, err := m.FileOperation(ctx, prefix, "recreate", "")
	if err != nil {
		t.Fatal(err)
	}
	run(fileJob, "succeeded")
	if web().ID == beforeFileEdit.ID || web().Image != beforeFileEdit.Image {
		t.Fatal("configuration recreation did not use the cached image")
	}
	foundEdited := false
	for _, entry := range web().Config.Env {
		if entry == "LITERAL=edited$private-value" {
			foundEdited = true
		}
	}
	if !foundEdited {
		t.Fatal("saved env was not applied")
	}
	assertData()
	docker("start", registry)
	t.Log("PASS: migration exports .env; inline edits preserve containers until cached-only Recreate and stale writes are rejected")
	// Preserve original filenames and Compose merge order across manual edits.
	originalFiles, err := m.readFileSnapshot(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	multi := originalFiles
	multi.Files = map[string]string{}
	for file, content := range originalFiles.Files {
		multi.Files[file] = content
	}
	multi.ComposeFiles = []string{"compose.yml", "override.yaml"}
	multi.Files["override.yaml"] = "services:\n  web:\n    environment: {MANUAL_FILE: original}\n"
	if err := saveConfigurationFiles(ctx, originalFiles, multi); err != nil {
		t.Fatal(err)
	}
	encodedCompose, _ := json.Marshal(multi.ComposeFiles)
	if err := data.SetFileConfiguration(store.FileConfiguration{Name: prefix, Directory: next.ProjectDir, ComposeFiles: string(encodedCompose), State: "ready"}); err != nil {
		t.Fatal(err)
	}
	manual, err := m.readFileSnapshot(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	changed := manual
	changed.Files = map[string]string{}
	for file, content := range manual.Files {
		changed.Files[file] = content
	}
	changed.Files["override.yaml"] = strings.Replace(changed.Files["override.yaml"], "original", "from-terminal", 1)
	if err := saveConfigurationFiles(ctx, manual, changed); err != nil {
		t.Fatal(err)
	}
	for _, item := range runtime() {
		if item.Config.Labels["com.docker.compose.service"] == "idle" {
			docker("stop", item.ID)
		}
	}
	fileJob, err = m.FileOperation(ctx, prefix, "recreate", "")
	if err != nil {
		t.Fatal(err)
	}
	run(fileJob, "succeeded")
	foundManual := false
	for _, entry := range web().Config.Env {
		if entry == "MANUAL_FILE=from-terminal" {
			foundManual = true
		}
	}
	if !foundManual {
		t.Fatal("manual multi-file override ignored")
	}
	for _, item := range runtime() {
		if item.Config.Labels["com.docker.compose.service"] == "idle" && item.State.Running {
			t.Fatal("Recreate started an originally stopped service")
		}
	}
	t.Log("PASS: multiple Compose files retain merge order; terminal edits apply and stopped services remain stopped")
	// An invalid application configuration must restore runtime without changing
	// the user's current files, which remain available to correct and retry.
	beforeBad := web()
	configuration, err = m.Configuration(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	var override string
	for _, file := range configuration.Files {
		if file.Path == "override.yaml" {
			override = file.Content
		}
	}
	badOverride := override + "    healthcheck: {test: [CMD, 'false'], interval: 1s, timeout: 1s, retries: 1}\n"
	if _, err := m.SaveConfiguration(ctx, prefix, ConfigurationEdit{Fingerprint: configuration.Fingerprint, Path: "override.yaml", Content: badOverride}); err != nil {
		t.Fatal(err)
	}
	fileJob, err = m.FileOperation(ctx, prefix, "recreate", "")
	if err != nil {
		t.Fatal(err)
	}
	badJob := run(fileJob, "failed")
	if badJob.Rollback != "restored" || web().Image != beforeBad.Image {
		t.Fatalf("file-based rollback failed: %s", badJob.Error)
	}
	afterBad, err := m.readFileSnapshot(ctx, prefix)
	if err != nil || afterBad.Files["override.yaml"] != badOverride {
		t.Fatal("rollback overwrote manual source files")
	}
	assertData()
	t.Log("PASS: file-based unhealthy rollback restores runtime and data while preserving the user's edited Compose")
	testConfigurationMigrationDocker(t, ctx, m, data, prefix, docker)
}
