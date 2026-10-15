package httpapi

import (
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/managed"
	"net/http"
)

// Reading file contents is an explicit CSRF-protected POST, just like revealing
// runtime environment values. The response must never enter browser caches.
func (s *Server) projectConfiguration(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var output any
	var err error
	switch r.PathValue("operation") {
	case "read":
		var input struct {
			Reveal bool `json:"reveal"`
		}
		if r.ContentLength > 0 && !decodeJSON(w, r, &input) {
			return
		}
		output, err = s.application.ReadProjectConfiguration(r.Context(), r.PathValue("id"), input.Reveal)
	case "save":
		var input managed.ConfigurationEdit
		if !decodeManaged(w, r, &input) {
			return
		}
		output, err = s.application.SaveProjectConfiguration(r.Context(), r.PathValue("id"), input)
	case "retry-migration":
		var input struct {
			UseCurrentFiles bool `json:"useCurrentFiles"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		err = s.application.RetryProjectMigration(r.Context(), r.PathValue("id"), input.UseCurrentFiles)
		if err == nil {
			output, err = s.application.ProjectConfiguration(r.Context(), r.PathValue("id"))
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		code, problem := application.ClassifyError(err, false)
		writeError(w, code, problem.Message)
		return
	}
	writeJSON(w, http.StatusOK, output)
}
func (s *Server) configurationMigration(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	states, err := s.store.FileConfigurations(r.Context())
	if err != nil {
		writeError(w, 500, "Cannot read configuration migration status.")
		return
	}
	writeJSON(w, 200, states)
}
