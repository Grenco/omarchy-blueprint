# TUI Internal Consolidation Design

**Status:** Proposed design  
**Date:** 2026-09-17  
**Target PR:** #27 — TUI internal consolidation  
**Expected repository path:** `docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md`  
**Baseline:** `main` after PR #26 (`6c1450b00c10f52fef8b6424a8737cacaa4d85c1`)

## 1. Purpose

PR #27 is a behavior-preserving internal consolidation of the Omarchy Blueprint TUI.

The TUI comprehension work deliberately deferred three follow-ups:

1. user-facing README/docs restructuring;
2. internal TUI file decomposition and duplication cleanup;
3. onboarding/empty-state polish discovered through use.

The documentation pass is now complete. This design covers the second step only.

The goal is not to redesign the TUI. The goal is to make the existing TUI easier to understand, test, and extend before onboarding polish and later migration UI add more behavior.

The success criterion is:

> One obvious owner for each root-level TUI responsibility, with no user-visible behavior change.

## 2. Current problem

`internal/tui/model.go` is currently the main coordination point for the Bubble Tea application, but it also owns several unrelated responsibilities.

At the current `main` baseline it is roughly 58 KB and contains, among other things:

- root model state;
- screen interfaces and screen IDs;
- screen construction;
- screen adapter types;
- action and binding adaptation;
- wrapped screen-command routing;
- root-owned event handling;
- global key/focus/navigation handling;
- profile welcome/create/open flow;
- modal stack handling;
- command palette handling;
- help handling;
- capture-completion refresh orchestration;
- session reload orchestration;
- external-tool handoff orchestration;
- root rendering;
- header/footer rendering;
- modal rendering;
- details/sidebar rendering helpers.

File size alone is not the problem. The problem is overlapping ownership.

### 2.1 Root events have duplicate paths

Some messages can currently be handled in both the wrapped-screen path and the top-level `Update` path.

Examples include:

- `screens.OverviewTarget`;
- `screens.CaptureComplete`;
- `screens.Notice`;
- `screens.HandoffRequest`;
- `handoffFinishedMsg`.

The wrapped path needs to preserve the originating screen so a message can be routed back correctly, but root-owned effects should not have a second implementation when the same message reaches the root directly.

The result is duplicated navigation, refresh, notification, and external-handoff logic that can drift over time.

### 2.2 Screen adapters are mixed into root orchestration

Root-facing wrappers for Config, Overview, Capture, Resources, Machines, Restore, Sync, and generic provider screens currently live in `model.go`.

These adapters perform valuable work:

- provide the root-facing `ScreenID`;
- decide whether a key belongs to the screen or the root;
- expose command-palette actions;
- expose help/footer bindings;
- adapt screen-local action state into root `Action` objects.

That is a coherent responsibility, but it is not root-model orchestration.

### 2.3 Modal and welcome flows are embedded inside the main update loop

Palette, help, screen-requested confirmations/text input, and profile create/open onboarding all have their own state transitions.

They currently occupy substantial portions of `Update`, `View`, and footer rendering. This makes it harder to reason about ordinary workspace input because transient-state handling and normal root navigation are interleaved.

### 2.4 Rendering concerns are mixed with state transition concerns

`model.go` also owns composition of:

- header;
- footer;
- responsive content panes;
- details viewport;
- sidebar viewport;
- overlay composition;
- modal content;
- help content.

These are root-level concerns and should remain in `internal/tui`, but they do not need to share a file with root event dispatch and key routing.

## 3. Design constraints

This PR is intentionally conservative.

### 3.1 No user-visible behavior changes

Preserve exactly:

- sidebar order and grouping;
- all screen descriptions;
- all existing keybindings;
- command-palette actions and search behavior;
- help behavior and search behavior;
- focus behavior;
- responsive layouts and breakpoints;
- Config group navigation;
- Config filter ownership;
- Overview deep-link behavior;
- Capture confirmation and refresh behavior;
- Restore Normal/Forced behavior;
- Profile Git Sync behavior;
- profile Create/Open/Quit behavior;
- notifications;
- external editor/file-manager/browser/LazyGit handoff;
- theme reload and `NO_COLOR`;
- CLI/TUI entrypoint behavior.

If a refactor reveals an existing behavioral bug, fix it in a separate follow-up unless the bug prevents the consolidation from preserving current behavior.

### 3.2 No domain or workflow redesign

Do not change:

- `internal/workflow` APIs or semantics;
- provider behavior;
- profile schema;
- Config classification/policy semantics;
- capture semantics;
- restore semantics;
- CLI or JSON behavior;
- machine overlay semantics;
- Profile Git semantics.

This is a presentation-layer ownership cleanup.

### 3.3 No package proliferation

Keep the consolidation inside `internal/tui`.

Do not create a new architectural package merely to make `model.go` smaller.

