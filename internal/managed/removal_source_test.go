package managed

import "testing"

func TestRemovalConfirmationSurvivesWorkerObservationFreeze(t *testing.T) {
	before := FileSnapshot{Directory: "/projects/sample", ComposeFiles: []string{"compose.yml"}, Files: map[string]string{"compose.yml": "services: {}"}, Context: ComposeContext{ObservedLabels: "observed-before-job", Revision: 2}}
	worker := before
	worker.Context.ObservedLabels = ""
	if removalSourceFingerprint(before) != removalSourceFingerprint(worker) {
		t.Fatal("accepting the worker invalidated its own removal confirmation")
	}
	changed := worker
	changed.Context.Revision++
	if removalSourceFingerprint(before) == removalSourceFingerprint(changed) {
		t.Fatal("source selection revision was not bound")
	}
	changed = worker
	changed.Files = map[string]string{"compose.yml": "services: {replacement: {image: alpine}}"}
	if removalSourceFingerprint(before) == removalSourceFingerprint(changed) {
		t.Fatal("changed Compose source was not bound")
	}
	changed = worker
	changed.Directory = "/projects/other"
	if removalSourceFingerprint(before) == removalSourceFingerprint(changed) {
		t.Fatal("changed source directory was not bound")
	}
}
