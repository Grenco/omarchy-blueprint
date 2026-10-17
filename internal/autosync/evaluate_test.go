package autosync

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func decide(t *testing.T, g Grant, ev Evidence, ops ...OperationInput) Evaluation {
	t.Helper()
	return Evaluate(g, testIdentity("/profile"), testRegistry(), ev, ops)
}

func TestProductionRegistryAuthorisesNothingInPRA(t *testing.T) {
	g := activeGrant("/profile", PresetHandsOff, Categories...)
	g.RunCode = map[string]RunCodeScope{"themes": {}, "hooks": {}, "defaults": {}}
	g.PrivilegedInstall = true
	g = approved(g)
	all := evidenceFor{}
	for _, c := range []Condition{CondSourceHashEnforced, CondNotDesiredCurrent, CondAuditedOmarchy, CondInstalledApp, CondNoThemeHook} {
		all[c] = true
	}
	result := Evaluate(g, testIdentity("/profile"), DefaultRegistry(), all, []OperationInput{themeCopy("garden"), defaultSet("browser"), hookWrite("theme-set")})
	if result.Eligible {
		t.Fatal("PR A authorised unattended mutation")
	}
	for _, d := range result.Decisions {
		if d.Eligible || d.Reason != ReasonNotAvailableYet || d.Increment == "" {
			t.Fatalf("%s: %+v", d.OperationID, d)
		}
	}
}

func TestGrantGatesBlockEverything(t *testing.T) {
	base := activeGrant("/profile", PresetHandsOff, Categories...)
	cases := map[Reason]func(Grant) Grant{
		ReasonOff:               func(g Grant) Grant { g.Setting = SettingOff; return g },
		ReasonPaused:            func(g Grant) Grant { g.Setting = SettingPaused; return g },
		ReasonSuspended:         func(g Grant) Grant { g.Suspended = &Suspension{Reason: "incomplete"}; return g },
		ReasonIdentityMismatch:  func(g Grant) Grant { g.Identity.HostID = testRemote; return approved(g) },
		ReasonCapabilityVersion: func(g Grant) Grant { g.CapabilityVersion = "restore-capabilities/0"; return approved(g) },
		ReasonApprovalOutOfDate: func(g Grant) Grant {
			g.Categories["packages"] = CategoryScope{AllTargets: true}
			delete(g.Categories, "themes")
			g.Categories["themes"] = CategoryScope{Targets: []string{"x"}}
			return g
		},
		ReasonNotifyOnly: func(g Grant) Grant { g.Preset = PresetNotifyOnly; return approved(g) },
	}
	for want, mutate := range cases {
		g := mutate(activeGrant("/profile", base.Preset, Categories...))
		result := decide(t, g, nil, themeCopy("garden"))
		if result.Eligible || result.Decisions[0].Reason != want {
			t.Fatalf("%s: got %+v", want, result.Decisions[0])
		}
	}
}

func TestHardDenialsBeatDeliveredEntries(t *testing.T) {
	g := activeGrant("/profile", PresetHandsOff, Categories...)
	cases := map[Reason]func(*model.Effect){
		ReasonInteractionRequired: func(e *model.Effect) { e.Interaction = model.InteractionRequired },
		ReasonUnpinnedSource:      func(e *model.Effect) { e.Source = model.SourceUnpinned },
		ReasonUnprovenRemoval:     func(e *model.Effect) { e.Change = model.EffectRemove; e.Recovery = model.RecoveryNone },
		ReasonUndescribed:         func(e *model.Effect) { e.Payload = "" },
	}
	for want, mutate := range cases {
		op := themeCopy("garden")
		copied := *op.Operation.Effect
		mutate(&copied)
		op.Operation.Effect = &copied
		if d := decide(t, g, nil, op).Decisions[0]; d.Eligible || d.Reason != want {
			t.Fatalf("%s: %+v", want, d)
		}
	}
	undescribed := themeCopy("garden")
	undescribed.Operation.Effect = nil
	if d := decide(t, g, nil, undescribed).Decisions[0]; d.Reason != ReasonUndescribed {
		t.Fatalf("nil descriptor: %+v", d)
	}
	foreign := themeCopy("garden")
	foreign.Operation.Provider = "config"
	if d := decide(t, g, nil, foreign).Decisions[0]; d.Reason != ReasonUnresolvedProvider {
		t.Fatalf("foreign family: %+v", d)
	}
	unregistered := themeCopy("garden")
	unregistered.Operation.Effect.Family = "themes.force-write"
	if d := decide(t, g, nil, unregistered).Decisions[0]; d.Reason != ReasonUnregistered {
		t.Fatalf("unregistered: %+v", d)
	}
}