`internal/tui/screens` remains responsible for individual screen state machines and screen-specific rendering. `internal/tui/components` remains responsible for reusable visual/input components.

### 3.4 Refactor toward ownership, not abstraction count

Avoid generic frameworks such as:

- a universal screen base class;
- reflection-driven action registration;
- a generic reducer abstraction;
- a new dependency-injection layer;
- a second event bus;
- interfaces introduced only to reduce line count.

Prefer small package-local helpers and files with concrete responsibilities.

## 4. Target architecture

Keep `model` as the Bubble Tea root coordinator.

After consolidation, the root package should read conceptually as:

```text
Bubble Tea root
    |
    +-- model/update orchestration
    |
    +-- root event dispatch
    |     navigation
    |     capture completion
    |     reload requests
    |     handoff requests/results
    |     notices
    |
    +-- screen registry/adapters
    |     screen construction
    |     key ownership
    |     root actions
    |     root bindings
    |
    +-- transient UI
    |     modal stack
    |     palette
    |     help
    |     welcome/profile chooser
    |
    +-- root view composition
          header/footer
          responsive panes
          details/sidebar viewports
          overlays
```

Individual screens continue to live in:

```text
internal/tui/screens/
```

Shared visual/input primitives continue to live in:

```text
internal/tui/components/
```

## 5. File responsibilities

Exact filenames may vary slightly during implementation, but ownership should follow this shape.

### 5.1 `model.go`

`model.go` remains the application's root state and high-level Bubble Tea coordinator.

It should own:

- `model` state;
- `focusArea`;
- core constructors;
- `Init`;
- the high-level `Update` flow;
- root navigation/focus primitives such as `moveScreen`, `selectScreen`, `cycleFocus`;
- active-screen lookup;
- cancellation/stop behavior.

The important outcome is that `Update` becomes readable as an ordered set of root decisions rather than containing the full implementations of every subsystem.

Conceptually:

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // profile/session lifecycle messages
    // wrapped screen message
    // root-owned direct message
    // window/theme messages
    // active modal/transient
    // screen key ownership
    // global navigation
    // non-key forwarding
}
```

The exact code does not need to match this pseudocode, but the control flow should be understandable without scrolling through adapter definitions or rendering implementations.

### 5.2 `screen_adapters.go`

Move the root-facing screen layer out of `model.go`.

This file should own:

- `screen` and optional root-facing screen interfaces if they are only used by the root;
- `placeholderScreen`;
- `configScreen`;
- `overviewScreen`;
- `captureScreen`;
- `resourcesScreen`;
- `machinesScreen`;
- `restoreScreen`;
- `syncScreen`;
- `providerScreen`;
- each adapter's `ID`;
- each adapter's `HandleKey`;
- each adapter's `Actions`;
- each adapter's `Bindings`;
- adapter helpers such as resource/sync action-to-binding conversion and root/screen key ownership helpers where appropriate.

These are adapters because they translate a screen implementation into the root TUI contract. They should not acquire screen business logic.

### 5.3 `screen_registry.go`

Centralize construction of the root's screen map.

It should own something equivalent to:

```go
func newScreens(ctx context.Context, session *workflow.Session) map[ScreenID]screen
```

Responsibilities:

- install placeholders when no session exists;
- create each concrete screen from the shared workflow session;
- create generic provider screens for Packages, Themes, Plugins, Shell, Hooks, and Defaults;
- apply initial root-facing wrapping only.

Styling may remain a separate root pass because the active palette belongs to the root.

The registry is not a plugin system. It is simply the single construction point for the fixed set of shipped screens.

### 5.4 `events.go`

Introduce one root-owned event dispatch path.

The root needs to distinguish:

1. messages the root owns;
2. messages that should be delivered to a screen.

Use one helper with explicit handled/unhandled semantics, for example:

```go
func (m model) handleRootMessage(origin ScreenID, msg tea.Msg) (model, tea.Cmd, bool)
```

The exact signature may differ, but it must make these rules clear:

- root-owned messages are handled once;
- a wrapped screen message retains its `origin`;
- if the root does not own a wrapped message, it is forwarded back to that originating screen;
- root-owned message families that are valid both directly and wrapped use the same handlers in both forms;
- messages whose semantics require an originating screen, such as a screen-requested modal that must return a response to its requester, remain origin-required and must not invent a direct form;
- direct messages that are not root-owned continue through normal `Update` behavior rather than being accidentally swallowed.

Root-owned message families should include the existing behavior for:

- `showHelpMsg`;
- `components.ModalRequest`;
- `screens.OverviewTarget`;
- `screens.CaptureComplete`;
- `screens.SessionReloadNeeded`;
- `screens.HandoffRequest`;
- `handoffFinishedMsg`;
- `screens.MachineMutationComplete`;
- `screens.Notice`;
- any other currently duplicated root-effect message discovered during implementation.

Do not create a map/reflection dispatcher. A straightforward type switch is easier to audit.

### 5.5 Root event helpers

Keep focused helpers for effects that already exist, such as:

- `handleOverviewTarget`;
- `handleCaptureComplete`;
- `reloadSessionScreens`;
- `handleHandoffRequest`;
- `handleHandoffFinished`.

A helper may mutate notification state and return commands, but it should have one owner and one call path.

In particular, remove the inline handoff-request implementation from the main `Update` loop and use the existing handoff helper as the canonical behavior.

### 5.6 `modals.go`

Move root modal mechanics and the palette/help transient state out of `model.go`.

Own:

- `modalKind`;
- `ModalRequest`;
- `openModal`;
- `closeModal`;
- requested screen modal input/submit/cancel routing;
- `updatePalette`;
- `updateHelp`;
- modal footer text;
- modal rendering;
- overlay composition;
- help-line rendering if it remains tightly coupled to the help modal.

The modal code must preserve the current stack behavior and screen ownership of confirmation responses.

The root still decides *when* modal handling takes priority. `modals.go` owns *how* the active modal behaves.

### 5.7 `welcome.go`

Treat profile Create/Open/Quit as a distinct transient workflow.

Own:

- `profileCreatedMsg`;
- welcome/profile-chooser fields' supporting logic;
- `enableProfileChooser`;
- `updateWelcome`;
- `expandWelcomePath`;
- welcome modal content;
- welcome footer text.

The state may remain fields on `model` in this PR to avoid a risky state-structure rewrite. Moving them into a nested `welcomeState` is optional only if it clearly simplifies the code without changing behavior.

Do not redesign onboarding copy or empty states here. That belongs to the following onboarding PR.

### 5.8 `view.go`

Move ordinary root view composition out of `model.go`.

Own:

- `View`;
- `header`;
- ordinary footer/statusbar composition;
- `contentView`;
- `workspaceContent`;
- screen sizing;
- sidebar rendering/viewport positioning;
- detail rendering/viewport positioning;
- bounded line/box helpers if root-specific.

`layout.go`, `viewport.go`, `catalog.go`, `actions.go`, `handoff.go`, `theme.go`, and existing component files should continue to own their present focused responsibilities unless a tiny move is necessary to remove circular ownership.

## 6. Event-routing contract

This is the most important behavioral part of the consolidation.

### 6.1 Screen commands remain origin-tagged

Commands returned by a screen continue to be wrapped with the originating `ScreenID`.

That origin is necessary because asynchronous work may complete after the user navigates elsewhere.

A message originating from Config must not accidentally be delivered to whichever screen happens to be active when it completes.

### 6.2 Root-owned events are intercepted once

For an origin-tagged message:

```text
screen command completes
        ↓
