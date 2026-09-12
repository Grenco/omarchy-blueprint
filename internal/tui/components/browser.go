package components

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type BrowserMode string

const (
	BrowseResource BrowserMode = "resource"
	PickDirectory  BrowserMode = "directory"
	BrowseReadOnly BrowserMode = "read-only"
)

type BrowserEntry struct {
	Name, Path, Type string
	Hidden           bool
}

type Bookmark struct{ Label, Path string }

// BrowserInspectionMsg is delivered by the inspection command for a selection.
type BrowserInspectionMsg struct {
	RequestID  uint64
	Inspection workflow.PathInspection
	Err        error
}

type BrowserConfig struct {
	Home           string
	ProfileDir     string
	Profile        profile.Data
	EffectiveRoots map[string]string
	RecentDirs     []string
	InspectPathCmd func(requestID uint64, path string) tea.Cmd
}

type Browser struct {
	mode           BrowserMode
	path           string
	entries        []BrowserEntry
	bookmarks      []Bookmark
	selected       int
	filter         string
	closed         bool
	err            error
	inspectPathCmd func(requestID uint64, path string) tea.Cmd
	requestID      uint64
	inspection     workflow.PathInspection
}

func NewBrowser(mode BrowserMode, config BrowserConfig) Browser {
	home := config.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	b := Browser{
		mode:           mode,
		path:           filepath.Clean(home),
		bookmarks:      BuildBookmarks(config),
		inspectPathCmd: config.InspectPathCmd,
	}
	b.readDir()
	return b
}

// BuildBookmarks returns the stable, existing locations useful to resource picking.
func BuildBookmarks(config BrowserConfig) []Bookmark {
	home := filepath.Clean(config.Home)
	if home == "." || home == "" {
		home, _ = os.UserHomeDir()
	}
	candidates := []Bookmark{{"Home", home}, {"Config", filepath.Join(home, ".config")}}
	if config.ProfileDir != "" {
		candidates = append(candidates, Bookmark{"Profile", config.ProfileDir})
	}
	resources := append([]profile.Resource(nil), config.Profile.Resources.Items...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	for _, resource := range resources {
		if path := expandHome(home, resource.Path); path != "" {
			candidates = append(candidates, Bookmark{"Resource: " + resource.ID, path})
		}
	}
	ids := make([]string, 0, len(config.EffectiveRoots))
	for id := range config.EffectiveRoots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		candidates = append(candidates, Bookmark{"Effective: " + id, config.EffectiveRoots[id]})
	}
	for _, name := range []string{"Projects", "Code"} {
		candidates = append(candidates, Bookmark{name, filepath.Join(home, name)})
	}
	for _, path := range []string{"/mnt", "/media", "/run/media"} {
		candidates = append(candidates, Bookmark{filepath.Base(path), path})
	}
	for _, path := range config.RecentDirs {
		candidates = append(candidates, Bookmark{"Recent", path})
	}
	seen := map[string]bool{}
	bookmarks := make([]Bookmark, 0, len(candidates))
	for _, candidate := range candidates {
		path, err := canonicalDir(candidate.Path)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		candidate.Path = path
		bookmarks = append(bookmarks, candidate)
	}
	return bookmarks
}

func (b *Browser) Init() tea.Cmd { return b.inspectSelected() }

