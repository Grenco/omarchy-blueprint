# TUI Internal Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate the Bubble Tea root so root-owned events, screen adapters, transient flows, and root rendering each have one clear owner while preserving every shipped TUI behavior.

**Architecture:** Keep `model` as the Bubble Tea root coordinator inside `internal/tui`. Introduce one origin-aware root-event dispatcher, move root-facing screen adapters/construction into focused files, isolate modal and welcome state-machine behavior, and move root view composition out of `model.go` without changing `internal/workflow`, provider semantics, CLI behavior, schema, copy, keybindings, or layouts.

**Tech Stack:** Go 1.25+, Bubble Tea v2, Lip Gloss v2, existing `internal/tui`, `internal/tui/screens`, `internal/tui/components`, and `internal/workflow`; no new runtime or test dependencies.

**Spec:** `docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md`

**ADR:** `docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md`

## Global Constraints

- Behavior-preserving PR: do not change user-facing copy, sidebar order/grouping, keybindings, command-palette commands, help behavior, responsive breakpoints, notifications, or safety semantics.
- Do not change `internal/workflow` APIs or semantics, providers, profile schema, Config semantics, capture/restore semantics, machine overlays, Profile Git semantics, CLI behavior, or JSON behavior.
- Keep the consolidation inside `internal/tui`; do not create a new architectural package.
- Keep `internal/tui/screens` responsible for individual screen state machines and screen-specific rendering.
- Keep `internal/tui/components` responsible for reusable visual/input components.
- Preserve `screenMsg{Screen, Msg}` origin tagging for asynchronous screen commands.
- Root-owned event families that are valid both directly and wrapped must use the same canonical handler.
- Origin-required messages such as screen-requested modals must retain their requesting screen.
- If a wrapped message is not root-owned, deliver it to its originating screen even if another screen is active.
- Do not introduce reflection-driven dispatch, an event bus, a universal screen base class, a generic reducer framework, or a new dependency-injection layer.
- Do not fold onboarding/empty-state UX changes into this PR.
- Prefer behavior-characterization tests for pure code moves and red/green tests for new routing helpers or contracts.
- Every code-moving commit must leave `go test ./internal/tui/... -count=1` green.

---

## Target file structure

The final responsibility split should be close to:

```text
internal/tui/
├── actions.go
├── catalog.go
├── events.go               # root-owned message dispatch/effects
├── handoff.go
├── keys.go
├── layout.go
├── messages.go
├── modals.go               # modal stack, palette/help, modal rendering
├── model.go                # root state + high-level Init/Update/navigation
├── model_test.go
├── run.go
├── screen_adapters.go      # root-facing screen wrappers/actions/bindings
├── screen_adapters_test.go # focused adapter/key-ownership tests where useful
├── screen_registry.go      # fixed screen construction
├── theme.go
├── view.go                 # root header/footer/content/layout composition
├── viewport.go
├── welcome.go              # Create/Open/Quit transient workflow
├── components/
└── screens/
```

Exact test-file placement may follow existing package conventions. Do not create files solely to hit a line-count target.

---

### Task 1: Record the decision and lock the root-event contract

**Files:**
- Create: `docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md`
- Create: `docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md`
- Create: `docs/planning/plans/2026-09-17-tui-internal-consolidation-implementation-plan.md`
- Modify: `internal/tui/model_test.go`
- Create: `internal/tui/events.go`
- Modify: `internal/tui/model.go`

**Interfaces:**
- Consumes: existing `screenMsg{Screen ScreenID, Msg any}` from `internal/tui/messages.go`.
- Produces: `func (m model) handleRootMessage(origin ScreenID, msg tea.Msg) (model, tea.Cmd, bool)`.
- Contract: `bool=true` means the root consumed the message and it must not also be forwarded to a screen.

- [ ] **Step 1: Copy the approved design, ADR, and this plan into their repository paths**

Use the three documents supplied with the implementation handoff without rewriting their decisions during implementation.

Run:

```sh
test -f docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md
test -f docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md
test -f docs/planning/plans/2026-09-17-tui-internal-consolidation-implementation-plan.md
```

Expected: all three commands exit 0.

- [ ] **Step 2: Add a failing regression test proving a root-owned wrapped notice is not delivered to its screen**

Add a package-local test screen that records every message:

```go
type allMessageRecordingScreen struct {
	id       ScreenID
	messages []tea.Msg
}

func (s *allMessageRecordingScreen) ID() ScreenID     { return s.id }
func (s *allMessageRecordingScreen) SetSize(int, int) {}
func (s *allMessageRecordingScreen) Update(msg tea.Msg) tea.Cmd {
	s.messages = append(s.messages, msg)
	return nil
}
func (s *allMessageRecordingScreen) View() string      { return "recording" }
func (s *allMessageRecordingScreen) Actions() []Action { return nil }
```

Add:

