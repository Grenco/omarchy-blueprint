# TUI Comprehension & Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TUI easier to understand and navigate by adding grouped information architecture, layered screen explanations, plain-language terminology, clearer Config presentation, and Vim-friendly group jumps.

**Architecture:** Add one declarative screen catalogue in the root TUI package and make navigation, sidebar rendering, screen labels, descriptions, help, and navigation-search aliases read from it. Keep domain/internal values unchanged. Add generic anchor jumping to the existing selectable component, then use it in Config/shared-provider grouped lists while the root model handles sidebar-section jumps.

**Tech Stack:** Go 1.25+, Bubble Tea v2, Lip Gloss v2, existing `internal/tui/components`, existing workflow/provider APIs.

**Spec:** `docs/superpowers/specs/2026-09-13-tui-comprehension-design.md`

## Global constraints

- No profile-schema changes.
- No Config classification/safety semantic changes.
- No CLI/JSON field-name changes.
- No new network behaviour.
- Section headings are not selectable and are not collapsible.
- `j`/`k` continue to move only between selectable screens/rows.
- `[` / `]` are help-only structural navigation bindings and must not crowd the normal footer.
- Internal source value `git`, provider IDs, and baseline types remain unchanged; only presentation copy changes.
- Preserve current terminal-sanitisation, TTY/non-TTY, NO_COLOR, async request-ID, stale-message, and modal-ownership behaviour.

---

## File map

### Create

- `internal/tui/catalog.go` — screen/section metadata, ordering, descriptions, search aliases, sidebar-section navigation helpers.
- `internal/tui/catalog_test.go` — catalogue completeness/order/copy/search aliases/section-jump tests.
- `internal/tui/components/sidebar_test.go` — grouped-sidebar rendering and selected-line tests.
- `docs/superpowers/specs/2026-09-13-tui-comprehension-design.md` — approved product design.
- `docs/superpowers/plans/2026-09-13-tui-comprehension-navigation.md` — this plan.

### Modify

- `internal/tui/model.go` — consume catalogue; grouped sidebar; workspace description; contextual help; sidebar-section navigation.
- `internal/tui/model_test.go` — integration coverage for layout, descriptions, help, palette aliases, focus-aware `[`/`]`.
- `internal/tui/actions.go` — add a help-only binding flag without changing action semantics.
- `internal/tui/actions_test.go` — help-only binding stays searchable/help-visible.
- `internal/tui/components/sidebar.go` — render non-selectable section headings and report selected visual line.
- `internal/tui/components/selectable.go` — generic non-wrapping jump-to-anchor helper.
- `internal/tui/components/selectable_test.go` — anchor-jump semantics.
- `internal/tui/screens/config.go` — broad Config groups, plain-language state/policy/detail copy, `[`/`]`.
- `internal/tui/screens/config_test.go` — Config grouping, copy, default-collapse, counts, detail, group navigation.
- `internal/tui/screens/provider.go` — installed-vs-git copy and `[`/`]` group navigation.
- `internal/tui/screens/provider_test.go` — source wording and group navigation.
- Screen adapter/binding section in `internal/tui/model.go` — help-only group bindings and user-facing `category` wording for Capture actions.

---

## Task 1: Add the declarative screen catalogue

**Files**
- Create `internal/tui/catalog.go`
- Create `internal/tui/catalog_test.go`
- Modify `internal/tui/model.go`

**Interfaces**
- Produce:
  - `type ScreenSection string`
  - `type ScreenInfo struct { ID ScreenID; Label string; Section ScreenSection; Short string; Long string; Keywords string }`
  - `func screenInfo(ScreenID) ScreenInfo`
  - `func orderedScreenIDs() []ScreenID`
  - `func sidebarSections() []components.NavSection`
  - `func moveSidebarSection(current ScreenID, delta int) ScreenID`
- Consume later from navigation, sidebar rendering, help, and palette actions.

- [ ] **Write failing catalogue tests**

Assert exact screen order:

```go
want := []ScreenID{
    ScreenOverview,
    ScreenCapture, ScreenRestore,
    ScreenPackages, ScreenThemes, ScreenPlugins, ScreenDefaults,
    ScreenConfig, ScreenShell, ScreenHooks, ScreenResources,
    ScreenMachines, ScreenSync,
}
```

Assert exact visible section labels:

```text
Workflow
Software
Customisation
Profile
```

Assert every screen has non-empty Label, Short, Long, and Keywords.

Assert Config Short exactly equals:

```text
Review dotfiles and other system/app configuration Blueprint can remember and restore.
```

