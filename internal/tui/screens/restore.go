package screens

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/compatibility"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Restore presents the one current plan for the selected machine and one-run
// options. Applying always asks workflow to create a fresh plan, never the
// previewed one.
type Restore struct {
	readPresentation
	ctx                     context.Context
	session                 *workflow.Session
	width, height, selected int
	styles                  components.Styles
	confirm                 bool
	reviewingActivation     bool
	activationSelected      int
	activationChoices       []string
	compatibilityExpanded   bool
	busy                    bool
	planning                bool
	err                     error
	current                 model.RestorePlan
	options                 policy.RestoreOptions
	override                bool
	// deferred is this run's explicit deferral of categories that are not
	// ready (ADR 0028). It is never persisted.
	deferred        []string
	forcedOverrides int
	// run is the in-progress apply's shared state; stopApply cancels it
	// once stopRequested confirms (Ctrl+C twice); the running step is asked to stop.
	run           *restoreRunState
	stopRequested bool
	stopApply     context.CancelFunc
	planRequestID uint64
	changesTable  components.Table
	skipsTable    components.Table
	settingsTable components.Table
	summaryTable  components.Table
	// entryOffset is the first visible line of the Changes/Skipped region
	// when the plan is taller than the workspace.
	entryOffset int
	// exec hands the terminal to a blocking command. By default it asks the
	// root model to do so (TerminalRequest).
	exec func(tea.ExecCommand, tea.ExecCallback) tea.Cmd
	// lastRun is the outcome of the last apply, shown above the refreshed
	// plan; lastRunExpanded shows it in full in the details pane (o).
	lastRun         *restoreRun
	lastRunExpanded bool
}

func (s *Restore) scope() workflow.RestoreScope {
	return workflow.RestoreScope{Defer: append([]string(nil), s.deferred...)}
}

// TerminalRequest asks the root model to hand the real terminal to Command
// and deliver Done's message back to the requesting screen. A screen cannot
// return tea.Exec itself: the root routes each screen command's message
// back to its screen, and Bubble Tea would never see the exec request.
type TerminalRequest struct {
	Command tea.ExecCommand
	Done    tea.ExecCallback
}

func requestTerminal(command tea.ExecCommand, done tea.ExecCallback) tea.Cmd {
	return func() tea.Msg { return TerminalRequest{Command: command, Done: done} }
}

type restoreAppliedMsg struct {
	result workflow.RestoreResult
	err    error
}

// restoreRun is the outcome of the last apply.
type restoreRun struct {
	result workflow.RestoreResult
	err    error
}

// restoreLastRunLines bounds the last-run section so a run with many
// failures cannot push the plan off screen; o shows everything.
const restoreLastRunLines = 6

func (r *restoreRun) failedCount() int { return len(r.result.Execution.Failed) }

// headline sums up the last apply in one line.
func (r *restoreRun) headline() string {
	execution := r.result.Execution
	switch {
	case r.failedCount() > 0:
		return fmt.Sprintf("Last restore: %d %s failed, %d completed", r.failedCount(), pluralWord(r.failedCount(), "step", "steps"), len(execution.Completed))
	case errors.Is(r.err, context.Canceled):
		return fmt.Sprintf("Last restore: stopped by you after %d completed %s; plan again to finish it", len(execution.Completed), pluralWord(len(execution.Completed), "step", "steps"))
	case r.err != nil && !r.result.Applied:
		return "Last restore didn't run: " + components.DisplayText(r.err.Error())
	case r.err != nil:
		return "Last restore stopped: " + components.DisplayText(r.err.Error())
	case len(execution.SkippedByYou) > 0:
		return "Last restore: done, except what you skipped (it's still in the plan below)"
	case !r.result.Verification.OK && r.result.Applied:
		return "Last restore: applied, but some targets still don't match the profile"
	default:
		return "Last restore: applied and verified"
	}
}

// report is every detail of the last apply, for the details pane.
func (r *restoreRun) report() []string {
	execution := r.result.Execution
	lines := []string{"Last restore", r.headline()}
	if len(execution.Failed) > 0 {
		lines = append(lines, "", "Failed:")
		for _, failure := range execution.Failed {
			lines = append(lines, "✗ "+restoreStepLabel(failure.Operation)+": "+components.DisplayText(failure.Error))
		}
	}
	if len(execution.SkippedByYou) > 0 {
		lines = append(lines, "", "Skipped by you:")
		for _, op := range execution.SkippedByYou {
			lines = append(lines, "↷ "+restoreStepLabel(op))
		}
	}
	if len(execution.Blocked) > 0 {
		lines = append(lines, "", "Held back (they depend on a step that didn't complete):")
		for _, blocked := range execution.Blocked {
			lines = append(lines, "↷ "+restoreStepLabel(blocked.Operation)+" (waits on "+components.DisplayText(blocked.Dependency)+")")
		}
	}
	if missing := r.result.Verification.Missing; len(missing) > 0 {
		lines = append(lines, "", "Still not matching the profile:")
		for _, item := range missing {
			lines = append(lines, "  "+components.DisplayText(item))
		}
	}
	if r.result.Journal != "" {
		lines = append(lines, "", "Journal: "+components.DisplayText(r.result.Journal))
	}
	return lines
}

