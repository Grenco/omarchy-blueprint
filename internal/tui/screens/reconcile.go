package screens

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type reconcileWorkflow interface {
	InspectSync(context.Context, workflow.SyncInspectOptions) (syncengine.Plan, error)
	ExecuteSync(context.Context, workflow.SyncExecutionRequest) (workflow.SyncExecutionResult, error)
}

// syncReviewCoordinator is implemented by sessions that coordinate with
// Auto Sync: an open review holds a lease so scheduled checks skip, and the
// review re-reads the tracking revision before confirmation.
type syncReviewCoordinator interface {
	HoldSyncReview() error
	HeartbeatSyncReview() error
	ReleaseSyncReview() error
	SyncTrackingRevision(context.Context) (string, error)
}

// reconcileLeaseHeartbeat is how often an open review renews its lease.
var reconcileLeaseHeartbeat = 30 * time.Second

type reconcileLeaseTickMsg struct{}

type reconcileTrackingMsg struct {
	id       uint64
	revision string
	err      error
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
	// stale: the shared profile moved while this review was open; it must
	// be refreshed before confirmation. verifying: that is being checked.
	stale, verifying bool
	// leaseHeld/leaseTicking track the review lease and its heartbeat.
	leaseHeld, leaseTicking bool
}

type reconcileInspectedMsg struct {
	bootstrap *workflow.SyncBootstrapPlan
	id        uint64
	plan      syncengine.Plan
	err       error
	// carry: a stale-review refresh, which keeps decisions for settings
	// that did not change.
	carry bool
}

func NewReconcileContext(ctx context.Context, session reconcileWorkflow) *Reconcile {
	return &Reconcile{ctx: ctx, session: session, choices: map[syncengine.TargetKey]syncengine.TargetResolution{}, expanded: map[string]bool{}}
}
func (s *Reconcile) SetSize(w, h int)                   { s.width, s.height = w, h }
func (s *Reconcile) SetStyles(styles components.Styles) { s.styles = styles }
func (s *Reconcile) TransientActive() bool              { return s.confirming || s.busy }
func (s *Reconcile) Init() tea.Cmd                      { return s.inspect(true) }

// Unrelated workspace changes may refresh cached evidence, never fetch.
func (s *Reconcile) RefreshCached() tea.Cmd     { return s.inspect(false) }
func (s *Reconcile) inspect(fetch bool) tea.Cmd { return s.inspectWith(fetch, false) }

func (s *Reconcile) inspectWith(fetch, carry bool) tea.Cmd {
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
	s.checking, s.ready, s.confirming, s.verifying = true, false, false, false
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
		return reconcileInspectedMsg{id: id, plan: p, bootstrap: bootstrap, err: err, carry: carry}
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
		priorBase, priorChoices := s.base, s.choices
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
		// A new inspection starts a new review. Only a stale-review refresh
		// keeps decisions, and only for settings whose every input is
		// unchanged; execution still revalidates everything.
		s.choices = map[syncengine.TargetKey]syncengine.TargetResolution{}
		if m.carry && m.bootstrap == nil {
			s.carryForward(priorBase, priorChoices)
		}
		if s.stale {
			s.stale = false
			if s.message == reconcileStaleReview {
				s.message = ""
			}
		}
		s.selectAfterInspection(previous, hadPrevious)
		return s.updateLease()
	case reconcileLeaseTickMsg:
		coordinator, ok := s.session.(syncReviewCoordinator)
		if !ok || !s.leaseHeld {
			s.leaseTicking = false
			return nil
		}
		_ = coordinator.HeartbeatSyncReview()
		return reconcileLeaseTick()
	case reconcileTrackingMsg:
		if m.id != s.requestID || !s.verifying {
			return nil
		}
		s.verifying = false
		if m.err == nil && m.revision != s.base.Identity.Remote {
			s.stale = true
			s.message = reconcileStaleReview
			return nil
		}
		if s.CanExecute() {
			s.confirming = true
		}
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
			return s.confirmWhenCurrent()
		}
	case "r":
		return s.inspectWith(true, s.stale)
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
	if !s.ready || s.checking || s.busy || s.stale || s.verifying || s.current.BootstrapRequired || len(s.current.Blockers) > 0 || s.current.Identity.Local == "" {
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

// confirmWhenCurrent re-reads the tracking revision before confirmation, so
// a shared profile that moved during the review (a manual fetch, another
// tool, a check after a lapsed lease) is offered as a Refresh rather than
// failing at execution.
func (s *Reconcile) confirmWhenCurrent() tea.Cmd {
	coordinator, ok := s.session.(syncReviewCoordinator)
	if !ok {
		s.confirming = true
		return nil
	}
	s.verifying = true
	id, ctx := s.requestID, s.ctx
	return func() tea.Msg {
		revision, err := coordinator.SyncTrackingRevision(ctx)
		return reconcileTrackingMsg{id: id, revision: revision, err: err}
	}
}

// carryForward keeps prior decisions whose setting is unchanged in every
// input; the rest are dropped. If the kept set no longer resolves, none are
// kept.
func (s *Reconcile) carryForward(prior syncengine.Plan, choices map[syncengine.TargetKey]syncengine.TargetResolution) {
	before := map[syncengine.TargetKey]syncengine.Item{}
	for _, item := range prior.Items {
		before[item.Key] = item
	}
	kept := map[syncengine.TargetKey]syncengine.TargetResolution{}
	for _, item := range s.base.Items {
		if choice, ok := choices[item.Key]; ok && reflect.DeepEqual(before[item.Key], item) {
			kept[item.Key] = choice
		}
	}
	if len(kept) == 0 {
		return
	}
	resolved, err := workflow.ResolveSyncPlan(s.base, reconcileChoices(kept))
	if err != nil {
		return
	}
	s.choices, s.current = kept, resolved
}

// reviewOpen: this review has something a person is deciding or about to
// run, so scheduled checks should leave the shared profile alone.
func (s *Reconcile) reviewOpen() bool {
	if !s.ready {
		return false
	}
	if s.bootstrap != nil || len(s.base.Blockers) > 0 {
		return true
	}
	for _, item := range s.base.Items {
		if len(item.Actions) > 0 || item.Review != syncengine.ReviewNone || item.Deferred {
			return true
		}
	}
	return false
}

// updateLease holds the review lease while a review is open and releases it
// otherwise, starting the heartbeat when needed.
func (s *Reconcile) updateLease() tea.Cmd {
	coordinator, ok := s.session.(syncReviewCoordinator)
	if !ok {
		return nil
	}
	if !s.reviewOpen() {
		if s.leaseHeld {
			_ = coordinator.ReleaseSyncReview()
			s.leaseHeld = false
		}
		return nil
	}
	if !s.leaseHeld {
		if coordinator.HoldSyncReview() != nil {
			return nil
		}
		s.leaseHeld = true
	}
	if s.leaseTicking {
		return nil
	}
	s.leaseTicking = true
	return reconcileLeaseTick()
}

func reconcileLeaseTick() tea.Cmd {
	return tea.Tick(reconcileLeaseHeartbeat, func(time.Time) tea.Msg { return reconcileLeaseTickMsg{} })
}

var _ syncReviewCoordinator = (*workflow.Session)(nil)