```go
func TestRootOwnedWrappedNoticeIsHandledOnce(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	owner := &allMessageRecordingScreen{id: ScreenResources}
	m.screens[ScreenResources] = owner

	updated, _ := m.Update(screenMsg{
		Screen: ScreenResources,
		Msg:    screens.Notice{Message: "saved"},
	})
	m = updated.(model)

	if m.notification != "saved" {
		t.Fatalf("notification = %q, want saved", m.notification)
	}
	if len(owner.messages) != 0 {
		t.Fatalf("root-owned notice leaked back to screen: %#v", owner.messages)
	}
}
```

Why this test matters: the current wrapped `screens.Notice` path updates the root notification and then falls through to screen forwarding. The canonical dispatcher must consume it once.

- [ ] **Step 3: Run the new test and verify it fails on the current implementation**

Run:

```sh
go test ./internal/tui -run TestRootOwnedWrappedNoticeIsHandledOnce -count=1
```

Expected: FAIL because the wrapped root-owned notice is also passed to the origin screen.

- [ ] **Step 4: Add the explicit root dispatcher**

Create `internal/tui/events.go` with:

```go
package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

func (m model) handleRootMessage(origin ScreenID, msg tea.Msg) (model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case showHelpMsg:
		m.openModal(modalHelp)
		return m, nil, true
	case components.ModalRequest:
		if origin == "" {
			return m, nil, false
		}
		m.requestedModal = &ModalRequest{
			Title:       msg.Title,
			Content:     msg.Content,
			Input:       msg.Input,
			Placeholder: msg.Placeholder,
		}
		m.requestedModalScreen = origin
		m.openModal(modalConfirm)
		if msg.Input != "" || msg.Placeholder != "" {
			m.requestedModalInput = components.NewTextInputModal(msg.Input, msg.Placeholder)
			return m, m.requestedModalInput.Focus(), true
		}
		return m, nil, true
	case screens.OverviewTarget:
		updated, cmd := m.handleOverviewTarget(msg)
		return updated.(model), cmd, true
	case screens.CaptureComplete:
		updated, cmd := m.handleCaptureComplete(msg)
		return updated.(model), cmd, true
	case screens.SessionReloadNeeded:
		updated, cmd := m.reloadSessionScreens(msg.Reason)
		return updated.(model), cmd, true
	case screens.HandoffRequest:
		updated, cmd := m.handleHandoffRequest(msg)
		return updated.(model), cmd, true
	case handoffFinishedMsg:
		updated, cmd := m.handleHandoffFinished(msg)
		return updated.(model), cmd, true
	case screens.MachineMutationComplete:
		if msg.Err == nil {
			m.notification = msg.Notice
		} else {
			m.notification = "Machine update failed: " + msg.Err.Error()
		}
		return m, nil, true
	case screens.Notice:
		m.notification = msg.Message
		return m, nil, true
	}
	return m, nil, false
}
```

If the actual helper signatures are simplified to return `(model, tea.Cmd)` instead of `(tea.Model, tea.Cmd)`, keep them consistent across `events.go`; do not add conversion helpers just to preserve an old signature.

- [ ] **Step 5: Extract canonical Overview and handoff-finished helpers from existing root logic**

Move the existing Overview target behavior into one helper:

```go
func (m model) handleOverviewTarget(target screens.OverviewTarget) (tea.Model, tea.Cmd) {
	navigation := m.selectScreen(ScreenID(target.Target))
	switch current := m.activeScreen().(type) {
	case *configScreen:
		return m, tea.Batch(navigation, wrapScreenCmd(m.screenID(), current.Focus(target.Ref)))
	case *resourcesScreen:
		return m, tea.Batch(navigation, wrapScreenCmd(m.screenID(), current.Focus(target.Ref)))
	case *machinesScreen:
		current.Focus(target.Ref)
	}
	return m, navigation
}
```

Move the existing `handoffFinishedMsg` behavior into one helper:

```go
func (m model) handleHandoffFinished(finished handoffFinishedMsg) (tea.Model, tea.Cmd) {
	if finished.Kind == "lazygit" {
		return m.reloadSessionScreens("LazyGit")
	}
	m.notification = "External tool closed. Refreshing " + finished.Kind + "."
	if finished.Err != nil {
		m.notification = "External tool closed with an error. Refreshing " + finished.Kind + "."
	}
	if current := m.screens[finished.Refresh]; current != nil {
		return m, wrapScreenCmd(finished.Refresh, current.Update(tea.KeyPressMsg{Code: 'r'}))
	}
	return m, nil
}
```

Do not change notification text.

- [ ] **Step 6: Make wrapped-screen routing use the dispatcher once**

Replace the root-owned type switch in `updateScreenMsg` with:

```go
func (m model) updateScreenMsg(wrapped screenMsg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.handleRootMessage(wrapped.Screen, wrapped.Msg); handled {
		return next, cmd
	}
	if current := m.screens[wrapped.Screen]; current != nil {
		return m, wrapScreenCmd(wrapped.Screen, current.Update(wrapped.Msg))
	}
	return m, nil
}
```

This preserves origin for unhandled screen-local messages.

