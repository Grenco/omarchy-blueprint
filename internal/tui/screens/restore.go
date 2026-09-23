package screens

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Restore presents the one current plan for the selected machine and one-run
// options. Applying always asks workflow to create a fresh plan, never the
// previewed one.
type Restore struct {
	ctx                     context.Context
	session                 *workflow.Session
	width, height, selected int
	styles                  components.Styles
	confirm                 bool
	busy                    bool
	planning                bool
	err                     error
	current                 model.RestorePlan
	options                 policy.RestoreOptions
	override                bool
	forcedOverrides         int
	planRequestID           uint64
	changesTable            components.Table
	skipsTable              components.Table
	settingsTable           components.Table
	summaryTable            components.Table
	// entryOffset is the first visible line of the Changes/Skipped region
	// when the plan is taller than the workspace.
	entryOffset int
}

const restoreScopeAll = ""

type restoreAppliedMsg struct {
	result workflow.RestoreResult
	err    error
}
type restorePlanMsg struct {
	requestID       uint64
	plan            model.RestorePlan
	forcedOverrides int
	err             error
}

func NewRestore(session *workflow.Session) *Restore {
	return NewRestoreContext(context.Background(), session)
}
func NewRestoreContext(ctx context.Context, session *workflow.Session) *Restore {
	return &Restore{ctx: ctx, session: session, options: policy.DefaultRestoreOptions()}
}
func (s *Restore) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Restore) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Restore) Init() tea.Cmd {
	if s.session == nil {
		return nil
	}
	return s.refreshPlan()
}
func (s *Restore) TransientActive() bool { return s.confirm }

func (s *Restore) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case restorePlanMsg:
		if msg.requestID != s.planRequestID {
			return nil
		}
		s.current, s.forcedOverrides, s.err, s.planning = msg.plan, msg.forcedOverrides, msg.err, false
		s.selected = min(s.selected, max(0, s.currentEntryCount()-1))
		return nil
	case restoreAppliedMsg:
		s.err, s.confirm, s.busy = msg.err, false, false
		if msg.err == nil {
			return s.refreshPlan()
		}
		return nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.confirm {
		switch key.String() {
		case "esc":
			s.confirm = false
		case "enter":
			if !s.busy {
				s.confirm, s.busy = false, true
				return s.apply()
			}
		}
		return nil
	}
	if s.busy || s.planning {
		return nil
	}
	switch key.String() {
	case "j", "down":
		if s.selected < s.currentEntryCount()-1 {
			s.selected++
			s.layoutPlan()
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
			s.layoutPlan()
		}
	case "f":
		s.ensureOptions()
		if s.options.Conflicts == policy.ConflictSafe {
			s.options.Conflicts = policy.ConflictForce
		} else {
			s.options.Conflicts = policy.ConflictSafe
		}
		s.override = true
		return s.refreshPlan()
	case "e":
		s.ensureOptions()
		if s.options.Convergence == policy.ConvergenceAdditive {
			s.options.Convergence = policy.ConvergenceExact
		} else {
			s.options.Convergence = policy.ConvergenceAdditive
		}
		s.override = true
		return s.refreshPlan()
	case "enter":
		if len(s.plan().Operations) > 0 {
			if s.hasInteractiveOperation() {
				s.err = fmt.Errorf("this restore needs an interactive terminal; run the restore from the CLI")
				return nil
			}
			s.confirm = true
			return func() tea.Msg {
				return components.ModalRequest{Title: "Apply restore", Content: components.Confirm(s.confirmation())}
			}
		}
	}
	return nil
}

