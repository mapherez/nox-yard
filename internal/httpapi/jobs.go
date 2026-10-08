package httpapi

import (
	"net/http"

	"github.com/mapherez/nox-yard/internal/application"
)

func (s *Server) jobHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	history, err := s.application.JobHistory(r.Context(), r.URL.Query().Get("target"))
	if err != nil {
		status, p := application.ClassifyError(err, false)
		writeError(w, status, p.Message)
		return
	}
	writeJSON(w, http.StatusOK, history)
}
func (s *Server) operationJob(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	job, found, err := s.application.Job(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read operation status.")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "Operation not found.")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
func (s *Server) acknowledgeRecovery(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	var input struct {
		Confirm   bool  `json:"confirm"`
		UpdatedAt int64 `json:"updatedAt"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	job, err := s.application.AcknowledgeRecovery(r.Context(), r.PathValue("id"), input.UpdatedAt, input.Confirm)
	if err != nil {
		status, p := application.ClassifyError(err, false)
		writeError(w, status, p.Message)
		return
	}
	writeJSON(w, http.StatusOK, job)
}
