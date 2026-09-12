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
	resourceBrowse           resourcePhase = "browse"
	resourceCandidateInspect resourcePhase = "candidate-inspect"
	resourceStrategy         resourcePhase = "strategy"
	resourceUntracked        resourcePhase = "untracked-selector"
	resourceConfirm          resourcePhase = "confirm"
	resourcePending          resourcePhase = "pending"
	resourceResult           resourcePhase = "result"
)

type Resources struct {
	session                 *workflow.Session
	width, height, selected int
	discover                bool
	items                   []profile.Resource
	git                     map[string]resourcesprovider.GitWorkingSummary
	browser                 *components.Browser
	phase                   resourcePhase
	strategy                string
	candidate               components.BrowserEntry
	untracked               []string
	chosen                  map[string]bool
	confirm                 string
	focusID                 string
	err                     error
}

type resourcesStatusMsg struct {
	items []profile.Resource
	git   map[string]resourcesprovider.GitWorkingSummary
	err   error
}
type resourceTrackedMsg struct{ err error }
type resourceUntrackedMsg struct{ err error }

func NewResources(session *workflow.Session) *Resources {
	return &Resources{session: session, phase: resourceBrowse}
}
func (s *Resources) Focus(id string) tea.Cmd   { s.focusID = id; return s.rescan() }
func (s *Resources) SetSize(width, height int) { s.width, s.height = width, height }
func (s *Resources) Init() tea.Cmd             { return s.rescan() }
func (s *Resources) TransientActive() bool {
	return s.browser != nil || s.phase != resourceBrowse || s.confirm != ""
}
func (s *Resources) HandlesKey(key string) bool { return key == "tab" }

