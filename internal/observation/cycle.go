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
