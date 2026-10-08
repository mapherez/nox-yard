package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrOperationConflict = errors.New("target has an active operation or requires recovery")
var ErrJobChanged = errors.New("job ownership or recovery state changed")

type ImageIdentity struct {
	Service     string `json:"service,omitempty"`
	ContainerID string `json:"containerID,omitempty"`
	ImageID     string `json:"imageID"`
	Platform    string `json:"platform,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
}

// Job is the additive public operation contract. Payload and execution ownership
// are private: they may contain source files, interpolation values and tokens.
type Job struct {
	ID           string          `json:"id"`
	ProjectName  string          `json:"projectName"`
	TargetID     string          `json:"targetID"`
	Domain       string          `json:"domain"`
	Operation    string          `json:"operation"`
	Status       string          `json:"status"`
	Stage        string          `json:"stage"`
	Outcome      string          `json:"outcome,omitempty"`
	WorkerID     string          `json:"workerID,omitempty"`
	SourceImages []ImageIdentity `json:"sourceImages,omitempty"`
	TargetImages []ImageIdentity `json:"targetImages,omitempty"`
	Rollback     string          `json:"rollback,omitempty"`
	Error        string          `json:"error,omitempty"`
	CleanupError string          `json:"cleanupError,omitempty"`
	CreatedAt    int64           `json:"createdAt"`
	StartedAt    int64           `json:"startedAt,omitempty"`
	UpdatedAt    int64           `json:"updatedAt"`
	CompletedAt  int64           `json:"completedAt,omitempty"`
	DeadlineAt   int64           `json:"deadlineAt,omitempty"`
	Resources    []string        `json:"-"`
	Owner        string          `json:"-"`
	Payload      string          `json:"-"`
}

func migrateOperations(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS managed_jobs (id TEXT PRIMARY KEY, project_name TEXT NOT NULL, operation TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, completed_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS self_update_jobs (id TEXT PRIMARY KEY, status TEXT NOT NULL, old_image_id TEXT NOT NULL, target_image_id TEXT NOT NULL, target_digest TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, completed_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE operation_jobs (
    id TEXT PRIMARY KEY, project_name TEXT NOT NULL DEFAULT '', target_id TEXT NOT NULL, domain TEXT NOT NULL,
    operation TEXT NOT NULL, status TEXT NOT NULL, stage TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT '',
    worker_id TEXT NOT NULL DEFAULT '', owner TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '',
    resources_json TEXT NOT NULL DEFAULT '[]', source_images_json TEXT NOT NULL DEFAULT '[]', target_images_json TEXT NOT NULL DEFAULT '[]',
    rollback TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', cleanup_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL, started_at INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, completed_at INTEGER NOT NULL DEFAULT 0, deadline_at INTEGER NOT NULL DEFAULT 0, worker_cleaned_at INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX operation_jobs_target ON operation_jobs(target_id, created_at DESC)`,
		`CREATE TABLE operation_locks (resource TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES operation_jobs(id))`,
		`INSERT INTO operation_jobs (id, project_name, target_id, domain, operation, status, stage, error, created_at, updated_at, completed_at)
    SELECT id, project_name, 'compose:' || project_name, 'managed', operation, status,
    CASE WHEN status = 'running' THEN 'legacy_reconciliation' ELSE 'completed' END,
    CASE WHEN error='' THEN '' ELSE 'Previous managed operation failed; inspect the target on the host.' END, created_at, created_at, completed_at FROM managed_jobs`,
		// A previous version could have admitted overlapping jobs. Retain one lock
		// per resource; all legacy running outcomes still require reconciliation.
		`INSERT INTO operation_locks(resource, job_id) SELECT target_id, MIN(id) FROM operation_jobs WHERE status = 'running' GROUP BY target_id`,
		`INSERT INTO operation_jobs (id,target_id,domain,operation,status,stage,worker_id,source_images_json,target_images_json,error,created_at,updated_at,completed_at)
		 SELECT id,'compose:nox-yard','self-update','update',CASE WHEN status='updating' THEN 'running' ELSE status END,
		 CASE WHEN status='updating' THEN 'legacy_reconciliation' ELSE 'completed' END,'nox-yard-update-' || id,
		 json_array(json_object('imageID',old_image_id)),json_array(json_object('imageID',target_image_id)),
		 CASE WHEN error='' THEN '' ELSE 'Previous self-update failed; inspect its dedicated status.' END,created_at,created_at,completed_at FROM self_update_jobs`,
		`INSERT OR IGNORE INTO operation_locks(resource,job_id) SELECT 'yard:self',id FROM operation_jobs WHERE domain='self-update' AND status='running' LIMIT 1`,
		`PRAGMA user_version = 7`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate operation jobs: %w", err)
		}
	}
	return tx.Commit()
}

