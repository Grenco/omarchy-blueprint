package components

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// BrowserReadDirMsg is the asynchronous result for one directory generation.
type BrowserReadDirMsg struct {
	Generation uint64
	Path       string
	Entries    []BrowserEntry
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
	list           Selectable
	cursors        map[string]int
	width, height  int
	styles         Styles
	filter         string
	filtering      bool
	bookmarksOpen  bool
	bookmark       int
	closed         bool
	readErr        error
	inspectErr     error
	inspectPathCmd func(requestID uint64, path string) tea.Cmd
	requestID      uint64
	directoryID    uint64
	inspection     workflow.PathInspection
	inspectionPath string
}

func NewBrowser(mode BrowserMode, config BrowserConfig) Browser {
	b := Browser{
		mode:           mode,
		path:           filepath.Clean(config.Home),
		bookmarks:      BuildBookmarks(config),
		inspectPathCmd: config.InspectPathCmd,
		cursors:        make(map[string]int),
	}
	return b
}

// SetSize bounds the current-directory viewport without reading beyond it.
func (b *Browser) SetSize(width, height int) { b.width, b.height = width, height }
func (b *Browser) SetStyles(styles Styles)   { b.styles = styles }

// BuildBookmarks returns the stable, existing locations useful to resource picking.
func BuildBookmarks(config BrowserConfig) []Bookmark {
	home := filepath.Clean(config.Home)
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

func (b *Browser) Init() tea.Cmd { return b.readDir() }

func (b *Browser) Update(msg tea.Msg) tea.Cmd {
	if result, ok := msg.(BrowserReadDirMsg); ok {
		if result.Generation != b.directoryID || result.Path != b.path {
			return nil
		}
		b.entries, b.readErr = result.Entries, result.Err
		b.selected = b.cursors[b.path]
		b.list.SetSelected(b.selected, len(b.filteredEntries()), b.listHeight())
		b.selected = b.list.Selected
		if result.Err == nil {
			b.addRecent(result.Path)
		}
		b.clearInspection()
		return b.inspectSelected()
	}
	if result, ok := msg.(BrowserInspectionMsg); ok {
		if result.RequestID == b.requestID {
			b.inspection, b.inspectErr, b.inspectionPath = result.Inspection, result.Err, ""
			if result.Err == nil {
				b.inspectionPath = result.Inspection.Path
			}
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok || b.closed {
		return nil
	}
	if b.bookmarksOpen {
		switch key.String() {
		case "esc":
			b.bookmarksOpen = false
		case "j", "down":
			b.bookmark = min(len(b.bookmarks)-1, b.bookmark+1)
		case "k", "up":
			b.bookmark = max(0, b.bookmark-1)
		case "enter", "l", "right":
			if b.bookmark < len(b.bookmarks) {
				b.path, b.filter, b.bookmarksOpen = b.bookmarks[b.bookmark].Path, "", false
				return b.readDir()
			}
		}
		return nil
	}
	switch key.String() {
	case "esc":
		if b.filtering {
			b.filtering, b.filter, b.selected = false, "", 0
			return b.inspectSelected()
		}
		if b.filter != "" {
			b.filter, b.selected = "", 0
			return b.inspectSelected()
		}
		b.closed = true
	case "backspace":
		if b.filtering {
			if len(b.filter) > 0 {
				b.filter = b.filter[:len(b.filter)-1]
				b.selected = 0
				return b.inspectSelected()
			}
			b.filtering = false
			return nil
		}
		return b.goParent()
	case "h", "left":
		return b.goParent()
	case "j", "down":
		if b.selected < len(b.filteredEntries())-1 {
			b.selected++
			b.cursors[b.path] = b.selected
			b.list.SetSelected(b.selected, len(b.filteredEntries()), b.listHeight())
			return b.inspectSelected()
		}
	case "k", "up":
		if b.selected > 0 {
			b.selected--
			b.cursors[b.path] = b.selected
			b.list.SetSelected(b.selected, len(b.filteredEntries()), b.listHeight())
			return b.inspectSelected()
		}
	case "enter", "l", "right":
		if entry, ok := b.selectedEntry(); ok && entry.Type == "directory" {
			b.cursors[b.path] = b.selected
			b.path, b.filter = entry.Path, ""
			return b.readDir()
		}
	case "g":
		b.bookmarksOpen = !b.bookmarksOpen
		b.bookmark = 0
	case "/":
		b.filtering = true
	default:
		if b.filtering && len(key.String()) == 1 {
			b.filter += key.String()
			b.selected = 0
			return b.inspectSelected()
		}
	}
	return nil
}

func (b Browser) View() string {
	lines := []string{"Parent: " + filepath.Dir(b.path), "Current: " + b.path}
	if b.readErr != nil {
		lines = append(lines, "Unable to read directory: "+b.readErr.Error())
	}
	if b.bookmarksOpen {
		lines = append(lines, "Bookmarks")
		for i, bookmark := range b.bookmarks {
			marker := " "
			if i == b.bookmark {
				marker = ">"
			}
			lines = append(lines, fmt.Sprintf("%s %s  %s", marker, bookmark.Label, bookmark.Path))
		}
		return strings.Join(lines, "\n")
	}
	if b.width == 0 {
		lines = append(lines, b.currentView())
	} else if b.width >= 90 {
		parent := b.parentView()
		current := b.currentView()
		preview := b.DetailView()
		pane := max(18, b.width/3-1)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(pane).MaxWidth(pane).Render(parent), " ",
			lipgloss.NewStyle().Width(pane).MaxWidth(pane).Render(current), " ",
			lipgloss.NewStyle().Width(pane).MaxWidth(pane).Render(preview)))
	} else {
		lines = append(lines, b.currentView(), b.DetailView())
	}
	if b.filtering || b.filter != "" {
		lines = append(lines, "Filter: "+b.filter)
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
func (b Browser) DetailView() string {
	if b.readErr != nil {
		return "Unable to read directory: " + b.readErr.Error()
	}
	if b.inspectErr != nil {
		return "Unable to inspect selection: " + b.inspectErr.Error()
	}
	if b.inspection.Path == "" {
		return "Loading selection details..."
	}
	return browserPreview(b.inspection)
}

// CanSelect reports whether the current selection may cause a mutation.
func (b Browser) CanSelect() bool {
	entry, ok := b.selectedEntry()
	if !ok || b.mode == BrowseReadOnly || b.inspectErr != nil || b.inspectionPath != entry.Path || b.inspection.BlockedReason != "" {
		return false
	}
	if b.mode == PickDirectory {
		return entry.Type == "directory"
	}
	return entry.Type == "directory" || entry.Type == "file"
}

func (b *Browser) readDir() tea.Cmd {
	b.clearInspection()
	b.directoryID++
	generation, path := b.directoryID, b.path
	return func() tea.Msg {
		entries, err := os.ReadDir(path)
		if err != nil {
			return BrowserReadDirMsg{Generation: generation, Path: path, Err: err}
		}
		result := make([]BrowserEntry, 0, len(entries))
		for _, entry := range entries {
			entryPath := filepath.Join(path, entry.Name())
			info, err := os.Lstat(entryPath)
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
			result = append(result, BrowserEntry{Name: entry.Name(), Path: entryPath, Type: kind, Hidden: strings.HasPrefix(entry.Name(), ".")})
		}
		sort.Slice(result, func(i, j int) bool {
			left, right := result[i], result[j]
			if (left.Type == "directory") != (right.Type == "directory") {
				return left.Type == "directory"
			}
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		})
		return BrowserReadDirMsg{Generation: generation, Path: path, Entries: result}
	}
}

func (b *Browser) goParent() tea.Cmd {
	parent := filepath.Dir(b.path)
	if parent == b.path {
		return nil
	}
	b.cursors[b.path] = b.selected
	b.path, b.filter = parent, ""
	return b.readDir()
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
	b.clearInspection()
	b.requestID++
	return b.inspectPathCmd(b.requestID, entry.Path)
}

func (b *Browser) clearInspection() {
	b.inspection = workflow.PathInspection{}
	b.inspectionPath = ""
	b.inspectErr = nil
}

func (b *Browser) addRecent(path string) {
	for _, bookmark := range b.bookmarks {
		if bookmark.Path == path {
			return
		}
	}
	b.bookmarks = append(b.bookmarks, Bookmark{Label: "Recent", Path: path})
}

func (b Browser) currentView() string {
	lines := []string{"Current"}
	entries := b.filteredEntries()
	b.list.SetSelected(b.selected, len(entries), b.listHeight())
	start := b.list.offset
	end := min(len(entries), start+b.listHeight())
	for i := start; i < end; i++ {
		entry := entries[i]
		line := fmt.Sprintf("  %s %s", browserIcon(entry.Type), entry.Name)
		if i == b.selected {
			if !b.styles.Palette.ColorEnabled {
				line = Icons.Selected + line[1:]
			}
			line = b.styles.Selection(line, true)
		}
		lines = append(lines, line)
	}
	if len(entries) == 0 {
		lines = append(lines, "  (empty)")
	}
	return strings.Join(lines, "\n")
}

func (b Browser) parentView() string {
	parent := filepath.Dir(b.path)
	lines := []string{"Parent"}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return strings.Join(append(lines, "  "+err.Error()), "\n")
	}
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Join(parent, name) == b.path {
			lines = append(lines, Icons.Selected+" "+name)
		}
	}
	if len(lines) == 1 {
		lines = append(lines, "  /")
	}
	return strings.Join(lines, "\n")
}

func (b Browser) listHeight() int {
	if b.height == 0 {
		return len(b.filteredEntries()) + 1
	}
	return max(1, b.height-6)
}

func browserIcon(kind string) string {
	switch kind {
	case "directory":
		return "[D]"
	case "file":
		return "[F]"
	case "symlink":
		return "[@]"
	default:
		return "[?]"
	}
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