func (s *Resources) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case components.BrowserInspectionMsg, components.BrowserReadDirMsg:
		if s.browser != nil {
			cmd := s.browser.Update(msg)
			if s.phase == resourceCandidateInspect || s.phase == resourceStrategy || s.phase == resourceUntracked {
				s.setCandidateInspection()
			}
			return cmd
		}
		return nil
	case resourcesStatusMsg:
		s.items, s.git = msg.items, msg.git
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
		return nil
	case resourceTrackedMsg:
		s.err = msg.err
		if msg.err != nil {
			s.phase = resourceResult
			return nil
		}
		s.resetDiscovery()
		return s.rescan()
	case resourceUntrackedMsg:
		s.err, s.confirm = msg.err, ""
		if msg.err != nil {
			return nil
		}
		return s.rescan()
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.confirm != "" {
		switch key.String() {
		case "esc":
			s.confirm = ""
			s.phase = resourceStrategy
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
	if s.browser != nil && s.phase == resourceBrowse {
		if key.String() == "enter" && s.phase == resourceBrowse && s.browser.CanSelect() {
			s.candidate, _ = s.browser.Selected()
			s.phase = resourceCandidateInspect
			return nil
		}
		cmd := s.browser.Update(msg)
		if s.browser.Closed() {
			s.browser = nil
			s.phase = resourceBrowse
		}
		return cmd
	}
	if s.phase == resourceCandidateInspect {
		switch key.String() {
		case "esc":
			s.resetDiscovery()
		case "enter":
			s.phase = resourceStrategy
		}
		return nil
	}
	if s.phase == resourceStrategy {
		switch key.String() {
		case "esc":
			s.phase = resourceCandidateInspect
		case "1":
			s.strategy = "copy"
		case "2":
			s.strategy = "git"
		case "3":
			s.strategy = "git+diff"
		case "enter":
			if s.strategy == "git+diff" && len(s.untracked) > 0 {
				s.phase = resourceUntracked
			} else if s.strategy != "" {
				s.phase, s.confirm = resourceConfirm, "track"
			}
		}
		return nil
	}
	if s.phase == resourceUntracked {
		switch key.String() {
		case "esc":
			s.phase = resourceStrategy
		case "j", "down":
			s.selected = min(len(s.untracked)-1, s.selected+1)
		case "k", "up":
			s.selected = max(0, s.selected-1)
		case "space", " ":
			if len(s.untracked) > 0 {
				path := s.untracked[s.selected]
				s.chosen[path] = !s.chosen[path]
			}
		case "a":
			for _, path := range s.untracked {
				s.chosen[path] = true
			}
		case "enter":
			s.phase, s.confirm = resourceConfirm, "track"
		}
		return nil
	}
	if s.phase == resourceResult {
		if key.String() == "esc" {
			s.resetDiscovery()
		}
		return nil
	}
	switch key.String() {
	case "tab":
		s.discover = !s.discover
	case "j", "down":
		if s.selected < len(s.items)-1 {
			s.selected++
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
		}
	case "n":
		s.discover, s.err = true, nil
		browser := components.NewBrowser(components.BrowseResource, components.BrowserConfig{Home: s.session.HomeDir(), ProfileDir: s.session.ProfileDir(), Profile: s.session.Profile(), InspectPathCmd: s.inspectPath})
		s.browser = &browser
		return s.browser.Init()
	case "x":
		if !s.discover && s.selectedResource().ID != "" {
			s.confirm = "untrack"
		}
	case "e", "o", "y":
		if item := s.selectedResource(); item.ID != "" {
			inspection, err := s.session.InspectResource(context.Background(), item.ID)
			if err == nil {
				return func() tea.Msg {
					return HandoffRequest{Source: "resources", Kind: map[string]string{"e": "editor", "o": "open", "y": "copy"}[key.String()], Path: inspection.EffectivePath, IsDir: inspection.Resource.Kind == "directory", Refresh: "resources"}
				}
			}
		}
	}
	return nil
}

func (s *Resources) View() string {
	if s.confirm == "untrack" {
		return components.Confirm("Untrack " + s.selectedResource().ID + "? Its live path remains untouched.")
	}
	if s.phase == resourceConfirm {
		return components.Confirm("Track selected path with strategy " + s.strategy + "?")
	}
	if s.phase == resourcePending {
		return "Tracking resource..."
	}
	if s.phase == resourceResult {
		if s.err != nil {
			return "Resource tracking failed: " + s.err.Error()
		}
		return "Resource tracked. Press Esc to continue."
	}
	if s.browser != nil && s.phase == resourceBrowse {
		return "Resources: Discover\n" + s.browser.View()
	}
	if s.phase == resourceCandidateInspect {
		return "Candidate: " + s.candidate.Path + "\nPress Enter to choose a strategy, Esc to browse again."
	}
	if s.phase == resourceStrategy {
		return "Choose a strategy: 1 copy (snapshot files), 2 git (repository only), 3 git+diff (repository plus local changes).\nSelected: " + s.strategy + ". Press Enter to continue."
	}
	if s.phase == resourceUntracked {
		lines := []string{"Select untracked files: Space toggle, a all, Enter confirm"}
		for i, path := range s.untracked {
			marker, selected := " ", " "
			if i == s.selected {
				marker = ">"
			}
			if s.chosen[path] {
				selected = "x"
			}
			lines = append(lines, fmt.Sprintf("%s [%s] %s", marker, selected, path))
		}
		return strings.Join(lines, "\n")
	}
	tab := "Tracked"
	if s.discover {
		tab = "Discover"
	}
	lines := []string{"Resources  [" + tab + "]", "tab switch subtabs"}
	if s.err != nil {
		lines = append(lines, "Last action failed: "+s.err.Error())
	}
	if s.discover {
		return strings.Join(append(lines, "Press n to browse a resource."), "\n")
	}
	for i, item := range s.items {
		marker, label := " ", item.Strategy
		if i == s.selected {
			marker = ">"
		}
		if item.Strategy == "git" && item.Dirty {
			label += " (dirty: local changes are not captured)"
		}
		if item.Strategy == "git+diff" {
			state := s.git[item.ID]
			label += fmt.Sprintf(" (staged:%d unstaged:%d untracked:%d selected:%d)", state.StagedTracked, state.UnstagedTracked, len(state.Untracked), len(state.SelectedUntracked))
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", marker, item.ID, item.Path, label))
	}
	if len(s.items) == 0 {
		lines = append(lines, "No tracked resources.")
	}
	return strings.Join(append(lines, "n discover  x untrack  e edit  o open  y copy"), "\n")
}

func (s *Resources) DetailView() string {
	if s.browser != nil {
		return s.browser.DetailView()
	}
	if s.phase == resourceCandidateInspect || s.phase == resourceStrategy || s.phase == resourceUntracked {
		return resourceDetail(s.candidate.Path, s.browserInspection())
	}
	if item := s.selectedResource(); item.ID != "" {
		return "Resource: " + item.ID + "\nSaved path: " + item.Path + "\nStrategy: " + item.Strategy
	}
	return "Resources details"
}
func (s *Resources) browserInspection() workflow.PathInspection {
	if s.browser != nil {
		return s.browser.Inspection()
	}
	return workflow.PathInspection{}
}
func resourceDetail(path string, inspection workflow.PathInspection) string {
	lines := []string{"Candidate: " + path}
	if inspection.Git != nil {
		lines = append(lines, fmt.Sprintf("Git: %s (%s), staged:%d unstaged:%d untracked:%d", inspection.Git.Branch, inspection.Git.Revision, inspection.Git.Staged, inspection.Git.Unstaged, inspection.Git.Untracked))
	}
	if inspection.SuggestedStrategy != "" {
		lines = append(lines, "Suggested: "+inspection.SuggestedStrategy+" - "+inspection.StrategyReason)
	}
	if inspection.BlockedReason != "" {
		lines = append(lines, "Blocked: "+inspection.BlockedReason)
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
		inspection, err := s.session.InspectPath(context.Background(), path)
		return components.BrowserInspectionMsg{RequestID: requestID, Inspection: inspection, Err: err}
	}
}
func (s *Resources) track() tea.Cmd {
	request := workflow.TrackRequest{Path: s.candidate.Path, Strategy: s.strategy}
	for _, path := range s.untracked {
		if s.chosen[path] {
			request.IncludeUntracked = append(request.IncludeUntracked, path)
		}
	}
	return func() tea.Msg {
		_, _, err := s.session.TrackResource(context.Background(), request)
		return resourceTrackedMsg{err}
	}
}
func (s *Resources) untrack() tea.Cmd {
	id := s.selectedResource().ID
	return func() tea.Msg {
		_, err := s.session.UntrackResource(context.Background(), id)
		return resourceUntrackedMsg{err}
	}
}
func (s *Resources) resetDiscovery() {
	s.browser, s.phase, s.strategy, s.candidate, s.untracked, s.chosen, s.confirm, s.err = nil, resourceBrowse, "", components.BrowserEntry{}, nil, nil, "", nil
	s.discover = false
}
func (s *Resources) rescan() tea.Cmd {
	return func() tea.Msg {
		report, err := s.session.Status(context.Background(), "resources")
		if err != nil {
			return resourcesStatusMsg{err: err}
		}
		for _, provider := range report.Providers {
			if provider.ID == "resources" {
				return resourcesStatusMsg{items: report.Profile.Resources.Items, git: provider.ResourceGit}
			}
		}
		return resourcesStatusMsg{}
	}
}
func (s *Resources) setCandidateInspection() {
	inspection := s.browserInspection()
	if inspection.Git != nil {
		s.untracked = append([]string(nil), inspection.Git.UntrackedPaths...)
	}
	s.chosen = make(map[string]bool)
	s.selected = 0
}
