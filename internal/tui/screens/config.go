package screens

import (
	"context"
	"fmt"
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
	collapsed               map[configDisplayGroup]bool
	table                   components.Table
	navigation              components.Selectable
	styles                  components.Styles
	focusPath               string
	statusID, inspectionID  uint64
	busy                    bool
	notice                  string
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
func (s *Config) Focus(path string) tea.Cmd {
	s.focusPath, s.filter, s.filtering = path, "", false
	for _, candidate := range s.candidates {
		if candidate.Path == path {
			if s.collapsed == nil {
				s.collapsed = defaultConfigCollapsed()
			}
			s.collapsed[configPresentationFor(candidate.Classification).Group] = false
		}
	}
	return s.rescan()
}
func (s *Config) SetSize(width, height int) {
	s.width, s.height = width, height
	if s.diff != nil {
		s.diff.SetSize(width, height)
	}
}
func (s *Config) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Config) Init() tea.Cmd                      { return s.rescan() }
func (s *Config) TransientActive() bool              { return s.confirm != "" || s.diff != nil || s.filtering }

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
		for _, candidate := range s.candidates {
			if candidate.Path == s.focusPath {
				if s.collapsed == nil {
					s.collapsed = defaultConfigCollapsed()
				}
				s.collapsed[configPresentationFor(candidate.Classification).Group] = false
			}
		}
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
		if msg.inspection.Candidate.Path != s.selectedCandidate().Path {
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
		s.notice = "Policy updated."
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
	s.navigation.Selected = s.selected
	if key.String() == "[" || key.String() == "]" {
		if s.navigation.JumpToAnchor(s.groupAnchors(), key.String() == "]", len(s.rows()), max(1, s.listHeight()-1)) {
			before := s.selected
			s.selected = s.navigation.Selected
			s.ensureSelection()
			if before != s.selected {
				return s.inspectSelected()
			}
		}
		return nil
	}
	if s.navigation.Vim(key.String(), len(s.rows()), max(1, s.listHeight()-1)) {
		before := s.selected
		s.selected = s.navigation.Selected
		s.ensureSelection()
		if before != s.selected {
			return s.inspectSelected()
		}
		return nil
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
				s.collapsed = defaultConfigCollapsed()
			}
			s.collapsed[row.group] = !s.collapsed[row.group]
			s.ensureSelection()
			return s.inspectSelected()
		}
	case "space", " ", "i":
		if s.selectedCandidate().Path == "" {
			return nil
		}
		policy := "include"
		if key.String() == " " || key.String() == "space" {
			policy = s.nextPolicy(s.selectedCandidate().Path)
		}
		return s.setPolicy(s.selectedCandidate().Path, policy)
	case "x":
		if s.selectedCandidate().Path == "" {
			return nil
		}
		return s.setPolicy(s.selectedCandidate().Path, "exclude")
	case "a":
		if s.selectedCandidate().Path == "" {
			return nil
		}
		return s.setPolicy(s.selectedCandidate().Path, "auto")
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
		return "Config diff: " + components.DisplayText(document.OldLabel) + " <-> " + components.DisplayText(document.NewLabel) + "\n" + s.diff.View()
	}
	lines := []string{"Review  " + s.reviewCounts()}
	if s.busy {
		lines = append(lines, "Working...")
	}
	if s.err != nil {
		lines = append(lines, "Last action failed: "+components.DisplayText(s.err.Error()))
	}
	if s.notice != "" {
		lines = append(lines, s.styles.Success(s.notice))
	}
	rows := s.rows()
	if len(s.candidates) == 0 {
		lines = append(lines, renderEmptyState(s.styles, s.widthOrDefault(), emptyStateCopy{
			Heading:     "No configuration needs review",
			Explanation: "Config is for dotfiles and application/system configuration that differs meaningfully from Omarchy defaults.",
			Guidance:    "If there is nothing suitable for Config to manage, there is nothing to do here.",
		}))
		if s.filtering || s.filter != "" {
			lines = append(lines, "Filter: "+s.filter)
		}
		return strings.Join(lines, "\n")
	}
	if len(rows) == 0 && s.filter != "" {
		lines = append(lines, "No configuration matches this filter.")
		if s.filtering || s.filter != "" {
			lines = append(lines, "Filter: "+s.filter)
		}
		return strings.Join(lines, "\n")
	}
	tableRows := make([]components.Row, 0, len(rows))
	for i, row := range rows {
		if row.group != "" {
			marker := components.Icons.Expanded
			if s.collapsed[row.group] {
				marker = components.Icons.Collapsed
			}
			tableRows = append(tableRows, components.Row{Cells: []string{s.styles.Accent(marker + " " + string(row.group)), "", ""}, Selected: i == s.selected, Focused: true})
			continue
		}
		policy := s.candidatePolicy(row.candidate.Path)
		switch policy {
		case "Included":
			policy = s.styles.Added(policy)
		case "Excluded":
			policy = s.styles.Removed(policy)
		default:
			policy = s.styles.Muted(policy)
		}
		tableRows = append(tableRows, components.Row{Cells: []string{"  " + components.DisplayText(row.candidate.Path), configPresentationFor(row.candidate.Classification).Label, policy}, Selected: i == s.selected, Focused: true})
	}
	lines = append(lines, s.table.Render([]components.Column{{Title: "Path", Width: 0, MinWidth: 12}, {Title: "State", Width: 32, MinWidth: 10}, {Title: "Policy", Width: 12, MinWidth: 6}}, tableRows, s.widthOrDefault(), s.listHeight(), s.styles))
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
	group     configDisplayGroup
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
	rows := make([]configRow, 0, len(items)+len(configDisplayGroups))
	for _, displayGroup := range configDisplayGroups {
		group := make([]config.Candidate, 0)
		for _, candidate := range items {
			if configPresentationFor(candidate.Classification).Group == displayGroup {
				group = append(group, candidate)
			}
		}
		if len(group) == 0 {
			continue
		}
		rows = append(rows, configRow{group: displayGroup})
		if !s.groupCollapsed(displayGroup) {
			for _, candidate := range group {
				rows = append(rows, configRow{candidate: candidate})
			}
		}
	}
	return rows
}

