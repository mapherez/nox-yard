package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Locations and migration state only. Private recovery snapshots belong to jobs.
type FileConfiguration struct {
	Name         string `json:"name"`
	Directory    string `json:"directory"`
	ComposeFiles string `json:"-"`
	State        string `json:"state"`
	Error        string `json:"error,omitempty"`
}

// CompleteFileMigration removes a legacy active copy only after the caller has
// read the exported files back and supplied their verified fingerprint. The
// exact original SQL contents must also exist in a completed recovery job.
// A stale export, missing backup or live worker refuses cleanup atomically.
func (s *Store) CompleteFileMigration(previous ManagedProject, location FileConfiguration, backupID, verifiedFingerprint string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var payload string
	if err := tx.QueryRow(`SELECT payload FROM operation_jobs WHERE id=? AND project_name=? AND operation='migrate_configuration' AND status='succeeded' AND outcome='verified'`, backupID, previous.Name).Scan(&payload); err != nil {
		return fmt.Errorf("verified migration recovery snapshot is missing")
	}
	var backup struct {
		Previous ManagedProject
		Exported struct {
			Fingerprint string
			Directory   string
			Files       map[string]string
		}
	}
	if json.Unmarshal([]byte(payload), &backup) != nil || len(verifiedFingerprint) != 64 || backup.Exported.Fingerprint != verifiedFingerprint || backup.Exported.Directory != location.Directory || backup.Previous != previous || len(backup.Exported.Files) == 0 {
		return fmt.Errorf("verified migration recovery snapshot does not match")
	}
	var active int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM operation_jobs WHERE target_id=? AND status='running'`, "compose:"+previous.Name).Scan(&active); err != nil {
		return err
	}
	if active != 0 {
		return ErrOperationConflict
	}
	result, err := tx.Exec(`UPDATE managed_projects SET project_dir=?,yaml='',variables_json='{}',env_files_json='{}' WHERE name=? AND project_dir=? AND yaml=? AND variables_json=? AND env_files_json=?`, location.Directory, previous.Name, previous.ProjectDir, previous.YAML, previous.VariablesJSON, previous.EnvFilesJSON)
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
	if _, err := tx.Exec(`INSERT INTO file_configurations(name,directory,compose_files,state,error) VALUES(?,?,?,'ready','') ON CONFLICT(name) DO UPDATE SET directory=excluded.directory,compose_files=excluded.compose_files,state='ready',error=''`, location.Name, location.Directory, location.ComposeFiles); err != nil {
		return err
	}
	return tx.Commit()
}

func migrateConfigurations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS file_configurations (name TEXT PRIMARY KEY,directory TEXT NOT NULL,compose_files TEXT NOT NULL DEFAULT '[]',state TEXT NOT NULL,error TEXT NOT NULL DEFAULT '')`)
	return err
}
func (s *Store) FileConfiguration(name string) (FileConfiguration, bool, error) {
	v := FileConfiguration{Name: name}
	err := s.db.QueryRow(`SELECT directory,compose_files,state,error FROM file_configurations WHERE name=?`, name).Scan(&v.Directory, &v.ComposeFiles, &v.State, &v.Error)
	if err == sql.ErrNoRows {
		return v, false, nil
	}
	return v, err == nil, err
}
func (s *Store) FileConfigurations(ctx context.Context) ([]FileConfiguration, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name,directory,compose_files,state,error FROM file_configurations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []FileConfiguration{}
	for rows.Next() {
		var v FileConfiguration
		if err := rows.Scan(&v.Name, &v.Directory, &v.ComposeFiles, &v.State, &v.Error); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
func (s *Store) SetFileConfiguration(v FileConfiguration) error {
	_, err := s.db.Exec(`INSERT INTO file_configurations(name,directory,compose_files,state,error) VALUES(?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET directory=excluded.directory,compose_files=excluded.compose_files,state=excluded.state,error=excluded.error`, v.Name, v.Directory, v.ComposeFiles, v.State, v.Error)
	return err
}
