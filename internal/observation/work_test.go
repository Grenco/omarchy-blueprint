package observation

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// Without registration, Close returns while a forwarded provider still runs.
func TestCycleCloseJoinsRegisteredWork(t *testing.T) {
	c := New(context.Background())
	release, err := c.BeginWork()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	joined := make(chan struct{})
	go func() { c.Close(); close(joined) }()
	<-c.Context().Done()
	select {
	case <-joined:
		t.Fatal("Close returned with work outstanding")
	default:
	}
	if _, err := c.BeginWork(); !errors.Is(err, ErrCycleClosed) {
		t.Fatalf("late work: %v", err)
	}
	release()
	release()
	<-joined
}

func TestCycleWorkRegistrationRacesClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		c := New(context.Background())
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if release, err := c.BeginWork(); err == nil {
				release()
			} else if !errors.Is(err, ErrCycleClosed) {
				t.Errorf("registration: %v", err)
			}
		}()
		go func() { defer wg.Done(); c.Close() }()
		wg.Wait()
	}
}

func TestCanceledCycleDoesNotRegisterWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := New(ctx)
	defer c.Close()
	cancel()
	if _, err := c.BeginWork(); !errors.Is(err, context.Canceled) {
		t.Fatalf("registration: %v", err)
	}
}
