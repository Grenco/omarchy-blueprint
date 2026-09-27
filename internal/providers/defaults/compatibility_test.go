package defaults

import (
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilityDefaultsGetterAffirmsCurrentChoice(t *testing.T) {
	saved := profile.Defaults{Terminal: "alacritty"}
	got, err := RestoreCompatibility(saved, saved, map[string]bool{"terminal": true})
	if err != nil || got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged || len(got.Evidence) == 0 {
		t.Fatalf("matching managed default = %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityPortableChoiceUnestablishedRemainsUnknown(t *testing.T) {
	saved := profile.Defaults{Terminal: "alacritty"}
	current := profile.Defaults{Terminal: "foot"}
	got, err := RestoreCompatibility(saved, current, map[string]bool{"terminal": true})
	if err != nil || got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityUnchanged || len(got.Findings) != 1 || got.Findings[0].Code != "defaults.choice.unestablished" {
		t.Fatalf("uncertain semantic setter falsely supported: %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityNonportableDesktopAndAgentSafety(t *testing.T) {
	nonportable := profile.Defaults{Browser: "unmanaged.desktop"}
	got, err := RestoreCompatibility(nonportable, profile.Defaults{}, map[string]bool{"browser": true})
	if err != nil || got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked {
		t.Fatalf("raw desktop ID became portable: %+v err=%v", got, err)
	}
	saved := profile.Defaults{Agent: "codex"}
	plan := (Provider{}).Plan(saved, profile.Defaults{}, 13, "4.0", "5.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("agent setter became automatic: %+v", plan)
	}
	agent, err := RestoreCompatibility(saved, profile.Defaults{}, map[string]bool{"agent": true})
	if err != nil || agent.State != model.CompatibilityUnknown || agent.Authority != model.CompatibilityReduced || len(agent.Findings) != 1 {
		t.Fatalf("agent exclusion not visible: %+v err=%v", agent, err)
	}
}

func TestRestoreCompatibilityEmptyOrPolicySkippedDefaultHasNoIntent(t *testing.T) {
	for _, saved := range []profile.Defaults{{}, {Editor: "nvim"}} {
		got, err := RestoreCompatibility(saved, profile.Defaults{}, map[string]bool{"editor": false})
		if err != nil || got.Applies || got.State != "" || len(got.Findings) != 0 {
			t.Fatalf("unmanaged or skipped default = %+v err=%v", got, err)
		}
	}
}
