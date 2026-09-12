package screens

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type Resources struct {
	session                 *workflow.Session
	width, height, selected int
	discover                bool
	items                   []profile.Resource
	git                     map[string]resourcesprovider.GitWorkingSummary
	browser                 *components.Browser
	strategy, confirm       string
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

func NewResources(session *workflow.Session) *Resources { return &Resources{session: session} }
func (s *Resources) Focus(id string) tea.Cmd            { s.focusID = id; return s.rescan() }
func (s *Resources) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Resources) Init() tea.Cmd                      { return s.rescan() }

func (s *Resources) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case resourcesStatusMsg:
		s.items, s.git, s.err = msg.items, msg.git, msg.err
		for i, item := range s.items {
			if item.ID == s.focusID {
				s.selected = i
				break
			}
		}
		s.focusID = ""
		if s.selected >= len(s.items) {
			s.selected = max(0, len(s.items)-1)
		}
		return nil
	case resourceTrackedMsg:
		s.err = msg.err
		if msg.err == nil {
			s.browser, s.strategy, s.confirm = nil, "", ""
			s.discover = false
			return s.rescan()
		}
		return nil
	case resourceUntrackedMsg:
		s.err, s.confirm = msg.err, ""
		if msg.err == nil {
			return s.rescan()
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.confirm != "" {
		if key.String() == "esc" {
			s.confirm = ""
			return nil
		}
		if key.String() == "enter" {
			if s.confirm == "untrack" {
				return s.untrack()
			}
			return s.track()
		}
		return nil
	}
	if s.browser != nil {
		if key.String() == "enter" && s.browser.CanSelect() {
			inspection := s.browser.Inspection()
			s.strategy = inspection.SuggestedStrategy
			if s.strategy == "" {
				s.strategy = "copy"
			}
			return nil
		}
		cmd := s.browser.Update(msg)
		if s.browser.Closed() {
			s.browser = nil
		}
		return cmd
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
		s.discover = true
		home, _ := os.UserHomeDir()
		browser := components.NewBrowser(components.BrowseResource, components.BrowserConfig{Home: home, ProfileDir: s.session.ProfileDir(), Profile: s.session.Profile(), InspectPathCmd: s.inspectPath})
		s.browser = &browser
		return s.browser.Init()
	case "1":
		s.strategy = "copy"
	case "2":
		s.strategy = "git"
	case "3":
		s.strategy = "git+diff"
	case "enter":
		if s.discover && s.strategy != "" {
			s.confirm = "track"
		}
	case "x":
		if !s.discover && s.selectedResource().ID != "" {
			s.confirm = "untrack"
		}
	case "e", "o", "y":
		if item := s.selectedResource(); item.ID != "" {
			inspection, err := s.session.InspectResource(context.Background(), item.ID)
			if err == nil {
				return func() tea.Msg {
					return HandoffRequest{Kind: map[string]string{"e": "editor", "o": "open", "y": "copy"}[key.String()], Path: inspection.EffectivePath}
				}
			}
		}
	}
	return nil
}

func (s *Resources) View() string {
	if s.err != nil {
		return "Resources\n\nUnable to load Resources: " + s.err.Error()
	}
	if s.confirm == "untrack" {
		return components.Confirm("Untrack " + s.selectedResource().ID + "? Its live path remains untouched.")
	}
	if s.confirm == "track" {
		return components.Confirm("Track selected path with strategy " + s.strategy + "?")
	}
	if s.browser != nil {
		lines := []string{"Resources: Discover", s.browser.View(), "", "Choose a strategy: 1 copy (snapshot files), 2 git (repository only), 3 git+diff (repository plus local changes)."}
		if s.strategy != "" {
			lines = append(lines, "Selected: "+s.strategy+". Press Enter to confirm.")
		}
		return strings.Join(lines, "\n")
	}
	tab := "Tracked"
	if s.discover {
		tab = "Discover"
	}
	lines := []string{"Resources  [" + tab + "]", "tab switch subtabs"}
	if s.discover {
		return strings.Join(append(lines, "Press n to browse a resource, then choose and confirm a strategy."), "\n")
	}
	for i, item := range s.items {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		label := item.Strategy
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
	lines = append(lines, "n discover  x untrack  e edit  o open  y copy")
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
	entry, ok := s.browser.Selected()
	if !ok {
		return nil
	}
	request := workflow.TrackRequest{Path: entry.Path, Strategy: s.strategy}
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
