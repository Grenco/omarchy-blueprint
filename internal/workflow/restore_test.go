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

func TestRestorePlanTranslatesModeIntoCompatibilityRestoreContext(t *testing.T) {
	session := newCaptureSession(t, profile.New("test", time.Now()))
	var lastPlan RestoreContext
	session.SetProviders([]Provider{captureTestProvider{
		id: "packages", order: &[]string{},
		lastPlan: &lastPlan,
		targets:  []TargetInspection{{Key: "official:firefox"}},
	}})

	if _, err := session.PlanRestore(context.Background(), "", RestoreForced); err != nil {
		t.Fatal(err)
	}
	want := policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceAdditive}
	if lastPlan.Options != want {
		t.Fatalf("Plan received Options = %+v, want %+v", lastPlan.Options, want)
	}
	decision, ok := lastPlan.Lookup("official:firefox")
	if !ok || decision != DefaultRestoreDecision() {
		t.Fatalf("Plan received Targets[official:firefox] = %+v, %v, want the PR 2 compatibility default", decision, ok)
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

	if _, err := session.ApplyRestore(context.Background(), "", RestoreNormal); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lastPlan, lastVerify) {
		t.Fatalf("Plan context %+v != Verify context %+v; verification must judge the same effective Restore intent that planned it", lastPlan, lastVerify)
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
