package policy_test

import (
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func rule(category, target string, setting policy.Setting) policy.Rule {
	return policy.Rule{Category: category, Target: target, Setting: setting}
}

func TestResolveMachineTargetBeatsProfileTarget(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisRestore,
		Machine:  "desktop",
		Category: "packages",
		Target:   "official:firefox",
		MachineRules: policy.Rules{Restore: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingDisabled),
		}},
		ProfileRules: policy.Rules{Restore: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingEnabled),
		}},
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !got.Explicit || got.Source.Kind != policy.SourceMachineTarget {
		t.Fatalf("got %+v, want disabled explicit machine-target", got)
	}
	if got.Source.Machine != "desktop" || got.Source.Category != "packages" || got.Source.Target != "official:firefox" {
		t.Fatalf("source = %+v", got.Source)
	}
}

func TestResolveMachineCategoryBeatsProfileTarget(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisCapture,
		Machine:  "desktop",
		Category: "packages",
		Target:   "official:firefox",
		MachineRules: policy.Rules{Capture: []policy.Rule{
			rule("packages", "", policy.SettingDisabled),
		}},
		ProfileRules: policy.Rules{Capture: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingEnabled),
		}},
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !got.Explicit || got.Source.Kind != policy.SourceMachineCategory {
		t.Fatalf("got %+v, want disabled explicit machine-category", got)
	}
}

func TestResolveNearestConfigAncestor(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:      policy.AxisCapture,
		Category:  "config",
		Target:    ".config/nvim/init.lua",
		Ancestors: []string{".config/nvim", ".config"},
		ProfileRules: policy.Rules{Capture: []policy.Rule{
			rule("config", ".config", policy.SettingDisabled),
			rule("config", ".config/nvim", policy.SettingEnabled),
		}},
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Explicit || got.Source.Kind != policy.SourceProfileAncestor || got.Source.Target != ".config/nvim" {
		t.Fatalf("got %+v, want enabled inherited from nearest ancestor .config/nvim", got)
	}
}

func TestResolveProfileTargetBeatsProfileCategory(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisRestore,
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{Restore: []policy.Rule{
			rule("packages", "", policy.SettingDisabled),
			rule("packages", "official:firefox", policy.SettingEnabled),
		}},
		DefaultEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || !got.Explicit || got.Source.Kind != policy.SourceProfileTarget {
		t.Fatalf("got %+v, want enabled explicit profile-target", got)
	}
}

func TestResolveProfileAncestorOrderingPrefersNearest(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:      policy.AxisRestore,
		Category:  "config",
		Target:    ".config/nvim/lua/plugins/init.lua",
		Ancestors: []string{".config/nvim/lua/plugins", ".config/nvim/lua", ".config/nvim", ".config"},
		ProfileRules: policy.Rules{Restore: []policy.Rule{
			rule("config", ".config", policy.SettingDisabled),
			rule("config", ".config/nvim", policy.SettingDisabled),
			rule("config", ".config/nvim/lua", policy.SettingEnabled),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Source.Target != ".config/nvim/lua" {
		t.Fatalf("got %+v, want nearest ancestor .config/nvim/lua to win", got)
	}
}

func TestResolveProfileCategoryBeatsDefault(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisCapture,
		Category: "hooks",
		Target:   "post-update.d/refresh-icons",
		ProfileRules: policy.Rules{Capture: []policy.Rule{
			rule("hooks", "", policy.SettingDisabled),
		}},
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || !got.Explicit || got.Source.Kind != policy.SourceProfileCategory {
		t.Fatalf("got %+v, want disabled explicit profile-category", got)
	}
}

func TestResolveFallsBackToProviderDefault(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:           policy.AxisRestore,
		Category:       "themes",
		Target:         "theme:catppuccin",
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Explicit || got.Source.Kind != policy.SourceDefault {
		t.Fatalf("got %+v, want enabled inherited default", got)
	}
}

func TestResolveCaptureAndRestoreAreIndependent(t *testing.T) {
	request := policy.ResolveRequest{
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{
			Capture: []policy.Rule{rule("packages", "official:firefox", policy.SettingDisabled)},
			Restore: []policy.Rule{rule("packages", "official:firefox", policy.SettingEnabled)},
		},
		DefaultEnabled: true,
	}
	request.Axis = policy.AxisCapture
	capture, err := policy.Resolve(request)
	if err != nil {
		t.Fatal(err)
	}
	if capture.Enabled {
		t.Fatalf("capture = %+v, want disabled", capture)
	}
	request.Axis = policy.AxisRestore
	restore, err := policy.Resolve(request)
	if err != nil {
		t.Fatal(err)
	}
	if !restore.Enabled {
		t.Fatalf("restore = %+v, want enabled", restore)
	}
}

func TestResolveRejectsDuplicateRules(t *testing.T) {
	_, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisCapture,
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{Capture: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingEnabled),
			rule("packages", "official:firefox", policy.SettingDisabled),
		}},
	})
	if err == nil {
		t.Fatal("expected duplicate rule to be rejected")
	}
}

