package components

import (
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// PolicyPresentation is a screen-independent projection of one policy target.
// Screens own workflow calls; this component only renders their outcome.
type PolicyPresentation struct {
	Tab       string
	Scope     string
	Effective policy.EffectiveSetting
	Blocked   string
}

func RenderPolicy(value PolicyPresentation, width int, styles Styles) string {
	if width <= 0 {
		width = 80
	}
	parts := []string{TabBar([]string{"State", "Capture", "Restore"}, value.Tab, styles), value.Scope}
	if value.Blocked != "" {
		parts = append(parts, "Blocked: "+value.Blocked)
		return strings.Join(parts, "\n")
	}
	parts = append(parts, RenderPolicyStatus(value.Tab, value.Effective, ""))
	return strings.Join(parts, "\n")
}

// RenderPolicyStatus renders a policy outcome without persistence knowledge.
// A non-empty blocked reason always takes precedence over an intentional
// disabled setting so safety cannot be mistaken for a user policy choice.
func RenderPolicyStatus(tab string, effective policy.EffectiveSetting, blocked string) string {
	if blocked != "" {
		return "Blocked: " + blocked
	}
	label := "Apply"
	if tab == "Capture" {
		label = "Include"
	}
	if !effective.Enabled {
		if tab == "Capture" {
			label = "Ignore"
		} else {
			label = "Skip"
		}
	}
	source := "inherited"
	if effective.Explicit {
		source = "explicit"
	}
	return fmt.Sprintf("%s (%s)", label, source)
}
