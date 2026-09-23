package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// Capture composes existing provider capture operations into one discoverable screen.
type Capture struct {
	ctx                                  context.Context
	session                              *workflow.Session
	statuses                             []workflow.ProviderStatus
	selected, cursor                     int
	width, height                        int
	chosen                               map[string]bool
	table                                components.Table
	navigation                           components.Selectable
	styles                               components.Styles
	confirm, busy, capturing, captureAll bool
	err                                  error
	requestID                            uint64
	// review is non-nil while the chosen categories' proposed profile
	// changes are shown for approval. It is a preview only: approval runs
	// CaptureMany, which re-inspects and resolves policy afresh.
	review *captureReview
}

type captureReview struct {
	categories []string
	scope      workflow.PolicyScope
	sections   []workflow.CaptureReviewSection
	noAction   int
	cursor     int
	table      components.Table
	loading    bool
	requestID  uint64
	err        error
	notice     string
}

type captureReviewMsg struct {
	requestID     uint64
	inspection    workflow.CaptureInspection
	err           error
	focus         string
	policyChanged bool
}

type captureStatusMsg struct {
	requestID uint64
	statuses  []workflow.ProviderStatus
	err       error
}
type captureDoneMsg struct {
	requestID uint64
	providers []string
	err       error
	warning   error
}

func NewCaptureContext(ctx context.Context, session *workflow.Session) *Capture {
	return &Capture{ctx: ctx, session: session, chosen: map[string]bool{}}
}
func (s *Capture) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Capture) SetSize(width, height int) {
	s.width, s.height = width, height
	s.ensureCursorVisible()
}
func (s *Capture) Init() tea.Cmd         { return s.refresh() }
func (s *Capture) TransientActive() bool { return s.confirm || s.busy }
func (s *Capture) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case captureStatusMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.statuses, s.err, s.busy = msg.statuses, msg.err, false
		s.cursor = min(s.cursor, max(0, len(s.statuses)-1))
		s.ensureCursorVisible()
		return nil
	case captureReviewMsg:
		return s.applyReview(msg)
	case captureDoneMsg:
		if msg.requestID != s.requestID {
			return nil
		}
		s.err, s.busy, s.confirm, s.capturing, s.captureAll = msg.err, false, false, false, false
		s.review = nil
		if msg.err == nil {
			s.chosen = map[string]bool{}
		}
		return func() tea.Msg { return CaptureComplete{Providers: msg.providers, Err: msg.err, Warning: msg.warning} }
	}
	key, ok := msg.(tea.KeyPressMsg)
	// A background status reload must not freeze an open review.
	if !ok || s.capturing || (s.busy && s.review == nil) {
		return nil
	}
	if s.confirm {
		if key.String() == "esc" {
			s.confirm, s.captureAll = false, false
			return nil
		}
		if key.String() == "enter" {
			s.busy, s.confirm, s.capturing = true, false, true
			return s.capture()
		}
		return nil
	}
	if s.review != nil {
		return s.updateReview(key)
	}
	s.navigation.Selected = s.cursor
	if s.navigation.Vim(key.String(), len(s.statuses), max(1, s.height-2)) {
		s.cursor = s.navigation.Selected
		s.ensureCursorVisible()
		return nil
	}
	switch key.String() {
	case "j", "down":
		s.cursor = min(len(s.statuses)-1, s.cursor+1)
		s.ensureCursorVisible()
	case "k", "up":
		s.cursor = max(0, s.cursor-1)
		s.ensureCursorVisible()
	case "space", " ":
		if p := s.current(); p.ID != "" {
			s.chosen[p.ID] = !s.chosen[p.ID]
		}
	case "a":
		s.chosen = map[string]bool{}
		for _, p := range s.statuses {
			if len(p.Changes) > 0 {
				s.chosen[p.ID] = true
			}
		}
	case "c":
		s.captureAll = false
		return s.startReview()
	case "C", "shift+c":
		s.captureAll = true
		for _, p := range s.statuses {
			s.chosen[p.ID] = true
		}
		return s.startReview()
	case "r":
		return s.refresh()
	}
	return nil
}
func (s *Capture) View() string {
	if s.err != nil {
		return s.styles.Error("Capture failed: " + components.DisplayText(s.err.Error()))
	}
	if s.capturing {
		return "Capturing selected categories...\n\nThis can take a while as categories inspect the running system."
	}
	if s.review != nil {
		return s.reviewView()
	}
	if s.busy {
		return "Loading capture status..."
	}
	lines := []string{}
	if hint, ok := s.hint(); ok {
		lines = append(lines, hint, "")
	}
	rows := make([]components.Row, 0, len(s.statuses))
	for i, p := range s.statuses {
		selected := i == s.cursor
		changes, style := "not captured", s.styles.Muted
		if p.Captured {
			changes, style = "clean", s.styles.Success
		}
		if p.Captured && len(p.Changes) > 0 {
			changes, style = fmt.Sprintf("%d changed", len(p.Changes)), s.styles.Warning
		}
		if !selected {
			changes = style(changes)
		}
		check := " "
		if s.chosen[p.ID] {
			check = "x"
		}
		rows = append(rows, components.Row{Cells: []string{"[" + check + "]", components.DisplayText(p.ID), changes}, Selected: selected, Focused: true})
	}
	width := s.width
	if width <= 0 {
		width = 60
	}
	table := s.table.Render([]components.Column{{Title: "", Width: 3, MinWidth: 3}, {Title: "CATEGORY", Width: 24, MinWidth: 12}, {Title: "CHANGES", MinWidth: 8}}, rows, width, s.tableRowHeight(), s.styles)
	lines = append(lines, table)
	return strings.Join(lines, "\n")
}