Assert Config keywords contain both `dotfiles` and `.config`.

- [ ] **Run focused tests and verify failure**

```bash
go test ./internal/tui -run 'TestScreenCatalog|TestSidebarSectionMovement' -count=1
```

Expected: FAIL because the catalogue API does not exist.

- [ ] **Implement `catalog.go` with the exact approved copy**

Use:

```go
type ScreenSection string

const (
    SectionWorkflow      ScreenSection = "Workflow"
    SectionSoftware      ScreenSection = "Software"
    SectionCustomisation ScreenSection = "Customisation"
    SectionProfile       ScreenSection = "Profile"
)

type ScreenInfo struct {
    ID       ScreenID
    Label    string
    Section  ScreenSection
    Short    string
    Long     string
    Keywords string
}
```

Populate every screen with the exact wording from the spec.

`Overview` has an empty Section.

`moveSidebarSection` is non-wrapping:
- Overview +1 → Capture
- Workflow +1 → Packages
- Software -1 → Capture
- Workflow -1 → Overview
- Profile +1 → unchanged

- [ ] **Derive root ordering and labels from the catalogue**

Replace the hand-written `screenOrder` literal with `orderedScreenIDs()`.

Replace generated label logic with `screenInfo(id).Label`.

Keep all `ScreenID` constants unchanged.

- [ ] **Run TUI tests**

```bash
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Commit**

```bash
git add internal/tui/catalog.go internal/tui/catalog_test.go internal/tui/model.go
git commit -m "refactor: centralize TUI screen catalogue"
```

---

## Task 2: Render grouped sidebar sections and screen descriptions

**Files**
- Modify `internal/tui/components/sidebar.go`
- Create `internal/tui/components/sidebar_test.go`
- Modify `internal/tui/model.go`
- Modify `internal/tui/model_test.go`

**Interfaces**
- Consume `sidebarSections()` and `screenInfo(id)`.
- Produce:
  - `type components.NavSection struct { Label string; Items []NavItem }`
  - `type components.SidebarRender struct { Lines []string; SelectedLine int }`
  - `func components.RenderSidebar(sections []NavSection, selectedID string, width int, styles Styles, focused bool) SidebarRender`

- [ ] **Write grouped-sidebar tests**

Cover:
- Overview renders first with no heading.
- WORKFLOW, SOFTWARE, CUSTOMISATION, PROFILE render as headings.
- Items are indented below headings.
- Selection styling applies only to screen rows.
- `SelectedLine` points to the actual visual line after headings/blanks.
- No heading can be selected.
- NO_COLOR still has a visible selected marker.

- [ ] **Run the component test and verify failure**

```bash
go test ./internal/tui/components -run TestGroupedSidebar -count=1
```

Expected: FAIL.

- [ ] **Implement grouped sidebar rendering**

Render:

```text
Overview

WORKFLOW
  Capture
  Restore

SOFTWARE
  Packages
  Themes
  Plugins
  Defaults

CUSTOMISATION
  Config
  Shell
  Hooks
  Resources

PROFILE
  Machines
  Sync
```

Headings use muted styling. They never receive selection styling.

Return the selected visual line so viewport scrolling works after headers/blanks are inserted.

- [ ] **Write workspace-description tests**

At normal width assert:
- Overview short description appears in workspace.
- Config short description appears after selecting Config.
- Description appears once and not in Details.
- terminal-width bounding tests stay green.

- [ ] **Integrate description rendering and height accounting**

In `contentView`:
- prepend the wrapped active-screen Short text;
- style it muted;
- add one blank separator line;
- then render the screen body.

When calling `SetSize`, subtract:
- wrapped short-description line count;
- one separator line.

Use one helper for initial size, resize, and fresh-profile model creation so those paths cannot disagree.

- [ ] **Run layout regressions**

```bash
go test ./internal/tui/... -count=1
```

Expected: PASS at compact, two-pane, and three-pane tests.

- [ ] **Commit**

```bash
git add internal/tui/components/sidebar.go internal/tui/components/sidebar_test.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: group TUI navigation and describe screens"
```

---

## Task 3: Make `?` contextual and navigation search human-friendly

**Files**
- Modify `internal/tui/actions.go`
- Modify `internal/tui/actions_test.go`
- Modify `internal/tui/model.go`
- Modify `internal/tui/model_test.go`

**Interfaces**
- Consume `screenInfo(id).Long` and Keywords.
- Extend `Binding` with `HideFromFooter bool`.
- `statusActions` ignores only footer-hidden bindings; help still includes them.

- [ ] **Write help-layout tests**

For Config with no query, assert this order:

```text
CONFIG