func (s *Restore) View() string {
	if s.err != nil {
		return "Unable to prepare restore: " + components.DisplayText(s.err.Error())
	}
	if s.busy {
		return "Applying restore..."
	}
	if s.planning {
		return "Refreshing plan..."
	}
	if s.session != nil && !profileHasCapturedState(s.session.Profile()) {
		return renderEmptyState(s.styles, s.width, emptyStateCopy{
			Heading:     "Nothing to restore yet",
			Explanation: "Restore recreates state that is already saved in this Blueprint profile.",
			Guidance:    "Capture the parts of this machine you want Blueprint to remember before using Restore.",
		})
	}
	return s.currentPlanView()
}
func (s *Restore) DetailView() string {
	if s.selected < len(s.current.Operations) {
		op := s.current.Operations[s.selected]
		return fmt.Sprintf("Restore operation\nCategory: %s\nTarget: %s\nAction: %s\nOutcome: %s\nRisk: %s\nInteractive: %t", components.DisplayText(restoreCategoryLabel(op.Provider)), components.DisplayText(op.Resource), operationAction(op), titleMode(string(operationOutcome(op))), operationRisk(op), op.Interactive)
	}
	skipIndex := s.selected - len(s.current.Operations)
	if skipIndex >= 0 && skipIndex < len(s.current.Skipped) {
		skipped := s.current.Skipped[skipIndex]
		return fmt.Sprintf("Restore skip\nCategory: %s\nTarget: %s\nReason: %s", components.DisplayText(restoreCategoryLabel(skipped.Provider)), components.DisplayText(skipped.Resource), components.DisplayText(skipped.Reason))
	}
	return "Restore details\nNo plan item selected."
}

func (s *Restore) ensureOptions() {
	if s.override {
		return
	}
	if s.session == nil {
		if s.options.Conflicts == "" || s.options.Convergence == "" {
			s.options = policy.DefaultRestoreOptions()
		}
		return
	}
	s.options = policy.DefaultRestoreOptions()
	selected := s.session.Machine().Name
	for _, machine := range s.session.Profile().Machines.Items {
		if machine.Name == selected {
			s.options = machine.EffectiveRestoreDefaults()
			break
		}
	}
}
func (s *Restore) refreshPlan() tea.Cmd {
	if s.session == nil {
		return nil
	}
	s.planRequestID++
	requestID := s.planRequestID
	s.planning, s.err = true, nil
	s.ensureOptions()
	effectiveOptions := s.options
	var options *policy.RestoreOptions
	if s.override {
		options = &s.options
	}
	return func() tea.Msg {
		plan, err := s.session.PlanRestore(s.ctx, restoreScopeAll, options)
		if err != nil {
			return restorePlanMsg{requestID: requestID, err: err}
		}
		forcedOverrides := 0
		if effectiveOptions.Conflicts == policy.ConflictForce {
			safe := effectiveOptions
			safe.Conflicts = policy.ConflictSafe
			if safePlan, safeErr := s.session.PlanRestore(s.ctx, restoreScopeAll, &safe); safeErr == nil {
				forcedOverrides = countForcedOverrides(plan, safePlan)
			}
		}
		return restorePlanMsg{requestID: requestID, plan: plan, forcedOverrides: forcedOverrides}
	}
}
func (s *Restore) currentPlanView() string {
	header, entries := s.layoutPlan()
	if entries == nil {
		return strings.Join(append(header, "", "No restore operations required."), "\n")
	}
	return strings.Join(append(append(header, ""), entries...), "\n")
}

// restoreMinEntryLines is the smallest Changes/Skipped viewport worth keeping
// the full settings and summary tables for; below it the header compacts so
// the actual operations stay on screen.
const restoreMinEntryLines = 8

// layoutPlan returns the header and the visible Changes/Skipped lines. When
// the workspace height is known the result always fits it, and the selected
// operation or skip is always inside the returned entry lines, so nothing
// safety-relevant depends on the outer panel clipping.
func (s *Restore) layoutPlan() (header, entries []string) {
	s.ensureOptions()
	width := s.widthOrDefault()
	header = s.fullPlanHeader(width)
	if s.currentEntryCount() == 0 {
		return header, nil
	}
	lines := s.entryLines(width)
	if s.height <= 0 || len(header)+1+len(lines) <= s.height {
		s.entryOffset = 0
		return header, restoreLineTexts(lines)
	}
	if s.height-len(header)-1 < restoreMinEntryLines {
		header = s.compactPlanHeader(width)
	}
	return header, s.entryWindow(lines, max(3, s.height-len(header)-1), width)
}

