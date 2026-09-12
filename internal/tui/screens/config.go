package screens

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// HandoffRequest lets the parent TUI apply its centrally configured handoff.
type HandoffRequest struct{ Kind, Path string }

type Config struct {
	session                 *workflow.Session
	width, height, selected int
	candidates              []config.Candidate
	inspection              workflow.ConfigInspection
	diff                    *components.DiffViewer
	confirm                 string
	focusPath               string
	err                     error
}

type configStatusMsg struct {
	candidates []config.Candidate
	err        error
}
type configInspectionMsg struct {
	inspection workflow.ConfigInspection
	err        error
}
type configPolicyMsg struct{ err error }

func NewConfig(session *workflow.Session) *Config { return &Config{session: session} }
func (s *Config) Focus(path string) tea.Cmd       { s.focusPath = path; return s.rescan() }
func (s *Config) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.diff != nil {
		s.diff.SetSize(width, height)
	}
}
func (s *Config) Init() tea.Cmd { return s.rescan() }

func (s *Config) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case configStatusMsg:
		s.candidates, s.err = msg.candidates, msg.err
		s.selected = 0
		for i, candidate := range s.candidates {
			if candidate.Path == s.focusPath {
				s.selected = i
				break
			}
		}
		s.focusPath = ""
		return s.inspectSelected()
	case configInspectionMsg:
		s.inspection, s.err = msg.inspection, msg.err
		if msg.err == nil && s.diff != nil {
			document := s.inspection.BaselineToLive
			if s.inspection.Managed && s.inspection.ProfileToLive != nil {
				document = s.inspection.ProfileToLive
			}
			if document != nil {
				s.diff = ptrDiff(*document, s.width, s.height)
			}
		}
		return nil
	case configPolicyMsg:
		if msg.err != nil {
			s.err = msg.err
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
		case "enter":
			policy, logical := s.confirm, s.selectedCandidate().Path
			s.confirm, s.focusPath = "", logical
			return s.setPolicy(logical, policy)
		}
		return nil
	}
	if s.diff != nil {
		if key.String() == "esc" {
			s.diff = nil
			return nil
		}
		return s.diff.Update(msg)
	}
	switch key.String() {
	case "j", "down":
		if s.selected < len(s.candidates)-1 {
			s.selected++
			return s.inspectSelected()
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
			return s.inspectSelected()
		}
	case "d":
		if candidate := s.selectedCandidate(); candidate.Path != "" {
			s.diff = &components.DiffViewer{}
			return s.inspectForDiff(candidate.Path)
		}
	case " ", "i":
		s.confirm = "include"
	case "x":
		s.confirm = "exclude"
	case "a":
		s.confirm = "auto"
	case "e", "o", "y":
		if s.validLive() {
			kind := map[string]string{"e": "editor", "o": "open", "y": "copy"}[key.String()]
			return func() tea.Msg { return HandoffRequest{Kind: kind, Path: s.inspection.LivePath} }
		}
	}
	return nil
}

func (s *Config) View() string {
	if s.err != nil {
		return "Config\n\nUnable to load Config: " + s.err.Error()
	}
	if s.confirm != "" {
		return components.Confirm(fmt.Sprintf("Set %s policy for %s?", s.confirm, s.selectedCandidate().Path))
	}
	if s.diff != nil {
		if s.inspection.BaselineToLive == nil && s.inspection.ProfileToLive == nil {
			return "Loading diff..."
		}
		document := s.inspection.BaselineToLive
		if s.inspection.Managed && s.inspection.ProfileToLive != nil {
			document = s.inspection.ProfileToLive
		}
		if document == nil {
			return "No diff available."
		}
		return "Config diff: " + document.OldLabel + " <-> " + document.NewLabel + "\n" + s.diff.View()
	}
	lines := []string{"Config review"}
	for i, candidate := range s.candidates {
		marker := " "
		if i == s.selected {
			marker = ">"
		}
		label := string(candidate.Classification)
		if candidate.Reason != "" {
			label += " (" + candidate.Reason + ")"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s", marker, candidate.Path, label))
	}
	if len(s.candidates) == 0 {
		lines = append(lines, "No Config candidates.")
	}
	if candidate := s.selectedCandidate(); candidate.Path != "" {
		lines = append(lines, "", "Reason: "+candidate.Reason, "d diff  i include  x exclude  a auto  e edit  o open  y copy")
		if s.width >= 120 {
			lines = append(lines, "Live: "+s.inspection.LivePath)
		}
	}
	return strings.Join(lines, "\n")
}

func ptrDiff(document inspection.DiffDocument, width, height int) *components.DiffViewer {
	viewer := components.NewDiffViewer(document)
	viewer.SetSize(width, height)
	return &viewer
}
func (s *Config) selectedCandidate() config.Candidate {
	if s.selected >= 0 && s.selected < len(s.candidates) {
		return s.candidates[s.selected]
	}
	return config.Candidate{}
}
func (s *Config) rescan() tea.Cmd {
	return func() tea.Msg {
		report, err := s.session.Status(context.Background(), "config")
		if err != nil {
			return configStatusMsg{err: err}
		}
		for _, provider := range report.Providers {
			if provider.ID == "config" && provider.ConfigScan != nil {
				return configStatusMsg{candidates: provider.ConfigScan.Candidates}
			}
		}
		return configStatusMsg{err: fmt.Errorf("config scan is unavailable")}
	}
}
func (s *Config) inspectSelected() tea.Cmd { return s.inspectForDiff(s.selectedCandidate().Path) }
func (s *Config) inspectForDiff(logical string) tea.Cmd {
	return func() tea.Msg {
		inspection, err := s.session.InspectConfig(context.Background(), logical)
		return configInspectionMsg{inspection: inspection, err: err}
	}
}
func (s *Config) setPolicy(logical, policy string) tea.Cmd {
	return func() tea.Msg {
		return configPolicyMsg{err: s.session.SetConfigPolicy(context.Background(), logical, policy)}
	}
}
func (s *Config) validLive() bool {
	info, err := os.Lstat(s.inspection.LivePath)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}
func (s *Config) CanHandoff() bool { return s.validLive() }
