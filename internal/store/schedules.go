package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var ErrScheduleChanged = errors.New("schedule was disabled, changed or already consumed")

const AutomaticResource = "scheduler:projects"

type ProjectSchedule struct {
	Key               string `json:"-"`
	TargetID          string `json:"targetID"`
	Enabled           bool   `json:"enabled"`
	NextAt            int64  `json:"nextAt,omitempty"`
	LastAt            int64  `json:"lastAt,omitempty"`
	LastOutcome       string `json:"lastOutcome,omitempty"`
	LastReason        string `json:"lastReason,omitempty"`
	LastJobID         string `json:"lastJobID,omitempty"`
	FailedFingerprint string `json:"-"`
}
type ScheduleOccurrence struct {
	Key    string
	DueAt  int64
	Date   string
	NextAt int64
}
type scheduledContext struct{}

func WithSchedule(ctx context.Context, occurrence ScheduleOccurrence) context.Context {
	return context.WithValue(ctx, scheduledContext{}, occurrence)
}
func ScheduledJob(ctx context.Context, job Job) Job {
	if occurrence, ok := ctx.Value(scheduledContext{}).(ScheduleOccurrence); ok {
		job.ScheduleKey, job.ScheduledFor, job.scheduleDate, job.scheduleNext = occurrence.Key, occurrence.DueAt, occurrence.Date, occurrence.NextAt
		job.Resources = append(job.Resources, AutomaticResource)
	}
	return job
}
func migrateSchedules(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER TABLE operation_jobs ADD COLUMN schedule_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE operation_jobs ADD COLUMN scheduled_for INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE operation_jobs ADD COLUMN candidate_fingerprint TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE project_schedules (key TEXT PRIMARY KEY, target_id TEXT NOT NULL UNIQUE, enabled INTEGER NOT NULL DEFAULT 0, next_at INTEGER NOT NULL DEFAULT 0, last_at INTEGER NOT NULL DEFAULT 0, last_outcome TEXT NOT NULL DEFAULT '', last_reason TEXT NOT NULL DEFAULT '', last_job_id TEXT NOT NULL DEFAULT '', failed_fingerprint TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX project_schedules_due ON project_schedules(enabled,next_at,key)`,
		`CREATE INDEX scheduled_workers_cleanup ON operation_jobs(worker_cleaned_at) WHERE scheduled_for>0 AND worker_id!=''`,
		`CREATE TABLE schedule_occurrences (schedule_key TEXT NOT NULL REFERENCES project_schedules(key), date TEXT NOT NULL, due_at INTEGER NOT NULL, job_id TEXT NOT NULL UNIQUE REFERENCES operation_jobs(id), PRIMARY KEY(schedule_key,date))`,
		`PRAGMA user_version = 8`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const scheduleColumns = `key,target_id,enabled,next_at,last_at,last_outcome,last_reason,last_job_id,failed_fingerprint`

func scanSchedule(row interface{ Scan(...any) error }) (ProjectSchedule, error) {
	var value ProjectSchedule
	err := row.Scan(&value.Key, &value.TargetID, &value.Enabled, &value.NextAt, &value.LastAt, &value.LastOutcome, &value.LastReason, &value.LastJobID, &value.FailedFingerprint)
	return value, err
}
func (s *Store) ProjectSchedule(ctx context.Context, target string) (ProjectSchedule, bool, error) {
	value, err := scanSchedule(s.db.QueryRowContext(ctx, "SELECT "+scheduleColumns+" FROM project_schedules WHERE key=? OR target_id=?", target, target))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectSchedule{Key: target, TargetID: target}, false, nil
	}
	return value, err == nil, err
}
func (s *Store) SetProjectSchedule(ctx context.Context, key, target string, enabled bool, next int64) (ProjectSchedule, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_schedules(key,target_id,enabled,next_at) VALUES(?,?,?,?) ON CONFLICT(key) DO UPDATE SET target_id=excluded.target_id,enabled=excluded.enabled,next_at=CASE WHEN project_schedules.enabled=excluded.enabled THEN project_schedules.next_at ELSE excluded.next_at END`, key, target, enabled, next)
	if err != nil {
		return ProjectSchedule{}, err
	}
	value, _, err := s.ProjectSchedule(ctx, key)
	return value, err
}
func (s *Store) DueSchedules(ctx context.Context, now int64) ([]ProjectSchedule, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+scheduleColumns+" FROM project_schedules WHERE enabled=1 AND next_at<=? ORDER BY next_at,key LIMIT 4", now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []ProjectSchedule{}
	for rows.Next() {
		value, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *Store) AutomaticBusy(ctx context.Context) (bool, error) {
	var busy bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM operation_locks WHERE resource=?) OR EXISTS(SELECT 1 FROM operation_jobs WHERE scheduled_for>0 AND worker_id!='' AND worker_cleaned_at=0)`, AutomaticResource).Scan(&busy)
	return busy, err
}

// Deferral keeps the occurrence due for a single latest-slot catch-up.
func (s *Store) DeferDueSchedules(ctx context.Context, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE project_schedules SET last_at=?,last_job_id='',last_outcome='deferred',last_reason='Another automatic update is running, pending cleanup or awaiting recovery.' WHERE enabled=1 AND next_at<=? AND last_outcome!='deferred'`, now, now)
	return err
}
func (s *Store) CoalesceOccurrence(ctx context.Context, key, date string, next int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE project_schedules SET next_at=? WHERE key=? AND enabled=1 AND next_at<? AND EXISTS(SELECT 1 FROM schedule_occurrences WHERE schedule_key=? AND date=?)`, next, key, next, key, date)
	return err
}
func claimSchedule(tx *sql.Tx, job Job) error {
	if job.ScheduleKey == "" {
		return nil
	}
	// Admission also fences finished workers awaiting cleanup. The global lock
	// itself guards running/recovery work; this closes the preflight/finish race.
	if job.Status == "running" {
		var pending bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM operation_jobs WHERE id!=? AND scheduled_for>0 AND worker_id!='' AND worker_cleaned_at=0)`, job.ID).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrOperationConflict
		}
	}
	result, err := tx.Exec(`UPDATE project_schedules SET next_at=?,last_at=?,last_outcome=?,last_reason=?,last_job_id=? WHERE key=? AND target_id=? AND enabled=1 AND next_at<=?`, job.scheduleNext, job.ScheduledFor, "running", job.Error, job.ID, job.ScheduleKey, job.TargetID, job.ScheduledFor)
	if err := changed(result, err); err != nil {
		if errors.Is(err, ErrJobChanged) {
			return ErrScheduleChanged
		}
		return err
	}
	var consumed bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM schedule_occurrences WHERE schedule_key=? AND date=?)`, job.ScheduleKey, job.scheduleDate).Scan(&consumed); err != nil {
		return err
	}
	if consumed {
		return ErrScheduleChanged
	}
	_, err = tx.Exec(`INSERT INTO schedule_occurrences(schedule_key,date,due_at,job_id) VALUES(?,?,?,?)`, job.ScheduleKey, job.scheduleDate, job.ScheduledFor, job.ID)
	return err
}
func (s *Store) SkipSchedule(ctx context.Context, row ProjectSchedule, occurrence ScheduleOccurrence, reason string) error {
	job, err := NewJob(row.TargetID, "schedule", "update", nil)
	if err != nil {
		return err
	}
	job = ScheduledJob(WithSchedule(ctx, occurrence), job)
	job.Status, job.Stage, job.Outcome, job.Error = "succeeded", "completed", "skipped", reason
	job.StartedAt, job.CompletedAt = time.Now().Unix(), time.Now().Unix()
	return s.CreateJob(job)
}
func ImageFingerprint(images []ImageIdentity) string {
	values := []string{}
	seen := map[string]bool{}
	for _, image := range images {
		if image.ImageID == "" {
			continue
		}
		value := image.Service + "|" + image.Platform + "|" + image.ImageID
		if !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	if len(values) == 0 {
		return ""
	}
	sort.Strings(values)
	encoded, _ := json.Marshal(values)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// Workers check again after pulling and immediately before replacement. A
// manual update records its candidate too, but bypasses automatic suppression.
func (s *Store) UpdateCandidate(job Job, images []ImageIdentity) (string, error) {
	signature := ImageFingerprint(images)
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE operation_jobs SET candidate_fingerprint=? WHERE id=? AND owner=? AND status='running'`, signature, job.ID, job.Owner)
	if err := changed(result, err); err != nil {
		return "", err
	}
	reason := ""
	if job.ScheduleKey != "" {
		var enabled bool
		var failed string
		if err := tx.QueryRow(`SELECT enabled,failed_fingerprint FROM project_schedules WHERE key=?`, job.ScheduleKey).Scan(&enabled, &failed); err != nil {
			return "", err
		}
		if !enabled {
			reason = "Automatic updates were disabled before replacement."
		} else if signature != "" && signature == failed {
			reason = "The same image set previously failed. Waiting for new images or an explicit manual retry."
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return reason, nil
}
func (s *Store) ScheduleEnabled(job Job) (bool, error) {
	if job.ScheduleKey == "" {
		return true, nil
	}
	var enabled bool
	err := s.db.QueryRow(`SELECT enabled FROM project_schedules WHERE key=?`, job.ScheduleKey).Scan(&enabled)
	return enabled, err
}
func finishSchedule(tx *sql.Tx, id, status, outcome, message string) error {
	job, err := scanJob(tx.QueryRow("SELECT "+jobColumns+" FROM operation_jobs WHERE id=?", id))
	if err != nil {
		return err
	}
	if job.ScheduleKey != "" {
		_, err = tx.Exec(`UPDATE project_schedules SET last_outcome=?,last_reason=?,target_id=? WHERE key=? AND last_job_id=?`, outcome, message, job.TargetID, job.ScheduleKey, id)
		if err != nil {
			return err
		}
	}
	if job.Operation == "remove" && status == "succeeded" {
		_, err = tx.Exec(`UPDATE project_schedules SET enabled=0,next_at=0 WHERE target_id=? OR key IN (SELECT value FROM json_each(?))`, job.TargetID, mustJSON(job.Resources))
		if err != nil {
			return err
		}
	}
	if job.Operation != "update" || job.CandidateFingerprint == "" {
		return nil
	}
	// Failure records the immutable candidate before rollback overwrites public
	// image results. Successful explicit retries clear the failed-image fence.
	if status == "failed" {
		_, err = tx.Exec(`UPDATE project_schedules SET failed_fingerprint=? WHERE key=? OR target_id=? OR key IN (SELECT value FROM json_each(?))`, job.CandidateFingerprint, job.ScheduleKey, job.TargetID, mustJSON(job.Resources))
	} else if status == "succeeded" && (outcome == "verified" || outcome == "reconciled" || outcome == "unchanged") {
		_, err = tx.Exec(`UPDATE project_schedules SET failed_fingerprint='' WHERE key=? OR target_id=? OR key IN (SELECT value FROM json_each(?))`, job.ScheduleKey, job.TargetID, mustJSON(job.Resources))
	}
	return err
}
func mustJSON(value any) string { encoded, _ := json.Marshal(value); return string(encoded) }
