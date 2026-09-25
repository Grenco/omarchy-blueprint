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
	return s.captureMany(ctx, ids, nil)
}

// CaptureApproved is CaptureMany bound to a review the user approved. The
// approval is a precondition of the transaction itself, not a separate
// check: one fresh inspection is both compared with the approved review and
// handed to providers as their Capture decisions, and a second inspection
// after staging but before saving confirms nothing Capture read live has
// changed either. Any material difference (an outcome, or a written
// value's Fingerprint) rolls back with zero profile mutation and returns a
// CaptureReviewChangedError carrying the recalculated review.
func (s *Session) CaptureApproved(ctx context.Context, ids []string, approved CaptureInspection) (CaptureResult, error) {
	return s.captureMany(ctx, ids, &approved)
}

func (s *Session) captureMany(ctx context.Context, ids []string, approved *CaptureInspection) (CaptureResult, error) {
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
	var authority CaptureInspection
	var contexts map[string]CaptureContext
	if approved != nil {
		fresh, err := s.InspectCaptureMany(ctx, ids)
		if err != nil {
			return CaptureResult{}, err
		}
		if changes := approved.ChangesFrom(fresh); len(changes) > 0 {
			return CaptureResult{}, &CaptureReviewChangedError{Fresh: fresh, Changes: changes}
		}
		authority, contexts = fresh, fresh.captureContexts(s.machine.Name, ids)
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
		capCtx, ok := contexts[provider.ID()]
		if !ok {
			capCtx, err = s.resolveCaptureContext(ctx, provider, data)
			if err != nil {
				return CaptureResult{}, errors.Join(fmt.Errorf("inspect %s targets: %w", provider.ID(), err), rollback())
			}
		}
		state, changes, err := provider.Capture(ctx, &data, capCtx)
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
	if approved != nil {
		// Providers read live values while capturing; confirm they still
		// match what was approved before anything is saved.
		after, err := s.recheckCapture(ctx, ids, authority)
		if err != nil {
			return CaptureResult{}, errors.Join(fmt.Errorf("re-check capture: %w", err), rollback())
		}
		if changes := approved.ChangesFrom(after); len(changes) > 0 {
			if rollbackErr := rollback(); rollbackErr != nil {
				return CaptureResult{}, errors.Join(&CaptureReviewChangedError{Fresh: after, Changes: changes}, rollbackErr)
			}
			return CaptureResult{}, &CaptureReviewChangedError{Fresh: after, Changes: changes}
		}
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

// resolveCaptureContext inspects a provider's targets and resolves each
// one's effective Capture policy against the session's currently selected
// machine. Provider safety always wins: a target the provider itself
// reports ineligible resolves to disabled regardless of what policy says
// (Capabilities are descriptive, not permission-granting).
func (s *Session) resolveCaptureContext(ctx context.Context, provider Provider, data profile.Data) (CaptureContext, error) {
	targets, err := provider.InspectTargets(ctx, data)
	if err != nil {
		return CaptureContext{}, err
	}
	decisions := make(map[string]CaptureDecision, len(targets))
	for _, target := range targets {
		_, decision, err := s.resolveCaptureTarget(ctx, provider.ID(), target)
		if err != nil {
			return CaptureContext{}, fmt.Errorf("resolve %s policy for %s: %w", provider.ID(), target.Key, err)
		}
		decisions[target.Key] = decision
	}
	return CaptureContext{Machine: s.machine.Name, Targets: decisions}, nil
}
