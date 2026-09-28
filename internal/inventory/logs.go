package inventory

import (
	"context"
	"io"

	"github.com/containerd/errdefs"
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

func (r *DockerReader) OpenLogs(ctx context.Context, id string) (LogStream, error) {
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
		Follow:     true,
		Tail:       "200",
	})
	if errdefs.IsNotFound(err) {
		return LogStream{}, ErrContainerNotFound
	}
	if err != nil {
		return LogStream{}, err
	}
	return LogStream{Reader: reader, TTY: inspected.Container.Config != nil && inspected.Container.Config.Tty}, nil
}