- [ ] **Step 7: Make direct root messages use the same dispatcher**

Near the start of `Update`, after `profileCreatedMsg` and `screenMsg` handling but before ordinary window/theme/key routing, add:

```go
if next, cmd, handled := m.handleRootMessage("", msg); handled {
	return next, cmd
}
```

Remove the duplicated direct `showHelpMsg`, `screens.OverviewTarget`, `screens.CaptureComplete`, `screens.Notice`, `screens.HandoffRequest`, and `handoffFinishedMsg` implementations from `Update`.

Do not route `components.ModalRequest` directly: `handleRootMessage` explicitly leaves it unhandled when `origin == ""`.

- [ ] **Step 8: Run routing tests**

Run:

```sh
go test ./internal/tui -run 'TestRootOwnedWrappedNoticeIsHandledOnce|TestScreenMessagesKeepTheirOwnerAfterNavigation|TestBatchedScreenCommandsKeepTheirOwner|TestModelReloadRefreshesInitializedScreensAfterPullAndLazyGit|TestCaptureCleanupWarningRefreshesCommittedState|TestModelIntegrationRouteUsesSharedSessionAcrossRefreshes' -count=1
```

Expected: PASS.

- [ ] **Step 9: Run the full TUI package**

Run:

```sh
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit the routing consolidation and architecture records**

```sh
git add docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md \
        docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md \
        docs/planning/plans/2026-09-17-tui-internal-consolidation-implementation-plan.md \
        internal/tui/events.go \
        internal/tui/model.go \
        internal/tui/model_test.go
git commit -m "refactor: centralize TUI root event routing"
```

---

### Task 2: Extract the fixed screen registry and root-facing adapters

**Files:**
- Create: `internal/tui/screen_registry.go`
- Create: `internal/tui/screen_adapters.go`
- Create or modify: `internal/tui/screen_adapters_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go` only where test helpers move naturally

**Interfaces:**
- Consumes: `ScreenID`, existing `screen` interface, `workflow.Session`, `screens.New*Context` constructors.
- Produces: `func newScreens(ctx context.Context, session *workflow.Session) map[ScreenID]screen`.
- Produces: the same adapter types and root-facing `Actions`, `Bindings`, and `HandleKey` behavior currently embedded in `model.go`.

- [ ] **Step 1: Add a failing constructor test for the new registry**

Create `internal/tui/screen_adapters_test.go` and add:

```go
func TestNewScreensBuildsPlaceholdersWithoutSession(t *testing.T) {
	got := newScreens(context.Background(), nil)
	for _, id := range screenOrder {
		screen, ok := got[id]
		if !ok {
			t.Fatalf("missing screen %s", id)
		}
		if _, ok := screen.(*placeholderScreen); !ok {
			t.Fatalf("%s = %T, want *placeholderScreen", id, screen)
		}
	}
}
```

Add:

```go
func TestNewScreensBuildsExpectedAdaptersWithSession(t *testing.T) {
	got := newScreens(context.Background(), integrationSession(t))

	checks := map[ScreenID]any{
		ScreenOverview:  (*overviewScreen)(nil),
		ScreenCapture:   (*captureScreen)(nil),
		ScreenConfig:    (*configScreen)(nil),
		ScreenResources: (*resourcesScreen)(nil),
		ScreenMachines:  (*machinesScreen)(nil),
		ScreenRestore:   (*restoreScreen)(nil),
		ScreenSync:      (*syncScreen)(nil),
	}
	for id, want := range checks {
		if fmt.Sprintf("%T", got[id]) != fmt.Sprintf("%T", want) {
			t.Fatalf("%s = %T, want %T", id, got[id], want)
		}
	}
	for _, id := range []ScreenID{
		ScreenPackages, ScreenThemes, ScreenPlugins,
		ScreenShell, ScreenHooks, ScreenDefaults,
	} {
		if got[id].ID() != id {
			t.Fatalf("%s adapter returned ID %s", id, got[id].ID())
		}
		if _, ok := got[id].(*providerScreen); !ok {
			t.Fatalf("%s = %T, want *providerScreen", id, got[id])
		}
	}
}
```

Use direct type assertions instead of the `fmt.Sprintf` comparison if that makes the test clearer; the requirement is to verify the fixed registry shape, not to preserve this exact test syntax.

- [ ] **Step 2: Run the registry tests and verify they fail**

Run:

```sh
go test ./internal/tui -run 'TestNewScreensBuilds' -count=1
```

Expected: FAIL because `newScreens` does not yet exist.

- [ ] **Step 3: Create `screen_registry.go`**

Implement:

```go
package tui

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func newScreens(ctx context.Context, session *workflow.Session) map[ScreenID]screen {
	screenMap := make(map[ScreenID]screen, len(screenOrder))
	for _, id := range screenOrder {
		screenMap[id] = &placeholderScreen{id: id}
	}
	if session == nil {
		return screenMap
	}

	screenMap[ScreenOverview] = &overviewScreen{Overview: screens.NewOverviewContext(ctx, session)}
	screenMap[ScreenCapture] = &captureScreen{Capture: screens.NewCaptureContext(ctx, session)}
	screenMap[ScreenConfig] = &configScreen{Config: screens.NewConfigContext(ctx, session)}
	screenMap[ScreenResources] = &resourcesScreen{Resources: screens.NewResourcesContext(ctx, session)}
	screenMap[ScreenMachines] = &machinesScreen{Machines: screens.NewMachinesContext(ctx, session)}
	screenMap[ScreenRestore] = &restoreScreen{Restore: screens.NewRestoreContext(ctx, session)}
	screenMap[ScreenSync] = &syncScreen{Sync: screens.NewSyncContext(ctx, session)}

	for _, id := range []ScreenID{
		ScreenPackages,
		ScreenThemes,
		ScreenPlugins,
		ScreenShell,
		ScreenHooks,
		ScreenDefaults,
	} {
		screenMap[id] = &providerScreen{
			Provider: screens.NewProviderContext(ctx, session, string(id)),
			id:       id,
		}
	}
	return screenMap
}
```

- [ ] **Step 4: Change `newModelWithContext` to use the registry**

Replace inline construction with:

```go
screenMap := newScreens(ctx, session)
```

Keep the existing style application loop immediately after construction.

- [ ] **Step 5: Move root-facing adapter declarations and methods into `screen_adapters.go`**

Move without changing behavior:

```text
screen interface and optional screen interfaces, if root-only
DetailView
placeholderScreen
configScreen
overviewScreen
captureScreen
resourcesScreen
machinesScreen
restoreScreen
syncScreen
providerScreen
all adapter ID methods
all adapter HandleKey methods
all adapter Actions methods
all adapter Bindings methods
bindingsFromResourceActions
bindingsFromSyncActions
screenKey, if it is only adapter/root key-policy support
```

Do not move implementation types from `internal/tui/screens`.

Do not rename `providerScreen` merely because user-facing copy says category; it is an internal adapter around the existing provider screen.

- [ ] **Step 6: Run adapter and key-ownership tests**

Run:

```sh
go test ./internal/tui -run 'TestNewScreensBuilds|TestScreenKeyOwnershipPrecedesRootFocusAndOverlays|TestKeyIsDeliveredToClaimingScreenOnce|TestRootKeyRoutingPrecedenceAndFallback|TestGroupNavigationBindingsFollowFocusAndStayOutOfFooter' -count=1
```

Expected: PASS.

- [ ] **Step 7: Run the full TUI package and check the move**

```sh
go test ./internal/tui/... -count=1
git diff --check
```

Expected: PASS / no whitespace errors.

- [ ] **Step 8: Commit**

```sh
git add internal/tui/model.go \
        internal/tui/screen_registry.go \
        internal/tui/screen_adapters.go \
        internal/tui/screen_adapters_test.go \
        internal/tui/model_test.go
