package plugins

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilityFirstPartyAvailabilityAndSkip(t *testing.T) {
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	missing, err := RestoreCompatibility(saved, profile.Plugins{}, map[string]bool{"plugin:omarchy.clock": true}, false)
	if err != nil || missing.State != model.CompatibilityIncompatible || missing.Authority != model.CompatibilityBlocked || len(missing.Findings) != 1 || missing.Findings[0].Code != "plugins.first_party.unavailable" {
		t.Fatalf("first-party plugin absence = %+v err=%v", missing, err)
	}
	skipped, err := RestoreCompatibility(saved, profile.Plugins{}, map[string]bool{"plugin:omarchy.clock": false}, false)
	if err != nil || skipped.Applies || len(skipped.Findings) != 0 {
		t.Fatalf("policy Skip still blocked: %+v err=%v", skipped, err)
	}
	// Shell, not Plugins, owns enablement. Present catalog membership is the
	// plugin-provider compatibility fact even if enabled differs.
	present, err := RestoreCompatibility(saved, profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: false}}}, map[string]bool{"plugin:omarchy.clock": true}, false)
	if err != nil || present.State != model.CompatibilitySupported || present.Authority != model.CompatibilityUnchanged {
		t.Fatalf("first-party available with Shell-owned enablement = %+v err=%v", present, err)
	}
}

func TestRestoreCompatibilityThirdPartyCannotClaimSupportedByProvenanceAlone(t *testing.T) {
	for _, plugin := range []profile.Plugin{
		{ID: "weather", Source: "git", URL: "https://user:token@example.invalid/weather.git", Revision: strings.Repeat("a", 40)},
		{ID: "weather", Source: "local", Hash: strings.Repeat("b", 64)},
	} {
		got, err := RestoreCompatibility(profile.Plugins{Items: []profile.Plugin{plugin}}, profile.Plugins{}, map[string]bool{"plugin:weather": true}, false)
		if err != nil || got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityUnchanged || len(got.Findings) != 1 || got.Findings[0].Code != "plugins.third_party.unestablished" {
			t.Fatalf("third-party provenance alone = %+v err=%v", got, err)
		}
		for _, evidence := range got.Evidence {
			if strings.Contains(evidence.Summary, "token") || strings.Contains(evidence.Summary, "example.invalid") || strings.Contains(evidence.Summary, "/tmp/") {
				t.Fatalf("plugin provenance leaked into public evidence: %+v", evidence)
			}
		}
	}
}

func TestRestoreCompatibilityThirdPartyExistingCatalogProvenance(t *testing.T) {
	plugin := profile.Plugin{ID: "weather", Source: "local", Hash: strings.Repeat("a", 64)}
	got, err := RestoreCompatibility(profile.Plugins{Items: []profile.Plugin{plugin}}, profile.Plugins{Items: []profile.Plugin{plugin}}, map[string]bool{"plugin:weather": true}, false)
	if err != nil || got.State != model.CompatibilitySupported || len(got.Evidence) == 0 {
		t.Fatalf("already-present matching catalog plugin = %+v err=%v", got, err)
	}
}
