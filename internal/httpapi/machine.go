package httpapi

import (
	"net/http"
	"strings"

	"github.com/mapherez/nox-yard/internal/machineauth"
	"github.com/mapherez/nox-yard/internal/store"
)

func (s *Server) machineTokenAuth(w http.ResponseWriter, r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) == 1 {
		scheme, token, ok := strings.Cut(values[0], " ")
		if ok && strings.EqualFold(scheme, "Bearer") && !invalidControlKey(token) {
			valid, err := s.store.ValidateMachineToken(token)
			if err != nil {
				writeControlError(w, 503, "AUTH_UNAVAILABLE", "Authentication is unavailable.")
				return false
			}
			if valid {
				return true
			}
		}
	}
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeControlError(w, 401, "AUTH_INVALID", "A valid Yard token is required.")
	return false
}

// MCPHandler is served exclusively on the MCP listener. Auth is rechecked for
// every HTTP request; browser cookies and URL parameters never authenticate it.
func (s *Server) MCPHandler() http.Handler {
	return s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/mcp" {
			writeError(w, 404, "Not found.")
			return
		}
		settings, err := s.store.MachineSettings()
		if err != nil {
			writeError(w, 503, "MCP settings are unavailable.")
			return
		}
		if !settings.MCPEnabled {
			writeError(w, 503, "MCP is disabled.")
			return
		}
		if r.Header.Get("Origin") != "" && !s.checkOrigin(w, r) {
			return
		}
		// An explicitly supplied invalid credential never falls back to anonymous.
		if settings.AuthMode == "token" || len(r.Header.Values("Authorization")) != 0 {
			if !s.machineTokenAuth(w, r) {
				return
			}
		}
		if s.mcp == nil {
			writeError(w, 503, "MCP is unavailable.")
			return
		}
		ctx := machineauth.WithPolicy(r.Context(), machineauth.Policy{Write: settings.Write, Secrets: settings.Secrets})
		s.mcp.ServeHTTP(w, r.WithContext(ctx))
	}))
}

func (s *Server) machineSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	settings, err := s.store.MachineSettings()
	if err != nil {
		writeError(w, 503, "Machine settings are unavailable.")
		return
	}
	var network any
	if s.machineNetwork != nil {
		network, err = s.machineNetwork(r.Context())
		if err != nil {
			network = map[string]any{"available": false, "reason": "Automatic network changes require Yard running in Docker Compose with accessible project files."}
		}
	}
	writeJSON(w, 200, struct {
		Settings    store.MachineSettings `json:"settings"`
		Operational bool                  `json:"operational"`
		Network     any                   `json:"networkStatus"`
	}{settings, settings.MCPEnabled && s.mcpListening.Load(), network})
}

func (s *Server) machineSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	var input store.MachineSettings
	if !decodeJSON(w, r, &input) {
		return
	}
	before, err := s.store.MachineSettings()
	if err != nil {
		writeError(w, 503, "Machine settings are unavailable.")
		return
	}
	settings, err := s.store.SaveMachineSettings(input)
	if err != nil {
		status := 400
		if err == store.ErrJobChanged || err == store.ErrOperationConflict {
			status = 409
		}
		writeError(w, status, err.Error())
		return
	}
	if settings.AutoApply && before.Network != settings.Network && s.machineApply != nil {
		if _, err := s.machineApply(r.Context(), settings.Revision); err != nil {
			writeJSON(w, 200, map[string]any{"settings": settings, "applyError": "Settings saved. Automatic application could not start; review and use Apply now."})
			return
		}
	}
	writeJSON(w, 200, map[string]any{"settings": settings})
}

func (s *Server) machineToken(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	var input struct {
		Revoke  bool `json:"revoke"`
		Confirm bool `json:"confirm"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.Confirm {
		writeError(w, 400, "Confirm token rotation or revocation.")
		return
	}
	admin, err := s.store.Admin()
	if err != nil {
		writeError(w, 503, "Administrator is unavailable.")
		return
	}
	token, err := s.store.RotateMachineToken(admin.Username, input.Revoke)
	if err != nil {
		writeError(w, 500, "Unable to change the Yard token.")
		return
	}
	writeJSON(w, 200, map[string]string{"token": token})
}
func (s *Server) machineHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	events, err := s.store.MachineTokenHistory()
	if err != nil {
		writeError(w, 500, "Unable to read token history.")
		return
	}
	writeJSON(w, 200, events)
}
func (s *Server) machineApplyNow(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
		Confirm  bool  `json:"confirm"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.Confirm {
		writeError(w, 400, "Confirm network recreation.")
		return
	}
	if s.machineApply == nil {
		writeError(w, 503, "Automatic recreation is unavailable.")
		return
	}
	job, err := s.machineApply(r.Context(), input.Revision)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 202, job)
}
