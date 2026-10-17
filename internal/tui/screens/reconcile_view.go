package screens

import (
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
	"github.com/charmbracelet/x/ansi"
)

// Everything in this file is presentation: grouping, wording, layout and
// navigation of the Sync review. It never decides which choices are valid
// or what execution receives; choose, CanExecute and the workflow own that.

// reconcileRow is one visible row: a group heading, a plan target, a plan
// blocker, or (during first-time setup) a target that stops a choice.
type reconcileRow struct {
	group   string
	header  bool
	item    int // index into current.Items, or -1
	blocker int // index into current.Blockers, or -1
	setup   *workflow.SyncBootstrapItem
}

// reconcileRowKey identifies a row across rebuilds, so selection survives
// expanding, collapsing, deciding and fresh inspections.
type reconcileRowKey struct {
	group  string
	target syncengine.TargetKey
}

func (r reconcileRow) key(s *Reconcile) reconcileRowKey {
	switch {
	case r.header:
		return reconcileRowKey{group: r.group}
	case r.item >= 0:
		return reconcileRowKey{target: s.current.Items[r.item].Key}
	case r.setup != nil:
		return reconcileRowKey{group: r.group, target: r.setup.Key}
	}
	return reconcileRowKey{group: r.group, target: syncengine.TargetKey{Target: fmt.Sprint(r.blocker)}}
}

func reconcileGroup(item syncengine.Item) string {
	switch {
	case item.Deferred:
		return reconcileDeferred
	case item.Review != syncengine.ReviewNone:
		return reconcileNeedsDecision
	case reconcileDecisionLabel(item) != "":
		return reconcileDecided
	}
	restore, save := reconcileEffects(item)
	switch {
	case restore && save:
		return reconcileMultiple
	case restore:
		return reconcileComputer
	case save:
		return reconcileSave
	case item.Inapplicable && !item.Machine.Equal(item.Desired):
		return reconcileProtected
	case item.Settled:
		return reconcileMatches
	}
	return reconcileNothing
}

// reconcileEffects reports whether running Sync would change this computer
// (Restore) and whether it would save to the shared profile (Capture or
// Publish) for this target.
func reconcileEffects(item syncengine.Item) (restore, save bool) {
	for _, action := range item.Actions {
		switch action {
		case syncengine.ActionRestore:
			restore = true
		case syncengine.ActionCapture, syncengine.ActionPublish:
			save = true
		}
	}
	return restore, save
}

// reconcileGroupDefault opens what needs the user, keeps large action groups
// and everything that needs nothing closed, so the first screen is an
// overview rather than hundreds of rows.
func reconcileGroupDefault(group string, count int) bool {
	switch group {
	case reconcileAttention, reconcileNeedsDecision, reconcileDeferred, reconcileDecided:
		return true
	case reconcileMultiple, reconcileComputer, reconcileSave:
		return count <= 8
	}
	return false
}

func (s *Reconcile) groupExpanded(group string) bool {
	if open, ok := s.expanded[group]; ok {
		return open
	}
	count := 0
	for _, row := range s.allRows() {
		if row.group == group && !row.header {
			count++
		}
	}
	return reconcileGroupDefault(group, count)
}

func (s *Reconcile) setupMode() bool {
	return s.ready && s.bootstrap != nil && s.bootstrapChoice == nil
}

// allRows lists every group heading and member, expanded or not.
func (s *Reconcile) allRows() []reconcileRow {
	if !s.ready {
		return nil
	}
	var rows []reconcileRow
	add := func(group string, members []reconcileRow) {
		if len(members) == 0 {
			return
		}
		rows = append(rows, reconcileRow{group: group, header: true, item: -1, blocker: -1})
		rows = append(rows, members...)
	}
	rows = s.blockerRows()
	if s.setupMode() {
		for _, entry := range []struct {
			choice workflow.SyncBootstrapChoice
			group  string
		}{{"adopt-machine", reconcileSetupProtected}, {"reconcile-upstream", reconcileSetupUnshared}} {
			refusal := s.setupRefusals[entry.choice]
			if refusal == nil {
				continue
			}
			var members []reconcileRow
			for i := range refusal.Targets {
				members = append(members, reconcileRow{group: entry.group, item: -1, blocker: -1, setup: &refusal.Targets[i]})
			}
			add(entry.group, members)
		}
		return rows
	}
	for _, group := range reconcileGroups[1:] {
		var members []reconcileRow
		for i, item := range s.current.Items {
			if reconcileGroup(item) == group {
				members = append(members, reconcileRow{group: group, item: i, blocker: -1})
			}
		}
		add(group, members)
	}
	return rows
}

func (s *Reconcile) blockerRows() []reconcileRow {
	if len(s.current.Blockers) == 0 {
		return nil
	}
	rows := []reconcileRow{{group: reconcileAttention, header: true, item: -1, blocker: -1}}
	for i := range s.current.Blockers {
		rows = append(rows, reconcileRow{group: reconcileAttention, item: -1, blocker: i})
	}
	return rows
}

// rows is what is visible: headings always, members only when expanded, so a
// collapsed target can never be selected off-screen.
func (s *Reconcile) rows() []reconcileRow {
	var visible []reconcileRow
	open := map[string]bool{}
	for _, row := range s.allRows() {
		if row.header {
			open[row.group] = s.groupExpanded(row.group)
			visible = append(visible, row)
		} else if open[row.group] {
			visible = append(visible, row)
		}
	}
	return visible
}

func (s *Reconcile) groupAnchors() []int {
	var anchors []int
	for i, row := range s.rows() {
		if row.header {
			anchors = append(anchors, i)
		}
	}
	return anchors
}

func (s *Reconcile) listHeight() int { return max(1, s.height) }

func (s *Reconcile) selectedRow() (reconcileRow, bool) {
	rows := s.rows()
	if len(rows) == 0 {
		return reconcileRow{}, false
	}
	s.list.SetSelected(s.list.Selected, len(rows), s.listHeight())
	return rows[s.list.Selected], true
}

func (s *Reconcile) selectedItem() (syncengine.Item, bool) {
	row, ok := s.selectedRow()
	if !ok || row.item < 0 {
		return syncengine.Item{}, false
	}
	return s.current.Items[row.item], true
}

func (s *Reconcile) selectedRowKey() (reconcileRowKey, bool) {
	row, ok := s.selectedRow()
	if !ok {
		return reconcileRowKey{}, false
	}
	return row.key(s), true
}

