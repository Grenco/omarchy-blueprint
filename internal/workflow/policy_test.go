package workflow

import (
	"context"
	"os"
	"path/filepath"
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

// TestEffectivePolicyAncestorRuleAppliesToDescendantWithoutItsOwnRule is Task
// 23's "Config hierarchy" scenario proven at the workflow layer: a rule set
// on an ancestor key (a directory, for Config) governs a descendant target
// that has no rule of its own, and a more specific rule on the descendant
// itself still wins over the ancestor -- "directory disabled, child enabled"
// -- without the provider needing any hierarchy logic of its own, since
// EffectivePolicy already resolves one decision per exact target using
// target.Ancestors.
func TestEffectivePolicyAncestorRuleAppliesToDescendantWithoutItsOwnRule(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Policy = policy.Rules{Capture: []policy.Rule{{Category: "config", Target: "nvim", Setting: policy.SettingDisabled}}}
	session := newPolicySession(t, data)
	session.SetProviders([]Provider{
		captureTestProvider{id: "packages", order: &[]string{}},
		captureTestProvider{id: "config", order: &[]string{}},
	})

	child := TargetInspection{Key: "nvim/init.lua", Ancestors: []string{"nvim"}}
	got, err := session.EffectivePolicy(context.Background(), PolicyScope{}, "config", child)
	if err != nil {
		t.Fatal(err)
	}
	if got.Capture.Enabled || got.Capture.Source.Kind != policy.SourceProfileAncestor {
		t.Fatalf("capture = %+v, want disabled via the ancestor rule", got.Capture)
	}

	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "config", "nvim/init.lua", policy.SettingEnabled); err != nil {
		t.Fatal(err)
	}
	got, err = session.EffectivePolicy(context.Background(), PolicyScope{}, "config", child)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Capture.Enabled || got.Capture.Source.Kind != policy.SourceProfileTarget {
		t.Fatalf("capture = %+v, want the child's own rule to win over its directory's", got.Capture)
	}

	sibling := TargetInspection{Key: "nvim/other.lua", Ancestors: []string{"nvim"}}
	got, err = session.EffectivePolicy(context.Background(), PolicyScope{}, "config", sibling)
	if err != nil {
		t.Fatal(err)
	}
	if got.Capture.Enabled {
		t.Fatalf("capture = %+v, want the untouched sibling to still resolve disabled via the ancestor rule", got.Capture)
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

// TestSetPolicyRejectsMalformedTarget is a regression for a review finding
// on PR 3: Task 13 requires provider target validation before target-
// specific persistence, so a malformed target must never be silently
// persisted as an explicit rule.
func TestSetPolicyRejectsMalformedTarget(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "not-a-valid-target", policy.SettingDisabled); err == nil {
		t.Fatal("malformed target accepted")
	}
	if got := session.Profile().Policy; len(got.Capture) != 0 {
		t.Fatalf("policy = %+v, want nothing persisted for the rejected target", got)
	}
}

// TestSetPolicyAcceptsCategoryLevelRuleWithEmptyTarget confirms an empty
// target (a category-level rule) is never run through target validation,
// since it has no target shape to validate.
func TestSetPolicyAcceptsCategoryLevelRuleWithEmptyTarget(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.SetPolicy(PolicyScope{}, policy.AxisCapture, "packages", "", policy.SettingDisabled); err != nil {
		t.Fatal(err)
	}
}

// TestClearPolicyRejectsMalformedTarget mirrors SetPolicy's rejection: a
// target that could never have been persisted by SetPolicy should not
// silently no-op as if the caller's typo were a legitimate clear.
func TestClearPolicyRejectsMalformedTarget(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.ClearPolicy(PolicyScope{}, policy.AxisCapture, "packages", "not-a-valid-target"); err == nil {
		t.Fatal("malformed target accepted")
	}
}

// TestStopManagingRejectsMalformedTarget is a regression for a review
// finding on PR 3: Stop Managing is a filesystem-affecting API and should
// reuse the same target validator SetPolicy does.
func TestStopManagingRejectsMalformedTarget(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.StopManaging(context.Background(), "packages", "not-a-valid-target"); err == nil {
		t.Fatal("malformed target accepted")
	}
}

// TestSetProvidersRejectsHandEditedProfilePolicyTarget is a regression for a
// review finding on PR 3: profile.Load can only validate a policy rule's
// category/setting/duplicates structurally -- it has no provider registry
// to check target shape/canonical identity against. A hand-edited
// policy.toml with a provider-invalid target (e.g. a packages target
// missing its "kind:" prefix) must fail once providers become available,
// rather than loading silently and only surfacing much later at the first
// unrelated SetPolicy call that happens to touch the same target.
func TestSetProvidersRejectsHandEditedProfilePolicyTarget(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Policy = policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "not-a-valid-target", Setting: policy.SettingDisabled}}}
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
	if err := session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}}); err == nil {
		t.Fatal("hand-edited invalid policy target accepted")
	}
}

