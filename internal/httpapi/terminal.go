package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/mapherez/nox-yard/internal/inventory"
)

type terminalMessage struct {
	Type      string `json:"type"`
	CSRFToken string `json:"csrfToken,omitempty"`
	Cols      uint   `json:"cols,omitempty"`
	Rows      uint   `json:"rows,omitempty"`
	Code      int    `json:"code,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *Server) containerTerminal(w http.ResponseWriter, r *http.Request) {
	if !s.checkOrigin(w, r) {
		return
	}
	session, _, ok, err := s.currentSession(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Unable to read session.")
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, "Sign in to continue.")
		return
	}
	if !containerIDPattern.MatchString(r.PathValue("id")) {
		writeError(w, http.StatusBadRequest, "Invalid container ID.")
		return
	}
	if s.terminal == nil {
		writeError(w, http.StatusServiceUnavailable, "Docker terminal is unavailable.")
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64 * 1024)

	handshakeCtx, handshakeCancel := context.WithTimeout(r.Context(), 10*time.Second)
	messageType, payload, err := conn.Read(handshakeCtx)
	handshakeCancel()
	if err != nil {
		return
	}
	var initial terminalMessage
	if messageType != websocket.MessageText || json.Unmarshal(payload, &initial) != nil || initial.Type != "open" ||
		initial.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(initial.CSRFToken), []byte(session.CSRFToken)) != 1 ||
		!validTerminalSize(initial.Cols, initial.Rows) {
		_ = conn.Close(websocket.StatusPolicyViolation, "Invalid terminal request")
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	openCtx, openCancel := context.WithTimeout(ctx, 15*time.Second)
	stream, err := s.terminal.OpenTerminal(openCtx, r.PathValue("id"), initial.Cols, initial.Rows)
	openCancel()
	if err != nil {
		message := "Cannot open a shell in this container. It may not contain /bin/sh."
		if errors.Is(err, inventory.ErrContainerNotFound) {
			message = "Container no longer exists. Refresh the project list."
		} else if errors.Is(err, inventory.ErrContainerNotRunning) {
			message = "Start the container before opening a terminal."
		} else if errors.Is(err, inventory.ErrShellUnavailable) {
			message = "Terminal unavailable: this container has no /bin/sh shell."
		}
		_ = writeTerminalMessage(conn, terminalMessage{Type: "error", Error: message})
		return
	}
	defer stream.Close()
	if err := writeTerminalMessage(conn, terminalMessage{Type: "ready"}); err != nil {
		return
	}

	// Docker's TTY is a raw stream. Only this goroutine writes to the WebSocket
	// after the ready frame; the request goroutine handles input and resize.
	go func() {
		buffer := make([]byte, 16*1024)
		for {
			count, readErr := stream.Reader.Read(buffer)
			if count > 0 && conn.Write(ctx, websocket.MessageBinary, buffer[:count]) != nil {
				break
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					inspectCtx, inspectCancel := context.WithTimeout(ctx, 3*time.Second)
					code, inspectErr := s.terminal.TerminalExitCode(inspectCtx, stream.ID)
					inspectCancel()
					if inspectErr == nil {
						_ = writeTerminalMessage(conn, terminalMessage{Type: "exit", Code: code})
						break
					}
				}
				_ = writeTerminalMessage(conn, terminalMessage{Type: "error", Error: "Terminal connection ended."})
				break
			}
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		kind, data, readErr := conn.Read(ctx)
		if readErr != nil {
			return
		}
		if kind == websocket.MessageBinary {
			if _, err := stream.Writer.Write(data); err != nil {
				return
			}
			continue
		}
		if kind != websocket.MessageText {
			return
		}
		var control terminalMessage
		if json.Unmarshal(data, &control) != nil || control.Type != "resize" || !validTerminalSize(control.Cols, control.Rows) {
			_ = conn.Close(websocket.StatusPolicyViolation, "Invalid terminal message")
			return
		}
		resizeCtx, resizeCancel := context.WithTimeout(ctx, 3*time.Second)
		_ = s.terminal.ResizeTerminal(resizeCtx, stream.ID, control.Cols, control.Rows)
		resizeCancel()
	}
}

func validTerminalSize(cols, rows uint) bool {
	return cols >= 20 && cols <= 500 && rows >= 5 && rows <= 200
}

func writeTerminalMessage(conn *websocket.Conn, message terminalMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, payload)
}
