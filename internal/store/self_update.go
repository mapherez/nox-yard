package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

type SelfUpdateSettings struct {
	Automatic            bool
	CheckIntervalMinutes int
	LastCheckedAt        int64
	LastCheckError       string
	FailedDigest         string
}

type SelfUpdateJob struct {
	ID            string
	Status        string
	OldImageID    string
	TargetImageID string
	TargetDigest  string
	Error         string
	CreatedAt     int64
	CompletedAt   int64
	ProjectName   string
	ContainerID   string
}

func (s *Store) SelfUpdateSettings() (SelfUpdateSettings, error) {
	var settings SelfUpdateSettings
	err := s.db.QueryRow(`SELECT automatic, check_interval_minutes, last_checked_at, last_check_error, failed_digest
		FROM self_update_settings WHERE id = 1`).Scan(
		&settings.Automatic, &settings.CheckIntervalMinutes, &settings.LastCheckedAt,
		&settings.LastCheckError, &settings.FailedDigest)
	return settings, err
}

func (s *Store) SetSelfUpdateSettings(enabled bool, intervalMinutes int) error {
	_, err := s.db.Exec(`UPDATE self_update_settings SET automatic = ?, check_interval_minutes = ?,
		failed_digest = CASE WHEN automatic = 0 AND ? THEN '' ELSE failed_digest END
		WHERE id = 1`, enabled, intervalMinutes, enabled)
	return err
}

func (s *Store) ClearSelfUpdateFailure() error {
	_, err := s.db.Exec("UPDATE self_update_settings SET failed_digest = '' WHERE id = 1")
	return err
}

func (s *Store) RecordSelfUpdateCheck(message string) error {
	_, err := s.db.Exec(`UPDATE self_update_settings
		SET last_checked_at = ?, last_check_error = ? WHERE id = 1`, time.Now().Unix(), message)
	return err
}

func (s *Store) LatestSelfUpdateJob() (SelfUpdateJob, bool, error) {
	var job SelfUpdateJob
	err := s.db.QueryRow(`SELECT id, status, old_image_id, target_image_id, target_digest,
		error, created_at, completed_at FROM self_update_jobs ORDER BY rowid DESC LIMIT 1`).Scan(
		&job.ID, &job.Status, &job.OldImageID, &job.TargetImageID, &job.TargetDigest,
		&job.Error, &job.CreatedAt, &job.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SelfUpdateJob{}, false, nil
	}
	return job, err == nil, err
}

func (s *Store) SelfUpdateJob(id string) (SelfUpdateJob, error) {
	var job SelfUpdateJob
	err := s.db.QueryRow(`SELECT id, status, old_image_id, target_image_id, target_digest,
		error, created_at, completed_at FROM self_update_jobs WHERE id = ?`, id).Scan(
		&job.ID, &job.Status, &job.OldImageID, &job.TargetImageID, &job.TargetDigest,
		&job.Error, &job.CreatedAt, &job.CompletedAt)
	return job, err
}

func (s *Store) CreateSelfUpdateJob(job SelfUpdateJob) error {
	target := "compose:" + job.ProjectName
	if job.ProjectName == "" {
		target = "compose:nox-yard"
	}
	operation, err := NewJob(target, "self-update", "update", []string{"yard:self"})
	if err != nil {
		return err
	}
	operation.ID = job.ID
	operation.ProjectName = job.ProjectName
	operation.Stage = "pulling"
	operation.WorkerID = "nox-yard-update-" + job.ID
	operation.SourceImages = []ImageIdentity{{ContainerID: job.ContainerID, ImageID: job.OldImageID}}
	operation.TargetImages = []ImageIdentity{{ImageID: job.TargetImageID}}
	if job.ContainerID != "" {
		operation.Resources = append(operation.Resources, "container:"+job.ContainerID)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertJob(tx, operation); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO self_update_jobs
		(id, status, old_image_id, target_image_id, target_digest, created_at)
		VALUES (?, 'updating', ?, ?, ?, ?)`,
		job.ID, job.OldImageID, job.TargetImageID, job.TargetDigest, time.Now().Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishSelfUpdateJob(id, status, message string) error {
	return s.FinishSelfUpdateOutcome(id, status, message, false, "")
}

func (s *Store) FinishSelfUpdateOutcome(id, status, message string, recovery bool, rollback string) error {
	if status != "succeeded" && status != "failed" {
		return fmt.Errorf("invalid self-update job status %q", status)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE self_update_jobs SET status = ?, error = ?, completed_at = ?
		WHERE id = ? AND status = 'updating'`, status, message, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("self-update job %s is not active", id)
	}
	failedDigest := ""
	if status == "failed" {
		if err := tx.QueryRow("SELECT target_digest FROM self_update_jobs WHERE id = ?", id).Scan(&failedDigest); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE self_update_settings
		SET failed_digest = ?, last_check_error = '' WHERE id = 1`, failedDigest); err != nil {
		return err
	}
	messageSafe := ""
	outcome := "verified"
	if status == "failed" {
		messageSafe = "Self-update failed. Inspect the dedicated self-update status and retained rollback resources on the host."
		outcome = "failed"
	}
	stage := "completed"
	if recovery {
		outcome = "recovery_required"
		stage = "awaiting_recovery"
		messageSafe = "Self-update completion or rollback is uncertain. Inspect the running container, SQLite snapshot and retained rollback resources on the host, then acknowledge recovery."
	}
	if _, err := tx.Exec(`UPDATE operation_jobs SET status=?,stage=?,outcome=?,error=?,rollback=?,completed_at=?,updated_at=? WHERE id=? AND domain='self-update'`, status, stage, outcome, messageSafe, rollback, time.Now().Unix(), time.Now().Unix(), id); err != nil {
		return err
	}
	if !recovery {
		if _, err := tx.Exec("DELETE FROM operation_locks WHERE job_id=?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) BackupForSelfUpdate(path string) error {
	if _, err := s.db.Exec("VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("create SQLite rollback snapshot: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}