func (s *Restore) fullPlanHeader(width int) []string {
	counts := outcomeCounts(s.current)
	machine, conflicts, conflictMeaning, convergence, convergenceMeaning, defaults, defaultsMeaning := s.runSettings()
	settings := s.settingsTable.Render(
		[]components.Column{{Title: "SETTING", Width: 22, MinWidth: 18}, {Title: "VALUE", Width: 20, MinWidth: 12}, {Title: "MEANING", MinWidth: 22}},
		[]components.Row{
			{Cells: []string{"Machine", components.DisplayText(machine), "Target machine for this run"}},
			{Cells: []string{"Conflict handling (f)", conflicts, conflictMeaning}},
			{Cells: []string{"Convergence (e)", convergence, convergenceMeaning}},
			{Cells: []string{"Defaults", defaults, defaultsMeaning}},
		}, width, 5, s.styles,
	)
	sections := []string{components.SectionDivider("Run settings", width, s.styles) + "\n" + settings}
	if s.options.Convergence == policy.ConvergenceExact {
		sections = append(sections, s.wrapCurrentPlan([]string{"WARNING: Exact may remove Blueprint-managed desired-absent targets; Resource data is never deleted."}))
	}
	summary := s.summaryTable.Render(
		[]components.Column{{Title: "CREATE", MinWidth: 6}, {Title: "MODIFY", MinWidth: 6}, {Title: "REPLACE", MinWidth: 7}, {Title: "REMOVALS", MinWidth: 8}, {Title: "COMMANDS", MinWidth: 8}, {Title: "POLICY SKIPS", MinWidth: 12}, {Title: "FORCED OVERRIDES", MinWidth: 16}},
		[]components.Row{{Cells: []string{fmt.Sprint(counts.create), fmt.Sprint(counts.modify), fmt.Sprint(counts.replace), fmt.Sprint(counts.delete), fmt.Sprint(counts.commands), fmt.Sprint(policySkipCount(s.current)), fmt.Sprint(s.forcedOverrides)}}},
		width, 2, s.styles,
	)
	sections = append(sections, components.SectionDivider("Plan summary", width, s.styles)+"\n"+summary)
	return strings.Split(strings.Join(sections, "\n\n"), "\n")
}

// compactPlanHeader keeps every run setting, the Exact warning and every
// count, but as wrapped lines rather than tables.
func (s *Restore) compactPlanHeader(width int) []string {
	counts := outcomeCounts(s.current)
	machine, conflicts, _, convergence, _, defaults, _ := s.runSettings()
	lines := []string{}
	for _, line := range components.WrapText(fmt.Sprintf("Run: %s (f) · %s (e) · %s · %s", conflicts, convergence, defaults, machine), width) {
		lines = append(lines, s.styles.SubtleAccent(line))
	}
	if s.options.Convergence == policy.ConvergenceExact {
		for _, line := range components.WrapText("! Exact removes Blueprint-managed extras; Resource data is never deleted.", width) {
			lines = append(lines, s.styles.Warning(line))
		}
	}
	summary := fmt.Sprintf("Plan: %d create · %d modify · %d replace · %d removals · %d commands · %d policy skips · %d forced", counts.create, counts.modify, counts.replace, counts.delete, counts.commands, policySkipCount(s.current), s.forcedOverrides)
	return append(lines, components.WrapText(summary, width)...)
}

func (s *Restore) runSettings() (machine, conflicts, conflictMeaning, convergence, convergenceMeaning, defaults, defaultsMeaning string) {
	machine = "Profile defaults"
	if s.session != nil && s.session.Machine().Name != "" {
		machine = s.session.Machine().Name
	}
	conflicts, conflictMeaning = titleMode(string(s.options.Conflicts)), "Keep conflicting files"
	if s.options.Conflicts == policy.ConflictForce {
		conflictMeaning = "Overwrite conflicting files"
	}
	convergence, convergenceMeaning = titleMode(string(s.options.Convergence)), "Keep additional items"
	if s.options.Convergence == policy.ConvergenceExact {
		convergenceMeaning = "Remove managed extras"
	}
	defaults, defaultsMeaning = "Machine defaults", "Saved defaults are in use"
	if s.override {
		defaults, defaultsMeaning = "One-run override", "Machine defaults are unchanged"
	}
	return
}

