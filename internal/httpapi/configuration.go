package httpapi

import (
	"errors"
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
	case "override-preview", "override-apply":
		var input managed.OverrideRequest
		if !decodeManaged(w, r, &input) {
			return
		}
		if r.PathValue("operation") == "override-preview" {
			output, err = s.application.PreviewProjectOverride(r.Context(), r.PathValue("id"), input)
		} else {
			output, err = s.application.ApplyProjectOverride(r.Context(), r.PathValue("id"), input)
		}
	case "save":
		var input managed.ConfigurationEdit
		if !decodeManaged(w, r, &input) {
			return
		}
		output, err = s.application.SaveProjectConfiguration(r.Context(), r.PathValue("id"), input)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		code, problem := application.ClassifyError(err, false)
		writeConfigurationError(w, code, problem.Message, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func writeConfigurationError(w http.ResponseWriter, status int, message string, err error) {
	var diagnostic *managed.ComposeDiagnostic
	if errors.As(err, &diagnostic) {
		writeJSON(w, status, map[string]any{"error": message, "diagnostic": diagnostic})
		return
	}
	writeError(w, status, message)
}