// lastRunLines summarizes the last apply in at most limit lines: what
// failed and why first, then what was skipped or held back, and where to
// see everything.
func (s *Restore) lastRunLines(width, limit int) []string {
	run := s.lastRun
	execution := run.result.Execution
	style := s.styles.Muted
	if run.failedCount() > 0 || run.err != nil {
		style = s.styles.Error
	} else if len(execution.SkippedByYou) > 0 || (!run.result.Verification.OK && run.result.Applied) {
		style = s.styles.Warning
	}
	headline := run.headline()
	if len(run.report()) > 2 {
		headline += " (o details)"
	}
	lines := []string{style(components.WrapText(headline, width)[0])}
	var detail []string
	for _, failure := range execution.Failed {
		detail = append(detail, "✗ "+restoreStepLabel(failure.Operation)+": "+components.DisplayText(failure.Error))
	}
	if len(execution.SkippedByYou) > 0 {
		names := make([]string, 0, len(execution.SkippedByYou))
		for _, op := range execution.SkippedByYou {
			names = append(names, restoreStepLabel(op))
		}
		detail = append(detail, "↷ Skipped by you: "+strings.Join(names, ", "))
	}
	if len(execution.Blocked) > 0 {
		detail = append(detail, fmt.Sprintf("↷ %d held back because they depend on a step that didn't complete", len(execution.Blocked)))
	}
	if run.result.Journal != "" && run.failedCount() > 0 {
		detail = append(detail, "Journal: "+components.DisplayText(run.result.Journal))
	}
	room := limit - 1
	for i, line := range detail {
		if room <= 0 {
			break
		}
		if room == 1 && i < len(detail)-1 {
			lines = append(lines, s.styles.Muted(fmt.Sprintf("… %d more (o details)", len(detail)-i)))
			break
		}
		lines = append(lines, style(components.WrapText(line, width)[0]))
		room--
	}
	return lines
}

func restoreStepLabel(op model.Operation) string {
	if op.Label != "" {
		return components.DisplayText(op.Label)
	}
	return operationAction(op) + " " + components.DisplayText(op.Resource)
}

// restoreProgressLine is one terminal line per step for the terminal
// handoff; long steps report about every 30 seconds.
func restoreProgressLine(event restore.Progress) string {
	label := restoreStepLabel(event.Operation)
	switch event.Type {
	case restore.ProgressStarted:
		if event.Operation.AwaitsYou != "" {
			return "→ " + label + "\n! This step waits for you: " + components.DisplayText(event.Operation.AwaitsYou) + ". (Ctrl+C skips this step; the rest of the restore continues.)"
		}
		return "→ " + label
	case restore.ProgressSkipped:
		return "↷ Skipped " + label + " (you pressed Ctrl+C); continuing with the rest of the restore"
	case restore.ProgressCompleted:
		return fmt.Sprintf("✓ %s (%s)", label, event.Elapsed.Round(time.Second))
	case restore.ProgressFailed:
		return fmt.Sprintf("✗ %s failed after %s", label, event.Elapsed.Round(time.Second))
	case restore.ProgressHeartbeat:
		if int(event.Elapsed.Seconds())%30 < 5 {
			line := fmt.Sprintf("  still running: %s (%s)", label, event.Elapsed.Round(time.Second))
			if event.Operation.AwaitsYou != "" {
				line += "; it may be waiting for you to " + components.DisplayText(event.Operation.AwaitsYou) + " (Ctrl+C skips this step; the rest of the restore continues)"
			}
			return line
		}
	}
	return ""
}

type restorePlanMsg struct {
	requestID       uint64
	plan            model.RestorePlan
	forcedOverrides int
	err             error
	preview         *workflow.RestorePreview
}

