package screens

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type resourcePhase string

const (
	resourceBrowse       resourcePhase = "browse"
	resourceStrategy     resourcePhase = "strategy"
	resourceUntracked    resourcePhase = "untracked-selector"
	resourceConfirm      resourcePhase = "confirm"
	resourcePending      resourcePhase = "pending"
	resourceResult       resourcePhase = "result"
	resourceInspectError resourcePhase = "inspect-error"
)

type Resources struct {
	ctx                     context.Context
	session                 *workflow.Session
	width, height, selected int
	discover                bool
	items                   []profile.Resource
	git                     map[string]resourcesprovider.GitWorkingSummary
	effective               map[string]string
	browser                 *components.Browser
	phase                   resourcePhase
	confirmFrom             resourcePhase
	strategy                string
	strategyCursor          int
	untrackedCursor         int
	editingResourceID       string
	candidate               components.BrowserEntry
	candidateInspection     workflow.PathInspection
	untracked               []string
	eligibleUntracked       map[string]bool
	chosen                  map[string]bool
	confirm                 string
	focusID                 string
	list                    components.Selectable
	table                   components.Table
	styles                  components.Styles
	err                     error
	requestID               uint64
}

// ResourceAction describes an action currently meaningful for the resources view.
// The TUI root adapts this screen-local contract to its shared action primitive.
type ResourceAction struct {
	ID, Label, Shortcut, DisabledReason string
	Enabled                             bool
}

type resourcesStatusMsg struct {
	requestID uint64
	items     []profile.Resource
	git       map[string]resourcesprovider.GitWorkingSummary
	effective map[string]string
	err       error
}
type resourceTrackedMsg struct {
	requestID uint64
	err       error
}
type resourceUntrackedMsg struct {
	requestID uint64
	err       error
}
type resourceExistingInspectMsg struct {
	requestID  uint64
	item       profile.Resource
	inspection workflow.ResourceInspection
	path       workflow.PathInspection
	err        error
}

func NewResources(session *workflow.Session) *Resources {
	return NewResourcesContext(context.Background(), session)
}
func NewResourcesContext(ctx context.Context, session *workflow.Session) *Resources {
	return &Resources{ctx: ctx, session: session, phase: resourceBrowse}
}
func (s *Resources) Focus(id string) tea.Cmd { s.focusID = id; return s.rescan() }
func (s *Resources) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.browser != nil {
		s.browser.SetSize(width, height)
	}
}
func (s *Resources) SetStyles(styles components.Styles) {
	s.styles = styles
	if s.browser != nil {
		s.browser.SetStyles(styles)
	}
}
func (s *Resources) Init() tea.Cmd { return s.rescan() }
func (s *Resources) TransientActive() bool {
	return s.browser != nil || s.phase != resourceBrowse || s.confirm != ""
}
func (s *Resources) Actions() []ResourceAction {
	if s.phase == resourceUntracked {
		return []ResourceAction{
			{ID: "select", Label: "Toggle untracked file", Shortcut: "space", Enabled: len(s.untracked) > 0},
			{ID: "select-all", Label: "Select all untracked files", Shortcut: "a", Enabled: len(s.untracked) > 0},
			{ID: "continue", Label: "Continue tracking", Shortcut: "enter", Enabled: true},
		}
	}
	if s.phase != resourceBrowse || s.browser != nil {
		return nil
	}
	if s.discover {
		return nil
	}
	item := s.selectedResource()
	actions := []ResourceAction{}
	if item.ID == "" {
		return actions
	}
	return append(actions,
		ResourceAction{ID: "strategy", Label: "Change strategy", Shortcut: "s", Enabled: true},
		ResourceAction{ID: "untrack", Label: "Untrack resource", Shortcut: "u", Enabled: true},
		ResourceAction{ID: "edit", Label: "Edit resource", Shortcut: "e", Enabled: true},
		ResourceAction{ID: "open", Label: "Open resource", Shortcut: "o", Enabled: true},
		ResourceAction{ID: "copy", Label: "Copy effective path", Shortcut: "y", Enabled: true},
	)
}

