package screens

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
)

func TestPresentationUsesUserFacingStateAndPolicyVocabulary(t *testing.T) {
	for _, test := range []struct {
		name string
		got  string
		want string
	}{
		{name: "present", got: stateValue("present"), want: "Present"},
		{name: "absent", got: stateValue("absent"), want: "Absent"},
		{name: "capture enabled", got: policyDecision("Capture", true, false), want: "Include"},
		{name: "capture disabled", got: policyDecision("Capture", false, false), want: "Preserve"},
		{name: "restore enabled", got: policyDecision("Restore", true, false), want: "Apply"},
		{name: "restore disabled", got: policyDecision("Restore", false, false), want: "Skip"},
		{name: "blocked", got: policyDecision("Restore", true, true), want: "Blocked"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("got %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestPresentationUsesConcisePolicySources(t *testing.T) {
	for _, test := range []struct {
		kind policy.SourceKind
		want string
	}{
		{policy.SourceDefault, "Default"},
		{policy.SourceProfileTarget, "Profile"},
		{policy.SourceProfileCategory, "Profile"},
		{policy.SourceMachineTarget, "This machine"},
		{policy.SourceMachineCategory, "This machine"},
		{policy.SourceProfileAncestor, "Parent"},
		{policy.SourceMachineAncestor, "Parent"},
	} {
		if got := policySource(policy.Source{Kind: test.kind}); got != test.want {
			t.Errorf("source %q = %q, want %q", test.kind, got, test.want)
		}
	}
	if got := policySourceLabel(policy.Source{Kind: policy.SourceMachineTarget}, true); got != "Safety" {
		t.Fatalf("blocked source = %q, want Safety", got)
	}
}

func TestPresentationStylesReinforceButDoNotReplaceMeaning(t *testing.T) {
	plain := components.NewStyles(components.ThemePalette{ColorEnabled: false})
	for _, value := range []string{"Include", "Preserve", "Apply", "Skip", "Blocked", "Absent"} {
		if got := styledDecision(plain, value, false); got != value {
			t.Errorf("no-colour %q = %q", value, got)
		}
	}
	colour := components.NewStyles(components.ThemePalette{ColorEnabled: true, Added: "#00ff00", Removed: "#ff0000", Muted: "#888888", Warning: "#ffff00"})
	for _, value := range []string{"Include", "Preserve", "Blocked"} {
		if got := styledDecision(colour, value, false); got == value || !strings.Contains(got, "\x1b[") {
			t.Errorf("colour did not style %q: %q", value, got)
		}
	}
}

func TestPresentationStylesSkipSelectedRows(t *testing.T) {
	// A selected row's cells must stay plain: Styles.Selection wraps the whole
	// line afterwards, and a foreground style embedded inside that line does
	// not survive the wrap (its reset code cuts the selection background
	// short). See presentation.go's styledDecision/groupCells comment.
	colour := components.NewStyles(components.ThemePalette{ColorEnabled: true, Added: "#00ff00", Removed: "#ff0000", Muted: "#888888", Warning: "#ffff00"})
	for _, value := range []string{"Include", "Preserve", "Blocked", "Absent"} {
		if got := styledDecision(colour, value, true); got != value {
			t.Errorf("selected %q should stay plain, got %q", value, got)
		}
	}
	if got := groupCells("group", 2, true, colour, true); got[0] != components.Icons.Expanded+" group" {
		t.Errorf("selected group cell should stay plain, got %q", got[0])
	}
}

func TestPresentationSelectsDecisionCriticalResponsiveColumns(t *testing.T) {
	for _, test := range []struct {
		width   int
		state   []string
		capture []string
		restore []string
	}{
		{140, []string{"ITEM", "DESIRED", "CURRENT", "STATUS"}, []string{"ITEM", "CURRENT", "CAPTURE", "SOURCE"}, []string{"ITEM", "DESIRED", "RESTORE", "SOURCE"}},
		{100, []string{"ITEM", "DESIRED", "CURRENT", "STATUS"}, []string{"ITEM", "CURRENT", "CAPTURE", "SOURCE"}, []string{"ITEM", "DESIRED", "RESTORE", "SOURCE"}},
		{80, []string{"ITEM", "DESIRED", "STATUS"}, []string{"ITEM", "CAPTURE", "STATE"}, []string{"ITEM", "RESTORE", "STATE"}},
	} {
		assertColumnTitles(t, stateColumns(test.width), test.state)
		assertColumnTitles(t, policyColumns("Capture", test.width), test.capture)
		assertColumnTitles(t, policyColumns("Restore", test.width), test.restore)
	}
}

func assertColumnTitles(t *testing.T, columns []components.Column, want []string) {
	t.Helper()
	if len(columns) != len(want) {
		t.Fatalf("columns = %#v, want titles %q", columns, want)
	}
	for i := range want {
		if columns[i].Title != want[i] {
			t.Fatalf("column %d = %q, want %q", i, columns[i].Title, want[i])
		}
	}
}