type restoreLineKind uint8

const (
	restoreLineBlank restoreLineKind = iota
	restoreLineDivider
	restoreLineColumns
	restoreLineEntry
)

// restoreLine is one rendered line of the Changes/Skipped region, tagged so
// the viewport can find the selected entry and re-show its section heading.
type restoreLine struct {
	text    string
	kind    restoreLineKind
	section int
	entry   int
}

func (s *Restore) entryLines(width int) []restoreLine {
	lines := []restoreLine{}
	appendSection := func(section int, title, table string, firstEntry int) {
		if len(lines) > 0 {
			lines = append(lines, restoreLine{kind: restoreLineBlank, section: section, entry: -1})
		}
		lines = append(lines, restoreLine{text: components.SectionDivider(title, width, s.styles), kind: restoreLineDivider, section: section, entry: -1})
		for i, text := range strings.Split(table, "\n") {
			if i == 0 {
				lines = append(lines, restoreLine{text: text, kind: restoreLineColumns, section: section, entry: -1})
				continue
			}
			lines = append(lines, restoreLine{text: text, kind: restoreLineEntry, section: section, entry: firstEntry + i - 1})
		}
	}
	if len(s.current.Operations) > 0 {
		columns := restoreOperationColumns(width)
		rows := make([]components.Row, 0, len(s.current.Operations))
		for i, op := range s.current.Operations {
			selected := i == s.selected
			action := operationAction(op)
			risk := operationRisk(op)
			cells := []string{components.DisplayText(restoreCategoryLabel(op.Provider)), components.DisplayText(op.Resource), styleRestoreAction(s.styles, action, selected), styleRestoreRisk(s.styles, risk, selected)}
			if len(columns) == 3 {
				cells = []string{components.DisplayText(op.Resource), styleRestoreAction(s.styles, action, selected), styleRestoreRisk(s.styles, risk, selected)}
			}
			rows = append(rows, components.Row{Cells: cells, Selected: selected, Focused: true})
		}
		appendSection(0, "Changes", s.changesTable.Render(columns, rows, width, len(rows)+1, s.styles), 0)
	}
	if len(s.current.Skipped) > 0 {
		rows := make([]components.Row, 0, len(s.current.Skipped))
		for i, skipped := range s.current.Skipped {
			selected := len(s.current.Operations)+i == s.selected
			reason := skipReasonLabel(skipped.Reason)
			if !selected {
				reason = s.styles.Warning(reason)
			}
			rows = append(rows, components.Row{Cells: []string{components.DisplayText(restoreCategoryLabel(skipped.Provider)), components.DisplayText(skipped.Resource), reason}, Selected: selected, Focused: true})
		}
		appendSection(1, "Skipped by policy / safety / mode", s.skipsTable.Render(restoreSkipColumns(width), rows, width, len(rows)+1, s.styles), len(s.current.Operations))
	}
	return lines
}

