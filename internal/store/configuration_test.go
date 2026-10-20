package store

import (
	"errors"
	"testing"
)

func TestVerifiedDeploymentCommitsFileLocationWithJob(t *testing.T) {
	data, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	project := ManagedProject{Name: "sample", ProjectDir: "/srv/sample", YAML: "private source", VariablesJSON: `{"TOKEN":"private"}`, EnvFilesJSON: `{"app.env":"private"}`}
	if err := data.SaveManagedProject(project); err != nil {
		t.Fatal(err)
	}
	job, _ := NewJob("compose:sample", "managed", "new", nil)
	job.ProjectName = project.Name
	if err := data.CreateJob(job); err != nil {
		t.Fatal(err)
	}
	if err := data.CommitManagedJob(job, &project, false, "verified"); err != nil {
		t.Fatal(err)
	}
	saved, found, err := data.ManagedProject(project.Name)
	if err != nil || !found || saved.YAML != "" || saved.VariablesJSON != "{}" || saved.EnvFilesJSON != "{}" {
		t.Fatalf("active file contents retained in metadata: %+v %v", saved, err)
	}
	location, found, err := data.FileConfiguration(project.Name)
	if err != nil || !found || location.Directory != project.ProjectDir || location.ComposeFiles != `["compose.yml"]` {
		t.Fatalf("file location missing: %+v %v", location, err)
	}
	project.ProjectDir = "/wrong"
	if err := data.CommitManagedJob(job, &project, false, "reconciled"); !errors.Is(err, ErrJobChanged) {
		t.Fatal("stale job rewrote metadata", err)
	}
	location, _, _ = data.FileConfiguration(project.Name)
	if location.Directory != "/srv/sample" {
		t.Fatal("stale job overwrote file location")
	}
}
