package store

import (
	"database/sql"
)

// Project file locations only. The state/error columns remain for database compatibility. Private recovery snapshots belong to jobs.
type FileConfiguration struct {
	Name         string `json:"name"`
	Directory    string `json:"directory"`
	ComposeFiles string `json:"-"`
	State        string `json:"state"`
	Error        string `json:"error,omitempty"`
}

func ensureFileConfigurationTable(db *sql.DB) error {
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
func (s *Store) SetFileConfiguration(v FileConfiguration) error {
	_, err := s.db.Exec(`INSERT INTO file_configurations(name,directory,compose_files,state,error) VALUES(?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET directory=excluded.directory,compose_files=excluded.compose_files,state=excluded.state,error=excluded.error`, v.Name, v.Directory, v.ComposeFiles, v.State, v.Error)
	return err
}
