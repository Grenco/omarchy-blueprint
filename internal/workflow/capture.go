package workflow

import (
	"context"
	"errors"
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

type PostCommitWarning struct{ Err error }

func (w PostCommitWarning) Error() string {
	return "capture applied; cleanup warning: " + w.Err.Error()
}
func (w PostCommitWarning) Unwrap() error { return w.Err }

type captureTransaction interface {
	CommitCapture() error
	FinalizeCapture() error
	RollbackCapture() error
}

func (s *Session) Capture(ctx context.Context, onlyProvider string) (CaptureResult, error) {
	if onlyProvider == "" {
		ids := make([]string, 0, len(s.providers))
		for _, provider := range s.providers {
			ids = append(ids, provider.ID())
		}
		return s.CaptureMany(ctx, ids)
	}
	return s.CaptureMany(ctx, []string{onlyProvider})
}

// CaptureMany captures the requested providers in configured order and saves
// their combined state as one profile update.
func (s *Session) CaptureMany(ctx context.Context, ids []string) (CaptureResult, error) {
	requested := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, ok := ProviderByID(s.providers, id); !ok {
			return CaptureResult{}, fmt.Errorf("unknown category %s", id)
		}
		requested[id] = true
	}
	selected := make([]Provider, 0, len(requested))
	for _, provider := range s.providers {
		if requested[provider.ID()] {
			selected = append(selected, provider)
		}
	}
	info, err := omarchy.Detect(ctx, s.deps.Runner)
	if err != nil {
		return CaptureResult{}, err
	}
	data := s.profile
	result := CaptureResult{Profile: data}
	var transactions []captureTransaction
	rollback := func() error {
		var rollbackErr error
		for i := len(transactions) - 1; i >= 0; i-- {
			rollbackErr = errors.Join(rollbackErr, transactions[i].RollbackCapture())
		}
		return rollbackErr
	}
	for _, provider := range selected {
		state, changes, err := provider.Capture(ctx, &data)
		if err != nil {
			return CaptureResult{}, errors.Join(fmt.Errorf("capture %s: %w", provider.ID(), err), rollback())
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
			return CaptureResult{}, errors.Join(fmt.Errorf("save profile: %w", err), rollback())
		}
		for _, transaction := range transactions {
			if err := transaction.CommitCapture(); err != nil {
				rollbackErr := rollback()
				restoreErr := profile.Save(s.opts.ProfileDir, s.profile)
				return CaptureResult{}, errors.Join(fmt.Errorf("commit capture: %w", err), rollbackErr, restoreErr)
			}
		}
		s.profile = data
		result.Profile = data
		var finalizeErr error
		for _, transaction := range transactions {
			finalizeErr = errors.Join(finalizeErr, transaction.FinalizeCapture())
		}
		if finalizeErr != nil {
			return result, PostCommitWarning{Err: finalizeErr}
		}
	}
	s.profile = data
	result.Profile = data
	return result, nil
}