func NewRestore(session *workflow.Session) *Restore {
	return NewRestoreContext(context.Background(), session)
}
func NewRestoreContext(ctx context.Context, session *workflow.Session) *Restore {
	return &Restore{ctx: ctx, session: session, options: policy.DefaultRestoreOptions(), exec: requestTerminal}
}
func (s *Restore) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Restore) SetSize(width, height int)          { s.width, s.height = width, height }
func (s *Restore) Init() tea.Cmd {
	if s.session == nil {
		return nil
	}
	return s.refreshPlan()
}
func (s *Restore) TransientActive() bool { return s.confirm || s.reviewingActivation }

func (s *Restore) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case restorePlanMsg:
		if msg.requestID != s.planRequestID {
			return nil
		}
		s.current, s.forcedOverrides, s.err, s.planning = msg.plan, msg.forcedOverrides, msg.err, false
		if msg.err == nil && msg.preview != nil {
			s.options = msg.preview.Options
			s.acceptSnapshot(workflow.ReadSnapshot{Profile: msg.preview.Profile, Machine: msg.preview.Machine})
		}
		s.selected = min(s.selected, max(0, s.currentEntryCount()-1))
		return nil
	case restoreEventMsg:
		return s.handleRestoreEvent(msg)
	case restoreAppliedMsg:
		// An apply outcome, failed or not, is reported above the plan; it is
		// never a planning error. The machine may have changed either way, so
		// always plan again.
		s.confirm, s.busy, s.stopRequested, s.stopApply = false, false, false, nil
		s.lastRun = &restoreRun{result: msg.result, err: msg.err}
		return s.refreshPlan()
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if s.reviewingActivation {
		return s.updateActivationReview(key.String())
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
	if s.err != nil && key.String() != "r" {
		// The plan could not be prepared, so there is nothing on screen to
		// navigate or apply; only planning again does anything.
		return nil
	}
	switch key.String() {
	case "a":
		s.ensureOptions()
		switch s.options.ActivationMode() {
		case policy.ActivationPersistentOnly:
			s.options.Activation = policy.ActivationRestoreWorkingState
		case policy.ActivationRestoreWorkingState:
			s.options.Activation = policy.ActivationReview
		default:
			s.options.Activation = policy.ActivationPersistentOnly
		}
		s.options.ReviewActivation = nil
		s.override = true
		return s.refreshPlan()
	case "v":
		s.compatibilityExpanded = !s.compatibilityExpanded
		s.layoutPlan()
	case "r":
		return s.refreshPlan()
	case "o":
		if s.lastRun != nil {
			s.lastRunExpanded = !s.lastRunExpanded
		}
	case "d":
		if len(s.deferred) > 0 {
			s.deferred = nil
			return s.refreshPlan()
		}
		if s.CanDefer() {
			s.deferred = compatibility.NotReadyCategories(s.current)
			return s.refreshPlan()
		}
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
		if s.options.ActivationMode() == policy.ActivationReview && s.options.ReviewActivation == nil && len(s.current.ActivationReview) > 0 && workflow.CheckRestoreApplicable(s.current, true) == nil {
			s.reviewingActivation, s.activationSelected, s.activationChoices = true, 0, nil
			return nil
		}
		if s.CanApply() {
			s.confirm = true
			return func() tea.Msg {
				return components.ModalRequest{Title: "Apply restore", Content: components.Confirm(s.confirmation())}
			}
		}
	}
	return nil
}

