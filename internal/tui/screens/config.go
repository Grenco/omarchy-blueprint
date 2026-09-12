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
	table                   components.Table
	styles                  components.Styles
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
func (s *Config) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Config) Init() tea.Cmd                      { return s.rescan() }
func (s *Config) TransientActive() bool              { return s.confirm != "" || s.diff != nil }

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
		for i, row := range s.rows() {
			if row.candidate.Path == s.focusPath {
				s.selected = i
				break
			}
		}
		s.ensureSelection()
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
		s.selected = 0
		s.ensureSelection()
		return s.inspectSelected()
	}
	switch key.String() {
	case "j", "down":
		before := s.selected
		s.selected = min(len(s.rows())-1, s.selected+1)
		s.ensureSelection()
		if before != s.selected {
			return s.inspectSelected()
		}
	case "k", "up":
		before := s.selected
		s.selected = max(0, s.selected-1)
		s.ensureSelection()
		if before != s.selected {
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
		if row := s.selectedRow(); row.group != "" {
			if s.collapsed == nil {
				s.collapsed = map[config.Classification]bool{}
			}
			s.collapsed[row.group] = !s.collapsed[row.group]
			s.ensureSelection()
			return s.inspectSelected()
		}
	case "space", " ", "i":
		if s.selectedCandidate().Path == "" {
			return nil
		}
		s.confirm = "include"
		return s.confirmModal()
	case "x":
		if s.selectedCandidate().Path == "" {
			return nil
		}
		s.confirm = "exclude"
		return s.confirmModal()
	case "a":
		if s.selectedCandidate().Path == "" {
			return nil
		}
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
	rows := s.rows()
	tableRows := make([]components.Row, 0, len(rows))
	for i, row := range rows {
		if row.group != "" {
			marker := "-"
			if s.collapsed[row.group] {
				marker = "+"
			}
			tableRows = append(tableRows, components.Row{Cells: []string{"[" + marker + "] " + configState(row.group), "", ""}, Selected: i == s.selected})
			continue
		}
		tableRows = append(tableRows, components.Row{Cells: []string{configState(row.candidate.Classification), row.candidate.Path, candidatePolicy(row.candidate)}, Selected: i == s.selected})
	}
	if len(tableRows) == 0 {
		lines = append(lines, "✓ No configuration needs review.")
	} else {
		lines = append(lines, s.table.Render([]components.Column{{Title: "State", Width: 23, MinWidth: 8}, {Title: "Path", Width: 0, MinWidth: 12}, {Title: "Policy", Width: 14, MinWidth: 6}}, tableRows, s.widthOrDefault(), s.listHeight(), s.styles))
	}
	if s.filtering || s.filter != "" {
		lines = append(lines, "Filter: "+s.filter)
	}
	return strings.Join(lines, "\n")
}

func ptrDiff(document inspection.DiffDocument, width, height int) *components.DiffViewer {
	viewer := components.NewDiffViewer(document)
	viewer.SetSize(width, height)
	return &viewer
}
func (s *Config) selectedCandidate() config.Candidate {
	return s.selectedRow().candidate
}

type configRow struct {
	group     config.Classification
	candidate config.Candidate
}

func (s *Config) selectedRow() configRow {
	rows := s.rows()
	if s.selected >= 0 && s.selected < len(rows) {
		return rows[s.selected]
	}
	return configRow{}
}
func (s *Config) rows() []configRow {
	items := s.filteredCandidates()
	rows := make([]configRow, 0, len(items)+len(configClassifications))
	for _, classification := range configClassifications {
		group := make([]config.Candidate, 0)
		for _, candidate := range items {
			if candidate.Classification == classification {
				group = append(group, candidate)
			}
		}
		if len(group) == 0 {
			continue
		}
		rows = append(rows, configRow{group: classification})
		if !s.collapsed[classification] {
			for _, candidate := range group {
				rows = append(rows, configRow{candidate: candidate})
			}
		}
	}
	return rows
}

var configClassifications = []config.Classification{config.ConfigAmbiguousBaseline, config.ConfigAmbiguousDeletion, config.ConfigModifiedBaseline, config.ConfigDeletedBaseline, config.ConfigAdded, config.ConfigUnchangedBaseline, config.ConfigHistoricalBaseline, config.ConfigExcluded, config.ConfigDelegated, config.ConfigVolatile, config.ConfigSensitive, config.ConfigOversized, config.ConfigUnsupported, config.ConfigUnmanagedSymlink}

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
			parts = append(parts, fmt.Sprintf("%s:%d", configState(classification), counts[classification]))
		}
	}
	if len(parts) == 0 {
		return "clean:0"
	}
	return strings.Join(parts, "  ")
}
func (s *Config) listHeight() int {
	if s.height == 0 {
		return max(1, len(s.rows())+1)
	}
	// The table's header is a rendered row too; retain one data row below it.
	return max(2, s.height-1)
}
func (s *Config) ensureSelection() {
	s.table.Ensure(s.selected, len(s.rows()), max(1, s.listHeight()-1))
}
func (s *Config) widthOrDefault() int {
	if s.width == 0 {
		return 120
	}
	return s.width
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
	lines := []string{"Path: " + candidate.Path, "State: " + configState(candidate.Classification), "Policy: " + candidatePolicy(candidate), "Why: " + configReason(candidate.Reason)}
	if s.inspection.LivePath != "" {
		lines = append(lines, "Live: "+s.inspection.LivePath, "Baseline: "+s.inspection.BaselinePath, "Profile: "+s.inspection.ProfilePath)
	}
	policy := "automatic"
	if s.inspection.Managed {
		policy = "managed"
	}
	lines = append(lines, "Effective policy: "+policy, "Provider reason: "+candidate.Reason, "Available actions: view diff, include, exclude, automatic")
	if s.validLive() {
		lines = append(lines, "Edit/open/copy: available")
	} else {
		lines = append(lines, "Edit/open/copy: unavailable (live path is not a regular file)")
	}
	return strings.Join(lines, "\n")
}

func configState(classification config.Classification) string {
	states := map[config.Classification]string{
		config.ConfigUnchangedBaseline: "Matches baseline", config.ConfigModifiedBaseline: "Changed from baseline", config.ConfigDeletedBaseline: "Deleted from live system", config.ConfigAdded: "New configuration", config.ConfigDelegated: "Managed elsewhere", config.ConfigExcluded: "Excluded", config.ConfigVolatile: "Not safe to capture", config.ConfigSensitive: "Sensitive", config.ConfigUnmanagedSymlink: "Unmanaged link", config.ConfigUnsupported: "Unsupported", config.ConfigOversized: "Too large", config.ConfigHistoricalBaseline: "Matches older baseline", config.ConfigAmbiguousBaseline: "Needs a decision", config.ConfigAmbiguousDeletion: "Needs a deletion decision",
	}
	if state := states[classification]; state != "" {
		return state
	}
	return string(classification)
}
func candidatePolicy(candidate config.Candidate) string {
	switch candidate.Classification {
	case config.ConfigExcluded:
		return "excluded"
	case config.ConfigDelegated:
		return "managed elsewhere"
	case config.ConfigSensitive, config.ConfigOversized, config.ConfigVolatile, config.ConfigUnmanagedSymlink, config.ConfigUnsupported:
		return "not eligible"
	case config.ConfigModifiedBaseline, config.ConfigDeletedBaseline:
		return "managed"
	default:
		return "automatic"
	}
}
func configReason(reason string) string {
	if reason == "" {
		return "Blueprint found this path during configuration discovery."
	}
	return strings.ReplaceAll(reason, "-", " ")
}