// TestSetProvidersRejectsHandEditedMachinePolicyTarget mirrors the
// profile-scope case for a machine's own policy overrides, which
// profile.Load validates the same structural way and which real Restore
// planning would otherwise trip over as an invalid target much later.
func TestSetProvidersRejectsHandEditedMachinePolicyTarget(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{
		Name:   "desktop",
		Policy: policy.Rules{Restore: []policy.Rule{{Category: "packages", Target: "not-a-valid-target", Setting: policy.SettingEnabled}}},
	}}
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
	if err := session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}}); err == nil {
		t.Fatal("hand-edited invalid machine policy target accepted")
	}
}

// TestSetProvidersAcceptsValidHandEditedPolicyTargets is the paired
// happy-path: a well-formed target, and a category the registered provider
// set does not even cover, must both be accepted rather than over-rejected.
func TestSetProvidersAcceptsValidHandEditedPolicyTargets(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Policy = policy.Rules{
		Capture: []policy.Rule{
			{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled},
			{Category: "shell", Target: "shell", Setting: policy.SettingDisabled},
		},
	}
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
	if err := session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}}); err != nil {
		t.Fatal(err)
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

// TestSetPackageExcludedStripsDesiredStateAndRecordsPolicy is a regression
// for a review finding on PR 3: excluding a package must strip it from
// desired-present state immediately (no desired state at all, matching the
// design) and record Capture Disabled + Restore Disabled policy for it
// instead of a separate Packages.Excluded list; including must clear that
// policy without itself re-adding any desired state.
func TestSetPackageExcludedStripsDesiredStateAndRecordsPolicy(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox", "git"}
	session := newPolicySession(t, data)

	if err := session.SetPackageExcluded("official:firefox", true); err != nil {
		t.Fatal(err)
	}
	if !sameStrings(session.Profile().Packages.Official, []string{"git"}) {
		t.Fatalf("official = %#v, want firefox stripped", session.Profile().Packages.Official)
	}
	want := policy.Rules{
		Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}},
		Restore: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}},
	}
	if !rulesEqual(session.Profile().Policy, want) {
		t.Fatalf("policy = %+v, want %+v", session.Profile().Policy, want)
	}
	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(reloaded.Packages.Official, []string{"git"}) || !rulesEqual(reloaded.Policy, want) {
		t.Fatalf("reloaded official=%#v policy=%+v", reloaded.Packages.Official, reloaded.Policy)
	}

	if err := session.SetPackageExcluded("official:firefox", false); err != nil {
		t.Fatal(err)
	}
	if len(session.Profile().Policy.Capture) != 0 || len(session.Profile().Policy.Restore) != 0 {
		t.Fatalf("policy = %+v, want cleared", session.Profile().Policy)
	}
	if !sameStrings(session.Profile().Packages.Official, []string{"git"}) {
		t.Fatalf("official = %#v, want firefox still not re-added by include alone", session.Profile().Packages.Official)
	}
}

