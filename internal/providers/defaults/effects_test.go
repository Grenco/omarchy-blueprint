package defaults

import (
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Switching a default replaces the current choice: an update from Omarchy's
// fixed vocabulary, never an add (capability audit §4.2).
func TestDefaultSetIsDescribedAsClosedSetUpdate(t *testing.T) {
	plan := (Provider{}).Plan(profile.Defaults{Browser: "firefox", Terminal: "ghostty"}, profile.Defaults{Browser: "chromium"}, 1, "", "")
	if len(plan.Operations) != 2 {
		t.Fatalf("operations %+v", plan.Operations)
	}
	for _, op := range plan.Operations {
		e := op.Effect
		if e == nil || !e.Complete() || e.Family != "defaults.set" || e.Change != model.EffectUpdate || e.Source != model.SourceClosedSet {
			t.Fatalf("%s: %+v", op.ID, e)
		}
	}
}
