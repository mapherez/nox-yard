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
	Context      string `json:"-"`
}

func ensureFileConfigurationTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS file_configurations (name TEXT PRIMARY KEY,directory TEXT NOT NULL,compose_files TEXT NOT NULL DEFAULT '[]',state TEXT NOT NULL,error TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		return err
	}
	// Additive migration; historical file locations and jobs remain valid.
	rows, err := db.Query("PRAGMA table_info(file_configurations)")
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var id, notnull, pk int
		var name, kind string
		var value any
		if err := rows.Scan(&id, &name, &kind, &notnull, &value, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "context_json"
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !found {
		_, err = db.Exec("ALTER TABLE file_configurations ADD COLUMN context_json TEXT NOT NULL DEFAULT '{}'")
	}
	return err
}
func (s *Store) FileConfiguration(name string) (FileConfiguration, bool, error) {
	v := FileConfiguration{Name: name}
	err := s.db.QueryRow(`SELECT directory,compose_files,state,error,context_json FROM file_configurations WHERE name=?`, name).Scan(&v.Directory, &v.ComposeFiles, &v.State, &v.Error, &v.Context)
	if err == sql.ErrNoRows {
		return v, false, nil
	}
	return v, err == nil, err
}
func (s *Store) SetFileConfiguration(v FileConfiguration) error {
	if v.Context == "" {
		v.Context = "{}"
	}
	_, err := s.db.Exec(`INSERT INTO file_configurations(name,directory,compose_files,state,error,context_json) VALUES(?,?,?,?,?,?) ON CONFLICT(name) DO UPDATE SET directory=excluded.directory,compose_files=excluded.compose_files,state=excluded.state,error=excluded.error,context_json=CASE WHEN excluded.context_json='{}' THEN file_configurations.context_json ELSE excluded.context_json END`, v.Name, v.Directory, v.ComposeFiles, v.State, v.Error, v.Context)
	return err
}
