package workflow

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/observation"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"sync"
)

type scheduledReadProbe struct {
	id   string
	diff func(context.Context) error
}

func (p scheduledReadProbe) ID() string               { return p.id }
func (scheduledReadProbe) Captured(profile.Data) bool { return true }
func (scheduledReadProbe) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return nil, nil
}
func (p scheduledReadProbe) Diff(ctx context.Context, _ profile.Data) ([]model.Change, error) {
	if p.diff != nil {
		if err := p.diff(ctx); err != nil {
			return nil, err
		}
	}
	return []model.Change{{Provider: p.id, Name: p.id}}, nil
}
func scheduledDiff(ctx context.Context, p ReadProvider) (ProviderStatus, error) {
	changes, err := p.Diff(ctx, profile.Data{})
	return ProviderStatus{ID: p.ID(), Changes: changes}, err
}
func schedulerReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not make progress")
		var zero T
		return zero
	}
}

// A serial implementation cannot launch the first four probes; unbounded fanout
// starts probe five before release. Completion order must not become report order.
func TestReadSchedulerBoundsWorkersAndKeepsOrder(t *testing.T) {
	started := make(chan string, 8)
	release := make(chan struct{})
	var active, peak atomic.Int32
	providers := make([]ReadProvider, 8)
	for i := range providers {
		id := fmt.Sprint(i)
		providers[i] = scheduledReadProbe{id: id, diff: func(ctx context.Context) error {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- id
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		statuses []ProviderStatus
		err      error
	}
	done := make(chan result, 1)
	go func() { values, err := observeProviders(ctx, providers, 4, scheduledDiff); done <- result{values, err} }()
	for i := 0; i < 4; i++ {
		schedulerReceive(t, started)
	}
	select {
	case id := <-started:
		t.Fatalf("unbounded launch %s", id)
	default:
	}
	close(release)
	got := schedulerReceive(t, done)
	if got.err != nil {
		t.Fatal(got.err)
	}
	var ids []string
	for _, item := range got.statuses {
		ids = append(ids, item.ID)
		if len(item.Changes) != 1 || item.Changes[0].Provider != item.ID {
			t.Fatalf("misplaced result: %+v", item)
		}
	}
	if !reflect.DeepEqual(ids, []string{"0", "1", "2", "3", "4", "5", "6", "7"}) {
		t.Fatalf("order %v", ids)
	}
	if peak.Load() != 4 || active.Load() != 0 {
		t.Fatalf("peak=%d remaining=%d", peak.Load(), active.Load())
	}
}

func TestReadSchedulerDelayedFirstStillAppearsFirst(t *testing.T) {
	second := make(chan struct{})
	providers := []ReadProvider{
		scheduledReadProbe{id: "a", diff: func(ctx context.Context) error {
			select {
			case <-second:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}},
		scheduledReadProbe{id: "b", diff: func(context.Context) error { close(second); return nil }},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := observeProviders(ctx, providers, 2, scheduledDiff)
	if err != nil || len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("results=%v err=%v", got, err)
	}
}

func TestReadSchedulerCancelsAndJoinsOnMeaningfulFailure(t *testing.T) {
	started := make(chan struct{})
	joined := make(chan struct{})
	root := errors.New("probe failed")
	var future atomic.Int32
	providers := []ReadProvider{
		scheduledReadProbe{id: "a", diff: func(ctx context.Context) error { close(started); <-ctx.Done(); close(joined); return ctx.Err() }},
		scheduledReadProbe{id: "b", diff: func(context.Context) error { <-started; return root }},
		scheduledReadProbe{id: "c", diff: func(context.Context) error { future.Add(1); return nil }},
	}
	got, err := observeProviders(context.Background(), providers, 2, scheduledDiff)
	if !errors.Is(err, root) || got != nil {
		t.Fatalf("partial=%v err=%v", got, err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("worker survived failure")
	}
	if future.Load() != 0 {
		t.Fatal("launched future probe after failure")
	}
}

func TestReadSchedulerIndependentFailuresChooseProviderOrder(t *testing.T) {
	aReady := make(chan struct{})
	bReady := make(chan struct{})
	aErr, bErr := errors.New("a failed"), errors.New("b failed")
	providers := []ReadProvider{
		scheduledReadProbe{id: "a", diff: func(context.Context) error { close(aReady); <-bReady; return aErr }},
		scheduledReadProbe{id: "b", diff: func(context.Context) error { close(bReady); <-aReady; return bErr }},
	}
	_, err := observeProviders(context.Background(), providers, 2, scheduledDiff)
	if !errors.Is(err, aErr) {
		t.Fatalf("lost index precedence: %v", err)
	}
}

func TestReadSchedulerRejectsAuthorityAndInvalidLimitBeforeWork(t *testing.T) {
	var calls atomic.Int32
	observe := func(context.Context, ReadProvider) (ProviderStatus, error) {
		calls.Add(1)
		return ProviderStatus{}, nil
	}
	for _, limit := range []int{0, -1} {
		if _, err := observeProviders(context.Background(), nil, limit, observe); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if _, err := observeProviders(context.Background(), []ReadProvider{&cycleExpensiveProvider{id: "packages"}}, 4, observe); err == nil {
		t.Fatal("mutation provider accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request launched work")
	}
}

func TestReadSchedulerCanceledAndEmptyRequestsDoNotLaunch(t *testing.T) {
	var calls atomic.Int32
	observe := func(context.Context, ReadProvider) (ProviderStatus, error) {
		calls.Add(1)
		return ProviderStatus{}, nil
	}
	got, err := observeProviders(context.Background(), nil, 4, observe)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty=%v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = observeProviders(ctx, []ReadProvider{scheduledReadProbe{id: "a"}}, 4, observe)
	if !errors.Is(err, context.Canceled) || got != nil || calls.Load() != 0 {
		t.Fatalf("canceled=%v err=%v calls=%d", got, err, calls.Load())
	}
}

func TestReadSchedulerSubprocessCancellationDoesNotHideFailure(t *testing.T) {
	started := make(chan struct{})
	root := errors.New("meaningful failure")
	providers := []ReadProvider{
		scheduledReadProbe{id: "a", diff: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return &command.RunError{ExitCode: -1, Err: &exec.ExitError{}}
		}},
		scheduledReadProbe{id: "b", diff: func(context.Context) error { <-started; return root }},
	}
	_, err := observeProviders(context.Background(), providers, 2, scheduledDiff)
	if !errors.Is(err, root) {
		t.Fatalf("subprocess cancellation replaced root failure: %v", err)
	}
}

func schedulerCycle(ctx context.Context, providers ...ReadProvider) *ReadCycle {
	return &ReadCycle{observations: observation.New(ctx), providers: providers, finishDiagnostics: func() {}}
}

func TestStatusSchedulesFourReadProviders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 4)
	release := make(chan struct{})
	var providers []ReadProvider
	for _, id := range []string{"a", "b", "c", "d"} {
		providers = append(providers, scheduledReadProbe{id: id, diff: func(ctx context.Context) error {
			started <- id
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}})
	}
	c := schedulerCycle(ctx, providers...)
	defer c.Close()
	type result struct {
		report StatusReport
		err    error
	}
	done := make(chan result, 1)
	go func() { report, err := c.Status(ctx, ""); done <- result{report, err} }()
	for i := 0; i < 4; i++ {
		schedulerReceive(t, started)
	}
	close(release)
	got := schedulerReceive(t, done)
	if got.err != nil || len(got.report.Providers) != 4 {
		t.Fatalf("report=%v err=%v", got.report, got.err)
	}
	for i, id := range []string{"a", "b", "c", "d"} {
		if got.report.Providers[i].ID != id {
			t.Fatalf("order=%v", got.report.Providers)
		}
	}
}

func TestStatusCloseJoinsForwardedWorkers(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	finish := make(chan struct{})
	returned := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(finish) }) }
	defer release()
	c := schedulerCycle(context.Background(), scheduledReadProbe{id: "a", diff: func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-finish
		close(returned)
		return ctx.Err()
	}})
	done := make(chan error, 1)
	go func() { _, err := c.Status(context.Background(), ""); done <- err }()
	schedulerReceive(t, started)
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	schedulerReceive(t, canceled)
	select {
	case <-closed:
		t.Fatal("Close returned while forwarded worker was active")
	default:
	}
	release()
	schedulerReceive(t, closed)
	schedulerReceive(t, returned)
	if err := schedulerReceive(t, done); err == nil {
		t.Fatal("canceled Status returned success")
	}
}

