package workflow

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

type SyncInspectOptions struct {
	Fetch       bool
	Resolutions []syncengine.TargetResolution
}

// InspectSync creates exactly one read lifecycle. All results are descriptive;
// this path cannot acquire mutation authority or write the cursor/profile.
func (s *Session) InspectSync(ctx context.Context, opts SyncInspectOptions) (syncengine.Plan, error) {
	r, err := s.BeginRead(ctx)
	if err != nil {
		return syncengine.Plan{}, err
	}
	defer r.Close()
	s.configMu.Lock()
	providers := append([]Provider(nil), s.providers...)
	deps := s.deps
	root := s.opts.ProfileDir
	s.configMu.Unlock()
	plan := syncengine.Plan{}
	block := func(code, message string) (syncengine.Plan, error) {
		plan.Blockers = append(plan.Blockers, syncengine.Blocker{Code: code, Message: message})
		return plan, nil
	}
	if r.machine.Name == "" {
		return block("machine-required", "Select a named machine before establishing Sync lineage")
	}
	info, err := r.profileGit.CurrentRevisionInfo(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("git-identity", "Sync requires an exact profile repository, attached branch, and configured upstream")
	}
	plan.Identity = syncengine.Identity{Local: info.SHA, Machine: r.machine.Name, Branch: info.Branch, Upstream: info.Upstream, RemoteFingerprint: info.OriginFingerprint}
	if opts.Fetch {
		if err := r.profileGit.FetchUpstream(ctx); err != nil {
			if ctx.Err() != nil {
				return plan, ctx.Err()
			}
			return block("remote-unavailable", "Configured Sync upstream could not be fetched")
		}
	}
	remote, err := r.profileGit.UpstreamCommit(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("remote-revision", "Configured upstream commit is unavailable; fetch explicitly")
	}
	plan.Identity.Remote = remote
	status, err := r.profileGit.ReadOnlyStatus(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("git-status", "Profile Git status is unavailable")
	}
	plan.Git = syncengine.GitState{Clean: len(status.Changes) == 0, Ahead: status.Ahead, Behind: status.Behind, Diverged: status.Ahead > 0 && status.Behind > 0}
	if !plan.Git.Clean {
		plan.Blockers = append(plan.Blockers, syncengine.Blocker{Code: "dirty-profile", Message: "Commit or discard active worktree/index changes before Sync execution"})
	}
	stateHome, err := deps.StateHome()
	if err != nil {
		return plan, fmt.Errorf("sync: machine-local state home unavailable")
	}
	cursor, exists, err := (syncengine.CursorStore{StateHome: stateHome, ProfileRoot: root}).Load()
	if err != nil {
		return block("cursor-invalid", "Sync cursor cannot be loaded; explicit re-bootstrap is required")
	}
	if !exists {
		plan.BootstrapRequired = true
		return block("bootstrap-required", "No reconciled baseline exists; inspect Sync bootstrap choices")
	}
	plan.Identity.Base = cursor.Revision
	if cursor.Machine != plan.Identity.Machine || cursor.Branch != info.Branch || cursor.Upstream != info.Upstream || cursor.RemoteFingerprint != info.OriginFingerprint {
		return block("cursor-identity", "Machine, branch, upstream, or repository identity changed; re-bootstrap explicitly")
	}
	for _, tip := range []string{info.SHA, remote} {
		ok, err := r.profileGit.IsAncestor(ctx, cursor.Revision, tip)
		if err != nil || !ok {
			if ctx.Err() != nil {
				return plan, ctx.Err()
			}
			return block("cursor-lineage", "Cursor must exist and be an ancestor of local and remote revisions; no inferred replacement base")
		}
	}
	// Cache exact views only within this invocation, including when B/L/R coincide.
	views := map[string]SyncProfileView{}
	cleanups := []func(){}
	defer func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}()
	for i, sha := range []string{cursor.Revision, info.SHA, remote} {
		if _, ok := views[sha]; ok {
			continue
		}
		v, cleanup, err := loadSyncView(ctx, r.profileGit, sha, providers, r.machine.Name)
		if err != nil {
			if ctx.Err() != nil {
				return plan, ctx.Err()
			}
			b := syncViewBlocker([]string{"Last-sync (B)", "Pinned local (L)", "Pinned upstream (R)"}[i], err)
			return block(b.Code, b.Message)
		}
		views[sha] = v
		cleanups = append(cleanups, cleanup)
	}
	if err := r.normalizeSyncViews(ctx, views, providers); err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("semantic-identity", "Provider identity context could not be established from the shared observation")
	}
	b, l, remoteView := views[cursor.Revision], views[info.SHA], views[remote]
	control := classifySyncControl(b.Control, l.Control, remoteView.Control)
	plan.Control = &control
	if control.History == syncengine.HistoryConflict {
		return block("control-conflict", "Two-sided control-plane disagreement requires explicit resolution outside target Sync")
	}
	chosen := remoteView
	if control.SelectedSource == syncengine.ControlSourceLocal {
		chosen = l
	}
	plan.Identity.ControlFingerprint = chosen.Control.Fingerprint
	if syncResourceInterpretationChanged(l.Profile, chosen.Profile, r.machine.Name) {
		return block("control-reinspection", "Resource interpretation changed; inspect under the selected mapping before claiming target settlement")
	}
	if syncObservationScopeChanged(l.Profile, chosen.Profile) {
		return block("control-reinspection", "Observation scope changed; reconcile Config include/exclude or resource link exclusions before claiming target settlement")
	}
	// Audit every divergent commit, including reverted non-managed changes.
	if plan.Git.Diverged {
		for _, tip := range []string{info.SHA, remote} {
			paths, err := r.profileGit.CommittedPaths(ctx, cursor.Revision, tip)
			if err != nil {
				return plan, err
			}
			for _, path := range paths {
				if !profile.IsManagedRepositoryPath(path) {
					plan.Blockers = append(plan.Blockers, syncengine.Blocker{Code: "non-managed-history", Message: "Divergent history changed non-Blueprint-managed paths; resolve with Git before semantic reconciliation"})
					break
				}
			}
		}
	}
	histories := []profile.Data{remoteView.Profile, l.Profile, b.Profile}
	live, err := r.observeSync(ctx, l.Profile, histories)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("live-observation", "Machine semantic observation failed; no partial target plan is available")
	}
	plan.Blockers = append(plan.Blockers, live.Blockers...)
	bm, lm, rm, mm := syncTargetMap(b.Targets), syncTargetMap(l.Targets), syncTargetMap(remoteView.Targets), syncTargetMap(live.Targets)
	inspections := map[syncengine.TargetKey]TargetInspection{}
	for category, targets := range live.Inspections {
		for _, t := range targets {
			inspections[syncengine.TargetKey{Category: category, Target: t.Key}] = t
		}
	}
	inputs := []syncengine.TargetInput{}
	for _, key := range syncKeys(bm, lm, rm, mm) {
		target, found := inspections[key]
		if !found {
			return block("live-target-missing", "A historical target lacks trustworthy live semantic evidence")
		}
		effective, err := effectivePolicy(chosen.Profile, PolicyScope{Machine: r.machine.Name}, key.Category, target)
		if err != nil {
			return block("policy-invalid", "Selected historical machine/policy interpretation is unavailable")
		}
		m, found := mm[key]
		if !found {
			return block("live-target-missing", "A historical target lacks normalized live state")
		}
		inputs = append(inputs, syncengine.TargetInput{Key: key, Base: syncState(bm, key), Local: syncState(lm, key), Remote: syncState(rm, key), Machine: m, Policy: syncengine.EffectivePolicy{Capture: effective.Capture.Enabled, Restore: effective.Restore.Enabled}, Resolution: syncengine.ResolutionNone, MachineResolution: syncengine.MachineResolutionNone})
	}
	reconciled, err := syncengine.Reconcile(syncengine.PlanRequest{Identity: plan.Identity, Git: plan.Git, Targets: inputs})
	if err != nil {
		return plan, err
	}
	reconciled.Blockers = plan.Blockers
	reconciled.Control = plan.Control
	if len(opts.Resolutions) > 0 {
		reconciled, err = ResolveSyncPlan(reconciled, opts.Resolutions)
		if err != nil {
			return plan, err
		}
	}
	for _, item := range reconciled.Items {
		reconciled.Blockers = append(reconciled.Blockers, syncDesiredSafetyBlockers(item, inspections[item.Key], live.DesiredInspections)...)
	}
	if err := r.check(ctx); err != nil {
		return syncengine.Plan{}, err
	}
	return reconciled, nil
}

