package syncengine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func statesForTest() []State {
	return []State{{Kind: StateUnmanaged}, {Kind: StatePresent, Fingerprint: "a"}, {Kind: StatePresent, Fingerprint: "b"}, {Kind: StateAbsent, Fingerprint: "a"}, {Kind: StateAbsent, Fingerprint: "b"}}
}

// Literal matrices catch collapsing unmanaged/absence, kind omission, fingerprint omission, and wrong history precedence.
func TestHistoryExhaustiveSemanticTruthTable(t *testing.T) {
	const U = HistoryUnchanged
	const L = HistoryLocal
	const R = HistoryRemote
	const C = HistoryConverged
	const F = HistoryConflict
	wants := [][][]HistoryClass{
		{{U, R, R, R, R}, {L, C, F, F, F}, {L, F, C, F, F}, {L, F, F, C, F}, {L, F, F, F, C}},
		{{C, L, F, F, F}, {R, U, R, R, R}, {F, L, C, F, F}, {F, L, F, C, F}, {F, L, F, F, C}},
		{{C, F, L, F, F}, {F, C, L, F, F}, {R, R, U, R, R}, {F, F, L, C, F}, {F, F, L, F, C}},
		{{C, F, F, L, F}, {F, C, F, L, F}, {F, F, C, L, F}, {R, R, R, U, R}, {F, F, F, L, C}},
		{{C, F, F, F, L}, {F, C, F, F, L}, {F, F, C, F, L}, {F, F, F, C, L}, {R, R, R, R, U}},
	}
	states := statesForTest()
	for b, base := range states {
		for l, local := range states {
			for r, remote := range states {
				got, err := ClassifyHistory(base, local, remote)
				if err != nil || got != wants[b][l][r] {
					t.Fatalf("B/L/R=%d/%d/%d got=%s err=%v want=%s", b, l, r, got, err, wants[b][l][r])
				}
				local.Summary = "different presentation"
				again, err := ClassifyHistory(base, local, remote)
				if err != nil || again != got {
					t.Fatal("summary changed intent", err)
				}
			}
		}
	}
}

func validPlanRequest(targets ...TargetInput) PlanRequest {
	return PlanRequest{Identity: Identity{Base: strings.Repeat("a", 40), Local: strings.Repeat("b", 40), Remote: strings.Repeat("c", 40), Machine: "laptop", Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("d", 64), ControlFingerprint: strings.Repeat("e", 64)}, Targets: targets}
}

func targetForTest(b, l, r, m int, capture, restore bool) TargetInput {
	s := statesForTest()
	return TargetInput{Key: TargetKey{Category: "config", Target: "test"}, Base: s[b], Local: s[l], Remote: s[r], Machine: s[m], Policy: EffectivePolicy{Capture: capture, Restore: restore}}
}

