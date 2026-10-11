package workflow

import (
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"testing"
)

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
