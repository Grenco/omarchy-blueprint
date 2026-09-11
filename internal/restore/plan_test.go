package restore

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

func TestValidatePlanAcceptsBackwardDependencies(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "plugins.install.acme", Provider: "plugins", Command: []string{"true"}},
		{ID: "shell.write", Provider: "shell", File: &model.FileWrite{}, DependsOn: []string{"plugins.install.acme"}},
	}}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
}

func TestValidatePlanRejectsBlankAndDuplicateOperationIDs(t *testing.T) {
	blank := model.RestorePlan{Operations: []model.Operation{
		{ID: "", Command: []string{"true"}},
	}}
	if err := ValidatePlan(blank); err == nil || !strings.Contains(err.Error(), "empty id") {
		t.Fatalf("err = %v", err)
	}
	duplicate := model.RestorePlan{Operations: []model.Operation{
		{ID: "op.one", Command: []string{"true"}},
		{ID: "op.one", Command: []string{"true"}},
	}}
	if err := ValidatePlan(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidatePlanRejectsUnknownDependency(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "shell.write", DependsOn: []string{"plugins.install.acme"}},
	}}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "unknown or later") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidatePlanRejectsForwardDependency(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "shell.write", DependsOn: []string{"shell.restart"}},
		{ID: "shell.restart"},
	}}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "unknown or later") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidatePlanRejectsBlankDependency(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{
		{ID: "shell.write", DependsOn: []string{" "}},
	}}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "empty dependency") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidatePlanAcceptsFileDeleteAsOnlyAction(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID:     "config.delete",
		Delete: &model.FileDelete{Destination: "/tmp/config", ExpectedExisting: &model.FilesystemPrecondition{Type: "file"}},
	}}}
	if err := ValidatePlan(plan); err != nil {
		t.Fatalf("valid delete rejected: %v", err)
	}
}

func TestValidatePlanRejectsMultipleActionsIncludingFileDelete(t *testing.T) {
	plan := model.RestorePlan{Operations: []model.Operation{{
		ID:     "config.delete",
		File:   &model.FileWrite{},
		Delete: &model.FileDelete{Destination: "/tmp/config", ExpectedExisting: &model.FilesystemPrecondition{Type: "file"}},
	}}}
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("err=%v", err)
	}
}

func TestValidatePlanValidatesGitPatchActions(t *testing.T) {
	validHash := strings.Repeat("a", 64)
	valid := model.RestorePlan{Operations: []model.Operation{{
		ID:       "resources.git.apply-index.dotfiles",
		GitPatch: &model.GitPatchApply{Repository: "/tmp/dotfiles", Source: "/tmp/index.patch", SourceHash: validHash, ToIndex: true},
	}}}
	if err := ValidatePlan(valid); err != nil {
		t.Fatalf("valid git patch rejected: %v", err)
	}

	for _, operation := range []model.Operation{
		{ID: "multiple", Command: []string{"true"}, GitPatch: &model.GitPatchApply{Repository: "/tmp/repo", Source: "/tmp/patch", SourceHash: validHash}},
		{ID: "repository", GitPatch: &model.GitPatchApply{Source: "/tmp/patch", SourceHash: validHash}},
		{ID: "source", GitPatch: &model.GitPatchApply{Repository: "/tmp/repo", SourceHash: validHash}},
		{ID: "empty-hash", GitPatch: &model.GitPatchApply{Repository: "/tmp/repo", Source: "/tmp/patch"}},
		{ID: "invalid-hash", GitPatch: &model.GitPatchApply{Repository: "/tmp/repo", Source: "/tmp/patch", SourceHash: "not-a-hash"}},
	} {
		if err := ValidatePlan(model.RestorePlan{Operations: []model.Operation{operation}}); err == nil {
			t.Fatalf("invalid git patch accepted: %#v", operation)
		}
	}
}

func TestValidatePlanRejectsInvalidFileDelete(t *testing.T) {
	cases := []model.FileDelete{
		{},
		{Destination: "/tmp/config", ExpectedMissing: true, ExpectedExisting: &model.FilesystemPrecondition{Type: "file"}},
		{Destination: "/tmp/config"},
		{Destination: "/tmp/config", ExpectedExisting: &model.FilesystemPrecondition{Type: "other"}},
		{Destination: "/tmp/config", ExpectedMissing: true, Backup: true},
	}
	for _, action := range cases {
		plan := model.RestorePlan{Operations: []model.Operation{{ID: "config.delete", Delete: &action}}}
		if err := ValidatePlan(plan); err == nil {
			t.Fatalf("invalid delete accepted: %#v", action)
		}
	}
}
