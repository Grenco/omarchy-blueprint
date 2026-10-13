package screens

import (
	"context"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type reconcileWorkflow interface {
	InspectSync(context.Context, workflow.SyncInspectOptions) (syncengine.Plan, error)
	ExecuteSync(context.Context, workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error)
}

// Reconcile owns presentation and session-only choices, never mutation authority.
// Construction is inert; only entering/explicitly refreshing the screen fetches.
type Reconcile struct {
	bootstrap                         *workflow.SyncBootstrapPlan
	bootstrapChoice                   *workflow.SyncBootstrapChoice
	ctx                               context.Context
	session                           reconcileWorkflow
	width, height, selected           int
	styles                            components.Styles
	base, current                     syncengine.Plan
	choices                           map[syncengine.TargetKey]syncengine.TargetResolution
	checking, ready, confirming, busy bool
	requestID                         uint64
	stop                              context.CancelFunc
	message                           string
}

type reconcileInspectedMsg struct {
	bootstrap *workflow.SyncBootstrapPlan
	id        uint64
	plan      syncengine.Plan
	err       error
}

func NewReconcileContext(ctx context.Context, session reconcileWorkflow) *Reconcile {
	return &Reconcile{ctx: ctx, session: session, choices: map[syncengine.TargetKey]syncengine.TargetResolution{}}
}
func (s *Reconcile) SetSize(w, h int)                   { s.width, s.height = w, h }
func (s *Reconcile) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Reconcile) TransientActive() bool              { return s.confirming || s.busy }
func (s *Reconcile) Init() tea.Cmd                      { return s.inspect(true) }

// Unrelated workspace changes may refresh cached evidence, never fetch.
func (s *Reconcile) RefreshCached() tea.Cmd { return s.inspect(false) }
func (s *Reconcile) inspect(fetch bool) tea.Cmd {
	if s.busy {
		return nil
	}
	if s.stop != nil {
		s.stop()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.stop = cancel
	s.requestID++
	id := s.requestID
	s.checking, s.ready, s.confirming = true, false, false
	session := s.session
	return func() tea.Msg {
		defer cancel()
		if session == nil {
			return reconcileInspectedMsg{id: id, err: fmt.Errorf("Sync session unavailable")}
		}
		p, err := session.InspectSync(ctx, workflow.SyncInspectOptions{Fetch: fetch})
		var bootstrap *workflow.SyncBootstrapPlan
		if err == nil && p.BootstrapRequired {
			if loader, ok := session.(interface {
				InspectSyncBootstrap(context.Context, workflow.SyncInspectOptions) (workflow.SyncBootstrapPlan, error)
			}); ok {
				b, loadErr := loader.InspectSyncBootstrap(ctx, workflow.SyncInspectOptions{Fetch: false})
				err = loadErr
				bootstrap = &b
				p.Identity, p.Git, p.Blockers = b.Identity, b.Git, b.Blockers
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return reconcileInspectedMsg{id: id, plan: p, bootstrap: bootstrap, err: err}
	}
}
func (s *Reconcile) Update(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case reconcileInspectedMsg:
		if m.id != s.requestID {
			return nil
		}
		s.checking = false
		if m.err != nil {
			s.ready = false
			s.message = "Inspection failed or cancelled. Reinspect before execution."
			return nil
		}
		s.base, s.current = m.plan, m.plan
		s.bootstrap, s.bootstrapChoice = m.bootstrap, nil
		s.ready = true
		// A new inspection always starts a new review. Never replay choices
		// over fresh revision/live evidence automatically.
		s.choices = map[syncengine.TargetKey]syncengine.TargetResolution{}
		s.selected = min(s.selected, max(0, len(s.current.Items)-1))
		return nil
	case SyncExecutionComplete:
		s.busy = false
		s.ready = false
		if m.Err != nil {
			s.message = reconcileFailure(m.Err)
		} else if !m.Result.CursorAdvanced {
			s.message = "Sync did not record settlement. Inspect before retrying."
		} else {
			s.message = "Last execution recorded settlement; checking fresh state now."
		}
		return s.inspect(false)
	case tea.KeyPressMsg:
		if s.busy {
			return nil
		}
		if s.checking {
			if m.String() == "esc" && s.stop != nil {
				s.stop()
			}
			return nil
		}
		if s.confirming {
			if m.String() == "esc" {
				s.confirming = false
				return nil
			}
			if m.String() == "enter" {
				s.confirming = false
				return s.execute()
			}
			return nil
		}
		switch m.String() {
		case "u":
			s.chooseBootstrap("reconcile-upstream")
		case "v":
			s.chooseBootstrap("adopt-machine")
		case "esc":
			if s.bootstrap != nil {
				s.bootstrapChoice = nil
				s.current = s.base
			}
		case "enter":
			if s.CanExecute() {
				s.confirming = true
			}
		case "r":
			return s.inspect(true)
		case "j", "down":
			s.selected = min(s.selected+1, max(0, len(s.current.Items)-1))
		case "k", "up":
			s.selected = max(0, s.selected-1)
		case "L":
			s.choose("keep-local")
		case "a":
			s.choose("accept-remote")
		case "m":
			s.choose("keep-machine")
		case "p":
			s.choose("keep-profile")
		case "d":
			s.choose("defer")
		}
	}
	return nil
}
func (s *Reconcile) choose(choice string) {
	if s.bootstrap != nil {
		return
	}
	if !s.ready || len(s.current.Items) == 0 {
		return
	}
	item := s.selectedItem()
	key := item.Key
	resolution := s.choices[key]
	resolution.Key = key
	if item.History == syncengine.HistoryConflict && (choice == "keep-local" || choice == "accept-remote" || choice == "defer") {
		resolution.Resolution = syncengine.Resolution(choice)
		if choice == "defer" {
			resolution.MachineResolution = syncengine.MachineResolutionNone
		}
	} else if item.Review == syncengine.ReviewMachine || item.MachineResolution != syncengine.MachineResolutionNone || item.Deferred {
		if choice != "keep-machine" && choice != "keep-profile" && choice != "defer" {
			return
		}
		resolution.MachineResolution = syncengine.MachineResolution(choice)
	} else {
		return
	}
	next := make(map[syncengine.TargetKey]syncengine.TargetResolution, len(s.choices)+1)
	for k, v := range s.choices {
		next[k] = v
	}
	next[key] = resolution
	resolved, err := workflow.ResolveSyncPlan(s.base, reconcileChoices(next))
	if err != nil {
		s.message = "Choice is not valid for this target; review its available choices."
		return
	}
	s.choices, s.current = next, resolved
	for i, index := range s.presentationOrder() {
		if s.current.Items[index].Key == key {
			s.selected = i
			break
		}
	}
}

func (s *Reconcile) chooseBootstrap(choice workflow.SyncBootstrapChoice) {
	if !s.ready || s.bootstrap == nil {
		return
	}
	plan, err := workflow.ResolveSyncBootstrapPlan(*s.bootstrap, choice)
	if err != nil {
		s.message = "Bootstrap choice is blocked. Review lineage/policy in CLI or Profile Git first."
		return
	}
	s.current, s.bootstrapChoice = plan, &choice
}
func reconcileChoices(choices map[syncengine.TargetKey]syncengine.TargetResolution) []syncengine.TargetResolution {
	result := make([]syncengine.TargetResolution, 0, len(choices))
	for _, v := range choices {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].Key, result[j].Key
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Target < b.Target
	})
	return result
}
func (s *Reconcile) CanExecute() bool {
	if !s.ready || s.checking || s.busy || s.current.BootstrapRequired || len(s.current.Blockers) > 0 || s.current.Identity.Local == "" {
		return false
	}
	for _, item := range s.current.Items {
		if item.Deferred || item.Review != syncengine.ReviewNone {
			return false
		}
	}
	return true
}