func (s *Resources) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case components.BrowserInspectionMsg, components.BrowserReadDirMsg, components.BrowserChildReadDirMsg:
		if s.browser != nil {
			return s.browser.Update(msg)
		}
		return nil
	case resourcesStatusMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.items, s.git, s.effective = msg.items, msg.git, msg.effective
		if msg.err != nil {
			s.err = msg.err
		}
		for i, item := range s.items {
			if item.ID == s.focusID {
				s.selected = i
			}
		}
		s.focusID = ""
		s.selected = min(s.selected, max(0, len(s.items)-1))
		s.list.SetSelected(s.selected, len(s.items), s.listHeight())
		s.table.Ensure(s.selected, len(s.items), s.listHeight()-1)
		return nil
	case resourceTrackedMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.err = msg.err
		if msg.err != nil {
			s.phase = resourceResult
			return nil
		}
		s.focusID = s.editingResourceID
		s.resetDiscovery()
		return s.rescan()
	case resourceUntrackedMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.err, s.confirm = msg.err, ""
		if msg.err != nil {
			return nil
		}
		s.phase = resourceBrowse
		return s.rescan()
	case resourceExistingInspectMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		if msg.err != nil {
			s.err, s.phase = msg.err, resourceInspectError
			return nil
		}
		s.candidate = components.BrowserEntry{Path: msg.inspection.EffectivePath, Type: msg.inspection.Resource.Kind}
		s.candidateInspection = msg.path
		if s.candidateInspection.Git == nil && msg.inspection.Git != nil {
			s.candidateInspection.Git = &workflow.GitInspection{Untracked: len(msg.inspection.Git.Untracked), UntrackedPaths: append([]string(nil), msg.inspection.Git.Untracked...)}
		}
		s.editingResourceID = msg.item.ID
		s.strategy, s.phase = msg.item.Strategy, resourceStrategy
		if msg.path.Git != nil {
			s.untracked = append([]string(nil), msg.path.Git.UntrackedPaths...)
		}
		s.chosen = make(map[string]bool, len(msg.item.Untracked))
		for _, file := range msg.item.Untracked {
			s.chosen[file.Path] = true
		}
		s.setUntracked(msg.item.Untracked)
		s.selectStrategy(msg.item.Strategy)
		s.untrackedCursor = 0
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.confirm != "" {
		switch key.String() {
		case "esc":
			s.confirm, s.phase = "", s.confirmFrom
		case "enter":
			s.confirm = ""
			if s.phase == resourceConfirm {
				s.phase = resourcePending
				return s.track()
			}
			return s.untrack()
		}
		return nil
	}
	if s.phase == resourcePending {
		return nil
	}
	if key.String() == "tab" && s.discover {
		s.resetDiscovery()
		return nil
	}
	if s.browser != nil && s.phase == resourceBrowse {
		if key.String() == "enter" && s.phase == resourceBrowse && s.browser.CanSelect() {
			s.candidate, _ = s.browser.Selected()
			s.candidateInspection = clonePathInspection(s.browser.Inspection())
			s.browser = nil
			s.setCandidateInspection()
			s.phase = resourceStrategy
			return nil
		}
		cmd := s.browser.Update(msg)
		if s.browser.Closed() {
			// Esc exits Discover directly; it must never leave an empty tab behind.
			s.resetDiscovery()
		}
		return cmd
	}
	if s.phase == resourceBrowse && !s.discover && s.list.Vim(key.String(), len(s.items), s.listHeight()) {
		s.selected = s.list.Selected
		s.table.Ensure(s.selected, len(s.items), s.listHeight()-1)
		return nil
	}
	if s.phase == resourceStrategy {
		switch key.String() {
		case "esc":
			s.resetDiscovery()
		case "j", "down":
			s.strategyCursor = min(len(s.strategies())-1, s.strategyCursor+1)
			s.strategy = s.strategies()[s.strategyCursor]
		case "k", "up":
			s.strategyCursor = max(0, s.strategyCursor-1)
			s.strategy = s.strategies()[s.strategyCursor]
		case "enter":
			if s.strategy == "git+diff" && len(s.untracked) > 0 {
				s.phase = resourceUntracked
			} else if s.strategy != "" {
				s.phase = resourcePending
				return s.track()
			}
		}
		return nil
	}
	if s.phase == resourceUntracked {
		switch key.String() {
		case "esc":
			s.phase = resourceStrategy
		case "j", "down":
			s.untrackedCursor = min(len(s.untracked)-1, s.untrackedCursor+1)
		case "k", "up":
			s.untrackedCursor = max(0, s.untrackedCursor-1)
		case "space", " ":
			if len(s.untracked) > 0 {
				path := s.untracked[s.untrackedCursor]
				s.chosen[path] = !s.chosen[path]
			}
		case "a":
			for _, path := range s.untracked {
				s.chosen[path] = true
			}
		case "enter":
			s.phase = resourcePending
			return s.track()
		}
		return nil
	}
	if s.phase == resourceResult || s.phase == resourceInspectError {
		if key.String() == "esc" {
			s.resetDiscovery()
		}
		return nil
	}
	switch key.String() {
	case "tab":
		if !s.discover {
			s.discover, s.err = true, nil
			if s.session == nil {
				return nil
			}
			browser := components.NewBrowser(components.BrowseResource, s.browserConfig())
			browser.SetSize(s.width, s.height)
			browser.SetStyles(s.styles)
			s.browser = &browser
			return s.browser.Init()
		}
		s.resetDiscovery()
	case "j", "down":
		s.list.Move(1, len(s.items), s.listHeight())
		s.selected = s.list.Selected
		s.table.Ensure(s.selected, len(s.items), s.listHeight()-1)
	case "k", "up":
		s.list.Move(-1, len(s.items), s.listHeight())
		s.selected = s.list.Selected
		s.table.Ensure(s.selected, len(s.items), s.listHeight()-1)
	case "u", "x":
		if !s.discover && s.selectedResource().ID != "" {
			s.confirmFrom, s.confirm = s.phase, "untrack"
			id := s.selectedResource().ID
			return func() tea.Msg {
				return components.ModalRequest{Title: "Confirm untrack", Content: components.Confirm("Untrack " + components.DisplayText(id) + "? Its live path remains untouched.")}
			}
		}
	case "e", "o", "y":
		if item := s.selectedResource(); item.ID != "" {
			if path := s.effective[item.ID]; path != "" {
				return func() tea.Msg {
					return HandoffRequest{Source: "resources", Kind: map[string]string{"e": "editor", "o": "open", "y": "copy"}[key.String()], Path: path, IsDir: item.Kind == "directory", Refresh: "resources"}
				}
			}
		}
	case "s":
		if item := s.selectedResource(); item.ID != "" {
			return s.inspectExisting(item)
		}
	}
	return nil
}

