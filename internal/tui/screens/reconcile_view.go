package screens

import (
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/charmbracelet/x/ansi"
)

// Everything in this file is presentation: grouping, wording and layout of
// the Sync review. It never decides which choices are valid or what
// execution receives; choose, CanExecute and the workflow own that.

const (
	reconcileNeedsDecision = "Needs decision"
	reconcileDeferred      = "Deferred"
	reconcileDecided       = "Decided"
	reconcileIncoming      = "Incoming"
	reconcileOutgoing      = "Outgoing"
	reconcileMachineOnly   = "This machine"
	reconcileConverged     = "Same on both sides"
	reconcileUnchanged     = "Unchanged"
)

// Review priority: what blocks a run first, then what a run will do, then
// what needs nothing.
var reconcileGroups = []string{reconcileNeedsDecision, reconcileDeferred, reconcileDecided, reconcileIncoming, reconcileOutgoing, reconcileMachineOnly, reconcileConverged, reconcileUnchanged}

var reconcileGroupIcons = map[string]string{
	reconcileNeedsDecision: components.Icons.Attention,
	reconcileDeferred:      "‖",
	reconcileDecided:       "•",
	reconcileIncoming:      "↓",
	reconcileOutgoing:      "↑",
	reconcileMachineOnly:   components.Icons.Changed,
	reconcileConverged:     "=",
	reconcileUnchanged:     "·",
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
	switch item.History {
	case syncengine.HistoryLocal:
		return reconcileOutgoing
	case syncengine.HistoryRemote:
		return reconcileIncoming
	case syncengine.HistoryConverged:
		return reconcileConverged
	}
	if len(item.Actions) > 0 {
		return reconcileMachineOnly
	}
	return reconcileUnchanged
}

type reconcileChoice struct{ key, label, meaning string }

var reconcileChoiceInfo = map[string]reconcileChoice{
	string(syncengine.ResolutionKeepLocal):           {"L", "Keep local", "your committed profile wins and is published"},
	string(syncengine.ResolutionAcceptRemote):        {"a", "Accept remote", "the upstream value wins and is restored here"},
	string(syncengine.ResolutionDefer):               {"d", "Defer", "decide later; this machine can't settle until you do"},
	string(syncengine.MachineResolutionKeepMachine):  {"m", "Keep machine", "capture this machine's state into the profile"},
	string(syncengine.MachineResolutionKeepProfile):  {"p", "Keep profile", "restore this machine from the profile"},
	string(syncengine.MachineResolutionDefer) + "/m": {"d", "Defer", "leave this machine as is; it can't settle until you decide"},
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
	if !s.ready || s.busy || len(s.current.Items) == 0 {
		return available
	}
	for _, choice := range s.availableChoices(s.selectedItem()) {
		available[reconcileChoiceInfo[choice].label] = true
	}
	return available
}

func (s *Reconcile) Actions() []SyncAction {
	choices := s.selectedChoices()
	bootstrap := s.ready && !s.busy && s.bootstrap != nil && len(s.base.Blockers) == 0
	return []SyncAction{
		{"reconcile.keep-local", "Keep local", "L", "select a target changed on both sides", choices["Keep local"]},
		{"reconcile.accept-remote", "Accept remote", "a", "select a target changed on both sides", choices["Accept remote"]},
		{"reconcile.keep-machine", "Keep machine", "m", "select a target where this machine differs", choices["Keep machine"]},
		{"reconcile.keep-profile", "Keep profile", "p", "select a target where this machine differs", choices["Keep profile"]},
		{"reconcile.defer", "Defer", "d", "select a target that needs a decision", choices["Defer"]},
		{"reconcile.bootstrap-upstream", "Start from upstream", "u", "only for a first sync without blockers", bootstrap},
		{"reconcile.bootstrap-machine", "Start from this machine", "v", "only for a first sync without blockers", bootstrap},
		{"reconcile.execute", "Review & run", "enter", "decide every target and clear blockers first", s.CanExecute()},
		{"reconcile.refresh", "Fetch & inspect", "r", "Sync is busy", !s.busy && !s.checking},
	}
}

