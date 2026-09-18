# TUI Contextual Empty-State Onboarding Design

## Status

Proposed for PR #28.

## Summary

Omarchy Blueprint's TUI exposes a fixed set of workflow, software, customisation, and profile screens. Some of those screens represent optional capabilities that a user may never use: Hooks, Resources, machine-specific Resource paths, Profile Git, custom Shell state, and similar categories.

Today, an empty screen often reports only that nothing is present. That is technically correct but can leave a new user asking why the section exists, whether something is missing, and whether they are expected to configure it.

PR #28 adds contextual onboarding to empty and unconfigured states.

The governing rule is:

> When a section exists because Blueprint supports something the user may not currently use, explain what it is, why Blueprint has a place for it, and whether the user needs to do anything.

This is presentation and workflow guidance only. It does not add an onboarding wizard, persist onboarding progress, change capture or restore semantics, add profile fields, or change CLI behavior.

## Product principles

### 1. Empty-state guidance should reassure, not create homework

An optional feature being empty is not an error, warning, incomplete setup, or failed onboarding step.

Guidance should say when emptiness is normal and should only suggest an action when that action is genuinely useful.

Do not use warning/error styling for legitimate empty states.

### 2. Explain purpose before action

An empty-state message should answer, in this order:

1. **What is this?**
2. **Why does Blueprint support it?**
3. **Is it normal that there is nothing here?**
4. **What can I do next, if I actually want this feature?**

Do not lead with a keybinding or call to action before explaining why the feature exists.

### 3. Never hide useful pre-capture or live information

Contextual onboarding supplements existing inspection and controls; it does not replace them.

Examples:

- Capture continues to list all available categories before first capture.
- Config continues to show the live configuration candidates it already discovers and continues to allow policy control.
- Resources continues to expose its Discover workflow.
- Existing populated saved-state, difference, and restore views remain unchanged.

If a screen already has useful content, do not replace it with a large onboarding panel merely because the profile is new.

### 4. Do not claim knowledge Blueprint does not have

The current workflow can distinguish a captured category from an uncaptured category. It cannot, for every uncaptured category, show the full state that a future capture would produce.

Therefore:

- **uncaptured** may be described as "not saved to this profile yet";
- **captured and empty** may be described as genuinely empty when the saved state proves that;
- an uncaptured category must not say "nothing found on this machine" unless that screen already performs an independent live inspection that supports the claim.

This distinction is especially important for Hooks, Packages, Themes, Plugins, Defaults, and Shell.

### 5. Guidance is derived, not persisted

There is no:

- onboarding-complete flag;
- dismissed-hint state;
- checklist progress;
- first-run timestamp;
- tutorial mode;
- profile-schema field;
- separate onboarding state machine after profile creation.

The TUI derives guidance from state it already owns: profile capture metadata, screen results, tracked Resources, machine mappings, Profile Git status, filters, and other existing presentation state.

When the relevant state changes, the guidance naturally disappears or changes.

## Relationship to existing screen descriptions

The short and long screen descriptions established by the TUI comprehension work remain authoritative and unchanged.

Screen descriptions answer "what is this screen?" in normal use.

Empty-state onboarding is more specific. It answers "why is this screen empty for me, and is that okay?"

Avoid copying the long help description verbatim into an empty state.

## State vocabulary

The design uses three presentation states.

### Uncaptured

Blueprint has not saved desired state for the category in this profile.

The message may explain the category and say that Capture is how it becomes part of the profile.

It must not imply that the live system has been fully inspected for the would-be captured contents unless the screen independently performs that inspection.

### Captured but empty / zero-state

Blueprint has captured the category, and its saved state legitimately contains no items or no Blueprint-managed customisation.

The message explains why that is a valid result.

It should not nag the user to capture again.

### Optional feature not configured

The feature is not primarily represented by a normal capture flag, or its useful empty state is better explained by feature-specific state.

Examples include:

- no tracked Resources;
- no machine-specific Resource paths;
- profile is not a Git repository.

The message explains when the feature becomes useful and may name the existing action that starts it.

## Fresh-profile experience

A newly created profile remains a normal Blueprint profile. The full sidebar and existing screens remain available.

There is no wizard.

### Overview

When the profile has no captured system categories, Overview should show a compact orientation message instead of implying that a clean/empty Overview means the profile is already fully configured.

Proposed copy:

> **Nothing has been captured yet**  
> Capture is where you choose which parts of this machine Blueprint should remember. You can still explore the other sections to understand what each category covers.  
> **Next:** open Capture when you're ready to save something.