git commit -m "refactor: isolate TUI screen adapters"
```

---

### Task 3: Isolate modal, palette, and help behavior

**Files:**
- Create: `internal/tui/modals.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: existing `modalKind`, `ModalRequest`, palette/help fields on `model`, `ActionRegistry`, `components.TextInputModal`.
- Produces: focused modal helpers while keeping the state on `model`.
- Preserve: `requestedModalScreen` as the authoritative requester for confirmation/input response routing.

- [ ] **Step 1: Add a failing helper test for modal footer ownership**

Add:

```go
func TestModalFooterTextPreservesCurrentModes(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})

	m.modal = modalPalette
	if got := m.modalFooter(); got != "type search   up/down select   enter run   esc close" {
		t.Fatalf("palette footer = %q", got)
	}

	m.modal = modalHelp
	m.requestedModal = nil
	if got := m.modalFooter(); got != "type search   up/down scroll   esc close" {
		t.Fatalf("help footer = %q", got)
	}

	m.requestedModal = &ModalRequest{Content: "confirm"}
	if got := m.modalFooter(); got != "enter confirm   esc cancel" {
		t.Fatalf("confirm footer = %q", got)
	}

	m.requestedModal = &ModalRequest{Input: "value"}
	if got := m.modalFooter(); got != "enter save   esc cancel" {
		t.Fatalf("input footer = %q", got)
	}
}
```

- [ ] **Step 2: Run and verify failure**

```sh
go test ./internal/tui -run TestModalFooterTextPreservesCurrentModes -count=1
```

Expected: FAIL because `modalFooter` does not exist.

- [ ] **Step 3: Create `modals.go` and move modal types/mechanics**

Move:

```text
ModalRequest
modalKind constants
openModal
closeModal
updatePalette
updateHelp
modalView
composeOverlay
helpLines
appendHelpEntries
```

Also add:

```go
func (m model) modalFooter() string {
	switch m.modal {
	case modalPalette:
		return "type search   up/down select   enter run   esc close"
	case modalHelp:
		if m.requestedModal == nil {
			return "type search   up/down scroll   esc close"
		}
		if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
			return "enter save   esc cancel"
		}
		return "enter confirm   esc cancel"
	case modalWelcome:
		return m.welcomeFooter()
	default:
		if m.requestedModal != nil {
			if m.requestedModal.Input != "" || m.requestedModal.Placeholder != "" {
				return "enter save   esc cancel"
			}
			return "enter confirm   esc cancel"
		}
		return "esc close"
	}
}
```

`welcomeFooter` is introduced in Task 4. Until Task 4 lands, either keep the existing `modalWelcome` footer branch in `modalFooter` or introduce `welcomeFooter` in the same commit as a small delegation with identical output. Do not leave the package uncompilable between commits.

- [ ] **Step 4: Extract requested-screen modal input/confirm handling from `Update`**

Add a helper such as:

```go
func (m model) updateRequestedModal(msg tea.Msg, key string, isKey bool) (model, tea.Cmd, bool) {
	if m.requestedModal == nil {
		return m, nil, false
	}
	if (m.requestedModal.Input != "" || m.requestedModal.Placeholder != "") && isKey {
		switch key {
		case "esc":
			screenID := m.requestedModalScreen
			m.closeModal()
			_, cmd := m.updateScreenMsg(screenMsg{
				Screen: screenID,
				Msg:    tea.KeyPressMsg{Code: tea.KeyEsc},
			})
			return m, cmd, true
		case "enter":
			screenID, value := m.requestedModalScreen, m.requestedModalInput.Value()
			m.closeModal()
			_, cmd := m.updateScreenMsg(screenMsg{
				Screen: screenID,
				Msg:    components.TextInputSubmitted{Value: value},
			})
			return m, cmd, true
		default:
			return m, m.requestedModalInput.Update(msg), true
		}
	}
	if isKey && (key == "enter" || key == "esc") {
		screenID := m.requestedModalScreen
		code := tea.KeyEsc
		if key == "enter" {
			code = tea.KeyEnter
		}
		m.closeModal()
		_, cmd := m.updateScreenMsg(screenMsg{
			Screen: screenID,
			Msg:    tea.KeyPressMsg{Code: code},
		})
		return m, cmd, true
	}
	return m, nil, false
}
```

Keep the current rule that the response goes to `requestedModalScreen`.

- [ ] **Step 5: Add/retain a regression test that navigation does not steal a requested-modal response**

Use a recording screen as requester, open a root `components.ModalRequest` from that screen, change the selected screen, then submit/cancel and assert the response is delivered to the requester.

Example structure:

```go
func TestRequestedModalResponseReturnsToRequestingScreenAfterNavigation(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})
	requester := &allMessageRecordingScreen{id: ScreenResources}
	m.screens[ScreenResources] = requester

	updated, _ := m.Update(screenMsg{
		Screen: ScreenResources,
		Msg: components.ModalRequest{
			Title:   "Confirm",
			Content: "Proceed?",
		},
	})
	m = updated.(model)
	m.selected = screenIndex(t, ScreenSync)

	m = updateModel(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(requester.messages) != 1 {
		t.Fatalf("requester messages = %#v", requester.messages)
	}
	key, ok := requester.messages[0].(tea.KeyPressMsg)
	if !ok || key.String() != "enter" {
		t.Fatalf("modal response = %#v, want enter key", requester.messages[0])
	}
}
```

- [ ] **Step 6: Replace the modal footer branch in `footer()`**

Use:

```go
if m.modal != modalNone {
	return m.modalFooter()
}
```

Do not change ordinary statusbar behavior.

- [ ] **Step 7: Run modal/palette/help tests**