// entryWindow returns at most budget lines of the entry region containing the
// selected entry. When a section's heading has scrolled above the window it is
// repeated at the top, so a row is never shown without its Changes/Skipped
// context.
func (s *Restore) entryWindow(lines []restoreLine, budget, width int) []string {
	selected := 0
	for i, line := range lines {
		if line.kind == restoreLineEntry && line.entry == s.selected {
			selected = i
			break
		}
	}
	sticky := func(offset int) []string {
		first := lines[offset]
		if first.kind != restoreLineEntry && first.kind != restoreLineColumns {
			return nil
		}
		heading := []string{}
		for _, line := range lines[:offset] {
			if line.section == first.section && (line.kind == restoreLineDivider || (line.kind == restoreLineColumns && first.kind == restoreLineEntry)) {
				heading = append(heading, line.text)
			}
		}
		return heading
	}
	visible := func(offset int) int { return max(1, budget-len(sticky(offset))) }
	offset := max(0, min(s.entryOffset, len(lines)-1))
	if selected < offset {
		offset = selected
	}
	for selected >= offset+visible(offset) {
		offset++
	}
	for offset > 0 && offset+visible(offset) > len(lines) && selected < offset-1+visible(offset-1) {
		offset--
	}
	s.entryOffset = offset
	window := sticky(offset)
	end := min(len(lines), offset+visible(offset))
	for _, line := range lines[offset:end] {
		window = append(window, line.text)
	}
	return window
}

func restoreLineTexts(lines []restoreLine) []string {
	texts := make([]string, len(lines))
	for i, line := range lines {
		texts[i] = line.text
	}
	return texts
}

func (s *Restore) wrapCurrentPlan(lines []string) string {
	width := s.width
	if width <= 0 {
		width = 80
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, components.WrapText(line, width)...)
	}
	return strings.Join(wrapped, "\n")
}

func (s *Restore) widthOrDefault() int {
	if s.width <= 0 {
		return 80
	}
	return s.width
}

func restoreOperationColumns(width int) []components.Column {
	if width > 0 && width < 70 {
		return []components.Column{{Title: "TARGET", Width: 32, MinWidth: 12}, {Title: "ACTION", Width: 12, MinWidth: 8}, {Title: "RISK", MinWidth: 6}}
	}
	return []components.Column{{Title: "CATEGORY", Width: 18, MinWidth: 10}, {Title: "TARGET", Width: 48, MinWidth: 12}, {Title: "ACTION", Width: 14, MinWidth: 8}, {Title: "RISK", MinWidth: 6}}
}

func restoreSkipColumns(width int) []components.Column {
	return []components.Column{{Title: "CATEGORY", Width: 18, MinWidth: 10}, {Title: "TARGET", Width: 42, MinWidth: 12}, {Title: "REASON", MinWidth: 12}}
}

func restoreCategoryLabel(provider string) string {
	switch provider {
	case "plugins":
		return "Installed plugins"
	case "defaults":
		return "Defaults"
	case "":
		return "Other"
	default:
		return titleFor(provider)
	}
}

func operationAction(op model.Operation) string {
	if operationOutcome(op) == restoreOutcomeDelete {
		return "Remove"
	}
	if op.Action != "" {
		return titleMode(op.Action)
	}
	return titleMode(string(operationOutcome(op)))
}

func operationRisk(op model.Operation) string {
	if op.Risk == "" {
		return "Normal"
	}
	return titleMode(string(op.Risk))
}

func styleRestoreAction(styles components.Styles, action string, selected bool) string {
	if selected {
		return action
	}
	if action == "Remove" {
		return styles.Removed(action)
	}
	return styles.Added(action)
}

func styleRestoreRisk(styles components.Styles, risk string, selected bool) string {
	if selected {
		return risk
	}
	switch risk {
	case "High":
		return styles.Error(risk)
	case "Medium":
		return styles.Warning(risk)
	default:
		return styles.Muted(risk)
	}
}

func skipReasonLabel(reason string) string {
	lower := strings.ToLower(reason)
	if strings.Contains(lower, "restore disabled") || strings.Contains(lower, "policy") {
		return "Policy: Skip"
	}
	if strings.Contains(lower, "additional") && strings.Contains(lower, "removal disabled") {
		return "Additive"
	}
	if strings.Contains(lower, "overwrite disabled") || strings.Contains(lower, "conflict") {
		return "Safe"
	}
	if strings.Contains(lower, "blocked") || strings.Contains(lower, "unsafe") || strings.Contains(lower, "not safe") {
		return "Safety"
	}
	return "Skipped"
}

