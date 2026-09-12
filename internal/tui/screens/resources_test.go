package screens

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

func TestResourceScreenTrackedStatusLabels(t *testing.T) {
	screen := &Resources{items: []profile.Resource{
		{ID: "copy", Path: "~/Copy", Strategy: "copy"},
		{ID: "git", Path: "~/Git", Strategy: "git", Dirty: true},
		{ID: "git-diff", Path: "~/Diff", Strategy: "git+diff"},
	}, git: map[string]resourcesprovider.GitWorkingSummary{"git-diff": {StagedTracked: 1, UnstagedTracked: 2, Untracked: []string{"new.txt", "other.txt"}, SelectedUntracked: []string{"new.txt"}}}}
	view := screen.View()
	for _, want := range []string{"copy  ~/Copy  copy", "git  ~/Git  git (dirty: local changes are not captured)", "git-diff  ~/Diff  git+diff (staged:1 unstaged:2 untracked:2 selected:1)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestResourceScreenUntrackWarningPreservesLivePath(t *testing.T) {
	screen := &Resources{items: []profile.Resource{{ID: "projects", Path: "~/Projects"}}, confirm: "untrack"}
	if view := screen.View(); !strings.Contains(view, "live path remains untouched") {
		t.Fatalf("view=%q", view)
	}
}
