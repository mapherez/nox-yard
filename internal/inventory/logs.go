package inventory

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// LogStream keeps Docker's raw stream format within the server. Callers must
// close Reader when the browser disconnects or the stream ends.
var ErrLogsUnavailable = errors.New("This container's logging driver does not make logs available through Docker.")

type LogStream struct {
	Reader io.ReadCloser
	TTY    bool
}

type LogReader interface {
	OpenLogs(context.Context, string) (LogStream, error)
}

type LogOptions struct {
	Follow bool
	Tail   int
	Since  string
	Until  string
}
type SnapshotLogReader interface {
	OpenLogsWithOptions(context.Context, string, LogOptions) (LogStream, error)
}

func (r *DockerReader) OpenLogs(ctx context.Context, id string) (LogStream, error) {
	return r.OpenLogsWithOptions(ctx, id, LogOptions{Follow: true, Tail: 200})
}
func (r *DockerReader) OpenLogsWithOptions(ctx context.Context, id string, options LogOptions) (LogStream, error) {
	inspected, err := r.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return LogStream{}, ErrContainerNotFound
	}
	if err != nil {
		return LogStream{}, err
	}
	if inspected.Container.HostConfig != nil && inspected.Container.HostConfig.LogConfig.Type == "none" {
		return LogStream{}, ErrLogsUnavailable
	}
	reader, err := r.client.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     options.Follow,
		Tail:       strconv.Itoa(options.Tail),
		Since:      options.Since, Until: options.Until,
	})
	if errdefs.IsNotFound(err) {
		return LogStream{}, ErrContainerNotFound
	}
	if err != nil {
		if strings.Contains(err.Error(), "does not support reading") || errdefs.IsNotImplemented(err) {
			return LogStream{}, ErrLogsUnavailable
		}
		return LogStream{}, err
	}
	return LogStream{Reader: reader, TTY: inspected.Container.Config != nil && inspected.Container.Config.Tty}, nil
}

// Transport chunks are explicit; they are never presented as separate lines.
const MaxLogLineBytes = 64 * 1024
const MaxLogPageBytes = 4 * 1024 * 1024

type LogLine struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}
type LogEvent struct {
	Stream       string `json:"stream"`
	Text         string `json:"text"`
	Timestamp    string `json:"timestamp,omitempty"`
	Continuation bool   `json:"continuation,omitempty"`
	LineEnd      bool   `json:"lineEnd"`
}

func DecodeLogEvents(ctx context.Context, stream LogStream, emit func(LogEvent) error) error {
	stdout := &eventWriter{ctx: ctx, stream: "stdout", emit: emit}
	stderr := &eventWriter{ctx: ctx, stream: "stderr", emit: emit}
	var err error
	if stream.TTY {
		stdout.stream = "combined"
		_, err = io.Copy(stdout, stream.Reader)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, stream.Reader)
	}
	if e := stdout.flush(false); err == nil {
		err = e
	}
	if e := stderr.flush(false); err == nil {
		err = e
	}
	return err
}

// Finite legacy snapshots still return complete logical records. Live/history
// adapters consume bounded events and reassemble them without invented newlines.
func DecodeLogs(ctx context.Context, stream LogStream, emit func(LogLine) error) error {
	pending := map[string]string{}
	err := DecodeLogEvents(ctx, stream, func(event LogEvent) error {
		text := pending[event.Stream] + event.Text
		if len(text) > MaxLogPageBytes {
			return errors.New("log record exceeds the finite snapshot limit; use streaming logs")
		}
		if event.LineEnd {
			delete(pending, event.Stream)
			return emit(LogLine{event.Stream, text})
		}
		pending[event.Stream] = text
		return nil
	})
	if err != nil {
		return err
	}
	for _, key := range []string{"stdout", "stderr", "combined"} {
		if text, ok := pending[key]; ok && text != "" {
			if err := emit(LogLine{key, text}); err != nil {
				return err
			}
		}
	}
	return nil
}

type eventWriter struct {
	ctx       context.Context
	stream    string
	emit      func(LogEvent) error
	buffer    []byte
	continued bool
	timestamp string
}

func (w *eventWriter) Write(p []byte) (int, error) {
	for i, char := range p {
		if err := w.ctx.Err(); err != nil {
			return i, err
		}
		if char == '\n' {
			if err := w.flush(true); err != nil {
				return i, err
			}
			continue
		}
		w.buffer = append(w.buffer, char)
		// Wait only for an incomplete trailing rune. Invalid bytes must not
		// prevent bounded delivery indefinitely; JSON replaces them with U+FFFD.
		if len(w.buffer) >= MaxLogLineBytes && (utf8.Valid(w.buffer) || len(w.buffer) >= MaxLogLineBytes+utf8.UTFMax) {
			if err := w.flush(false); err != nil {
				return i + 1, err
			}
		}
	}
	return len(p), nil
}
func (w *eventWriter) flush(end bool) error {
	if len(w.buffer) == 0 && !end {
		return nil
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	text := string(w.buffer)
	if end {
		text = strings.TrimSuffix(text, "\r")
	}
	if !w.continued {
		w.timestamp = ""
		if stamp, _, ok := strings.Cut(text, " "); ok {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				w.timestamp = stamp
			}
		}
	}
	event := LogEvent{Stream: w.stream, Text: text, Timestamp: w.timestamp, Continuation: w.continued, LineEnd: end}
	if err := w.emit(event); err != nil {
		return err
	}
	w.buffer = w.buffer[:0]
	w.continued = !end
	if end {
		w.timestamp = ""
	}
	return nil
}