<Config long description>

Search: <input>

KEYS
...
GLOBAL
...
Focus: workspace
```

With query `diff`, assert:
- Config long description remains visible;
- matching diff help remains;
- unrelated key entries disappear.

- [ ] **Write navigation-alias tests**

Assert:
- `dotfiles` finds Config
- `rebuild` finds Restore
- `commit` finds Sync
- `laptop` finds Machines
- `scripts` finds Hooks

- [ ] **Run focused tests and verify failure**

```bash
go test ./internal/tui -run 'TestHelp|TestPalette.*Alias|TestAction.*Footer' -count=1
```

Expected: FAIL.

- [ ] **Add help-only binding support**

Add:

```go
HideFromFooter bool
```

to `Binding`.

In `statusActions`, skip bindings where it is true.

Do not skip them in `SearchHelp`.

- [ ] **Rebuild default help around the catalogue**

Default help order:
1. uppercase screen label
2. blank
3. wrapped Long description
4. blank
5. `Search: ...`
6. blank
7. `KEYS`
8. screen/context bindings
9. blank
10. `GLOBAL`
11. global bindings
12. blank
13. `Focus: ...`

Help search filters key/action entries only; description is pinned.

- [ ] **Use catalogue Keywords for navigation actions**

Keep IDs such as `screen.config` unchanged.

- [ ] **Run TUI tests**

```bash
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Commit**

```bash
git add internal/tui/actions.go internal/tui/actions_test.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: add contextual TUI help and search aliases"
```

---

## Task 4: Add `[` / `]` group navigation

**Files**
- Modify `internal/tui/components/selectable.go`
- Modify `internal/tui/components/selectable_test.go`
- Modify `internal/tui/screens/config.go`
- Modify `internal/tui/screens/config_test.go`
- Modify `internal/tui/screens/provider.go`
- Modify `internal/tui/screens/provider_test.go`
- Modify `internal/tui/model.go`
- Modify `internal/tui/model_test.go`

**Interfaces**
- Produce:

```go
func (s *Selectable) JumpToAnchor(anchors []int, forward bool, count, height int) bool
```

- Root uses `moveSidebarSection`.
- Config/shared Provider pass their visible group-header row indices as anchors.

- [ ] **Write anchor-jump tests**

Exact semantics:
- selected child row 3, anchors `[0, 2, 6]`, backward → 2
- selected header 2, backward → 0
- selected child 3, forward → 6
- first header backward stays put and returns false
- final group forward stays put and returns false
- no wrap
- viewport follows destination

- [ ] **Run component test and verify failure**

```bash
go test ./internal/tui/components -run TestSelectableJumpToAnchor -count=1
```

Expected: FAIL.

- [ ] **Implement `JumpToAnchor`**

Keep it generic: it knows only row anchors, not Config/provider concepts.

- [ ] **Write Config/shared-provider group-navigation tests**

Verify:
- `[` from child → current group heading
- second `[` → preceding group
- `]` → following group
- collapsed group remains collapsed when jumped to
- no wrap
- Enter remains expand/collapse
- Config filter input retains literal `[`/`]`

- [ ] **Implement screen group anchors**

Config: anchors are `rows()` entries with non-empty group.

Provider: anchors are `rows()` entries with non-empty group.

Handle brackets after transient/filter/input ownership checks and before ordinary row movement.

- [ ] **Write sidebar-section navigation tests**

With sidebar focus:
- Overview `]` → Capture
- Restore `]` → Packages
- Themes `[` → Capture
- Config `]` → Machines
- Sync `[` → Config
- Profile `]` → unchanged
- Workflow `[` → Overview
- no wrap

Repeat one case in compact mode with sidebar open.

When workspace focus is on an ungrouped screen, brackets must not change the active screen.

- [ ] **Implement root sidebar section jumps**

When `focus == focusSidebar`:
- consume `[` / `]`;
- call `moveSidebarSection`;
- update selected screen index;
- ensure sidebar viewport using the grouped render's visual SelectedLine;
- initialise destination screen.

- [ ] **Add context-sensitive help-only bindings**

Sidebar:
- `[` Previous sidebar section
- `]` Next sidebar section

Config/shared Provider workspace:
- `[` Previous group
- `]` Next group

Set `HideFromFooter: true`.

- [ ] **Run all TUI tests**