func (s *Restore) View() string {
	if s.reviewingActivation {
		return s.activationReviewView()
	}
	if s.err != nil {
		return strings.Join(append(components.WrapText("Unable to prepare restore: "+components.DisplayText(s.err.Error()), s.widthOrDefault()), "", "Press r to plan again."), "\n")
	}
	if s.busy {
		return s.busyView()
	}
	if s.planning {
		return "Refreshing plan..."
	}
	if s.session != nil && !profileHasCapturedState(s.desiredProfile(s.session)) {
		return renderEmptyState(s.styles, s.width, emptyStateCopy{
			Heading:     "Nothing to restore yet",
			Explanation: "Restore recreates state that is already saved in this Blueprint profile.",
			Guidance:    "Capture the parts of this machine you want Blueprint to remember before using Restore.",
		})
	}
	return s.currentPlanView()
}
func (s *Restore) DetailView() string {
	if s.lastRunExpanded && s.lastRun != nil {
		return strings.Join(s.lastRun.report(), "\n")
	}
	if s.compatibilityExpanded {
		return strings.Join(s.compatibilityReportLines(s.widthOrDefault(), false), "\n")
	}
	if s.selected < len(s.current.Operations) {
		op := s.current.Operations[s.selected]
		detail := fmt.Sprintf("Restore operation\nCategory: %s\nTarget: %s\nAction: %s\nOutcome: %s\nRisk: %s\nInteractive: %t\n%s", components.DisplayText(restoreCategoryLabel(op.Provider)), components.DisplayText(op.Resource), operationAction(op), titleMode(string(operationOutcome(op))), operationRisk(op), op.Interactive, components.DisplayText(op.Notice))
		if op.AwaitsYou != "" {
			detail += "\nWaits for you: " + components.DisplayText(op.AwaitsYou)
		}
		return detail
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
	if s.readSnapshot != nil {
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
		copied := effectiveOptions
		options = &copied
	}
	session, ctx, scope := s.session, s.ctx, s.scope()
	return func() tea.Msg {
		cycle, err := session.BeginRead(ctx)
		if err != nil {
			return restorePlanMsg{requestID: requestID, err: err}
		}
		defer cycle.Close()
		preview, err := cycle.PreviewRestoreScope(ctx, scope, options)
		if err != nil {
			return restorePlanMsg{requestID: requestID, err: err}
		}
		plan := preview.Plan
		effectiveOptions = preview.Options
		forcedOverrides := 0
		if effectiveOptions.Conflicts == policy.ConflictForce {
			safe := effectiveOptions
			safe.Conflicts = policy.ConflictSafe
			if safePreview, safeErr := cycle.PreviewRestoreScope(ctx, scope, &safe); safeErr == nil {
				forcedOverrides = countForcedOverrides(plan, safePreview.Plan)
			}
		}
		return restorePlanMsg{requestID: requestID, plan: plan, forcedOverrides: forcedOverrides, preview: &preview}
	}
}
func (s *Restore) currentPlanView() string {
	header, entries := s.layoutPlan()
	if entries == nil {
		message := "No restore operations required."
		var blocked *workflow.BlockedCompatibilityError
		var unmet *workflow.UnmetRequirementsError
		applicability := workflow.CheckRestoreApplicable(s.plan(), true)
		if errors.As(applicability, &blocked) || errors.As(applicability, &unmet) {
			message = "Restore can't apply yet; see what isn't ready above."
		}
		return strings.Join(append(header, "", message), "\n")
	}
	return strings.Join(append(append(header, ""), entries...), "\n")
}

// A compact blocked plan needs a heading, category state, and finding before
// optional provenance or summary lines.
const restoreMinBlockerLines = 3

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
		if s.height > 0 && len(header)+2 > s.height {
			header = s.compactPlanHeader(width)
		}
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
			{Cells: []string{"Activation (a)", activationLabel(s.options.ActivationMode()), "Explicit permission for this run"}},
			{Cells: []string{"Defaults", defaults, defaultsMeaning}},
		}, width, 6, s.styles,
	)
	sections := []string{components.SectionDivider("Run settings", width, s.styles) + "\n" + settings}
	if s.lastRun != nil {
		sections = append([]string{strings.Join(s.lastRunLines(width, restoreLastRunLines), "\n")}, sections...)
	}
	if deferred := s.deferredLine(width); deferred != "" {
		sections = append(sections, deferred)
	}
	if len(s.current.ActivationReview) > 0 {
		sections = append(sections, "Review activation: press Enter to select individual starts before applying.")
	}
	if s.options.Convergence == policy.ConvergenceExact {
		sections = append(sections, s.wrapCurrentPlan([]string{"WARNING: Exact may remove Blueprint-managed desired-absent targets; Resource data is never deleted."}))
	}
	if compatibility := s.compatibilityLines(width, false); len(compatibility) > 0 {
		sections = append(sections, strings.Join(compatibility, "\n"))
	}
	if len(s.unlinkedRequirements()) > 0 {
		sections = append(sections, components.SectionDivider("Requires before applying", width, s.styles)+"\n"+s.requirementsView())
	}
	summary := s.summaryTable.Render(
		[]components.Column{{Title: "CREATE", MinWidth: 6}, {Title: "MODIFY", MinWidth: 6}, {Title: "REPLACE", MinWidth: 7}, {Title: "REMOVALS", MinWidth: 8}, {Title: "COMMANDS", MinWidth: 8}, {Title: "POLICY SKIPS", MinWidth: 12}, {Title: "FORCED OVERRIDES", MinWidth: 16}},
		[]components.Row{{Cells: []string{fmt.Sprint(counts.create), fmt.Sprint(counts.modify), fmt.Sprint(counts.replace), fmt.Sprint(counts.delete), fmt.Sprint(counts.commands), fmt.Sprint(policySkipCount(s.current)), fmt.Sprint(s.forcedOverrides)}}},
		width, 2, s.styles,
	)
	sections = append(sections, components.SectionDivider("Plan summary", width, s.styles)+"\n"+summary)
	return strings.Split(strings.Join(sections, "\n\n"), "\n")
}