// freshProfile reports whether no category has been captured yet, the
// condition under which the onboarding hint is offered.
func (s *Capture) freshProfile() bool {
	if len(s.statuses) == 0 {
		return false
	}
	for _, status := range s.statuses {
		if status.Captured {
			return false
		}
	}
	return true
}

func (s *Capture) hintWidth() int {
	hintWidth := s.width
	if hintWidth <= 0 || hintWidth > 60 {
		hintWidth = 60
	}
	return hintWidth
}

// hint returns the onboarding hint text and whether it should be shown. At
// constrained heights it compacts to a heading-only line rather than take
// room the category table needs; it is never shown at the expense of a
// category becoming unreachable.
func (s *Capture) hint() (string, bool) {
	if !s.freshProfile() {
		return "", false
	}
	full := renderEmptyState(s.styles, s.hintWidth(), emptyStateCopy{
		Heading:     "Choose what this profile should remember",
		Explanation: "Nothing has been captured yet. Select the categories you want in this profile; you do not need to capture everything.",
	})
	if s.height <= 0 || s.height-hintBlockHeight(full) >= minTableRowHeight {
		return full, true
	}
	compact := renderEmptyState(s.styles, s.hintWidth(), emptyStateCopy{
		Heading: "Choose what this profile should remember",
	})
	return compact, true
}

// minTableRowHeight is the smallest table height (header plus data rows)
// Capture tries to preserve for the category table before compacting or
// dropping onboarding prose; the table itself is never hidden below this.
const minTableRowHeight = 4

func hintBlockHeight(hint string) int {
	// +1 for the blank separator line View() appends after the hint.
	return strings.Count(hint, "\n") + 1 + 1
}

// tableRowHeight is the header+data-row height passed to Table.Render,
// budgeted from the remaining space after any onboarding hint.
func (s *Capture) tableRowHeight() int {
	if s.height <= 0 {
		return max(2, len(s.statuses)+1)
	}
	budget := s.height
	if hint, ok := s.hint(); ok {
		budget -= hintBlockHeight(hint)
	}
	return max(2, budget)
}

