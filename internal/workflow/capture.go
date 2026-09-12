package workflow

import (
	"context"
	"fmt"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
)

type CaptureResult struct {
	Profile    profile.Data
	Providers  []string
	Changes    []model.Change
	ConfigScan *configprovider.ScanSummary
}

type captureTransaction interface {
	CommitCapture() error
	FinalizeCapture() error
	RollbackCapture() error
}

func (s *Session) Capture(ctx context.Context, onlyProvider string) (CaptureResult, error) {
	selected := s.providers
	if onlyProvider != "" {
		provider, ok := ProviderByID(s.providers, onlyProvider)
		if !ok {
			return CaptureResult{}, fmt.Errorf("unknown category %s", onlyProvider)
		}
		selected = []Provider{provider}
	}
	info, err := omarchy.Detect(ctx, s.deps.Runner)
	if err != nil {
		return CaptureResult{}, err
	}
	data := s.profile
	result := CaptureResult{Profile: data}
	var transactions []captureTransaction
	for _, provider := range selected {
		state, changes, err := provider.Capture(ctx, &data)
		if err != nil {
			for _, transaction := range transactions {
				_ = transaction.RollbackCapture()
			}
			return CaptureResult{}, fmt.Errorf("capture %s: %w", provider.ID(), err)
		}
		if transaction, ok := provider.(captureTransaction); ok {
			transactions = append(transactions, transaction)
		}
		if state == nil {
			continue
		}
		result.Providers = append(result.Providers, provider.ID())
		if config, ok := state.(configprovider.CaptureResult); ok {
			scan := config.Scan
			result.ConfigScan = &scan
		}
		result.Changes = append(result.Changes, changes...)
	}
	if len(result.Providers) > 0 {
		data.Manifest.Profile.UpdatedAt = s.deps.Now().UTC()
		data.Manifest.Omarchy.CapturedVersion, data.Manifest.Omarchy.Channel = info.Version, info.Channel
		if err := profile.Save(s.opts.ProfileDir, data); err != nil {
			for _, transaction := range transactions {
				_ = transaction.RollbackCapture()
			}
			return CaptureResult{}, fmt.Errorf("save profile: %w", err)
		}
		for _, transaction := range transactions {
			if err := transaction.CommitCapture(); err != nil {
				return CaptureResult{}, err
			}
		}
		for _, transaction := range transactions {
			if err := transaction.FinalizeCapture(); err != nil {
				return CaptureResult{}, err
			}
		}
	}
	s.profile = data
	result.Profile = data
	return result, nil
}
