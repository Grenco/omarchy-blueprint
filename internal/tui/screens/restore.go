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
		}
	case "k", "up":
		if s.selected > 0 {
			s.selected--
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
	s.ensureOptions()
	machine := "Profile defaults"
	if s.session != nil && s.session.Machine().Name != "" {
		name := s.session.Machine().Name
		machine = "Machine: " + name
	}
	counts := outcomeCounts(s.current)
	lines := []string{machine, fmt.Sprintf("Conflicts: %s (f) · Convergence: %s (e)", titleMode(string(s.options.Conflicts)), titleMode(string(s.options.Convergence)))}
	if s.override {
		lines = append(lines, "One-run override active; machine defaults are unchanged.")
	}
	if s.options.Convergence == policy.ConvergenceExact {
		lines = append(lines, "WARNING: Exact may remove Blueprint-managed desired-absent targets; Resource data is never deleted.")
	}
	lines = append(lines, "", fmt.Sprintf("Current plan: create:%d modify:%d replace:%d removals:%d commands:%d policy-skips:%d forced-overrides:%d", counts.create, counts.modify, counts.replace, counts.delete, counts.commands, policySkipCount(s.current), s.forcedOverrides))
	if len(s.current.Operations) == 0 && len(s.current.Skipped) == 0 {
		return s.wrapCurrentPlan(append(lines, "", "No restore operations required."))
	}
	width := s.widthOrDefault()
	wrapped := s.wrapCurrentPlan(lines)
	sections := []string{wrapped}
	if len(s.current.Operations) > 0 {
		columns := restoreOperationColumns(width)
		rows := make([]components.Row, 0, len(s.current.Operations))
		for i, op := range s.current.Operations {
			action := operationAction(op)
			risk := operationRisk(op)
			cells := []string{components.DisplayText(restoreCategoryLabel(op.Provider)), components.DisplayText(op.Resource), styleRestoreAction(s.styles, action), styleRestoreRisk(s.styles, risk)}
			if len(columns) == 3 {
				cells = []string{components.DisplayText(op.Resource), styleRestoreAction(s.styles, action), styleRestoreRisk(s.styles, risk)}
			}
			rows = append(rows, components.Row{Cells: cells, Selected: i == s.selected, Focused: true})
		}
		sections = append(sections, "Changes", s.changesTable.Render(columns, rows, width, len(rows)+1, s.styles))
	}
	if len(s.current.Skipped) > 0 {
		rows := make([]components.Row, 0, len(s.current.Skipped))
		for i, skipped := range s.current.Skipped {
			rows = append(rows, components.Row{Cells: []string{components.DisplayText(restoreCategoryLabel(skipped.Provider)), components.DisplayText(skipped.Resource), s.styles.Warning(skipReasonLabel(skipped.Reason))}, Selected: len(s.current.Operations)+i == s.selected, Focused: true})
		}
		sections = append(sections, "Skipped by policy / safety / mode", s.skipsTable.Render(restoreSkipColumns(width), rows, width, len(rows)+1, s.styles))
	}
	return strings.Join(sections, "\n\n")
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

func styleRestoreAction(styles components.Styles, action string) string {
	if action == "Remove" {
		return styles.Removed(action)
	}
	return styles.Added(action)
}

func styleRestoreRisk(styles components.Styles, risk string) string {
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
	return fmt.Sprintf("Restore all captured providers? conflicts:%s convergence:%s create:%d modify:%d replace:%d removals:%d commands:%d policy-skips:%d forced-overrides:%d high-risk:%d", s.options.Conflicts, s.options.Convergence, counts.create, counts.modify, counts.replace, counts.delete, counts.commands, policySkipCount(s.plan()), s.forcedOverrides, high)
}
