package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/spf13/cobra"
	"io"
	"sort"
	"strings"
)

func parseSyncChoices(flags map[string][]string) ([]syncengine.TargetResolution, error) {
	byKey := map[syncengine.TargetKey]syncengine.TargetResolution{}
	for _, name := range []string{"keep-local", "accept-remote", "keep-machine", "keep-profile", "defer"} {
		for _, raw := range flags[name] {
			category, target, ok := strings.Cut(raw, ":")
			if !ok || category == "" || target == "" || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\x00\r\n") {
				return nil, errors.New("invalid Sync target selector; use category:provider-target")
			}
			key := syncengine.TargetKey{Category: category, Target: target}
			choice := byKey[key]
			choice.Key = key
			if name == "keep-machine" || name == "keep-profile" {
				if choice.MachineResolution != "" || choice.Resolution == syncengine.ResolutionDefer {
					return nil, errors.New("duplicate or conflicting Sync resolution")
				}
				choice.MachineResolution = syncengine.MachineResolution(name)
			} else {
				if choice.Resolution != "" || (name == "defer" && choice.MachineResolution != "") {
					return nil, errors.New("duplicate or conflicting Sync resolution")
				}
				choice.Resolution = syncengine.Resolution(name)
			}
			byKey[key] = choice
		}
	}
	choices := make([]syncengine.TargetResolution, 0, len(byKey))
	for _, choice := range byKey {
		choices = append(choices, choice)
	}
	sort.Slice(choices, func(i, j int) bool {
		a, b := choices[i].Key, choices[j].Key
		if a.Category == b.Category {
			return a.Target < b.Target
		}
		return a.Category < b.Category
	})
	return choices, nil
}