func TestResolveRejectsInvalidSetting(t *testing.T) {
	_, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisCapture,
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{Capture: []policy.Rule{
			rule("packages", "official:firefox", policy.Setting("maybe")),
		}},
	})
	if err == nil {
		t.Fatal("expected invalid setting to be rejected")
	}
}

func TestResolveRejectsEmptyAxis(t *testing.T) {
	_, err := policy.Resolve(policy.ResolveRequest{
		Category: "packages",
		Target:   "official:firefox",
	})
	if err == nil {
		t.Fatal("expected empty axis to be rejected")
	}
}

func TestResolveRejectsUnknownAxis(t *testing.T) {
	_, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.Axis("restroe"),
		Category: "packages",
		Target:   "official:firefox",
	})
	if err == nil {
		t.Fatal("expected unknown axis to be rejected")
	}
}

func TestResolveUnknownAxisDoesNotSilentlyConsultCaptureRules(t *testing.T) {
	// A Restore resolution with a mistyped axis must fail rather than fall
	// through to Capture rules, which could turn a Restore Skip into an
	// apparent Apply (or vice versa).
	request := policy.ResolveRequest{
		Axis:     policy.Axis("restroe"),
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{
			Capture: []policy.Rule{rule("packages", "official:firefox", policy.SettingEnabled)},
			Restore: []policy.Rule{rule("packages", "official:firefox", policy.SettingDisabled)},
		},
	}
	if _, err := policy.Resolve(request); err == nil {
		t.Fatal("expected unknown axis to be rejected instead of resolving against Capture rules")
	}
}

func TestResolveProfileTargetIsInheritedWhenViewedFromMachineScope(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisRestore,
		Machine:  "desktop",
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{Restore: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingEnabled),
		}},
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Explicit {
		t.Fatalf("got %+v, want Explicit=false: a profile-scope rule is inherited, not an override, when viewed from machine scope", got)
	}
	if got.Source.Kind != policy.SourceProfileTarget {
		t.Fatalf("source = %+v, want profile-target", got.Source)
	}
}

func TestResolveProfileCategoryIsInheritedWhenViewedFromMachineScope(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisCapture,
		Machine:  "desktop",
		Category: "hooks",
		Target:   "post-update.d/refresh-icons",
		ProfileRules: policy.Rules{Capture: []policy.Rule{
			rule("hooks", "", policy.SettingDisabled),
		}},
		DefaultEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Explicit {
		t.Fatalf("got %+v, want Explicit=false: a profile-category rule is inherited, not an override, when viewed from machine scope", got)
	}
	if got.Source.Kind != policy.SourceProfileCategory {
		t.Fatalf("source = %+v, want profile-category", got.Source)
	}
}

func TestResolveMachineTargetIsExplicitAtMachineScope(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisRestore,
		Machine:  "desktop",
		Category: "packages",
		Target:   "official:firefox",
		MachineRules: policy.Rules{Restore: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingDisabled),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Explicit {
		t.Fatalf("got %+v, want Explicit=true: a machine-scope rule is an override at its own machine's scope", got)
	}
}

func TestResolveProfileTargetIsExplicitAtProfileDefaultsScope(t *testing.T) {
	got, err := policy.Resolve(policy.ResolveRequest{
		Axis:     policy.AxisRestore,
		Machine:  "",
		Category: "packages",
		Target:   "official:firefox",
		ProfileRules: policy.Rules{Restore: []policy.Rule{
			rule("packages", "official:firefox", policy.SettingEnabled),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Explicit {
		t.Fatalf("got %+v, want Explicit=true: a profile-scope rule is an override at profile-defaults scope", got)
	}
}
