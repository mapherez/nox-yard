package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mapherez/nox-yard/internal/auth"
	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/mapherez/nox-yard/internal/lifecycle"
	"github.com/mapherez/nox-yard/internal/managed"
	"github.com/mapherez/nox-yard/internal/selfupdate"
	"github.com/mapherez/nox-yard/internal/store"
)

const sessionLifetime = 7 * 24 * time.Hour

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)
var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Server struct {
	store        *store.Store
	webDir       string
	publicOrigin string
	secureCookie bool
	limiter      loginLimiter
	inventory    inventory.Reader
	logs         inventory.LogReader
	terminal     inventory.TerminalManager
	lifecycle    lifecycle.Controller
	managed      *managed.Manager
	updates      *selfupdate.Manager
	changes      *inventory.Notifier
	control      controlSettings
}

type bootstrapResponse struct {
	NeedsSetup    bool   `json:"needsSetup"`
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	CSRFToken     string `json:"csrfToken,omitempty"`
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func New(data *store.Store, webDir, publicURL string) (*Server, error) {
	s := &Server{store: data, webDir: webDir, limiter: loginLimiter{entries: make(map[string]loginAttempt)}, changes: inventory.NewNotifier()}
	if publicURL == "" {
		return s, nil
	}
	parsed, err := url.Parse(publicURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("NOX_PUBLIC_URL must be an http(s) origin without a path")
	}
	s.publicOrigin = parsed.Scheme + "://" + parsed.Host
	s.secureCookie = parsed.Scheme == "https"
	return s, nil
}

func (s *Server) SetInventory(reader inventory.Reader) {
	s.inventory = reader
}

func (s *Server) SetLogs(reader inventory.LogReader) {
	s.logs = reader
}

func (s *Server) SetTerminal(manager inventory.TerminalManager) {
	s.terminal = manager
}

func (s *Server) SetLifecycle(controller lifecycle.Controller) {
	s.lifecycle = controller
}

func (s *Server) SetManaged(manager *managed.Manager) {
	s.managed = manager
	manager.SetNotifier(s.changes)
}

func (s *Server) Notify(change inventory.Change) { s.changes.Notify(change) }

func (s *Server) SetSelfUpdate(manager *selfupdate.Manager) {
	s.updates = manager
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /api/projects", s.projects)
	mux.HandleFunc("GET /api/projects/events", s.projectEvents)
	mux.HandleFunc("GET /api/metrics", s.metrics)
	mux.HandleFunc("POST /api/managed/source", s.managedSource)
	mux.HandleFunc("GET /api/managed/settings", s.managedSettingsGet)
	mux.HandleFunc("PUT /api/managed/settings", s.managedSettingsPut)
	mux.HandleFunc("POST /api/managed/preview", s.managedPreview)
	mux.HandleFunc("POST /api/managed/deploy", s.managedDeploy)
	mux.HandleFunc("GET /api/managed/jobs/{id}", s.managedJob)
	mux.HandleFunc("POST /api/managed/projects/{name}/operations", s.managedOperation)
	mux.HandleFunc("GET /api/containers/{id}", s.containerInspection)
	mux.HandleFunc("GET /api/containers/{id}/logs", s.containerLogs)
	mux.HandleFunc("GET /api/containers/{id}/terminal", s.containerTerminal)
	mux.HandleFunc("POST /api/containers/{id}/environment", s.containerEnvironment)
	mux.HandleFunc("POST /api/containers/{id}/actions", s.containerAction)
	mux.HandleFunc("POST /api/projects/{id}/actions", s.projectAction)
	mux.HandleFunc("POST /api/containers/{id}/pull", s.containerPull)
	mux.HandleFunc("POST /api/projects/{id}/pull", s.projectPull)
	mux.HandleFunc("POST /api/containers/{id}/remove", s.containerRemove)
	mux.HandleFunc("POST /api/projects/{id}/remove", s.projectRemove)
	mux.HandleFunc("GET /api/containers/{id}/remove/preview", s.containerRemovePreview)
	mux.HandleFunc("GET /api/projects/{id}/remove/preview", s.projectRemovePreview)
	mux.HandleFunc("GET /api/self-update", s.selfUpdateStatus)
	mux.HandleFunc("PUT /api/self-update", s.selfUpdateSettings)
	mux.HandleFunc("POST /api/self-update/check", s.selfUpdateCheck)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.Handle("GET /", s.staticHandler())
	control := s.controlHandler()
	return s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1" || strings.HasPrefix(r.URL.Path, "/v1/") {
			control.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func (s *Server) selfUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "Self-update is unavailable.")
		return
	}
	status, err := s.updates.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read self-update status.")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) selfUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "Self-update is unavailable.")
		return
	}
	var input struct {
		Automatic       *bool `json:"automatic"`
		IntervalMinutes *int  `json:"intervalMinutes"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Automatic == nil || input.IntervalMinutes == nil {
		writeError(w, http.StatusBadRequest, "Automatic and intervalMinutes are required.")
		return
	}
	if err := s.updates.SetSettings(*input.Automatic, *input.IntervalMinutes); errors.Is(err, selfupdate.ErrUpdateInProgress) {
		writeError(w, http.StatusConflict, "Wait for the current update to finish.")
		return
	} else if errors.Is(err, selfupdate.ErrInvalidInterval) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to save automatic update setting.")
		return
	}
	status, err := s.updates.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read self-update status.")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) selfUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.updates == nil {
		writeError(w, http.StatusServiceUnavailable, "Self-update is unavailable.")
		return
	}
	if err := s.updates.CheckNow(); errors.Is(err, selfupdate.ErrUpdateInProgress) || errors.Is(err, selfupdate.ErrCheckInProgress) {
		writeError(w, http.StatusConflict, "An update check or installation is already in progress.")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to start update check.")
		return
	}
	status, err := s.updates.Status()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read self-update status.")
		return
	}
	writeJSON(w, http.StatusAccepted, status)
}

func (s *Server) requireSession(w http.ResponseWriter, r *http.Request, csrfRequired bool) bool {
	session, _, ok, err := s.currentSession(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read session.")
		return false
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "Sign in to continue.")
		return false
	}
	if csrfRequired && (r.Header.Get("X-CSRF-Token") == "" ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRFToken)) != 1) {
		writeError(w, http.StatusForbidden, "Invalid request token.")
		return false
	}
	return true
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	_, _, ok, err := s.currentSession(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read session.")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	if s.inventory == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker inventory is unavailable.")
		return
	}
	snapshot, err := s.readProjects(r.Context())
	if err != nil {
		status := http.StatusServiceUnavailable
		var storage managedReadError
		if errors.As(err, &storage) {
			status = http.StatusInternalServerError
		}
		writeError(w, status, projectReadMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) managedSource(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, managed.MaxSourceBytes*2)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input managed.SourceInput
	if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid source request.")
		return
	}
	source, err := managed.LoadSource(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	info, err := managed.InspectSource(source.YAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		managed.Source
		Variables []managed.Variable `json:"variables"`
		managed.SourceInfo
	}{source, managed.Variables(source.YAML), info})
}

func (s *Server) containerInspection(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	s.writeContainerInspection(w, r, false)
}

func (s *Server) containerEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	s.writeContainerInspection(w, r, true)
}

func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	s.lifecycleAction(w, r, true)
}

func (s *Server) projectAction(w http.ResponseWriter, r *http.Request) {
	s.lifecycleAction(w, r, false)
}

func (s *Server) lifecycleAction(w http.ResponseWriter, r *http.Request, container bool) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	id := r.PathValue("id")
	if container && !containerIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Invalid container ID.")
		return
	}
	if !container && !(strings.HasPrefix(id, "compose:") || strings.HasPrefix(id, "container:")) {
		writeError(w, http.StatusBadRequest, "Invalid project ID.")
		return
	}
	var input struct {
		Action lifecycle.Action `json:"action"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Action != lifecycle.Start && input.Action != lifecycle.Stop && input.Action != lifecycle.Restart {
		writeError(w, http.StatusBadRequest, "Action must be start, stop, or restart.")
		return
	}
	if s.lifecycle == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker management is unavailable.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.runAction(ctx, id, container, input.Action)
	switch {
	case errors.Is(err, lifecycle.ErrNotFound):
		writeError(w, http.StatusNotFound, "Target no longer exists. Refresh the project list.")
	case errors.Is(err, lifecycle.ErrProtected):
		writeError(w, http.StatusConflict, "This NoX Yard container cannot be managed by that action.")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "Cannot perform this action on the local Docker Engine.")
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) writeContainerInspection(w http.ResponseWriter, r *http.Request, revealEnvironment bool) {
	id := r.PathValue("id")
	if !containerIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Invalid container ID.")
		return
	}
	if s.inventory == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker inventory is unavailable.")
		return
	}
	inspection, err := s.inventory.InspectContainer(r.Context(), id, revealEnvironment)
	if errors.Is(err, inventory.ErrContainerNotFound) {
		writeError(w, http.StatusNotFound, "Container no longer exists. Refresh the project list.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Cannot inspect this container on the local Docker Engine.")
		return
	}
	writeJSON(w, http.StatusOK, inspection)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// xterm injects runtime CSS for its cursor, dimensions, and colors.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ws: wss:")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "Storage is unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	exists, err := s.store.HasAdmin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read setup status.")
		return
	}
	if !exists {
		writeJSON(w, http.StatusOK, bootstrapResponse{NeedsSetup: true})
		return
	}

	session, _, ok, err := s.currentSession(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read session.")
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, bootstrapResponse{})
		return
	}
	writeJSON(w, http.StatusOK, bootstrapResponse{
		Authenticated: true,
		Username:      session.Username,
		CSRFToken:     session.CSRFToken,
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	var input credentials
	if !decodeJSON(w, r, &input) {
		return
	}
	if !usernamePattern.MatchString(input.Username) {
		writeError(w, http.StatusBadRequest, "Username must be 3–32 characters using letters, numbers, dots, underscores, or hyphens.")
		return
	}
	passwordHash, err := auth.HashPassword(input.Password)
	if errors.Is(err, auth.ErrInvalidPassword) {
		writeError(w, http.StatusBadRequest, auth.ErrInvalidPassword.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to create administrator.")
		return
	}
	if err := s.store.CreateAdmin(input.Username, passwordHash); err != nil {
		if errors.Is(err, store.ErrAdminExists) {
			writeError(w, http.StatusConflict, "Setup has already been completed.")
			return
		}
		writeError(w, http.StatusInternalServerError, "Unable to create administrator.")
		return
	}
	s.issueSession(w, input.Username, http.StatusCreated)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	var input credentials
	if !decodeJSON(w, r, &input) {
		return
	}
	key := remoteIP(r.RemoteAddr)
	if !s.limiter.allowed(key) {
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, "Too many login attempts. Try again later.")
		return
	}
	admin, err := s.store.Admin()
	if errors.Is(err, store.ErrNoAdmin) {
		writeError(w, http.StatusConflict, "Create the administrator account first.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to sign in.")
		return
	}
	valid := auth.VerifyPassword(admin.PasswordHash, input.Password) && strings.EqualFold(input.Username, admin.Username)
	if !valid {
		s.limiter.failed(key)
		writeError(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}
	s.limiter.clear(key)
	s.issueSession(w, admin.Username, http.StatusOK)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	session, tokenHash, ok, err := s.currentSession(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to sign out.")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	csrf := r.Header.Get("X-CSRF-Token")
	if csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRFToken)) != 1 {
		writeError(w, http.StatusForbidden, "Invalid request token.")
		return
	}
	if err := s.store.DeleteSession(tokenHash); err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to sign out.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.secureCookie || r.TLS != nil,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) issueSession(w http.ResponseWriter, username string, status int) {
	token, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to create session.")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to create session.")
		return
	}
	expires := time.Now().Add(sessionLifetime)
	if err := s.store.CreateSession(hashToken(token), csrf, expires); err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to create session.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.secureCookie,
	})
	writeJSON(w, status, bootstrapResponse{Authenticated: true, Username: username, CSRFToken: csrf})
}