func reconcileDecisionLabel(item syncengine.Item) string {
	var parts []string
	switch item.Resolution {
	case syncengine.ResolutionKeepLocal:
		parts = append(parts, "Keep local")
	case syncengine.ResolutionAcceptRemote:
		parts = append(parts, "Accept remote")
	}
	switch item.MachineResolution {
	case syncengine.MachineResolutionKeepMachine:
		parts = append(parts, "Keep machine")
	case syncengine.MachineResolutionKeepProfile:
		parts = append(parts, "Keep profile")
	}
	return strings.Join(parts, " + ")
}

// reconcileStatus is the table cell: a decided target shows its decision.
func reconcileStatus(item syncengine.Item) string {
	switch {
	case item.Review == syncengine.ReviewHistory:
		return "Changed on both sides"
	case item.Review == syncengine.ReviewMachine:
		return "Machine differs"
	case item.Deferred:
		return "Deferred"
	}
	if label := reconcileDecisionLabel(item); label != "" {
		return label
	}
	return reconcileSituation(item)
}

// reconcileSituation describes what changed, independent of any decision.
func reconcileSituation(item syncengine.Item) string {
	switch {
	case item.History == syncengine.HistoryConflict:
		return "Changed on both sides"
	case item.Review == syncengine.ReviewMachine, item.MachineResolution != "" && item.MachineResolution != syncengine.MachineResolutionNone:
		return "Machine differs"
	}
	switch item.History {
	case syncengine.HistoryRemote:
		return "Changed upstream"
	case syncengine.HistoryLocal:
		return "Changed locally"
	case syncengine.HistoryConverged:
		return "Same change on both"
	}
	for _, action := range item.Actions {
		switch action {
		case syncengine.ActionCapture:
			return "Changed on machine"
		case syncengine.ActionRestore:
			return "Machine differs"
		}
	}
	return "No change"
}

func reconcileNext(s *Reconcile, item syncengine.Item) string {
	if item.Review != syncengine.ReviewNone {
		var keys []string
		for _, choice := range s.availableChoices(item) {
			keys = append(keys, reconcileChoiceInfo[choice].key)
		}
		if len(keys) == 0 {
			return "Decide"
		}
		return "Choose " + strings.Join(keys, " ")
	}
	if item.Deferred {
		return "Held back"
	}
	if len(item.Actions) > 0 {
		return reconcileActionFlow(item.Actions)
	}
	if item.Inapplicable {
		return "Skipped by policy"
	}
	return "Nothing to do"
}

func reconcileActionFlow(actions []syncengine.PlannedAction) string {
	parts := make([]string, len(actions))
	for i, action := range actions {
		parts[i] = titleMode(string(action))
	}
	return strings.Join(parts, " → ")
}

func shortRevision(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	if sha == "" {
		return "—"
	}
	return sha
}

func (s *Reconcile) widthOrDefault() int {
	if s.width <= 0 {
		return 80
	}
	return s.width
}

// verdict is the one line that answers "can I run Sync, and if not why".
func (s *Reconcile) verdict() string {
	plan := s.current
	review, deferred := 0, 0
	for _, item := range plan.Items {
		if item.Deferred {
			deferred++
		} else if item.Review != syncengine.ReviewNone {
			review++
		}
	}
	switch {
	case len(plan.Blockers) > 0:
		return s.styles.Warning(components.Icons.Blocked + " Blocked — Sync can't run until the issues below are resolved")
	case plan.BootstrapRequired && s.bootstrapChoice == nil:
		return s.styles.Warning(components.Icons.Attention + " First sync on this machine — choose where to start")
	case review > 0:
		return s.styles.Warning(fmt.Sprintf("%s %s before Sync can run", components.Icons.Attention, plural(review, "decision needed", "decisions needed")))
	case deferred > 0:
		return s.styles.Warning(fmt.Sprintf("%s %s — Sync can't run while intent is deferred", components.Icons.Attention, plural(deferred, "target deferred", "targets deferred")))
	case !s.CanExecute():
		return s.styles.Warning(components.Icons.Attention + " Not ready to run")
	}
	counts := s.actionCounts()
	if counts[syncengine.ActionCapture]+counts[syncengine.ActionPublish]+counts[syncengine.ActionRestore] == 0 {
		return s.styles.Success(components.Icons.Ready + " Nothing to change · Enter reviews the run")
	}
	var parts []string
	for _, action := range []syncengine.PlannedAction{syncengine.ActionCapture, syncengine.ActionPublish, syncengine.ActionRestore} {
		if counts[action] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[action], strings.ToLower(titleMode(string(action)))))
		}
	}
	return s.styles.Success(components.Icons.Ready+" Ready to sync") + s.styles.Muted(" · "+strings.Join(parts, " · ")+" · Enter to review")
}

