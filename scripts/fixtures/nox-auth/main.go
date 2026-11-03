// Disposable acceptance-test identity provider. This program is NOT built into
// the Yard image and must only be run against an explicitly labelled fixture.
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	noxauth "github.com/mapherez/nox-auth/client"
	"github.com/mapherez/nox-yard/internal/store"
	"github.com/mapherez/nox-yard/internal/testauth"
)

func digest(value string) string {
	h := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	secret := os.Getenv("NOX_AUTH_FIXTURE_SECRET")
	if len(secret) < 43 {
		log.Fatal("explicit fixture secret required")
	}
	data, err := store.Open("/data")
	if err != nil {
		return err
	}
	defer data.Close()
	config, found, err := data.NoXLoginConfiguration()
	if err != nil {
		return err
	}
	if !found {
		config = store.NoXLoginConfig{Issuer: testauth.Issuer, Origin: os.Getenv("NOX_AUTH_FIXTURE_ORIGIN"), ClientID: "nox-yard", Secret: noxauth.GenerateSecret(), SocketPath: "/data/.test-nox-auth.sock"}
	}
	if err = data.SaveNoXLoginConfiguration(config); err != nil {
		return err
	}
	provider, err := testauth.New()
	if err != nil {
		return err
	}
	provider.Register(config.ClientID, config.Secret)
	_ = os.Remove(config.SocketPath)
	listener, err := net.Listen("unix", config.SocketPath)
	if err != nil {
		return err
	}
	defer listener.Close()
	go func() { _ = http.Serve(listener, provider.Handler()) }()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("POST /fixture/session", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Fixture-Secret")), []byte(secret)) != 1 {
			http.Error(w, "denied", 403)
			return
		}
		credential := provider.Issue(config.ClientID, "owner@example.test")
		token := noxauth.GenerateSecret()
		csrf := noxauth.GenerateSecret()
		if err := data.CreateNoXSession(digest(token), csrf, credential); err != nil {
			http.Error(w, "fixture storage unavailable", 503)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "nox_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(credential.ExpiresAt).Seconds())})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": true, "username": credential.Identity.Email, "csrfToken": csrf, "needsSetup": false, "loginAvailable": true})
	})
	// All public endpoints are in this test-only process, never in Yard itself.
	return http.ListenAndServe(":8082", mux)
}