func (s *Resources) View() string {
	phase := s.phase
	if phase == resourceConfirm {
		phase = s.confirmFrom
	}
	if s.phase == resourcePending {
		return "Tracking resource..."
	}
	if s.phase == resourceResult {
		if s.err != nil {
			return "Resource tracking failed: " + components.DisplayText(s.err.Error())
		}
		return "Resource tracked. Press Esc to continue."
	}
	if s.phase == resourceInspectError {
		return "Unable to inspect resource: " + components.DisplayText(s.err.Error())
	}
	if s.browser != nil && phase == resourceBrowse {
		return s.browser.View()
	}
	if phase == resourceStrategy {
		lines := []string{"Choose tracking strategy"}
		for i, strategy := range s.strategies() {
			line := "  " + strategy + ": " + strategyDescription(strategy)
			if i == s.strategyCursor {
				line = components.Icons.Selected + line[1:]
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	if phase == resourceUntracked {
		lines := []string{"Select eligible untracked files"}
		for i, path := range s.untracked {
			selected := " "
			if s.chosen[path] {
				selected = "x"
			}
			label := components.DisplayText(path)
			if s.eligibleUntracked != nil && !s.eligibleUntracked[path] {
				label += "  ignored now"
			}
			line := fmt.Sprintf("  [%s] %s", selected, label)
			if i == s.untrackedCursor {
				if !s.styles.Palette.ColorEnabled {
					line = components.Icons.Selected + line[1:]
				}
				line = s.styles.Selection(line, true)
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	tracked, discover := "Tracked "+fmt.Sprint(len(s.items)), "Discover"
	active := tracked
	if s.discover {
		active = discover
	}
	lines := []string{components.TabBar([]string{tracked, discover}, active, s.styles)}
	if s.err != nil {
		lines = append(lines, "Last action failed: "+components.DisplayText(s.err.Error()))
	}
	if s.discover {
		return strings.Join(lines, "\n")
	}
	rows := make([]components.Row, 0, len(s.items))
	for i, item := range s.items {
		rows = append(rows, components.Row{Cells: []string{components.DisplayText(item.ID), components.DisplayText(item.Strategy), components.DisplayText(item.Path), components.DisplayText(s.effective[item.ID]), components.DisplayText(s.resourceState(item))}, Selected: i == s.selected, Focused: true})
	}
	if len(s.items) == 0 {
		rows = append(rows, components.Row{Cells: []string{"No tracked resources."}})
	}
	width := s.width
	if width == 0 {
		width = 120
	}
	lines = append(lines, s.table.Render([]components.Column{{Title: "Resource", MinWidth: 10}, {Title: "Strategy", MinWidth: 8}, {Title: "Portable path", MinWidth: 16}, {Title: "Effective path", MinWidth: 16}, {Title: "State", MinWidth: 9}}, rows, width, s.listHeight(), s.styles))
	return strings.Join(lines, "\n")
}

func (s *Resources) DetailView() string {
	if s.browser != nil {
		return s.browser.DetailView()
	}
	if s.phase == resourceStrategy || s.phase == resourceUntracked {
		return resourceDetail(s.candidate.Path, s.candidateInspection)
	}
	if item := s.selectedResource(); item.ID != "" {
		lines := []string{"Resource: " + components.DisplayText(item.ID), "Portable path: " + components.DisplayText(item.Path), "Effective path: " + components.DisplayText(s.effective[item.ID]), "Kind: " + components.DisplayText(item.Kind), "Strategy: " + components.DisplayText(item.Strategy), "Hash: " + components.DisplayText(item.Hash), "Mode: " + components.DisplayText(item.Mode), "Remote: " + components.DisplayText(item.Remote), "Branch: " + components.DisplayText(item.Branch), "Revision: " + components.DisplayText(item.Revision)}
		if item.Strategy == "git" || item.Strategy == "git+diff" {
			state := s.git[item.ID]
			lines = append(lines, fmt.Sprintf("Staged: %d", state.StagedTracked), fmt.Sprintf("Unstaged: %d", state.UnstagedTracked), "Selected untracked: "+components.DisplayText(strings.Join(state.SelectedUntracked, ", ")), "Other untracked: "+components.DisplayText(strings.Join(otherUntracked(state), ", ")))
		}
		return strings.Join(lines, "\n")
	}
	return "Resources details"
}
func resourceDetail(path string, inspection workflow.PathInspection) string {
	lines := []string{"Candidate: " + components.DisplayText(path)}
	if inspection.Git != nil {
		lines = append(lines, fmt.Sprintf("Git: %s (%s), staged:%d unstaged:%d untracked:%d", components.DisplayText(inspection.Git.Branch), components.DisplayText(inspection.Git.Revision), inspection.Git.Staged, inspection.Git.Unstaged, inspection.Git.Untracked))
	}
	if inspection.SuggestedStrategy != "" {
		lines = append(lines, "Suggested: "+components.DisplayText(inspection.SuggestedStrategy)+" - "+components.DisplayText(inspection.StrategyReason))
	}
	if inspection.BlockedReason != "" {
		lines = append(lines, "Blocked: "+components.DisplayText(inspection.BlockedReason))
	}
	return strings.Join(lines, "\n")
}
func (s *Resources) selectedResource() profile.Resource {
	if s.selected >= 0 && s.selected < len(s.items) {
		return s.items[s.selected]
	}
	return profile.Resource{}
}
func (s *Resources) inspectPath(requestID uint64, path string) tea.Cmd {
	return func() tea.Msg {
		inspection, err := s.session.InspectPath(s.ctx, path)
		return components.BrowserInspectionMsg{RequestID: requestID, Inspection: inspection, Err: err}
	}
}
func (s *Resources) track() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	request := s.trackRequest()
	return func() tea.Msg {
		_, _, err := s.session.TrackResource(s.ctx, request)
		return resourceTrackedMsg{requestID: requestID, err: err}
	}
}
func (s *Resources) trackRequest() workflow.TrackRequest {
	request := workflow.TrackRequest{Path: s.candidate.Path, Strategy: s.strategy}
	if s.editingResourceID != "" {
		request.ID = s.editingResourceID
	}
	if s.strategy != "git+diff" {
		return request
	}
	for _, path := range s.untracked {
		if s.chosen[path] {
			request.IncludeUntracked = append(request.IncludeUntracked, path)
		} else {
			request.ExcludeUntracked = append(request.ExcludeUntracked, path)
		}
	}
	return request
}
func (s *Resources) inspectExisting(item profile.Resource) tea.Cmd {
	s.requestID++
	requestID := s.requestID
	return func() tea.Msg {
		inspection, err := s.session.InspectResource(s.ctx, item.ID)
		if err != nil {
			return resourceExistingInspectMsg{requestID: requestID, item: item, err: err}
		}
		path, err := s.session.InspectPath(s.ctx, inspection.EffectivePath)
		return resourceExistingInspectMsg{requestID: requestID, item: item, inspection: inspection, path: path, err: err}
	}
}
func (s *Resources) untrack() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	id := s.selectedResource().ID
	return func() tea.Msg {
		_, err := s.session.UntrackResource(s.ctx, id)
		return resourceUntrackedMsg{requestID: requestID, err: err}
	}
}
func (s *Resources) resetDiscovery() {
	s.browser, s.phase, s.strategy, s.strategyCursor, s.untrackedCursor, s.editingResourceID, s.candidate, s.candidateInspection, s.untracked, s.eligibleUntracked, s.chosen, s.confirm, s.err = nil, resourceBrowse, "", 0, 0, "", components.BrowserEntry{}, workflow.PathInspection{}, nil, nil, nil, "", nil
	s.discover = false
}
func (s *Resources) rescan() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	return func() tea.Msg {
		report, err := s.session.Status(s.ctx, "resources")
		if err != nil {
			return resourcesStatusMsg{requestID: requestID, err: err}
		}
		for _, provider := range report.Providers {
			if provider.ID == "resources" {
				effective := make(map[string]string, len(report.Profile.Resources.Items))
				for _, item := range report.Profile.Resources.Items {
					inspection, err := s.session.InspectResource(s.ctx, item.ID)
					if err == nil {
						effective[item.ID] = inspection.EffectivePath
					}
				}
				return resourcesStatusMsg{requestID: requestID, items: report.Profile.Resources.Items, git: provider.ResourceGit, effective: effective}
			}
		}
		return resourcesStatusMsg{requestID: requestID}
	}
}

func (s *Resources) resourceState(item profile.Resource) string {
	if item.Strategy != "git" && item.Strategy != "git+diff" {
		if item.Dirty {
			return "modified"
		}
		return "clean"
	}
	state, ok := s.git[item.ID]
	if !ok {
		return "clean"
	}
	parts := make([]string, 0, 2)
	if state.StagedTracked > 0 || state.UnstagedTracked > 0 {
		parts = append(parts, "modified")
	}
	if len(state.Untracked) > 0 {
		parts = append(parts, "untracked")
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, ", ")
}

func otherUntracked(state resourcesprovider.GitWorkingSummary) []string {
	selected := make(map[string]bool, len(state.SelectedUntracked))
	for _, path := range state.SelectedUntracked {
		selected[path] = true
	}
	other := make([]string, 0, len(state.Untracked))
	for _, path := range state.Untracked {
		if !selected[path] {
			other = append(other, path)
		}
	}
	return other
}
func (s *Resources) listHeight() int {
	if s.height == 0 {
		return len(s.items) + 1
	}
	return max(1, s.height-4)
}
func (s *Resources) setCandidateInspection() {
	inspection := s.candidateInspection
	s.chosen = make(map[string]bool)
	s.setUntracked(nil)
	s.untrackedCursor = 0
	if s.strategy == "" {
		s.strategy = inspection.SuggestedStrategy
	}
	s.selectStrategy(s.strategy)
}

func (s *Resources) setUntracked(saved []profile.GitUntrackedFile) {
	s.untracked = nil
	s.eligibleUntracked = map[string]bool{}
	if git := s.candidateInspection.Git; git != nil {
		for _, path := range git.UntrackedPaths {
			s.eligibleUntracked[path] = true
			s.untracked = append(s.untracked, path)
		}
	}
	for _, file := range saved {
		if !s.eligibleUntracked[file.Path] {
			s.untracked = append(s.untracked, file.Path)
		}
	}
}

func (s *Resources) strategies() []string {
	strategies := []string{"copy"}
	if s.candidateInspection.Git != nil {
		strategies = append(strategies, "git", "git+diff")
	}
	return strategies
}

func (s *Resources) selectStrategy(strategy string) {
	for i, candidate := range s.strategies() {
		if strategy == candidate {
			s.strategy, s.strategyCursor = strategy, i
			return
		}
	}
	s.strategy, s.strategyCursor = s.strategies()[0], 0
}

func strategyDescription(strategy string) string {
	return map[string]string{"copy": "snapshot files and directories", "git": "portable repository provenance only", "git+diff": "repository plus selected local changes"}[strategy]
}

func clonePathInspection(inspection workflow.PathInspection) workflow.PathInspection {
	if inspection.Git != nil {
		git := *inspection.Git
		git.UntrackedPaths = append([]string(nil), git.UntrackedPaths...)
		inspection.Git = &git
	}
	return inspection
}

func (s *Resources) browserConfig() components.BrowserConfig {
	return components.BrowserConfig{Home: s.session.HomeDir(), ProfileDir: s.session.ProfileDir(), Profile: s.session.Profile(), InspectPathCmd: s.inspectPath}
}
