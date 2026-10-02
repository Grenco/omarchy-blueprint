package observation

import (
	"context"
	"sync"
)

// Slot loads at most once per cycle, sharing an in-flight load and its error.
// The loader transfers ownership of its result; clone must deeply copy it.
// Lock order is cycle then slot. Neither lock is held during loading; clones
// use only the slot lock so independent slots never serialize copying.
type Slot[T any] struct {
	mu      sync.Mutex
	cycle   *Cycle
	load    func(context.Context) (T, error)
	clone   func(T) T
	started bool
	done    chan struct{}
	value   T
	err     error
}

func NewSlot[T any](c *Cycle, load func(context.Context) (T, error), clone func(T) T) *Slot[T] {
	s := &Slot[T]{cycle: c, load: load, clone: clone, done: make(chan struct{})}
	c.mu.Lock()
	if !c.closed {
		c.clear = append(c.clear, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			var zero T
			s.value = zero
			s.load = nil
			s.clone = nil
		})
	}
	c.mu.Unlock()
	return s
}

// Get returns a defensive copy, including to the first caller. Canceling a
// waiter does not cancel the shared cycle-context load.
func (s *Slot[T]) Get(ctx context.Context) (T, error) {
	var zero T
	c := s.cycle
	c.mu.Lock()
	if err := c.errLocked(); err != nil {
		c.mu.Unlock()
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return zero, err
	}
	s.mu.Lock()
	if !s.started {
		s.started = true
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			value, err := s.load(c.ctx)
			c.mu.Lock()
			if cycleErr := c.errLocked(); cycleErr != nil {
				err = cycleErr
				value = zero
			}
			s.mu.Lock()
			s.value, s.err = value, err
			close(s.done)
			s.mu.Unlock()
			c.mu.Unlock()
		}()
	}
	s.mu.Unlock()
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.ctx.Done():
	case <-s.done:
	}
	c.mu.Lock()
	if err := c.errLocked(); err != nil {
		c.mu.Unlock()
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return zero, err
	}
	s.mu.Lock()
	c.mu.Unlock()
	defer s.mu.Unlock()
	if s.err != nil {
		return zero, s.err
	}
	return s.clone(s.value), nil
}