// selectRowKey selects the row for key, falling back to its group heading,
// then to the nearest row, so a change in the list never strands selection.
func (s *Reconcile) selectRowKey(key reconcileRowKey) {
	rows := s.rows()
	target, heading := -1, -1
	for i, row := range rows {
		if row.key(s) == key {
			target = i
			break
		}
		if row.header && row.group == key.group {
			heading = i
		}
	}
	if target < 0 && key.target != (syncengine.TargetKey{}) {
		for i, row := range rows {
			if row.item >= 0 && s.current.Items[row.item].Key == key.target {
				target = i
			}
		}
	}
	if target < 0 {
		target = heading
	}
	if target < 0 {
		target = min(s.list.Selected, max(0, len(rows)-1))
	}
	s.list.SetSelected(target, len(rows), s.listHeight())
}

// selectAfterInspection keeps the previous row when it still exists;
// otherwise it starts on the first visible setting, which is the first one
// that needs the user because those groups open first.
func (s *Reconcile) selectAfterInspection(previous reconcileRowKey, hadPrevious bool) {
	rows := s.rows()
	if hadPrevious {
		for i, row := range rows {
			if row.key(s) == previous {
				s.list.SetSelected(i, len(rows), s.listHeight())
				return
			}
		}
	}
	for i, row := range rows {
		if !row.header {
			s.list.SetSelected(i, len(rows), s.listHeight())
			return
		}
	}
	s.list.SetSelected(0, len(rows), s.listHeight())
}

func (s *Reconcile) showSetupRefusal() {
	if !s.setupMode() {
		return
	}
	for _, group := range []string{reconcileSetupProtected, reconcileSetupUnshared, reconcileAttention} {
		for _, row := range s.allRows() {
			if !row.header && row.group == group {
				s.expanded[group] = true
				s.selectRowKey(row.key(s))
				return
			}
		}
	}
}

// captureSettingsCategory names the category whose Capture settings protect
// the selected (or, during setup, the first) protected setting.
func (s *Reconcile) captureSettingsCategory() (string, bool) {
	row, ok := s.selectedRow()
	if ok && row.setup != nil && row.group == reconcileSetupProtected {
		return row.setup.Key.Category, true
	}
	if ok && row.item >= 0 && reconcileGroup(s.current.Items[row.item]) == reconcileProtected {
		return s.current.Items[row.item].Key.Category, true
	}
	if ok && row.header && row.group == reconcileProtected {
		for _, item := range s.current.Items {
			if reconcileGroup(item) == reconcileProtected {
				return item.Key.Category, true
			}
		}
	}
	if s.setupMode() {
		if refusal := s.setupRefusals["adopt-machine"]; refusal != nil && refusal.Cause == workflow.SyncBootstrapCaptureProtected && len(refusal.Targets) > 0 {
			return refusal.Targets[0].Key.Category, true
		}
	}
	return "", false
}

// availableChoices mirrors choose's gates so Details and the footer only
// offer keys that choose would act on. A history conflict owns d.
func (s *Reconcile) availableChoices(item syncengine.Item) []string {
	if s.bootstrap != nil {
		return nil
	}
	var choices []string
	conflict := item.History == syncengine.HistoryConflict
	if conflict {
		choices = append(choices, string(syncengine.ResolutionKeepLocal), string(syncengine.ResolutionAcceptRemote), string(syncengine.ResolutionDefer))
	}
	machineDecided := item.MachineResolution != "" && item.MachineResolution != syncengine.MachineResolutionNone
	if item.Review == syncengine.ReviewMachine || machineDecided || (!conflict && item.Deferred) {
		if item.Review == syncengine.ReviewMachine && len(item.Choices) > 0 {
			for _, choice := range item.Choices {
				if choice == string(syncengine.MachineResolutionDefer) {
					if !conflict {
						choices = append(choices, choice+"/m")
					}
					continue
				}
				choices = append(choices, choice)
			}
			return choices
		}
		if item.Policy.Capture {
			choices = append(choices, string(syncengine.MachineResolutionKeepMachine))
		}
		choices = append(choices, string(syncengine.MachineResolutionKeepProfile))
		if !conflict {
			choices = append(choices, string(syncengine.MachineResolutionDefer)+"/m")
		}
	}
	return choices
}

func (s *Reconcile) selectedChoices() map[string]bool {
	available := map[string]bool{}
	item, ok := s.selectedItem()
	if !s.ready || s.busy || !ok {
		return available
	}
	for _, choice := range s.availableChoices(item) {
		available[reconcileChoiceInfo[choice].key] = true
	}
	return available
}

func (s *Reconcile) Actions() []SyncAction {
	if s.confirming {
		return []SyncAction{
			{"reconcile.continue", "Continue to approvals", "enter", "", true},
			{"reconcile.back", "Back to review", "b", "", true},
			{"reconcile.cancel-run", "Cancel — do not run Sync", "c", "", true},
		}
	}
	choices := s.selectedChoices()
	row, hasRow := s.selectedRow()
	setup := s.setupMode()
	_, captureSettings := s.captureSettingsCategory()
	toggle := "Expand or collapse group"
	if hasRow && row.header {
		if s.groupExpanded(row.group) {
			toggle = "Collapse group"
		} else {
			toggle = "Expand group"
		}
	}
	return []SyncAction{
		{"reconcile.bootstrap-upstream", "See what would change", "u", s.setupUnavailableReason("reconcile-upstream"), setup && s.setupRefusals["reconcile-upstream"] == nil},
		{"reconcile.bootstrap-machine", "See what would be saved", "v", s.setupUnavailableReason("adopt-machine"), setup && s.setupRefusals["adopt-machine"] == nil},
		{"reconcile.show-protected", "Show protected settings", "o", "only when a first-time choice is unavailable", setup && (s.setupRefusals["adopt-machine"] != nil || s.setupRefusals["reconcile-upstream"] != nil)},
		{"reconcile.capture-settings", "Review Capture settings", "c", "select a protected setting", s.ready && !s.busy && captureSettings},
		{"reconcile.keep-local", reconcileChoiceInfo["keep-local"].label, "L", "select a setting changed differently in two copies of your profile", choices["L"]},
		{"reconcile.accept-remote", reconcileChoiceInfo["accept-remote"].label, "a", "select a setting changed differently in two copies of your profile", choices["a"]},
		{"reconcile.keep-machine", reconcileChoiceInfo["keep-machine"].label, "m", "select a setting that differs on this computer", choices["m"]},
		{"reconcile.keep-profile", reconcileChoiceInfo["keep-profile"].label, "p", "select a setting that differs on this computer", choices["p"]},
		{"reconcile.defer", "Decide later", "d", "select a setting that needs your decision", choices["d"]},
		{"reconcile.execute", "Review Sync changes", "s", s.executeUnavailableReason(), s.CanExecute()},
		{"reconcile.toggle-group", toggle, "enter", "nothing to expand", hasRow},
		{"reconcile.refresh", "Check for updates", "r", "Sync is busy", !s.busy && !s.checking},
		{"reconcile.cancel-setup", "Cancel setup — leave everything as it is", "esc", "only during first-time Sync setup", setup},
		{"reconcile.choose-again", "Choose a different starting point", "esc", "only while previewing a first-time choice", s.ready && s.bootstrap != nil && s.bootstrapChoice != nil},
	}
}

