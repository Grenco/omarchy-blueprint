# ADR 0018: TUI Root Ownership and Origin-Preserving Event Routing

## Status

Accepted

## Context

ADR 0017 established the TUI as Omarchy Blueprint's primary interactive client, backed by shared native Go workflows rather than subprocess calls to the CLI. That architecture is working and remains unchanged.

After the first TUI comprehension pass, the root Bubble Tea implementation has accumulated several responsibilities in `internal/tui/model.go`:

- root model state and initialization;
- construction and wrapping of all screens;
- root-facing screen adapters, actions, and bindings;
- global input/focus/navigation handling;
- command palette, help, confirmation, and text-input modals;
- profile Create/Open/Quit flow;
- root rendering and responsive pane composition;
- capture/session refresh orchestration;
- external-tool handoff;
- routing of asynchronous screen messages.

The problem is not merely that `model.go` is large. Several concerns have overlapping ownership.

In particular, some root-owned events have handling logic in both the origin-tagged screen-message path and the top-level `Update` path. Examples include Overview navigation, capture completion, notifications, and external-tool handoff. Keeping multiple implementations of the same root effect creates a risk that one path gains refresh, notification, or navigation behavior that the other does not.

At the same time, origin information is necessary. Bubble Tea commands launched by one screen may finish after the user has navigated elsewhere. A Config or Resources result must return to the screen that launched it, not to whichever screen is active when the command completes.

The next planned TUI work is onboarding and empty-state polish. Before adding more behavior, the root needs clearer ownership boundaries so future changes do not make the coordinator harder to reason about.

This ADR refines the internal implementation of ADR 0017. It does not change the TUI's product behavior, workflow architecture, CLI parity rule, or safety semantics.

## Decision

### 1. Keep `model` as the Bubble Tea root coordinator

Do not replace the current root with a generic reducer, event bus, framework, or second orchestration layer.

`model` remains responsible for:

- application-level state;
- Bubble Tea `Init`, `Update`, and root `View`;
- focus and screen navigation;
- owning the active session and cancellation context;
- deciding precedence between root transients, screens, and global navigation.

The implementation should be split by responsibility inside `internal/tui`, but the package remains one coherent Bubble Tea client.

### 2. Give root-owned events one canonical dispatcher

Introduce one explicit root-event handling path.

Conceptually:

```go
func (m model) handleRootMessage(origin ScreenID, msg tea.Msg) (model, tea.Cmd, bool)
```

The exact signature may vary, but the result must distinguish handled from unhandled messages.

Root-owned event families include behavior such as:

- opening root help;
- accepting a screen-requested modal;
- Overview navigation;
- capture-completion refresh;
- session reload;
- external-tool handoff requests and results;
- machine mutation notifications;
- root notices.

A root-owned event is implemented once. The top-level `Update` path and origin-tagged screen path both delegate to that implementation where both forms are valid.

Messages whose semantics require an originating screen, such as a screen-requested confirmation that must return the answer to its requester, remain origin-required. The root must not invent an ambiguous direct form.

Use a straightforward type switch. Do not add reflection, registration maps, or an event bus.

### 3. Preserve the originating screen for asynchronous screen commands

Screen commands remain wrapped with their `ScreenID`.

Conceptually:

```text
screen command
    ↓
screenMsg{Screen: origin, Msg: result}
    ↓
root event dispatcher
    ├── root owns result → handle root effect once
    └── otherwise → deliver result to origin screen
```

If an origin-tagged message is not root-owned, it must be delivered to the originating screen even when another screen is active.

Any follow-up command returned by that screen remains wrapped with the same origin.

This is a correctness invariant, not an implementation convenience.

### 4. Treat root-facing screen wrappers as adapters

The root-facing wrappers for Config, Overview, Capture, Resources, Machines, Restore, Sync, and generic category screens are adapters between `internal/tui/screens` and the root TUI contract.

Those adapters own:

- root `ScreenID`;
- key-ownership decisions;
- command-palette actions;
- contextual bindings/help/footer metadata;
- adaptation of screen-local action state into root `Action` values.

They do not own provider semantics or screen business logic.

