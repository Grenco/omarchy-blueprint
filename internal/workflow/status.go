package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

type ProviderStatus struct {
	ID           string
	Captured     bool
	Changes      []model.Change
	ConfigScan   *configprovider.ScanSummary
	ResourceGit  map[string]resourcesprovider.GitWorkingSummary
	Verification *model.VerificationResult
}

type StatusReport struct {
	Profile   profile.Data
	Machine   machine.Selection
	Providers []ProviderStatus
}

type scanProvider interface {
	DiffWithScan(context.Context, profile.Data) ([]model.Change, configprovider.ScanSummary, error)
}
type resourceProvider interface {
	DiffWithGitWorkingState(context.Context, profile.Data) ([]model.Change, map[string]resourcesprovider.GitWorkingSummary, error)
}

func (s *Session) Status(ctx context.Context, onlyProvider string) (StatusReport, error) {
	selected := capturedProviders(s.providers, s.profile)
	if onlyProvider != "" {
		provider, ok := ProviderByID(s.providers, onlyProvider)
		if !ok {
			return StatusReport{}, fmt.Errorf("unknown category %s", onlyProvider)
		}
		if !provider.Captured(s.profile) {
			return StatusReport{}, CaptureRequiredError(provider.ID())
		}
		selected = []Provider{provider}
	}
	report := StatusReport{Profile: s.profile, Machine: s.machine, Providers: make([]ProviderStatus, 0, len(selected))}
	for _, provider := range selected {
		status := ProviderStatus{ID: provider.ID(), Captured: true}
		var err error
		if scanner, ok := provider.(scanProvider); ok {
			var scan configprovider.ScanSummary
			status.Changes, scan, err = scanner.DiffWithScan(ctx, s.profile)
			status.ConfigScan = &scan
		} else if scanner, ok := provider.(resourceProvider); ok {
			status.Changes, status.ResourceGit, err = scanner.DiffWithGitWorkingState(ctx, s.profile)
		} else {
			status.Changes, err = provider.Diff(ctx, s.profile)
		}
		if err != nil {
			return StatusReport{}, fmt.Errorf("diff %s: %w", provider.ID(), err)
		}
		report.Providers = append(report.Providers, status)
	}
	return report, nil
}

func CaptureRequiredError(id string) error {
	switch id {
	case "themes":
		return fmt.Errorf("theme state has not been captured; run capture themes first")
	case "plugins":
		return fmt.Errorf("plugin state has not been captured; run capture plugins first")
	case "config":
		return fmt.Errorf("config state has not been captured; run capture config first")
	case "defaults":
		return fmt.Errorf("defaults state has not been captured; run capture defaults first")
	case "shell":
		return fmt.Errorf("shell state has not been captured; run capture shell first")
	case "hooks":
		return fmt.Errorf("hooks state has not been captured; run capture hooks first")
	case "resources":
		return fmt.Errorf("resources state has not been captured; track a resource or run capture resources first")
	default:
		return fmt.Errorf("%s state has not been captured", id)
	}
}