func TestPresetsAreStrictlyOrdered(t *testing.T) {
	ops := []OperationInput{themeCopy("garden"), defaultSet("browser"), hookWrite("theme-set")}
	ev := evidenceFor{CondInstalledApp: true}
	eligible := map[Preset][]bool{}
	for _, preset := range []Preset{PresetConservative, PresetBalanced, PresetHandsOff} {
		g := activeGrant("/profile", preset, "themes", "defaults", "hooks")
		if preset == PresetHandsOff {
			g.RunCode = map[string]RunCodeScope{"hooks": {}}
			g = approved(g)
		}
		for _, d := range decide(t, g, ev, ops...).Decisions {
			eligible[preset] = append(eligible[preset], d.Eligible)
		}
	}
	want := map[Preset][]bool{PresetConservative: {true, false, false}, PresetBalanced: {true, true, false}, PresetHandsOff: {true, true, true}}
	if fmt.Sprint(eligible) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", eligible, want)
	}
}

func TestRunCodeIsSeparateAndScoped(t *testing.T) {
	g := activeGrant("/profile", PresetHandsOff, "hooks")
	if d := decide(t, g, nil, hookWrite("theme-set")).Decisions[0]; d.Reason != ReasonRunCodeRequired {
		t.Fatalf("code ran without run-code authorisation: %+v", d)
	}
	g.RunCode = map[string]RunCodeScope{"hooks": {Families: []string{"hooks.delete"}}}
	if d := decide(t, approved(g), nil, hookWrite("theme-set")).Decisions[0]; d.Reason != ReasonRunCodeRequired {
		t.Fatalf("family-scoped authorisation leaked: %+v", d)
	}
	g.RunCode = map[string]RunCodeScope{"hooks": {Families: []string{"hooks.write"}}}
	if d := decide(t, approved(g), nil, hookWrite("theme-set")).Decisions[0]; !d.Eligible {
		t.Fatalf("authorised family refused: %+v", d)
	}
}

// A user-controlled hook makes theme activation Hands-off with Themes and
// Hooks run-code; a hook permission alone never makes it Balanced.
func TestThemeActivationWithHookNeverBalanced(t *testing.T) {
	activate := OperationInput{Operation: model.Operation{ID: "themes.activate.nord", Provider: "themes", Resource: "theme:nord", Effect: effect("themes.activate", model.EffectUpdate, model.PayloadExecNow, model.SourceLocal)}, Category: "themes", Targets: []string{"theme:nord"}, RestoreApply: map[string]bool{"theme:nord": true}}
	balanced := activeGrant("/profile", PresetBalanced, "themes", "hooks")
	if d := decide(t, balanced, nil, activate).Decisions[0]; d.Eligible {
		t.Fatalf("Balanced ran a hook: %+v", d)
	}
	custom := activeGrant("/profile", PresetCustom)
	custom.Categories = map[string]CategoryScope{"themes": {AllTargets: true, MaxClass: ClassBalanced}, "hooks": {AllTargets: true, MaxClass: ClassHandsOff}}
	custom.RunCode = map[string]RunCodeScope{"hooks": {}}
	if d := decide(t, approved(custom), nil, activate).Decisions[0]; d.Eligible {
		t.Fatalf("hook permission downgraded activation: %+v", d)
	}
	handsOff := activeGrant("/profile", PresetHandsOff, "themes")
	handsOff.RunCode = map[string]RunCodeScope{"themes": {}}
	if d := decide(t, approved(handsOff), nil, activate).Decisions[0]; d.Reason != ReasonRunCodeRequired {
		t.Fatalf("activation ran a hook without Hooks authorisation: %+v", d)
	}
	handsOff.RunCode["hooks"] = RunCodeScope{}
	if d := decide(t, approved(handsOff), nil, activate).Decisions[0]; !d.Eligible {
		t.Fatalf("fully authorised activation refused: %+v", d)
	}
	// Positive proof that the path is non-executable admits Balanced.
	if d := decide(t, balanced, inertActivation{}, activate).Decisions[0]; !d.Eligible {
		t.Fatalf("proven inert activation refused: %+v", d)
	}
}

