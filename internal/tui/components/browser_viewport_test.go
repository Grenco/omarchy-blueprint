package components

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func assertBrowserFits(t *testing.T, browser Browser, width, height int) string {
	t.Helper()
	view := browser.View()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Fatalf("browser emitted %d lines for a %dx%d workspace:\n%s", len(lines), width, height, view)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("browser line is %d columns in a %dx%d workspace: %q", w, width, height, line)
		}
	}
	return ansi.Strip(view)
}

// crowdedTree is a directory with hundreds of sibling and child directories:
// home sits among 300 siblings and holds 300 children of its own.
func crowdedTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for i := range 300 {
		mustMkdir(t, filepath.Join(root, fmt.Sprintf("sibling-%03d", i)))
	}
	home := filepath.Join(root, "sibling-150")
	for i := range 300 {
		mustMkdir(t, filepath.Join(home, fmt.Sprintf("child-%03d", i)))
	}
	return home
}

func TestBrowserPanesStayWithinTheWorkspaceWithHundredsOfEntries(t *testing.T) {
	home := crowdedTree(t)
	for _, size := range []struct{ width, height int }{{94, 31}, {120, 40}, {100, 12}, {58, 15}, {40, 12}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			browser := NewBrowser(BrowseResource, BrowserConfig{Home: home})
			browser.SetSize(size.width, size.height)
			deliverBrowser(t, &browser, browser.Init())
			plain := assertBrowserFits(t, browser, size.width, size.height)
			if size.width >= 90 && !strings.Contains(plain, "sibling-150") {
				t.Fatalf("the parent column lost the current directory:\n%s", plain)
			}
			deliverBrowser(t, &browser, browser.Update(tea.KeyPressMsg{Code: 'G', Text: "G"}))
			plain = assertBrowserFits(t, browser, size.width, size.height)
			if !strings.Contains(plain, "child-299") {
				t.Fatalf("the selected last entry is not visible:\n%s", plain)
			}
			// Resizing keeps the view in bounds and the selection visible.
			browser.SetSize(size.width, size.height+7)
			if plain = assertBrowserFits(t, browser, size.width, size.height+7); !strings.Contains(plain, "child-299") {
				t.Fatalf("resize lost the selection:\n%s", plain)
			}
		})
	}
}

func TestBrowserBookmarksScrollWithTheSelection(t *testing.T) {
	browser := NewBrowser(BrowseResource, BrowserConfig{Home: t.TempDir()})
	browser.SetSize(94, 12)
	deliverBrowser(t, &browser, browser.Init())
	for i := range 200 {
		browser.bookmarks = append(browser.bookmarks, Bookmark{Label: fmt.Sprintf("Recent %03d", i), Path: "/tmp/" + strings.Repeat("deep/", 30) + fmt.Sprint(i)})
	}
	browser.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	assertBrowserFits(t, browser, 94, 12)
	for range len(browser.bookmarks) {
		browser.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	plain := assertBrowserFits(t, browser, 94, 12)
	if last := browser.bookmarks[len(browser.bookmarks)-1].Label; !strings.Contains(plain, last) {
		t.Fatalf("the selected last bookmark %q is not visible:\n%s", last, plain)
	}
	for range len(browser.bookmarks) {
		browser.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if plain = assertBrowserFits(t, browser, 94, 12); !strings.Contains(plain, browser.bookmarks[0].Label) {
		t.Fatalf("scrolling back up lost the first bookmark:\n%s", plain)
	}
}

// At / there is no parent to show: / is not read or listed as its own
// parent, and Current and Next take the space.
func TestBrowserAtTheRootShowsTwoColumnsAndStaysThere(t *testing.T) {
	browser := NewBrowser(BrowseResource, BrowserConfig{Home: "/usr"})
	browser.SetSize(120, 30)
	deliverBrowser(t, &browser, browser.Init())
	deliverBrowser(t, &browser, browser.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}))
	if browser.Path() != "/" {
		t.Fatalf("going up from /usr reached %q", browser.Path())
	}
	if browser.parentEntries != nil || browser.parentErr != nil {
		t.Fatalf("/ was read as its own parent: %d entries, err %v", len(browser.parentEntries), browser.parentErr)
	}
	plain := assertBrowserFits(t, browser, 120, 30)
	if strings.Contains(plain, "Parent") || !strings.Contains(plain, "Current") || !strings.Contains(plain, "Next") {
		t.Fatalf("root layout is not Current + Next:\n%s", plain)
	}
	if cmd := browser.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}); cmd != nil || browser.Path() != "/" {
		t.Fatalf("going up from / moved to %q", browser.Path())
	}
	// And back down and up again lands on / with the same layout.
	deliverBrowser(t, &browser, browser.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	deliverBrowser(t, &browser, browser.Update(tea.KeyPressMsg{Code: 'h', Text: "h"}))
	if plain = assertBrowserFits(t, browser, 120, 30); browser.Path() != "/" || strings.Contains(plain, "Parent") {
		t.Fatalf("returning to / is unstable: path %q\n%s", browser.Path(), plain)
	}
}