func (s *Reconcile) setupUnavailableReason(choice workflow.SyncBootstrapChoice) string {
	if !s.setupMode() {
		return "only during first-time Sync setup"
	}
	switch refusal := s.setupRefusals[choice]; {
	case refusal == nil:
		return ""
	case refusal.Cause == workflow.SyncBootstrapCaptureProtected:
		return "some settings that differ are protected from Capture"
	case refusal.Cause == workflow.SyncBootstrapUnpublishedLocal:
		return "your saved profile has unshared changes that differ"
	default:
		problem, _ := reconcileSetupCause(refusal.Cause)
		return strings.TrimSuffix(strings.ToLower(problem[:1])+problem[1:], ".")
	}
}

func (s *Reconcile) executeUnavailableReason() string {
	switch {
	case !s.ready:
		return "check for updates first"
	case len(s.current.Blockers) > 0:
		return "fix the problems listed first"
	case s.bootstrap != nil && s.bootstrapChoice == nil:
		return "choose where to start first"
	}
	for _, item := range s.current.Items {
		if item.Deferred || item.Review != syncengine.ReviewNone {
			return "decide every setting that needs your decision first"
		}
	}
	return "Sync is busy"
}

func reconcileDecisionLabel(item syncengine.Item) string {
	var parts []string
	switch item.Resolution {
	case syncengine.ResolutionKeepLocal, syncengine.ResolutionAcceptRemote:
		parts = append(parts, reconcileChoiceInfo[string(item.Resolution)].label)
	}
	switch item.MachineResolution {
	case syncengine.MachineResolutionKeepMachine, syncengine.MachineResolutionKeepProfile:
		parts = append(parts, reconcileChoiceInfo[string(item.MachineResolution)].label)
	}
	return strings.Join(parts, " + ")
}

// reconcileWhatHappens is the row's one-line consequence.
func reconcileWhatHappens(s *Reconcile, item syncengine.Item) string {
	switch {
	case item.Review == syncengine.ReviewHistory:
		return "Changed in two copies · choose " + reconcileKeys(s, item)
	case item.Review == syncengine.ReviewMachine:
		return "Differs here · choose " + reconcileKeys(s, item)
	case item.Deferred:
		return "Left for later · Sync can't finish yet"
	}
	var parts []string
	if label := reconcileDecisionLabel(item); label != "" {
		parts = append(parts, "You chose: "+label)
	}
	restore, save := reconcileEffects(item)
	if save {
		if captures(item) {
			parts = append(parts, "save this computer's setting")
		} else {
			parts = append(parts, "send saved change to shared profile")
		}
	}
	if restore {
		if removes(item) {
			parts = append(parts, "remove it from this computer")
		} else {
			parts = append(parts, "change this computer")
		}
	}
	if len(parts) > 0 {
		text := strings.Join(parts, " · ")
		return strings.ToUpper(text[:1]) + text[1:]
	}
	switch reconcileGroup(item) {
	case reconcileProtected:
		return "Different, but left alone by your settings"
	case reconcileMatches:
		return "Already matches"
	}
	return "Nothing to change"
}

func reconcileKeys(s *Reconcile, item syncengine.Item) string {
	var keys []string
	for _, choice := range s.availableChoices(item) {
		keys = append(keys, reconcileChoiceInfo[choice].key)
	}
	return strings.Join(keys, "/")
}

func captures(item syncengine.Item) bool {
	for _, action := range item.Actions {
		if action == syncengine.ActionCapture {
			return true
		}
	}
	return false
}

func removes(item syncengine.Item) bool {
	restore, _ := reconcileEffects(item)
	return restore && item.Desired.Kind == syncengine.StateAbsent && item.Machine.Kind == syncengine.StatePresent
}

func shortRevision(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "none"
	}
	return sha
}

func (s *Reconcile) widthOrDefault() int {
	if s.width <= 0 {
		return 80
	}
	return s.width
}

type reconcileCounts struct{ matches, computer, save, protected, decisions, deferred, removals int }

func (s *Reconcile) counts() reconcileCounts {
	var c reconcileCounts
	for _, item := range s.current.Items {
		restore, save := reconcileEffects(item)
		if restore {
			c.computer++
		}
		if save {
			c.save++
		}
		if removes(item) {
			c.removals++
		}
		switch reconcileGroup(item) {
		case reconcileMatches:
			c.matches++
		case reconcileProtected:
			c.protected++
		case reconcileNeedsDecision:
			c.decisions++
		case reconcileDeferred:
			c.deferred++
		}
	}
	return c
}

// verdict answers "can I run Sync, and if not why", truthfully.
func (s *Reconcile) verdict() string {
	c := s.counts()
	id := s.current.Identity
	switch {
	case len(s.current.Blockers) > 0:
		return s.styles.Warning(components.Icons.Blocked + " Sync cannot continue yet. Open the items below to see what needs attention.")
	case c.decisions > 0:
		return s.styles.Warning(components.Icons.Attention + " " + reconcileCount(c.decisions, "%d setting needs your decision before Sync can run.", "%d settings need your decision before Sync can run."))
	case c.deferred > 0:
		return s.styles.Warning(components.Icons.Attention + " Some settings are left for later, so Sync can't be completed yet. Decide them to continue.")
	case !s.CanExecute():
		return s.styles.Warning(components.Icons.Attention + " Sync can't run yet.")
	case c.computer+c.save > 0:
		return s.styles.Success(components.Icons.Ready + " Your review is ready. Check the proposed changes before running Sync.")
	case c.protected == 0 && s.bootstrap == nil && id.Base != "" && id.Base == id.Local && id.Base == id.Remote:
		return s.styles.Success(components.Icons.Ready + " Everything Blueprint can safely check is already in sync. No changes are proposed.")
	}
	return s.styles.Success(components.Icons.Ready + " No changes are proposed for the settings checked.")
}

