package httpapi

import (
	"net/http"

	"github.com/mapherez/nox-yard/internal/suite"
)

func (s *Server) SetSuite(controller *suite.Controller) { s.suite = controller }
func (s *Server) suiteState(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	if s.suite == nil {
		writeError(w, 503, "Suite integration is unavailable.")
		return
	}
	view, err := s.suite.Snapshot(r.Context())
	if err != nil {
		writeError(w, 500, "Unable to load Suite integration.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, view)
}
func (s *Server) suiteRetry(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.suite == nil {
		writeError(w, 503, "Suite integration is unavailable.")
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.suite.Retry(r.Context(), r.PathValue("id"), input.Revision); err != nil {
		writeError(w, 409, "Suite integration changed; refresh before retrying.")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) suiteDeclaration(w http.ResponseWriter, r *http.Request) {
	if s.suite == nil {
		writeError(w, 503, "Suite integration is unavailable.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(s.suite.Declaration())
}