// ensureCursorVisible keeps the selected category within the table's
// scrolled viewport using the table's own Offset, rather than a second
// scrolling mechanism.
func (s *Capture) ensureCursorVisible() {
	s.table.Ensure(s.cursor, len(s.statuses), max(1, s.tableRowHeight()-1))
}
func (s *Capture) DetailView() string {
	if s.review != nil {
		return s.reviewDetail()
	}
	p := s.current()
	if p.ID == "" {
		return "Select categories to capture."
	}
	selected := "No"
	if s.chosen[p.ID] {
		selected = "Yes"
	}
	return "Category: " + components.DisplayText(p.ID) + "\nChanges: " + fmt.Sprint(len(p.Changes)) + "\nSelected for capture: " + selected
}
func (s *Capture) current() workflow.ProviderStatus {
	if s.cursor >= 0 && s.cursor < len(s.statuses) {
		return s.statuses[s.cursor]
	}
	return workflow.ProviderStatus{}
}
func (s *Capture) refresh() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	s.busy = true
	return func() tea.Msg {
		report, err := s.session.CaptureStatus(s.ctx)
		return captureStatusMsg{requestID: requestID, statuses: report.Providers, err: err}
	}
}
func (s *Capture) capture() tea.Cmd {
	s.requestID++
	requestID := s.requestID
	ids := s.chosenIDs()
	return func() tea.Msg {
		result, err := s.session.CaptureMany(s.ctx, ids)
		var warning workflow.PostCommitWarning
		if errors.As(err, &warning) {
			return captureDoneMsg{requestID: requestID, providers: result.Providers, warning: warning.Err}
		}
		return captureDoneMsg{requestID: requestID, providers: result.Providers, err: err}
	}
}
func (s *Capture) chosenCount() int {
	count := 0
	for _, chosen := range s.chosen {
		if chosen {
			count++
		}
	}
	return count
}

// chosenIDs is the one-run category choice Capture review and approval use.
func (s *Capture) chosenIDs() []string {
	ids := make([]string, 0, len(s.statuses))
	for _, status := range s.statuses {
		if s.captureAll || s.chosen[status.ID] {
			ids = append(ids, status.ID)
		}
	}
	return ids
}

func (s *Capture) startReview() tea.Cmd {
	ids := s.chosenIDs()
	if len(ids) == 0 {
		return nil
	}
	s.review = &captureReview{categories: ids, scope: initialPolicyScope(s.session)}
	return s.inspectReview("", false, nil)
}

// inspectReview recalculates the preview, optionally after a policy
// mutation, always through workflow so the screen never edits policy itself.
func (s *Capture) inspectReview(focus string, policyChanged bool, mutate func() error) tea.Cmd {
	review := s.review
	review.requestID++
	review.loading, review.err, review.notice = true, nil, ""
	requestID, ids, session, ctx := review.requestID, append([]string(nil), review.categories...), s.session, s.ctx
	return func() tea.Msg {
		if mutate != nil {
			if err := mutate(); err != nil {
				return captureReviewMsg{requestID: requestID, err: err, focus: focus}
			}
		}
		merged := workflow.CaptureInspection{Categories: map[string][]workflow.CaptureTarget{}}
		for _, id := range ids {
			inspection, err := session.InspectCapture(ctx, id)
			if err != nil {
				return captureReviewMsg{requestID: requestID, err: err, focus: focus, policyChanged: policyChanged}
			}
			for category, targets := range inspection.Categories {
				merged.Categories[category] = targets
			}
		}
		return captureReviewMsg{requestID: requestID, inspection: merged, focus: focus, policyChanged: policyChanged}
	}
}

func (s *Capture) applyReview(msg captureReviewMsg) tea.Cmd {
	review := s.review
	if review == nil || msg.requestID != review.requestID {
		return nil
	}
	review.loading, review.err = false, msg.err
	if msg.err == nil {
		review.sections, review.noAction = nil, 0
		for _, section := range msg.inspection.Review() {
			if section.Group == workflow.CaptureReviewNoAction {
				review.noAction = len(section.Targets)
				continue
			}
			review.sections = append(review.sections, section)
		}
		review.cursor = min(review.cursor, max(0, len(s.reviewTargets())-1))
		for i, target := range s.reviewTargets() {
			if reviewKey(target) == msg.focus {
				review.cursor = i
			}
		}
	}
	if msg.policyChanged {
		return func() tea.Msg { return AuthorityChanged{Notice: "Capture policy updated."} }
	}
	return nil
}

