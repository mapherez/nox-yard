package inventory

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"
)

// LogStream keeps Docker's raw stream format within the server. Callers must
// close Reader when the browser disconnects or the stream ends.
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
	reader, err := r.client.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     options.Follow,
		Tail:       strconv.Itoa(options.Tail),
	})
	if errdefs.IsNotFound(err) {
		return LogStream{}, ErrContainerNotFound
	}
	if err != nil {
		return LogStream{}, err
	}
	return LogStream{Reader: reader, TTY: inspected.Container.Config != nil && inspected.Container.Config.Tty}, nil
}

const MaxLogLineBytes = 16 * 1024

type LogLine struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

// DecodeLogs is shared by finite MCP snapshots and browser SSE streams.
// The caller owns and closes the underlying reader on cancellation.
func DecodeLogs(ctx context.Context, stream LogStream, emit func(LogLine) error) error {
	stdout := &lineWriter{ctx: ctx, stream: "stdout", emit: emit}
	stderr := &lineWriter{ctx: ctx, stream: "stderr", emit: emit}
	var err error
	if stream.TTY {
		_, err = io.Copy(stdout, stream.Reader)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, stream.Reader)
	}
	if flushErr := stdout.flush(); err == nil {
		err = flushErr
	}
	if flushErr := stderr.flush(); err == nil {
		err = flushErr
	}
	return err
}

type lineWriter struct {
	ctx    context.Context
	stream string
	emit   func(LogLine) error
	buffer []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	for i, char := range p {
		if err := w.ctx.Err(); err != nil {
			return i, err
		}
		if char == '\n' {
			if err := w.flush(); err != nil {
				return i, err
			}
			continue
		}
		w.buffer = append(w.buffer, char)
		if len(w.buffer) >= MaxLogLineBytes {
			if err := w.flush(); err != nil {
				return i + 1, err
			}
		}
	}
	return len(p), nil
}
func (w *lineWriter) flush() error {
	if len(w.buffer) == 0 {
		return nil
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	line := LogLine{Stream: w.stream, Text: strings.TrimSuffix(string(w.buffer), "\r")}
	w.buffer = w.buffer[:0]
	return w.emit(line)
}
