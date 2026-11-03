package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	noxauth "github.com/mapherez/nox-auth/client"
	"github.com/mapherez/nox-yard/internal/noxlogin"
	"github.com/mapherez/nox-yard/internal/store"
)

type noxAdmission struct {
	mu      sync.Mutex
	session *noxauth.Session
	renewAt time.Time
}

type noxLoginState struct {
	mu       sync.Mutex
	client   *noxauth.Client
	config   store.NoXLoginConfig
	sessions map[string]*noxAdmission
}

func (s *Server) noXClient() (*noxauth.Client, store.NoXLoginConfig, error) {
	s.noxLogin.mu.Lock()
	defer s.noxLogin.mu.Unlock()
	if s.noxLogin.client != nil {
		return s.noxLogin.client, s.noxLogin.config, nil
	}
	config, found, err := s.store.NoXLoginConfiguration()
	if err != nil {
		return nil, config, err
	}
	if !found {
		return nil, config, errors.New("NoX Login is not configured")
	}
	socket := config.SocketPath
	if socket == "" {
		socket = filepath.Join(authSocketDirectory(), "nox-auth.sock")
	}
	client, err := noxauth.New(noxauth.Config{Issuer: config.Issuer, ClientID: config.ClientID, ClientSecret: config.Secret, SocketPath: socket})
	if err != nil {
		return nil, config, err
	}
	s.noxLogin.client = client
	s.noxLogin.config = config
	s.noxLogin.sessions = map[string]*noxAdmission{}
	return client, config, nil
}
func (s *Server) currentNoXSession(r *http.Request) (store.Session, string, bool, error) {
	cookie, err := r.Cookie(s.cookieName())
	if err != nil || cookie.Value == "" {
		return store.Session{}, "", false, nil
	}
	hash := hashToken(cookie.Value)
	stored, credentials, found, err := s.store.NoXSession(hash)
	if err != nil || !found {
		return stored, hash, false, err
	}
	client, _, err := s.noXClient()
	if err != nil {
		return stored, hash, false, err
	}
	s.noxLogin.mu.Lock()
	session := s.noxLogin.sessions[hash]
	if session == nil {
		if len(s.noxLogin.sessions) >= 1024 {
			clear(s.noxLogin.sessions)
		}
		session = &noxAdmission{}
		s.noxLogin.sessions[hash] = session
	}
	s.noxLogin.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	session.mu.Lock()
	if session.session == nil || !time.Now().Before(session.renewAt) {
		session.session = noxauth.NewSession(credentials)
	}
	decision, err := session.session.Authenticate(ctx, client)
	snapshot := session.session.Snapshot()
	if err == nil {
		session.renewAt = decision.ValidUntil.Add(-30 * time.Second)
	}
	session.mu.Unlock()
	if errors.Is(err, noxauth.ErrDenied) {
		_ = s.store.DeleteNoXSession(hash)
		s.forgetNoXSession(hash)
		return store.Session{}, hash, false, nil
	}
	if err != nil {
		return stored, hash, false, err
	}
	if snapshot.ExpiresAt != credentials.ExpiresAt || snapshot.Identity != credentials.Identity {
		if err = s.store.RefreshNoXSession(hash, snapshot); err != nil {
			return stored, hash, false, err
		}
	}
	stored.Username = decision.Identity.Email
	stored.ExpiresAt = decision.ExpiresAt
	return stored, hash, true, nil
}
func (s *Server) forgetNoXSession(hash string) {
	s.noxLogin.mu.Lock()
	defer s.noxLogin.mu.Unlock()
	if session := s.noxLogin.sessions[hash]; session != nil {
		session.mu.Lock()
		if session.session != nil {
			session.session.Invalidate()
		}
		session.mu.Unlock()
	}
	delete(s.noxLogin.sessions, hash)
}
func (s *Server) noxLoginStart(w http.ResponseWriter, r *http.Request) {
	if !s.loginStarts.Allow() {
		w.Header().Set("Retry-After", "1")
		writeError(w, 429, "Too many login requests. Try again shortly.")
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeError(w, 403, "Open NoX Login from Yard.")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !s.checkOrigin(w, r) {
		return
	}
	client, config, err := s.noXClient()
	if err != nil {
		writeError(w, 503, "NoX Login is not configured. Apply Yard's Auth setup configuration.")
		return
	}
	returnPath := r.URL.Query().Get("return_to")
	if len(returnPath) > 2048 || strings.HasPrefix(returnPath, "/auth/") {
		writeError(w, 400, "Invalid return path.")
		return
	}
	if returnPath == "" {
		returnPath = "/"
	}
	tx, location, err := client.Begin(config.Origin+noxlogin.CallbackPath, returnPath)
	if err != nil {
		writeError(w, 400, "Invalid return path.")
		return
	}
	browser, err := randomToken()
	if err != nil {
		writeError(w, 500, "Unable to start login.")
		return
	}
	if err = s.store.SaveNoXLoginTransaction(hashToken(tx.State), hashToken(browser), tx); err != nil {
		writeError(w, 503, "Unable to store login transaction.")
		return
	}
	name := "nox_login_" + tx.State[:12]
	if s.secureCookie {
		name = "__Host-" + name
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: browser, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, location, http.StatusFound)
}
func (s *Server) noxLoginCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if len(state) != 43 {
		writeError(w, 400, "Invalid or expired NoX Login transaction.")
		return
	}
	name := "nox_login_" + state[:12]
	if s.secureCookie {
		name = "__Host-" + name
	}
	cookie, err := r.Cookie(name)
	if err != nil {
		writeError(w, 400, "NoX Login transaction does not belong to this browser.")
		return
	}
	tx, err := s.store.ConsumeNoXLoginTransaction(hashToken(state), hashToken(cookie.Value))
	if err != nil {
		writeError(w, 400, "Invalid, expired or already used NoX Login transaction.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: name, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if subtle.ConstantTimeCompare([]byte(tx.State), []byte(state)) != 1 {
		writeError(w, 400, "Invalid login state.")
		return
	}
	client, config, err := s.noXClient()
	if err != nil {
		writeError(w, 503, "NoX Login is unavailable.")
		return
	}
	credentials, err := client.Complete(r.Context(), config.Origin+noxlogin.CallbackPath, tx, r.URL.Query())
	if err != nil {
		http.Redirect(w, r, "/?login_error=denied", http.StatusSeeOther)
		return
	}
	// Rotate this browser's old app session; preserve Auth SSO and other app sessions.
	if previous, e := r.Cookie(s.cookieName()); e == nil {
		hash := hashToken(previous.Value)
		_ = s.store.DeleteNoXSession(hash)
		s.forgetNoXSession(hash)
	}
	if err = s.issueNoXSession(w, credentials); err != nil {
		writeError(w, 503, "Unable to save the Yard session.")
		return
	}
	http.Redirect(w, r, tx.ReturnPath, http.StatusSeeOther)
}
func (s *Server) issueNoXSession(w http.ResponseWriter, credentials noxauth.Credentials) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	csrf, err := randomToken()
	if err != nil {
		return err
	}
	if err = s.store.CreateNoXSession(hashToken(token), csrf, credentials); err != nil {
		return err
	}
	lifetime := int(time.Until(credentials.ExpiresAt).Seconds())
	if lifetime < 1 {
		return errors.New("expired NoX Login result")
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode, Expires: credentials.ExpiresAt, MaxAge: lifetime})
	return nil
}
