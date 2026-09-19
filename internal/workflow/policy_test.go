package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func newPolicySession(t *testing.T, data profile.Data) *Session {
	t.Helper()
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{
		Now:       func() time.Time { return time.Unix(1, 0) },
		StateHome: func() (string, error) { return stateHome, nil },
	}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})
	return session
}

func TestEffectivePolicyResolvesProfileDefaultsScope(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Policy = policy.Rules{Capture: []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}}
	session := newPolicySession(t, data)

	got, err := session.EffectivePolicy(context.Background(), PolicyScope{}, "packages", TargetInspection{Key: "official:firefox"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Capture.Enabled || !got.Capture.Explicit || got.Capture.Source.Kind != policy.SourceProfileCategory {
		t.Fatalf("capture = %+v, want disabled/explicit/profile-category", got.Capture)
	}
	if !got.Restore.Enabled || got.Restore.Explicit {
		t.Fatalf("restore = %+v, want enabled default", got.Restore)
	}
}

func TestEffectivePolicyMachineTargetOverridesProfileCategory(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Policy = policy.Rules{Capture: []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}}
	data.Machines.Items = []profile.Machine{{
		Name:   "desktop",
		Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingEnabled}}},
	}}
	session := newPolicySession(t, data)

	got, err := session.EffectivePolicy(context.Background(), PolicyScope{Machine: "desktop"}, "packages", TargetInspection{Key: "official:firefox"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Capture.Enabled || !got.Capture.Explicit || got.Capture.Source.Kind != policy.SourceMachineTarget {
		t.Fatalf("capture = %+v, want the machine target rule to win over the profile category rule", got.Capture)
	}
}

func TestEffectivePolicyProfileDefaultsScopeIgnoresOtherMachinesRules(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{
		Name:   "desktop",
		Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}}},
	}}
	session := newPolicySession(t, data)

	got, err := session.EffectivePolicy(context.Background(), PolicyScope{}, "packages", TargetInspection{Key: "official:firefox"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Capture.Enabled || got.Capture.Explicit {
		t.Fatalf("capture = %+v, want the built-in default: profile-defaults scope must not see a machine's rules", got.Capture)
	}
}

func TestSetPolicyPersistsProfileDefaultsRuleAcrossReload(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	want := policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}}}
	if got := session.Profile().Policy; !rulesEqual(got, want) {
		t.Fatalf("policy = %+v, want %+v", got, want)
	}
	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if !rulesEqual(reloaded.Policy, want) {
		t.Fatalf("reloaded policy = %+v, want %+v", reloaded.Policy, want)
	}
}

func TestSetPolicyPersistsMachineScopedRule(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	session := newPolicySession(t, data)
	if err := session.SetPolicy(PolicyScope{Machine: "desktop"}, policy.AxisRestore, "packages", "", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	want := policy.Rules{Restore: []policy.Rule{{Category: "packages", Setting: policy.SettingDisabled}}}
	machine := session.Profile().Machines.Items[0]
	if !rulesEqual(machine.Policy, want) {
		t.Fatalf("machine policy = %+v, want %+v", machine.Policy, want)
	}
	if len(session.Profile().Policy.Capture) != 0 || len(session.Profile().Policy.Restore) != 0 {
		t.Fatalf("portable policy = %+v, want untouched", session.Profile().Policy)
	}
}

func TestSetPolicyUpsertsRatherThanDuplicating(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox", policy.SettingEnabled); err != nil {
		t.Fatal(err)
	}
	want := policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingEnabled}}}
	if got := session.Profile().Policy; !rulesEqual(got, want) {
		t.Fatalf("policy = %+v, want exactly one upserted rule %+v", got, want)
	}
}

func TestSetPolicyRejectsUnknownCategory(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "bogus", "", policy.SettingDisabled); err == nil {
		t.Fatal("unknown category accepted")
	}
}

func TestClearPolicyRemovesExplicitRule(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
	if err := session.ClearPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox"); err != nil {
		t.Fatal(err)
	}
	if got := session.Profile().Policy; len(got.Capture) != 0 {
		t.Fatalf("policy = %+v, want the rule cleared", got)
	}
}

func TestClearPolicyOfMissingRuleIsNotAnError(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.ClearPolicy(PolicyScope{}, policy.AxisCapture, "packages", "official:firefox"); err != nil {
		t.Fatal(err)
	}
}

func TestSetMachineRestoreDefaultsPersistsAndValidates(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	session := newPolicySession(t, data)
	if err := session.SetMachineRestoreDefaults("desktop", policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact}); err != nil {
		t.Fatal(err)
	}
	machine := session.Profile().Machines.Items[0]
	if got := machine.EffectiveRestoreDefaults(); got.Conflicts != policy.ConflictForce || got.Convergence != policy.ConvergenceExact {
		t.Fatalf("restore defaults = %+v, want Force/Exact", got)
	}
	if err := session.SetMachineRestoreDefaults("desktop", policy.RestoreOptions{Conflicts: "sometimes", Convergence: policy.ConvergenceAdditive}); err == nil {
		t.Fatal("invalid restore options accepted")
	}
}

func rulesEqual(a, b policy.Rules) bool {
	if len(a.Capture) != len(b.Capture) || len(a.Restore) != len(b.Restore) {
		return false
	}
	for i := range a.Capture {
		if a.Capture[i] != b.Capture[i] {
			return false
		}
	}
	for i := range a.Restore {
		if a.Restore[i] != b.Restore[i] {
			return false
		}
	}
	return true
}
