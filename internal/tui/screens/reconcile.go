package screens

import (
	"context"
	"errors"
	"fmt"
	"sort"

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
	bootstrap       *workflow.SyncBootstrapPlan
	bootstrapChoice *workflow.SyncBootstrapChoice
	// setupRefusals preflights each first-time choice through the same
	// ResolveSyncBootstrapPlan that execution uses, so the screen never offers
	// a choice the workflow already refuses.
	setupRefusals map[workflow.SyncBootstrapChoice]*workflow.SyncBootstrapUnavailableError
	ctx           context.Context
	session       reconcileWorkflow
	width, height int
	styles        components.Styles
	base, current syncengine.Plan
	choices       map[syncengine.TargetKey]syncengine.TargetResolution
	// expanded holds the user's explicit open/closed choice per group; groups
	// without one use reconcileGroupDefault. It survives navigation and new
	// inspections because group names are stable.
	expanded                          map[string]bool
	list                              components.Selectable
	table                             components.Table
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
	return &Reconcile{ctx: ctx, session: session, choices: map[syncengine.TargetKey]syncengine.TargetResolution{}, expanded: map[string]bool{}}
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
			s.message = reconcileCheckFailed
			return nil
		}
		previous, hadPrevious := s.selectedRowKey()
		s.base, s.current = m.plan, m.plan
		s.bootstrap, s.bootstrapChoice = m.bootstrap, nil
		s.setupRefusals = nil
		if m.bootstrap != nil {
			s.setupRefusals = map[workflow.SyncBootstrapChoice]*workflow.SyncBootstrapUnavailableError{}
			for _, choice := range []workflow.SyncBootstrapChoice{"reconcile-upstream", "adopt-machine"} {
				var refused *workflow.SyncBootstrapUnavailableError
				if _, err := workflow.ResolveSyncBootstrapPlan(*m.bootstrap, choice); errors.As(err, &refused) {
					s.setupRefusals[choice] = refused
				}
			}
		}
		s.ready = true
		// A new inspection always starts a new review. Never replay choices
		// over fresh revision/live evidence automatically.
		s.choices = map[syncengine.TargetKey]syncengine.TargetResolution{}
		s.selectAfterInspection(previous, hadPrevious)
		return nil
	case SyncExecutionComplete:
		s.busy = false
		s.ready = false
		switch {
		case m.Err != nil:
			s.message = reconcileFailure(m.Err)
		case !m.Result.CursorAdvanced:
			s.message = reconcileIncomplete
		default:
			s.message = reconcileCompleted
		}
		return s.inspect(false)
	case tea.KeyPressMsg:
		return s.handleKey(m.String())
	}
	return nil
}

func (s *Reconcile) handleKey(key string) tea.Cmd {
	if s.busy {
		return nil
	}
	if s.checking {
		if key == "esc" && s.stop != nil {
			s.stop()
		}
		return nil
	}
	if s.confirming {
		switch key {
		case "enter":
			s.confirming = false
			return s.execute()
		case "esc", "b":
			s.confirming = false
		case "c":
			// Nothing has run yet: discard this review's decisions so the user
			// starts again from what Blueprint found.
			s.confirming = false
			s.choices = map[syncengine.TargetKey]syncengine.TargetResolution{}
			s.current, s.bootstrapChoice = s.base, nil
			s.message = reconcileCancelledRun
			s.list.SetSelected(0, len(s.rows()), s.listHeight())
		}
		return nil
	}
	rows := s.rows()
	if key == "[" || key == "]" {
		s.list.JumpToAnchor(s.groupAnchors(), key == "]", len(rows), s.listHeight())
		return nil
	}
	if s.list.Vim(key, len(rows), s.listHeight()) {
		return nil
	}
	switch key {
	case "j", "down":
		s.list.Move(1, len(rows), s.listHeight())
	case "k", "up":
		s.list.Move(-1, len(rows), s.listHeight())
	case "enter":
		if row, ok := s.selectedRow(); ok && row.header {
			s.toggleGroup(row.group)
		}
	case "space", " ":
		if row, ok := s.selectedRow(); ok {
			s.toggleGroup(row.group)
		}
	case "o":
		s.showSetupRefusal()
	case "c":
		if category, ok := s.captureSettingsCategory(); ok {
			return func() tea.Msg { return PolicyNavigation{Category: category} }
		}
	case "u":
		s.chooseBootstrap("reconcile-upstream")
	case "v":
		s.chooseBootstrap("adopt-machine")
	case "esc":
		switch {
		case s.bootstrap != nil && s.bootstrapChoice != nil:
			s.bootstrapChoice = nil
			s.current = s.base
			s.list.SetSelected(0, len(s.rows()), s.listHeight())
		case s.bootstrap != nil:
			s.message = reconcileSetupCancelled
		}
	case "s":
		if s.CanExecute() {
			s.confirming = true
		}
	case "r":
		return s.inspect(true)
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
	return nil
}

func (s *Reconcile) choose(choice string) {
	if s.bootstrap != nil {
		return
	}
	item, ok := s.selectedItem()
	if !s.ready || !ok {
		return
	}
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
		s.message = reconcileChoiceRefused
		return
	}
	s.choices, s.current = next, resolved
	// Follow the target into its new group so the decision stays visible.
	for _, moved := range s.current.Items {
		if moved.Key == key {
			s.expanded[reconcileGroup(moved)] = true
		}
	}
	s.selectRowKey(reconcileRowKey{target: key})
}

func (s *Reconcile) chooseBootstrap(choice workflow.SyncBootstrapChoice) {
	if !s.ready || s.bootstrap == nil {
		return
	}
	if s.setupRefusals[choice] != nil {
		// Preflight already knows this choice cannot proceed: show why.
		s.showSetupRefusal()
		return
	}
	plan, err := workflow.ResolveSyncBootstrapPlan(*s.bootstrap, choice)
	if err != nil {
		s.message = reconcileChoiceRefused
		return
	}
	s.current, s.bootstrapChoice = plan, &choice
	s.message = ""
	s.list.SetSelected(0, len(s.rows()), s.listHeight())
}

func (s *Reconcile) toggleGroup(group string) {
	s.expanded[group] = !s.groupExpanded(group)
	s.selectRowKey(reconcileRowKey{group: group})
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
	if s.bootstrap != nil && s.bootstrapChoice == nil {
		return false
	}
	for _, item := range s.current.Items {
		if item.Deferred || item.Review != syncengine.ReviewNone {
			return false
		}
	}
	return true
}
