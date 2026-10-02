package workflow

import (
	"slices"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

func clonePointer[T any](source *T) *T {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}
func cloneRestoreOptions(o policy.RestoreOptions) policy.RestoreOptions {
	o.ReviewActivation = clonePointer(o.ReviewActivation)
	if o.ReviewActivation != nil {
		o.ReviewActivation.Units = slices.Clone(o.ReviewActivation.Units)
	}
	return o
}
func cloneTargets(source []TargetInspection) []TargetInspection {
	out := slices.Clone(source)
	for i := range out {
		out[i].Ancestors = slices.Clone(out[i].Ancestors)
		out[i].RecommendedDependencies = slices.Clone(out[i].RecommendedDependencies)
	}
	return out
}

func cloneReadPlan(p model.RestorePlan) model.RestorePlan {
	p.ActivationReview = slices.Clone(p.ActivationReview)
	p.Skipped = slices.Clone(p.Skipped)
	p.Requirements = slices.Clone(p.Requirements)
	for i := range p.Requirements {
		p.Requirements[i].Remediation = slices.Clone(p.Requirements[i].Remediation)
		p.Requirements[i].Operations = slices.Clone(p.Requirements[i].Operations)
	}
	p.Operations = slices.Clone(p.Operations)
	for i := range p.Operations {
		op := &p.Operations[i]
		op.Items = slices.Clone(op.Items)
		op.Command = slices.Clone(op.Command)
		op.DependsOn = slices.Clone(op.DependsOn)
		op.Copy = clonePointer(op.Copy)
		op.Directory = clonePointer(op.Directory)
		op.GitPatch = clonePointer(op.GitPatch)
		op.Symlink = clonePointer(op.Symlink)
		if op.Symlink != nil {
			op.Symlink.ExpectedExisting = clonePointer(op.Symlink.ExpectedExisting)
			op.Symlink.ExpectedTarget = clonePointer(op.Symlink.ExpectedTarget)
		}
		op.Delete = clonePointer(op.Delete)
		if op.Delete != nil {
			op.Delete.ExpectedExisting = clonePointer(op.Delete.ExpectedExisting)
		}
		op.File = clonePointer(op.File)
		if op.File != nil {
			op.File.Content = slices.Clone(op.File.Content)
			op.File.Mode = clonePointer(op.File.Mode)
			op.File.ExpectedMode = clonePointer(op.File.ExpectedMode)
			op.File.ExpectedExisting = clonePointer(op.File.ExpectedExisting)
		}
	}
	p.Compatibility.Categories = slices.Clone(p.Compatibility.Categories)
	for i := range p.Compatibility.Categories {
		category := &p.Compatibility.Categories[i]
		category.Evidence = slices.Clone(category.Evidence)
		category.Findings = slices.Clone(category.Findings)
		for j := range category.Findings {
			category.Findings[j].Evidence = slices.Clone(category.Findings[j].Evidence)
		}
	}
	return p
}
func cloneReadFragment(f RestoreFragment) RestoreFragment {
	p := cloneReadPlan(model.RestorePlan{ActivationReview: f.ActivationReview, Operations: f.Operations, Skipped: f.Skipped, Requirements: f.Requirements, Compatibility: model.CompatibilityReport{Categories: []model.CompatibilityCategory{f.Compatibility}}})
	return RestoreFragment{ActivationReview: p.ActivationReview, Operations: p.Operations, Skipped: p.Skipped, Requirements: p.Requirements, Compatibility: p.Compatibility.Categories[0]}
}
