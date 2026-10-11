package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

type scheduledSyncProbe struct {
	scheduledReadProbe
	observe func(context.Context) error
}

type slotSyncProbe struct {
	scheduledReadProbe
	slot *observation.Slot[int]
}

func (p slotSyncProbe) SyncObserve(ctx context.Context, _ profile.Data, _ []profile.Data) (SyncLiveObservation, error) {
	_, err := p.slot.Get(ctx)
	return SyncLiveObservation{}, err
}

func TestSyncSchedulerCancellationAndFailureJoinSharedLoader(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := schedulerCycle(context.Background())
			defer r.Close()
			started, canceled, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			slot := observation.NewSlot(r.observations, func(ctx context.Context) (int, error) {
				close(started)
				<-ctx.Done()
				close(canceled)
				<-release
				close(joined)
				return 1, nil // Late successful result must not survive cancellation.
			}, func(x int) int { return x })
			r.providers = []ReadProvider{slotSyncProbe{scheduledReadProbe: scheduledReadProbe{id: "slot"}, slot: slot}}
			wanted := errors.New("initiating failure")
			if failure {
				r.providers = append(r.providers, scheduledSyncProbe{scheduledReadProbe: scheduledReadProbe{id: "failure"}, observe: func(context.Context) error { <-started; return wanted }})
			}
			type result struct {
				value syncCycleObservation
				err   error
			}
			done := make(chan result, 1)
			go func() { value, err := r.observeSync(ctx, profile.Data{}, nil); done <- result{value, err} }()
			schedulerReceive(t, started)
			if !failure {
				cancel()
			}
			schedulerReceive(t, canceled)
			select {
			case <-done:
				t.Fatal("returned before shared loader joined")
			default:
			}
			close(release)
			got := schedulerReceive(t, done)
			if failure && !errors.Is(got.err, wanted) || !failure && !errors.Is(got.err, context.Canceled) {
				t.Fatal(got.err)
			}
			if len(got.value.Targets) != 0 {
				t.Fatal("partial observation")
			}
			select {
			case <-joined:
			default:
				t.Fatal("shared loader leaked")
			}
			if _, err := slot.Get(context.Background()); !errors.Is(err, observation.ErrCycleClosed) {
				t.Fatal("late result committed", err)
			}
		})
	}
}

func (p scheduledSyncProbe) SyncObserve(ctx context.Context, _ profile.Data, _ []profile.Data) (SyncLiveObservation, error) {
	if err := p.observe(ctx); err != nil {
		return SyncLiveObservation{}, err
	}
	return SyncLiveObservation{Targets: []syncengine.DesiredTarget{{Key: syncengine.TargetKey{Category: p.id, Target: "target"}, State: syncengine.State{Kind: syncengine.StatePresent, Fingerprint: strings.Repeat("a", 64)}}}}, nil
}

func TestSyncSchedulerBoundsWorkersAndKeepsOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 8)
	finished := make(chan string, 8)
	releases := make([]chan struct{}, 8)
	var active, peak atomic.Int32
	providers := make([]ReadProvider, 8)
	for i := range providers {
		releases[i] = make(chan struct{})
		id := fmt.Sprint(i)
		providers[i] = scheduledSyncProbe{scheduledReadProbe: scheduledReadProbe{id: id}, observe: func(ctx context.Context) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- id
			select {
			case <-releases[i]:
				finished <- id
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
	}
	r := schedulerCycle(ctx, providers...)
	defer r.Close()
	type result struct {
		value syncCycleObservation
		err   error
	}
	done := make(chan result, 1)
	go func() { value, err := r.observeSync(ctx, profile.Data{}, nil); done <- result{value, err} }()
	for i := 0; i < 4; i++ {
		schedulerReceive(t, started)
	}
	select {
	case id := <-started:
		t.Fatalf("unbounded Sync launch %s", id)
	default:
	}
	// Force the initial batch to complete in reverse provider order.
	for i := 3; i >= 0; i-- {
		close(releases[i])
		if got := schedulerReceive(t, finished); got != fmt.Sprint(i) {
			t.Fatal("unexpected completion", got)
		}
	}
	for i := 4; i < len(releases); i++ {
		close(releases[i])
	}
	got := schedulerReceive(t, done)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if peak.Load() != 4 || active.Load() != 0 {
		t.Fatal("wrong worker bound/join", peak.Load(), active.Load())
	}
	if len(got.value.Targets) != 8 {
		t.Fatal(got.value)
	}
	for i, target := range got.value.Targets {
		if target.Key.Category != fmt.Sprint(i) {
			t.Fatal("completion order became result order", got.value.Targets)
		}
	}
}

func TestSyncSchedulerErrorCancelsAndJoinsWithoutPartialResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	joined := make(chan struct{})
	wanted := errors.New("probe failure")
	providers := []ReadProvider{
		scheduledSyncProbe{scheduledReadProbe: scheduledReadProbe{id: "first"}, observe: func(ctx context.Context) error { close(started); <-ctx.Done(); close(joined); return ctx.Err() }},
		scheduledSyncProbe{scheduledReadProbe: scheduledReadProbe{id: "second"}, observe: func(context.Context) error { <-started; return wanted }},
	}
	r := schedulerCycle(ctx, providers...)
	defer r.Close()
	type result struct {
		value syncCycleObservation
		err   error
	}
	done := make(chan result, 1)
	go func() { value, err := r.observeSync(ctx, profile.Data{}, nil); done <- result{value, err} }()
	got := schedulerReceive(t, done)
	if !errors.Is(got.err, wanted) {
		t.Fatal(got.err)
	}
	if len(got.value.Targets) != 0 || len(got.value.Inspections) != 0 {
		t.Fatal("partial Sync observation escaped", got.value)
	}
	select {
	case <-joined:
	default:
		t.Fatal("returned before canceled provider joined")
	}
}