func (s *Reconcile) summaryLines() []string {
	c := s.counts()
	lines := []string{s.styles.Heading("Here's what Blueprint found")}
	add := func(n int, one, many string) {
		lines = append(lines, "  "+reconcileCount(n, one, many))
	}
	if c.decisions > 0 {
		add(c.decisions, "%d decision needs your attention", "%d decisions need your attention")
	}
	if c.deferred > 0 {
		add(c.deferred, "%d setting is left for later", "%d settings are left for later")
	}
	add(c.computer, "%d setting could change on this computer", "%d settings could change on this computer")
	add(c.save, "%d change could be saved to your shared profile", "%d changes could be saved to your shared profile")
	add(c.protected, "%d setting is protected by your choices", "%d settings are protected by your choices")
	add(c.matches, "%d setting already matches", "%d settings already match")
	return lines
}

func (s *Reconcile) messageLines(width int) []string {
	if s.message == "" {
		return nil
	}
	style := s.styles.Warning
	if s.message == reconcileCompleted || s.message == reconcileSetupCancelled || s.message == reconcileCancelledRun {
		style = s.styles.Success
	}
	lines := components.WrapText(s.message, width)
	for i := range lines {
		lines[i] = style(lines[i])
	}
	return lines
}

func (s *Reconcile) View() string {
	width := s.widthOrDefault()
	switch {
	case s.busy:
		return s.bound(components.WrapText("Sync is running in the terminal. Follow the prompts there; this screen will update when it finishes.", width), width)
	case s.confirming:
		return strings.Join(s.confirmationView(width), "\n")
	case s.checking:
		lines := append([]string{s.styles.Accent("Checking for updates and comparing them with this computer…")}, s.messageLines(width)...)
		lines = append(lines, "")
		lines = append(lines, components.WrapText("This only reads your profile and this computer. Esc stops checking.", width)...)
		return s.bound(lines, width)
	case !s.ready:
		lines := append(s.messageLines(width), "", "Press r to check for updates.")
		return s.bound(lines, width)
	case s.setupMode():
		return s.setupView(width)
	}
	header := append([]string{s.verdict()}, s.messageLines(width)...)
	if s.bootstrapChoice != nil {
		label := "Start with my saved Blueprint setup"
		if *s.bootstrapChoice == "adopt-machine" {
			label = "Start with this computer's setup"
		}
		header = append(header, s.styles.Accent("First-time Sync setup: "+label)+s.styles.Muted(" · Esc to choose again"))
	}
	summary := s.summaryLines()
	if len(s.current.Items) == 0 && len(s.current.Blockers) == 0 {
		return s.bound(append(append(header, ""), summary...), width)
	}
	// Keep the verdict and the list; shed the summary when space is short.
	full := append(append(header, ""), summary...)
	if s.height > 0 && len(full)+1+1+reconcileMinTableRows > s.height {
		full = header
	}
	full = append(full, "")
	return s.bound(append(full, s.groupTable(width, s.tableHeight(len(full)))), width)
}

// reconcileMinTableRows keeps a heading and a couple of rows on screen.
const reconcileMinTableRows = 4

func (s *Reconcile) tableHeight(used int) int {
	if s.height <= 0 {
		return len(s.rows()) + 1
	}
	return max(2, s.height-used)
}

func (s *Reconcile) groupTable(width, height int) string {
	rows := s.rows()
	if len(rows) == 0 {
		return ""
	}
	s.list.SetSelected(s.list.Selected, len(rows), s.listHeight())
	totals := map[string]int{}
	for _, row := range s.allRows() {
		if !row.header {
			totals[row.group]++
		}
	}
	rendered := make([]components.Row, 0, len(rows))
	for i, row := range rows {
		selected := i == s.list.Selected
		if row.header {
			// Headings span the full width, as on the category screens, so the
			// count is never cut off.
			rendered = append(rendered, components.Row{Cells: []string{s.headerLabel(row.group, totals[row.group], selected)}, Selected: selected, Focused: true, Divider: true})
			continue
		}
		rendered = append(rendered, components.Row{Cells: s.rowCells(row, selected), Selected: selected, Focused: true})
	}
	// Bring a target's group heading into view with it when there is room.
	if s.list.Selected > 0 && rows[s.list.Selected-1].header {
		s.table.Ensure(s.list.Selected-1, len(rows), height-1)
	}
	s.table.Ensure(s.list.Selected, len(rows), height-1)
	columns := []components.Column{{Title: "SETTING", Width: 40, MinWidth: 18}, {Title: "WHAT SYNC WOULD DO", MinWidth: 16}}
	return s.table.Render(columns, rendered, width, height, s.styles)
}

func (s *Reconcile) headerLabel(group string, total int, selected bool) string {
	icon := components.Icons.Collapsed
	if s.groupExpanded(group) {
		icon = components.Icons.Expanded
	}
	label := fmt.Sprintf("%s %s (%d)", icon, group, total)
	note := ""
	switch group {
	case reconcileComputer, reconcileMultiple:
		note = "may replace or remove settings on this computer"
		if removals := s.groupRemovals(group); removals > 0 {
			note = reconcileCount(removals, "%d would be removed from this computer", "%d would be removed from this computer")
		}
	case reconcileNeedsDecision:
		note = "Blueprint will not decide for you"
	case reconcileDeferred:
		note = "Sync can't finish until you decide"
	}
	if selected {
		if note != "" {
			label += " · " + note
		}
		return label
	}
	tone := s.styles.Accent
	switch group {
	case reconcileAttention, reconcileNeedsDecision, reconcileDeferred, reconcileSetupProtected, reconcileSetupUnshared:
		tone = s.styles.Warning
	case reconcileMatches, reconcileNothing, reconcileProtected:
		tone = s.styles.Muted
	}
	label = tone(label)
	if note == "" {
		return label
	}
	if group == reconcileComputer || group == reconcileMultiple {
		return label + " " + s.styles.Warning("· "+note)
	}
	return label + " " + s.styles.Muted("· "+note)
}

func (s *Reconcile) groupRemovals(group string) int {
	n := 0
	for _, item := range s.current.Items {
		if reconcileGroup(item) == group && removes(item) {
			n++
		}
	}
	return n
}