func (s *Restore) apply() tea.Cmd {
	var options *policy.RestoreOptions
	if s.override {
		options = &s.options
	}
	return func() tea.Msg {
		if s.session == nil {
			return restoreAppliedMsg{err: fmt.Errorf("restore session is unavailable")}
		}
		result, err := s.session.ApplyRestore(s.ctx, restoreScopeAll, options)
		return restoreAppliedMsg{result, err}
	}
}
func (s *Restore) plan() model.RestorePlan { return s.current }
func (s *Restore) currentEntryCount() int {
	return len(s.current.Operations) + len(s.current.Skipped)
}
func (s *Restore) hasInteractiveOperation() bool {
	for _, op := range s.plan().Operations {
		if op.Interactive {
			return true
		}
	}
	return false
}
func (s *Restore) CanApply() bool {
	return !s.planning && len(s.plan().Operations) > 0 && !s.hasInteractiveOperation()
}

type restoreCounts struct{ create, modify, replace, delete, commands int }

type restoreOutcome string

const (
	restoreOutcomeCreate  restoreOutcome = "create"
	restoreOutcomeModify  restoreOutcome = "modify"
	restoreOutcomeReplace restoreOutcome = "replace"
	restoreOutcomeDelete  restoreOutcome = "delete"
)

func outcomeCounts(plan model.RestorePlan) restoreCounts {
	var counts restoreCounts
	for _, op := range plan.Operations {
		switch operationOutcome(op) {
		case restoreOutcomeCreate:
			counts.create++
		case restoreOutcomeModify:
			counts.modify++
		case restoreOutcomeReplace:
			counts.replace++
		case restoreOutcomeDelete:
			counts.delete++
		}
		if len(op.Command) > 0 {
			counts.commands++
		}
	}
	return counts
}
func operationOutcome(op model.Operation) restoreOutcome {
	if op.Delete != nil || op.Action == "remove" {
		return restoreOutcomeDelete
	}
	if op.File != nil {
		if op.File.ReplaceExisting {
			return restoreOutcomeReplace
		}
		if op.File.ExpectedMissing {
			return restoreOutcomeCreate
		}
		return restoreOutcomeModify
	}
	if op.Symlink != nil {
		if op.Symlink.ReplaceExisting {
			return restoreOutcomeReplace
		}
		if op.Symlink.ExpectedMissing {
			return restoreOutcomeCreate
		}
		return restoreOutcomeModify
	}
	if op.Directory != nil {
		return restoreOutcomeCreate
	}
	return restoreOutcomeModify
}
func policySkipCount(plan model.RestorePlan) int {
	count := 0
	for _, skipped := range plan.Skipped {
		if strings.HasPrefix(skipped.Reason, "restore disabled") {
			count++
		}
	}
	return count
}
func countForcedOverrides(forced, safe model.RestorePlan) int {
	safeByID := make(map[string]model.Operation, len(safe.Operations))
	for _, op := range safe.Operations {
		safeByID[op.ID] = op
	}
	count := 0
	for _, op := range forced.Operations {
		if safeOp, ok := safeByID[op.ID]; !ok || !reflect.DeepEqual(safeOp, op) {
			count++
		}
	}
	return count
}
func (s *Restore) confirmation() string {
	counts := outcomeCounts(s.plan())
	high := 0
	for _, op := range s.plan().Operations {
		if op.Risk == model.RiskHigh {
			high++
		}
	}
	s.ensureOptions()
	prompt := fmt.Sprintf(
		"Apply this restore plan? %d create, %d modify, %d replace, %d removals, %d commands (%s conflicts, %s convergence).",
		counts.create, counts.modify, counts.replace, counts.delete, counts.commands,
		titleMode(string(s.options.Conflicts)), titleMode(string(s.options.Convergence)),
	)
	if high > 0 {
		prompt += fmt.Sprintf(" %d high-risk.", high)
	}
	if s.forcedOverrides > 0 {
		prompt += fmt.Sprintf(" %d forced override(s).", s.forcedOverrides)
	}
	return prompt
}