func (s *Config) groupAnchors() []int {
	rows := s.rows()
	anchors := make([]int, 0, len(rows))
	for index, row := range rows {
		if row.group != "" {
			anchors = append(anchors, index)
		}
	}
	return anchors
}

func (s *Config) filteredCandidates() []config.Candidate {
	items := make([]config.Candidate, 0, len(s.candidates))
	filter := strings.ToLower(s.filter)
	for _, candidate := range s.candidates {
		presentation := configPresentationFor(candidate.Classification)
		if filter == "" || strings.Contains(strings.ToLower(candidate.Path+" "+string(presentation.Group)+" "+presentation.Label+" "+presentation.Meaning), filter) {
			items = append(items, candidate)
		}
	}
	return items
}
func (s *Config) reviewCounts() string {
	counts := map[configDisplayGroup]int{}
	for _, candidate := range s.candidates {
		counts[configPresentationFor(candidate.Classification).Group]++
	}
	return strings.Join([]string{
		configCount(counts[configNeedsReview], "needs review", "need review"),
		configCount(counts[configChanges], "change", "changes"),
		configCount(counts[configNoActionNeeded], "no action", "no action"),
		configCount(counts[configNotManaged], "not managed", "not managed"),
	}, " · ")
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
	prompt := fmt.Sprintf("Set %s policy for %s?", s.confirm, components.DisplayText(s.selectedCandidate().Path))
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
func (s *Config) inspectSelected() tea.Cmd {
	s.inspectionID++
	s.inspection = workflow.ConfigInspection{}
	return s.inspectForDiff(s.selectedCandidate().Path)
}
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
func (s *Config) nextPolicy(path string) string {
	for _, excluded := range s.session.Profile().Config.Excluded {
		if excluded == path {
			return "auto"
		}
	}
	for _, included := range s.session.Profile().Config.Included {
		if included == path {
			return "exclude"
		}
	}
	return "include"
}
func (s *Config) validLive() bool {
	return s.inspection.LiveHandoffSafe && s.inspection.Candidate.Path == s.selectedCandidate().Path && s.selectedCandidate().Path != ""
}
func (s *Config) CanHandoff() bool { return s.validLive() }
func (s *Config) CanPolicy() bool  { return s.selectedCandidate().Path != "" }

// DetailView supplies the root three-pane preview without duplicating it below the list.
func (s *Config) DetailView() string {
	candidate := s.selectedCandidate()
	if candidate.Path == "" {
		return "Config details"
	}
	presentation := configPresentationFor(candidate.Classification)
	policy := s.candidatePolicy(candidate.Path)
	lines := []string{"Path: " + components.DisplayText(candidate.Path), "State: " + presentation.Label, "Policy: " + policy, "Policy meaning: " + configPolicyMeaning(policy), "Meaning: " + presentation.Meaning, "Saved in profile: " + map[bool]string{true: "Yes", false: "No"}[s.inspection.Managed]}
	if policy == "Included" {
		lines = append(lines, "Included never overrides safety rules.")
	}
	if s.inspection.LivePath != "" {
		lines = append(lines, "Live file: "+components.DisplayText(s.inspection.LivePath))
	}
	if s.inspection.BaselinePath != "" {
		lines = append(lines, "Omarchy default: "+components.DisplayText(s.inspection.BaselinePath))
	}
	if s.inspection.ProfilePath != "" {
		lines = append(lines, "Saved copy: "+components.DisplayText(s.inspection.ProfilePath))
	}
	return strings.Join(lines, "\n")
}

func configPolicyMeaning(policy string) string {
	return map[string]string{"Auto": "Blueprint decides based on Omarchy defaults, ownership, and safety rules.", "Included": "You asked Config to manage this path when it is safe to do so.", "Excluded": "You asked Config to leave this path alone."}[policy]
}

type configDisplayGroup string

const (
	configNeedsReview    configDisplayGroup = "Needs review"
	configChanges        configDisplayGroup = "Changes"
	configNoActionNeeded configDisplayGroup = "No action needed"
	configNotManaged     configDisplayGroup = "Not managed by Config"
)

type configPresentation struct {
	Group   configDisplayGroup
	Label   string
	Meaning string
}

var configDisplayGroups = []configDisplayGroup{configNeedsReview, configChanges, configNoActionNeeded, configNotManaged}

func configPresentationFor(classification config.Classification) configPresentation {
	return configPresentations[classification]
}

var configPresentations = map[config.Classification]configPresentation{
	config.ConfigAmbiguousBaseline:  {configNeedsReview, "Needs review", "This file differs, but Blueprint cannot safely tell whether it is your change or an Omarchy-version difference."},
	config.ConfigAmbiguousDeletion:  {configNeedsReview, "Removal needs review", "An Omarchy default is missing, but Blueprint cannot safely tell whether you intentionally removed it."},
	config.ConfigModifiedBaseline:   {configChanges, "Changed from Omarchy default", "You changed an Omarchy-provided configuration file. Blueprint can remember the change."},
	config.ConfigDeletedBaseline:    {configChanges, "Omarchy default removed", "An Omarchy-provided file is absent and Blueprint has enough information to treat that removal as intentional."},
	config.ConfigAdded:              {configChanges, "Added configuration", "This configuration file exists on your system but is not part of Omarchy's defaults."},
	config.ConfigUnchangedBaseline:  {configNoActionNeeded, "Matches Omarchy default", "This file is unchanged from Omarchy, so there is nothing to save."},
	config.ConfigHistoricalBaseline: {configNoActionNeeded, "Matches a previous Omarchy default", "This looks like an older Omarchy version rather than a personal customisation. Blueprint leaves it alone unless you explicitly include it."},
	config.ConfigDelegated:          {configNotManaged, "Handled elsewhere", "Another Blueprint feature owns this path, so Config will not capture it again."},
	config.ConfigExcluded:           {configNotManaged, "Excluded", "You told Config to leave this path alone."},
	config.ConfigVolatile:           {configNotManaged, "Not suitable for capture", "This looks like application/runtime state rather than portable configuration."},
	config.ConfigSensitive:          {configNotManaged, "Sensitive", "Blueprint detected content that should not be saved into the profile."},
	config.ConfigOversized:          {configNotManaged, "Too large", "This file exceeds Config's safe automatic capture limit."},
	config.ConfigUnsupported:        {configNotManaged, "Unsupported", "This filesystem entry cannot be handled safely by Config."},
	config.ConfigUnmanagedSymlink:   {configNotManaged, "Symlink not managed here", "This path is a symbolic link that Config deliberately does not own."},
}

func defaultConfigCollapsed() map[configDisplayGroup]bool {
	return map[configDisplayGroup]bool{configNoActionNeeded: true, configNotManaged: true}
}

func (s *Config) groupCollapsed(group configDisplayGroup) bool {
	if s.collapsed == nil {
		return group == configNoActionNeeded || group == configNotManaged
	}
	return s.collapsed[group]
}

func configCount(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}
func (s *Config) candidatePolicy(path string) string {
	if s.session == nil {
		return "Auto"
	}
	for _, excluded := range s.session.Profile().Config.Excluded {
		if configPathWithin(path, excluded) {
			return "Excluded"
		}
	}
	for _, included := range s.session.Profile().Config.Included {
		if configPathWithin(path, included) {
			return "Included"
		}
	}
	return "Auto"
}
func configPathWithin(path, root string) bool {
	root = strings.TrimSuffix(root, "/")
	return path == root || strings.HasPrefix(path, root+"/")
}