func reconcileTargetName(key syncengine.TargetKey) string {
	return titleMode(components.DisplayText(key.Category)) + " · " + components.DisplayText(key.Target)
}

func (s *Reconcile) rowCells(row reconcileRow, selected bool) []string {
	var name, what string
	switch {
	case row.blocker >= 0:
		blocker := s.current.Blockers[row.blocker]
		problem, _ := reconcileBlockerCopy(blocker.Code)
		name, what = reconcileBlockerTitle(blocker.Code), problem
		if blocker.Target != nil {
			name = reconcileTargetName(*blocker.Target) + ": " + reconcileBlockerTitle(blocker.Code)
		}
	case row.setup != nil:
		name = reconcileTargetName(row.setup.Key)
		capture, restore := reconcilePolicyText(row.setup.Policy)
		what = "Capture: " + capture + " · Restore: " + restore
		if row.group == reconcileSetupUnshared {
			what = "Saved here, not yet shared, and different from the shared profile"
		}
	default:
		item := s.current.Items[row.item]
		name, what = reconcileTargetName(item.Key), reconcileWhatHappens(s, item)
	}
	name = "  " + name
	if selected {
		return []string{name, what}
	}
	switch row.group {
	case reconcileMatches, reconcileNothing, reconcileProtected:
		return []string{s.styles.Muted(name), s.styles.Muted(what)}
	}
	return []string{name, what}
}

func (s *Reconcile) bound(lines []string, width int) string {
	var bounded []string
	for _, line := range lines {
		for _, part := range strings.Split(line, "\n") {
			bounded = append(bounded, ansi.Truncate(part, width, "…"))
		}
	}
	if s.height > 0 && len(bounded) > s.height {
		bounded = bounded[:s.height]
	}
	return strings.Join(bounded, "\n")
}

// setupView is first-time Sync setup. When the full explanation does not fit
// it shows a compact version; Details always carries the full text.
func (s *Reconcile) setupView(width int) string {
	visible := len(s.rows())
	table := visible > 0
	var lines []string
	// Use the most complete wording that leaves room for the list.
	for level := 0; level <= reconcileSetupMinimal; level++ {
		lines = s.setupLines(width, level)
		need := len(lines)
		if table {
			need += 1 + 1 + min(visible, 3)
		}
		if s.height <= 0 || need <= s.height {
			break
		}
	}
	if table {
		lines = append(lines, "")
		lines = append(lines, s.groupTable(width, s.tableHeight(len(lines))))
	}
	return s.bound(lines, width)
}

// Setup wording levels, from most to least complete. Details always carries
// the full text.
const (
	reconcileSetupFull = iota
	reconcileSetupShortRefusal
	reconcileSetupShortIntro
	reconcileSetupCompact
	reconcileSetupMinimal
)

func (s *Reconcile) setupLines(width, level int) []string {
	if level == reconcileSetupMinimal {
		return s.setupMinimalLines(width)
	}
	compact := level == reconcileSetupCompact
	wrap := func(text string, indent int, style func(string) string) []string {
		pad := strings.Repeat(" ", indent)
		var out []string
		for _, line := range components.WrapText(text, max(10, width-indent)) {
			if style != nil {
				line = style(line)
			}
			out = append(out, pad+line)
		}
		return out
	}
	lines := []string{s.styles.Heading("Set up Sync on this computer")}
	lines = append(lines, s.messageLines(width)...)
	if level >= reconcileSetupShortIntro {
		lines = append(lines, wrap("Blueprint has not completed a Sync on this computer yet. Choose where to start; nothing changes until you review and approve. Details explains each choice.", 0, nil)...)
	} else {
		for _, paragraph := range []string{
			"Blueprint has not completed a Sync on this computer yet.",
			"Before it can track future changes, it needs to compare this computer with your saved setup.",
			"Choose where you want to start. You can review everything before making changes.",
		} {
			lines = append(lines, wrap(paragraph, 0, nil)...)
		}
	}
	id := s.current.Identity
	if id.Local != "" && id.Local == id.Remote {
		lines = append(lines, wrap("Your saved profile and the shared profile already match. We only need to check how this computer compares with that saved setup.", 0, s.styles.Muted)...)
	}
	upstream, machine := s.setupRefusals["reconcile-upstream"], s.setupRefusals["adopt-machine"]
	if upstream != nil && machine != nil && upstream.Cause == machine.Cause && upstream.Cause != workflow.SyncBootstrapCaptureProtected && upstream.Cause != workflow.SyncBootstrapUnpublishedLocal {
		// One cause stops both choices: say so once instead of per choice.
		problem, next := reconcileSetupCause(upstream.Cause)
		lines = append(lines, "")
		lines = append(lines, wrap(components.Icons.Blocked+" "+problem, 0, s.styles.Warning)...)
		lines = append(lines, wrap(next, 2, nil)...)
		lines = append(lines, wrap("Neither starting choice is available until this is fixed.", 2, s.styles.Muted)...)
		return append(lines, "", s.styles.Accent("Esc")+"  Cancel setup — leave everything as it is")
	}
	choice := func(number, title, key, preview string, about []string, refusal *workflow.SyncBootstrapUnavailableError) {
		lines = append(lines, "")
		heading := number + ". " + title
		if refusal != nil {
			lines = append(lines, s.styles.Muted(heading+" — not available"))
		} else {
			lines = append(lines, s.styles.Accent(heading))
		}
		if !compact {
			for _, paragraph := range about {
				lines = append(lines, wrap(paragraph, 3, nil)...)
			}
		}
		if refusal == nil {
			lines = append(lines, "   "+s.styles.Accent(key)+"  "+preview)
			return
		}
		lines = append(lines, s.refusalLines(refusal, level >= reconcileSetupShortRefusal, wrap)...)
	}
	choice("1", "Start with my saved Blueprint setup", "u", "See what would change", []string{
		"Use the setup already saved in Git as the starting point.",
		"Blueprint will show which settings differ on this computer and what it could apply. Some changes may also be eligible to save.",
		"You will review the proposed changes before anything happens.",
	}, upstream)
	choice("2", "Start with this computer's setup", "v", "See what would be saved", []string{
		"Use eligible settings from this computer as the starting point and save approved differences to your shared Blueprint profile.",
		"Blueprint will respect settings you have chosen not to save.",
		"You will review the proposed changes before anything happens.",
	}, machine)
	lines = append(lines, "", s.styles.Accent("Esc")+"  Cancel setup — leave everything as it is")
	return lines
}