func TestPrivilegedInstallNeedsItsOwnAuthorisation(t *testing.T) {
	install := OperationInput{Operation: model.Operation{ID: "packages.install.official", Provider: "packages", Resource: "official:git", Effect: &model.Effect{Family: "packages.install-official", Change: model.EffectAdd, Payload: model.PayloadExecNow, Source: model.SourceSigned, Activation: model.ActivationNone, Privilege: model.PrivilegeRoot, Interaction: model.InteractionPossible, Recovery: model.RecoveryNone, Verifiable: true}}, Category: "packages", Targets: []string{"official:git"}, RestoreApply: map[string]bool{"official:git": true}}
	g := activeGrant("/profile", PresetHandsOff, "packages")
	g.RunCode = map[string]RunCodeScope{"packages": {}}
	g = approved(g)
	all := evidenceFor{CondSignedRepoHelper: true, CondNoninteractive: true}
	if d := decide(t, g, all, install).Decisions[0]; d.Reason != ReasonPrivilegedNeeded {
		t.Fatalf("root without privileged-install: %+v", d)
	}
	g.PrivilegedInstall = true
	if d := decide(t, approved(g), evidenceFor{CondSignedRepoHelper: true}, install).Decisions[0]; d.Reason != ReasonMayPrompt && d.Reason != ReasonConditionUnproven {
		t.Fatalf("possible prompt accepted: %+v", d)
	}
	if d := decide(t, approved(g), all, install).Decisions[0]; !d.Eligible {
		t.Fatalf("authorised install refused: %+v", d)
	}
}

func TestRestoreAndScopeRestrict(t *testing.T) {
	op := themeCopy("garden")
	g := activeGrant("/profile", PresetConservative, "themes")
	skip := op
	skip.RestoreApply = map[string]bool{"theme:garden": false}
	if d := decide(t, g, nil, skip).Decisions[0]; d.Reason != ReasonRestoreSkip {
		t.Fatalf("Restore Skip ignored: %+v", d)
	}
	g.Categories["themes"] = CategoryScope{AllTargets: true, DenyTargets: []string{"theme:garden"}}
	if d := decide(t, approved(g), nil, op).Decisions[0]; d.Reason != ReasonTargetDenied {
		t.Fatalf("deny ignored: %+v", d)
	}
	g.Categories["themes"] = CategoryScope{AllTargets: true, DenyFamilies: []string{"themes.copy-local"}}
	if d := decide(t, approved(g), nil, op).Decisions[0]; d.Reason != ReasonFamilyDenied {
		t.Fatalf("family deny ignored: %+v", d)
	}
	// Restore loosened beyond a confirmed target list.
	for link, want := range map[RestoreLink]Reason{LinkLinked: ReasonRestoreWidened, LinkCopyOnce: ReasonOutsideGrant, LinkSeparate: ReasonOutsideGrant} {
		g.RestoreLink = link
		g.Categories = ScopeFromRestore(map[string][]string{"themes": {"theme:nord"}})
		if d := decide(t, approved(g), nil, op).Decisions[0]; d.Reason != want {
			t.Fatalf("%s: %+v", link, d)
		}
	}
	unattributed := op
	unattributed.Targets = nil
	g = activeGrant("/profile", PresetConservative)
	g.Categories = ScopeFromRestore(map[string][]string{"themes": {"theme:garden"}})
	if d := decide(t, approved(g), nil, unattributed).Decisions[0]; d.Reason != ReasonUnattributed {
		t.Fatalf("category step allowed without category-wide scope: %+v", d)
	}
	g = activeGrant("/profile", PresetConservative, "defaults")
	if d := decide(t, g, nil, op).Decisions[0]; d.Reason != ReasonOutsideGrant {
		t.Fatalf("uncovered category: %+v", d)
	}
}

func TestDependenciesCannotHideSteps(t *testing.T) {
	g := activeGrant("/profile", PresetConservative, "themes", "hooks")
	child := themeCopy("garden")
	risky := hookWrite("theme-set")
	child.Operation.DependsOn = []string{risky.Operation.ID}
	result := decide(t, g, nil, risky, child)
	if result.Eligible || result.Decisions[1].Reason != ReasonDependencyBlocked {
		t.Fatalf("eligible step depending on a review step: %+v", result.Decisions)
	}
	missing := themeCopy("garden")
	missing.Operation.DependsOn = []string{"absent"}
	if d := decide(t, g, nil, missing).Decisions[0]; d.Reason != ReasonDependencyMissing {
		t.Fatalf("missing dependency: %+v", d)
	}
	a, b := themeCopy("a"), themeCopy("b")
	a.Operation.DependsOn, b.Operation.DependsOn = []string{b.Operation.ID}, []string{a.Operation.ID}
	for _, d := range decide(t, g, nil, a, b).Decisions {
		if d.Eligible {
			t.Fatalf("cycle eligible: %+v", d)
		}
	}
	if decide(t, g, nil).Eligible {
		t.Fatal("empty plan reported eligible")
	}
}

