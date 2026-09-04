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
