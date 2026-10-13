package screens

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/charmbracelet/x/ansi"
)

func busySyncStatus(changes int) profilegit.Status {
	status := profilegit.Status{Repository: true, Branch: "main", Head: "0123456789abcdef", Upstream: "origin/main", Origin: "git@github.com:someone/" + strings.Repeat("very-long-repository-name-", 6) + ".git", Ahead: 2, Behind: 1}
	for i := range changes {
		status.Changes = append(status.Changes, profilegit.Change{Path: fmt.Sprintf("c/%03d.toml", i), Worktree: "M", Managed: i%3 != 0})
	}
	return status
}

func TestSyncViewFitsKeepsTheSelectionAndNeverDropsItsWarnings(t *testing.T) {
	for _, size := range workspaceSizes {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			screen := &Sync{width: size.width, height: size.height, status: busySyncStatus(40), err: errors.New("fetch failed")}
			for range 40 {
				screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			view := screen.View()
			assertFits(t, "Sync", view, size.width, size.height)
			plain := ansi.Strip(view)
			if !strings.Contains(plain, "c/039.toml") {
				t.Fatalf("the selected last change scrolled out of view:\n%s", plain)
			}
			if !strings.Contains(strings.Join(strings.Fields(plain), " "), "unmanaged working-tree changes will not be included") {
				t.Fatalf("the unmanaged-changes warning was dropped:\n%s", plain)
			}
		})
	}
}

func TestSyncShowsTheFullRepositorySummaryWhenItFits(t *testing.T) {
	screen := &Sync{width: 94, height: 31, status: busySyncStatus(3)}
	if view := ansi.Strip(screen.View()); !strings.Contains(view, "Remote (last fetched)") || !strings.Contains(view, "  Upstream  origin/main") {
		t.Fatalf("a tall workspace lost the full summary:\n%s", view)
	}
	screen.height = 9
	if view := ansi.Strip(screen.View()); !strings.Contains(view, "Branch main · HEAD") {
		t.Fatalf("a short workspace did not use the compact summary:\n%s", view)
	}
}

func TestSyncDiffFitsBeneathItsTitle(t *testing.T) {
	for _, size := range workspaceSizes {
		screen := &Sync{width: size.width, height: size.height}
		screen.requestID = 1
		screen.Update(syncDiffMsg{requestID: 1, diff: profilegit.Diff{Files: []profilegit.DiffFile{{Document: longDiff(200)}}}})
		assertFits(t, "Sync diff", screen.View(), size.width, size.height)
		screen.SetSize(size.width, size.height)
		assertFits(t, "Sync diff after resize", screen.View(), size.width, size.height)
	}
}