// setupMinimalLines keeps only the choices, their keys and why one is
// unavailable, so the list below stays visible on a short terminal.
func (s *Reconcile) setupMinimalLines(width int) []string {
	lines := []string{s.styles.Heading("Set up Sync on this computer")}
	lines = append(lines, s.messageLines(width)...)
	upstream, machine := s.setupRefusals["reconcile-upstream"], s.setupRefusals["adopt-machine"]
	line := func(key, title, preview string, refusal *workflow.SyncBootstrapUnavailableError) string {
		if refusal == nil {
			return s.styles.Accent(key) + "  " + title + " — " + preview
		}
		reason := s.setupUnavailableReason(map[string]workflow.SyncBootstrapChoice{"u": "reconcile-upstream", "v": "adopt-machine"}[key])
		return s.styles.Muted(key+"  "+title) + " — " + s.styles.Warning("not available: "+reason)
	}
	for _, text := range []string{
		line("u", "Start with my saved Blueprint setup", "See what would change", upstream),
		line("v", "Start with this computer's setup", "See what would be saved", machine),
	} {
		lines = append(lines, components.WrapText(text, width)...)
	}
	if upstream != nil || machine != nil {
		lines = append(lines, components.WrapText(s.styles.Accent("o")+" Show why · "+s.styles.Accent("c")+" Review Capture settings · Details explains each choice", width)...)
	}
	return append(lines, s.styles.Accent("Esc")+"  Cancel setup — leave everything as it is")
}

func (s *Reconcile) refusalLines(refusal *workflow.SyncBootstrapUnavailableError, compact bool, wrap func(string, int, func(string) string) []string) []string {
	var lines []string
	switch refusal.Cause {
	case workflow.SyncBootstrapCaptureProtected:
		lines = append(lines, wrap(components.Icons.Blocked+" Can't start from this computer yet", 3, s.styles.Warning)...)
		if !compact {
			for _, paragraph := range []string{
				"Some settings on this computer differ from your saved profile, but Blueprint is currently set not to save those settings.",
				"Starting from this computer would require changing them in your saved profile, so Blueprint has stopped to respect your choices.",
				"Nothing has been changed.",
			} {
				lines = append(lines, wrap(paragraph, 3, nil)...)
			}
		}
		lines = append(lines, wrap(reconcileCount(len(refusal.Targets), "%d setting is protected from Capture", "%d settings are protected from Capture"), 3, s.styles.Warning)...)
		lines = append(lines, wrap(s.styles.Accent("o")+" Show protected settings · "+s.styles.Accent("c")+" Review Capture settings · "+s.styles.Accent("u")+" Choose the saved setup instead · "+s.styles.Accent("Esc")+" Cancel", 3, nil)...)
	case workflow.SyncBootstrapUnpublishedLocal:
		lines = append(lines, wrap(components.Icons.Blocked+" Can't start from the saved setup yet", 3, s.styles.Warning)...)
		if !compact {
			lines = append(lines, wrap("Your saved profile on this computer has changes that haven't been sent to the shared profile, and they differ from it. Starting from the shared setup would lose them, so Blueprint has stopped. Nothing has been changed.", 3, nil)...)
			lines = append(lines, wrap("Send them to the shared profile with Profile Git first, or choose \"Start with this computer's setup\".", 3, nil)...)
		}
		lines = append(lines, wrap(reconcileCount(len(refusal.Targets), "%d setting has unshared changes", "%d settings have unshared changes"), 3, s.styles.Warning)...)
		lines = append(lines, wrap(s.styles.Accent("o")+" Show these settings · "+s.styles.Accent("Esc")+" Cancel", 3, nil)...)
	default:
		problem, _ := reconcileSetupCause(refusal.Cause)
		lines = append(lines, wrap(components.Icons.Blocked+" Not available: "+problem, 3, s.styles.Warning)...)
	}
	return lines
}

// confirmationView describes the reviewed run in the order ExecuteSync owns,
// listing only stages that apply. Counts come from the reviewed plan; the
// workflow re-checks everything and asks for its own approvals.
func (s *Reconcile) confirmationView(width int) []string {
	c := s.counts()
	captureCount, publish := 0, false
	for _, item := range s.current.Items {
		for _, action := range item.Actions {
			switch action {
			case syncengine.ActionCapture:
				captureCount++
			case syncengine.ActionPublish:
				publish = true
			}
		}
	}
	git := s.current.Git
	type stage struct {
		title, note string
		warn        bool
	}
	stages := []stage{{title: "Check that nothing has changed since your review"}}
	if git.Diverged || git.Behind > 0 {
		stages = append(stages, stage{title: "Combine approved changes to the saved profile"})
	}
	if captureCount > 0 {
		stages = append(stages, stage{title: "Save selected settings from this computer", note: reconcileCount(captureCount, "%d setting", "%d settings")})
	}
	if captureCount > 0 || publish || git.Ahead > 0 || git.Diverged {
		stages = append(stages, stage{title: "Send approved profile changes to the shared repository"})
	}
	if c.computer > 0 {
		stages = append(stages, stage{title: "Apply selected saved settings to this computer", note: reconcileCount(c.computer, "%d setting", "%d settings")})
		risk := "This may replace or remove settings on this computer."
		if c.removals > 0 {
			risk = reconcileCount(c.removals, "This may replace settings on this computer, and %d setting would be removed.", "This may replace settings on this computer, and %d settings would be removed.")
		}
		stages = append(stages, stage{title: risk, warn: true})
	}
	stages = append(stages, stage{title: "Check the results and record a completed Sync"})

	body := []string{components.SectionDivider("Review your Sync changes", width, s.styles)}
	body = append(body, components.WrapText("Blueprint is ready to carry out the changes you reviewed.", width)...)
	body = append(body, components.WrapText("Depending on your choices, it may update your saved profile, send those changes to the shared Git repository, and apply settings to this computer.", width)...)
	opening := len(body)
	body = append(body, "")
	number := 0
	for _, st := range stages {
		if st.warn {
			for _, line := range components.WrapText(st.title, max(10, width-5)) {
				body = append(body, "     "+s.styles.Warning(line))
			}
			continue
		}
		number++
		text := st.title
		if st.note != "" {
			text += " — " + st.note
		}
		for i, line := range components.WrapText(text, max(10, width-5)) {
			prefix := "     "
			if i == 0 {
				prefix = s.styles.Accent(fmt.Sprintf("  %d", number)) + "  "
			}
			body = append(body, prefix+line)
		}
	}
	note := components.WrapText("You will be asked to approve the actual Capture and Restore operations separately. If anything important has changed, Blueprint will stop and ask you to review again.", width)
	tail := []string{s.styles.Accent("Enter") + " Continue to approvals · " + s.styles.Accent("b") + " Back to review · " + s.styles.Accent("c") + " Cancel — do not run Sync"}
	full := append(append(append([]string{}, body...), ""), note...)
	if s.height > 0 && len(full)+len(tail) > s.height {
		// Short workspace: drop the opening paragraph and spacing first; the
		// stages, the risk and the approval note are what matter.
		full = append(append([]string{body[0]}, body[opening+1:]...), note...)
	}
	if s.height <= 0 || len(full)+1+len(tail) <= s.height {
		full = append(full, "")
	}
	for i, line := range full {
		full[i] = ansi.Truncate(line, width, "…")
	}
	for i, line := range tail {
		tail[i] = ansi.Truncate(line, width, "…")
	}
	return fitBlock(full, tail, s.height, width)
}