func (s *Reconcile) actionCounts() map[syncengine.PlannedAction]int {
	counts := map[syncengine.PlannedAction]int{}
	for _, item := range s.current.Items {
		for _, action := range item.Actions {
			counts[action]++
		}
	}
	return counts
}

func (s *Reconcile) identityLines() []string {
	id, git := s.current.Identity, s.current.Git
	label := s.styles.Muted
	where := []string{components.DisplayText(valueOr(id.Machine, "this machine")), components.DisplayText(valueOr(id.Branch, "detached")) + " → " + components.DisplayText(valueOr(id.Upstream, "no upstream"))}
	var history []string
	if git.Ahead > 0 {
		history = append(history, fmt.Sprintf("%d ahead", git.Ahead))
	}
	if git.Behind > 0 {
		history = append(history, fmt.Sprintf("%d behind", git.Behind))
	}
	if git.Diverged {
		history = append(history, "diverged")
	}
	if !git.Clean {
		history = append(history, "uncommitted changes")
	}
	if len(history) > 0 {
		where = append(where, strings.Join(history, ", "))
	}
	lines := []string{strings.Join(where, label(" · ")), s.revisionsLine()}
	if control := s.current.Control; control != nil {
		lines = append(lines, label("Control settings ")+reconcileControlText(*control))
	}
	return lines
}

func (s *Reconcile) revisionsLine() string {
	id, label := s.current.Identity, s.styles.Muted
	base := shortRevision(id.Base)
	if id.Base == "" {
		base = "none yet"
	}
	return label("Last sync ") + base + label(" · Local ") + shortRevision(id.Local) + label(" · Remote ") + shortRevision(id.Remote)
}

func reconcileControlText(control syncengine.ControlChange) string {
	var change string
	switch control.History {
	case syncengine.HistoryUnchanged:
		change = "unchanged"
	case syncengine.HistoryLocal:
		change = "changed locally"
	case syncengine.HistoryRemote:
		change = "changed upstream"
	case syncengine.HistoryConverged:
		change = "same change on both sides"
	case syncengine.HistoryConflict:
		return "changed on both sides — blocks Sync"
	default:
		change = components.DisplayText(string(control.History))
	}
	switch control.SelectedSource {
	case syncengine.ControlSourceLocal:
		return change + " · using local"
	case syncengine.ControlSourceRemote:
		return change + " · using remote"
	}
	return change
}

func (s *Reconcile) blockerLines(width int) []string {
	var lines []string
	for _, b := range s.current.Blockers {
		text := components.Icons.Blocked + " " + components.DisplayText(b.Message)
		if b.Target != nil {
			text += " (" + components.DisplayText(b.Target.Category+" · "+b.Target.Target) + ")"
		}
		for _, line := range components.WrapText(text+" ["+components.DisplayText(b.Code)+"]", width) {
			lines = append(lines, s.styles.Warning(line))
		}
	}
	return lines
}

func (s *Reconcile) messageLines(width int) []string {
	if s.message == "" {
		return nil
	}
	// Every message is a warning except the settled-run receipt.
	style := s.styles.Warning
	if strings.HasPrefix(s.message, "Last execution recorded settlement") {
		style = s.styles.Success
	}
	lines := components.WrapText(s.message, width)
	for i := range lines {
		lines[i] = style(lines[i])
	}
	return lines
}

