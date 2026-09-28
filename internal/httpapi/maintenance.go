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
	s.maintenanceOperation(w, r, true, false)
}

func (s *Server) projectPull(w http.ResponseWriter, r *http.Request) {
	s.maintenanceOperation(w, r, false, false)
}

func (s *Server) containerRemove(w http.ResponseWriter, r *http.Request) {
	s.maintenanceOperation(w, r, true, true)
}

func (s *Server) projectRemove(w http.ResponseWriter, r *http.Request) {
	s.maintenanceOperation(w, r, false, true)
}

func (s *Server) maintenanceOperation(w http.ResponseWriter, r *http.Request, container, remove bool) {
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
	var expectedIDs []string
	if remove {
		var input struct {
			Confirm      bool     `json:"confirm"`
			ContainerIDs []string `json:"containerIds"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if !input.Confirm {
			writeError(w, http.StatusBadRequest, "Confirm removal before continuing.")
			return
		}
		if !container {
			if len(input.ContainerIDs) == 0 || len(input.ContainerIDs) > 256 {
				writeError(w, http.StatusBadRequest, "Expected container IDs are required.")
				return
			}
			for _, containerID := range input.ContainerIDs {
				if !containerIDPattern.MatchString(containerID) {
					writeError(w, http.StatusBadRequest, "Invalid expected container ID.")
					return
				}
			}
		}
		expectedIDs = input.ContainerIDs
	}
	if s.lifecycle == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker management is unavailable.")
		return
	}

	timeout := 2 * time.Minute
	if !remove {
		timeout = 10 * time.Minute
		// Image downloads can exceed the server's normal response deadline.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	var result lifecycle.MaintenanceResult
	var err error
	switch {
	case container && remove:
		result, err = s.lifecycle.RemoveContainer(ctx, id)
	case container:
		result, err = s.lifecycle.PullContainer(ctx, id)
	case remove:
		result, err = s.lifecycle.RemoveProject(ctx, id, expectedIDs)
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
