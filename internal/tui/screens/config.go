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
type HandoffRequest struct {
	Source, Kind, Path string
	IsDir              bool
	Refresh            string
}

type Config struct {
	ctx                     context.Context
	session                 *workflow.Session
	width, height, selected int
	candidates              []config.Candidate
	inspection              workflow.ConfigInspection
	diff                    *components.DiffViewer
	confirm                 string
	filter                  string
	filtering               bool
	collapsed               map[config.Classification]bool
	list                    components.Selectable
	focusPath               string
	statusID, inspectionID  uint64
	busy                    bool
	err                     error
}

type configStatusMsg struct {
	requestID  uint64
	candidates []config.Candidate
	err        error
}
type configInspectionMsg struct {
	requestID  uint64
	inspection workflow.ConfigInspection
	err        error
}
type configPolicyMsg struct {
	requestID uint64
	err       error
}

func NewConfig(session *workflow.Session) *Config {
	return NewConfigContext(context.Background(), session)
}
func NewConfigContext(ctx context.Context, session *workflow.Session) *Config {
	return &Config{ctx: ctx, session: session}
}
func (s *Config) Focus(path string) tea.Cmd { s.focusPath = path; return s.rescan() }
func (s *Config) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.diff != nil {
		s.diff.SetSize(width, height)
	}
}
func (s *Config) Init() tea.Cmd         { return s.rescan() }
func (s *Config) TransientActive() bool { return s.confirm != "" || s.diff != nil }

func (s *Config) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case configStatusMsg:
		if msg.requestID != s.statusID {
			return nil
		}
		if msg.err != nil {
			s.err, s.busy = msg.err, false
			return nil
		}
		s.candidates, s.err, s.busy = msg.candidates, nil, false
		s.selected = 0
		for i, candidate := range s.candidates {
			if candidate.Path == s.focusPath {
				s.selected = i
				break
			}
		}
		s.list.SetSelected(s.selected, len(s.filteredCandidates()), s.listHeight())
		s.focusPath = ""
		return s.inspectSelected()
	case configInspectionMsg:
		if msg.requestID != s.inspectionID {
			return nil
		}
		if msg.err != nil {
			s.err = msg.err
			return nil
		}
		s.inspection, s.err = msg.inspection, nil
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
		if msg.requestID != s.statusID {
			return nil
		}
		s.busy = false
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
	if s.busy {
		return nil
	}
	if s.diff != nil {
		if key.String() == "esc" {
			s.diff = nil
			return nil
		}
		return s.diff.Update(msg)
	}
	if s.filtering {
		switch key.String() {
		case "esc", "enter":
			s.filtering = false
		case "backspace":
			if len(s.filter) > 0 {
				s.filter = s.filter[:len(s.filter)-1]
			}
		default:
			if len(key.String()) == 1 {
				s.filter += key.String()
			}
		}
		s.list.SetSelected(0, len(s.filteredCandidates()), s.listHeight())
		return s.inspectSelected()
	}
	switch key.String() {
	case "j", "down":
		if s.list.Move(1, len(s.filteredCandidates()), s.listHeight()) {
			s.selected = s.list.Selected
			return s.inspectSelected()
		}
	case "k", "up":
		if s.list.Move(-1, len(s.filteredCandidates()), s.listHeight()) {
			s.selected = s.list.Selected
			return s.inspectSelected()
		}
	case "/":
		s.filtering = true
	case "d":
		if candidate := s.selectedCandidate(); candidate.Path != "" {
			s.diff = &components.DiffViewer{}
			return s.inspectForDiff(candidate.Path)
		}
	case "enter":
		if candidate := s.selectedCandidate(); candidate.Path != "" {
			if s.collapsed == nil {
				s.collapsed = map[config.Classification]bool{}
			}
			s.collapsed[candidate.Classification] = !s.collapsed[candidate.Classification]
			s.list.SetSelected(0, len(s.filteredCandidates()), s.listHeight())
			return s.inspectSelected()
		}
	case "space", " ", "i":
		s.confirm = "include"
		return s.confirmModal()
	case "x":
		s.confirm = "exclude"
		return s.confirmModal()
	case "a":
		s.confirm = "auto"
		return s.confirmModal()
	case "e", "o", "y":
		if s.validLive() {
			kind := map[string]string{"e": "editor", "o": "open", "y": "copy"}[key.String()]
			return func() tea.Msg {
				return HandoffRequest{Source: "config", Kind: kind, Path: s.inspection.LivePath, Refresh: "config"}
			}
		}
	}
	return nil
}

