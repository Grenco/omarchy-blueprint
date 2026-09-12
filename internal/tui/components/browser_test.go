package components

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestBrowserNavigationAndListing(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, "adir"))
	mustMkdir(t, filepath.Join(home, "adir", "nested"))
	mustWrite(t, filepath.Join(home, ".hidden"))
	mustWrite(t, filepath.Join(home, "zfile"))
	if err := os.Symlink(filepath.Join(home, "adir"), filepath.Join(home, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	browser := NewBrowser(BrowseResource, BrowserConfig{Home: home})
	if browser.Path() != home {
		t.Fatalf("start path = %q, want home %q", browser.Path(), home)
	}
	if got := entryNames(browser.Entries()); !reflect.DeepEqual(got, []string{"adir", ".hidden", "linked-dir", "zfile"}) {
		t.Fatalf("entries = %#v", got)
	}
	entry, _ := browser.Selected()
	if entry.Type != "directory" {
		t.Fatalf("first entry type = %q", entry.Type)
	}
	browser.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if browser.Path() != filepath.Join(home, "adir") {
		t.Fatalf("entered path = %q", browser.Path())
	}
	browser.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if browser.Path() != home {
		t.Fatalf("parent path = %q", browser.Path())
	}
	for browser.Path() != string(filepath.Separator) {
		browser.Update(tea.KeyPressMsg{Code: 'h'})
	}
	browser.Update(tea.KeyPressMsg{Code: 'h'})
	if browser.Path() != string(filepath.Separator) {
		t.Fatalf("root parent = %q", browser.Path())
	}

	browser = NewBrowser(BrowseResource, BrowserConfig{Home: home})
	browser.Update(tea.KeyPressMsg{Code: 'z'})
	if got := entryNames(browser.Entries()); !reflect.DeepEqual(got, []string{"zfile"}) {
		t.Fatalf("filtered entries = %#v", got)
	}
	browser.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if browser.Closed() || browser.filter != "" {
		t.Fatal("first escape must clear filter")
	}
	browser.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !browser.Closed() {
		t.Fatal("second escape did not close browser")
	}
}

func TestBuildBookmarksDeterministicAndCanonical(t *testing.T) {
	home := t.TempDir()
	config, profileDir := filepath.Join(home, ".config"), filepath.Join(home, "profile")
	projects, code, tracked := filepath.Join(home, "Projects"), filepath.Join(home, "Code"), filepath.Join(home, "tracked")
	for _, path := range []string{config, profileDir, projects, code, tracked} {
		mustMkdir(t, path)
	}
	if err := os.Symlink(tracked, filepath.Join(home, "effective")); err != nil {
		t.Fatal(err)
	}
	configInput := BrowserConfig{
		Home:           home,
		ProfileDir:     profileDir,
		Profile:        profile.Data{Resources: profile.Resources{Items: []profile.Resource{{ID: "z", Path: "~/tracked"}, {ID: "a", Path: "~/Code"}}}},
		EffectiveRoots: map[string]string{"z": filepath.Join(home, "effective"), "a": code},
		RecentDirs:     []string{projects, filepath.Join(home, "effective")},
	}
	bookmarks := BuildBookmarks(configInput)
	paths := make([]string, len(bookmarks))
	for i, bookmark := range bookmarks {
		paths[i] = bookmark.Path
	}
	for _, want := range []string{home, config, profileDir, tracked, code, projects} {
		if !contains(paths, want) {
			t.Errorf("bookmarks missing %q: %#v", want, bookmarks)
		}
	}
	if count(paths, tracked) != 1 || count(paths, code) != 1 || count(paths, projects) != 1 {
		t.Fatalf("bookmarks were not canonically deduplicated: %#v", bookmarks)
	}
	if !reflect.DeepEqual(bookmarks, BuildBookmarks(configInput)) {
		t.Fatal("bookmarks are not deterministic")
	}
}

func TestBrowserIgnoresStalePreview(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "a"))
	mustWrite(t, filepath.Join(home, "b"))
	var requests []uint64
	browser := NewBrowser(BrowseResource, BrowserConfig{Home: home, InspectPathCmd: func(id uint64, _ string) tea.Cmd {
		requests = append(requests, id)
		return nil
	}})
	browser.Init()
	browser.Update(tea.KeyPressMsg{Code: 'j'})
	if !reflect.DeepEqual(requests, []uint64{1, 2}) {
		t.Fatalf("requests = %#v", requests)
	}
	browser.Update(BrowserInspectionMsg{RequestID: 1, Inspection: workflow.PathInspection{Path: "stale"}})
	if browser.inspection.Path != "" {
		t.Fatalf("stale inspection applied: %#v", browser.inspection)
	}
	browser.Update(BrowserInspectionMsg{RequestID: 2, Inspection: workflow.PathInspection{Path: "current", OwnershipProvider: "resources", SuggestedStrategy: "copy"}})
	if !strings.Contains(browser.View(), "Preview: current") || !strings.Contains(browser.View(), "Owner: resources") {
		t.Fatalf("current inspection missing from preview: %s", browser.View())
	}
}

func TestBrowserSelectionModes(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, "directory"))
	mustWrite(t, filepath.Join(home, "file"))
	for _, test := range []struct {
		mode      BrowserMode
		moveDown  bool
		canSelect bool
		blocked   bool
	}{
		{mode: PickDirectory, canSelect: true},
		{mode: PickDirectory, moveDown: true, canSelect: false},
		{mode: BrowseResource, moveDown: true, canSelect: true},
		{mode: BrowseResource, canSelect: false, blocked: true},
		{mode: BrowseReadOnly, canSelect: false},
	} {
		browser := NewBrowser(test.mode, BrowserConfig{Home: home})
		if test.moveDown {
			browser.Update(tea.KeyPressMsg{Code: 'j'})
		}
		if test.blocked {
			browser.inspection.BlockedReason = "owned"
		}
		if browser.CanSelect() != test.canSelect {
			t.Errorf("mode=%s moveDown=%t blocked=%t CanSelect=%t", test.mode, test.moveDown, test.blocked, browser.CanSelect())
		}
	}
}

func entryNames(entries []BrowserEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}
	return names
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, want string) bool { return count(values, want) > 0 }
func count(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
