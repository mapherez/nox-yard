package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/mapherez/nox-yard/internal/application"
)

func (s *Server) projectSchedule(w http.ResponseWriter, r *http.Request) {
	write := r.Method == http.MethodPut
	if (write && !s.checkOrigin(w, r)) || !s.requireSession(w, r, write) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var result application.ScheduleStatus
	var err error
	if write {
		var input struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.Enabled == nil {
			writeError(w, http.StatusBadRequest, "Choose whether automatic updates are enabled.")
			return
		}
		result, err = s.application.SetProjectSchedule(ctx, r.PathValue("id"), *input.Enabled)
	} else {
		result, err = s.application.ProjectSchedule(ctx, r.PathValue("id"))
	}
	if err != nil {
		status, problem := application.ClassifyError(err, false)
		writeError(w, status, problem.Message)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
