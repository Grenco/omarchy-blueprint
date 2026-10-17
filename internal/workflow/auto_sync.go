package workflow

import (
	"context"
	"sort"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// AutoSyncPreviewOptions configures a read-only Auto Sync preview.
type AutoSyncPreviewOptions struct {
	// Fetch updates the remote-tracking ref first, as an explicit, disclosed
	// effect. A fetching preview holds the profile lock, taken without
	// waiting, around the fetch and the inspection, and returns
	// syncengine.ErrProfileBusy if a Sync holds it.
	Fetch    bool
	Grant    autosync.Grant
	HostID   string
	Registry autosync.Registry
	Evidence autosync.Evidence

	fetchTimeout time.Duration
	afterLock    func()
}

// AutoSyncPlanReason is a whole-plan reason (a run-time blocker or a part of
// the plan Auto Follow never performs). Each stops unattended application
// for this run only.
type AutoSyncPlanReason string

const (
	AutoSyncBlocker            AutoSyncPlanReason = "sync-blocker"
	AutoSyncManualSyncRequired AutoSyncPlanReason = "manual-sync-required"
	AutoSyncDiverged           AutoSyncPlanReason = "diverged-history"
	AutoSyncUnpublishedLocal   AutoSyncPlanReason = "unpublished-local-changes"
	AutoSyncLocalChanges       AutoSyncPlanReason = "local-changes-need-capture"
	AutoSyncNeedsDecision      AutoSyncPlanReason = "needs-decision"
	AutoSyncDeferred           AutoSyncPlanReason = "deferred"
	AutoSyncPolicyChanged      AutoSyncPlanReason = "settings-changed"
	// Planning the pinned remote revision before fast-forward needs
	// snapshot-root planning (Phase 11 PR C, Task C0).
	AutoSyncPlanUnavailable    AutoSyncPlanReason = "operation-plan-unavailable"
	AutoSyncRestoreCannotApply AutoSyncPlanReason = "restore-cannot-apply"
	AutoSyncRequirement        AutoSyncPlanReason = "restore-requirement"
	AutoSyncCompatibility      AutoSyncPlanReason = "compatibility-not-supported"
	AutoSyncActivationReview   AutoSyncPlanReason = "activation-review"
	AutoSyncOperationsReview   AutoSyncPlanReason = "operations-need-review"
)

// AutoSyncPreview is descriptive only: it never authorises execution, which
// rechecks everything under the profile lock.
type AutoSyncPreview struct {
	Fetched bool `json:"fetched"`
	// Eligible: every proposed change could run unattended now. UpToDate:
	// nothing is proposed. Both are false when anything needs review.
	Eligible bool                    `json:"eligible"`
	UpToDate bool                    `json:"up_to_date"`
	Reasons  []AutoSyncPlanReason    `json:"reasons,omitempty"`
	Blockers []string                `json:"blockers,omitempty"`
	Counts   AutoSyncCounts          `json:"counts"`
	Plan     autosync.Evaluation     `json:"plan"`
	Presets  []autosync.Availability `json:"presets"`
}

type AutoSyncCounts struct {
	Automatic   int `json:"automatic"`    // operations that could run now
	NeedsReview int `json:"needs_review"` // operations or targets waiting for a person
	Matching    int `json:"matching"`     // targets already settled
	Preserved   int `json:"preserved"`    // differences kept by policy, nothing proposed
}

// PreviewAutoSync inspects Sync (fetching only when asked) and classifies
// the entire plan against the local grant. It writes no grant, checkout,
// cursor or live state.
func (s *Session) PreviewAutoSync(ctx context.Context, opts AutoSyncPreviewOptions) (AutoSyncPreview, error) {
	return s.previewAutoSync(ctx, opts)
}

func (s *Session) previewAutoSync(ctx context.Context, opts AutoSyncPreviewOptions) (AutoSyncPreview, error) {
	if opts.Fetch {
		s.configMu.Lock()
		deps, root := s.deps, s.opts.ProfileDir
		s.configMu.Unlock()
		stateHome, err := deps.StateHome()
		if err != nil {
			return AutoSyncPreview{}, err
		}
		lock, err := syncengine.AcquireProfileLock(stateHome, root)
		if err != nil {
			return AutoSyncPreview{}, err
		}
		defer lock.Close()
		if opts.afterLock != nil {
			opts.afterLock()
		}
		// A profile without an exact repository, branch and upstream has
		// nothing to fetch; the inspection reports why instead.
		s.configMu.Lock()
		git := s.profileGit
		s.configMu.Unlock()
		if _, err := git.CurrentRevisionInfo(ctx); err != nil {
			if ctx.Err() != nil {
				return AutoSyncPreview{}, ctx.Err()
			}
			opts.Fetch = false
		} else if err := s.fetchSyncUpstream(ctx, opts.fetchTimeout); err != nil {
			return AutoSyncPreview{}, err
		}
	}
	registry := opts.Registry
	if registry.Version == "" {
		registry = autosync.DefaultRegistry()
	}
	preview := AutoSyncPreview{Fetched: opts.Fetch}
	for _, preset := range []autosync.Preset{autosync.PresetConservative, autosync.PresetBalanced, autosync.PresetHandsOff} {
		preview.Presets = append(preview.Presets, autosync.PresetAvailability(registry, preset))
	}
	// Any fetch has already happened, under the lock.
	plan, err := s.InspectSync(ctx, SyncInspectOptions{})
	if err != nil {
		return preview, err
	}
	reasons := map[AutoSyncPlanReason]bool{}
	for _, blocker := range plan.Blockers {
		reasons[AutoSyncBlocker] = true
		preview.Blockers = append(preview.Blockers, blocker.Code)
	}
	if plan.BootstrapRequired || plan.Identity.Base == "" {
		reasons[AutoSyncManualSyncRequired] = true
	}
	if plan.Git.Diverged {
		reasons[AutoSyncDiverged] = true
	} else if plan.Git.Ahead > 0 {
		reasons[AutoSyncUnpublishedLocal] = true
	}
	// Remote changes to Capture/Restore choices or interpretation rules are
	// never adopted unattended: they could widen what Auto Sync may apply.
	if plan.Control != nil && plan.Control.History != syncengine.HistoryUnchanged && plan.Control.History != syncengine.HistoryConverged {
		reasons[AutoSyncPolicyChanged] = true
	}
	follow := TargetSelection{}
	restoreApply := map[syncengine.TargetKey]bool{}
	for _, item := range plan.Items {
		restoreApply[item.Key] = item.Policy.Restore
		switch {
		case item.Review != syncengine.ReviewNone:
			reasons[AutoSyncNeedsDecision] = true
			preview.Counts.NeedsReview++
		case item.Deferred:
			reasons[AutoSyncDeferred] = true
			preview.Counts.NeedsReview++
		case item.Settled:
			preview.Counts.Matching++
		case len(item.Actions) == 0:
			preview.Counts.Preserved++
		}
		for _, action := range item.Actions {
			switch action {
			case syncengine.ActionCapture, syncengine.ActionPublish:
				reasons[AutoSyncLocalChanges] = true
			case syncengine.ActionRestore:
				follow[item.Key.Category] = append(follow[item.Key.Category], item.Key.Target)
			}
		}
	}
	if len(follow) > 0 && len(plan.Blockers) == 0 {
		if plan.Git.Behind > 0 {
			reasons[AutoSyncPlanUnavailable] = true
		} else if err := s.previewRestore(ctx, opts, registry, plan, follow, restoreApply, reasons, &preview); err != nil {
			return preview, err
		}
	}
	for reason := range reasons {
		preview.Reasons = append(preview.Reasons, reason)
	}
	sort.Slice(preview.Reasons, func(i, j int) bool { return preview.Reasons[i] < preview.Reasons[j] })
	sort.Strings(preview.Blockers)
	preview.UpToDate = len(preview.Reasons) == 0 && len(follow) == 0
	preview.Eligible = len(preview.Reasons) == 0 && len(follow) > 0 && preview.Plan.Eligible
	return preview, nil
}

func (s *Session) previewRestore(ctx context.Context, opts AutoSyncPreviewOptions, registry autosync.Registry, plan syncengine.Plan, follow TargetSelection, restoreApply map[syncengine.TargetKey]bool, reasons map[AutoSyncPlanReason]bool, preview *AutoSyncPreview) error {
	restorePlan, providers, _, _, err := s.PlanRestoreScopeWithContext(ctx, RestoreScope{Targets: follow}, nil)
	if err != nil {
		return err
	}
	if len(restorePlan.Skipped) > 0 {
		reasons[AutoSyncRestoreCannotApply] = true
	}
	if len(restorePlan.Requirements) > 0 {
		reasons[AutoSyncRequirement] = true
	}
	if len(restorePlan.ActivationReview) > 0 {
		reasons[AutoSyncActivationReview] = true
	}
	for _, category := range restorePlan.Compatibility.Categories {
		if category.Applies && category.State != model.CompatibilitySupported {
			reasons[AutoSyncCompatibility] = true
		}
	}
	root, err := machine.CanonicalProfileRoot(s.ProfileDir())
	if err != nil {
		return err
	}
	current := autosync.Identity{ProfileRoot: root, HostID: opts.HostID, Machine: plan.Identity.Machine, Branch: plan.Identity.Branch, Upstream: plan.Identity.Upstream, RemoteFingerprint: plan.Identity.RemoteFingerprint}
	inputs := make([]autosync.OperationInput, 0, len(restorePlan.Operations))
	for _, op := range restorePlan.Operations {
		in := autosync.OperationInput{Operation: op, Category: op.Provider, RestoreApply: map[string]bool{}}
		if p, ok := ProviderByID(providers, op.Provider); ok {
			if resolver, ok := any(p).(RestoreTargetResolver); ok {
				if keys, ok := resolver.RestoreOperationTargetKeys(op); ok {
					in.Targets = append([]string(nil), keys...)
				}
			}
		}
		for _, key := range in.Targets {
			in.RestoreApply[key] = restoreApply[syncengine.TargetKey{Category: op.Provider, Target: key}]
		}
		inputs = append(inputs, in)
	}
	preview.Plan = autosync.Evaluate(opts.Grant, current, registry, opts.Evidence, inputs)
	for _, d := range preview.Plan.Decisions {
		if d.Eligible {
			preview.Counts.Automatic++
		} else {
			preview.Counts.NeedsReview++
		}
	}
	if !preview.Plan.Eligible {
		reasons[AutoSyncOperationsReview] = true
	}
	return nil
}
