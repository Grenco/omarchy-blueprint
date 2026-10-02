package observation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSlotSharesConcurrentLoad(t *testing.T) {
	c := New(context.Background())
	defer c.Close()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := NewSlot(c, func(context.Context) (int, error) { calls.Add(1); close(started); <-release; return 42, nil }, func(v int) int { return v })
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.Get(context.Background())
			if e != nil || v != 42 {
				t.Errorf("got %v, %v", v, e)
			}
		}()
	}
	<-started
	independent := NewSlot(c, func(context.Context) (int, error) { return 7, nil }, func(v int) int { return v })
	if v, e := independent.Get(context.Background()); e != nil || v != 7 {
		t.Fatalf("independent slot: %v %v", v, e)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("loads=%d", calls.Load())
	}
}

func TestSlotCachesFailureUntilNextCycle(t *testing.T) {
	sentinel := errors.New("probe failed")
	calls := 0
	for range 2 {
		c := New(context.Background())
		s := NewSlot(c, func(context.Context) (int, error) { calls++; return 0, sentinel }, func(v int) int { return v })
		for range 3 {
			if _, e := s.Get(context.Background()); !errors.Is(e, sentinel) {
				t.Fatal(e)
			}
		}
		c.Close()
	}
	if calls != 2 {
		t.Fatalf("loads=%d", calls)
	}
}

func TestSlotReturnsDefensiveCopyToFirstAndLaterCaller(t *testing.T) {
	c := New(context.Background())
	defer c.Close()
	s := NewSlot(c, func(context.Context) ([]int, error) { return []int{1}, nil }, func(v []int) []int { return append([]int(nil), v...) })
	for range 3 {
		v, e := s.Get(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if v[0] != 1 {
			t.Fatal(v)
		}
		v[0] = 99
	}
}

func TestSlotCanceledWaiterDoesNotCancelSharedLoad(t *testing.T) {
	c := New(context.Background())
	defer c.Close()
	started, release := make(chan struct{}), make(chan struct{})
	s := NewSlot(c, func(ctx context.Context) (int, error) { close(started); <-release; return 8, ctx.Err() }, func(v int) int { return v })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := s.Get(ctx); done <- e }()
	<-started
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	close(release)
	if v, e := s.Get(context.Background()); e != nil || v != 8 {
		t.Fatalf("%v %v", v, e)
	}
}

func TestAlreadyCanceledCallerDoesNotStartLoad(t *testing.T) {
	c := New(context.Background())
	defer c.Close()
	calls := 0
	s := NewSlot(c, func(context.Context) (int, error) { calls++; return 1, nil }, func(v int) int { return v })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := s.Get(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}

func TestCycleCancelPreventsPublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := New(ctx)
	defer c.Close()
	started, finished := make(chan struct{}), make(chan struct{})
	s := NewSlot(c, func(ctx context.Context) (int, error) { close(started); <-ctx.Done(); close(finished); return 9, nil }, func(v int) int { return v })
	done := make(chan error, 1)
	go func() { _, e := s.Get(context.Background()); done <- e }()
	<-started
	cancel()
	<-finished
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := s.Get(context.Background()); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestCycleCloseJoinsAndRejectsLateResult(t *testing.T) {
	c := New(context.Background())
	started, release := make(chan struct{}), make(chan struct{})
	s := NewSlot(c, func(ctx context.Context) (int, error) { close(started); <-ctx.Done(); <-release; return 9, nil }, func(v int) int { return v })
	done := make(chan error, 1)
	go func() { _, e := s.Get(context.Background()); done <- e }()
	<-started
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	// Wait until cycle cancellation, then Close must still be joining the loader.
	<-c.ctx.Done()
	select {
	case <-closed:
		t.Fatal("Close returned before loader exit")
	default:
	}
	close(release)
	<-closed
	if e := <-done; !errors.Is(e, ErrCycleClosed) {
		t.Fatal(e)
	}
	if _, e := s.Get(context.Background()); !errors.Is(e, ErrCycleClosed) {
		t.Fatal(e)
	}
}

func TestConcurrentGetAndClose(t *testing.T) {
	for range 100 {
		c := New(context.Background())
		s := NewSlot(c, func(ctx context.Context) (int, error) { return 1, ctx.Err() }, func(v int) int { return v })
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, e := s.Get(context.Background())
				if e != nil && !errors.Is(e, ErrCycleClosed) {
					t.Error(e)
				}
			}()
		}
		c.Close()
		wg.Wait()
	}
}

func TestCycleCloseIdempotent(t *testing.T) {
	c := New(context.Background())
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Close() }()
	}
	wg.Wait()
	s := NewSlot(c, func(context.Context) (int, error) { t.Error("late load"); return 0, nil }, func(v int) int { return v })
	if _, e := s.Get(context.Background()); !errors.Is(e, ErrCycleClosed) {
		t.Fatal(e)
	}
}