type syncCycleObservation struct {
	Targets            []syncengine.DesiredTarget
	Inspections        map[string][]TargetInspection
	DesiredInspections []SyncDesiredInspection
	Blockers           []syncengine.Blocker
}

func (r *ReadCycle) observeSync(ctx context.Context, hints profile.Data, histories []profile.Data) (syncCycleObservation, error) {
	release, err := r.observations.BeginWork()
	if err != nil {
		return syncCycleObservation{}, err
	}
	defer release()
	ctx, stop := r.withContext(ctx)
	defer stop()
	result := syncCycleObservation{Inspections: map[string][]TargetInspection{}}
	var participatingProviders []ReadProvider
	for _, p := range r.providers {
		if err := r.check(ctx); err != nil {
			return syncCycleObservation{}, err
		}
		participating := p.Captured(hints)
		for _, d := range histories {
			participating = participating || p.Captured(d)
		}
		if !participating {
			continue
		}
		participatingProviders = append(participatingProviders, p)
	}
	factsByProvider, err := observeReadProviders(ctx, participatingProviders, readProviderConcurrency, func(ctx context.Context, p ReadProvider) (SyncLiveObservation, error) {
		source := readCapabilitySource(p)
		observer, ok := source.(SyncLiveProvider)
		if !ok {
			return SyncLiveObservation{}, fmt.Errorf("sync: provider %s lacks read-bound live projection", p.ID())
		}
		facts, err := observer.SyncObserve(ctx, profile.CloneData(hints), cloneSyncHistories(histories))
		if err != nil {
			return SyncLiveObservation{}, err
		}
		seen := map[syncengine.TargetKey]bool{}
		for _, x := range facts.Targets {
			if x.Key.Category != p.ID() || x.Key.Target == "" || seen[x.Key] {
				return SyncLiveObservation{}, fmt.Errorf("sync: live provider returned wrong category")
			}
			seen[x.Key] = true
			switch x.State.Kind {
			case syncengine.StateUnmanaged:
				if x.State.Fingerprint != "" {
					return SyncLiveObservation{}, fmt.Errorf("sync: unmanaged live state has fingerprint")
				}
			case syncengine.StatePresent, syncengine.StateAbsent:
				if len(x.State.Fingerprint) != 64 {
					return SyncLiveObservation{}, fmt.Errorf("sync: live fingerprint is invalid")
				}
			default:
				return SyncLiveObservation{}, fmt.Errorf("sync: live state is invalid")
			}
		}
		return facts, nil
	})
	if err != nil {
		// Shared slot loaders belong to the cycle, not the scheduler waiter.
		// End this failed inspection and join them before returning. Release
		// our enrollment first so Close cannot wait on its own caller.
		release()
		r.Close()
		return syncCycleObservation{}, err
	}
	for i, facts := range factsByProvider {
		p := participatingProviders[i]
		result.Targets = append(result.Targets, facts.Targets...)
		result.Inspections[p.ID()] = cloneTargets(facts.Inspections)
		result.DesiredInspections = append(result.DesiredInspections, facts.DesiredInspections...)
		result.Blockers = append(result.Blockers, facts.Blockers...)
	}
	if err := r.check(ctx); err != nil {
		return syncCycleObservation{}, err
	}
	return result, nil
}