// compactPlanHeader keeps run settings and counts as wrapped lines. At the
// smallest viewport a blocked finding takes precedence over expanded counts
// and the Exact warning (the run line still names Exact).
func (s *Restore) compactPlanHeader(width int) []string {
	counts := outcomeCounts(s.current)
	machine, conflicts, _, convergence, _, defaults, _ := s.runSettings()
	lines := []string{}
	if s.lastRun != nil {
		lines = append(lines, s.lastRunLines(width, 1)...)
	}
	for _, line := range components.WrapText(fmt.Sprintf("Run: %s (f) · %s (e) · %s activation (a) · %s · %s", conflicts, convergence, activationLabel(s.options.ActivationMode()), defaults, machine), width) {
		lines = append(lines, s.styles.SubtleAccent(line))
	}
	if s.options.Convergence == policy.ConvergenceExact {
		for _, line := range components.WrapText("! Exact removes Blueprint-managed extras; Resource data is never deleted.", width) {
			lines = append(lines, s.styles.Warning(line))
		}
	}
	if deferred := s.deferredLine(width); deferred != "" {
		lines = append(lines, strings.Split(deferred, "\n")...)
	}
	requirementLines := s.compactRequirementLines(width)
	summary := fmt.Sprintf("Plan: %d create · %d modify · %d replace · %d removals · %d commands · %d policy skips · %d forced", counts.create, counts.modify, counts.replace, counts.delete, counts.commands, policySkipCount(s.current), s.forcedOverrides)
	summaryLines := components.WrapText(summary, width)
	reserve := 2 // blank line and no-operations message
	if s.currentEntryCount() > 0 {
		reserve = 1 + 3 // blank line and selected entry viewport
	}
	budget := s.height - len(lines) - len(requirementLines) - len(summaryLines) - reserve
	var blocked *workflow.BlockedCompatibilityError
	if errors.As(workflow.CheckRestoreApplicable(s.plan(), true), &blocked) && budget < restoreMinBlockerLines {
		// At the application's 70x18 minimum, the blocker and its target are
		// more important than counts or provenance. Keep the selected work row.
		summaryLines = components.WrapText(fmt.Sprintf("Plan: %d changes · %d skips", len(s.current.Operations), len(s.current.Skipped)), width)
		budget = s.height - len(lines) - len(requirementLines) - len(summaryLines) - reserve
		if budget < restoreMinBlockerLines {
			summaryLines = nil
			budget = s.height - len(lines) - len(requirementLines) - reserve
		}
		if budget < restoreMinBlockerLines && len(requirementLines) > 1 {
			first := s.unlinkedRequirements()[0]
			hint := "Requires: " + components.DisplayText(strings.Join(first.Remediation, " "))
			requirementLines = []string{s.styles.Warning(components.WrapText(hint, width)[0])}
			budget = s.height - len(lines) - len(requirementLines) - reserve
		}
		if budget < restoreMinBlockerLines && len(lines) > 1 {
			lines = lines[:1] // Exact is still visible in the Run line.
			budget = s.height - len(lines) - len(requirementLines) - reserve
		}
	}
	lines = append(lines, s.compactCompatibilityLines(width, budget)...)
	lines = append(lines, requirementLines...)
	return append(lines, summaryLines...)
}

func (s *Restore) compactRequirementLines(width int) []string {
	requirements := s.unlinkedRequirements()
	if len(requirements) == 0 {
		return nil
	}
	first := requirements[0]
	remediation := components.WrapText("Run "+components.DisplayText(strings.Join(first.Remediation, " "))+", then press r: "+components.DisplayText(first.Reason), width)
	lines := []string{s.styles.Warning("Requires before applying")}
	lines = append(lines, remediation[0])
	if len(requirements) > 1 {
		more := fmt.Sprintf("… %d more requirements (expand Restore)", len(requirements)-1)
		lines = append(lines, s.styles.Muted(components.WrapText(more, width)[0]))
	} else if len(remediation) > 1 {
		lines = append(lines, s.styles.Muted(components.WrapText("… more readiness detail (expand Restore)", width)[0]))
	}
	return lines
}

