package components

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
)

func TestDiffViewerUnifiedAndNavigation(t *testing.T) {
	document := inspection.DiffDocument{Kind: inspection.DiffText, OldLabel: "old", NewLabel: "new", Hunks: []inspection.DiffHunk{
		{OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1, Lines: []inspection.DiffLine{{Kind: "remove", OldLine: 1, Text: "old\x1b[31m"}, {Kind: "add", NewLine: 1, Text: "new\t "}}},
		{OldStart: 4, OldCount: 1, NewStart: 4, NewCount: 1, Lines: []inspection.DiffLine{{Kind: "context", OldLine: 4, NewLine: 4, Text: "later"}}},
	}}
	viewer := NewDiffViewer(document)
	viewer.SetSize(80, 5)
	view := viewer.View()
	for _, want := range []string{"--- old", "+++ new", "@@ -1,1 +1,1 @@", "-   1", "?"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
	viewer.SetSize(80, 1)
	viewer.Update(tea.KeyPressMsg{Code: '}'})
	if viewer.offset != 5 {
		t.Fatal("next hunk did not move viewport")
	}
	viewer.Update(tea.KeyPressMsg{Code: '{'})
	if viewer.offset != 2 {
		t.Fatalf("previous hunk offset = %d, want 2", viewer.offset)
	}
}

func TestDiffViewerSideBySideWidthGate(t *testing.T) {
	document := inspection.DiffDocument{Kind: inspection.DiffText, OldLabel: "old", NewLabel: "new", Hunks: []inspection.DiffHunk{{Lines: []inspection.DiffLine{{Kind: "context", OldLine: 1, NewLine: 1, Text: "line"}}}}}
	viewer := NewDiffViewer(document)
	viewer.SetSize(sideBySideMinWidth-1, 10)
	viewer.Update(tea.KeyPressMsg{Code: 's'})
	if viewer.sideBySide {
		t.Fatal("side-by-side enabled below width threshold")
	}
	viewer.SetSize(sideBySideMinWidth, 10)
	viewer.Update(tea.KeyPressMsg{Code: 's'})
	if !viewer.sideBySide || !strings.Contains(viewer.View(), " | ") {
		t.Fatalf("side-by-side view = %q", viewer.View())
	}
}

func TestDiffViewerMetadataOnly(t *testing.T) {
	for _, kind := range []inspection.DiffKind{inspection.DiffSensitive, inspection.DiffBinary, inspection.DiffTooLarge} {
		t.Run(string(kind), func(t *testing.T) {
			viewer := NewDiffViewer(inspection.DiffDocument{Kind: kind, Metadata: []inspection.DiffFact{{Key: "reason", Value: "protected"}}, Hunks: []inspection.DiffHunk{{Lines: []inspection.DiffLine{{Text: "must not render"}}}}})
			view := viewer.View()
			if !strings.Contains(view, string(kind)+" diff") || !strings.Contains(view, "reason: protected") || strings.Contains(view, "must not render") {
				t.Fatalf("metadata view = %q", view)
			}
		})
	}
}