func (s *Reconcile) bootstrapGuide(width int) []string {
	if s.bootstrap == nil || s.bootstrapChoice != nil || len(s.current.Blockers) > 0 {
		return nil
	}
	upstream := components.DisplayText(valueOr(s.current.Identity.Upstream, "upstream"))
	lines := []string{"", components.SectionDivider("Where should this machine start?", width, s.styles)}
	for _, option := range [][2]string{
		{"u", "Start from upstream — review " + upstream + " against this machine"},
		{"v", "Start from this machine — capture and publish what's here"},
		{"Esc", "Leave it — nothing is written"},
	} {
		lines = append(lines, components.WrapText(fmt.Sprintf("%s %s", s.styles.Accent(fmt.Sprintf("%-3s", option[0])), option[1]), width)...)
	}
	if total := len(s.bootstrap.Items); total > 0 {
		matching := 0
		for _, item := range s.bootstrap.Items {
			if item.Converged {
				matching++
			}
		}
		lines = append(lines, s.styles.Muted(fmt.Sprintf("%s inspected · %d already match upstream", plural(total, "target", "targets"), matching)))
	}
	return lines
}

func (s *Reconcile) bootstrapChoiceLine() []string {
	if s.bootstrapChoice == nil {
		return nil
	}
	label := "Start from upstream"
	if *s.bootstrapChoice == "adopt-machine" {
		label = "Start from this machine"
	}
	return []string{s.styles.Accent("First sync: "+label) + s.styles.Muted(" · Esc to change")}
}

// reconcileMinTableRows keeps a group heading and a couple of targets.
const reconcileMinTableRows = 4

func (s *Reconcile) View() string {
	width := s.widthOrDefault()
	switch {
	case s.busy:
		return "Sync has the terminal. Waiting for it to finish…"
	case s.confirming:
		return strings.Join(s.confirmationView(width), "\n")
	case s.checking:
		lines := append([]string{s.styles.Accent("Fetching upstream and inspecting this machine…")}, s.messageLines(width)...)
		lines = append(lines, "", s.styles.Muted("Inspection is read-only. Esc cancels."))
		return s.bound(lines, width)
	case !s.ready:
		lines := append(s.messageLines(width), "", "Press r to fetch and inspect again.")
		return s.bound(lines, width)
	}
	essential := append([]string{s.verdict()}, s.messageLines(width)...)
	essential = append(essential, s.blockerLines(width)...)
	essential = append(essential, s.bootstrapChoiceLine()...)
	optional := s.identityLines()
	optional = append(optional, s.bootstrapGuide(width)...)
	if len(s.current.Items) == 0 {
		lines := append(essential, optional...)
		if s.bootstrap == nil {
			lines = append(lines, "", s.styles.Muted("No Blueprint targets to compare."))
		}
		return s.bound(lines, width)
	}
	// Keep the verdict, warnings and the target list; shed context lines from
	// the bottom up when the workspace is short.
	if s.height > 0 {
		for len(optional) > 0 && len(essential)+len(optional)+1+1+reconcileMinTableRows > s.height {
			optional = optional[:len(optional)-1]
		}
	}
	header := append(essential, optional...)
	header = append(header, "")
	tableHeight := len(s.current.Items) + len(reconcileGroups) + 1
	if s.height > 0 {
		tableHeight = max(2, s.height-len(header))
	}
	return s.bound(append(header, s.targetTable(width, tableHeight)), width)
}

func (s *Reconcile) targetTable(width, height int) string {
	order := s.presentationOrder()
	counts := map[string]int{}
	for _, index := range order {
		counts[reconcileGroup(s.current.Items[index])]++
	}
	var rows []components.Row
	selectedRow, group := 0, ""
	for i, index := range order {
		item := s.current.Items[index]
		if next := reconcileGroup(item); next != group {
			group = next
			rows = append(rows, components.Row{Divider: true, Cells: []string{s.styles.Accent(group) + " " + s.styles.Muted(fmt.Sprintf("(%d)", counts[group]))}})
		}
		selected := i == s.selected
		if selected {
			selectedRow = len(rows)
		}
		rows = append(rows, components.Row{Cells: s.targetCells(item, group, selected), Selected: selected, Focused: true})
	}
	// Bring the group heading into view with its first target.
	if selectedRow > 0 && rows[selectedRow-1].Divider {
		s.table.Ensure(selectedRow-1, len(rows), height-1)
	}
	s.table.Ensure(selectedRow, len(rows), height-1)
	columns := []components.Column{{Title: "TARGET", Width: 36, MinWidth: 18}, {Title: "STATUS", Width: 22, MinWidth: 12}, {Title: "NEXT", MinWidth: 10}}
	return s.table.Render(columns, rows, width, height, s.styles)
}