// compatibilityLines renders the plan's own category states. Full views keep
// report order; compact views prioritize blocked/reduced findings so a long
// report cannot push the reason Apply is disabled beyond the viewport.
func (s *Restore) compatibilityLines(width int, compact bool) []string {
	if s.compatibilityExpanded {
		return s.compatibilityReportLines(width, compact)
	}
	report := s.current.Compatibility
	if len(report.Categories) == 0 && !report.Target.Known && !report.ProfileLastCapture.Known {
		return nil
	}
	limited := 0
	blocked := []model.CompatibilityCategory{}
	for _, category := range report.Categories {
		if !category.Applies {
			continue
		}
		if category.Authority == model.CompatibilityReduced {
			limited++
		}
		if category.Authority == model.CompatibilityBlocked {
			blocked = append(blocked, category)
		}
	}
	if len(blocked) == 0 {
		if limited > 0 {
			return components.WrapText(s.styles.Warning(fmt.Sprintf("Compatibility: %d limited — some operations withheld (v details)", limited)), width)
		}
		return components.WrapText(s.styles.Muted("Compatibility: Ready · no compatibility blockers (v details)"), width)
	}
	return s.blockerLines(width, len(blocked))
}

// restoreBlockerGroupBudget bounds the blocker section so it can never push
// the plan off screen, however many targets are blocked (ADR 0028).
const restoreBlockerGroupBudget = 4

// blockerLines summarizes what blocks the plan: one entry per category and
// cause with a target count, the fix when a requirement names one, and the
// ways forward. Every finding stays available in the details view (v).
func (s *Restore) blockerLines(width, categories int) []string {
	heading := fmt.Sprintf("Compatibility: Blocked · %d %s not ready (v details)", categories, pluralWord(categories, "category", "categories"))
	lines := components.WrapText(s.styles.Error(heading), width)
	states := map[string]model.CompatibilityState{}
	for _, category := range s.current.Compatibility.Categories {
		states[category.Category] = category.State
	}
	groups := compatibility.BlockerGroups(s.current)
	for i, group := range groups {
		if i == restoreBlockerGroupBudget {
			lines = append(lines, s.styles.Muted(components.WrapText(fmt.Sprintf("… %d more causes (v details)", len(groups)-i), width)[0]))
			break
		}
		count := ""
		if len(group.Targets) > 1 {
			count = fmt.Sprintf(" · %d targets", len(group.Targets))
		} else if len(group.Targets) == 1 {
			count = " · " + group.Targets[0]
		}
		title := components.DisplayText(restoreCategoryLabel(group.Category) + " · " + titleMode(string(states[group.Category])) + " · Blocked" + count)
		for _, line := range components.WrapText(title, width) {
			lines = append(lines, s.styles.Error(line))
		}
		reason := components.WrapText(components.DisplayText(group.Summary), max(1, width-2))
		if len(reason) > 2 {
			reason = append(reason[:1], strings.TrimSuffix(reason[1], " ")+" …")
		}
		for _, line := range reason {
			lines = append(lines, s.styles.Error("  "+line))
		}
		if group.Requirement != nil {
			fix := "  Fix: run `" + strings.Join(group.Requirement.Remediation, " ") + "` in a terminal, then press r to plan again."
			lines = append(lines, components.WrapText(s.styles.Warning(components.DisplayText(fix)), width)...)
		}
	}
	return append(lines, s.nextStepLines(width)...)
}

// nextStepLines states the ways forward from a plan that cannot apply.
// Force is never offered: it does not override compatibility.
func (s *Restore) nextStepLines(width int) []string {
	text := "Fix the cause and press r to plan again"
	if s.CanDefer() {
		labels := []string{}
		for _, category := range compatibility.NotReadyCategories(s.current) {
			labels = append(labels, restoreCategoryLabel(category))
		}
		text += ", or press d to restore everything else now and leave " + strings.Join(labels, ", ") + " for later"
	}
	return components.WrapText(s.styles.SubtleAccent(text+"."), width)
}

// deferredLine names the categories this run leaves out and how to bring
// them back; they are planned again only when the person asks.
func (s *Restore) deferredLine(width int) string {
	if len(s.current.Deferred) == 0 {
		return ""
	}
	labels := make([]string, 0, len(s.current.Deferred))
	for _, category := range s.current.Deferred {
		labels = append(labels, restoreCategoryLabel(category))
	}
	text := "Deferred for later: " + strings.Join(labels, ", ") + " · not part of this restore · press d to plan them again"
	return strings.Join(components.WrapText(s.styles.Warning(components.DisplayText(text)), width), "\n")
}

// HasLastRun reports whether an apply outcome is available to show.
func (s *Restore) HasLastRun() bool { return s.lastRun != nil }