```bash
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Commit**

```bash
git add internal/tui/components/selectable.go internal/tui/components/selectable_test.go internal/tui/screens/config.go internal/tui/screens/config_test.go internal/tui/screens/provider.go internal/tui/screens/provider_test.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: add Vim-style group navigation"
```

---

## Task 5: Reframe Config around attention and plain language

**Files**
- Modify `internal/tui/screens/config.go`
- Modify `internal/tui/screens/config_test.go`

**Interfaces**
- Provider classifications stay unchanged.
- Add presentation metadata:

```go
type configDisplayGroup string

type configPresentation struct {
    Group   configDisplayGroup
    Label   string
    Meaning string
}
```

- `configPresentationFor(classification)` is the only source of user-facing state wording.

- [ ] **Write a table-driven test for all 14 Config classifications**

Exact mapping:

```text
ambiguous-baseline   -> Needs review / Needs review
ambiguous-deletion   -> Needs review / Removal needs review
modified-baseline    -> Changes / Changed from Omarchy default
deleted-baseline     -> Changes / Omarchy default removed
added                -> Changes / Added configuration
unchanged-baseline   -> No action needed / Matches Omarchy default
historical-baseline  -> No action needed / Matches a previous Omarchy default
delegated            -> Not managed by Config / Handled elsewhere
excluded             -> Not managed by Config / Excluded
volatile             -> Not managed by Config / Not suitable for capture
sensitive            -> Not managed by Config / Sensitive
oversized            -> Not managed by Config / Too large
unsupported          -> Not managed by Config / Unsupported
unmanaged-symlink    -> Not managed by Config / Symlink not managed here
```

Also assert exact Meaning text from the spec.

Do not allow fallback to raw internal classification strings.

- [ ] **Write grouping/default-collapse tests**

Exact order:
1. Needs review
2. Changes
3. No action needed
4. Not managed by Config

Default state:
- Needs review expanded
- Changes expanded
- No action needed collapsed
- Not managed by Config collapsed

- [ ] **Write summary tests**

Representative output:

```text
Review  2 need review · 7 changes · 18 no action · 14 not managed
```

For count 1, use:

```text
1 needs review
```

Do not print internal classification names.

- [ ] **Write detail-pane copy tests**

Assert:
- `Baseline:` absent
- `Provider reason:` absent
- `Effective policy:` absent
- `Available actions:` absent
- `Omarchy default:` present if path exists
- `Saved copy:` present if path exists
- `Saved in profile: Yes/No` reflects `inspection.Managed`
- `Meaning:` contains friendly explanation

- [ ] **Run Config tests and verify failure**

```bash
go test ./internal/tui/screens -run TestConfig -count=1
```

Expected: FAIL.

- [ ] **Implement presentation mapping and broad groups**

Do not touch `internal/providers/config`.

Keep table columns:

```text
Path | State | Policy
```

Broad group headings may show their counts.

- [ ] **Keep policy values but use exact explanations**

- Auto — `Blueprint decides based on Omarchy defaults, ownership, and safety rules.`
- Included — `You asked Config to manage this path when it is safe to do so.`
- Excluded — `You asked Config to leave this path alone.`

Never imply Included bypasses safety.

- [ ] **Simplify `DetailView`**

Use:

```text
Path:
State:
Policy:
Meaning:
Saved in profile:
Live file:
Omarchy default:
Saved copy:
```

Omit empty path lines.

- [ ] **Run Config and root integration tests**

```bash
go test ./internal/tui/screens -count=1
go test ./internal/tui -count=1
```

Expected: PASS.

- [ ] **Commit**

```bash
git add internal/tui/screens/config.go internal/tui/screens/config_test.go
git commit -m "feat: clarify Config states and policy"
```

---

## Task 6: Clean up user-facing provider terminology

**Files**
- Modify `internal/tui/screens/provider.go`
- Modify `internal/tui/screens/provider_test.go`
- Modify `internal/tui/model.go`
- Modify `internal/tui/model_test.go`

**Interfaces**
- Persisted/internal `git`, `builtin`, `local`, and provider IDs do not change.
- Presentation maps `git` to `installed`.

- [ ] **Write wording tests**

Assert:
- Plugins group is `Installed plugins`, never `Git plugins`.
- Git-backed theme source renders as `installed`, never `(git)`.
- Built-in/local wording remains readable.
- underlying profile values remain unchanged.

- [ ] **Write Capture action-copy tests**

Exact visible labels:

```text
Toggle category selection
Select changed categories
Capture selected categories
Capture all categories
Refresh capture status
```

- [ ] **Run focused tests and verify failure**

```bash
go test ./internal/tui/screens -run 'TestProvider.*Wording' -count=1
go test ./internal/tui -run 'TestCapture.*Action' -count=1
```

Expected: FAIL on old wording.

- [ ] **Implement presentation-only source labels**

Example helper:

```go
func sourceLabel(source string) string {
    switch source {
    case "git":
        return "installed"
    case "builtin":
        return "built-in"
    case "local":
        return "local"
    default:
        return source
    }
}
```

Do not mutate stored values.

- [ ] **Replace user-facing Capture `provider` with `category`**

Do not rename workflow APIs.

- [ ] **Run all TUI tests**

```bash
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Commit**

