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
	ID           string                                         `json:"id"`
	Captured     bool                                           `json:"captured"`
	Snapshot     any                                            `json:"snapshot,omitempty"`
	Changes      []model.Change                                 `json:"changes"`
	ConfigScan   *configprovider.ScanSummary                    `json:"config_scan,omitempty"`
	ResourceGit  map[string]resourcesprovider.GitWorkingSummary `json:"resource_git,omitempty"`
	Verification *model.VerificationResult                      `json:"verification,omitempty"`
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

func (s *ReadCycle) Status(ctx context.Context, onlyProvider string) (StatusReport, error) {
	release, err := s.observations.BeginWork()
	if err != nil {
		return StatusReport{}, err
	}
	defer release()
	ctx, stop := s.withContext(ctx)
	defer stop()
	if e := s.check(ctx); e != nil {
		return StatusReport{}, e
	}
	selected := capturedProviders(s.providers, s.profile)
	if onlyProvider != "" {
		provider, ok := ProviderByID(s.providers, onlyProvider)
		if !ok {
			return StatusReport{}, fmt.Errorf("unknown category %s", onlyProvider)
		}
		if !provider.Captured(profile.CloneData(s.profile)) {
			return StatusReport{}, CaptureRequiredError(provider.ID())
		}
		selected = []ReadProvider{provider}
	}
	report := StatusReport{Profile: profile.CloneData(s.profile), Machine: cloneMachine(s.machine), Providers: make([]ProviderStatus, 0, len(selected))}
	observe := func(ctx context.Context, provider ReadProvider) (ProviderStatus, error) {
		status := ProviderStatus{ID: provider.ID(), Captured: true, Snapshot: providerSnapshot(profile.CloneData(s.profile), provider.ID())}
		var err error
		if scanner, ok := provider.(scanProvider); ok {
			var scan configprovider.ScanSummary
			status.Changes, scan, err = scanner.DiffWithScan(ctx, profile.CloneData(s.profile))
			status.ConfigScan = &scan
		} else if scanner, ok := provider.(resourceProvider); ok {
			status.Changes, status.ResourceGit, err = scanner.DiffWithGitWorkingState(ctx, profile.CloneData(s.profile))
		} else {
			status.Changes, err = provider.Diff(ctx, profile.CloneData(s.profile))
		}
		if err != nil {
			return ProviderStatus{}, fmt.Errorf("diff %s: %w", provider.ID(), err)
		}
		if e := s.check(ctx); e != nil {
			return ProviderStatus{}, e
		}
		return status, nil
	}
	report.Providers, err = observeProviders(ctx, selected, readProviderConcurrency, observe)
	if err != nil {
		// A failed refresh is terminal for this cycle. Cancel and join slot
		// loaders too: canceling a projection waiter alone leaves shared facts
		// loading. Release first so Close cannot wait on this operation itself.
		release()
		if !observationCanceled(err) || s.observations.Err() != nil {
			s.Close()
		}
		return StatusReport{}, err
	}
	if e := s.check(ctx); e != nil {
		return StatusReport{}, e
	}
	return report, nil
}

// CaptureStatus includes uncaptured categories so the Capture screen can make
// first capture discoverable while reusing normal status for captured state.
func (s *ReadCycle) CaptureStatus(ctx context.Context) (StatusReport, error) {
	report, err := s.Status(ctx, "")
	if err != nil {
		return StatusReport{}, err
	}
	byID := make(map[string]ProviderStatus, len(report.Providers))
	for _, status := range report.Providers {
		byID[status.ID] = status
	}
	report.Providers = report.Providers[:0]
	for _, id := range ProviderIDs(s.providers) {
		if status, ok := byID[id]; ok {
			report.Providers = append(report.Providers, status)
		} else {
			report.Providers = append(report.Providers, ProviderStatus{ID: id})
		}
	}
	return report, nil
}

// providerSnapshot exposes the saved desired state without coupling callers to
// profile.Data's complete on-disk layout.
func providerSnapshot(data profile.Data, id string) any {
	switch id {
	case "packages":
		return data.Packages
	case "themes":
		return data.Themes
	case "plugins":
		return data.Plugins
	case "resources":
		return data.Resources
	case "config":
		return data.Config
	case "defaults":
		return data.Defaults
	case "shell":
		return data.Shell
	case "hooks":
		return data.Hooks
	case "services":
		return data.Services
	default:
		return nil
	}
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
	case "services":
		return fmt.Errorf("services state has not been captured; select a service in capture --review services first")
	case "resources":
		return fmt.Errorf("resources state has not been captured; track a resource or run capture resources first")
	default:
		return fmt.Errorf("%s state has not been captured", id)
	}
}