func (s *Reconcile) DetailView() string {
	var lines []string
	row, ok := s.selectedRow()
	switch {
	case s.confirming || !ok:
		if s.setupMode() {
			lines = s.setupDetail()
		} else {
			lines = s.overviewDetail()
		}
	case row.header:
		lines = []string{row.group, reconcileGroupAbout[row.group]}
		if row.group == reconcileProtected || row.group == reconcileSetupProtected {
			lines = append(lines, "", "Press c to review the Capture settings that protect them. Blueprint will not change those permissions for you.")
		}
		if s.setupMode() {
			lines = append(lines, "")
			lines = append(lines, s.setupDetail()[1:]...)
		}
	case row.blocker >= 0:
		lines = s.blockerDetail(s.current.Blockers[row.blocker])
	case row.setup != nil:
		lines = s.setupTargetDetail(row)
	default:
		lines = s.itemDetail(s.current.Items[row.item])
	}
	if s.message != "" {
		lines = append(lines, "", s.message)
	}
	return strings.Join(lines, "\n")
}

func (s *Reconcile) overviewDetail() []string {
	lines := []string{"Sync",
		"Sync compares your saved profile, the shared profile from your other computers, and this computer, and shows what is different.",
		"",
		"Select a group and press Enter to open it, then select a setting to see what would happen to it. Nothing changes until you review and approve a Sync run.",
	}
	return append(lines, s.technicalPlan()...)
}

func (s *Reconcile) technicalPlan() []string {
	if !s.ready {
		return nil
	}
	id, git := s.current.Identity, s.current.Git
	lines := []string{"", "Technical details",
		"Computer: " + components.DisplayText(valueOr(id.Machine, "not selected")),
		"Branch: " + components.DisplayText(valueOr(id.Branch, "none")) + " → " + components.DisplayText(valueOr(id.Upstream, "none")),
		"Last completed Sync (B): " + shortRevision(id.Base),
		"Saved on this computer (L): " + shortRevision(id.Local),
		"Shared profile (R): " + shortRevision(id.Remote),
		fmt.Sprintf("Git: %d ahead, %d behind, diverged %t, uncommitted changes %t", git.Ahead, git.Behind, git.Diverged, !git.Clean),
	}
	if control := s.current.Control; control != nil {
		lines = append(lines, fmt.Sprintf("Control settings: %s, using %s", control.History, control.SelectedSource))
	}
	return lines
}

func (s *Reconcile) setupDetail() []string {
	lines := []string{"Set up Sync on this computer",
		"Blueprint has not completed a Sync on this computer yet. Before it can track future changes, it needs to compare this computer with your saved setup.",
		"",
		"1. Start with my saved Blueprint setup (u)",
		"Use the setup already saved in Git as the starting point. Blueprint will show which settings differ on this computer and what it could apply. Some changes may also be eligible to save. You will review the proposed changes before anything happens.",
		"",
		"2. Start with this computer's setup (v)",
		"Use eligible settings from this computer as the starting point and save approved differences to your shared Blueprint profile. Blueprint will respect settings you have chosen not to save. You will review the proposed changes before anything happens.",
		"",
		"Esc cancels setup and leaves everything as it is.",
		"",
		"Seeing a preview never runs Capture or Restore, never sends anything to the shared profile, and never records a completed Sync.",
	}
	return append(lines, s.technicalPlan()...)
}

func (s *Reconcile) blockerDetail(blocker syncengine.Blocker) []string {
	problem, next := reconcileBlockerCopy(blocker.Code)
	title := "Sync can't continue yet"
	if blocker.Target != nil {
		title = reconcileTargetName(*blocker.Target)
	}
	return []string{title, problem, "", "What to do next", next, "", "Technical details",
		"Code: " + components.DisplayText(blocker.Code),
		"Message: " + components.DisplayText(blocker.Message),
	}
}

func (s *Reconcile) setupTargetDetail(row reconcileRow) []string {
	x := row.setup
	capture, restore := reconcilePolicyText(x.Policy)
	lines := []string{reconcileTargetName(x.Key)}
	if row.group == reconcileSetupProtected {
		lines = append(lines,
			"This setting is different on this computer, but Blueprint is set not to save it.",
			"",
			"Saved profile: "+plainState(x.Remote),
			"This computer: "+plainState(x.Machine),
			"Capture allowed: "+capture,
			"Restore allowed: "+restore,
			"",
			"Why this matters",
			"Starting from this computer would mean saving this setting, which your Capture settings don't allow. Blueprint stops instead of ignoring your choice.",
			"",
			"Where to change it",
			"Press c to open the "+titleMode(components.DisplayText(x.Key.Category))+" Capture settings. Blueprint will not change this permission for you. You can also choose \"Start with my saved Blueprint setup\" (u) instead.",
		)
	} else {
		lines = append(lines,
			"Your saved profile on this computer has a change to this setting that hasn't been sent to the shared profile, and it is different from the shared version.",
			"",
			"Saved on this computer: "+plainState(x.Local),
			"Shared profile: "+plainState(x.Remote),
			"",
			"What to do next",
			"Send your saved changes to the shared profile with Profile Git, or choose \"Start with this computer's setup\" (v).",
		)
	}
	return append(lines, "", "Technical details",
		"Target: "+components.DisplayText(x.Key.Category+":"+x.Key.Target),
		"Saved on this computer (L): "+technicalState(x.Local),
		"Shared profile (R): "+technicalState(x.Remote),
		"This computer now (M): "+technicalState(x.Machine),
	)
}

