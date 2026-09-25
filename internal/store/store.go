package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrAdminExists = errors.New("administrator already exists")
	ErrNoAdmin     = errors.New("administrator is not configured")
)

type Store struct {
	db *sql.DB
}

type Admin struct {
	Username     string
	PasswordHash string
}

type Session struct {
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}

	path := filepath.Join(dataDir, "nox-yard.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure database: %w", err)
		}
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure database: %w", err)
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > 3 {
		return fmt.Errorf("database schema version %d is newer than this application", version)
	}
	if version == 3 {
		return nil
	}
	if version == 0 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, statement := range []string{
			`CREATE TABLE administrators (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
			`CREATE TABLE sessions (
			token_hash TEXT PRIMARY KEY,
			csrf_token TEXT NOT NULL,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
			"CREATE INDEX sessions_expiry ON sessions (expires_at)",
			"PRAGMA user_version = 1",
		} {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("migrate database: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if version < 2 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, statement := range []string{
			`CREATE TABLE self_update_settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			automatic INTEGER NOT NULL DEFAULT 0,
			last_checked_at INTEGER NOT NULL DEFAULT 0,
			last_check_error TEXT NOT NULL DEFAULT '',
			failed_digest TEXT NOT NULL DEFAULT ''
		)`,
			"INSERT INTO self_update_settings (id) VALUES (1)",
			`CREATE TABLE self_update_jobs (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			old_image_id TEXT NOT NULL,
			target_image_id TEXT NOT NULL,
			target_digest TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			completed_at INTEGER NOT NULL DEFAULT 0
		)`,
			"CREATE UNIQUE INDEX self_update_one_active ON self_update_jobs (status) WHERE status = 'updating'",
			"PRAGMA user_version = 2",
		} {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("migrate database: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER TABLE self_update_settings ADD COLUMN check_interval_minutes INTEGER NOT NULL DEFAULT 15
			CHECK (check_interval_minutes IN (5, 15, 30, 60, 360))`,
		"PRAGMA user_version = 3",
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Ping() error  { return s.db.Ping() }

func (s *Store) HasAdmin() (bool, error) {
	var exists bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM administrators WHERE id = 1)").Scan(&exists)
	return exists, err
}

func (s *Store) Admin() (Admin, error) {
	var admin Admin
	err := s.db.QueryRow("SELECT username, password_hash FROM administrators WHERE id = 1").Scan(&admin.Username, &admin.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrNoAdmin
	}
	return admin, err
}

func (s *Store) CreateAdmin(username, passwordHash string) error {
	_, err := s.db.Exec("INSERT INTO administrators (id, username, password_hash, created_at) VALUES (1, ?, ?, ?)",
		username, passwordHash, time.Now().Unix())
	if err != nil {
		exists, checkErr := s.HasAdmin()
		if checkErr == nil && exists {
			return ErrAdminExists
		}
		return err
	}
	return nil
}

func (s *Store) ResetAdminPassword(passwordHash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE administrators SET password_hash = ? WHERE id = 1", passwordHash)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNoAdmin
	}
	if _, err := tx.Exec("DELETE FROM sessions"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateSession(tokenHash, csrfToken string, expiresAt time.Time) error {
	_, err := s.db.Exec("INSERT INTO sessions (token_hash, csrf_token, expires_at, created_at) VALUES (?, ?, ?, ?)",
		tokenHash, csrfToken, expiresAt.Unix(), time.Now().Unix())
	return err
}

func (s *Store) Session(tokenHash string) (Session, bool, error) {
	var session Session
	var expires int64
	err := s.db.QueryRow(`SELECT a.username, s.csrf_token, s.expires_at
		FROM sessions s CROSS JOIN administrators a
		WHERE s.token_hash = ? AND a.id = 1`, tokenHash).Scan(&session.Username, &session.CSRFToken, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	session.ExpiresAt = time.Unix(expires, 0)
	if !session.ExpiresAt.After(time.Now()) {
		_ = s.DeleteSession(tokenHash)
		return Session{}, false, nil
	}
	return session, true, nil
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (s *Store) PruneSessions() error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix())
	return err
}