```sh
go test ./internal/tui -run 'TestModalFooterTextPreservesCurrentModes|TestRequestedModalResponseReturnsToRequestingScreenAfterNavigation|TestModalLeavesBaseViewVisibleAndViewportScrolls|TestRootKeyRoutingPrecedenceAndFallback' -count=1
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```sh
git add internal/tui/modals.go internal/tui/model.go internal/tui/model_test.go
git commit -m "refactor: isolate TUI modal flows"
```

---

### Task 4: Isolate the welcome/profile chooser workflow

**Files:**
- Create: `internal/tui/welcome.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/modals.go`
- Modify: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: existing welcome fields on `model`, `workflow.Options`, `createProfile`, `openSession`.
- Produces: `welcomeFooter`, welcome rendering helper(s), `enableProfileChooser`, `updateWelcome`, `expandWelcomePath`.
- State fields may remain on `model`; do not require a nested `welcomeState` in this PR.

- [ ] **Step 1: Add a failing footer test for welcome modes**

Add:

```go
func TestWelcomeFooterPreservesChooserModes(t *testing.T) {
	m := newModel(ThemeLoader{NoColor: true})

	m.welcomeChooser = true
	m.welcomeStep = "choose"
	if got := m.welcomeFooter(); got != "up/down select   enter continue   esc quit" {
		t.Fatalf("chooser footer = %q", got)
	}

	m.welcomeStep = "open-path"
	if got := m.welcomeFooter(); got != "enter continue   esc back" {
		t.Fatalf("path footer = %q", got)
	}

	m.welcomeChooser = false
	if got := m.welcomeFooter(); got != "enter create   esc quit" {
		t.Fatalf("create footer = %q", got)
	}
}
```

- [ ] **Step 2: Run and verify failure**

```sh
go test ./internal/tui -run TestWelcomeFooterPreservesChooserModes -count=1
```

Expected: FAIL until the helper exists.

- [ ] **Step 3: Create `welcome.go` and move the current welcome behavior verbatim**

Move:

```text
profileCreatedMsg
enableProfileChooser
updateWelcome
expandWelcomePath
```

Add:

```go
func (m model) welcomeFooter() string {
	if m.welcomeChooser && m.welcomeStep == "choose" {
		return "up/down select   enter continue   esc quit"
	}
	if m.welcomeChooser {
		return "enter continue   esc back"
	}
	return "enter create   esc quit"
}
```

- [ ] **Step 4: Move welcome modal content construction behind a helper**

Add a helper returning the current content, for example:

```go
func (m model) welcomeContent() string {
	lines := []string{}
	if m.welcomeChooser && m.welcomeStep == "choose" {
		lines = append(lines, "No Blueprint profile was found.", "", "Choose what to do:")
		for i, choice := range []string{
			"Create a new profile",
			"Open an existing profile",
			"Quit",
		} {
			marker := "  "
			if i == m.welcomeChoice {
				marker = "> "
			}
			lines = append(lines, marker+choice)
		}
	} else if m.welcomeStep == "create-path" || m.welcomeStep == "open-path" {
		action := "Create profile at:"
		if m.welcomeStep == "open-path" {
			action = "Open profile at:"
		}
		lines = append(lines, action, "", m.welcomePath.View())
	} else {
		lines = append(lines,
			"Create a new profile at:",
			components.DisplayText(m.profileDir),
			"",
			"Profile name: "+m.welcomeName.View(),
		)
	}
	if m.welcomeBusy {
		lines = append(lines, "", "Working...")
	}
	if m.welcomeError != nil {
		lines = append(lines, "Error: "+components.DisplayText(m.welcomeError.Error()))
	}
	return strings.Join(lines, "\n")
}
```

Then the `modalWelcome` rendering branch becomes:

```go
case modalWelcome:
	title = "Welcome to Omarchy Blueprint"
	overlay = m.welcomeContent()
```

Do not change strings.

- [ ] **Step 5: Run existing onboarding tests plus the new helper test**

Find the existing welcome/chooser tests with:

```sh
go test ./internal/tui -list 'Test.*(Welcome|Profile|Chooser)'
```

Run those exact tests plus:

```sh
go test ./internal/tui -run TestWelcomeFooterPreservesChooserModes -count=1
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Review the diff specifically for accidental copy changes**

Run:

```sh
git diff --word-diff -- internal/tui/model.go internal/tui/modals.go internal/tui/welcome.go
```

Expected: welcome strings are moved, not rewritten.

- [ ] **Step 7: Commit**

```sh
git add internal/tui/welcome.go internal/tui/modals.go internal/tui/model.go internal/tui/model_test.go
git commit -m "refactor: isolate TUI welcome workflow"
```

---

### Task 5: Move root view composition out of the update coordinator

**Files:**
- Create: `internal/tui/view.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/modals.go` only if root-specific view helpers settle there
- Modify: `internal/tui/model_test.go` only for focused rendering regression coverage

**Interfaces:**
- Consumes: existing `layoutForSize`, `components`, palette, screen catalog, viewport state.
- Produces: same `func (m model) View() tea.View` and root view helper behavior, located in `view.go`.

- [ ] **Step 1: Establish a green rendering baseline before the mechanical move**

Run:

```sh
go test ./internal/tui -run 'TestViewRendersMultiPaneLayouts|TestPaneRowsAreBoundedToTerminalWidth|TestWorkspaceShowsActiveScreenDescriptionAndAccountsForItsHeight|TestNoColorViewsKeepSemanticMarkers|TestRootNotificationSanitizesControls|TestRootHeaderSanitizesProfileName|TestResizeSafety' -count=1
```

Expected: PASS.

This task is primarily extraction; these characterization tests are the red/green guardrail. Do not manufacture a behavior change solely to create a failing test.

- [ ] **Step 2: Create `view.go` and move ordinary root rendering**

Move without changing implementation:

```text
View
footer
header
accent
warning
muted
color
contentView
workspaceContent
screenSize
setScreenSizes
sidebarRender
ensureSidebarSelection
detailLines
boundedBox
boundedLine
```

Keep `modalView`, modal overlay composition, and help rendering in `modals.go`.

If `boundedLine` is used by both ordinary root rendering and modal rendering, keep it in `view.go` as a package-local rendering primitive rather than duplicating it.

- [ ] **Step 3: Keep model initialization/navigation calling the same view helpers**

