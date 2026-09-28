package httpapi

import (
	"net/http"
)

func (s *Server) managedSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed projects are unavailable.")
		return
	}
	base, err := s.managed.ProjectsBase()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read projects directory.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ProjectsBase string `json:"projectsBase"`
	}{base})
}

func (s *Server) managedSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	if s.managed == nil {
		writeError(w, http.StatusServiceUnavailable, "Managed projects are unavailable.")
		return
	}
	var input struct {
		ProjectsBase string `json:"projectsBase"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	base, err := s.managed.SetProjectsBase(r.Context(), input.ProjectsBase)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		ProjectsBase string `json:"projectsBase"`
	}{base})
}
