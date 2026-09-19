package workflow

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// TestRestorePlanResolvesNilOptionsToSelectedMachineDefaults is a Task 24
// regression: nil options means the currently selected machine's persisted
// Restore defaults, not a hardcoded mode.
func TestRestorePlanResolvesNilOptionsToSelectedMachineDefaults(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop", RestoreConflicts: policy.ConflictForce, RestoreConvergence: policy.ConvergenceExact}}
	session := newExplicitMachineCaptureSession(t, data, "desktop")
	var lastPlan RestoreContext
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		lastPlan: &lastPlan,
		targets:  []TargetInspection{{Key: "official:firefox"}},
	}})

	if _, err := session.PlanRestore(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	want := policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact}
	if lastPlan.Options != want {
		t.Fatalf("Plan received Options = %+v, want the machine's persisted defaults %+v", lastPlan.Options, want)
	}
}

// TestRestorePlanResolvesNilOptionsToSafeAdditiveWhenNoMachineSelected is
// the paired case: no selected machine means the built-in Safe+Additive
// default, not an error.
func TestRestorePlanResolvesNilOptionsToSafeAdditiveWhenNoMachineSelected(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var lastPlan RestoreContext
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		lastPlan: &lastPlan,
		targets:  []TargetInspection{{Key: "official:firefox"}},
	}})

	if _, err := session.PlanRestore(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if lastPlan.Options != policy.DefaultRestoreOptions() {
		t.Fatalf("Plan received Options = %+v, want the built-in Safe+Additive default", lastPlan.Options)
	}
}

// TestRestorePlanExplicitOptionsOverrideMachineDefaultsWithoutPersisting is
// a Task 28 gate scenario (one-run override does not persist): an explicit
// options argument is used verbatim for this run, regardless of the
// selected machine's persisted defaults, and never writes to the profile.
func TestRestorePlanExplicitOptionsOverrideMachineDefaultsWithoutPersisting(t *testing.T) {
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop", RestoreConflicts: policy.ConflictForce, RestoreConvergence: policy.ConvergenceExact}}
	session := newExplicitMachineCaptureSession(t, data, "desktop")
	var lastPlan RestoreContext
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		lastPlan: &lastPlan,
		targets:  []TargetInspection{{Key: "official:firefox"}},
	}})

	override := policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceAdditive}
	if _, err := session.PlanRestore(context.Background(), "", &override); err != nil {
		t.Fatal(err)
	}
	if lastPlan.Options != override {
		t.Fatalf("Plan received Options = %+v, want the explicit one-run override %+v", lastPlan.Options, override)
	}
	loaded, err := profile.Load(session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Machines.Items[0].RestoreConflicts != policy.ConflictForce || loaded.Machines.Items[0].RestoreConvergence != policy.ConvergenceExact {
		t.Fatalf("machine defaults on disk = %+v, want left untouched by the one-run override", loaded.Machines.Items[0])
	}
}

// TestRestorePlanRejectsInvalidExplicitOptions confirms an explicit override
// is validated rather than passed through blindly.
func TestRestorePlanRejectsInvalidExplicitOptions(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})

	invalid := policy.RestoreOptions{Conflicts: "sometimes", Convergence: policy.ConvergenceAdditive}
	if _, err := session.PlanRestore(context.Background(), "", &invalid); err == nil {
		t.Fatal("invalid explicit restore options accepted")
	}
}

func TestRestorePlanAndVerifyReceiveIdenticalRestoreContext(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var lastPlan, lastVerify RestoreContext
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		lastPlan: &lastPlan, lastVerify: &lastVerify,
		targets: []TargetInspection{{Key: "official:firefox"}},
	}})

	if _, err := session.ApplyRestore(context.Background(), "", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lastPlan, lastVerify) {
		t.Fatalf("Plan context %+v != Verify context %+v; verification must judge the same effective Restore intent that planned it", lastPlan, lastVerify)
	}
}

// TestApplyRestoreResultStoresResolvedOptions is a Task 24 regression:
// RestoreResult records the resolved RestoreOptions the run actually used,
// not the caller's possibly-nil argument, so callers can tell what
// "defaults" resolved to without re-deriving it themselves.
func TestApplyRestoreResultStoresResolvedOptions(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	session.SetProviders([]Provider{captureTestProvider{id: "packages", order: &[]string{}}})

	result, err := session.ApplyRestore(context.Background(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Options != policy.DefaultRestoreOptions() {
		t.Fatalf("result.Options = %+v, want the resolved Safe+Additive default", result.Options)
	}
}

func TestRestoreConsequencesUseTypedOperations(t *testing.T) {
	normal := model.RestorePlan{Skipped: []model.Skipped{{Provider: "config", Resource: "file"}, {Provider: "config", Resource: "delete"}, {Provider: "other", Resource: "unknown"}}}
	forced := model.RestorePlan{Operations: []model.Operation{
		{ID: "write", Provider: "config", Resource: "file", Risk: model.RiskHigh, File: &model.FileWrite{ReplaceExisting: true}},
		{ID: "delete", Provider: "config", Resource: "delete", Risk: model.RiskMedium, Delete: &model.FileDelete{}},
		{ID: "same", Provider: "resources", Resource: "repo", Risk: model.RiskLow, Command: []string{"git", "clone"}},
		{ID: "link", Provider: "config", Resource: "link", Risk: model.RiskLow, Symlink: &model.SymlinkWrite{ReplaceExisting: true}},
	}}
	normal.Operations = append(normal.Operations, model.Operation{ID: "same", Provider: "resources", Resource: "repo", Risk: model.RiskLow, Command: []string{"git", "clone"}})
	items := restoreConsequences(normal, forced)
	want := map[string]struct {
		normal, forced Outcome
		risk           model.Risk
	}{
		"config\x00file":    {OutcomeSkip, OutcomeReplace, model.RiskHigh},
		"config\x00delete":  {OutcomeSkip, OutcomeDelete, model.RiskMedium},
		"resources\x00repo": {OutcomeModify, OutcomeModify, model.RiskLow},
		"config\x00link":    {OutcomeNoop, OutcomeReplace, model.RiskLow},
		"other\x00unknown":  {OutcomeSkip, OutcomeNoop, ""},
	}
	for _, item := range items {
		key := item.Provider + "\x00" + item.Resource
		expected, ok := want[key]
		if !ok {
			t.Fatalf("unexpected consequence %q", key)
		}
		if item.Normal != expected.normal || item.Forced != expected.forced || item.Risk != expected.risk {
			t.Errorf("%s = %#v, want normal=%s forced=%s risk=%s", key, item, expected.normal, expected.forced, expected.risk)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing consequences: %#v", want)
	}
}
