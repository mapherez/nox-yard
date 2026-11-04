package suite

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mapherez/nox-yard/internal/suite/local"
)

// Start activates the already implemented same-host adapter. Native HTTP and
// direct MCP remain separate; this listener never accepts remote TCP traffic.
func (c *Controller) Start(ctx context.Context, mcp http.Handler) (func(), error) {
	if c.cfg.Directory == "" {
		return func() {}, nil
	}
	listener, cleanup, err := local.Listen(c.cfg.Directory, "nox-yard")
	if err != nil {
		return nil, err
	}
	runContext, cancelRun := context.WithCancel(ctx)
	server := &http.Server{Handler: c.LocalHandler(mcp), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, BaseContext: func(_ net.Listener) context.Context { return runContext }}
	stopped := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { cancelRun(); close(stopped); _ = server.Close(); cleanup() }) }
	go func() { _ = server.Serve(listener) }()
	go func() {
		defer close(done)
		defer stop()
		timer := time.NewTicker(c.cfg.ReportInterval)
		defer timer.Stop()
		for {
			attempt, cancel := context.WithTimeout(runContext, 30*time.Second)
			_ = c.Reconcile(attempt)
			cancel()
			select {
			case <-runContext.Done():
				return
			case <-stopped:
				return
			case <-timer.C:
			}
		}
	}()
	return func() { stop(); <-done }, nil
}

func (c *Controller) Reconcile(ctx context.Context) error {
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	if err := c.trust.Reconcile(ctx); err != nil {
		c.markUnavailable()
		return err
	}
	if err := c.poll(ctx, 0); err != nil {
		c.markUnavailable()
		return err
	}
	return c.reconcileDeployments(ctx)
}
func (c *Controller) markUnavailable() { c.mu.Lock(); c.ready = false; c.mu.Unlock() }
