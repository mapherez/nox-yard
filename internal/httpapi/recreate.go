package httpapi

import (
	"context"
	"github.com/mapherez/nox-yard/internal/application"
	"github.com/mapherez/nox-yard/internal/recreate"
	"net/http"
	"time"
)

func (s *Server) recreateOperation(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("operation") != "preview" && r.PathValue("operation") != "submit" {
		http.NotFound(w, r)
		return
	}
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	var input recreate.Request
	if !decodeJSON(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var output any
	var err error
	status := http.StatusOK
	if r.PathValue("operation") == "preview" {
		output, err = s.application.RecreatePreview(ctx, input)
	} else {
		output, err = s.application.RecreateSubmit(ctx, input)
		status = http.StatusAccepted
	}
	if err != nil {
		code, problem := application.ClassifyError(err, false)
		writeError(w, code, problem.Message)
		return
	}
	writeJSON(w, status, output)
}