func TestEvidenceCannotChangeFamily(t *testing.T) {
	g := activeGrant("/profile", PresetConservative, "themes")
	if d := decide(t, g, familySwap{}, themeCopy("garden")).Decisions[0]; d.Eligible || d.Reason != ReasonUndescribed {
		t.Fatalf("evidence rewrote family: %+v", d)
	}
}

type familySwap struct{}

func (familySwap) Refine(_ model.Operation, e model.Effect) (model.Effect, map[Condition]bool) {
	e.Family = "themes.other"
	return e, nil
}

// Monotonicity: tightening any layer never turns review into eligible.
func TestTighteningNeverWidens(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	ops := []OperationInput{themeCopy("a"), themeCopy("b"), defaultSet("browser"), hookWrite("h")}
	presets := []Preset{PresetConservative, PresetBalanced, PresetHandsOff, PresetCustom}
	categories := []string{"themes", "defaults", "hooks"}
	for i := 0; i < 2000; i++ {
		g := activeGrant("/profile", presets[rng.Intn(len(presets))])
		g.RestoreLink = []RestoreLink{LinkLinked, LinkCopyOnce, LinkSeparate}[rng.Intn(3)]
		g.Categories = map[string]CategoryScope{}
		for _, c := range categories {
			if rng.Intn(4) == 0 {
				continue
			}
			scope := CategoryScope{AllTargets: rng.Intn(2) == 0}
			if !scope.AllTargets {
				scope.Targets = []string{"theme:a", "default:browser", "hook:h"}[:rng.Intn(4)]
			}
			if g.Preset == PresetCustom {
				scope.MaxClass = Class(rng.Intn(4))
			}
			g.Categories[c] = scope
		}
		if g.Preset == PresetHandsOff || g.Preset == PresetCustom {
			if rng.Intn(2) == 0 {
				g.RunCode = map[string]RunCodeScope{"hooks": {}}
			}
		}
		g = approved(g)
		ev := evidenceFor{CondInstalledApp: rng.Intn(2) == 0}
		before := decide(t, g, ev, ops...)
		tightened := tighten(rng, g)
		after := decide(t, approved(tightened), ev, ops...)
		for n := range ops {
			if after.Decisions[n].Eligible && !before.Decisions[n].Eligible {
				t.Fatalf("iteration %d: tightening widened %s: before %+v after %+v (grant %+v → %+v)", i, ops[n].Operation.ID, before.Decisions[n], after.Decisions[n], g, tightened)
			}
		}
		// Restore Skip on any target never widens either.
		skipped := append([]OperationInput(nil), ops...)
		k := rng.Intn(len(skipped))
		skipped[k].RestoreApply = map[string]bool{}
		if decide(t, g, ev, skipped...).Decisions[k].Eligible {
			t.Fatalf("Restore Skip did not veto %s", skipped[k].Operation.ID)
		}
	}
}

func tighten(rng *rand.Rand, g Grant) Grant {
	t := g
	t.Categories = map[string]CategoryScope{}
	for k, v := range g.Categories {
		t.Categories[k] = v
	}
	switch rng.Intn(5) {
	case 0:
		switch g.Preset {
		case PresetHandsOff:
			t.Preset = PresetBalanced
		case PresetBalanced:
			t.Preset = PresetConservative
		}
		if t.Preset != PresetHandsOff && t.Preset != PresetCustom {
			t.RunCode = nil
		}
	case 1:
		for k, v := range t.Categories {
			v.DenyTargets = append(append([]string(nil), v.DenyTargets...), "theme:a", "hook:h")
			t.Categories[k] = v
		}
	case 2:
		t.RunCode = nil
	case 3:
		for k := range t.Categories {
			delete(t.Categories, k)
			break
		}
	case 4:
		for k, v := range t.Categories {
			v.DenyFamilies = append(append([]string(nil), v.DenyFamilies...), "defaults.set")
			t.Categories[k] = v
		}
	}
	return t
}

func TestAvailabilityIsStrictlyBroaderPerPreset(t *testing.T) {
	registry := DefaultRegistry()
	c, b, h := PresetAvailability(registry, PresetConservative), PresetAvailability(registry, PresetBalanced), PresetAvailability(registry, PresetHandsOff)
	count := func(a Availability) int { return len(a.AvailableNow) + len(a.NotAvailableYet) }
	if !(count(c) < count(b) && count(b) < count(h)) {
		t.Fatalf("presets not strictly broader: %d %d %d", count(c), count(b), count(h))
	}
	for _, a := range []Availability{c, b, h} {
		if len(a.AvailableNow) != 0 {
			t.Fatalf("%s offers delivered operations in PR A: %v", a.Preset, a.AvailableNow)
		}
	}
}