```bash
git add internal/tui/screens/provider.go internal/tui/screens/provider_test.go internal/tui/model.go internal/tui/model_test.go
git commit -m "chore: simplify TUI terminology"
```

---

## Task 7: Final regression and manual acceptance

- [ ] **Format and whitespace**

```bash
gofmt -w internal/tui internal/tui/components internal/tui/screens
git diff --check
```

Expected: no `git diff --check` output.

- [ ] **Complete automated verification**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: PASS.

- [ ] **Check no domain semantic changes slipped in**

```bash
git diff main...HEAD -- internal/providers internal/profile internal/restore internal/workflow
```

Expected: no domain semantic changes; `internal/workflow` should normally be untouched.

- [ ] **Manual acceptance: navigation**

At 140, 100, and 80 columns:
- section headings readable
- headings never selectable
- `j/k` skip headings
- screen descriptions wrap cleanly
- selected screen remains visible while sidebar scrolls
- compact sidebar remains usable
- NO_COLOR remains legible

- [ ] **Manual acceptance: help/search**

On Config, Hooks, Resources, Machines, and Sync:
- short description works as an at-a-glance reminder
- `?` explains the screen before keys
- search keeps description pinned
- `dotfiles`, `rebuild`, `commit`, `laptop`, `scripts` find intended screens

- [ ] **Manual acceptance: bracket navigation**

Verify:
- Config `[ ]`
- Packages `[ ]`
- Plugins `[ ]`
- sidebar `[ ]`
- collapsed groups
- first/last group no-wrap
- compact sidebar
- Config `/` filter can type literal brackets
- modal/help/palette inputs retain key ownership

- [ ] **Manual acceptance: Config comprehension**

A new reader should be able to answer from the UI:
- Config means dotfiles/system/app configuration, not Blueprint settings.
- State is what Blueprint found.
- Policy is what the user told Blueprint to do.
- Needs review requires attention.
- Changes are meaningful customisations/removals/additions.
- Not managed by Config explains why something is intentionally outside capture.
- Included does not override safety.

---

## PR acceptance criteria

1. Sidebar order/grouping exactly matches the spec.
2. Section headings are non-selectable.
3. Every screen uses exact approved short/long copy.
4. Description height is accounted for at supported widths.
5. Help pins explanation while filtering commands.
6. Human-language palette aliases work.
7. `[`/`]` navigate grouped workspace rows and sidebar sections without wrapping.
8. `[`/`]` never steal input/filter/modal keys.
9. Config shows four broad groups with exact state wording.
10. Low-priority Config groups start collapsed.
11. Config uses `Omarchy default`, not `baseline`, in normal presentation.
12. Config provider semantics remain unchanged.
13. Themes/plugins display `installed`, not `git`, while stored values remain unchanged.
14. Capture says `category/categories`, not `provider/providers`, to users.
15. Automated verification passes.
16. Manual acceptance passes at compact, two-pane, three-pane, and NO_COLOR modes.

---

## Explicit follow-up PRs

### PR 2 — User documentation

- Cut README to landing page + quick start + mental model + safety summary + docs links.
- Add:
  - `docs/getting-started.md`
  - `docs/concepts.md`
  - `docs/tui.md`
  - `docs/commands.md`
  - `docs/safety.md`
  - `docs/captured-state/README.md`
  - per-category docs for packages/config/resources/themes/plugins/shell/hooks/defaults
  - `docs/machines.md`
  - `docs/sync.md`
- Reuse the same approved vocabulary as the TUI.

### PR 3 — TUI internal consolidation

Behaviour-preserving split:

```text
model.go
catalog.go
navigation.go
view.go
modals.go
screen_adapters.go
handoff.go
```

Optimize for focused files and reduced repetition, not arbitrary line-count reduction.

### PR 4 — Onboarding and empty states

Add category-specific empty states that answer:
- what this screen is;
- whether the user needs to do anything;
- the obvious next action when one exists.

Do not slow down experienced-user workflows.
