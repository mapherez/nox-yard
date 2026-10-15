package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFileMigrationRequiresVerifiedRecoverableExport(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	project := ManagedProject{Name: "sample", ProjectDir: "/srv/sample", YAML: "services: {app: {image: alpine}}", VariablesJSON: `{"TOKEN":"private-value"}`, EnvFilesJSON: "{}"}
	if err := data.SaveManagedProject(project); err != nil {
		t.Fatal(err)
	}
	location := FileConfiguration{Name: project.Name, Directory: project.ProjectDir, ComposeFiles: `["compose.yml"]`, State: "ready"}
	proof := strings.Repeat("a", 64)
	if err := data.CompleteFileMigration(project, location, "missing", proof); err == nil {
		t.Fatal("cleared data without backup")
	}
	backup, _ := NewJob("compose:sample", "managed", "migrate_configuration", nil)
	backup.ProjectName = "sample"
	encoded, _ := json.Marshal(struct {
		Previous ManagedProject
		Exported any
	}{project, map[string]any{"Fingerprint": proof, "Directory": project.ProjectDir, "Files": map[string]string{"compose.yml": project.YAML, ".env": "TOKEN=private-value\n"}}})
	backup.Payload = string(encoded)
	if err := data.CreateJob(backup); err != nil {
		t.Fatal(err)
	}
	if err := data.CompleteFileMigration(project, location, backup.ID, proof); err == nil {
		t.Fatal("accepted incomplete verification job")
	}
	if err := data.FinishJob(backup.ID, backup.Owner, "succeeded", "verified", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := data.CompleteFileMigration(project, location, backup.ID, strings.Repeat("b", 64)); err == nil {
		t.Fatal("accepted mismatched file proof")
	}
	before, _, _ := data.ManagedProject("sample")
	if before.YAML != project.YAML {
		t.Fatal("failed verification deleted source")
	}
	if err := data.CompleteFileMigration(project, location, backup.ID, proof); err != nil {
		t.Fatal(err)
	}
	after, _, _ := data.ManagedProject("sample")
	if after.YAML != "" || after.VariablesJSON != "{}" || after.EnvFilesJSON != "{}" {
		t.Fatal("legacy active contents remain")
	}
	if err := data.SaveManagedProject(project); err != nil {
		t.Fatal(err)
	}
	after, _, _ = data.ManagedProject("sample")
	if after.YAML != "" {
		t.Fatal("normal metadata save recreated a database configuration copy")
	}
	history, _ := data.Jobs(t.Context(), "compose:sample", false)
	public, _ := json.Marshal(history)
	if strings.Contains(string(public), "private-value") {
		t.Fatal("migration recovery values leaked into history")
	}
}