This message is informational, not a selectable checklist item.

Once at least one normal capture category has been captured, Overview returns to its existing presentation.

### Capture

Capture continues to list every available category before first capture.

When no category has been captured yet, add a compact hint above the existing table:

> **Choose what this profile should remember**  
> Nothing has been captured yet. Select the categories you want in this profile; you do not need to capture everything.

The table, selection controls, capture confirmation, refresh behavior, and capture semantics stay unchanged.

PR #28 does not add a detailed pre-capture candidate preview.

### Restore

When no restorable category has been captured, do not present the normal zero-operation comparison as if it were an ordinary already-satisfied restore.

Proposed copy:

> **Nothing to restore yet**  
> Restore recreates state that is already saved in this Blueprint profile. Capture the parts of this machine you want Blueprint to remember before using Restore.

When categories have been captured but the machine already matches the profile, retain the existing successful "no restore operations required" behavior. That is a healthy comparison result, not onboarding.

## Category screen guidance

Generic category screens should support category-specific guidance rather than mechanically inserting the category name into one generic paragraph.

The exact implementation can use a small presentation catalogue/helper, but the copy remains explicit and reviewable.

### Packages

#### Uncaptured

> **Packages are not saved in this profile yet**  
> Packages is where Blueprint remembers portable system packages, AUR packages, and global Mise tools so they can be installed again on another machine.  
> Capture Packages when you want that software set to become part of the profile.

#### Captured and empty

> **No packages or tools to carry**  
> Packages is where Blueprint remembers portable system packages, AUR packages, and global Mise tools so they can be installed again on another machine.  
> If this profile intentionally has no portable packages or tools, there is nothing to do here.

Do not describe machine-specific or excluded package behavior differently in this PR.

### Themes

#### Uncaptured

> **Themes are not saved in this profile yet**  
> Themes lets Blueprint remember your selected Omarchy theme and reconstruct installed or local themes when needed.  
> Capture Themes when you want that appearance state to become part of the profile.

#### Captured and empty

> **No themes to carry**  
> Themes lets Blueprint remember your selected Omarchy theme and reconstruct installed or local themes when needed.  
> If there is no theme state Blueprint needs to carry, this section can stay empty.

Use the established user-facing term **installed theme**, not "git theme."

### Plugins

#### Uncaptured

> **Plugins are not saved in this profile yet**  
> Plugins are Omarchy extensions Blueprint can reconstruct, including installed and local plugins.  
> Capture Plugins when you want those extensions to become part of the profile.

#### Captured and empty

> **No plugins to carry**  
> Plugins are Omarchy extensions Blueprint can reconstruct, including installed and local plugins.  
> Having no plugins is completely normal. There is nothing to configure here.

Use **Installed plugins**, not "Git plugins."

### Defaults

Do not add a large captured-empty tutorial state.

Defaults already represents concrete choices such as terminal, browser, editor, and agent, and empty individual values have an understandable domain meaning.

For an uncaptured Defaults category, a short category-specific explanation may replace the generic "No saved state yet" wording:

> **Defaults are not saved in this profile yet**  
> Defaults records the terminal, browser, editor, and agent choices Omarchy knows about. Capture Defaults when you want Blueprint to remember those selections.

Do not imply that every default must be set.

### Shell

#### Uncaptured

> **Shell state is not saved in this profile yet**  
> Shell is for supported customisations to the Omarchy shell, bar, and layout. Capture it when you want Blueprint to remember those changes.

#### Captured zero-state

> **No Blueprint-managed Shell customisation**  
> Shell is for supported customisations to the Omarchy shell, bar, and layout.  
> If you use the normal Omarchy shell setup here, there is no extra Shell state to carry.

This wording must follow the existing Shell semantics: an empty saved Shell hash can represent a captured desired state with no Blueprint-managed Shell customisation.

### Hooks

Hooks is the strongest example of the PR's onboarding goal.

#### Uncaptured

> **Hooks are not saved in this profile yet**  
> Hooks are scripts Omarchy runs automatically at supported events. Blueprint can carry those scripts so the same automation is available when you rebuild another machine.  
> Capture Hooks only when you want Blueprint to remember them.

Do not say "no hooks found" in this state. Uncaptured state alone does not prove that.

#### Captured and empty

> **No hooks to carry**  
> Hooks are scripts Omarchy runs automatically at supported events. Blueprint can carry those scripts so the same automation is available when you rebuild another machine.  
> Having no hooks is completely normal. There is nothing to set up here.

## Config

Config already performs independent live discovery and has richer state than the generic captured-category screens.

