package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/mapherez/nox-yard/internal/inventory"
)

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	metrics, err := s.application.Metrics(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Docker metrics are unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) projectEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requireSession(w, r, false) {
		return
	}
	if s.application.Inventory == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker inventory is unavailable.")
		return
	}
	changes, unsubscribe := s.changes.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(change inventory.Change) error {
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return writeLogEvent(w, controller, "change", change)
	}
	// No replay queue: reconnecting clients always reconcile authoritative state.
	if err := write(inventory.Change{Inventory: true, Metrics: true}); err != nil {
		return
	}
	heartbeat := time.NewTicker(s.sessionCheckInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case change := <-changes:
			if err := write(change); err != nil {
				return
			}
		case <-heartbeat.C:
			_, _, ok, err := s.currentSession(r)
			if err != nil || !ok {
				_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
				_ = writeLogEvent(w, controller, "session-expired", struct{}{})
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}