func (b *Browser) Update(msg tea.Msg) tea.Cmd {
	if result, ok := msg.(BrowserInspectionMsg); ok {
		if result.RequestID == b.requestID {
			b.inspection, b.err = result.Inspection, result.Err
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || b.closed {
		return nil
	}
	switch key.String() {
	case "esc":
		if b.filter != "" {
			b.filter, b.selected = "", 0
			return b.inspectSelected()
		}
		b.closed = true
	case "backspace", "h", "left":
		return b.goParent()
	case "j", "down":
		if b.selected < len(b.filteredEntries())-1 {
			b.selected++
			return b.inspectSelected()
		}
	case "k", "up":
		if b.selected > 0 {
			b.selected--
			return b.inspectSelected()
		}
	case "enter", "l", "right":
		if entry, ok := b.selectedEntry(); ok && entry.Type == "directory" {
			b.path, b.selected, b.filter = entry.Path, 0, ""
			b.readDir()
			return b.inspectSelected()
		}
	default:
		if len(key.String()) == 1 {
			b.filter += key.String()
			b.selected = 0
			return b.inspectSelected()
		}
	}
	return nil
}

func (b Browser) View() string {
	lines := []string{b.path}
	if b.err != nil {
		lines = append(lines, "Unable to inspect: "+b.err.Error())
	}
	for i, entry := range b.filteredEntries() {
		prefix := " "
		if i == b.selected {
			prefix = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, entry.Name, entry.Type))
	}
	if b.filter != "" {
		lines = append(lines, "Filter: "+b.filter)
	}
	if b.inspection.Path != "" {
		lines = append(lines, browserPreview(b.inspection))
	}
	return strings.Join(lines, "\n")
}

func (b Browser) Entries() []BrowserEntry { return append([]BrowserEntry(nil), b.filteredEntries()...) }
func (b Browser) Bookmarks() []Bookmark   { return append([]Bookmark(nil), b.bookmarks...) }
func (b Browser) Path() string            { return b.path }
func (b Browser) Closed() bool            { return b.closed }
func (b Browser) Selected() (BrowserEntry, bool) {
	return b.selectedEntry()
}
func (b Browser) Inspection() workflow.PathInspection { return b.inspection }

// CanSelect reports whether the current selection may cause a mutation.
func (b Browser) CanSelect() bool {
	entry, ok := b.selectedEntry()
	if !ok || b.mode == BrowseReadOnly || b.inspection.BlockedReason != "" {
		return false
	}
	if b.mode == PickDirectory {
		return entry.Type == "directory"
	}
	return entry.Type == "directory" || entry.Type == "file"
}

func (b *Browser) readDir() {
	entries, err := os.ReadDir(b.path)
	b.entries, b.err = nil, err
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(b.path, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		kind := "special"
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		} else if info.IsDir() {
			kind = "directory"
		} else if info.Mode().IsRegular() {
			kind = "file"
		}
		b.entries = append(b.entries, BrowserEntry{Name: entry.Name(), Path: path, Type: kind, Hidden: strings.HasPrefix(entry.Name(), ".")})
	}
	sort.Slice(b.entries, func(i, j int) bool {
		left, right := b.entries[i], b.entries[j]
		if (left.Type == "directory") != (right.Type == "directory") {
			return left.Type == "directory"
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
}

func (b *Browser) goParent() tea.Cmd {
	parent := filepath.Dir(b.path)
	if parent == b.path {
		return nil
	}
	b.path, b.selected, b.filter = parent, 0, ""
	b.readDir()
	return b.inspectSelected()
}

func (b Browser) filteredEntries() []BrowserEntry {
	if b.filter == "" {
		return b.entries
	}
	entries := make([]BrowserEntry, 0, len(b.entries))
	for _, entry := range b.entries {
		if strings.Contains(strings.ToLower(entry.Name), strings.ToLower(b.filter)) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (b Browser) selectedEntry() (BrowserEntry, bool) {
	entries := b.filteredEntries()
	if b.selected < 0 || b.selected >= len(entries) {
		return BrowserEntry{}, false
	}
	return entries[b.selected], true
}

func (b *Browser) inspectSelected() tea.Cmd {
	entry, ok := b.selectedEntry()
	if !ok || b.inspectPathCmd == nil {
		return nil
	}
	b.requestID++
	return b.inspectPathCmd(b.requestID, entry.Path)
}

func browserPreview(inspection workflow.PathInspection) string {
	lines := []string{"Preview: " + inspection.Path}
	if inspection.OwnershipProvider != "" {
		lines = append(lines, "Owner: "+inspection.OwnershipProvider)
	}
	if inspection.ResourceID != "" {
		lines = append(lines, "Resource: "+inspection.ResourceID)
	}
	if inspection.Git != nil {
		lines = append(lines, fmt.Sprintf("Git: %s (%s) staged:%d unstaged:%d untracked:%d", inspection.Git.Branch, inspection.Git.Revision, inspection.Git.Staged, inspection.Git.Unstaged, inspection.Git.Untracked))
	}
	if inspection.SuggestedStrategy != "" {
		lines = append(lines, "Suggested: "+inspection.SuggestedStrategy+" - "+inspection.StrategyReason)
	}
	if inspection.BlockedReason != "" {
		lines = append(lines, "Blocked: "+inspection.BlockedReason)
	}
	return strings.Join(lines, "\n")
}

func canonicalDir(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("not an existing directory: %s", path)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path, nil
}

func expandHome(home, path string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(path, "~/")))
	}
	return path
}
