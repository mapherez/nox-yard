package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/logexport"
)

func (s *Server) projectLogExport(w http.ResponseWriter, r *http.Request) {
	write := r.Method == http.MethodPost
	if write && !s.checkOrigin(w, r) {
		return
	}
	if !s.requireSession(w, r, write) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var content *string
	containerID := ""
	if write {
		if r.Header.Get("Content-Type") != "application/json" {
			writeError(w, 415, "Content-Type must be application/json.")
			return
		}
		var input struct {
			Confirm     bool   `json:"confirm"`
			ContainerID string `json:"containerID"`
			Content     string `json:"content"`
		}
		// JSON control characters can expand to six bytes per snapshot byte.
		r.Body = http.MaxBytesReader(w, r.Body, 6*logexport.MaxBytes+4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(w, 400, "Invalid log export request.")
			return
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) || !input.Confirm {
			writeError(w, 400, "Confirm saving the current log snapshot.")
			return
		}
		content, containerID = &input.Content, input.ContainerID
	}
	status, err := s.application.ProjectLogExport(r.Context(), r.PathValue("id"), containerID, content)
	if err != nil {
		code, problem := application.ClassifyError(err, false)
		writeError(w, code, problem.Message)
		return
	}
	code := http.StatusOK
	if write {
		code = http.StatusCreated
	}
	writeJSON(w, code, status)
}
