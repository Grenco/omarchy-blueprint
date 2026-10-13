package workflow

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"sort"
)

// TargetSelection is a per-operation narrowing, never persisted policy.
type TargetSelection map[string][]string

func (s *Session) InspectCaptureTargetsFresh(ctx context.Context, scope TargetSelection) (CaptureInspection, error) {
	ids, err := scope.categories()
	if err != nil {
		return CaptureInspection{}, err
	}
	if err := s.Reload(); err != nil {
		return CaptureInspection{}, err
	}
	review, err := s.inspectCaptureManyFresh(ctx, ids)
	if err != nil {
		return CaptureInspection{}, err
	}
	return narrowCaptureInspection(review, scope)
}
func (s *Session) CaptureApprovedTargets(ctx context.Context, scope TargetSelection, approved CaptureInspection) (CaptureResult, error) {
	ids, err := scope.categories()
	if err != nil {
		return CaptureResult{}, err
	}
	return s.captureScopedWithCheckpoint(ctx, ids, &approved, scope)
}

func (s TargetSelection) contains(category, key string) bool {
	for _, candidate := range s[category] {
		if candidate == key {
			return true
		}
	}
	return false
}
func (s TargetSelection) categories() ([]string, error) {
	if len(s) == 0 {
		return nil, fmt.Errorf("target scope must not be empty")
	}
	ids := make([]string, 0, len(s))
	for id, keys := range s {
		if id == "" || len(keys) == 0 {
			return nil, fmt.Errorf("invalid target scope")
		}
		seen := map[string]bool{}
		for _, key := range keys {
			if key == "" || seen[key] {
				return nil, fmt.Errorf("invalid or duplicate target selection")
			}
			seen[key] = true
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func narrowCaptureInspection(review CaptureInspection, scope TargetSelection) (CaptureInspection, error) {
	out := CaptureInspection{Categories: map[string][]CaptureTarget{}}
	for id, targets := range review.Categories {
		out.Categories[id] = append([]CaptureTarget(nil), targets...)
		for n := range out.Categories[id] {
			t := &out.Categories[id][n]
			if !scope.contains(id, t.Inspection.Key) {
				t.Decision.Capture = false
				t.Decision.Selected = false
				t.Selected = false
				t.Declined = false
				t.Outcome = CaptureOutcomePreserve
			} else if t.Inspection.RequiresSelection && t.Decision.Capture && t.Inspection.CaptureEligible {
				t.Selected = true
				t.Decision.Selected = true
				t.Outcome = captureOutcomeFor(t.Inspection, t.Decision, t.differs)
			}
		}
	}
	for id, keys := range scope {
		for _, key := range keys {
			found := false
			for _, t := range out.Categories[id] {
				if t.Inspection.Key == key {
					found = true
				}
			}
			if !found {
				return CaptureInspection{}, fmt.Errorf("selected Capture target is no longer observable; reinspect")
			}
		}
	}
	return out, nil
}

func narrowRestoreContext(rc RestoreContext, id string, scope TargetSelection) (RestoreContext, error) {
	for _, key := range scope[id] {
		if _, ok := rc.Targets[key]; !ok {
			return rc, fmt.Errorf("selected Restore target is no longer observable; reinspect")
		}
	}
	for key, decision := range rc.Targets {
		if !scope.contains(id, key) {
			decision.Restore = false
			decision.CompatibilityApply = false
			decision.Reason = "not selected for this run"
			rc.Targets[key] = decision
		}
	}
	return rc, nil
}

func checkCaptureTargetBoundary(before, after profile.Data, providers []Provider, scope TargetSelection) error {
	for _, p := range providers {
		projector, ok := p.(SyncDesiredProvider)
		if !ok {
			continue
		} // legacy callers retain their provider contract; Sync requires this capability at inspection.
		a, err := projector.SyncDesired(before)
		if err != nil {
			return err
		}
		b, err := projector.SyncDesired(after)
		if err != nil {
			return err
		}
		am, bm := syncTargetMap(a), syncTargetMap(b)
		for _, key := range syncKeys(am, bm) {
			if !scope.contains(key.Category, key.Target) && !syncState(am, key).Equal(syncState(bm, key)) {
				return fmt.Errorf("Capture changed an unselected semantic target; transaction refused")
			}
		}
	}
	return nil
}
