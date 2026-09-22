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
	err                     error
	current                 model.RestorePlan
	options                 policy.RestoreOptions
	override                bool
	forcedOverrides         int
	planRequestID           uint64
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
		s.current, s.forcedOverrides, s.err = msg.plan, msg.forcedOverrides, msg.err
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
	if s.busy {
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
		return fmt.Sprintf("Restore operation\nProvider: %s\nResource: %s\nAction: %s\nOutcome: %s\nRisk: %s\nInteractive: %t", components.DisplayText(op.Provider), components.DisplayText(op.Resource), components.DisplayText(op.Action), operationOutcome(op), components.DisplayText(string(op.Risk)), op.Interactive)
	}
	skipIndex := s.selected - len(s.current.Operations)
	if skipIndex >= 0 && skipIndex < len(s.current.Skipped) {
		skipped := s.current.Skipped[skipIndex]
		return fmt.Sprintf("Restore skip\nProvider: %s\nResource: %s\nReason: %s", components.DisplayText(skipped.Provider), components.DisplayText(skipped.Resource), components.DisplayText(skipped.Reason))
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
	lines := []string{machine, fmt.Sprintf("Conflicts: %s (f) · Convergence: %s (e)", s.options.Conflicts, s.options.Convergence)}
	if s.override {
		lines = append(lines, "One-run override active; machine defaults are unchanged.")
	}
	if s.options.Convergence == policy.ConvergenceExact {
		lines = append(lines, "WARNING: Exact may remove Blueprint-managed desired-absent targets; Resource data is never deleted.")
	}
	lines = append(lines, fmt.Sprintf("Current plan: create:%d modify:%d replace:%d removals:%d commands:%d policy-skips:%d forced-overrides:%d", counts.create, counts.modify, counts.replace, counts.delete, counts.commands, policySkipCount(s.current), s.forcedOverrides))
	if len(s.current.Operations) == 0 && len(s.current.Skipped) == 0 {
		return s.wrapCurrentPlan(append(lines, "", "No restore operations required."))
	}
	entry := 0
	for _, op := range s.current.Operations {
		line := fmt.Sprintf("%s: %s", components.DisplayText(op.Provider), components.DisplayText(op.Resource))
		if operationOutcome(op) == restoreOutcomeDelete {
			line += " — delete"
		}
		if entry == s.selected {
			line = components.Icons.Selected + " " + line
		}
		lines = append(lines, line)
		entry++
	}
	for _, skipped := range s.current.Skipped {
		line := "Skip: " + components.DisplayText(skipped.Resource) + " — " + components.DisplayText(skipped.Reason)
		if entry == s.selected {
			line = components.Icons.Selected + " " + line
		}
		lines = append(lines, line)
		entry++
	}
	return s.wrapCurrentPlan(lines)
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
	return len(s.plan().Operations) > 0 && !s.hasInteractiveOperation()
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