screenMsg{origin, msg}
        ↓
root event dispatcher
        ├── root owns msg → handle once
        └── root does not own msg → deliver to origin screen
```

For a direct message:

```text
direct msg
    ↓
same root event dispatcher
        ├── root owns msg → handle same behavior
        └── unhandled → continue normal root Update flow
```

This removes the need to duplicate event behavior in `updateScreenMsg` and `Update`.

### 6.3 Do not flatten all messages into root events

Screen-local async responses still belong to their screen.

The consolidation must not teach the root about every screen-specific message type.

The root dispatcher handles only events whose effect crosses root boundaries, such as navigation, global notifications, session reload, modal ownership, or external application handoff.

## 7. Key ownership contract

The current input model is deliberate and must survive the refactor.

Rules:

- active screen transients own their input before root shortcuts;
- workspace screens may consume a key before root fallback;
- root-owned keys remain available when the screen does not consume them;
- sidebar/detail focus retains root navigation ownership;
- modal input has priority over ordinary workspace input;
- Config filtering continues to own printable/reserved keys while filtering;
- palette/help text entry continues to treat searchable characters as text;
- `q`, `?`, `:`, `Tab`, pane navigation, and group navigation preserve current behavior.

Moving adapter code must not replace these nuanced ownership decisions with a blanket "screen gets all keys" or "root gets all global keys" rule.

## 8. Screen construction and styling

Screen construction should become a single root registry operation.

Construction remains dependent on the shared `workflow.Session`; no screen may begin constructing its own domain services.

Theme application remains root-owned because the root:

- loads the active palette;
- watches for palette changes;
- reapplies `components.Styles` to styleable screens.

A screen registry should not own theme reload behavior.

## 9. Rendering boundaries

This PR may reorganize rendering code but must not change rendered semantics.

Preserve:

- terminal-too-small behavior;
- wide/two-pane/compact composition;
- focus indication;
- header profile/machine/attention state;
- description placement;
- details-pane availability;
- viewport behavior;
- command palette/help overlays;
- dimmed overlay composition;
- notification placement;
- footer action enablement;
- no-color semantic fallbacks.

Snapshot/golden testing is not required if existing focused string assertions cover the behavior reliably. Avoid introducing brittle full-screen golden files solely for this refactor.

## 10. Testing strategy

The refactor must add regression coverage specifically around the ownership seams it changes.

### 10.1 Root event dispatch tests

Add table-driven or focused tests proving that root-owned behavior is the same whether the message arrives:

- directly at the root; or
- wrapped with an originating screen.

At minimum cover:

- Overview target navigation;
- Capture completion refresh;
- Notice notification;
- Handoff request;
- Handoff completion where practical.

Tests should verify observable root state/commands rather than internal helper call counts.

### 10.2 Origin preservation

Add a regression test proving that an unhandled wrapped screen message is delivered back to the originating screen even when another screen is currently selected.

This protects the asynchronous screen-routing contract.

### 10.3 Existing input regressions remain green

Retain and run existing tests for:

- command palette text ownership;
- help text ownership;
- Config filter reserved keys;
- Config filtered/collapsed focus;
- modal confirmation/input;
- sidebar/group navigation;
- responsive layouts;
- welcome chooser;
- capture refresh behavior.

Do not weaken these tests merely to accommodate file moves.

### 10.4 Adapter tests

Where screen adapter behavior is currently only indirectly tested, preserve or add focused tests for unusual key-ownership exceptions.

Especially protect:

- Config `/` filtering behavior;
- Resources `Tab`;
- provider `Tab`;
- group navigation keys;
- Machines workspace key ownership.

### 10.5 Verification

The implementation PR must run:

```sh
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Also run the TUI package tests directly while iterating:

