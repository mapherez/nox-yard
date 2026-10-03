package lifecycle

import (
	"context"
	"sync"
)

// contextMutex keeps the existing single-operation boundary while allowing a
// waiting request to leave when its deadline or client context is canceled.
// Its zero value is usable, including in managers with a test Docker transport.
type contextMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *contextMutex) Lock(ctx context.Context) error {
	m.once.Do(func() { m.token = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *contextMutex) Unlock() { <-m.token }
