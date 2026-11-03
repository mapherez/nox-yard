package noxlogin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mapherez/nox-yard/internal/store"
)

func TestSetupReusesSecretAndRestrictsEnrollment(t *testing.T) {
	data, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	dir, err := os.MkdirTemp("", "ya")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	type registration struct {
		ID          string   `json:"-"`
		Secret      string   `json:"client_secret"`
		Redirects   []string `json:"redirect_uris"`
		SuiteLogout bool     `json:"suite_logout"`
	}
	requests := make(chan registration, 3)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || !strings.HasPrefix(r.URL.Path, "/v1/clients/nox-yard-") {
			http.Error(w, "bad request", 400)
			return
		}
		var value registration
		if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		value.ID = strings.TrimPrefix(r.URL.Path, "/v1/clients/")
		requests <- value
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	for range 2 {
		if err := Setup(context.Background(), data, "https://auth.example.test", "http://192.0.2.10:8095", socket, "/run/nox-suite/nox-auth.sock"); err != nil {
			t.Fatal(err)
		}
	}
	first, second := <-requests, <-requests
	if first.ID != second.ID || len(first.Secret) < 43 || first.Secret != second.Secret || first.SuiteLogout || len(first.Redirects) != 1 || first.Redirects[0] != "http://192.0.2.10:8095/auth/nox/callback" {
		t.Fatal("Enrollment did not reuse a restricted exact client registration")
	}
	saved, found, err := data.NoXLoginConfiguration()
	if err != nil || !found || saved.Secret != first.Secret || saved.ClientID != first.ID {
		t.Fatal("Client secret not persisted", err)
	}
	if err := Setup(context.Background(), data, "https://other.example.test", "http://192.0.2.10:8095", socket, saved.SocketPath); err == nil {
		t.Fatal("Issuer changed silently")
	}
	if err := Setup(context.Background(), data, saved.Issuer, "http://yard.example.test/app", socket, saved.SocketPath); err == nil {
		t.Fatal("Non-origin callback accepted")
	}
	after, _, err := data.NoXLoginConfiguration()
	if err != nil || after != saved {
		t.Fatal("Rejected configuration changed persisted client", err)
	}
	other, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := Setup(context.Background(), other, saved.Issuer, saved.Origin, socket, saved.SocketPath); err != nil {
		t.Fatal(err)
	}
	third := <-requests
	if third.ID == first.ID || third.Secret == first.Secret {
		t.Fatal("Independent Yard installations share their Auth identity")
	}
	if len(requests) != 0 {
		t.Fatal("Rejected configuration reached enrollment authority")
	}
}