```sh
go test ./internal/tui/... -count=1
```

No new runtime dependency or test dependency is expected.

## 11. Implementation sequence

The implementation plan should preserve reviewability by moving one ownership seam at a time.

Recommended order:

1. lock root-event behavior with regression tests;
2. introduce the single root event dispatcher and remove duplicate handling;
3. extract screen registry and adapters without changing behavior;
4. extract modal/palette/help handling;
5. extract welcome/profile-chooser handling;
6. extract root view composition;
7. simplify `model.go` imports/control flow;
8. run full verification and manual diff review for unintended user-facing changes.

Prefer commits that each compile and pass the TUI test suite.

Avoid one giant "move everything" commit because it makes behavioral review unnecessarily difficult.

## 12. Non-goals

PR #27 does not include:

- onboarding or empty-state copy improvements;
- new welcome steps;
- new screens;
- new commands;
- new keybindings;
- action label changes;
- help text changes;
- sidebar changes;
- TUI visual redesign;
- workflow/domain refactors;
- provider consolidation;
- schema changes;
- CLI changes;
- new background work;
- new network behavior;
- migration UI;
- agent features.

The next TUI product pass may improve onboarding and empty states once this internal ownership work is merged.

## 13. Acceptance criteria

PR #27 is complete when all of the following are true:

- `model.go` is primarily root state plus high-level orchestration, not the home of screen adapters and all rendering/transient implementations;
- every root-owned message behavior has one canonical implementation;
- wrapped screen messages retain their originating screen;
- unhandled wrapped messages return to the originating screen;
- root-owned event families that are valid in both direct and wrapped forms behave equivalently;
- screen adapters have one clear file/owner;
- screen construction has one clear root registry;
- modal/palette/help mechanics are isolated from ordinary root navigation logic;
- welcome/profile-chooser mechanics are isolated from ordinary root navigation logic;
- root rendering is separated from root update orchestration;
- no user-facing copy, keybinding, navigation, workflow, or safety behavior changes;
- no `internal/workflow`, provider, schema, CLI, or JSON semantic changes;
- existing TUI tests remain green;
- new routing/ownership regression tests are green;
- `go test ./... -count=1` passes;
- `go vet ./...` passes;
- `go build ./cmd/omarchy-blueprint` passes;
- `git diff --check` passes.

## 14. Review guidance

Review this PR as a behavioral refactor, not as a line-count exercise.

A useful review question for every moved responsibility is:

> If this behavior changes later, is there now one obvious place to make that change?

Reject changes that merely move duplicated logic into different files.

Pay particular attention to:

- duplicate root event handling surviving the extraction;
- origin loss for asynchronous screen messages;
- global keys stealing input from active screen transients;
- modal responses being delivered to the currently active screen instead of the requesting screen;
- capture/session reload failing to refresh the same screens as before;
- handoff behavior drifting between direct and wrapped messages;
- accidental copy/keybinding changes hidden inside mechanical moves.

The desired end state is deliberately boring: the application should behave exactly as before, while the next engineer can understand the root TUI without treating `model.go` as the entire application.
