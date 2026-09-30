package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/compatibility"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

var ErrNotImplemented = errors.New("services persistent Restore and compatibility are not implemented yet")

// Provider keeps Capture behind reviewed ownership. Persistent Restore and
// compatibility remain fail-closed until their separate PR C boundary.
type Provider struct {
	Systemd      Systemd
	Roots        Roots
	ResolveRoots func() (Roots, error)
	ProfileDir   string
	prepared     *preparedCapture
}

var _ workflow.RestoreProvider = (*Provider)(nil)

func (Provider) ID() string                   { return "services" }
func (Provider) CategoryEnabled() bool        { return true }
func (Provider) Captured(d profile.Data) bool { return d.Manifest.Capture.Services }
func (Provider) ValidateTarget(target string) (string, error) {
	if target == "user-manager" {
		return target, nil
	}
	if err := profile.ValidateServiceUnitName(target); err != nil {
		return "", fmt.Errorf("services target: %w", err)
	}
	return target, nil
}
func (Provider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Provider == "services" && change.Kind == "user-service" && change.Name != "" {
		return change.Name, true
	}
	return "", false
}
func (Provider) Plan(_ context.Context, data profile.Data, _ omarchy.Info, restore workflow.RestoreContext) (workflow.RestoreFragment, error) {
	fragment := workflow.RestoreFragment{}
	var findings []model.CompatibilityFinding
	for _, unit := range data.Services.Units {
		decision, err := restore.Require(unit.Name)
		if err != nil {
			return workflow.RestoreFragment{}, err
		}
		reason := decision.Reason
		if reason == "" {
			reason = "Services Restore is not available in this build; service left untouched"
		}
		fragment.Skipped = append(fragment.Skipped, model.Skipped{Provider: "services", Resource: unit.Name, Reason: reason})
		if decision.Restore || decision.CompatibilityApply {
			findings = append(findings, model.CompatibilityFinding{Code: "services.restore.unavailable", Target: unit.Name, State: model.CompatibilityUnknown, Authority: model.CompatibilityReduced, Summary: "Services Restore is not available in this build; no service mutations planned"})
		}
	}
	category, err := compatibility.BuildCategory("services", len(findings) > 0, nil, findings)
	fragment.Compatibility = category
	return fragment, err
}
func (Provider) Verify(context.Context, profile.Data, workflow.RestoreContext) (model.VerificationResult, error) {
	// PR B promises no Services effects. PR C replaces both this no-op plan
	// and verification with persistent reconstruction of the selected intent.
	return model.VerificationResult{OK: true}, nil
}