func TestMachineClassificationAndPolicyActions(t *testing.T) {
	cases := []struct {
		name                  string
		b, l, r, m            int
		capture, restore      bool
		history               HistoryClass
		actions               []PlannedAction
		review                ReviewKind
		settled, inapplicable bool
	}{
		{"unchanged converged", 1, 1, 1, 1, true, true, HistoryUnchanged, nil, ReviewNone, true, false},
		{"drift capture", 1, 1, 1, 2, true, false, HistoryUnchanged, []PlannedAction{ActionCapture}, ReviewNone, false, false},
		{"drift alternatives", 1, 1, 1, 2, true, true, HistoryUnchanged, nil, ReviewMachine, false, false},
		{"drift capture disabled", 1, 1, 1, 2, false, true, HistoryUnchanged, []PlannedAction{ActionRestore}, ReviewNone, false, false},
		{"drift both disabled", 1, 1, 1, 2, false, false, HistoryUnchanged, nil, ReviewNone, true, true},
		{"remote follow", 1, 1, 2, 1, true, true, HistoryRemote, []PlannedAction{ActionRestore}, ReviewNone, false, false},
		{"remote inapplicable", 1, 1, 2, 1, true, false, HistoryRemote, nil, ReviewNone, true, true},
		{"remote already followed", 1, 1, 2, 2, true, true, HistoryRemote, nil, ReviewNone, true, false},
		{"remote live drift", 1, 1, 2, 0, true, true, HistoryRemote, nil, ReviewMachine, false, false},
		{"local publish", 1, 2, 1, 2, true, true, HistoryLocal, []PlannedAction{ActionPublish}, ReviewNone, false, false},
		{"local publish and follow", 1, 2, 1, 1, true, true, HistoryLocal, []PlannedAction{ActionPublish, ActionRestore}, ReviewNone, false, false},
		{"local publish inapplicable", 1, 2, 1, 1, true, false, HistoryLocal, []PlannedAction{ActionPublish}, ReviewNone, false, true},
		{"local vs machine", 1, 2, 1, 0, true, true, HistoryLocal, nil, ReviewMachine, false, false},
		{"identical results", 1, 2, 2, 2, true, true, HistoryConverged, nil, ReviewNone, true, false},
		{"identical results need follow", 1, 2, 2, 1, true, true, HistoryConverged, []PlannedAction{ActionRestore}, ReviewNone, false, false},
		{"conflict", 0, 1, 2, 1, true, true, HistoryConflict, nil, ReviewHistory, false, false},
		{"disabled conflict remains", 0, 1, 2, 1, false, false, HistoryConflict, nil, ReviewHistory, false, false},
		{"unmanaged live capture", 0, 0, 0, 1, true, true, HistoryUnchanged, []PlannedAction{ActionCapture}, ReviewNone, false, false},
		{"unmanaged cannot restore", 0, 0, 0, 1, false, true, HistoryUnchanged, nil, ReviewNone, true, true},
		{"unmanage remote", 1, 1, 0, 1, true, true, HistoryRemote, nil, ReviewNone, true, true},
		{"explicit absent follow", 1, 1, 3, 1, true, true, HistoryRemote, []PlannedAction{ActionRestore}, ReviewNone, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := Reconcile(validPlanRequest(targetForTest(tc.b, tc.l, tc.r, tc.m, tc.capture, tc.restore)))
			if err != nil {
				t.Fatal(err)
			}
			item := plan.Items[0]
			if item.History != tc.history || !reflect.DeepEqual(item.Actions, tc.actions) || item.Review != tc.review || item.Settled != tc.settled || item.Inapplicable != tc.inapplicable {
				t.Fatalf("got %+v", item)
			}
		})
	}
}

func TestReconcileResolutionsSelectIntentNotAuthority(t *testing.T) {
	for _, tc := range []struct {
		name       string
		target     TargetInput
		resolution Resolution
		machine    MachineResolution
		actions    []PlannedAction
		desired    int
		deferred   bool
	}{
		{"keep local", targetForTest(0, 1, 2, 1, true, true), ResolutionKeepLocal, MachineResolutionNone, []PlannedAction{ActionPublish}, 1, false},
		{"accept remote", targetForTest(0, 1, 2, 1, true, true), ResolutionAcceptRemote, MachineResolutionNone, []PlannedAction{ActionRestore}, 2, false},
		{"defer", targetForTest(0, 1, 2, 1, true, true), ResolutionDefer, MachineResolutionNone, nil, 1, true},
		{"keep machine", targetForTest(1, 2, 1, 0, true, true), ResolutionNone, MachineResolutionKeepMachine, []PlannedAction{ActionCapture, ActionPublish}, 0, false},
		{"keep profile", targetForTest(1, 2, 1, 0, true, true), ResolutionNone, MachineResolutionKeepProfile, []PlannedAction{ActionPublish, ActionRestore}, 2, false},
		{"defer machine", targetForTest(1, 2, 1, 0, true, true), ResolutionNone, MachineResolutionDefer, nil, 2, true},
		{"history plus third machine intent", targetForTest(3, 1, 2, 0, true, true), ResolutionKeepLocal, MachineResolutionKeepMachine, []PlannedAction{ActionCapture, ActionPublish}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := tc.target
			input.Resolution = tc.resolution
			input.MachineResolution = tc.machine
			plan, err := Reconcile(validPlanRequest(input))
			if err != nil {
				t.Fatal(err)
			}
			item := plan.Items[0]
			if !reflect.DeepEqual(item.Actions, tc.actions) || item.Deferred != tc.deferred || item.Desired != statesForTest()[tc.desired] || item.Settled {
				t.Fatal(item)
			}
		})
	}
	blocked := targetForTest(1, 2, 1, 0, false, true)
	blocked.MachineResolution = MachineResolutionKeepMachine
	if _, err := Reconcile(validPlanRequest(blocked)); err == nil {
		t.Fatal("policy-disabled capture resolution accepted")
	}
}

