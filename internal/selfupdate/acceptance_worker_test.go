package selfupdate

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Test-only process barrier: job success can precede worker cleanup. Keep the
// original test deadline and propagate inspection errors rather than retrying
// a failed update or weakening any runtime/resource assertion.
func waitSelfUpdateAcceptanceWorker(ctx context.Context, exited func(context.Context) (bool, error)) error {
	for {
		done, err := exited(ctx)
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestAcceptanceWaitsForWorkerCleanupAfterTerminalJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	inspected := make(chan struct{}, 1)
	cleanupFinished := make(chan struct{})
	returned := make(chan error, 1)
	go func() {
		returned <- waitSelfUpdateAcceptanceWorker(ctx, func(context.Context) (bool, error) {
			select {
			case <-cleanupFinished:
				return true, nil
			default:
				select {
				case inspected <- struct{}{}:
				default:
				}
				return false, nil
			}
		})
	}()
	select {
	case <-inspected:
	case <-ctx.Done():
		t.Fatal("worker was not inspected", ctx.Err())
	}
	select {
	case err := <-returned:
		t.Fatal("terminal job was treated as completed cleanup", err)
	default:
	}
	close(cleanupFinished)
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker completion was not observed", ctx.Err())
	}
}

func TestAcceptanceWorkerWaitKeepsDeadlineAndInspectionFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitSelfUpdateAcceptanceWorker(ctx, func(context.Context) (bool, error) {
		return false, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("worker still running must respect cancellation: %v", err)
	}
	inspectionFailure := errors.New("Docker inspection unavailable")
	if err := waitSelfUpdateAcceptanceWorker(context.Background(), func(context.Context) (bool, error) {
		return false, inspectionFailure
	}); !errors.Is(err, inspectionFailure) {
		t.Fatalf("inspection failure must not count as completed cleanup: %v", err)
	}
}
