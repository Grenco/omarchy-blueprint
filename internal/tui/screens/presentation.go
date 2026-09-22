package screens

import (
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func stateValue(value string) string {
	switch strings.ToLower(value) {
	case string(workflow.TargetPresent):
		return "Present"
	case string(workflow.TargetAbsent):
		return "Absent"
	case "", string(workflow.TargetUnknown):
		return "Not managed"
	default:
		return components.DisplayText(value)
	}
}

func currentStateValue(value workflow.TargetState) string {
	if value == workflow.TargetUnknown {
		return "Not checked"
	}
	return stateValue(string(value))
}

func targetStatus(target workflow.TargetInspection) string {
	switch {
	case target.Desired == workflow.TargetUnknown:
		return "Not managed"
	case target.Current == workflow.TargetUnknown:
		return "Not checked"
	case target.Desired == workflow.TargetAbsent:
		return "Desired absent"
	case target.Desired == target.Current:
		return "In sync"
	case target.Desired == workflow.TargetPresent && target.Current == workflow.TargetAbsent:
		return "Missing"
	default:
		return "Different"
	}
}

func policyDecision(tab string, enabled, blocked bool) string {
	if blocked {
		return "Blocked"
	}
	if tab == "Capture" {
		if enabled {
			return "Include"
		}
		return "Preserve"
	}
	if enabled {
		return "Apply"
	}
	return "Skip"
}

func policySource(source policy.Source) string {
	switch source.Kind {
	case policy.SourceMachineAncestor, policy.SourceProfileAncestor:
		return "Parent"
	case policy.SourceMachineTarget, policy.SourceMachineCategory:
		return "This machine"
	case policy.SourceProfileTarget, policy.SourceProfileCategory:
		return "Profile"
	default:
		return "Default"
	}
}

func policySourceLabel(source policy.Source, blocked bool) string {
	if blocked {
		return "Safety"
	}
	return policySource(source)
}

func styledDecision(styles components.Styles, value string) string {
	switch value {
	case "Include", "Apply", "Present", "In sync":
		return styles.Added(value)
	case "Absent", "Desired absent", "Remove", "Removal", "Delete":
		return styles.Removed(value)
	case "Blocked":
		return styles.Warning(value)
	case "Preserve", "Skip", "Not managed", "Default", "Profile", "This machine", "Parent":
		return styles.Muted(value)
	case "Missing", "Different":
		return styles.Warning(value)
	default:
		return value
	}
}

func stateColumns(width int) []components.Column {
	if width > 0 && width < 90 {
		return []components.Column{
			{Title: "ITEM", Width: 38, MinWidth: 12},
			{Title: "DESIRED", Width: 12, MinWidth: 7},
			{Title: "STATUS", MinWidth: 7},
		}
	}
	return []components.Column{
		{Title: "ITEM", Width: 42, MinWidth: 12},
		{Title: "DESIRED", Width: 12, MinWidth: 7},
		{Title: "CURRENT", Width: 12, MinWidth: 7},
		{Title: "STATUS", MinWidth: 7},
	}
}

func policyColumns(tab string, width int) []components.Column {
	decision := strings.ToUpper(tab)
	state := "CURRENT"
	if tab == "Restore" {
		state = "DESIRED"
	}
	if width > 0 && width < 90 {
		return []components.Column{
			{Title: "ITEM", Width: 40, MinWidth: 12},
			{Title: decision, Width: 13, MinWidth: 7},
			{Title: "STATE", MinWidth: 7},
		}
	}
	return []components.Column{
		{Title: "ITEM", Width: 42, MinWidth: 12},
		{Title: state, Width: 12, MinWidth: 7},
		{Title: decision, Width: 13, MinWidth: 7},
		{Title: "SOURCE", MinWidth: 7},
	}
}

func groupCells(label string, columnCount int, expanded bool, styles components.Styles) []string {
	icon := components.Icons.Collapsed
	if expanded {
		icon = components.Icons.Expanded
	}
	cells := make([]string, max(1, columnCount))
	cells[0] = styles.Accent(icon + " " + components.DisplayText(label))
	return cells
}

func policyDetailLines(header, key string, target workflow.TargetInspection, effective policy.Effective) []string {
	lines := []string{
		header,
		"Target: " + components.DisplayText(key),
		"Desired: " + stateValue(string(target.Desired)),
		"Current: " + currentStateValue(target.Current),
		"Capture policy: " + policyDetailDecision("Capture", effective.Capture, !target.CaptureEligible, target.SafetyReason),
		fmt.Sprintf("Capture source: %s", effective.Capture.Source.Kind),
		"Capture source is " + explicitLabel(effective.Capture.Explicit),
		"Capture source machine: " + components.DisplayText(effective.Capture.Source.Machine),
		"Capture source category: " + components.DisplayText(effective.Capture.Source.Category),
		"Capture source target: " + components.DisplayText(effective.Capture.Source.Target),
		"Restore policy: " + policyDetailDecision("Restore", effective.Restore, !target.RestoreEligible, target.SafetyReason),
		fmt.Sprintf("Restore source: %s", effective.Restore.Source.Kind),
		"Restore source is " + explicitLabel(effective.Restore.Explicit),
		"Restore source machine: " + components.DisplayText(effective.Restore.Source.Machine),
		"Restore source category: " + components.DisplayText(effective.Restore.Source.Category),
		"Restore source target: " + components.DisplayText(effective.Restore.Source.Target),
		fmt.Sprintf("Capture eligible: %t", target.CaptureEligible),
		fmt.Sprintf("Restore eligible: %t", target.RestoreEligible),
		fmt.Sprintf("Supports capture: %t", target.Capabilities.SupportsCapture),
		fmt.Sprintf("Supports restore: %t", target.Capabilities.SupportsRestore),
		fmt.Sprintf("Supports desired absence: %t", target.Capabilities.SupportsDesiredAbsence),
		fmt.Sprintf("Supports Exact removal: %t", target.Capabilities.SupportsExactRemoval),
		fmt.Sprintf("Hierarchical: %t", target.Capabilities.Hierarchical),
	}
	if target.SafetyReason != "" {
		lines = append(lines, "Safety restriction: "+components.DisplayText(target.SafetyReason))
	}
	return lines
}
