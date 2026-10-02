package services

import (
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Provider keeps Capture behind reviewed ownership and derives persistent
// Restore effects, compatibility, and Verify from the same selected intent.
type Provider struct {
	Systemd      Systemd
	Roots        Roots
	ResolveRoots func() (Roots, error)
	ProfileDir   string
	prepared     *preparedCapture
}

var _ workflow.RestoreProvider = (*Provider)(nil)

func (*Provider) ID() string                   { return "services" }
func (*Provider) CategoryEnabled() bool        { return true }
func (*Provider) Captured(d profile.Data) bool { return d.Manifest.Capture.Services }
func (*Provider) ValidateTarget(target string) (string, error) {
	if target == "user-manager" {
		return target, nil
	}
	if err := profile.ValidateServiceUnitName(target); err != nil {
		return "", fmt.Errorf("services target: %w", err)
	}
	return target, nil
}
func (*Provider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Provider == "services" && change.Kind == "user-service" && change.Name != "" {
		return change.Name, true
	}
	return "", false
}

func (*Provider) RestoreOperationTargetKeys(op model.Operation) ([]string, bool) {
	if op.Provider != "services" {
		return nil, false
	}
	if op.Action == "daemon-reload" && op.Resource == "" {
		return nil, true
	}
	if err := profile.ValidateServiceUnitName(op.Resource); err != nil {
		return nil, false
	}
	return []string{op.Resource}, true
}
