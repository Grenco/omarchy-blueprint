package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Release gate: a still-open read cycle observing A is never mutation authority
// after live state becomes B. No Capture/executor/Verify effect is permitted.
func TestReadPreviewCannotBecomeRestoreMutationAuthority(t *testing.T) {
	for _, id := range []string{"services", "packages"} {
		t.Run(id, func(t *testing.T) {
			p := &cycleExpensiveProvider{id: id}
			s := cycleSession(t, p, false)
			ctx := context.Background()
			c, e := s.BeginRead(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			preview, e := c.PreviewRestore(ctx, id, nil)
			if e != nil {
				t.Fatal(e)
			}
			before := p.loads.Load()
			p.live.Store(2)
			if _, e := s.ApplyApprovedRestore(ctx, id, nil, preview.Plan, false); !errors.Is(e, ErrRestorePlanChanged) {
				t.Fatalf("changed live state accepted stale approval: %v", e)
			}
			if p.loads.Load() <= before || p.mutations.Load() != 0 {
				t.Fatal("authority did not freshly inspect/refuse before effects")
			}
			unchanged, e := c.PreviewRestore(ctx, id, nil)
			if e != nil || unchanged.Plan.Skipped[0].Reason != "1" {
				t.Fatal("authority contaminated read cycle", e)
			}
		})
	}
}

type postStageCycleProvider struct {
	*cycleExpensiveProvider
	rollbacks int
}

func (p *postStageCycleProvider) Capture(_ context.Context, d *profile.Data, _ CaptureContext) (any, []model.Change, error) {
	p.mutations.Add(1)
	p.live.Store(3)
	d.Manifest.Capture.Services = true
	return nil, nil, nil
}
func (p *postStageCycleProvider) CommitCapture() error   { return nil }
func (p *postStageCycleProvider) FinalizeCapture() error { return nil }
func (p *postStageCycleProvider) RollbackCapture() error { p.rollbacks++; return nil }

func TestCapturePostStageRecheckStillObservesC(t *testing.T) {
	for _, id := range []string{"services", "packages"} {
		t.Run(id, func(t *testing.T) {
			p := &cycleExpensiveProvider{id: id}
			s := cycleSession(t, p, false)
			transaction := &postStageCycleProvider{cycleExpensiveProvider: p}
			if e := s.SetProviders([]Provider{transaction}); e != nil {
				t.Fatal(e)
			}
			saved, e := profile.Load(s.ProfileDir())
			if e != nil {
				t.Fatal(e)
			}
			ctx := context.Background()
			c, e := s.BeginRead(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			approved, e := c.InspectCaptureMany(ctx, []string{id})
			if e != nil {
				t.Fatal(e)
			}
			before := p.loads.Load()
			_, e = s.CaptureApproved(ctx, []string{id}, approved)
			var changed *CaptureReviewChangedError
			if !errors.As(e, &changed) {
				t.Fatal("post-stage state accepted", e)
			}
			if transaction.rollbacks != 1 || p.loads.Load() < before+3 {
				t.Fatal("post-stage inventory was not freshly rechecked/rolled back")
			}
			after, e := profile.Load(s.ProfileDir())
			if e != nil || !reflect.DeepEqual(saved, after) {
				t.Fatal("post-stage refusal changed saved profile", e)
			}
		})
	}
}

func TestReadPreviewCannotBecomeCaptureMutationAuthority(t *testing.T) {
	for _, id := range []string{"services", "packages"} {
		t.Run(id, func(t *testing.T) {
			p := &cycleExpensiveProvider{id: id}
			s := cycleSession(t, p, false)
			ctx := context.Background()
			c, e := s.BeginRead(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			review, e := c.InspectCaptureMany(ctx, []string{id})
			if e != nil {
				t.Fatal(e)
			}
			before := p.loads.Load()
			p.live.Store(2)
			_, e = s.CaptureApproved(ctx, []string{id}, review)
			var changed *CaptureReviewChangedError
			if !errors.As(e, &changed) {
				t.Fatalf("stale Capture review accepted: %v", e)
			}
			if p.loads.Load() <= before || p.mutations.Load() != 0 {
				t.Fatal("Capture did not freshly inspect/refuse before effects")
			}
		})
	}
}