Keep its existing candidate groups, policy controls, inspection, and diff behavior.

When there are genuinely no underlying Config candidates to present, replace the bare empty message with contextual guidance:

> **No configuration needs review**  
> Config is for dotfiles and application/system configuration that differs meaningfully from Omarchy defaults.  
> If there is nothing suitable for Config to manage, there is nothing to do here.

This guidance applies only when the underlying candidate set is empty.

If a text filter is active and merely produces zero visible rows, do **not** show the global empty-state explanation. Show a filter-specific result such as:

> No configuration matches this filter.

This prevents the UI from claiming the system has no relevant configuration when the user has simply filtered it out.

The exact Config policy meanings remain unchanged:

- Auto — Blueprint decides based on Omarchy defaults, ownership, and safety rules.
- Included — You asked Config to manage this path when it is safe to do so.
- Excluded — You asked Config to leave this path alone.

Included never overrides safety rules.

## Resources

When no Resources are tracked and the user is on the Tracked view, replace the table-only "No tracked resources" placeholder with contextual guidance.

Proposed copy:

> **No extra resources tracked**  
> Resources are files, folders, or Git projects you want Blueprint to carry when they do not fit one of its normal categories.  
> You only need this when there is something extra you want to reconstruct. Press **Tab** to Discover one.

The Discover tab/browser remains unchanged.

Do not imply that every profile should have Resources.

If the user is already in Discover, do not show the Tracked empty-state message.

## Machines

Machines exists to provide per-machine path overrides for portable Resources. Its empty state should explain that dependency.

### No relevant Resources

When there are no Resources for which machine-specific paths could matter:

> **No machine-specific paths needed**  
> Machines only matters when a Resource needs a different location on one computer. There are no Resources here that need mapping yet.  
> There is nothing to configure on this screen.

Do not encourage adding a machine solely to make the screen non-empty.

### Resources exist, but no override is needed

When Resources exist and there are no machine-specific path overrides:

> **Portable paths are in use**  
> Resources normally use the same portable path on every machine. Add a machine-specific mapping only when one computer needs a different location.  
> If the normal Resource paths work here, there is nothing to configure.

Existing Add / Use / Rename / Remove / Map behavior and keybindings remain unchanged.

## Sync

When the profile is not a Git repository, replace the one-line state with contextual guidance:

> **Profile Git is not set up**  
> Sync is optional. It keeps the Blueprint profile itself in Git so you can version it and share it between machines.  
> Press **i** to initialize a repository if you want to use Sync.

Do not imply that Git is required for Blueprint.

When the profile is a repository with a clean working tree, retain the existing normal Sync status. A clean repository is not an onboarding state.

## Presentation rules

### Placement

Empty-state guidance lives in the ordinary workspace body.

Do not use:

- a modal;
- a toast;
- a welcome wizard;
- a command-palette entry;
- a dismissible banner;
- a separate onboarding screen.

The existing profile Create/Open/Quit flow remains unchanged except for any narrowly necessary copy consistency.

### Visual hierarchy

Use existing TUI styles.

A typical empty state contains:

1. a concise heading;
2. one or two short explanatory lines/paragraphs;
3. an optional next-action line.

Legitimate empty states should use neutral or positive styling, never error/warning styling.

Avoid introducing a new visual design system solely for onboarding.

A small shared rendering helper is acceptable if it keeps wrapping/spacing consistent and remains presentation-only.

### Width and responsive behavior

Guidance must remain readable at the TUI's existing responsive widths.

Requirements:

- wrap prose to the available workspace width;
- do not force horizontal scrolling;
- do not add a new breakpoint;
- do not change the existing 140/100/80/too-small layout model;
- terminal-too-small behavior remains owned by the root and unchanged.

Do not use brittle full-screen golden tests for wrapping.

### Footer and actions

Existing keybindings, contextual actions, command palette entries, and footer ownership remain unchanged.

An empty-state message may mention an action that already exists, for example:

- `Tab` to Discover a Resource;
- `i` to initialize Profile Git.

Do not add a new shortcut solely for onboarding in this PR.

Do not mention a keybinding as a call to action unless that action is actually enabled in the displayed state.

## Implementation boundaries

Expected implementation area is the TUI presentation layer, primarily existing screens and focused presentation helpers under `internal/tui`.

The implementation may:

- add small helpers/catalogue data for category-specific empty-state copy;
- inspect existing `workflow.Session.Profile()` / profile state where already available to the TUI;
- use existing screen state to distinguish empty conditions;
- change TUI tests to assert the new contextual messages.

The implementation must not:

- add or change profile schema;
- change capture semantics;
- change restore planning or apply semantics;
- add a pre-capture candidate inspection API;
- change provider/category domain semantics;
- change Config classification or policy semantics;
- change Resources tracking semantics;
- change machine overlay semantics;
- change Profile Git operations;
- change CLI output or CLI commands;
- shell out from the TUI to the Blueprint CLI;
- add background/network behavior;
- persist onboarding progress.

## Explicitly deferred: pre-capture candidate preview

A future piece of work should consider richer, non-mutating inspection of what each category would capture before first capture.

That work is intentionally outside PR #28.

The current workflow's capture-status surface can list uncaptured categories, but it does not provide the full would-be captured state for every uncaptured category. Solving that properly requires a workflow/domain design for safe candidate inspection rather than onboarding copy pretending the data is already available.

That future work should preserve the Blueprint safety sequence:

**inspect → calculate → preview → approve → apply → verify**

PR #28 should not create an ad hoc TUI-only version of that capability.

## Testing strategy

Tests should verify state-specific presentation without coupling to complete terminal snapshots.

### Root / Overview

Cover:

- completely uncaptured profile shows fresh-profile orientation;
- once at least one relevant category is captured, the special fresh-profile orientation disappears;
- normal Overview attention/healthy behavior remains unchanged.

### Capture

Cover:

- fresh profile still renders every category;
- fresh-profile hint appears without replacing the category table;
- existing selection and confirmation behavior remains unchanged.

### Restore

Cover:

- no captured restorable state shows onboarding guidance;
- captured profile with zero required operations shows the ordinary clean restore state;
- normal/forced behavior is unchanged when operations exist.

### Generic category screens

Cover at least:

- uncaptured category copy;
- captured-and-empty category copy;
- populated category suppresses empty guidance;
- Hooks wording specifically distinguishes uncaptured from known-empty;
- Shell zero-state uses the Shell-specific explanation;
- existing Changes/Saved tabs and capture actions remain usable.

Prefer testing the copy catalogue/helper directly for all supported category variants rather than duplicating full screen setup for every string.

### Config

Cover:

- truly empty candidate set shows contextual guidance;
- active filter with no visible matches shows filter-specific empty text instead;
- populated candidates retain the normal grouped table;
- Config policy meanings and actions are unchanged.

### Resources

Cover:

- no tracked Resources shows contextual guidance;
- `Tab` Discover remains available;
- Discover mode does not show the Tracked empty-state explanation;
- tracked Resources retain the existing table.

### Machines

Cover:

- no relevant Resources shows the dependency explanation;
- Resources with no machine overrides show the portable-path explanation;
- existing mappings retain the current tables/actions.

### Sync

Cover:

- non-repository profile shows optional-Git explanation and the existing initialize key;
- repository clean state retains normal repository status;
- existing action enablement remains unchanged.

### Regression verification

Run at minimum:

```text
go test ./internal/tui/... -count=1
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Manual acceptance remains important at the project's established terminal widths:

- 140 columns;
- 100 columns;
- 80 columns;
- terminal-too-small state.

The formal real-Omarchy acceptance checkpoint remains scheduled after PR #28.

## Success criteria

PR #28 succeeds when:

- a user encountering an optional empty section can understand why it exists without consulting external documentation;
- legitimate emptiness is presented as normal rather than incomplete;
- uncaptured state is not mislabeled as known-empty live state;
- fresh profiles remain fully navigable and Capture remains populated with selectable categories;
- useful existing inspection/control surfaces remain visible;
- hints disappear or change naturally as existing state changes;
- there is no persisted onboarding state or wizard;
- there are no profile, workflow, provider, capture, restore, or CLI semantic changes;
- the future pre-capture candidate-preview capability remains clearly separate.

## Non-goals

- onboarding wizard or checklist;
- onboarding completion/dismissal persistence;
- new sidebar screens;
- new navigation model;
- new keybindings;
- candidate-state preview before first capture;
- profile-schema changes;
- workflow/provider redesign;
- capture/restore semantic changes;
- Config semantic changes;
- Resources or Machines semantic changes;
- Profile Git semantic changes;
- CLI changes;
- documentation rewrite outside the focused design/ADR/implementation artifacts;
- broad visual redesign.

## References

- `docs/adr/0017-tui-interactive-client-architecture.md`
- `docs/adr/0018-tui-root-ownership-and-origin-preserving-event-routing.md`
- `docs/planning/specs/2026-09-13-tui-comprehension-design.md`
- `docs/planning/specs/2026-09-17-tui-internal-consolidation-design.md`