func (s *Config) View() string {
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
	lines := []string{"Review  " + s.reviewCounts()}
	if s.busy {
		lines = append(lines, "Working...")
	}
	if s.err != nil {
		lines = append(lines, "Last action failed: "+s.err.Error())
	}
	currentGroup := ""
	for i, candidate := range s.filteredCandidates() {
		group := string(candidate.Classification)
		if group != currentGroup {
			marker := "-"
			if s.collapsed[candidate.Classification] {
				marker = "+"
			}
			lines = append(lines, "", "["+marker+" "+group+"]")
			currentGroup = group
		}
		if s.collapsed[candidate.Classification] {
			continue
		}
		marker := " "
		if i == s.list.Selected {
			marker = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s (%s)", marker, candidate.Path, candidate.Classification, candidate.Reason))
	}
	if len(s.candidates) == 0 {
		lines = append(lines, "No Config candidates.")
	}
	if s.filtering || s.filter != "" {
		lines = append(lines, "", "Filter: "+s.filter)
	}
	if candidate := s.selectedCandidate(); candidate.Path != "" {
		lines = append(lines, "", "Reason: "+candidate.Reason)
	}
	width := s.width
	if width == 0 {
		width = 120
	}
	return s.list.View(lines, width, s.listHeight())
}

func ptrDiff(document inspection.DiffDocument, width, height int) *components.DiffViewer {
	viewer := components.NewDiffViewer(document)
	viewer.SetSize(width, height)
	return &viewer
}
func (s *Config) selectedCandidate() config.Candidate {
	items := s.filteredCandidates()
	if s.list.Selected >= 0 && s.list.Selected < len(items) {
		return items[s.list.Selected]
	}
	return config.Candidate{}
}
func (s *Config) filteredCandidates() []config.Candidate {
	items := make([]config.Candidate, 0, len(s.candidates))
	filter := strings.ToLower(s.filter)
	for _, candidate := range s.candidates {
		if filter == "" || strings.Contains(strings.ToLower(candidate.Path+" "+string(candidate.Classification)+" "+candidate.Reason), filter) {
			items = append(items, candidate)
		}
	}
	return items
}
func (s *Config) reviewCounts() string {
	counts := map[config.Classification]int{}
	for _, candidate := range s.candidates {
		counts[candidate.Classification]++
	}
	parts := make([]string, 0, len(counts))
	for _, classification := range []config.Classification{config.ConfigModifiedBaseline, config.ConfigDeletedBaseline, config.ConfigAdded, config.ConfigAmbiguousBaseline, config.ConfigAmbiguousDeletion, config.ConfigSensitive, config.ConfigOversized, config.ConfigExcluded, config.ConfigDelegated, config.ConfigHistoricalBaseline} {
		if counts[classification] > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", classification, counts[classification]))
		}
	}
	if len(parts) == 0 {
		return "clean:0"
	}
	return strings.Join(parts, "  ")
}
func (s *Config) listHeight() int {
	if s.height == 0 {
		return len(s.candidates) + 8
	}
	return max(1, s.height-4)
}
func (s *Config) confirmModal() tea.Cmd {
	prompt := fmt.Sprintf("Set %s policy for %s?", s.confirm, s.selectedCandidate().Path)
	return func() tea.Msg {
		return components.ModalRequest{Title: "Config policy", Content: components.Confirm(prompt)}
	}
}
func (s *Config) rescan() tea.Cmd {
	s.statusID++
	requestID := s.statusID
	s.busy = true
	return func() tea.Msg {
		report, err := s.session.Status(s.ctx, "config")
		if err != nil {
			return configStatusMsg{requestID: requestID, err: err}
		}
		for _, provider := range report.Providers {
			if provider.ID == "config" && provider.ConfigScan != nil {
				return configStatusMsg{requestID: requestID, candidates: provider.ConfigScan.Candidates}
			}
		}
		return configStatusMsg{requestID: requestID, err: fmt.Errorf("config scan is unavailable")}
	}
}
func (s *Config) inspectSelected() tea.Cmd { return s.inspectForDiff(s.selectedCandidate().Path) }
func (s *Config) inspectForDiff(logical string) tea.Cmd {
	if logical == "" {
		return nil
	}
	s.inspectionID++
	requestID := s.inspectionID
	return func() tea.Msg {
		inspection, err := s.session.InspectConfig(s.ctx, logical)
		return configInspectionMsg{requestID: requestID, inspection: inspection, err: err}
	}
}
func (s *Config) setPolicy(logical, policy string) tea.Cmd {
	s.busy = true
	requestID := s.statusID
	return func() tea.Msg {
		return configPolicyMsg{requestID: requestID, err: s.session.SetConfigPolicy(s.ctx, logical, policy)}
	}
}
func (s *Config) validLive() bool {
	info, err := os.Lstat(s.inspection.LivePath)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}
func (s *Config) CanHandoff() bool { return s.validLive() }

// DetailView supplies the root three-pane preview without duplicating it below the list.
func (s *Config) DetailView() string {
	candidate := s.selectedCandidate()
	if candidate.Path == "" {
		return "Config details"
	}
	lines := []string{"Path: " + candidate.Path, "Classification: " + string(candidate.Classification), "Reason: " + candidate.Reason}
	if s.inspection.LivePath != "" {
		lines = append(lines, "Live: "+s.inspection.LivePath, "Baseline: "+s.inspection.BaselinePath, "Profile: "+s.inspection.ProfilePath)
	}
	policy := "automatic"
	if s.inspection.Managed {
		policy = "managed"
	}
	lines = append(lines, "Effective policy: "+policy, "Actions: diff, include, exclude, auto")
	if s.validLive() {
		lines = append(lines, "Edit/open/copy: available")
	} else {
		lines = append(lines, "Edit/open/copy: unavailable (live path is not a regular file)")
	}
	return strings.Join(lines, "\n")
}