// CanDefer reports whether d would narrow this plan: something is not ready,
// nothing is deferred yet, and at least one category would remain.
func (s *Restore) CanDefer() bool {
	if len(s.deferred) > 0 || s.planning || s.busy || s.err != nil {
		return false
	}
	notReady := compatibility.NotReadyCategories(s.current)
	return len(notReady) > 0 && len(notReady) < len(s.current.Compatibility.Categories)
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s *Restore) compatibilityReportLines(width int, compact bool) []string {
	report := s.current.Compatibility
	if len(report.Categories) == 0 && !report.Target.Known && !report.ProfileLastCapture.Known {
		return nil
	}
	label := "Compatibility"
	if compact {
		blocked, reduced := 0, 0
		for _, category := range report.Categories {
			switch category.Authority {
			case model.CompatibilityBlocked:
				blocked++
			case model.CompatibilityReduced:
				reduced++
			}
		}
		label = fmt.Sprintf("Compatibility · %d blocked · %d reduced", blocked, reduced)
	}
	lines := []string{components.SectionDivider(label, width, s.styles)}
	context := fmt.Sprintf("Profile last capture: %s · Target Omarchy: %s", restoreCompatibilityEnvironment(report.ProfileLastCapture), restoreCompatibilityEnvironment(report.Target))
	contextLines := components.WrapText(components.DisplayText(context), width)
	if !compact {
		lines = append(lines, contextLines...)
	}
	categories := append([]model.CompatibilityCategory(nil), report.Categories...)
	if compact {
		priority := func(category model.CompatibilityCategory) int {
			switch {
			case category.Authority == model.CompatibilityBlocked:
				return 0
			case category.Authority == model.CompatibilityReduced:
				return 1
			case category.State == model.CompatibilityUnknown:
				return 2
			case category.Applies:
				return 3
			default:
				return 4
			}
		}
		sort.SliceStable(categories, func(i, j int) bool { return priority(categories[i]) < priority(categories[j]) })
	}
	for _, category := range categories {
		state := "Not selected for Apply"
		if category.Applies {
			state = titleMode(string(category.State)) + " · " + titleMode(string(category.Authority))
		}
		row := components.DisplayText(restoreCategoryLabel(category.Category) + ": " + state)
		for _, line := range components.WrapText(row, width) {
			switch category.Authority {
			case model.CompatibilityBlocked:
				lines = append(lines, s.styles.Error(line))
			case model.CompatibilityReduced:
				lines = append(lines, s.styles.Warning(line))
			default:
				lines = append(lines, s.styles.Muted(line))
			}
		}
		findings := category.Findings
		if compact && category.Authority == model.CompatibilityBlocked {
			findings = append([]model.CompatibilityFinding(nil), findings...)
			sort.SliceStable(findings, func(i, j int) bool {
				return findings[i].Authority == model.CompatibilityBlocked && findings[j].Authority != model.CompatibilityBlocked
			})
		}
		for _, finding := range findings {
			if finding.State == model.CompatibilitySupported {
				continue
			}
			target := ""
			if finding.Target != "" {
				target = finding.Target + ": "
			}
			text := components.DisplayText("  " + target + finding.Summary)
			for _, line := range components.WrapText(text, width) {
				if finding.Authority == model.CompatibilityBlocked {
					lines = append(lines, s.styles.Error(line))
				} else if finding.Authority == model.CompatibilityReduced {
					lines = append(lines, s.styles.Warning(line))
				} else {
					lines = append(lines, s.styles.Muted(line))
				}
			}
		}
	}
	if compact {
		lines = append(lines, contextLines...)
	}
	return lines
}

func (s *Restore) compactCompatibilityLines(width, budget int) []string {
	if budget <= 0 {
		return nil
	}
	lines := s.compatibilityLines(width, true)
	if len(lines) <= budget {
		return lines
	}
	var blocked *workflow.BlockedCompatibilityError
	if budget <= restoreMinBlockerLines && errors.As(workflow.CheckRestoreApplicable(s.plan(), true), &blocked) {
		return lines[:budget]
	}
	if budget == 1 {
		return lines[:1]
	}
	more := fmt.Sprintf("… %d more compatibility lines (expand Restore)", len(lines)-budget+1)
	return append(lines[:budget-1], s.styles.Muted(components.WrapText(more, width)[0]))
}

func restoreCompatibilityEnvironment(env model.CompatibilityEnvironment) string {
	if !env.Known {
		return "unknown"
	}
	if env.OmarchyChannel != "" {
		return env.OmarchyVersion + " (" + env.OmarchyChannel + ")"
	}
	return env.OmarchyVersion
}

