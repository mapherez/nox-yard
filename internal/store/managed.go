package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type ManagedProject struct {
	Name          string
	SourceKind    string
	SourceURL     string
	Filename      string
	YAML          string
	VariablesJSON string
	EnvFilesJSON  string
	ProjectDir    string
}

type ManagedJob = Job

func (s *Store) ManagedProjects() ([]ManagedProject, error) {
	return s.ManagedProjectsContext(context.Background())
}

func (s *Store) ManagedProjectsContext(ctx context.Context) ([]ManagedProject, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, source_kind, source_url, filename, yaml, variables_json, env_files_json, project_dir FROM managed_projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []ManagedProject{}
	for rows.Next() {
		var project ManagedProject
		if err := rows.Scan(&project.Name, &project.SourceKind, &project.SourceURL, &project.Filename, &project.YAML, &project.VariablesJSON, &project.EnvFilesJSON, &project.ProjectDir); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) ManagedProject(name string) (ManagedProject, bool, error) {
	var project ManagedProject
	err := s.db.QueryRow(`SELECT name, source_kind, source_url, filename, yaml, variables_json, env_files_json, project_dir FROM managed_projects WHERE name = ?`, name).
		Scan(&project.Name, &project.SourceKind, &project.SourceURL, &project.Filename, &project.YAML, &project.VariablesJSON, &project.EnvFilesJSON, &project.ProjectDir)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedProject{}, false, nil
	}
	return project, err == nil, err
}

func (s *Store) ManagedByURL(sourceURL string) ([]ManagedProject, error) {
	rows, err := s.db.Query(`SELECT name, source_kind, source_url, filename, yaml, variables_json, env_files_json, project_dir FROM managed_projects WHERE source_url = ? ORDER BY name`, sourceURL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []ManagedProject{}
	for rows.Next() {
		var project ManagedProject
		if err := rows.Scan(&project.Name, &project.SourceKind, &project.SourceURL, &project.Filename, &project.YAML, &project.VariablesJSON, &project.EnvFilesJSON, &project.ProjectDir); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *Store) SaveManagedProject(project ManagedProject) error {
	return saveManagedProject(s.db, project)
}

func saveManagedProject(writer interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}, project ManagedProject) error {
	now := time.Now().Unix()
	var configurationState string
	if err := writer.QueryRow(`SELECT state FROM file_configurations WHERE name=? AND EXISTS(SELECT 1 FROM managed_projects WHERE name=? AND yaml='')`, project.Name, project.Name).Scan(&configurationState); err != nil && err != sql.ErrNoRows {
		return err
	}
	if configurationState == "ready" {
		project.YAML = ""
		project.VariablesJSON = "{}"
		project.EnvFilesJSON = "{}"
	}
	if project.EnvFilesJSON == "" {
		project.EnvFilesJSON = "{}"
	}
	_, err := writer.Exec(`INSERT INTO managed_projects (name, source_kind, source_url, filename, yaml, variables_json, env_files_json, project_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET source_kind = excluded.source_kind, source_url = excluded.source_url,
		filename = excluded.filename, yaml = excluded.yaml, variables_json = excluded.variables_json, env_files_json = excluded.env_files_json, project_dir = excluded.project_dir, updated_at = excluded.updated_at`,
		project.Name, project.SourceKind, project.SourceURL, project.Filename, project.YAML, project.VariablesJSON, project.EnvFilesJSON, project.ProjectDir, now, now)
	return err
}

func (s *Store) DeleteManagedProject(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM managed_projects WHERE name = ?", name); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM file_configurations WHERE name = ?", name); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitManagedJob fences metadata writes and resource release with the same
// terminal CAS. An expired observer/worker cannot overwrite a later project.
func (s *Store) CommitManagedJob(job Job, project *ManagedProject, remove bool, outcome string) error {
	if job.Domain != "managed" || (project != nil && project.Name != job.ProjectName) {
		return ErrJobChanged
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := finishJobTx(tx, job.ID, job.Owner, "succeeded", outcome, "", ""); err != nil {
		return err
	}
	if remove {
		if _, err := tx.Exec("DELETE FROM managed_projects WHERE name=?", job.ProjectName); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM file_configurations WHERE name=?", job.ProjectName); err != nil {
			return err
		}
	} else if project != nil {
		metadata := *project
		if project.ProjectDir != "" {
			// Verified deployment/adoption already wrote the authoritative files.
			// Commit location-only metadata with the terminal job CAS.
			metadata.YAML, metadata.VariablesJSON, metadata.EnvFilesJSON = "", "{}", "{}"
			var payload struct{ Context json.RawMessage }
			_ = json.Unmarshal([]byte(job.Payload), &payload)
			context := string(payload.Context)
			if context == "" || context == "null" {
				context = "{}"
			}
			if _, err := tx.Exec(`INSERT INTO file_configurations(name,directory,compose_files,state,error,context_json) VALUES(?,?,?,'ready','',?) ON CONFLICT(name) DO UPDATE SET directory=excluded.directory,compose_files=excluded.compose_files,state='ready',error='',context_json=CASE WHEN excluded.context_json='{}' THEN file_configurations.context_json ELSE excluded.context_json END`, project.Name, project.ProjectDir, `["compose.yml"]`, context); err != nil {
				return err
			}
		}
		if err := saveManagedProject(tx, metadata); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CreateManagedJob(job ManagedJob) error {
	if job.TargetID == "" {
		job.TargetID = "compose:" + job.ProjectName
	}
	if job.Domain == "" {
		job.Domain = "managed"
	}
	if job.Stage == "" {
		job.Stage = "queued"
	}
	if job.CreatedAt == 0 {
		job.CreatedAt = time.Now().Unix()
	}
	if job.UpdatedAt == 0 {
		job.UpdatedAt = job.CreatedAt
	}
	return s.CreateJob(job)
}

func (s *Store) FinishManagedJob(id, status, message string) error {
	outcome := "verified"
	if status == "failed" {
		outcome = "failed"
	}
	return s.FinishJob(id, "", status, outcome, message, "")
}

func (s *Store) ManagedJob(id string) (ManagedJob, bool, error) {
	return s.Job(id)
}