func reviewKey(target workflow.CaptureTarget) string {
	return target.Category + "\x00" + target.Inspection.Key
}

// reviewTargets are the listed, selectable review rows in display order;
// targets needing no action are only counted.
func (s *Capture) reviewTargets() []workflow.CaptureTarget {
	if s.review == nil {
		return nil
	}
	targets := []workflow.CaptureTarget{}
	for _, section := range s.review.sections {
		targets = append(targets, section.Targets...)
	}
	return targets
}

func (s *Capture) selectedReviewTarget() (workflow.CaptureTarget, bool) {
	targets := s.reviewTargets()
	if s.review == nil || s.review.cursor < 0 || s.review.cursor >= len(targets) {
		return workflow.CaptureTarget{}, false
	}
	return targets[s.review.cursor], true
}

func (s *Capture) updateReview(key tea.KeyPressMsg) tea.Cmd {
	review := s.review
	if review.loading {
		if key.String() == "esc" {
			s.review = nil
		}
		return nil
	}
	count := len(s.reviewTargets())
	switch key.String() {
	case "esc":
		s.review = nil
	case "j", "down":
		review.cursor = min(max(0, count-1), review.cursor+1)
	case "k", "up":
		review.cursor = max(0, review.cursor-1)
	case "g", "home":
		review.cursor = 0
	case "G", "shift+g", "end":
		review.cursor = max(0, count-1)
	case "r":
		target, _ := s.selectedReviewTarget()
		return s.inspectReview(reviewKey(target), false, nil)
	case "p":
		review.scope = togglePolicyScope(s.session, review.scope)
	case "space", " ", "x":
		target, ok := s.selectedReviewTarget()
		if !ok {
			return nil
		}
		if target.Outcome == workflow.CaptureOutcomeBlocked {
			review.notice = "Blocked by safety rules; Capture policy cannot override this."
			return nil
		}
		scope, session, category, targetKey := review.scope, s.session, target.Category, target.Inspection.Key
		if key.String() == "x" {
			return s.inspectReview(reviewKey(target), true, func() error {
				return session.ClearPolicy(scope, policy.AxisCapture, category, targetKey)
			})
		}
		setting := policy.SettingEnabled
		if target.Decision.Capture {
			setting = policy.SettingDisabled
		}
		return s.inspectReview(reviewKey(target), true, func() error {
			return session.SetPolicy(scope, policy.AxisCapture, category, targetKey, setting)
		})
	case "enter":
		return s.confirmCapture()
	}
	return nil
}

func (s *Capture) reviewCounts() map[workflow.CaptureReviewGroup]int {
	counts := map[workflow.CaptureReviewGroup]int{workflow.CaptureReviewNoAction: s.review.noAction}
	for _, section := range s.review.sections {
		counts[section.Group] = len(section.Targets)
	}
	return counts
}

func (s *Capture) confirmCapture() tea.Cmd {
	if len(s.chosenIDs()) == 0 {
		return nil
	}
	outcomes := map[workflow.CaptureOutcome]int{}
	for _, target := range s.reviewTargets() {
		outcomes[target.Outcome]++
	}
	parts := []string{}
	for _, item := range []struct {
		outcome workflow.CaptureOutcome
		label   string
	}{
		{workflow.CaptureOutcomeAdd, "to add"},
		{workflow.CaptureOutcomeUpdate, "to update"},
		{workflow.CaptureOutcomeAbsent, "to remember as absent"},
		{workflow.CaptureOutcomeStopManaging, "to stop managing"},
		{workflow.CaptureOutcomePreserve, "preserved by policy"},
		{workflow.CaptureOutcomeBlocked, "blocked"},
	} {
		if outcomes[item.outcome] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", outcomes[item.outcome], item.label))
		}
	}
	summary := "No profile changes were found."
	if len(parts) > 0 {
		summary = strings.Join(parts, " · ") + "."
	}
	s.confirm = true
	return func() tea.Msg {
		return components.ModalRequest{Title: "Capture selected categories", Content: components.Confirm("Capture into this profile? " + summary + "\n\nCapture re-checks this machine and your policy before writing, so the result may differ if anything changed since this review.")}
	}
}

