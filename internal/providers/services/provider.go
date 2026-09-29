package services

import (
	"context"
	"errors"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

var ErrNotImplemented = errors.New("services discovery, capture and restore are not implemented yet")

// Provider is intentionally unregistered and disabled in PR A. Later tasks
// install deliberate discovery, reviewed ownership, planning and compatibility
// before the application exposes Services as a category.
type Provider struct {
	Systemd    Systemd
	Roots      Roots
	ProfileDir string
}

var _ workflow.RestoreProvider = Provider{}

func (Provider) ID() string                   { return "services" }
func (Provider) CategoryEnabled() bool        { return false }
func (Provider) Captured(d profile.Data) bool { return d.Manifest.Capture.Services }
func (Provider) Capture(context.Context, *profile.Data, workflow.CaptureContext) (any, []model.Change, error) {
	return nil, nil, ErrNotImplemented
}
func (Provider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return nil, ErrNotImplemented
}
func (Provider) Plan(context.Context, profile.Data, omarchy.Info, workflow.RestoreContext) (workflow.RestoreFragment, error) {
	return workflow.RestoreFragment{}, ErrNotImplemented
}
func (Provider) Verify(context.Context, profile.Data, workflow.RestoreContext) (model.VerificationResult, error) {
	return model.VerificationResult{}, ErrNotImplemented
}
func (Provider) Check(context.Context, profile.Data) error { return ErrNotImplemented }
