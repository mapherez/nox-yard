package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"unicode"
)

// ControlConfig is startup-only configuration. The server retains only a key
// digest; do not serialize or log the input configuration.
type ControlConfig struct {
	Enabled bool
	Key     string
	Version string
}

type controlSettings struct {
	enabled   bool
	keyDigest [sha256.Size]byte
	version   string
}

func invalidControlKey(key string) bool {
	return key == "" || strings.Contains(key, ",") || strings.ContainsFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// ConfigureControlAPI must be called before serving requests.
func (s *Server) ConfigureControlAPI(config ControlConfig) error {
	if config.Enabled && invalidControlKey(config.Key) {
		return errors.New("NOX_YARD_API_KEY is required when the machine API is enabled and must contain no whitespace, commas, or control characters")
	}
	version := strings.TrimSpace(config.Version)
	if version == "" {
		version = "dev"
	}
	s.control = controlSettings{enabled: config.Enabled, version: version}
	if config.Enabled {
		s.control.keyDigest = sha256.Sum256([]byte(config.Key))
	}
	return nil
}

func (s *Server) controlVersion() string {
	if s.control.version == "" {
		return "dev"
	}
	return s.control.version
}

func (s *Server) requireControlAuth(w http.ResponseWriter, r *http.Request) bool {
	if !s.control.enabled {
		writeControlError(w, http.StatusServiceUnavailable, "API_DISABLED", "The machine API is disabled.")
		return false
	}
	values := r.Header.Values("Authorization")
	if len(values) == 0 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeControlError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "Bearer authentication is required.")
		return false
	}
	valid := false
	if len(values) == 1 {
		scheme, key, ok := strings.Cut(values[0], " ")
		if ok && strings.EqualFold(scheme, "Bearer") && !invalidControlKey(key) && !strings.Contains(key, ",") {
			digest := sha256.Sum256([]byte(key))
			valid = subtle.ConstantTimeCompare(digest[:], s.control.keyDigest[:]) == 1
		}
	}
	if !valid {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeControlError(w, http.StatusUnauthorized, "AUTH_INVALID", "Bearer authentication is invalid.")
	}
	return valid
}