// TestSetPackageExcludedStripsMiseDeclaration confirms the mise branch of
// the same strip, distinct from Official/AUR since Mise is a map.
func TestSetPackageExcludedStripsMiseDeclaration(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Mise = profile.MiseTools{"node": profile.MiseTool{"version": "24"}}
	session := newPolicySession(t, data)

	if err := session.SetPackageExcluded("mise:node", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := session.Profile().Packages.Mise["node"]; ok {
		t.Fatalf("mise = %#v, want node stripped", session.Profile().Packages.Mise)
	}
}

// TestSetPackageExcludedClearsExistingTombstoneAndIncludeDoesNotRevive is a
// regression for a review finding on PR 3: excluding a package that is
// currently tombstoned (Packages.Absent) must clear that tombstone too --
// otherwise later including it would leave the stale desired-absence intent
// in place, ready to silently reassert itself as "still absent" on the next
// Capture instead of the ref genuinely becoming unmanaged.
func TestSetPackageExcludedClearsExistingTombstoneAndIncludeDoesNotRevive(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Absent = []profile.PackageAbsence{{Ref: "official:firefox"}}
	session := newPolicySession(t, data)

	if err := session.SetPackageExcluded("official:firefox", true); err != nil {
		t.Fatal(err)
	}
	if len(session.Profile().Packages.Absent) != 0 {
		t.Fatalf("absent = %#v, want the tombstone cleared by exclusion", session.Profile().Packages.Absent)
	}

	if err := session.SetPackageExcluded("official:firefox", false); err != nil {
		t.Fatal(err)
	}
	if len(session.Profile().Packages.Absent) != 0 {
		t.Fatalf("absent = %#v, want still cleared: include must not revive a stale tombstone", session.Profile().Packages.Absent)
	}
	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Packages.Absent) != 0 {
		t.Fatalf("reloaded absent = %#v, want cleared", reloaded.Packages.Absent)
	}
}

// TestSetPackageExcludedIsAtomicOnSaveFailure is a regression for a review
// finding on PR 3: SetPackageExcluded(true) used to strip desired state,
// save, then set two policy rules as three separate saves -- a failure
// between them could leave "desired state removed, but only zero/one policy
// axes persisted." It now builds the complete next state (desired-state
// removal and both policy axes) in memory and saves once, so a failed save
// must leave both the in-memory session and the on-disk profile completely
// untouched, not partially applied.
func TestSetPackageExcludedIsAtomicOnSaveFailure(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox"}
	session := newPolicySession(t, data)
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	realDir := session.opts.ProfileDir
	session.opts.ProfileDir = filepath.Join(blocked, "profile")

	if err := session.SetPackageExcluded("official:firefox", true); err == nil {
		t.Fatal("save failure not propagated")
	}
	if !sameStrings(session.Profile().Packages.Official, []string{"firefox"}) {
		t.Fatalf("official = %#v, want untouched in memory after a failed save", session.Profile().Packages.Official)
	}
	if len(session.Profile().Policy.Capture) != 0 || len(session.Profile().Policy.Restore) != 0 {
		t.Fatalf("policy = %+v, want no rules recorded after a failed save", session.Profile().Policy)
	}
	reloaded, err := profile.Load(realDir)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(reloaded.Packages.Official, []string{"firefox"}) || len(reloaded.Policy.Capture) != 0 {
		t.Fatalf("on-disk profile changed despite the failed save: official=%#v policy=%+v", reloaded.Packages.Official, reloaded.Policy)
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

func TestStopManagingUnknownProviderIsAnError(t *testing.T) {
	session := newPolicySession(t, profile.New("test", time.Now()))
	if err := session.StopManaging(context.Background(), "bogus", "official:firefox"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestStopManagingPropagatesProviderRejection(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox"}
	session := newPolicySession(t, data)
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}, stopManagingFail: true}})
	if err := session.StopManaging(context.Background(), "packages", "official:firefox"); err == nil {
		t.Fatal("provider rejection not propagated")
	}
	if got := session.Profile().Packages.Official; len(got) != 1 || got[0] != "firefox" {
		t.Fatalf("official = %#v, want untouched after rejection", got)
	}
}