Move these adapters out of `model.go` into focused package-local files.

### 5. Centralize screen construction

Provide one fixed screen registry/construction path for the shipped screen set.

It may create placeholders when no session is available and concrete screens when a shared `workflow.Session` exists.

The registry is not a plugin system. The top-level screen set remains fixed and explicit.

Individual screens must not construct their own workflow/domain services.

### 6. Isolate transient UI flows from ordinary root navigation

Command palette, help, root confirmation/text-input modals, and profile Create/Open/Quit each have state-machine behavior distinct from normal workspace navigation.

Move their implementation into focused package-local files while preserving current precedence:

```text
ctrl-c / process-level quit handling
    ↓
active root modal or welcome flow
    ↓
active screen transient / workspace key ownership
    ↓
root navigation and global shortcuts
```

The root remains responsible for choosing which transient gets priority.

Screen-requested modal responses must be sent back to the requesting screen, not the currently active screen.

The welcome/profile chooser is isolated structurally only. Its user-facing copy and behavior are unchanged in this decision; onboarding redesign is a later product change.

### 7. Separate root rendering from root state transitions

Root rendering remains in `internal/tui`, but ordinary view composition should not live beside the full update implementation.

Focused rendering code owns:

- header;
- footer/status bar;
- responsive pane composition;
- workspace descriptions;
- sidebar and details viewports;
- terminal-too-small output;
- root overlays/modal rendering.

The existing layout breakpoints, descriptions, notification placement, focus indicators, and no-color behavior remain unchanged.

### 8. Do not change domain or public behavior

This consolidation must not change:

- user-facing copy;
- sidebar order/grouping;
- keybindings;
- command-palette commands;
- help behavior;
- responsive breakpoints;
- Config policy/group semantics;
- Capture behavior;
- Restore Normal/Forced behavior;
- Profile Git behavior;
- Create/Open/Quit behavior;
- external-tool handoff;
- CLI or JSON behavior;
- profile schema;
- provider semantics;
- `internal/workflow` semantics.

If the consolidation exposes an unrelated behavioral bug, address it separately unless preserving the bug would prevent the refactor from maintaining its routing invariants.

## Consequences

### Positive

- each root-level responsibility has one obvious owner;
- root-owned event effects cannot silently diverge between direct and wrapped paths;
- asynchronous screen results retain their correct owner after navigation;
- screen action/binding adaptation becomes easier to review independently of root navigation;
- modal and onboarding state transitions stop obscuring ordinary root input routing;
- root rendering can evolve without editing event-dispatch logic;
- onboarding/empty-state work can build on a clearer root without combining product changes with structural refactoring;
- ADR 0017's native workflow/client architecture remains intact.

### Negative

- a behavior-preserving PR touches several files even though product behavior does not change;
- code movement can create noisy diffs and requires careful regression review;
- package-local boundaries remain compile-time conventions rather than separate Go packages;
- some root state fields remain on `model` even after their behavior moves to focused files, because restructuring all state at once would add unnecessary risk;
- reviewers must distinguish meaningful routing changes from mechanical extraction.

## Rejected alternatives

- **Leave `model.go` as-is:** avoids short-term churn but preserves duplicate root-event ownership and makes the next TUI product changes harder to audit.
- **Only split the file mechanically:** improves file size but leaves duplicated event behavior and unclear ownership intact.
- **Move each adapter into its screen package:** couples `internal/tui/screens` to root-specific concepts such as global `ScreenID`, command-palette `Action`, and root key ownership.
- **Introduce an event bus or registration framework:** adds indirection and runtime wiring where an explicit type switch is easier to understand and test.
- **Replace the root with a generic reducer architecture:** unnecessary for the current application and creates a broad rewrite with no user benefit.
- **Move orchestration into `internal/workflow`:** violates the presentation/domain boundary; focus, modal routing, screen ownership, and Bubble Tea messages are TUI concerns.
- **Combine consolidation with onboarding polish:** makes it difficult to prove behavior preservation and obscures whether regressions come from refactoring or intentional UX changes.

## References

- `docs/adr/0017-tui-interactive-client-architecture.md`
- `docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md`
