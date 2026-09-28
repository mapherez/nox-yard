package store

import (
	"database/sql"
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

type ManagedJob struct {
	ID          string `json:"id"`
	ProjectName string `json:"projectName"`
	Operation   string `json:"operation"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	CompletedAt int64  `json:"completedAt,omitempty"`
}

func (s *Store) ManagedProjects() ([]ManagedProject, error) {
	rows, err := s.db.Query(`SELECT name, source_kind, source_url, filename, yaml, variables_json, env_files_json, project_dir FROM managed_projects ORDER BY name`)
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
	now := time.Now().Unix()
	if project.EnvFilesJSON == "" {
		project.EnvFilesJSON = "{}"
	}
	_, err := s.db.Exec(`INSERT INTO managed_projects (name, source_kind, source_url, filename, yaml, variables_json, env_files_json, project_dir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET source_kind = excluded.source_kind, source_url = excluded.source_url,
		filename = excluded.filename, yaml = excluded.yaml, variables_json = excluded.variables_json, env_files_json = excluded.env_files_json, project_dir = excluded.project_dir, updated_at = excluded.updated_at`,
		project.Name, project.SourceKind, project.SourceURL, project.Filename, project.YAML, project.VariablesJSON, project.EnvFilesJSON, project.ProjectDir, now, now)
	return err
}

func (s *Store) DeleteManagedProject(name string) error {
	_, err := s.db.Exec("DELETE FROM managed_projects WHERE name = ?", name)
	return err
}

func (s *Store) CreateManagedJob(job ManagedJob) error {
	_, err := s.db.Exec(`INSERT INTO managed_jobs (id, project_name, operation, status, created_at) VALUES (?, ?, ?, ?, ?)`,
		job.ID, job.ProjectName, job.Operation, job.Status, time.Now().Unix())
	return err
}

func (s *Store) FinishManagedJob(id, status, message string) error {
	_, err := s.db.Exec(`UPDATE managed_jobs SET status = ?, error = ?, completed_at = ? WHERE id = ?`, status, message, time.Now().Unix(), id)
	return err
}

func (s *Store) ManagedJob(id string) (ManagedJob, bool, error) {
	var job ManagedJob
	err := s.db.QueryRow(`SELECT id, project_name, operation, status, error, created_at, completed_at FROM managed_jobs WHERE id = ?`, id).
		Scan(&job.ID, &job.ProjectName, &job.Operation, &job.Status, &job.Error, &job.CreatedAt, &job.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedJob{}, false, nil
	}
	return job, err == nil, err
}

func (s *Store) InterruptManagedJobs() error {
	_, err := s.db.Exec(`UPDATE managed_jobs SET status = 'failed', error = 'Service restarted while the operation was running.', completed_at = ? WHERE status = 'running'`, time.Now().Unix())
	return err
}