func (s *Reconcile) targetCells(item syncengine.Item, group string, selected bool) []string {
	icon := reconcileGroupIcons[group]
	category := components.DisplayText(item.Key.Category)
	target := components.DisplayText(item.Key.Target)
	status, next := reconcileStatus(item), reconcileNext(s, item)
	if selected {
		return []string{icon + " " + category + " · " + target, status, next}
	}
	// Attention groups colour their whole row; quiet groups recede; the rest
	// keep plain status text with an accent icon.
	tone, statusStyle := s.styles.Accent, func(v string) string { return v }
	switch group {
	case reconcileNeedsDecision, reconcileDeferred:
		tone, statusStyle = s.styles.Warning, s.styles.Warning
	case reconcileConverged, reconcileUnchanged:
		tone, statusStyle = s.styles.Muted, s.styles.Muted
	}
	nextStyle := s.styles.Muted
	if item.Review != syncengine.ReviewNone {
		nextStyle = s.styles.Accent
	}
	return []string{tone(icon) + " " + s.styles.Muted(category+" ·") + " " + target, statusStyle(status), nextStyle(next)}
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

// confirmationView describes the reviewed run in the order ExecuteSync owns.
// Counts come from the reviewed plan and are descriptive; the workflow
// revalidates and replans every stage before it acts.
func (s *Reconcile) confirmationView(width int) []string {
	counts := s.actionCounts()
	id := s.current.Identity
	upstream := components.DisplayText(valueOr(id.Upstream, "upstream"))
	targets := func(n int) string { return plural(n, "target", "targets") }
	step := func(n int, title, note string, active bool) string {
		text := fmt.Sprintf(" %d  %-22s %s", n, title, note)
		if !active {
			return s.styles.Muted(text)
		}
		return s.styles.Accent(fmt.Sprintf(" %d", n)) + fmt.Sprintf("  %-22s ", title) + s.styles.Muted(note)
	}
	body := []string{
		components.SectionDivider("Run Sync?", width, s.styles),
		s.styles.Muted("Pinned  ") + s.revisionsLine(),
		"",
		step(1, "Revalidate", "stop if anything changed since this review", true),
		step(2, "Reconcile history", "only if local and remote both moved", true),
		step(3, "Capture "+targets(counts[syncengine.ActionCapture]), "asks for fresh approval", counts[syncengine.ActionCapture] > 0),
		step(4, "Publish", "push to "+upstream+" if needed", true),
		step(5, "Restore "+targets(counts[syncengine.ActionRestore]), "asks for fresh approval", counts[syncengine.ActionRestore] > 0),
		step(6, "Verify & record", "last sync advances only once settled", true),
		"",
	}
	for _, line := range components.WrapText("The terminal is handed to Sync while it runs.", width) {
		body = append(body, s.styles.Muted(line))
	}
	if s.height > 0 && len(body)+1 > s.height {
		// Short workspace: keep the steps, drop spacing and the note.
		body = append(body[:2], body[3:9]...)
	}
	for i, line := range body {
		body[i] = ansi.Truncate(line, width, "…")
	}
	return fitBlock(body, []string{s.styles.Accent("Enter") + " run · " + s.styles.Accent("Esc") + " back"}, s.height, width)
}

func (s *Reconcile) DetailView() string {
	if s.ready && len(s.current.Items) > 0 && !s.confirming {
		return strings.Join(append(s.itemDetail(s.selectedItem()), s.contextDetail()...), "\n")
	}
	lines := []string{"Sync"}
	if s.ready && s.bootstrap != nil && s.bootstrapChoice == nil {
		lines = append(lines, s.bootstrapDetail()...)
	} else {
		lines = append(lines,
			"Sync compares four versions of every target:",
			"Last sync (B): what this machine last settled on",
			"Local (L): your committed profile",
			"Remote (R): the fetched upstream profile",
			"Machine (M): what is live on this machine now",
		)
	}
	return strings.Join(append(lines, s.contextDetail()...), "\n")
}

func (s *Reconcile) bootstrapDetail() []string {
	lines := []string{"There is no last sync for this machine yet, so nothing can be compared against a shared starting point. Pick one:"}
	for _, choice := range []struct{ id, key, label string }{{"reconcile-upstream", "u", "Start from upstream"}, {"adopt-machine", "v", "Start from this machine"}} {
		lines = append(lines, "", choice.key+"  "+choice.label)
		for _, requirement := range s.bootstrap.ChoiceRequirements[choice.id] {
			lines = append(lines, "  · "+components.DisplayText(requirement))
		}
	}
	return append(lines, "", "Neither choice invents a last sync; it is recorded only after the run is verified.")
}

func (s *Reconcile) contextDetail() []string {
	var lines []string
	if s.message != "" {
		lines = append(lines, "", s.message)
	}
	if s.ready {
		if control := s.current.Control; control != nil {
			lines = append(lines, "", "Control settings: "+reconcileControlText(*control), "Policy and machine settings are compared separately from targets.")
		}
		for _, blocker := range s.current.Blockers {
			lines = append(lines, "", "Blocked ("+components.DisplayText(blocker.Code)+") — "+components.DisplayText(blocker.Message))
		}
	}
	return append(lines, "", "Inspection is read-only. Choices change nothing until you run Sync.")
}

func reconcileStateText(state syncengine.State) string {
	var kind string
	switch state.Kind {
	case syncengine.StatePresent:
		kind = "Present"
	case syncengine.StateAbsent:
		kind = "Absent"
	case syncengine.StateUnmanaged:
		kind = "Not managed"
	case "":
		return "—"
	default:
		kind = components.DisplayText(string(state.Kind))
	}
	if state.Summary == "" {
		return kind
	}
	if state.Kind == syncengine.StatePresent {
		return components.DisplayText(state.Summary)
	}
	return kind + " · " + components.DisplayText(state.Summary)
}

func (s *Reconcile) itemDetail(item syncengine.Item) []string {
	group := reconcileGroup(item)
	decision := reconcileDecisionLabel(item)
	switch {
	case item.Deferred:
		decision = "Defer"
	case decision == "" && item.Review != syncengine.ReviewNone:
		decision = "Not decided yet"
	}
	lines := []string{
		components.DisplayText(item.Key.Category) + " · " + components.DisplayText(item.Key.Target),
		"Status: " + reconcileGroupIcons[group] + " " + reconcileSituation(item),
	}
	if decision != "" {
		lines = append(lines, "Decision: "+decision)
	}
	lines = append(lines, "Next: "+reconcileNext(s, item), "")

	baseless := s.bootstrapChoice != nil
	compare := func(state syncengine.State) string {
		if baseless {
			return ""
		}
		if state.Equal(item.Base) {
			return " (unchanged)"
		}
		return " (changed)"
	}
	if !baseless {
		lines = append(lines, "Last sync (B): "+reconcileStateText(item.Base))
	}
	lines = append(lines,
		"Local (L): "+reconcileStateText(item.Local)+compare(item.Local),
		"Remote (R): "+reconcileStateText(item.Remote)+compare(item.Remote),
		"Machine (M): "+reconcileStateText(item.Machine)+reconcileMachineMatch(item),
		"Policy: "+reconcilePolicyText(item.Policy),
	)
	if item.Inapplicable {
		lines = append(lines, "Policy leaves this machine as it is for this target.")
	}
	if choices := s.availableChoices(item); len(choices) > 0 {
		title := "Choices"
		if item.Review == syncengine.ReviewNone {
			title = "Change decision"
		}
		lines = append(lines, "", title)
		for _, choice := range choices {
			info := reconcileChoiceInfo[choice]
			lines = append(lines, info.key+"  "+info.label+" — "+info.meaning)
		}
	}
	return lines
}

func reconcileMachineMatch(item syncengine.Item) string {
	switch {
	case item.Machine.Equal(item.Local) && item.Machine.Equal(item.Remote):
		return " (matches both)"
	case item.Machine.Equal(item.Local):
		return " (matches Local)"
	case item.Machine.Equal(item.Remote):
		return " (matches Remote)"
	}
	return " (differs from both)"
}

func reconcilePolicyText(policy syncengine.EffectivePolicy) string {
	allowed := func(name string, ok bool) string {
		if ok {
			return name + " allowed"
		}
		return name + " off"
	}
	return allowed("Capture", policy.Capture) + " · " + allowed("Restore", policy.Restore)
}
