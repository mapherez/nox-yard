package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
	"github.com/moby/moby/api/pkg/stdcopy"
)

const maxLogLineBytes = 16 * 1024

type logLine struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	id := r.PathValue("id")
	if !containerIDPattern.MatchString(id) {
		writeError(w, http.StatusBadRequest, "Invalid container ID.")
		return
	}
	if s.logs == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker logs are unavailable.")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stream, err := s.logs.OpenLogs(ctx, id)
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
		stdout := &logLineWriter{ctx: ctx, stream: "stdout", lines: lines}
		stderr := &logLineWriter{ctx: ctx, stream: "stderr", lines: lines}
		var readErr error
		if stream.TTY {
			_, readErr = io.Copy(stdout, stream.Reader)
		} else {
			_, readErr = stdcopy.StdCopy(stdout, stderr, stream.Reader)
		}
		_ = stdout.Flush()
		_ = stderr.Flush()
		close(lines)
		completed <- readErr
	}()

	heartbeat := time.NewTicker(15 * time.Second)
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

type logLineWriter struct {
	ctx    context.Context
	stream string
	lines  chan<- logLine
	buffer []byte
}

func (w *logLineWriter) Write(p []byte) (int, error) {
	for i, char := range p {
		if char == '\n' {
			if err := w.Flush(); err != nil {
				return i, err
			}
			continue
		}
		w.buffer = append(w.buffer, char)
		if len(w.buffer) >= maxLogLineBytes {
			if err := w.Flush(); err != nil {
				return i + 1, err
			}
		}
	}
	return len(p), nil
}

func (w *logLineWriter) Flush() error {
	if len(w.buffer) == 0 {
		return nil
	}
	line := logLine{Stream: w.stream, Text: strings.TrimSuffix(string(w.buffer), "\r")}
	w.buffer = w.buffer[:0]
	select {
	case w.lines <- line:
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}