No call-site behavior should change. In particular:

- `WindowSizeMsg` still calls `setScreenSizes`;
- navigation still calls `ensureSidebarSelection`;
- details scrolling still uses `detailLines`;
- notifications still append to the footer in the same location;
- `View` still uses `modalView` when `m.modal != modalNone`.

- [ ] **Step 4: Run rendering tests**

```sh
go test ./internal/tui -run 'TestViewRendersMultiPaneLayouts|TestPaneRowsAreBoundedToTerminalWidth|TestWorkspaceShowsActiveScreenDescriptionAndAccountsForItsHeight|TestModalLeavesBaseViewVisibleAndViewportScrolls|TestNoColorViewsKeepSemanticMarkers|TestRootNotificationSanitizesControls|TestRootHeaderSanitizesProfileName|TestResizeSafety' -count=1
```

Expected: PASS.

- [ ] **Step 5: Run all TUI tests and inspect the diff**

```sh
go test ./internal/tui/... -count=1
git diff --check
git diff --stat
```

Expected: PASS, no whitespace errors. Most changes should be line movement from `model.go` to `view.go`.

- [ ] **Step 6: Commit**

```sh
git add internal/tui/model.go internal/tui/view.go internal/tui/modals.go internal/tui/model_test.go
git commit -m "refactor: separate TUI root rendering"
```

---

### Task 6: Simplify the high-level root update flow and remove residual duplication

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/events.go`
- Modify: `internal/tui/modals.go`
- Modify: `internal/tui/welcome.go`
- Modify tests only if a newly discovered ownership seam lacks coverage

**Interfaces:**
- Consumes: the extracted dispatcher, screen adapters/registry, modal helpers, welcome helpers, view helpers.
- Produces: a readable `Update` whose branches express precedence rather than subsystem implementations.

- [ ] **Step 1: Read `model.go` top-to-bottom and classify every remaining function**

Every remaining function should belong to one of these root coordinator concerns:

```text
root state/constructors
Init
Update
active screen lookup
root key precedence/navigation
screen initialization
focus movement
screen command wrapping
cancellation
root action registry composition
```

If adapter, modal, welcome, or rendering implementation remains in `model.go`, move it to its owner before continuing.

- [ ] **Step 2: Remove stale compatibility state only when tests prove it is unused**

The fields:

```go
paletteOpen, helpOpen bool
```

are documented as package-local compatibility mirrors of canonical modal state.

Search:

```sh
git grep -n 'paletteOpen\|helpOpen' -- internal/tui
```

If tests or package code still read them, keep them synchronized exactly as today. If no behavior depends on them, remove them and update only tests that directly inspect the compatibility fields.

Do not remove state merely to make `model` smaller.

- [ ] **Step 3: Make `Update` read as ordered ownership**

The final structure should remain semantically equivalent to:

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if created, ok := msg.(profileCreatedMsg); ok {
		return m.handleProfileCreated(created)
	}
	if wrapped, ok := msg.(screenMsg); ok {
		return m.updateScreenMsg(wrapped)
	}
	if next, cmd, handled := m.handleRootMessage("", msg); handled {
		return next, cmd
	}

	// window/theme lifecycle

	key, isKey := keyName(msg)
	if isKey && key == "ctrl+c" {
		m.stop()
		return m, tea.Quit
	}

	// root transient priority: welcome, palette, help, confirmation/input

	// active screen transient/workspace ownership

	// root quit, palette/help open, focus, section/sidebar/details navigation

	if !isKey {
		return m, m.updateActiveScreen(msg)
	}
	return m, nil
}
```

Do not force this exact formatting if a nearby helper makes the precedence clearer.

- [ ] **Step 4: Add a regression test if any previously duplicated root effect is discovered**

If another root-owned message appears in both direct and wrapped paths, first add a test showing the duplicate/unequal behavior, then route it through `handleRootMessage`.

Do not opportunistically move screen-local messages into the root dispatcher.

- [ ] **Step 5: Run routing, key, modal, welcome, and rendering suites together**

```sh
go test ./internal/tui -run 'Test(ScreenMessagesKeepTheirOwnerAfterNavigation|BatchedScreenCommandsKeepTheirOwner|RootOwnedWrappedNoticeIsHandledOnce|ScreenKeyOwnershipPrecedesRootFocusAndOverlays|KeyIsDeliveredToClaimingScreenOnce|QuitDefersToTransientInput|RootKeyRoutingPrecedenceAndFallback|Modal|RequestedModal|Welcome|Navigation|ResizeSafety)' -count=1
```

If the regex does not match some current test names, use `go test ./internal/tui -list Test` to select the equivalent tests rather than renaming unrelated tests.

Then run:

```sh
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Measure the resulting responsibility split**

Run:

```sh
wc -l internal/tui/model.go \
      internal/tui/events.go \
      internal/tui/screen_adapters.go \
      internal/tui/screen_registry.go \
      internal/tui/modals.go \
      internal/tui/welcome.go \
      internal/tui/view.go