func (s *Reconcile) Actions() []SyncAction {
	return []SyncAction{
		{"reconcile.bootstrap-upstream", "Bootstrap: Reconcile Upstream", "u", "missing cursor and unblocked bootstrap required", s.ready && !s.busy && s.bootstrap != nil && len(s.base.Blockers) == 0},
		{"reconcile.bootstrap-machine", "Bootstrap: Adopt Machine", "v", "missing cursor and unblocked bootstrap required", s.ready && !s.busy && s.bootstrap != nil && len(s.base.Blockers) == 0},
		{"reconcile.refresh", "Fetch and inspect Sync", "r", "Sync is busy", !s.busy && !s.checking},
		{"reconcile.keep-local", "Keep Local", "L", "select a history conflict", s.ready && !s.busy},
		{"reconcile.accept-remote", "Accept Remote", "a", "select a history conflict", s.ready && !s.busy},
		{"reconcile.defer", "Defer semantic intent", "d", "select a conflict", s.ready && !s.busy},
		{"reconcile.keep-machine", "Keep Machine", "m", "select machine drift", s.ready && !s.busy},
		{"reconcile.keep-profile", "Keep Profile", "p", "select machine drift", s.ready && !s.busy},
		{"reconcile.execute", "Execute reconciliation", "enter", "reinspect and resolve blockers/deferred intent first", s.CanExecute()},
	}
}
func reconcileGroup(item syncengine.Item) string {
	if item.Deferred {
		return "Deferred"
	}
	if item.Review != syncengine.ReviewNone {
		return "Conflicts"
	}
	switch item.History {
	case syncengine.HistoryLocal:
		return "Local"
	case syncengine.HistoryRemote:
		return "Remote"
	case syncengine.HistoryConverged:
		return "Converged"
	}
	if len(item.Actions) > 0 {
		return "Machine"
	}
	return "Unchanged"
}

var reconcileGroups = []string{"Conflicts", "Remote", "Local", "Machine", "Converged", "Deferred", "Unchanged"}

