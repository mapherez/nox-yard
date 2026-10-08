package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
)

type logLine = inventory.LogLine

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	id := r.PathValue("id")
	if !containerIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Invalid container ID.")
		return
	}
	if s.application.Logs == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker logs are unavailable.")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream, err := s.application.OpenLogs(ctx, id)
	if errors.Is(err, inventory.ErrContainerNotFound) {
		writeError(w, http.StatusNotFound, "Container no longer exists. Refresh the project list.")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Cannot read this container's logs from Docker.")
		return
	}
	defer stream.Reader.Close()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{})
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}

	lines := make(chan logLine, 64)
	completed := make(chan error, 1)
	go func() {
		readErr := inventory.DecodeLogs(ctx, stream, func(line inventory.LogLine) error {
			select {
			case lines <- line:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(lines)
		completed <- readErr
	}()

	heartbeat := time.NewTicker(s.sessionCheckInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-lines:
			if !ok {
				readErr := <-completed
				if readErr != nil && !errors.Is(readErr, context.Canceled) {
					_ = writeLogEvent(w, controller, "stream-error", map[string]string{"error": "Log stream ended unexpectedly."})
				} else {
					_ = writeLogEvent(w, controller, "end", map[string]string{})
				}
				return
			}
			if err := writeLogEvent(w, controller, "log", line); err != nil {
				return
			}
		case <-heartbeat.C:
			_, _, valid, err := s.currentSession(r)
			if err != nil || !valid {
				_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
				_ = writeLogEvent(w, controller, "session-expired", struct{}{})
				return
			}
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

func writeLogEvent(w io.Writer, controller *http.ResponseController, name string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, encoded); err != nil {
		return err
	}
	return controller.Flush()
}