func NewJob(target, domain, operation string, resources []string) (Job, error) {
	var id, owner [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Job{}, err
	}
	if _, err := rand.Read(owner[:]); err != nil {
		return Job{}, err
	}
	now := time.Now().Unix()
	return Job{ID: hex.EncodeToString(id[:]), Owner: hex.EncodeToString(owner[:]), TargetID: target, Domain: domain,
		Operation: operation, Status: "running", Stage: "queued", CreatedAt: now, UpdatedAt: now, DeadlineAt: now + 600, Resources: resources}, nil
}

func insertJob(tx *sql.Tx, job Job) error {
	resources := append([]string{job.TargetID}, job.Resources...)
	sort.Strings(resources)
	unique := resources[:0]
	for _, key := range resources {
		if key != "" && (len(unique) == 0 || unique[len(unique)-1] != key) {
			unique = append(unique, key)
		}
	}
	encode := func(value any) string { data, _ := json.Marshal(value); return string(data) }
	_, err := tx.Exec(`INSERT INTO operation_jobs (id, project_name, target_id, domain, operation, status, stage, worker_id, owner, payload,
 resources_json, source_images_json, target_images_json, created_at, updated_at, deadline_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		job.ID, job.ProjectName, job.TargetID, job.Domain, job.Operation, job.Status, job.Stage, job.WorkerID, job.Owner, job.Payload,
		encode(unique), encode(job.SourceImages), encode(job.TargetImages), job.CreatedAt, job.UpdatedAt, job.DeadlineAt)
	if err != nil {
		return err
	}
	for _, key := range unique {
		result, err := tx.Exec(`INSERT INTO operation_locks(resource,job_id) VALUES (?,?) ON CONFLICT(resource) DO NOTHING`, key, job.ID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrOperationConflict
		}
	}
	return nil
}

func (s *Store) CreateJob(job Job) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertJob(tx, job); err != nil {
		return err
	}
	return tx.Commit()
}

const jobColumns = `id, project_name, target_id, domain, operation, status, stage, outcome, worker_id, owner, payload,
 resources_json, source_images_json, target_images_json, rollback, error, cleanup_error, created_at, started_at, updated_at, completed_at, deadline_at`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var job Job
	var resources, source, target string
	err := row.Scan(&job.ID, &job.ProjectName, &job.TargetID, &job.Domain, &job.Operation, &job.Status, &job.Stage, &job.Outcome, &job.WorkerID, &job.Owner, &job.Payload,
		&resources, &source, &target, &job.Rollback, &job.Error, &job.CleanupError, &job.CreatedAt, &job.StartedAt, &job.UpdatedAt, &job.CompletedAt, &job.DeadlineAt)
	if err != nil {
		return job, err
	}
	for _, item := range []struct {
		raw   string
		value any
	}{{resources, &job.Resources}, {source, &job.SourceImages}, {target, &job.TargetImages}} {
		if err := json.Unmarshal([]byte(item.raw), item.value); err != nil {
			return Job{}, err
		}
	}
	return job, nil
}

func (s *Store) Job(id string) (Job, bool, error) {
	job, err := scanJob(s.db.QueryRow("SELECT "+jobColumns+" FROM operation_jobs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, false, nil
	}
	return job, err == nil, err
}

func (s *Store) Jobs(ctx context.Context, target string, active bool) ([]Job, error) {
	query := "SELECT " + jobColumns + " FROM operation_jobs WHERE (? = '' OR target_id = ? OR EXISTS (SELECT 1 FROM json_each(resources_json) WHERE value = ?))"
	if active {
		query += " AND (status = 'running' OR outcome = 'recovery_required')"
	}
	query += " ORDER BY created_at DESC, rowid DESC"
	if !active {
		query += " LIMIT 30"
	}
	rows, err := s.db.QueryContext(ctx, query, target, target, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) SetJobWorker(id, owner, worker string) error {
	result, err := s.db.Exec(`UPDATE operation_jobs SET worker_id=?, stage='launching',started_at=0,updated_at=? WHERE id=? AND owner=? AND status='running' AND (started_at=0 OR (domain='engine' AND stage='executing' AND worker_id=''))`, worker, time.Now().Unix(), id, owner)
	return changed(result, err)
}
func (s *Store) JobPayload(id, owner, payload string) error {
	result, err := s.db.Exec(`UPDATE operation_jobs SET payload=?,updated_at=? WHERE id=? AND owner=? AND status='running'`, payload, time.Now().Unix(), id, owner)
	return changed(result, err)
}
func (s *Store) ClaimJob(id, owner string) error {
	result, err := s.db.Exec(`UPDATE operation_jobs SET started_at=?, stage='preparing',updated_at=? WHERE id=? AND owner=? AND status='running' AND started_at=0 AND stage='launching'`, time.Now().Unix(), time.Now().Unix(), id, owner)
	return changed(result, err)
}
func (s *Store) JobProgress(id, owner, stage string, source, target []ImageIdentity) error {
	sourceJSON, _ := json.Marshal(source)
	targetJSON, _ := json.Marshal(target)
	result, err := s.db.Exec(`UPDATE operation_jobs SET stage=?, updated_at=?,started_at=CASE WHEN started_at=0 AND ?='executing' THEN unixepoch() ELSE started_at END,source_images_json=CASE WHEN ? THEN ? ELSE source_images_json END,target_images_json=CASE WHEN ? THEN ? ELSE target_images_json END WHERE id=? AND owner=? AND status='running'`,
		stage, time.Now().Unix(), stage, source != nil, string(sourceJSON), target != nil, string(targetJSON), id, owner)
	return changed(result, err)
}
func changed(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrJobChanged
	}
	return nil
}

func (s *Store) FinishJob(id, owner, status, outcome, message, rollback string) error {
	if status != "succeeded" && status != "failed" {
		return fmt.Errorf("invalid final operation status")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := finishJobTx(tx, id, owner, status, outcome, message, rollback); err != nil {
		return err
	}
	return tx.Commit()
}

func finishJobTx(tx *sql.Tx, id, owner, status, outcome, message, rollback string) error {
	stage := "completed"
	if outcome == "recovery_required" {
		stage = "awaiting_recovery"
	}
	result, err := tx.Exec(`UPDATE operation_jobs SET status=?, outcome=?,stage=?,error=?,rollback=?,updated_at=?,completed_at=? WHERE id=? AND status='running' AND (?='' OR owner=?)`, status, outcome, stage, message, rollback, time.Now().Unix(), time.Now().Unix(), id, owner, owner)
	if err := changed(result, err); err != nil {
		return err
	}
	if outcome != "recovery_required" {
		if _, err := tx.Exec("DELETE FROM operation_locks WHERE job_id=?", id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) JobCleanupError(id, message string) error {
	_, err := s.db.Exec("UPDATE operation_jobs SET cleanup_error=? WHERE id=?", message, id)
	return err
}

func (s *Store) WorkersForCleanup(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+jobColumns+" FROM operation_jobs WHERE status!='running' AND worker_id!='' AND worker_cleaned_at=0 AND domain!='self-update'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}
func (s *Store) WorkerCleaned(id string) error {
	_, err := s.db.Exec("UPDATE operation_jobs SET worker_cleaned_at=?,cleanup_error='' WHERE id=?", time.Now().Unix(), id)
	return err
}
func (s *Store) AcknowledgeRecovery(id string, updatedAt int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE operation_jobs SET outcome='recovery_acknowledged',stage='completed',worker_cleaned_at=0,updated_at=? WHERE id=? AND status='failed' AND outcome='recovery_required' AND updated_at=?`, time.Now().Unix(), id, updatedAt)
	if err := changed(result, err); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM operation_locks WHERE job_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