type scheduledSlotProbe struct {
	scheduledReadProbe
	slot *observation.Slot[int]
}

func (p scheduledSlotProbe) Diff(ctx context.Context, _ profile.Data) ([]model.Change, error) {
	_, err := p.slot.Get(ctx)
	return nil, err
}

func TestStatusFailureJoinsSharedLoader(t *testing.T) {
	started := make(chan struct{})
	joined := make(chan struct{})
	c := schedulerCycle(context.Background())
	defer c.Close()
	slot := observation.NewSlot(c.observations, func(ctx context.Context) (int, error) {
		close(started)
		<-ctx.Done()
		close(joined)
		return 0, ctx.Err()
	}, func(v int) int { return v })
	root := errors.New("diff failed")
	c.providers = []ReadProvider{scheduledSlotProbe{scheduledReadProbe: scheduledReadProbe{id: "a"}, slot: slot}, scheduledReadProbe{id: "b", diff: func(context.Context) error { <-started; return root }}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Status(ctx, "")
	if !errors.Is(err, root) {
		t.Fatalf("failure=%v", err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("shared loader survived failed Status")
	}
	if _, err := c.Status(context.Background(), ""); !errors.Is(err, observation.ErrCycleClosed) {
		t.Fatalf("failed cycle reused: %v", err)
	}
}

func TestScheduledStatusRetainsSpecializedReports(t *testing.T) {
	c := schedulerCycle(context.Background(), NarrowReadProvider(overviewProvider{}), NarrowReadProvider(overviewResources{}))
	defer c.Close()
	report, err := c.Status(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Providers) != 2 || report.Providers[0].ID != "config" || report.Providers[1].ID != "resources" {
		t.Fatalf("provider order: %v", report.Providers)
	}
	if report.Providers[0].ConfigScan == nil || len(report.Providers[0].ConfigScan.Candidates) != 2 || report.Providers[0].ConfigScan.Candidates[0].Classification != configprovider.ConfigModifiedBaseline {
		t.Fatal("lost Config scan")
	}
	if !reflect.DeepEqual(report.Providers[1].ResourceGit, map[string]resourcesprovider.GitWorkingSummary{"repo": {UnstagedTracked: 1}}) {
		t.Fatal("lost Resources Git state")
	}
}

func TestScheduledStatusSharesFactsAcrossOverlappingRequestsAndReload(t *testing.T) {
	packages, services := &cycleExpensiveProvider{id: "packages"}, &cycleExpensiveProvider{id: "services"}
	s := cycleSession(t, packages, false)
	services.live.Store(1)
	if err := s.SetProviders([]Provider{packages, services}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c, err := s.BeginRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				report, err := c.Status(ctx, "")
				if err != nil {
					t.Error(err)
					return
				}
				if len(report.Providers) != 2 || report.Providers[0].ID != "packages" || report.Providers[1].ID != "services" {
					t.Error("unstable provider order")
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			if err := s.Reload(); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	if packages.loads.Load() != 1 || services.loads.Load() != 1 {
		t.Fatalf("loads packages=%d services=%d", packages.loads.Load(), services.loads.Load())
	}
	if packages.mutations.Load() != 0 || services.mutations.Load() != 0 {
		t.Fatal("read scheduler invoked authority")
	}
}

func TestReadSchedulerParentCancellationJoinsAndRejectsLateSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	done := make(chan error, 1)
	p := scheduledReadProbe{id: "a", diff: func(ctx context.Context) error { close(started); <-ctx.Done(); close(finished); return nil }}
	go func() {
		values, err := observeProviders(ctx, []ReadProvider{p}, 4, scheduledDiff)
		if values != nil {
			done <- errors.New("late success published")
			return
		}
		done <- err
	}()
	schedulerReceive(t, started)
	cancel()
	if err := schedulerReceive(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("worker survived cancellation")
	}
}

func TestReadSchedulerCancellationDoesNotHideMissingExecutable(t *testing.T) {
	started := make(chan struct{})
	missing := &exec.Error{Name: "missing-probe", Err: exec.ErrNotFound}
	providers := []ReadProvider{
		scheduledReadProbe{id: "a", diff: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return &command.RunError{ExitCode: -1, Err: missing}
		}},
		scheduledReadProbe{id: "b", diff: func(context.Context) error { <-started; return errors.New("other failure") }},
	}
	_, err := observeProviders(context.Background(), providers, 2, scheduledDiff)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("independent launch failure hidden: %v", err)
	}
}
