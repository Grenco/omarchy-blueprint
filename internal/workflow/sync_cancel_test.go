package workflow

import (
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"testing"
	"time"
)

type syncBlockingRead struct {
	ReadProvider
	started, canceled, release chan struct{}
}

func (p *syncBlockingRead) SyncObserve(ctx context.Context, _ profile.Data, _ []profile.Data) (SyncLiveObservation, error) {
	close(p.started)
	<-ctx.Done()
	close(p.canceled)
	<-p.release
	return SyncLiveObservation{}, ctx.Err()
}

func TestSyncCycleCloseCancelsAndJoinsForwardedObservation(t *testing.T) {
	p := &syncBlockingRead{ReadProvider: narrowReadProvider(statusTestProvider{id: "packages"}), started: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	r := &ReadCycle{observations: observation.New(context.Background()), providers: []ReadProvider{p}, finishDiagnostics: func() {}}
	done := make(chan error, 1)
	go func() { _, err := r.observeSync(context.Background(), profile.Data{}, nil); done <- err }()
	<-p.started
	closed := make(chan struct{})
	go func() { r.Close(); close(closed) }()
	<-p.canceled
	premature := false
	select {
	case <-closed:
		premature = true
	case <-time.After(20 * time.Millisecond):
	}
	close(p.release)
	if err := <-done; !errors.Is(err, context.Canceled) && !errors.Is(err, observation.ErrCycleClosed) {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cycle close leaked work")
	}
	if premature {
		t.Fatal("cycle closed before forwarded observation joined")
	}
	if _, err := r.observeSync(context.Background(), profile.Data{}, nil); !errors.Is(err, observation.ErrCycleClosed) {
		t.Fatal("closed cycle accepted result", err)
	}
}
