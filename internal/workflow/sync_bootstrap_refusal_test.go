package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// The TUI preflights first-time choices through ResolveSyncBootstrapPlan,
// so its refusal must name every affected target while keeping the exact
// rules and error text execution relies on.
func TestResolveSyncBootstrapPlanRefusalNamesEveryAffectedTarget(t *testing.T) {
	state := func(fp string) syncengine.State {
		return syncengine.State{Kind: syncengine.StatePresent, Fingerprint: fp}
	}
	key := func(target string) syncengine.TargetKey {
		return syncengine.TargetKey{Category: "config", Target: target}
	}
	plan := SyncBootstrapPlan{Identity: syncengine.Identity{Local: strings.Repeat("b", 40), Remote: strings.Repeat("c", 40)}, Git: syncengine.GitState{Clean: true, Ahead: 1}, Items: []SyncBootstrapItem{
		{Key: key("a"), Local: state("l"), Remote: state("r"), Machine: state("m")},
		{Key: key("b"), Local: state("x"), Remote: state("x"), Machine: state("x"), Policy: syncengine.EffectivePolicy{Capture: true}, Converged: true},
		{Key: key("c"), Local: state("l"), Remote: state("r"), Machine: state("m"), Policy: syncengine.EffectivePolicy{Restore: true}},
	}}
	_, err := ResolveSyncBootstrapPlan(plan, "adopt-machine")
	var refused *SyncBootstrapUnavailableError
	if !errors.As(err, &refused) || refused.Cause != SyncBootstrapCaptureProtected || len(refused.Targets) != 2 || refused.Targets[0].Key != key("a") || refused.Targets[1].Key != key("c") {
		t.Fatalf("adopt-machine refusal = %#v", err)
	}
	if err.Error() != "bootstrap adoption cannot bypass Capture policy" {
		t.Fatalf("error text changed: %q", err)
	}
	_, err = ResolveSyncBootstrapPlan(plan, "reconcile-upstream")
	if !errors.As(err, &refused) || refused.Cause != SyncBootstrapUnpublishedLocal || len(refused.Targets) != 2 || err.Error() != "baseless local committed intent differs; reconcile Git externally or explicitly adopt machine intent" {
		t.Fatalf("reconcile-upstream refusal = %#v", err)
	}
	for _, tc := range []struct {
		edit  func(*SyncBootstrapPlan)
		cause SyncBootstrapUnavailableCause
	}{
		{func(p *SyncBootstrapPlan) { p.Blockers = []syncengine.Blocker{{Code: "dirty-profile"}} }, SyncBootstrapBlocked},
		{func(p *SyncBootstrapPlan) { p.Git.Clean = false }, SyncBootstrapUncommitted},
		{func(p *SyncBootstrapPlan) { p.Git.Diverged = true }, SyncBootstrapDiverged},
	} {
		blocked := plan
		tc.edit(&blocked)
		_, err := ResolveSyncBootstrapPlan(blocked, "adopt-machine")
		if !errors.As(err, &refused) || refused.Cause != tc.cause || err.Error() != "bootstrap is blocked; divergent baseless history requires external Git reconciliation" {
			t.Fatalf("%s refusal = %#v", tc.cause, err)
		}
	}
}
