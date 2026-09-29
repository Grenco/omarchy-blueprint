package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type capturePreviewTarget struct {
	Category                string                  `json:"category"`
	Key                     string                  `json:"key"`
	Label                   string                  `json:"label"`
	Desired                 workflow.TargetState    `json:"desired"`
	Current                 workflow.TargetState    `json:"current"`
	CaptureEligible         bool                    `json:"capture_eligible"`
	RequiresSelection       bool                    `json:"requires_selection,omitempty"`
	ReviewRemoval           bool                    `json:"review_removal,omitempty"`
	Selected                bool                    `json:"selected"`
	Advanced                bool                    `json:"advanced,omitempty"`
	RecommendedDependencies []string                `json:"recommended_dependencies,omitempty"`
	SafetyReason            string                  `json:"safety_reason,omitempty"`
	Policy                  cliEffectiveSetting     `json:"policy"`
	Outcome                 workflow.CaptureOutcome `json:"outcome"`
}

type capturePreviewSection struct {
	Group   workflow.CaptureReviewGroup `json:"group"`
	Targets []capturePreviewTarget      `json:"targets"`
}

func capturePreviewCommand(ctx context.Context, deps Dependencies, opt *options, args []string, dryRun, review bool) error {
	if review && !opt.json && !deps.IsTTY() {
		return fmt.Errorf("capture --review requires an interactive terminal; use --dry-run or --json to inspect without applying")
	}
	session, err := openWorkflow(deps, opt)
	if err != nil {
		return profileError(opt.profileDir, err)
	}
	ids := make([]string, 0)
	if len(args) > 0 {
		ids = append(ids, args[0])
	} else {
		for _, provider := range workflowProviders(deps, opt) {
			ids = append(ids, provider.ID())
		}
	}
	inspection, err := session.InspectCaptureMany(ctx, ids)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(deps.In)
	if review && !dryRun && !opt.json && len(args) == 1 && args[0] == "services" {
		inspection, err = chooseCaptureCandidates(deps.Out, reader, inspection)
		if err != nil {
			return err
		}
	}
	sections := make([]capturePreviewSection, 0)
	var human strings.Builder
	fmt.Fprintf(&human, "Capture preview for machine %s\n", session.Machine().Name)
	for _, section := range inspection.Review() {
		output := capturePreviewSection{Group: section.Group, Targets: make([]capturePreviewTarget, 0, len(section.Targets))}
		fmt.Fprintf(&human, "\n%s\n", section.Group)
		for _, target := range section.Targets {
			item := capturePreviewTarget{Category: target.Category, Key: target.Inspection.Key, Label: target.Inspection.Label, Desired: target.Inspection.Desired, Current: target.Inspection.Current, CaptureEligible: target.Inspection.CaptureEligible, RequiresSelection: target.Inspection.RequiresSelection, ReviewRemoval: target.Inspection.ReviewRemoval, Selected: target.Selected, Advanced: target.Inspection.Advanced, RecommendedDependencies: target.Inspection.RecommendedDependencies, SafetyReason: target.Inspection.SafetyReason, Policy: effectivePolicyValue(policy.AxisCapture, target.Policy), Outcome: target.Outcome}
			output.Targets = append(output.Targets, item)
			fmt.Fprintf(&human, "  %s/%s: %s", item.Category, item.Key, target.Outcome.Label())
			if item.SafetyReason != "" {
				fmt.Fprintf(&human, " (%s)", item.SafetyReason)
			}
			human.WriteByte('\n')
		}
		sections = append(sections, output)
	}
	if dryRun || opt.json {
		return emit(deps.Out, opt.json, "capture preview", true, map[string]any{"machine": session.Machine().Name, "sections": sections}, human.String())
	}
	fmt.Fprint(deps.Out, human.String(), "\nApply this Capture? [y/N] ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.ToLower(strings.TrimSpace(line)) != "y" && strings.ToLower(strings.TrimSpace(line)) != "yes" {
		return fmt.Errorf("capture cancelled")
	}
	result, err := session.CaptureApproved(ctx, ids, inspection)
	if err != nil {
		var changed *workflow.CaptureReviewChangedError
		if errors.As(err, &changed) {
			return fmt.Errorf("%w; review again: %s", err, strings.Join(changed.Changes, "; "))
		}
		var warning workflow.PostCommitWarning
		if !errors.As(err, &warning) {
			return err
		}
		fmt.Fprintln(deps.Err, "Warning:", warning.Error())
	}
	return emit(deps.Out, opt.json, "capture", true, map[string]any{"changes": result.Changes, "providers": result.Providers}, renderChanges("Captured state", result.Changes))
}

func chooseCaptureCandidates(out io.Writer, reader *bufio.Reader, inspection workflow.CaptureInspection) (workflow.CaptureInspection, error) {
	type choice struct {
		category, key string
		dependencies  int
	}
	var choices []choice
	for category, targets := range inspection.Categories {
		for _, target := range targets {
			if target.Inspection.RequiresSelection && target.Inspection.CaptureEligible && target.Decision.Capture {
				choices = append(choices, choice{category: category, key: target.Inspection.Key, dependencies: len(target.Inspection.RecommendedDependencies)})
			}
		}
	}
	// Ask parents first, then allow a recommended child to be declined.
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].dependencies != choices[j].dependencies {
			return choices[i].dependencies > choices[j].dependencies
		}
		if choices[i].category != choices[j].category {
			return choices[i].category < choices[j].category
		}
		return choices[i].key < choices[j].key
	})
	for _, candidate := range choices {
		var target workflow.CaptureTarget
		for _, item := range inspection.Categories[candidate.category] {
			if item.Inspection.Key == candidate.key {
				target = item
				break
			}
		}
		verb := "Manage"
		if target.Inspection.ReviewRemoval {
			verb = "Record removal of"
		}
		defaultYes, hint := target.Selected, "[y/N]"
		if defaultYes {
			hint = "[Y/n]"
		}
		fmt.Fprintf(out, "%s\n%s %s/%s? %s ", target.Inspection.Label, verb, candidate.category, candidate.key, hint)
		answer, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return workflow.CaptureInspection{}, err
		}
		selected := defaultYes
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			selected = true
		case "n", "no":
			selected = false
		case "":
		default:
			return workflow.CaptureInspection{}, fmt.Errorf("answer yes or no when selecting %s/%s", candidate.category, candidate.key)
		}
		inspection, err = inspection.SelectCandidate(candidate.category, candidate.key, selected)
		if err != nil {
			return workflow.CaptureInspection{}, err
		}
	}
	return inspection, nil
}
