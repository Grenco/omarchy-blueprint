package policy_test

import (
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func TestValidateSetting(t *testing.T) {
	for _, valid := range []policy.Setting{policy.SettingEnabled, policy.SettingDisabled} {
		if err := policy.ValidateSetting(valid); err != nil {
			t.Fatalf("valid setting %q: %v", valid, err)
		}
	}
	if err := policy.ValidateSetting(policy.Setting("maybe")); err == nil {
		t.Fatal("expected invalid setting to fail validation")
	}
}

func TestValidateRestoreOptions(t *testing.T) {
	valid := []policy.RestoreOptions{
		{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceAdditive},
		{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceAdditive},
		{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceExact},
		{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact},
	}
	for _, options := range valid {
		if err := policy.ValidateRestoreOptions(options); err != nil {
			t.Fatalf("valid options %+v: %v", options, err)
		}
	}
	if err := policy.ValidateRestoreOptions(policy.RestoreOptions{
		Conflicts: "unsafe", Convergence: policy.ConvergenceAdditive,
	}); err == nil {
		t.Fatal("expected invalid conflict mode")
	}
	if err := policy.ValidateRestoreOptions(policy.RestoreOptions{
		Conflicts: policy.ConflictSafe, Convergence: "partial",
	}); err == nil {
		t.Fatal("expected invalid convergence mode")
	}
}

func TestDefaultRestoreOptions(t *testing.T) {
	got := policy.DefaultRestoreOptions()
	want := policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceAdditive}
	if got != want {
		t.Fatalf("DefaultRestoreOptions() = %+v, want %+v", got, want)
	}
	if err := policy.ValidateRestoreOptions(got); err != nil {
		t.Fatalf("default options must validate: %v", err)
	}
}

func TestRuleAndRulesConstruction(t *testing.T) {
	rules := policy.Rules{
		Capture: []policy.Rule{{Category: "config", Setting: policy.SettingEnabled}},
		Restore: []policy.Rule{{Category: "config", Target: ".config/nvim", Setting: policy.SettingDisabled}},
	}
	if len(rules.Capture) != 1 || rules.Capture[0].Category != "config" {
		t.Fatalf("capture rules = %#v", rules.Capture)
	}
	if len(rules.Restore) != 1 || rules.Restore[0].Target != ".config/nvim" {
		t.Fatalf("restore rules = %#v", rules.Restore)
	}
}
