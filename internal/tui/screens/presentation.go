package screens

import (
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

func targetStatus(target workflow.TargetInspection) string {
	switch {
	case target.Desired == workflow.TargetUnknown:
		return "Not managed"
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
			{Title: "STATUS", Width: 18, MinWidth: 7},
		}
	}
	return []components.Column{
		{Title: "ITEM", Width: 42, MinWidth: 12},
		{Title: "DESIRED", Width: 12, MinWidth: 7},
		{Title: "CURRENT", Width: 12, MinWidth: 7},
		{Title: "STATUS", Width: 18, MinWidth: 7},
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
			{Title: "STATE", Width: 16, MinWidth: 7},
		}
	}
	return []components.Column{
		{Title: "ITEM", Width: 42, MinWidth: 12},
		{Title: state, Width: 12, MinWidth: 7},
		{Title: decision, Width: 13, MinWidth: 7},
		{Title: "SOURCE", Width: 16, MinWidth: 7},
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
