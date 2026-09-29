package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

var ErrNotImplemented = errors.New("services persistent Restore and compatibility are not implemented yet")

// Provider keeps Capture behind reviewed ownership. Persistent Restore and
// compatibility remain fail-closed until their separate PR C boundary.
type Provider struct {
	Systemd    Systemd
	Roots      Roots
	ProfileDir string
	prepared   *preparedCapture
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
func (Provider) Plan(context.Context, profile.Data, omarchy.Info, workflow.RestoreContext) (workflow.RestoreFragment, error) {
	return workflow.RestoreFragment{}, ErrNotImplemented
}
func (Provider) Verify(context.Context, profile.Data, workflow.RestoreContext) (model.VerificationResult, error) {
	return model.VerificationResult{}, ErrNotImplemented
}
