package store

func (s *Store) ProjectsBase() (string, error) {
	var base string
	err := s.db.QueryRow("SELECT projects_base FROM managed_settings WHERE id = 1").Scan(&base)
	return base, err
}

func (s *Store) SetProjectsBase(base string) error {
	_, err := s.db.Exec("UPDATE managed_settings SET projects_base = ? WHERE id = 1", base)
	return err
}
