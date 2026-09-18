# ADR 0019: Contextual, State-Derived TUI Onboarding

## Status

Accepted

## Context

Omarchy Blueprint's TUI intentionally exposes a fixed set of screens for the capabilities Blueprint supports.

Some capabilities are optional or situational:

- Hooks only matters when the user has hook scripts;
- Resources only matters when the user wants Blueprint to carry extra files, folders, or Git projects outside its normal categories;
- Machines only matters when a Resource needs a different path on a particular computer;
- Sync only matters when the user wants the Blueprint profile itself managed through Git;
- some captured categories can legitimately contain no items or no Blueprint-managed customisation.

A fixed navigation structure is useful because it makes Blueprint's capabilities discoverable. However, terse empty states such as "No saved hooks", "No tracked resources", or "Profile is not a Git repository" can make optional features look incomplete or broken.

The TUI could address this with a first-run wizard, an onboarding checklist, persisted dismissed hints, or a root-level onboarding mode. Those approaches introduce a second user journey and state model that is not necessary to explain the product.

The application also has an important constraint: uncaptured state does not always mean Blueprint has already inspected and calculated the complete state that a future capture would save. The UI must not turn an onboarding explanation into a false pre-capture preview.

## Decision

### 1. Onboarding is contextual and derived from existing state

The TUI will not maintain persistent onboarding progress.

Guidance is derived from existing application state such as:

- profile capture metadata;
- screen-local saved state;
- Config's existing live discovery;
- tracked Resources;
- machine mappings;
- Profile Git status;
- current filter or transient state.

When the underlying state changes, the guidance changes or disappears automatically.

There is no onboarding-complete flag, dismissal state, tutorial profile metadata, or separate first-run state machine after profile creation.

### 2. Empty states explain optional capabilities without making them required

When an empty screen could reasonably make a user ask why it exists, the TUI explains:

1. what the feature represents;
2. why Blueprint supports it;
3. whether having nothing there is normal;
4. the existing next action, only when the user actually wants the feature.

Legitimate emptiness is neutral or positive. It is not presented as a warning, error, failed setup step, or task the user must complete.

### 3. Guidance supplements useful content; it does not replace it

The presence of onboarding guidance must not hide useful inspection or controls.

In particular:

- Capture continues to show all available categories before first capture;
- Config continues to show and control the candidates it already discovers;
- Resources continues to expose Discover;
- populated saved-state and difference views remain the primary content.

A completely fresh profile remains a normal, fully navigable profile rather than entering a wizard mode.

### 4. Distinguish uncaptured from known-empty

The TUI must only make claims supported by its current state.

An uncaptured category may be described as not yet saved to the profile and may explain what the category is for.

It must not be described as "nothing found on this machine" merely because there is no saved state.

A captured category may use a stronger empty-state explanation when its captured state proves that there are no items or no Blueprint-managed customisations to carry.

Screens with independent live discovery, such as Config, may use the knowledge produced by that existing inspection.

### 5. Keep onboarding in the presentation layer

This decision does not change Blueprint's workflow or persistence model.

PR #28 must not add:

- profile-schema fields;
- provider/category semantics;
- capture behavior;
- restore behavior;
- a pre-capture candidate inspection API;
- Resources or machine-overlay semantics;
- Profile Git operations;
- CLI behavior.

A small TUI presentation helper or copy catalogue is acceptable to keep contextual empty states consistent.

### 6. Pre-capture candidate preview is separate work

Showing the complete state that every uncaptured category would save is valuable, but it is not an onboarding-copy problem.

The current capture-status surface can make categories discoverable before first capture without necessarily providing each category's full would-be captured contents.

A richer candidate preview requires a separate workflow/domain design for non-mutating inspection and should follow Blueprint's safety sequence:

**inspect → calculate → preview → approve → apply → verify**

PR #28 will not fake or partially invent that capability in TUI code.

## Consequences

### Positive

- users can understand optional Blueprint capabilities in the place where confusion occurs;
- empty sections no longer look like missing setup by default;
- no tutorial state can become stale or disagree with the actual profile;
- guidance naturally disappears when the user starts using a feature;
- fresh profiles remain fully explorable;
- Capture and other useful pre-capture controls stay visible;
- the TUI does not overstate what Blueprint has inspected;
- the future candidate-preview design remains cleanly separated from presentation work.

### Negative

- contextual copy must be maintained across several screens;
- some categories require specific wording instead of one universal empty-state string;
- screen tests need to distinguish uncaptured, captured-empty, filtered-empty, and configured states;
- without a wizard, users are guided locally rather than through one linear tutorial;
- a future candidate-preview feature may revise some uncaptured-state presentation once richer inspection data exists.

## Rejected alternatives

- **First-run wizard:** creates a separate journey, encourages a linear setup model, and hides the fact that many Blueprint features are optional.
- **Persistent onboarding checklist:** introduces state that can become stale and makes optional capabilities look like required tasks.
- **Dismissible hints stored in the profile:** adds profile semantics for a presentation concern and makes portable machine state depend on tutorial history.
- **One root-level onboarding banner for every empty screen:** lacks the domain context each screen already owns and tends to duplicate local explanations.
- **One generic "nothing here" template:** cannot explain why Hooks, Resources, Machines, Shell, and Sync are empty in meaningfully different ways.
- **Treat uncaptured as empty:** can falsely imply Blueprint inspected the live system and found nothing.
- **Add pre-capture preview as part of onboarding:** expands a presentation PR into workflow/domain design and risks an ad hoc TUI-only inspection path.

## References

- `docs/adr/0017-tui-interactive-client-architecture.md`
- `docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md`
- `docs/planning/specs/2026-09-18-tui-contextual-empty-state-onboarding-design.md`