func TestPlanValidationDeterminismAndInputImmutability(t *testing.T) {
	a := targetForTest(1, 1, 1, 1, true, true)
	a.Key = TargetKey{Category: "services", Target: "a"}
	b := a
	b.Key = TargetKey{Category: "packages", Target: "z"}
	c := a
	c.Key = TargetKey{Category: "packages", Target: "a"}
	req := validPlanRequest(a, b, c)
	before, _ := json.Marshal(req)
	first, err := Reconcile(req)
	if err != nil {
		t.Fatal(err)
	}
	req2 := validPlanRequest(c, a, b)
	second, err := Reconcile(req2)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("nondeterministic plan", err)
	}
	want := []TargetKey{{"packages", "a"}, {"packages", "z"}, {"services", "a"}}
	for i, item := range first.Items {
		if item.Key != want[i] {
			t.Fatal(item.Key)
		}
	}
	after, _ := json.Marshal(req)
	if string(before) != string(after) {
		t.Fatal("input mutated")
	}
	for _, change := range []func(*PlanRequest){
		func(p *PlanRequest) { p.Targets = append(p.Targets, p.Targets[0]) },
		func(p *PlanRequest) { p.Identity.Base = "" }, func(p *PlanRequest) { p.Identity.Local = "" }, func(p *PlanRequest) { p.Identity.Remote = "" },
		func(p *PlanRequest) { p.Identity.Machine = "" }, func(p *PlanRequest) { p.Identity.ControlFingerprint = "" },
		func(p *PlanRequest) { p.Targets[0].Base = State{Kind: "invalid"} },
		func(p *PlanRequest) { p.Targets[0].Machine = State{Kind: StatePresent} },
		func(p *PlanRequest) { p.Targets[0].Base = State{Kind: StateUnmanaged, Fingerprint: "not unmanaged"} },
		func(p *PlanRequest) { p.Targets[0].Resolution = ResolutionKeepLocal },
		func(p *PlanRequest) { p.Targets[0].MachineResolution = MachineResolutionKeepProfile },
		func(p *PlanRequest) { p.Targets[0].Key.Category = "" },
	} {
		bad := validPlanRequest(a)
		change(&bad)
		if _, err := Reconcile(bad); err == nil {
			t.Fatal("invalid plan accepted", bad)
		}
	}
}

func TestReconcileHistoryEvidenceSurvivesEveryMachinePolicyCombination(t *testing.T) {
	states := statesForTest()
	for _, cap := range []bool{false, true} {
		for _, restore := range []bool{false, true} {
			for m := range states {
				conflict := targetForTest(0, 1, 2, m, cap, restore)
				plan, err := Reconcile(validPlanRequest(conflict))
				if err != nil || plan.Items[0].History != HistoryConflict || plan.Items[0].Settled || plan.Items[0].Inapplicable {
					t.Fatal("policy erased conflict", err)
				}
			}
		}
	}
}

// The combined matrix exercises 2,500 B/L/R/M/policy combinations. Independent
// invariants catch forbidden action selection and falsely settled conflicts.
func TestReconcileFullStateAndPolicyMatrix(t *testing.T) {
	states := statesForTest()
	for b := range states {
		for l := range states {
			for r := range states {
				for m := range states {
					for _, capture := range []bool{false, true} {
						for _, restore := range []bool{false, true} {
							input := targetForTest(b, l, r, m, capture, restore)
							plan, err := Reconcile(validPlanRequest(input))
							if err != nil {
								t.Fatal("valid normalized facts rejected", err)
							}
							item := plan.Items[0]
							for _, action := range item.Actions {
								if action == ActionCapture && !capture {
									t.Fatal("forbidden Capture", item)
								}
								if action == ActionRestore && (!restore || item.Desired.Kind == StateUnmanaged) {
									t.Fatal("forbidden Restore", item)
								}
							}
							if item.Settled && (item.Review != ReviewNone || item.Deferred || len(item.Actions) > 0) {
								t.Fatal("unsettled action/review marked settled", item)
							}
							if item.Settled && !item.Inapplicable && !item.Machine.Equal(item.Desired) {
								t.Fatal("drift marked converged", item)
							}
							if item.History == HistoryConflict && (item.Settled || item.Inapplicable || item.Review != ReviewHistory) {
								t.Fatal("conflict erased", item)
							}
						}
					}
				}
			}
		}
	}
}