func TestStopManagingDelegatesToProviderAndClearsPolicyOverrides(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox"}
	data.Policy = policy.Rules{
		Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}},
		Restore: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}},
	}
	data.Machines.Items = []profile.Machine{{
		Name:   "desktop",
		Policy: policy.Rules{Capture: []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingEnabled}}},
	}}
	session := newPolicySession(t, data)
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})

	if err := session.StopManaging(context.Background(), "packages", "official:firefox"); err != nil {
		t.Fatal(err)
	}
	if got := session.Profile().Packages.Official; len(got) != 0 {
		t.Fatalf("official = %#v, want the provider's removal applied", got)
	}
	if got := session.Profile().Policy; len(got.Capture) != 0 || len(got.Restore) != 0 {
		t.Fatalf("profile policy = %+v, want the target's rules cleared", got)
	}
	if got := session.Profile().Machines.Items[0].Policy; len(got.Capture) != 0 {
		t.Fatalf("machine policy = %+v, want the target's rules cleared", got)
	}
	reloaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Packages.Official) != 0 || len(reloaded.Policy.Capture) != 0 {
		t.Fatalf("reloaded = %#v, want the removal and policy clearing persisted", reloaded)
	}
}

// TestStopManagingRollsBackArtifactTransactionOnSaveFailure is a regression
// for a review finding on PR 3: a provider that stages its artifact removal
// (implementing the same commit/finalize/rollback hooks CaptureMany already
// uses) must have that removal rolled back, not finalized, when the profile
// save that forgets the target's metadata fails -- otherwise a failed save
// could leave the profile still referencing an artifact already destroyed.
func TestStopManagingRollsBackArtifactTransactionOnSaveFailure(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox"}
	session := newCaptureSession(t, data)
	commits, rollbacks, finalizes := 0, 0, 0
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		commits: &commits, rollbacks: &rollbacks, finalizes: &finalizes,
	}})
	// Point the save path at something profile.Save cannot create a
	// directory under, forcing a deterministic save failure after the
	// provider's own StopManaging has already "succeeded" (in this fake,
	// with no real filesystem side effect to stage).
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	session.opts.ProfileDir = filepath.Join(blocked, "profile")

	if err := session.StopManaging(context.Background(), "packages", "official:firefox"); err == nil {
		t.Fatal("save failure not propagated")
	}
	if commits != 0 || finalizes != 0 {
		t.Fatalf("commits=%d finalizes=%d, want neither called when save fails", commits, finalizes)
	}
	if rollbacks != 1 {
		t.Fatalf("rollbacks=%d, want exactly one rollback when save fails", rollbacks)
	}
	if len(session.Profile().Packages.Official) != 1 {
		t.Fatalf("official = %#v, want the session's in-memory profile untouched by the failed save", session.Profile().Packages.Official)
	}
}

// TestStopManagingCommitsAndFinalizesArtifactTransactionOnSaveSuccess proves
// the mirror image: a successful save commits and finalizes the staged
// artifact removal rather than leaving it pending.
func TestStopManagingCommitsAndFinalizesArtifactTransactionOnSaveSuccess(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"firefox"}
	session := newCaptureSession(t, data)
	commits, rollbacks, finalizes := 0, 0, 0
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		commits: &commits, rollbacks: &rollbacks, finalizes: &finalizes,
	}})

	if err := session.StopManaging(context.Background(), "packages", "official:firefox"); err != nil {
		t.Fatal(err)
	}
	if commits != 1 || finalizes != 1 || rollbacks != 0 {
		t.Fatalf("commits=%d finalizes=%d rollbacks=%d, want exactly one commit and finalize, no rollback", commits, finalizes, rollbacks)
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
