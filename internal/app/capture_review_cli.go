package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type capturePreviewTarget struct {
	Category        string                  `json:"category"`
	Key             string                  `json:"key"`
	Label           string                  `json:"label"`
	Desired         workflow.TargetState    `json:"desired"`
	Current         workflow.TargetState    `json:"current"`
	CaptureEligible bool                    `json:"capture_eligible"`
	SafetyReason    string                  `json:"safety_reason,omitempty"`
	Policy          policy.EffectiveSetting `json:"policy"`
	Outcome         workflow.CaptureOutcome `json:"outcome"`
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
	sections := make([]capturePreviewSection, 0)
	var human strings.Builder
	fmt.Fprintf(&human, "Capture preview for machine %s\n", session.Machine().Name)
	for _, section := range inspection.Review() {
		output := capturePreviewSection{Group: section.Group, Targets: make([]capturePreviewTarget, 0, len(section.Targets))}
		fmt.Fprintf(&human, "\n%s\n", section.Group)
		for _, target := range section.Targets {
			item := capturePreviewTarget{Category: target.Category, Key: target.Inspection.Key, Label: target.Inspection.Label, Desired: target.Inspection.Desired, Current: target.Inspection.Current, CaptureEligible: target.Inspection.CaptureEligible, SafetyReason: target.Inspection.SafetyReason, Policy: target.Policy, Outcome: target.Outcome}
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
	line, err := bufio.NewReader(deps.In).ReadString('\n')
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
