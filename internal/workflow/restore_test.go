package workflow

import (
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

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
