package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	noxauth "github.com/mapherez/nox-auth/client"
)

type NoXLoginConfig struct{ Issuer, Origin, ClientID, Secret, SocketPath string }

func ensureNoXLoginTables(db *sql.DB) error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS nox_login_config(id INTEGER PRIMARY KEY CHECK(id=1),body BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS nox_login_transactions(state_hash TEXT PRIMARY KEY,browser_hash TEXT NOT NULL,body BLOB NOT NULL,expires_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS nox_login_sessions(token_hash TEXT PRIMARY KEY,csrf_token TEXT NOT NULL,credentials BLOB NOT NULL,expires_at INTEGER NOT NULL)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) NoXLoginConfiguration() (NoXLoginConfig, bool, error) {
	var config NoXLoginConfig
	var body []byte
	err := s.db.QueryRow(`SELECT body FROM nox_login_config WHERE id=1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return config, false, nil
	}
	if err != nil {
		return config, false, err
	}
	err = json.Unmarshal(body, &config)
	return config, err == nil, err
}
func (s *Store) SaveNoXLoginConfiguration(c NoXLoginConfig) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO nox_login_config VALUES(1,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body`, body)
	return err
}
func (s *Store) SaveNoXLoginTransaction(state, browser string, t noxauth.Transaction) error {
	if _, err := s.db.Exec(`DELETE FROM nox_login_transactions WHERE expires_at<=?`, time.Now().Unix()); err != nil {
		return err
	}
	var pending int
	if err := s.db.QueryRow(`SELECT count(*) FROM nox_login_transactions`).Scan(&pending); err != nil {
		return err
	}
	if pending >= 1000 {
		return errors.New("too many pending login transactions")
	}
	body, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO nox_login_transactions VALUES(?,?,?,?)`, state, browser, body, t.Created.Add(10*time.Minute).Unix())
	return err
}
func (s *Store) ConsumeNoXLoginTransaction(state, browser string) (noxauth.Transaction, error) {
	var tx noxauth.Transaction
	var body []byte
	err := s.db.QueryRow(`DELETE FROM nox_login_transactions WHERE state_hash=? AND browser_hash=? AND expires_at>? RETURNING body`, state, browser, time.Now().Unix()).Scan(&body)
	if err != nil {
		return tx, err
	}
	err = json.Unmarshal(body, &tx)
	return tx, err
}
func (s *Store) CreateNoXSession(hash, csrf string, c noxauth.Credentials) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO nox_login_sessions VALUES(?,?,?,?)`, hash, csrf, body, c.ExpiresAt.Unix())
	return err
}
func (s *Store) NoXSession(hash string) (Session, noxauth.Credentials, bool, error) {
	var session Session
	var credentials noxauth.Credentials
	var body []byte
	var expires int64
	err := s.db.QueryRow(`SELECT csrf_token,credentials,expires_at FROM nox_login_sessions WHERE token_hash=?`, hash).Scan(&session.CSRFToken, &body, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return session, credentials, false, nil
	}
	if err != nil {
		return session, credentials, false, err
	}
	if expires <= time.Now().Unix() {
		_ = s.DeleteNoXSession(hash)
		return session, credentials, false, nil
	}
	if err = json.Unmarshal(body, &credentials); err != nil {
		return session, credentials, false, err
	}
	session.Username = credentials.Identity.Email
	session.ExpiresAt = time.Unix(expires, 0)
	return session, credentials, true, nil
}
func (s *Store) RefreshNoXSession(hash string, c noxauth.Credentials) error {
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE nox_login_sessions SET credentials=?,expires_at=? WHERE token_hash=?`, body, c.ExpiresAt.Unix(), hash)
	return err
}
func (s *Store) DeleteNoXSession(hash string) error {
	_, err := s.db.Exec(`DELETE FROM nox_login_sessions WHERE token_hash=?`, hash)
	return err
}