```

There is no hard line-count target. Review whether `model.go` now reads primarily as root state plus high-level orchestration. If it is still large because a coherent root navigation/action section remains, keep it rather than creating arbitrary files.

- [ ] **Step 7: Commit cleanup**

```sh
git add internal/tui/model.go \
        internal/tui/events.go \
        internal/tui/modals.go \
        internal/tui/welcome.go \
        internal/tui/view.go \
        internal/tui/model_test.go
git commit -m "refactor: simplify TUI root coordination"
```

---

### Task 7: Full verification and behavior-preservation review

**Files:**
- No new production files expected.
- Modify only tests/docs if verification exposes a real consolidation omission.

**Interfaces:**
- Consumes: completed Tasks 1–6.
- Produces: merge-ready PR #27 with evidence that the refactor did not change product behavior.

- [ ] **Step 1: Run the TUI package uncached**

```sh
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 2: Run all repository tests uncached**

```sh
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run static analysis**

```sh
go vet ./...
```

Expected: exit 0.

- [ ] **Step 4: Build the CLI/TUI binary**

```sh
go build ./cmd/omarchy-blueprint
```

Expected: exit 0.

- [ ] **Step 5: Check whitespace**

```sh
git diff --check
```

Expected: no output.

- [ ] **Step 6: Confirm no out-of-scope domain/public files changed**

Run:

```sh
git diff --name-only origin/main...HEAD
```

Expected code changes should be concentrated under:

```text
internal/tui/
docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md
docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md
docs/planning/plans/2026-09-17-tui-internal-consolidation-implementation-plan.md
```

If `internal/workflow`, `internal/providers`, profile/schema files, or `internal/app` changed, explain why and remove the change unless it is strictly necessary to preserve compilation. The design expects none of them to change.

- [ ] **Step 7: Search for accidental user-facing copy changes**

Review:

```sh
git diff origin/main...HEAD -- internal/tui | grep '^[+-][^+-]' || true
```

Inspect changed string literals. Confirm the PR did not intentionally change:

```text
screen labels/descriptions
action labels
help labels
notifications
welcome copy
modal copy
footer hints
```

Mechanical relocation is fine; rewritten copy is out of scope.

- [ ] **Step 8: Review the event-routing invariants manually**

Confirm in code and tests:

```text
screen command keeps ScreenID origin
root-owned event handled once
unhandled wrapped event returns to origin screen
follow-up screen command is rewrapped with the same origin
screen-requested modal response returns to requester
direct and wrapped root events share canonical handlers where both are valid
```

- [ ] **Step 9: Review key-precedence invariants manually**

Confirm:

```text
ctrl-c still preempts everything
root modal/welcome owns input before workspace
active screen transient owns its input
workspace screen gets first refusal for screen-owned keys
root navigation only handles unconsumed keys
sidebar/details focus still owns root navigation
```

- [ ] **Step 10: Review the final diff as a refactor**

Run:

```sh
git diff --stat origin/main...HEAD
git diff origin/main...HEAD -- internal/tui/model.go internal/tui/events.go
```

The important question is not whether `model.go` is small. Verify:

> If a root event, screen adapter, modal, welcome flow, or root view changes later, is there now one obvious file/owner to edit?

- [ ] **Step 11: Commit any verification-only corrections**

Only if Step 1–10 exposed a real omission:

```sh
git add <exact corrected files>
git commit -m "test: preserve TUI consolidation behavior"
```

Do not create an empty final commit.

---

## PR guidance

Suggested title:

```text
refactor: consolidate TUI root ownership
```

Suggested body:

```markdown
## Summary
- centralize root-owned Bubble Tea event handling while preserving screen-origin routing for async results
- move screen adapters/construction, modal/help/palette flows, welcome flow, and root rendering into focused internal TUI files
- keep all shipped TUI behavior, workflow/domain semantics, CLI behavior, and profile formats unchanged

## Architecture
- adds ADR 0018 for TUI root ownership and origin-preserving event routing
- keeps `model` as the Bubble Tea root coordinator
- does not change `internal/workflow`, providers, schema, or public command semantics

## Verification
- `go test ./internal/tui/... -count=1`
- `go test ./... -count=1`
- `go vet ./...`
- `go build ./cmd/omarchy-blueprint`
- `git diff --check`
```

## Reviewer checklist

Reviewers should specifically look for:

- a root-owned event still implemented in more than one place;
- wrapped messages losing their originating `ScreenID`;
- screen-local responses being delivered to the currently selected screen;
- modal confirmation/input returning to the active screen instead of its requester;
- `q`, `?`, `:`, Tab, pane, or group-navigation precedence changing;
- Capture completion refreshing fewer/more surfaces than before;
- LazyGit or other handoff return behavior changing;
- user-facing copy or action labels changing inside a mechanical move;
- `internal/workflow` or provider changes sneaking into the refactor.

No onboarding/empty-state improvements belong in this PR. Those are the next product-facing TUI pass after consolidation merges.
