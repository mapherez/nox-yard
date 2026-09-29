package inventory

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

var ErrContainerNotRunning = errors.New("container is not running")
var ErrShellUnavailable = errors.New("terminal unavailable: this container has no /bin/sh shell")

type shellEntry struct {
	available bool
	expires   time.Time
}

// Check only on terminal use. Unknown Docker errors are not cached as absence.
func (r *DockerReader) shellAvailable(ctx context.Context, id string) (bool, error) {
	r.mu.RLock()
	entry, found := r.shells[id]
	generation := r.generation
	r.mu.RUnlock()
	if found && time.Now().Before(entry.expires) {
		return entry.available, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	_, err := r.client.ContainerStatPath(ctx, id, client.ContainerStatPathOptions{Path: "/bin/sh"})
	if err != nil && !errdefs.IsNotFound(err) {
		return false, err
	}
	available := err == nil
	r.mu.Lock()
	if r.shells == nil {
		r.shells = make(map[string]shellEntry)
	}
	if generation == r.generation {
		r.shells[id] = shellEntry{available: available, expires: time.Now().Add(5 * time.Minute)}
	}
	r.mu.Unlock()
	return available, nil
}

type TerminalSession struct {
	ID     string
	Reader io.Reader
	Writer io.Writer
	Close  func()
}

type TerminalManager interface {
	OpenTerminal(context.Context, string, uint, uint) (TerminalSession, error)
	ResizeTerminal(context.Context, string, uint, uint) error
	TerminalExitCode(context.Context, string) (int, error)
}

func (r *DockerReader) OpenTerminal(ctx context.Context, id string, cols, rows uint) (TerminalSession, error) {
	inspected, err := r.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if errdefs.IsNotFound(err) {
		return TerminalSession{}, ErrContainerNotFound
	}
	if err != nil {
		return TerminalSession{}, err
	}
	if inspected.Container.State == nil || !inspected.Container.State.Running {
		return TerminalSession{}, ErrContainerNotRunning
	}
	available, err := r.shellAvailable(ctx, id)
	if err != nil {
		return TerminalSession{}, err
	}
	if !available {
		return TerminalSession{}, ErrShellUnavailable
	}

	created, err := r.client.ExecCreate(ctx, id, client.ExecCreateOptions{
		Cmd:          []string{"/bin/sh"},
		Env:          []string{"TERM=xterm-256color"},
		TTY:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		ConsoleSize:  client.ConsoleSize{Height: rows, Width: cols},
	})
	if err != nil {
		return TerminalSession{}, err
	}
	attached, err := r.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{
		TTY:         true,
		ConsoleSize: client.ConsoleSize{Height: rows, Width: cols},
	})
	if err != nil {
		return TerminalSession{}, err
	}
	return TerminalSession{
		ID:     created.ID,
		Reader: attached.Reader,
		Writer: attached.Conn,
		Close: func() {
			_ = attached.CloseWrite()
			attached.Close()
		},
	}, nil
}

func (r *DockerReader) ResizeTerminal(ctx context.Context, id string, cols, rows uint) error {
	_, err := r.client.ExecResize(ctx, id, client.ExecResizeOptions{Width: cols, Height: rows})
	return err
}

func (r *DockerReader) TerminalExitCode(ctx context.Context, id string) (int, error) {
	result, err := r.client.ExecInspect(ctx, id, client.ExecInspectOptions{})
	return result.ExitCode, err
}
