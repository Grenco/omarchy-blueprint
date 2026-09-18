package screens

import (
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// emptyStateCopy is presentation-only contextual guidance for an optional or
// currently-empty section of the TUI. It never persists and never implies
// onboarding progress.
type emptyStateCopy struct {
	Heading     string
	Explanation string
	Guidance    string
}

// renderEmptyState renders contextual empty-state copy wrapped to width,
// using the active theme's accent for the heading.
func renderEmptyState(styles components.Styles, width int, copy emptyStateCopy) string {
	if width <= 0 {
		width = 80
	}
	heading := components.WrapText(copy.Heading, width)
	for i := range heading {
		heading[i] = styles.Accent(heading[i])
	}
	lines := append([]string(nil), heading...)
	for _, paragraph := range []string{copy.Explanation, copy.Guidance} {
		if paragraph == "" {
			continue
		}
		lines = append(lines, "", strings.Join(components.WrapText(paragraph, width), "\n"))
	}
	return strings.Join(lines, "\n")
}

// profileHasCapturedState reports whether any category has ever been
// captured into the profile.
func profileHasCapturedState(data profile.Data) bool {
	captured := data.Manifest.Capture
	return captured.Packages ||
		captured.Themes ||
		captured.Plugins ||
		captured.Config ||
		captured.Defaults ||
		captured.Shell ||
		captured.Hooks ||
		captured.Resources
}

// providerEmptyState returns contextual guidance for a generic category
// screen, distinguishing uncaptured state from a captured category whose
// saved state legitimately contains nothing to carry. ok is false when the
// normal populated/difference presentation should be used instead.
func providerEmptyState(id string, status workflow.ProviderStatus, tab string) (emptyStateCopy, bool) {
	if !status.Captured {
		return uncapturedProviderCopy(id)
	}
	if tab == "Changes" {
		return emptyStateCopy{}, false
	}

	switch value := status.Snapshot.(type) {
	case profile.Packages:
		if len(value.Official) == 0 &&
			len(value.AUR) == 0 &&
			len(value.Mise) == 0 &&
			len(value.MachineSpecific) == 0 &&
			len(value.Excluded) == 0 {
			return emptyStateCopy{
				Heading:     "No packages or tools to carry",
				Explanation: "Packages is where Blueprint remembers portable system packages, AUR packages, and global Mise tools so they can be installed again on another machine.",
				Guidance:    "If this profile intentionally has no portable packages or tools, there is nothing to do here.",
			}, true
		}
	case profile.Themes:
		if value.Current == "" && len(value.Items) == 0 {
			return emptyStateCopy{
				Heading:     "No themes to carry",
				Explanation: "Themes lets Blueprint remember your selected Omarchy theme and reconstruct installed or local themes when needed.",
				Guidance:    "If there is no theme state Blueprint needs to carry, this section can stay empty.",
			}, true
		}
	case profile.Plugins:
		if len(value.Items) == 0 {
			return emptyStateCopy{
				Heading:     "No plugins to carry",
				Explanation: "Plugins are Omarchy extensions Blueprint can reconstruct, including installed and local plugins.",
				Guidance:    "Having no plugins is completely normal. There is nothing to configure here.",
			}, true
		}
	case profile.Shell:
		if value.Hash == "" {
			return emptyStateCopy{
				Heading:     "No Blueprint-managed Shell customisation",
				Explanation: "Shell is for supported customisations to the Omarchy shell, bar, and layout.",
				Guidance:    "If you use the normal Omarchy shell setup here, there is no extra Shell state to carry.",
			}, true
		}
	case profile.Hooks:
		if len(value.Items) == 0 {
			return emptyStateCopy{
				Heading:     "No hooks to carry",
				Explanation: "Hooks are scripts Omarchy runs automatically at supported events. Blueprint can carry those scripts so the same automation is available when you rebuild another machine.",
				Guidance:    "Having no hooks is completely normal. There is nothing to set up here.",
			}, true
		}
	}

	return emptyStateCopy{}, false
}

// uncapturedProviderCopy explains an optional category that has not been
// captured into the profile yet. It never claims to have inspected the live
// system for the category's contents.
func uncapturedProviderCopy(id string) (emptyStateCopy, bool) {
	switch id {
	case "packages":
		return emptyStateCopy{
			Heading:     "Packages are not saved in this profile yet",
			Explanation: "Packages is where Blueprint remembers portable system packages, AUR packages, and global Mise tools so they can be installed again on another machine.",
			Guidance:    "Capture Packages when you want that software set to become part of the profile.",
		}, true
	case "themes":
		return emptyStateCopy{
			Heading:     "Themes are not saved in this profile yet",
			Explanation: "Themes lets Blueprint remember your selected Omarchy theme and reconstruct installed or local themes when needed.",
			Guidance:    "Capture Themes when you want that appearance state to become part of the profile.",
		}, true
	case "plugins":
		return emptyStateCopy{
			Heading:     "Plugins are not saved in this profile yet",
			Explanation: "Plugins are Omarchy extensions Blueprint can reconstruct, including installed and local plugins.",
			Guidance:    "Capture Plugins when you want those extensions to become part of the profile.",
		}, true
	case "defaults":
		return emptyStateCopy{
			Heading:     "Defaults are not saved in this profile yet",
			Explanation: "Defaults records the terminal, browser, editor, and agent choices Omarchy knows about.",
			Guidance:    "Capture Defaults when you want Blueprint to remember those selections.",
		}, true
	case "shell":
		return emptyStateCopy{
			Heading:     "Shell state is not saved in this profile yet",
			Explanation: "Shell is for supported customisations to the Omarchy shell, bar, and layout.",
			Guidance:    "Capture Shell when you want Blueprint to remember those changes.",
		}, true
	case "hooks":
		return emptyStateCopy{
			Heading:     "Hooks are not saved in this profile yet",
			Explanation: "Hooks are scripts Omarchy runs automatically at supported events. Blueprint can carry those scripts so the same automation is available when you rebuild another machine.",
			Guidance:    "Capture Hooks only when you want Blueprint to remember them.",
		}, true
	default:
		return emptyStateCopy{}, false
	}
}
