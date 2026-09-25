package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

type SelfUpdateSettings struct {
	Automatic      bool
	LastCheckedAt  int64
	LastCheckError string
	FailedDigest   string
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
}

func (s *Store) SelfUpdateSettings() (SelfUpdateSettings, error) {
	var settings SelfUpdateSettings
	err := s.db.QueryRow(`SELECT automatic, last_checked_at, last_check_error, failed_digest
		FROM self_update_settings WHERE id = 1`).Scan(
		&settings.Automatic, &settings.LastCheckedAt, &settings.LastCheckError, &settings.FailedDigest)
	return settings, err
}

func (s *Store) SetAutomaticUpdates(enabled bool) error {
	_, err := s.db.Exec(`UPDATE self_update_settings SET automatic = ?,
		failed_digest = CASE WHEN automatic = 0 AND ? THEN '' ELSE failed_digest END
		WHERE id = 1`, enabled, enabled)
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
	_, err := s.db.Exec(`INSERT INTO self_update_jobs
		(id, status, old_image_id, target_image_id, target_digest, created_at)
		VALUES (?, 'updating', ?, ?, ?, ?)`,
		job.ID, job.OldImageID, job.TargetImageID, job.TargetDigest, time.Now().Unix())
	return err
}

func (s *Store) FinishSelfUpdateJob(id, status, message string) error {
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
