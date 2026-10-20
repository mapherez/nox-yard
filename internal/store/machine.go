package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type NetworkEndpoint struct {
	Bind string `json:"bind"`
	Port int    `json:"port"`
}
type MachineNetwork struct {
	Yard NetworkEndpoint `json:"yard"`
	MCP  NetworkEndpoint `json:"mcp"`
}
type MachineSettings struct {
	Revision       int64          `json:"revision"`
	MCPEnabled     bool           `json:"mcpEnabled"`
	APIEnabled     bool           `json:"apiEnabled"`
	AuthMode       string         `json:"authMode"`
	Write          bool           `json:"write"`
	Secrets        bool           `json:"secrets"`
	AutoApply      bool           `json:"autoApply"`
	Network        MachineNetwork `json:"network"`
	TokenCreatedAt int64          `json:"tokenCreatedAt"`
	HasToken       bool           `json:"hasToken"`
}
type TokenEvent struct {
	ID        int64  `json:"id"`
	Action    string `json:"action"`
	Actor     string `json:"actor"`
	CreatedAt int64  `json:"createdAt"`
}

func ensureMachineTables(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS machine_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), settings TEXT NOT NULL, token_digest TEXT NOT NULL DEFAULT '', token_created_at INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS machine_token_events (id INTEGER PRIMARY KEY, action TEXT NOT NULL, actor TEXT NOT NULL, created_at INTEGER NOT NULL);`)
	return err
}

// InitializeMachine imports the legacy key only on the first initialization.
// A stale environment key can never resurrect a rotated or revoked token.
func (s *Store) InitializeMachine(network MachineNetwork, apiEnabled bool, legacyKey string) error {
	settings := MachineSettings{Revision: 1, AuthMode: "token", APIEnabled: apiEnabled, Network: network}
	digest, created := "", int64(0)
	if apiEnabled && legacyKey != "" {
		digest, created = machineDigest(legacyKey), time.Now().Unix()
		settings.Write, settings.Secrets = true, true
	}
	encoded, _ := json.Marshal(settings)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO machine_settings(id,settings,token_digest,token_created_at) VALUES(1,?,?,?) ON CONFLICT(id) DO NOTHING`, string(encoded), digest, created)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 && digest != "" {
		if _, err := tx.Exec(`INSERT INTO machine_token_events(action,actor,created_at) VALUES('imported','Legacy configuration',?)`, created); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MachineSettings() (MachineSettings, error) {
	var value MachineSettings
	var raw, digest string
	var created int64
	err := s.db.QueryRow(`SELECT settings,token_digest,token_created_at FROM machine_settings WHERE id=1`).Scan(&raw, &digest, &created)
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal([]byte(raw), &value); err != nil {
		return value, err
	}
	value.HasToken = digest != ""
	value.TokenCreatedAt = created
	return value, nil
}

func validMachineSettings(v MachineSettings) bool {
	validEndpoint := func(e NetworkEndpoint) bool {
		return (e.Bind == "127.0.0.1" || e.Bind == "0.0.0.0") && e.Port > 0 && e.Port <= 65535
	}
	return (v.AuthMode == "token" || v.AuthMode == "anonymous") && validEndpoint(v.Network.Yard) && validEndpoint(v.Network.MCP) && v.Network.Yard.Port != v.Network.MCP.Port
}

func (s *Store) SaveMachineSettings(v MachineSettings) (MachineSettings, error) {
	if !validMachineSettings(v) {
		return v, errors.New("Choose different ports from 1 to 65535 and a supported bind address.")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	var raw, digest string
	if err := tx.QueryRow(`SELECT settings,token_digest FROM machine_settings WHERE id=1`).Scan(&raw, &digest); err != nil {
		return v, err
	}
	var old MachineSettings
	if err := json.Unmarshal([]byte(raw), &old); err != nil {
		return v, err
	}
	if old.Revision != v.Revision {
		return v, ErrJobChanged
	}
	var locked int
	if err := tx.QueryRow(`SELECT count(*) FROM operation_locks WHERE resource='yard:self'`).Scan(&locked); err != nil {
		return v, err
	}
	if locked != 0 {
		return v, ErrOperationConflict
	}
	if digest == "" && ((v.MCPEnabled && v.AuthMode == "token") || v.APIEnabled) {
		return v, errors.New("Generate a Yard token before enabling authenticated access.")
	}
	v.Revision++
	v.HasToken = false
	v.TokenCreatedAt = 0
	encoded, _ := json.Marshal(v)
	if _, err := tx.Exec(`UPDATE machine_settings SET settings=? WHERE id=1`, string(encoded)); err != nil {
		return v, err
	}
	if err := tx.Commit(); err != nil {
		return v, err
	}
	return s.MachineSettings()
}

func machineDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) ValidateMachineToken(token string) (bool, error) {
	var digest string
	if err := s.db.QueryRow(`SELECT token_digest FROM machine_settings WHERE id=1`).Scan(&digest); err != nil {
		return false, err
	}
	return digest != "" && subtle.ConstantTimeCompare([]byte(digest), []byte(machineDigest(token))) == 1, nil
}

func (s *Store) RotateMachineToken(actor string, revoke bool) (string, error) {
	token, digest, action := "", "", "revoked"
	if !revoke {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		token = "yard_" + base64.RawURLEncoding.EncodeToString(random[:])
		digest = machineDigest(token)
		action = "generated"
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRow(`SELECT token_digest FROM machine_settings WHERE id=1`).Scan(&previous); err != nil {
		return "", err
	}
	if previous != "" && !revoke {
		action = "replaced"
	}
	now := time.Now().Unix()
	created := now
	query := `UPDATE machine_settings SET token_digest=?, token_created_at=? WHERE id=1`
	args := []any{digest, created}
	if revoke {
		query = `UPDATE machine_settings SET token_digest=? WHERE id=1`
		args = []any{digest}
	}
	if _, err := tx.Exec(query, args...); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO machine_token_events(action,actor,created_at) VALUES(?,?,?)`, action, actor, now); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM machine_token_events WHERE id NOT IN (SELECT id FROM machine_token_events ORDER BY id DESC LIMIT 10)`); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) MachineTokenHistory() ([]TokenEvent, error) {
	rows, err := s.db.Query(`SELECT id,action,actor,created_at FROM machine_token_events ORDER BY id DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []TokenEvent{}
	for rows.Next() {
		var event TokenEvent
		if err := rows.Scan(&event.ID, &event.Action, &event.Actor, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (n MachineNetwork) Validate() error {
	if !validMachineSettings(MachineSettings{AuthMode: "token", Network: n}) {
		return fmt.Errorf("invalid network settings")
	}
	return nil
}