// plainState is a safe, ordinary description of one version of a setting.
func plainState(state syncengine.State) string {
	switch state.Kind {
	case syncengine.StatePresent:
		if state.Summary != "" {
			return components.DisplayText(state.Summary)
		}
		return "set (no preview available)"
	case syncengine.StateAbsent:
		return "removed"
	case syncengine.StateUnmanaged:
		return "not saved"
	}
	return "unknown"
}

func technicalState(state syncengine.State) string {
	text := components.DisplayText(string(state.Kind))
	if text == "" {
		text = "—"
	}
	if state.Summary != "" {
		text += " · " + components.DisplayText(state.Summary)
	}
	if fp := state.Fingerprint; fp != "" {
		if len(fp) > 12 {
			fp = fp[:12]
		}
		text += " · " + components.DisplayText(fp)
	}
	return text
}

// reconcileConsequence says what running Sync would do after a decision.
func reconcileConsequence(item syncengine.Item) string {
	restore, save := reconcileEffects(item)
	switch {
	case restore && save:
		return "Running Sync would send the kept version to your shared profile and change this setting on this computer to match."
	case restore && removes(item):
		return "Running Sync would remove this setting from this computer."
	case restore:
		return "Running Sync would change this setting on this computer to match."
	case save && captures(item):
		return "Running Sync would save this computer's setting to your shared profile."
	case save:
		return "Running Sync would send the kept version to your shared profile. This computer already matches it."
	case item.Inapplicable:
		return "Your Blueprint settings leave this computer as it is for this setting."
	}
	return "Nothing else needs to change."
}

func (s *Reconcile) itemDetail(item syncengine.Item) []string {
	lines := []string{reconcileTargetName(item.Key)}
	saved := plainState(item.Desired)
	here := plainState(item.Machine)
	restore, save := reconcileEffects(item)
	switch {
	case item.History == syncengine.HistoryConflict:
		lines = append(lines,
			"This setting was changed differently in two copies of your saved profile.",
			"",
			"Saved on this computer: "+plainState(item.Local),
			"Shared saved version: "+plainState(item.Remote),
			"This computer right now: "+here,
			"")
		if item.Review != syncengine.ReviewNone || item.Deferred {
			lines = append(lines, "Choose which version to keep. Blueprint will not decide for you.")
		} else {
			lines = append(lines, reconcileConsequence(item))
		}
	case item.Review == syncengine.ReviewMachine || item.Deferred || (item.MachineResolution != "" && item.MachineResolution != syncengine.MachineResolutionNone):
		lines = append(lines,
			"This setting is different on this computer.",
			"",
			"Saved profile: "+plainState(item.Local),
			"This computer: "+here,
			"")
		if item.Review != syncengine.ReviewNone || item.Deferred {
			lines = append(lines, "Choose whether to save this computer's setting or apply the saved one.")
		} else {
			lines = append(lines, reconcileConsequence(item))
		}
	case reconcileGroup(item) == reconcileProtected:
		capture, restoreAllowed := reconcilePolicyText(item.Policy)
		lines = append(lines,
			"This setting is different, but your Blueprint settings say to leave it alone.",
			"",
			"Saved profile: "+saved,
			"This computer: "+here,
			"Capture allowed: "+capture,
			"Restore allowed: "+restoreAllowed,
			"",
			"Sync will not change it unless you deliberately change those permissions. Press c to review this setting's Capture settings.")
	case restore && save:
		lines = append(lines,
			"Running Sync would do more than one thing with this setting.",
			"",
			"Saved profile: "+saved,
			"This computer: "+here,
			"",
			"It would send the saved change to your shared profile, and change this setting on this computer to match.")
	case restore:
		lines = append(lines,
			"This setting is different on this computer.",
			"",
			"Saved profile: "+saved,
			"This computer: "+here,
			"")
		if removes(item) {
			lines = append(lines, "If you apply the saved version, Blueprint will remove this setting from this computer.")
		} else {
			lines = append(lines, "If you apply the saved version, Blueprint will change this setting on this computer.")
		}
	case save && captures(item):
		lines = append(lines,
			"This computer has a change that Blueprint is allowed to save.",
			"",
			"Saved profile: "+plainState(item.Remote),
			"This computer: "+here,
			"",
			"Running Sync can add this change to your shared profile so your other computers can receive it.")
	case save:
		lines = append(lines,
			"Your saved profile on this computer has a change that hasn't been shared yet.",
			"",
			"Saved on this computer: "+plainState(item.Local),
			"Shared profile: "+plainState(item.Remote),
			"",
			"Running Sync can send it to your shared profile so your other computers can receive it.")
	case item.Settled:
		lines = append(lines, "This setting already matches your saved profile. Nothing will change.", "", "Saved profile: "+saved, "This computer: "+here)
	default:
		lines = append(lines, "Blueprint has nothing to do for this setting.", "", "Saved profile: "+saved, "This computer: "+here)
	}
	if label := reconcileDecisionLabel(item); label != "" {
		lines = append(lines, "", "Your decision: "+label)
	} else if item.Deferred {
		lines = append(lines, "", "Your decision: Decide later. Sync can't be completed until you decide.")
	}
	if choices := s.availableChoices(item); len(choices) > 0 {
		title := "Your choices"
		if item.Review == syncengine.ReviewNone {
			title = "Change your decision"
		}
		lines = append(lines, "", title)
		for _, choice := range choices {
			info := reconcileChoiceInfo[choice]
			lines = append(lines, info.key+"  "+info.label+" — "+info.meaning)
		}
		if item.Review == syncengine.ReviewMachine && !item.Policy.Capture {
			lines = append(lines, "Saving this computer's setting isn't offered because your Capture settings don't allow it.")
		}
	}
	capture, restoreAllowed := reconcilePolicyText(item.Policy)
	actions := make([]string, len(item.Actions))
	for i, action := range item.Actions {
		actions[i] = string(action)
	}
	return append(lines, "", "Technical details",
		"Target: "+components.DisplayText(item.Key.Category+":"+item.Key.Target),
		"Last completed Sync (B): "+technicalState(item.Base),
		"Saved on this computer (L): "+technicalState(item.Local),
		"Shared profile (R): "+technicalState(item.Remote),
		"This computer now (M): "+technicalState(item.Machine),
		"History: "+string(item.History)+" · review: "+string(item.Review)+" · actions: "+valueOr(strings.Join(actions, ", "), "none"),
		"Capture allowed: "+capture+" · Restore allowed: "+restoreAllowed,
	)
}
