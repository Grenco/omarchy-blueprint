package workflow

import (
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"strings"
	"testing"
)

func TestSyncReconciledRestoreUsesDesiredSafety(t *testing.T) {
	key := syncengine.TargetKey{Category: "defaults", Target: "browser"}
	local := syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "local"}
	remote := syncengine.State{Kind: syncengine.StatePresent, Fingerprint: "remote"}
	identity := syncengine.Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("b", 40), Remote: strings.Repeat("c", 40), Machine: "test", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("d", 64), ControlFingerprint: strings.Repeat("e", 64)}
	plan, err := syncengine.Reconcile(syncengine.PlanRequest{Identity: identity, Targets: []syncengine.TargetInput{{Key: key, Base: local, Local: local, Remote: remote, Machine: local, Policy: syncengine.EffectivePolicy{Restore: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || len(plan.Items[0].Actions) != 1 || plan.Items[0].Actions[0] != syncengine.ActionRestore {
		t.Fatal(plan)
	}
	evidence := []SyncDesiredInspection{
		{Key: key, Desired: local, Inspection: TargetInspection{RestoreEligible: true}},
		{Key: key, Desired: remote, Inspection: TargetInspection{RestoreEligible: false}},
	}
	got := syncDesiredSafetyBlockers(plan.Items[0], TargetInspection{RestoreEligible: true}, evidence)
	if len(got) != 1 || got[0].Code != "restore-ineligible" {
		t.Fatal("local safety shadowed remote intent", got)
	}
	if !plan.Items[0].Policy.Restore {
		t.Fatal("safety rewrote policy")
	}
	evidence[1].Inspection.RestoreEligible = true
	if got := syncDesiredSafetyBlockers(plan.Items[0], TargetInspection{}, evidence); len(got) != 0 {
		t.Fatal("safe remote intent blocked", got)
	}
	if got := syncDesiredSafetyBlockers(plan.Items[0], TargetInspection{RestoreEligible: true}, evidence[:1]); len(got) != 1 || got[0].Code != "restore-evidence-missing" {
		t.Fatal("missing intent evidence allowed fallback", got)
	}
	evidence = append(evidence, SyncDesiredInspection{Key: key, Desired: remote})
	if got := syncDesiredSafetyBlockers(plan.Items[0], TargetInspection{}, evidence); len(got) != 1 || got[0].Code != "restore-ineligible" {
		t.Fatal("restrictive equal-intent view ignored", got)
	}
	item := plan.Items[0]
	item.Actions = []syncengine.PlannedAction{syncengine.ActionCapture}
	if got := syncDesiredSafetyBlockers(item, TargetInspection{CaptureEligible: true}, evidence); len(got) != 0 {
		t.Fatal("Restore evidence contaminated Capture", got)
	}
	item.Actions = []syncengine.PlannedAction{syncengine.ActionCapture, syncengine.ActionPublish}
	if got := syncDesiredSafetyBlockers(item, TargetInspection{CaptureEligible: true, RequiresSelection: true}, evidence); len(got) != 1 || got[0].Code != "target-review-required" {
		t.Fatal("duplicate review evidence", got)
	}
}

func TestSyncSafetyRemainsSeparateFromPolicy(t *testing.T) {
	key := syncengine.TargetKey{Category: "services", Target: "demo.service"}
	item := syncengine.Item{TargetInput: syncengine.TargetInput{Key: key, Policy: syncengine.EffectivePolicy{Capture: true, Restore: true}}, Actions: []syncengine.PlannedAction{syncengine.ActionCapture, syncengine.ActionRestore}}
	blockers := syncSafetyBlockers(item, TargetInspection{RequiresSelection: true})
	if len(blockers) != 3 {
		t.Fatal("missing capture/restore/adoption evidence", blockers)
	}
	if !item.Policy.Capture || !item.Policy.Restore {
		t.Fatal("safety silently changed policy")
	}
	for _, b := range blockers {
		if b.Target == nil || *b.Target != key {
			t.Fatal(b)
		}
	}
	item.Actions = nil
	if got := syncSafetyBlockers(item, TargetInspection{}); len(got) != 0 {
		t.Fatal("settled target falsely blocked", got)
	}
}