var captureOutcomeLabels = map[workflow.CaptureOutcome]string{
	workflow.CaptureOutcomeAdd:          "+ Add",
	workflow.CaptureOutcomeUpdate:       "~ Update",
	workflow.CaptureOutcomeAbsent:       "− Remember absent",
	workflow.CaptureOutcomeStopManaging: "Stop managing",
	workflow.CaptureOutcomePreserve:     "Preserve",
	workflow.CaptureOutcomeBlocked:      "! Blocked",
	workflow.CaptureOutcomeNoop:         "No change",
}

var captureOutcomeMeaning = map[workflow.CaptureOutcome]string{
	workflow.CaptureOutcomeAdd:          "Capture will add this to the profile.",
	workflow.CaptureOutcomeUpdate:       "Capture will update the saved copy from this machine.",
	workflow.CaptureOutcomeAbsent:       "It is missing here, so Capture will remember that it should be absent.",
	workflow.CaptureOutcomeStopManaging: "Capture will stop managing it; nothing is remembered as absent.",
	workflow.CaptureOutcomePreserve:     "Capture keeps whatever the profile already has for it.",
	workflow.CaptureOutcomeBlocked:      "Safety rules keep Capture from changing it.",
	workflow.CaptureOutcomeNoop:         "Nothing to capture.",
}

func captureScopeLabel(session *workflow.Session, scope workflow.PolicyScope) string {
	if scope.Machine == "" {
		return "Policy scope: Profile defaults"
	}
	label := "Policy scope: " + components.DisplayText(scope.Machine)
	if scope.Machine == activeMachineName(session) {
		label += " (this machine)"
	}
	return label
}

func (s *Capture) reviewView() string {
	review := s.review
	counts := s.reviewCounts()
	width := s.width
	if width <= 0 {
		width = 100
	}
	// Header lines are wrapped here so the table's height budget below
	// counts the rows they really occupy.
	summary := "Review Capture  " + plural(counts[workflow.CaptureReviewChanges], "change", "changes") +
		fmt.Sprintf(" · %d preserved · %d blocked · %d no action needed", counts[workflow.CaptureReviewPreserved], counts[workflow.CaptureReviewBlocked], counts[workflow.CaptureReviewNoAction])
	lines := []string{}
	for i, line := range components.WrapText(summary, width) {
		if i == 0 && strings.HasPrefix(line, "Review Capture") {
			line = s.styles.Accent("Review Capture") + strings.TrimPrefix(line, "Review Capture")
		}
		lines = append(lines, line)
	}
	for _, line := range components.WrapText(captureScopeLabel(s.session, review.scope), width) {
		lines = append(lines, s.styles.SubtleAccent(line))
	}
	switch {
	case review.loading:
		return strings.Join(append(lines, "", "Inspecting "+strings.Join(review.categories, ", ")+"..."), "\n")
	case review.err != nil:
		for _, line := range components.WrapText("Unable to review Capture: "+components.DisplayText(review.err.Error()), width) {
			lines = append(lines, s.styles.Error(line))
		}
	case review.notice != "":
		for _, line := range components.WrapText(review.notice, width) {
			lines = append(lines, s.styles.Warning(line))
		}
	}
	if len(review.sections) == 0 {
		return strings.Join(append(lines, "", "Nothing to review: the chosen categories already match this profile."), "\n")
	}
	rows, selectedRow, index := []components.Row{}, 0, 0
	for _, section := range review.sections {
		rows = append(rows, components.Row{Cells: []string{s.styles.Accent(string(section.Group))}, Divider: true})
		for _, target := range section.Targets {
			selected := index == review.cursor
			if selected {
				selectedRow = len(rows)
			}
			rows = append(rows, components.Row{Cells: s.reviewCells(target, selected), Selected: selected, Focused: true})
			index++
		}
	}
	height := s.height - len(lines)
	if s.height <= 0 {
		height = len(rows) + 1
	}
	height = max(3, height)
	review.table.Ensure(selectedRow, len(rows), height-1)
	columns := []components.Column{{Title: "CATEGORY", Width: 10, MinWidth: 8}, {Title: "TARGET", MinWidth: 12}, {Title: "OUTCOME", Width: 18, MinWidth: 12}, {Title: "CAPTURE", Width: 11, MinWidth: 9}}
	return strings.Join(append(lines, review.table.Render(columns, rows, width, height, s.styles)), "\n")
}

