package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SuiteTarget is private deployment metadata, frozen in the existing job payload.
// It is a candidate identity, never proof of Backend registration.
type SuiteTarget struct {
	ProductID string `json:"productId"`
	Service   string `json:"service"`
}
type SuiteBinding struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectId"`
	Service   string          `json:"service"`
	ProductID string          `json:"productId"`
	State     string          `json:"state"`
	Message   string          `json:"message,omitempty"`
	UpdatedAt int64           `json:"updatedAt"`
	Revision  int64           `json:"revision"`
	Private   json.RawMessage `json:"-"`
}
type SuiteOperation struct {
	Sequence   int64    `json:"sequence"`
	JobID      string   `json:"jobId"`
	TargetID   string   `json:"targetId"`
	Operation  string   `json:"operation"`
	Status     string   `json:"status"`
	Outcome    string   `json:"outcome"`
	Bindings   []string `json:"bindings"`
	OccurredAt int64    `json:"occurredAt"`
}

func ensureSuiteTables(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS suite_private (name TEXT PRIMARY KEY, value BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS suite_bindings (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, service TEXT NOT NULL, product_id TEXT NOT NULL, state TEXT NOT NULL, message TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL, revision INTEGER NOT NULL DEFAULT 1, private_json BLOB NOT NULL DEFAULT '{}')`,
		`CREATE UNIQUE INDEX IF NOT EXISTS suite_binding_live ON suite_bindings(project_id,service) WHERE state NOT IN ('removing','removed')`,
		`CREATE TABLE IF NOT EXISTS suite_outbox (sequence INTEGER PRIMARY KEY AUTOINCREMENT, event_json BLOB NOT NULL)`,
	} {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) SuitePrivate(name string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT value FROM suite_private WHERE name=?`, name).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}
func (s *Store) SaveSuitePrivate(name string, b []byte) error {
	if len(b) > 8<<20 || !json.Valid(b) {
		return errors.New("invalid private Suite state")
	}
	_, err := s.db.Exec(`INSERT INTO suite_private VALUES(?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, name, b)
	return err
}

const suiteBindingColumns = `id,project_id,service,product_id,state,message,updated_at,revision,private_json`

func scanSuiteBinding(row interface{ Scan(...any) error }) (SuiteBinding, error) {
	var b SuiteBinding
	var private []byte
	err := row.Scan(&b.ID, &b.ProjectID, &b.Service, &b.ProductID, &b.State, &b.Message, &b.UpdatedAt, &b.Revision, &private)
	b.Private = json.RawMessage(private)
	return b, err
}
func (s *Store) SuiteBindings(ctx context.Context) ([]SuiteBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+suiteBindingColumns+` FROM suite_bindings ORDER BY updated_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SuiteBinding{}
	for rows.Next() {
		b, err := scanSuiteBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) SaveSuiteBinding(b SuiteBinding) error {
	if len(b.Private) > 2<<20 || !json.Valid(b.Private) {
		return errors.New("invalid Suite recovery state")
	}
	result, err := s.db.Exec(`UPDATE suite_bindings SET state=?,message=?,updated_at=?,private_json=?,revision=revision+1 WHERE id=? AND revision=?`, b.State, b.Message, time.Now().Unix(), []byte(b.Private), b.ID, b.Revision)
	return changed(result, err)
}
func (s *Store) SuiteRetry(id string, revision int64) error {
	result, err := s.db.Exec(`UPDATE suite_bindings SET message='',updated_at=?,revision=revision+1 WHERE id=? AND revision=? AND state NOT IN ('removed')`, time.Now().Unix(), id, revision)
	return changed(result, err)
}
func (s *Store) SuiteOperations(ctx context.Context) ([]SuiteOperation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,event_json FROM suite_outbox ORDER BY sequence LIMIT 128`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SuiteOperation{}
	for rows.Next() {
		var n int64
		var raw []byte
		if err := rows.Scan(&n, &raw); err != nil {
			return nil, err
		}
		var e SuiteOperation
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		e.Sequence = n
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) FinishSuiteOperation(sequence int64) error {
	_, err := s.db.Exec(`DELETE FROM suite_outbox WHERE sequence=?`, sequence)
	return err
}

// This runs in the native operation transaction, including independent workers.
// Docker success and pending registration/removal are distinct durable results.
func queueSuiteJobTx(tx *sql.Tx, job Job) error {
	var payload struct {
		SuiteTargets []SuiteTarget `json:"suiteTargets"`
	}
	_ = json.Unmarshal([]byte(job.Payload), &payload)
	if job.Status == "succeeded" && job.Domain == "managed" && (job.Operation == "new" || job.Operation == "copy" || job.Operation == "adopt" || job.Operation == "sync") {
		// Retire changed/removed participant labels before inserting their
		// replacements. Ordinary image/start/stop jobs are not full source edits.
		rows, err := tx.Query(`SELECT id,service,product_id FROM suite_bindings WHERE project_id=? AND state NOT IN ('removing','removed')`, job.TargetID)
		if err != nil {
			return err
		}
		retire := []string{}
		for rows.Next() {
			var id, service, product string
			if err = rows.Scan(&id, &service, &product); err != nil {
				rows.Close()
				return err
			}
			found := false
			for _, target := range payload.SuiteTargets {
				if target.Service == service && target.ProductID == product {
					found = true
				}
			}
			if !found {
				retire = append(retire, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range retire {
			if _, err = tx.Exec(`UPDATE suite_bindings SET state='removing',revision=revision+1,updated_at=? WHERE id=?`, time.Now().Unix(), id); err != nil {
				return err
			}
		}
		for _, t := range payload.SuiteTargets {
			if t.ProductID == "" || t.Service == "" {
				continue
			}
			_, err := tx.Exec(`INSERT OR IGNORE INTO suite_bindings(id,project_id,service,product_id,state,updated_at) VALUES(?,?,?,?, 'pending',?)`, "deployment_"+rand.Text(), job.TargetID, t.Service, t.ProductID, time.Now().Unix())
			if err != nil {
				return err
			}
		}
	}
	targets := append([]string{job.TargetID}, job.Resources...)
	ids := []string{}
	seen := map[string]bool{}
	for _, target := range targets {
		rows, err := tx.Query(`SELECT id FROM suite_bindings WHERE project_id=? AND state NOT IN ('removing','removed')`, target)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	e := SuiteOperation{JobID: job.ID, TargetID: job.TargetID, Operation: job.Operation, Status: job.Status, Outcome: job.Outcome, Bindings: ids, OccurredAt: time.Now().Unix()}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO suite_outbox(event_json) VALUES(?)`, raw); err != nil {
		return err
	}
	if job.Operation == "remove" && job.Status == "succeeded" && job.Outcome != "recovery_required" {
		for _, id := range ids {
			if _, err = tx.Exec(`UPDATE suite_bindings SET state='removing',message='Deployment removed; Backend cleanup is pending.',revision=revision+1,updated_at=? WHERE id=? AND project_id=?`, time.Now().Unix(), id, job.TargetID); err != nil {
				return err
			}
		}
	}
	return nil
}
