package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/lifecycle"
)

func (s *Server) containerPull(w http.ResponseWriter, r *http.Request) {
	s.maintenanceOperation(w, r, true)
}

func (s *Server) projectPull(w http.ResponseWriter, r *http.Request) {
	s.maintenanceOperation(w, r, false)
}

func (s *Server) containerRemove(w http.ResponseWriter, r *http.Request) {
	s.removeOperation(w, r, true, false)
}

func (s *Server) projectRemove(w http.ResponseWriter, r *http.Request) {
	s.removeOperation(w, r, false, false)
}

func (s *Server) containerRemovePreview(w http.ResponseWriter, r *http.Request) {
	s.removeOperation(w, r, true, true)
}

func (s *Server) projectRemovePreview(w http.ResponseWriter, r *http.Request) {
	s.removeOperation(w, r, false, true)
}

func (s *Server) maintenanceOperation(w http.ResponseWriter, r *http.Request, container bool) {
	if !s.checkOrigin(w, r) || !s.requireSession(w, r, true) {
		return
	}
	id := r.PathValue("id")
	if container && !containerIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Invalid container ID.")
		return
	}
	if !container && !(strings.HasPrefix(id, "compose:") || strings.HasPrefix(id, "container:")) {
		writeError(w, http.StatusBadRequest, "Invalid project ID.")
		return
	}
	if s.lifecycle == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker management is unavailable.")
		return
	}

	// Image downloads can exceed the server's normal response deadline.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	var result lifecycle.MaintenanceResult
	var err error
	switch {
	case container:
		result, err = s.lifecycle.PullContainer(ctx, id)
	default:
		result, err = s.lifecycle.PullProject(ctx, id)
	}
	switch {
	case errors.Is(err, lifecycle.ErrNotFound):
		writeError(w, http.StatusNotFound, "Target no longer exists. Refresh the project list.")
	case errors.Is(err, lifecycle.ErrChanged):
		writeError(w, http.StatusConflict, "Project containers changed. Refresh the project list and confirm again.")
	case errors.Is(err, lifecycle.ErrProtected):
		writeError(w, http.StatusConflict, "This NoX Yard container cannot be removed or managed by that action.")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "Docker operation timed out.")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "Cannot complete this operation on the local Docker Engine.")
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) removeOperation(w http.ResponseWriter, r *http.Request, isContainer, preview bool) {
	if (!preview && !s.checkOrigin(w, r)) || !s.requireSession(w, r, !preview) {
		return
	}
	id := r.PathValue("id")
	if isContainer && !containerIDPattern.MatchString(id) || !isContainer && !(strings.HasPrefix(id, "compose:") || strings.HasPrefix(id, "container:")) {
		writeError(w, http.StatusBadRequest, "Invalid target ID.")
		return
	}
	if s.lifecycle == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker management is unavailable.")
		return
	}
	var input struct {
		Confirm     bool   `json:"confirm"`
		Fingerprint string `json:"fingerprint"`
	}
	if !preview {
		if !decodeJSON(w, r, &input) {
			return
		}
		if !input.Confirm || len(input.Fingerprint) != 64 {
			writeError(w, http.StatusBadRequest, "Review and confirm a current removal preview first.")
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	var result any
	var err error
	if preview {
		if isContainer {
			result, err = s.lifecycle.PreviewRemoveContainer(ctx, id)
		} else {
			result, err = s.lifecycle.PreviewRemoveProject(ctx, id)
		}
	} else if isContainer {
		result, err = s.lifecycle.RemoveContainer(ctx, id, input.Fingerprint)
	} else {
		result, err = s.lifecycle.RemoveProject(ctx, id, input.Fingerprint)
	}
	switch {
	case errors.Is(err, lifecycle.ErrNotFound):
		writeError(w, http.StatusNotFound, "Target no longer exists. Refresh the project list.")
	case errors.Is(err, lifecycle.ErrChanged):
		writeError(w, http.StatusConflict, "Docker resources changed since the preview. Reopen Remove and review the new list.")
	case errors.Is(err, lifecycle.ErrProtected):
		writeError(w, http.StatusConflict, "NoX Yard cannot remove itself or its temporary workers.")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "Docker operation timed out.")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, "Docker removal failed: "+err.Error())
	default:
		writeJSON(w, http.StatusOK, result)
	}
}