func (s *Capture) reviewCells(target workflow.CaptureTarget, selected bool) []string {
	label := target.Inspection.Label
	if label == "" {
		label = target.Inspection.Key
	}
	outcome := captureOutcomeLabels[target.Outcome]
	blocked := target.Outcome == workflow.CaptureOutcomeBlocked
	decision := policyDecision("Capture", target.Decision.Capture, blocked)
	if blocked {
		decision = policySourceLabel(target.Policy.Source, true)
	}
	setting := styledDecision(s.styles, decision, selected)
	if !target.Policy.Explicit && !blocked {
		setting = "↳ " + setting
	}
	if !selected {
		switch target.ReviewGroup() {
		case workflow.CaptureReviewBlocked:
			outcome = s.styles.Warning(outcome)
		case workflow.CaptureReviewPreserved:
			outcome = s.styles.Muted(outcome)
		case workflow.CaptureReviewChanges:
			if target.Outcome == workflow.CaptureOutcomeAbsent || target.Outcome == workflow.CaptureOutcomeStopManaging {
				outcome = s.styles.Removed(outcome)
			} else {
				outcome = s.styles.Added(outcome)
			}
		}
	}
	return []string{title(target.Category), components.DisplayText(label), outcome, setting}
}

func (s *Capture) reviewDetail() string {
	target, ok := s.selectedReviewTarget()
	if !ok {
		return "Capture review\nNothing selected."
	}
	label := target.Inspection.Label
	if label == "" {
		label = target.Inspection.Key
	}
	blocked := target.Outcome == workflow.CaptureOutcomeBlocked
	source := policySource(target.Policy.Source)
	explicit := "inherited"
	if target.Policy.Explicit {
		explicit = "explicit"
	}
	lines := []string{
		components.DisplayText(label),
		"Category: " + title(target.Category),
		"Target: " + components.DisplayText(target.Inspection.Key),
		"Outcome: " + strings.TrimLeft(captureOutcomeLabels[target.Outcome], "+~−! "),
		captureOutcomeMeaning[target.Outcome],
		"Capture policy: " + policyDecision("Capture", target.Decision.Capture, blocked) + " (" + explicit + ", " + source + ")",
	}
	if target.Inspection.SafetyReason != "" {
		lines = append(lines, "Safety: "+components.DisplayText(target.Inspection.SafetyReason))
	}
	if !blocked {
		lines = append(lines, "", "space switches Include/Preserve at "+strings.TrimPrefix(captureScopeLabel(s.session, s.review.scope), "Policy scope: ")+"; x resets it to inherited.")
	}
	return strings.Join(lines, "\n")
}

// Reviewing reports whether the Capture review is open.
func (s *Capture) Reviewing() bool { return s.review != nil }

// ReviewTargetSelected reports whether a review row other than a blocked one
// is selected, i.e. whether a Capture policy change can apply to it.
func (s *Capture) ReviewTargetSelected() bool {
	target, ok := s.selectedReviewTarget()
	return ok && target.Outcome != workflow.CaptureOutcomeBlocked
}

func plural(count int, one, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, one)
	}
	return fmt.Sprintf("%d %s", count, many)
}