func syncApplyCommand(deps Dependencies, opt *options, bootstrap *workflow.SyncBootstrapChoice) *cobra.Command {
	use := "apply"
	if bootstrap != nil {
		use = string(*bootstrap)
	}
	yes, dryRun, noFetch, exact, force := false, false, false, false, false
	activation := ""
	flags := map[string][]string{}
	cmd := &cobra.Command{Use: use, Short: "Review and execute one semantic reconciliation", Args: cobra.NoArgs}
	for _, name := range []string{"keep-local", "accept-remote", "keep-machine", "keep-profile", "defer"} {
		if bootstrap != nil {
			break
		}
		name := name
		cmd.Flags().StringArray(name, nil, "one-shot resolution for category:provider-target")
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "explicitly approve this invocation and its fresh Capture/Restore plans")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "inspect resolved intent without execution or cursor writes")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "inspect cached upstream without initial fetch")
	cmd.Flags().BoolVar(&exact, "exact", false, "one-run Restore exact convergence; safety/policy still apply")
	cmd.Flags().BoolVar(&force, "force", false, "one-run Restore force conflict mode; safety/policy still apply")
	cmd.Flags().StringVar(&activation, "activation", "", "one-run Services activation: persistent, working or interactive review")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if !dryRun && !yes && (opt.json || !deps.IsTTY()) {
			return errors.New("Sync execution without an interactive terminal requires --yes; use --dry-run to inspect")
		}
		if bootstrap == nil {
			for _, name := range []string{"keep-local", "accept-remote", "keep-machine", "keep-profile", "defer"} {
				flags[name], _ = cmd.Flags().GetStringArray(name)
			}
		}
		choices, err := parseSyncChoices(flags)
		if err != nil {
			return err
		}
		s, err := openWorkflow(deps, opt)
		if err != nil {
			return errors.New("Sync profile could not be opened safely")
		}
		req := workflow.SyncExecutionRequest{Resolutions: choices, Bootstrap: bootstrap, Terminal: deps.IsTTY() && !opt.json}
		var plan syncengine.Plan
		var reviewData any
		if bootstrap != nil {
			review, err := s.InspectSyncBootstrap(cmd.Context(), workflow.SyncInspectOptions{Fetch: !noFetch})
			if err != nil {
				return errors.New("Sync bootstrap inspection failed")
			}
			req.ReviewedBootstrap = &review
			reviewData = review
			plan, err = workflow.ResolveSyncBootstrapPlan(review, *bootstrap)
			if err != nil {
				if dryRun {
					return emit(deps.Out, opt.json, "sync bootstrap preview", true, map[string]any{"sync": review}, formatSyncBootstrap(review))
				}
				return err
			}
		} else {
			plan, err = s.InspectSync(cmd.Context(), workflow.SyncInspectOptions{Fetch: !noFetch, Resolutions: choices})
			if err != nil {
				return errors.New("Sync inspection/resolutions are invalid; inspect target IDs and choices")
			}
			req.ReviewedPlan = plan
			req.Resolutions = nil
			reviewData = plan
		}
		if dryRun {
			return emit(deps.Out, opt.json, "sync apply preview", true, map[string]any{"sync": reviewData}, formatSyncExecutionSummary(plan))
		}
		if len(plan.Blockers) > 0 || plan.BootstrapRequired {
			if !opt.json {
				fmt.Fprint(deps.Out, formatSyncExecutionSummary(plan))
			}
			return errors.New("Sync execution is blocked; inspect and resolve before applying")
		}
		for _, item := range plan.Items {
			if item.Review != syncengine.ReviewNone || item.Deferred {
				return errors.New("Sync has unresolved/deferred intent; no execution or cursor advancement")
			}
		}
		input := bufio.NewReader(deps.In)
		confirm := func(label string) error {
			if yes {
				return nil
			}
			fmt.Fprintf(deps.Out, "%s? [y/N] ", label)
			answer, err := input.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			value := strings.ToLower(strings.TrimSpace(answer))
			if value != "y" && value != "yes" {
				return errors.New("Sync cancelled")
			}
			return nil
		}
		if !opt.json {
			fmt.Fprint(deps.Out, formatSyncExecutionSummary(plan))
		}
		if err := confirm("Execute these reviewed Sync stages"); err != nil {
			return err
		}
		if force || exact || activation != "" {
			base := policy.DefaultRestoreOptions()
			if m := s.Machine().Machine; m != nil {
				base = m.EffectiveRestoreDefaults()
			}
			if force {
				base.Conflicts = policy.ConflictForce
			}
			if exact {
				base.Convergence = policy.ConvergenceExact
			}
			if activation != "" {
				base.Activation = policy.ActivationMode(activation)
				if err := policy.ValidateRestoreOptions(base); err != nil {
					return errors.New("invalid Sync Restore activation mode")
				}
			}
			req.RestoreOptions = &base
		}
		req.ReviewActivation = func(_ context.Context, candidates []model.ActivationCandidate) (*policy.ActivationReviewSelection, error) {
			if !req.Terminal {
				return nil, errors.New("Services activation review needs an interactive terminal")
			}
			selection := &policy.ActivationReviewSelection{}
			for _, candidate := range candidates {
				fmt.Fprintf(deps.Out, "Activate %s after Restore? %s [y/N] ", safeDisplay(candidate.Unit), safeDisplay(candidate.Reason))
				answer, err := input.ReadString('\n')
				if err != nil && strings.TrimSpace(answer) == "" {
					return nil, errors.New("Services activation review cancelled")
				}
				value := strings.ToLower(strings.TrimSpace(answer))
				if value == "y" || value == "yes" {
					selection.Units = append(selection.Units, candidate.Unit)
				}
			}
			return selection, nil
		}
		req.ApproveCapture = func(_ context.Context, review workflow.CaptureInspection) error {
			if !opt.json {
				fmt.Fprintln(deps.Out, "Fresh selected Capture review")
				for _, section := range review.Review() {
					for _, target := range section.Targets {
						if target.Decision.Capture {
							fmt.Fprintf(deps.Out, "  %s:%s — %s\n", safeDisplay(target.Category), safeDisplay(target.Inspection.Key), target.Outcome.Label())
						}
					}
				}
			}
			return confirm("Approve this freshly inspected Capture")
		}
		req.ApproveRestore = func(_ context.Context, review model.RestorePlan) error {
			if !opt.json {
				fmt.Fprint(deps.Out, renderPlanWithOptions(review, false, restorePlanOptionsFromPolicy(restoreOptionsForSync(s, req.RestoreOptions))))
			}
			return confirm("Approve this freshly planned Restore")
		}
		result, err := s.ExecuteSync(cmd.Context(), req)
		if err != nil {
			if emitErr := emit(deps.Out, opt.json, "sync apply", false, map[string]any{"execution": result}, "Sync stopped; inspect profile Git and Sync before retrying. Cursor not advanced.\n"); emitErr != nil {
				return emitErr
			}
			return errors.New("Sync execution stopped; published history may remain, machine may be unsettled, cursor was not advanced")
		}
		return emit(deps.Out, opt.json, "sync apply", true, map[string]any{"execution": result}, fmt.Sprintf("Sync settled through %s; cursor advanced.\n", syncShortSHA(result.FinalRevision)))
	}
	return cmd
}

func restoreOptionsForSync(s *workflow.Session, override *policy.RestoreOptions) policy.RestoreOptions {
	if override != nil {
		return *override
	}
	if m := s.Machine().Machine; m != nil {
		return m.EffectiveRestoreDefaults()
	}
	return policy.DefaultRestoreOptions()
}
func formatSyncExecutionSummary(plan syncengine.Plan) string {
	capture, follow, deferred := 0, 0, 0
	for _, item := range plan.Items {
		if item.Deferred {
			deferred++
		}
		for _, action := range item.Actions {
			if action == syncengine.ActionCapture {
				capture++
			}
			if action == syncengine.ActionRestore {
				follow++
			}
		}
	}
	history := "retain reviewed local history"
	if plan.Git.Diverged {
		history = "compose validated semantic tree and two-parent reconciliation"
	} else if plan.Git.Behind > 0 {
		history = "fast-forward to exact reviewed R"
	}
	return fmt.Sprintf("%s\nExact reviewed revisions: B %s  L %s  R %s\nExecution: %s -> fresh selected Capture (%d) -> managed commit/push -> verify publication -> fresh approved Restore (%d) -> fresh settlement -> cursor\nSemantic deferred: %d  Blockers: %d\n", formatSyncPlan(plan), safeDisplay(plan.Identity.Base), safeDisplay(plan.Identity.Local), safeDisplay(plan.Identity.Remote), history, capture, follow, deferred, len(plan.Blockers))
}
