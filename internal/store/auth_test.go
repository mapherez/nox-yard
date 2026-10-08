package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestConcurrentSetupAcrossConnections(t *testing.T) {
	dir := t.TempDir()
	var connections []*Store
	for i := 0; i < 8; i++ {
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, s)
		t.Cleanup(func() { s.Close() })
	}
	start := make(chan struct{})
	results := make(chan int, 8)
	var wg sync.WaitGroup
	for i, s := range connections {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			<-start
			err := s.CreateAdmin(fmt.Sprintf("owner%d", i), fmt.Sprintf("hash%d", i))
			if err == nil {
				results <- i
			} else if !errors.Is(err, ErrAdminExists) {
				t.Errorf("setup: %v", err)
			}
		}(i, s)
	}
	close(start)
	wg.Wait()
	close(results)
	winner := -1
	count := 0
	for i := range results {
		winner = i
		count++
	}
	if count != 1 {
		t.Fatalf("setup winners: %d", count)
	}
	admin, err := connections[0].Admin()
	if err != nil || admin.Username != fmt.Sprintf("owner%d", winner) || admin.PasswordHash != fmt.Sprintf("hash%d", winner) {
		t.Fatal("stored administrator is not the setup winner")
	}
}

func TestSessionsPersistExpireAndResetAtomically(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.Admin(); !errors.Is(err, ErrNoAdmin) {
		t.Fatal("empty store has administrator")
	}
	if err := s.ResetAdminPassword("new"); !errors.Is(err, ErrNoAdmin) {
		t.Fatal("reset without administrator")
	}
	if err := s.CreateAdmin("owner", "old"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"first-hash", "second-hash"} {
		if err := s.CreateSession(token, "csrf-"+token, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	session, ok, err := s.Session("first-hash")
	if err != nil || !ok || session.Username != "owner" || session.CSRFToken != "csrf-first-hash" {
		t.Fatal("session not preserved across reopen")
	}
	if _, ok, err := s.Session("csrf-first-hash"); err != nil || ok {
		t.Fatal("CSRF token used as session credential")
	}
	for _, expiry := range []time.Time{time.Now().Add(-time.Hour), time.Now().Truncate(time.Second)} {
		if err := s.CreateSession("expired", "csrf", expiry); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.Session("expired"); err != nil || ok {
			t.Fatal("expired session accepted")
		}
		var count int
		if err := s.db.QueryRow("SELECT count(*) FROM sessions WHERE token_hash='expired'").Scan(&count); err != nil || count != 0 {
			t.Fatal("expired session not removed")
		}
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_reset BEFORE DELETE ON sessions BEGIN SELECT RAISE(FAIL, 'fixture failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetAdminPassword("new"); err == nil {
		t.Fatal("reset ignored failed session revocation")
	}
	admin, err := s.Admin()
	if err != nil || admin.PasswordHash != "old" {
		t.Fatal("failed reset changed password")
	}
	if _, ok, err := s.Session("first-hash"); err != nil || !ok {
		t.Fatal("failed reset revoked session")
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_reset"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetAdminPassword("new"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	admin, err = s.Admin()
	if err != nil || admin.PasswordHash != "new" {
		t.Fatal("reset password not persisted")
	}
	for _, token := range []string{"first-hash", "second-hash"} {
		if _, ok, err := s.Session(token); err != nil || ok {
			t.Fatal("reset session survives")
		}
	}
}
