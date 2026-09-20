package components

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func TestPolicyPresentationRendersTabsScopeAndInheritedStatusAt80Columns(t *testing.T) {
	got := RenderPolicy(PolicyPresentation{
		Tab: "Capture", Scope: "Machine: laptop", Effective: policy.EffectiveSetting{Enabled: false, Source: policy.Source{Kind: policy.SourceProfileTarget}},
	}, 80, Styles{})
	for _, want := range []string{"State", "Capture", "Restore", "Machine: laptop", "Ignore", "inherited"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderPolicy = %q, want %q", got, want)
		}
	}
}

func TestPolicyPresentationKeepsBlockedDistinctFromRestoreSkip(t *testing.T) {
	blocked := RenderPolicy(PolicyPresentation{Tab: "Restore", Scope: "Profile defaults", Blocked: "unsafe on this machine"}, 80, Styles{})
	if !strings.Contains(blocked, "Blocked: unsafe on this machine") || strings.Contains(blocked, "Skip") {
		t.Fatalf("blocked = %q", blocked)
	}
	skip := RenderPolicy(PolicyPresentation{Tab: "Restore", Scope: "Profile defaults", Effective: policy.EffectiveSetting{Enabled: false, Explicit: true}}, 80, Styles{})
	if !strings.Contains(skip, "Skip") || !strings.Contains(skip, "explicit") {
		t.Fatalf("skip = %q", skip)
	}
}