func (s *Restore) runSettings() (machine, conflicts, conflictMeaning, convergence, convergenceMeaning, defaults, defaultsMeaning string) {
	machine = "Profile defaults"
	if selection := s.selectedMachine(s.session); selection.Name != "" {
		machine = selection.Name
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

// busyView shows which step is running while a plan applies, so a long
// step is visibly working rather than looking stuck.
func (s *Restore) busyView() string {
	width := s.widthOrDefault()
	var run restoreRunSnapshot
	if s.run != nil {
		run = s.run.snapshot()
	}
	lines := []string{s.styles.SubtleAccent(fmt.Sprintf("Applying restore · %d of %d steps done", run.done, run.total))}
	if run.failed > 0 {
		lines = append(lines, s.styles.Error(fmt.Sprintf("%d failed so far; Restore continues with independent steps", run.failed)))
	}
	if run.skipped > 0 {
		lines = append(lines, s.styles.Warning(fmt.Sprintf("%d skipped by you", run.skipped)))
	}
	if run.current != "" {
		now := "Now: " + run.current
		if run.elapsed > 0 {
			now += " · " + run.elapsed.Round(time.Second).String()
		}
		lines = append(lines, components.WrapText(now, width)...)
	}
	lines = append(lines, "")
	if s.stopRequested {
		lines = append(lines, components.WrapText(s.styles.Warning("Press Ctrl+C again to stop the restore: the running step is asked to stop, and no further step starts."), width)...)
	}
	lines = append(lines, components.WrapText(s.styles.Muted("Steps that need you, such as a sudo password or a sign-in, borrow the terminal and Blueprint comes back afterwards. Other steps can't prompt: anything that would ask for a password stops with an error instead of waiting."), width)...)
	return strings.Join(lines, "\n")
}
func (s *Restore) plan() model.RestorePlan { return s.current }

// CurrentPlan exposes the shared domain plan used by preview and approval.
func (s *Restore) CurrentPlan() model.RestorePlan { return s.current }
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
	return s.ApplyDisabledReason() == ""
}

// ApplyDisabledReason keeps the command palette and Enter's availability in
// sync with the workflow's first-failure applicability order.
func (s *Restore) ApplyDisabledReason() string {
	switch {
	case s.planning:
		return "restore plan is refreshing"
	case s.busy:
		return "restore is applying"
	case s.err != nil:
		return "restore plan could not be prepared"
	}
	if err := workflow.CheckRestoreApplicable(s.plan(), true); err != nil {
		var blocked *workflow.BlockedCompatibilityError
		var unmet *workflow.UnmetRequirementsError
		switch {
		case errors.As(err, &blocked):
			return "some categories aren't ready; fix them and press r, or press d to defer them"
		case errors.As(err, &unmet):
			return "requirements must be completed first; press r after completing them"
		default:
			return "restore plan cannot be applied yet"
		}
	}
	if len(s.plan().Operations) == 0 {
		return "current restore plan has no operations"
	}
	return ""
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
	if s.hasInteractiveOperation() {
		prompt += " Some operations may ask for administrator authentication in this terminal; Blueprint steps aside while they run."
	}
	for _, op := range s.plan().Operations {
		if op.AwaitsYou != "" {
			prompt += " " + components.DisplayText(restoreStepLabel(op)) + " waits for you: " + components.DisplayText(op.AwaitsYou) + "."
		}
	}
	if s.options.ActivationMode() != policy.ActivationPersistentOnly {
		prompt += " Services activation: " + activationLabel(s.options.ActivationMode()) + "."
	}
	return prompt
}

// unlinkedRequirements are the requirements no blocker already shows with
// its fix, so the same instruction never appears twice.
func (s *Restore) unlinkedRequirements() []model.Requirement {
	shown := map[string]bool{}
	for _, group := range compatibility.BlockerGroups(s.current) {
		if group.Requirement != nil {
			shown[group.Requirement.ID] = true
		}
	}
	var requirements []model.Requirement
	for _, requirement := range s.current.Requirements {
		if !shown[requirement.ID] {
			requirements = append(requirements, requirement)
		}
	}
	return requirements
}

// requirementsView explains what the user must do before this plan applies.
func (s *Restore) requirementsView() string {
	var b strings.Builder
	for _, requirement := range s.unlinkedRequirements() {
		fmt.Fprintf(&b, "%s\nRun `%s` in a terminal, then press r to plan again.\n", components.DisplayText(requirement.Reason), components.DisplayText(strings.Join(requirement.Remediation, " ")))
	}
	return s.wrapCurrentPlan(strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n"))
}
