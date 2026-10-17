package workflow

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

type SyncBootstrapItem struct {
	Key          syncengine.TargetKey       `json:"key"`
	Local        syncengine.State           `json:"local"`
	Remote       syncengine.State           `json:"remote"`
	Machine      syncengine.State           `json:"machine"`
	Policy       syncengine.EffectivePolicy `json:"policy"`
	Converged    bool                       `json:"converged"`
	Inapplicable bool                       `json:"inapplicable"`
}

// Bootstrap is explicitly baseless. No manufactured merge-base or remote-as-B
// identity is fed into the reconciler or persisted as a settlement claim.
type SyncBootstrapPlan struct {
	Identity            syncengine.Identity  `json:"identity"`
	Git                 syncengine.GitState  `json:"git"`
	Items               []SyncBootstrapItem  `json:"items"`
	Blockers            []syncengine.Blocker `json:"blockers,omitempty"`
	Choices             []string             `json:"choices"`
	ChoiceRequirements  map[string][]string  `json:"choice_requirements"`
	ConvergenceEligible bool                 `json:"convergence_eligible"`
}

func (s *Session) InspectSyncBootstrap(ctx context.Context, opts SyncInspectOptions) (SyncBootstrapPlan, error) {
	r, err := s.BeginRead(ctx)
	if err != nil {
		return SyncBootstrapPlan{}, err
	}
	defer r.Close()
	s.configMu.Lock()
	providers := append([]Provider(nil), s.providers...)
	s.configMu.Unlock()
	plan := SyncBootstrapPlan{
		Choices: []string{"reconcile-upstream", "adopt-machine", "cancel"},
		ChoiceRequirements: map[string][]string{
			"reconcile-upstream": {"Review upstream intent and independent Capture/Restore policy", "Fresh Restore planning, approval and verification before any future execution", "No deferred, skipped or blocked work before settlement"},
			"adopt-machine":      {"Review machine intent and Capture policy", "Fresh Capture planning/approval and publication before settlement", "No deferred, skipped or blocked work before settlement"},
			"cancel":             {"No writes"},
		},
	}
	block := func(code, message string) (SyncBootstrapPlan, error) {
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
		return block("git-identity", "Bootstrap requires an attached profile branch and configured upstream")
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
		return block("remote-revision", "Upstream revision is unavailable; fetch explicitly")
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
		plan.Blockers = append(plan.Blockers, syncengine.Blocker{Code: "dirty-profile", Message: "Commit or discard active checkout/index changes before Sync execution"})
	}
	local, cleanup, err := loadSyncView(ctx, r.profileGit, info.SHA, providers, r.machine.Name)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		b := syncViewBlocker("Pinned local (L)", err)
		return block(b.Code, b.Message)
	}
	defer cleanup()
	rv := local
	if remote != info.SHA {
		var cleanupRemote func()
		rv, cleanupRemote, err = loadSyncView(ctx, r.profileGit, remote, providers, r.machine.Name)
		if err != nil {
			if ctx.Err() != nil {
				return plan, ctx.Err()
			}
			b := syncViewBlocker("Pinned upstream (R)", err)
			return block(b.Code, b.Message)
		}
		defer cleanupRemote()
	}
	views := map[string]SyncProfileView{info.SHA: local, remote: rv}
	if err := r.normalizeSyncViews(ctx, views, providers); err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("semantic-identity", "Provider identity context could not be established from the shared observation")
	}
	local, rv = views[info.SHA], views[remote]
	plan.Identity.ControlFingerprint = rv.Control.Fingerprint
	if local.Control.Fingerprint != rv.Control.Fingerprint {
		plan.Blockers = append(plan.Blockers, syncengine.Blocker{Code: "bootstrap-control-difference", Message: "Without a baseline, differing control planes require explicit external reconciliation"})
	}
	if syncResourceInterpretationChanged(local.Profile, rv.Profile, r.machine.Name) {
		return block("control-reinspection", "Resource interpretation differs; no convergence claim can use the current mapping")
	}
	if syncObservationScopeChanged(local.Profile, rv.Profile) {
		return block("control-reinspection", "Observation scope differs; no convergence claim can use the current Config or resource link exclusions")
	}
	facts, err := r.observeSync(ctx, local.Profile, []profile.Data{rv.Profile, local.Profile})
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return block("live-observation", "Machine semantic observation failed; no partial bootstrap plan is available")
	}
	plan.Blockers = append(plan.Blockers, facts.Blockers...)
	lm, rm, mm := syncTargetMap(local.Targets), syncTargetMap(rv.Targets), syncTargetMap(facts.Targets)
	targets := map[syncengine.TargetKey]TargetInspection{}
	for category, items := range facts.Inspections {
		for _, x := range items {
			targets[syncengine.TargetKey{Category: category, Target: x.Key}] = x
		}
	}
	eligible := info.SHA == remote && plan.Git.Clean && len(plan.Blockers) == 0
	for _, key := range syncKeys(lm, rm, mm) {
		target, found := targets[key]
		machine, observed := mm[key]
		if !found || !observed {
			return block("live-target-missing", "Bootstrap target lacks trustworthy live evidence")
		}
		effective, err := effectivePolicy(rv.Profile, PolicyScope{Machine: r.machine.Name}, key.Category, target)
		if err != nil {
			return block("policy-invalid", "Upstream machine/policy interpretation is unavailable")
		}
		desired := syncState(rm, key)
		converged := machine.Equal(desired)
		inapplicable := !effective.Capture.Enabled && (!effective.Restore.Enabled || desired.Kind == syncengine.StateUnmanaged)
		plan.Items = append(plan.Items, SyncBootstrapItem{Key: key, Local: syncState(lm, key), Remote: desired, Machine: machine, Policy: syncengine.EffectivePolicy{Capture: effective.Capture.Enabled, Restore: effective.Restore.Enabled}, Converged: converged, Inapplicable: inapplicable})
		if !converged && !inapplicable {
			eligible = false
		}
	}
	plan.ConvergenceEligible = eligible
	if err := r.check(ctx); err != nil {
		return SyncBootstrapPlan{}, err
	}
	return plan, nil
}