// Group only presentation indices; the reviewed workflow plan retains its
// canonical target/operation ordering and fingerprint.
func (s *Reconcile) presentationOrder() []int {
	order := make([]int, 0, len(s.current.Items))
	for _, group := range reconcileGroups {
		for i, item := range s.current.Items {
			if reconcileGroup(item) == group {
				order = append(order, i)
			}
		}
	}
	return order
}

func (s *Reconcile) selectedItem() syncengine.Item {
	return s.current.Items[s.presentationOrder()[s.selected]]
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
func (s *Reconcile) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}
	lines := []string{"Sync — semantic reconciliation"}
	if s.message != "" {
		lines = append(lines, s.message)
	}
	if s.confirming {
		body := components.WrapText("Execute reconciliation?\n"+s.stageSummary()+"\nFresh Capture and Restore require separate approval.", w)
		return strings.Join(fitBlock(body, []string{"Enter: proceed · Esc: cancel"}, s.height, w), "\n")
	}
	if s.busy {
		return "Sync execution has the terminal. Waiting for it to finish…"
	}
	if s.checking {
		lines = append(lines, "Checking machine and pinned profile revisions… (Esc cancels)")
	} else if !s.ready {
		lines = append(lines, "Reinspect before execution.")
	} else {
		id := s.current.Identity
		lines = append(lines, fmt.Sprintf("Machine %s · B %s · L %s · R %s", components.DisplayText(id.Machine), shortRevision(id.Base), shortRevision(id.Local), shortRevision(id.Remote)), fmt.Sprintf("Branch %s · Upstream %s", components.DisplayText(id.Branch), components.DisplayText(id.Upstream)))
		if s.current.Control != nil {
			lines = append(lines, fmt.Sprintf("Control plane: %s · selected %s", s.current.Control.History, s.current.Control.SelectedSource))
		}
		if s.current.BootstrapRequired {
			lines = append(lines, "First run: choose u Reconcile Upstream / v Adopt Machine.", "Esc cancels; bootstrap is baseless and freshly verified before cursor creation.")
		}
		if s.bootstrapChoice != nil {
			lines = append(lines, "Bootstrap choice: "+string(*s.bootstrapChoice))
		}
		for _, b := range s.current.Blockers {
			lines = append(lines, "Blocked: "+components.DisplayText(b.Code)+" — "+components.DisplayText(b.Message))
		}
		counts := map[string]int{}
		for _, item := range s.current.Items {
			counts[reconcileGroup(item)]++
		}
		var parts []string
		for _, g := range reconcileGroups {
			if counts[g] > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", g, counts[g]))
			}
		}
		lines = append(lines, strings.Join(parts, " · "), "")
		budget := max(1, s.height-len(lines)-2)
		start := max(0, min(s.selected-budget/2, len(s.current.Items)-budget))
		order := s.presentationOrder()
		for i := start; i < min(len(s.current.Items), start+budget); i++ {
			item := s.current.Items[order[i]]
			mark := "  "
			if i == s.selected {
				mark = "› "
			}
			lines = append(lines, mark+reconcileGroup(item)+" · "+components.DisplayText(item.Key.Category+":"+item.Key.Target))
		}
	}
	var bounded []string
	for _, line := range lines {
		bounded = append(bounded, ansi.Truncate(line, w, "…"))
	}
	if s.height > 0 && len(bounded) > s.height {
		bounded = bounded[:s.height]
	}
	return strings.Join(bounded, "\n")
}
func (s *Reconcile) DetailView() string {
	lines := []string{"Read-only inspection; choices do not mutate profile or machine."}
	if s.message != "" {
		lines = append(lines, s.message)
	}
	if s.ready {
		if control := s.current.Control; control != nil {
			lines = append(lines, fmt.Sprintf("Control plane: %s · selected %s", control.History, control.SelectedSource))
		}
		for _, blocker := range s.current.Blockers {
			lines = append(lines, "Blocked: "+components.DisplayText(blocker.Code)+" — "+components.DisplayText(blocker.Message))
		}
	}
	if !s.ready || len(s.current.Items) == 0 {
		return strings.Join(lines, "\n")
	}
	item := s.selectedItem()
	lines = append(lines, "", components.DisplayText(item.Key.Category+":"+item.Key.Target))
	for _, v := range []struct {
		name  string
		state syncengine.State
	}{{"B", item.Base}, {"L", item.Local}, {"R", item.Remote}, {"M", item.Machine}} {
		lines = append(lines, fmt.Sprintf("%s: %s · %s", v.name, v.state.Kind, components.DisplayText(v.state.Summary)))
	}
	lines = append(lines, "History: "+string(item.History), "History choice: "+string(item.Resolution), "Machine choice: "+string(item.MachineResolution), "Available: "+strings.Join(item.Choices, ", "), "L Keep Local · a Accept Remote · d Defer", "m Keep Machine · p Keep Profile", "Enter: review execution · r: new inspection")
	return strings.Join(lines, "\n")
}