func (s *Server) currentSession(r *http.Request) (store.Session, string, bool, error) {
	cookie, err := r.Cookie(s.cookieName())
	if err != nil || cookie.Value == "" {
		return store.Session{}, "", false, nil
	}
	tokenHash := hashToken(cookie.Value)
	session, ok, err := s.store.Session(tokenHash)
	return session, tokenHash, ok, err
}

func (s *Server) cookieName() string {
	if s.secureCookie {
		return "__Host-nox_session"
	}
	return "nox_session"
}

func (s *Server) checkOrigin(w http.ResponseWriter, r *http.Request) bool {
	expected := s.publicOrigin
	if expected == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		expected = scheme + "://" + r.Host
	}
	if r.Header.Get("Origin") != expected {
		writeError(w, http.StatusForbidden, "Invalid request origin.")
		return false
	}
	return true
}

func (s *Server) staticHandler() http.Handler {
	files := http.FileServer(http.Dir(s.webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "Not found.")
			return
		}
		indexPath := filepath.Join(s.webDir, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			http.Error(w, "Web application is not built.", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/" {
			path := filepath.Join(s.webDir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, indexPath)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON request.")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON request.")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	return host
}

type loginAttempt struct {
	count int
	until time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	entries map[string]loginAttempt
}

func (l *loginLimiter) allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.entries[key]
	if time.Now().After(attempt.until) {
		delete(l.entries, key)
		return true
	}
	return attempt.count < 5
}

func (l *loginLimiter) failed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt := l.entries[key]
	if time.Now().After(attempt.until) {
		attempt = loginAttempt{until: time.Now().Add(15 * time.Minute)}
	}
	attempt.count++
	l.entries[key] = attempt
}

func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}
