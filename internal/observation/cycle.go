// Package observation owns short-lived, read-only machine observations.
// Its facts must never substitute for fresh mutation-authority inspection.
package observation

import (
	"context"
	"errors"
	"sync"
)

var ErrCycleClosed = errors.New("observation cycle closed")

// Cycle owns loaders and their retained facts. The owner must call Close.
type Cycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	joined chan struct{}
	wg     sync.WaitGroup
	clear  []func()
}

func New(ctx context.Context) *Cycle {
	ctx, cancel := context.WithCancel(ctx)
	return &Cycle{ctx: ctx, cancel: cancel, joined: make(chan struct{})}
}

// errLocked gives explicit closure precedence over parent cancellation.
func (c *Cycle) errLocked() error {
	if c.closed {
		return ErrCycleClosed
	}
	return c.ctx.Err()
}

// Err rejects work after closure or parent cancellation, even if a projection
// does not require an observation slot.
func (c *Cycle) Err() error { c.mu.Lock(); defer c.mu.Unlock(); return c.errLocked() }

// Context is canceled when the cycle's parent is canceled or it is closed.
func (c *Cycle) Context() context.Context { return c.ctx }

// BeginWork registers a read projection which may run forwarded providers
// outside a slot. Close cancels it and waits for release as well as loaders.
// The caller must release before calling Close; release is idempotent.
func (c *Cycle) BeginWork() (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.errLocked(); err != nil {
		return nil, err
	}
	c.wg.Add(1)
	var once sync.Once
	return func() { once.Do(c.wg.Done) }, nil
}

// Close cancels and joins all launched loaders, then releases retained facts.
// Concurrent Close callers all wait for the same completion.
func (c *Cycle) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		<-c.joined
		return
	}
	c.closed = true
	c.cancel()
	c.mu.Unlock()
	c.wg.Wait()
	c.mu.Lock()
	for _, clear := range c.clear {
		clear()
	}
	c.clear = nil
	close(c.joined)
	c.mu.Unlock()
}
