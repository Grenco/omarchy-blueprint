# TUI Presentation Coherence Implementation Plan

> Execute inline with `superpowers:executing-plans`. The user's PR7.5 brief is the approved product specification and binding authority.

**Goal:** Restore one scannable, responsive, table-based visual language across the machine-aware TUI without changing policy, capture, restore, profile, or CLI semantics.

**Architecture:** Keep each screen's existing presentation model and workflow inputs. Add small screen-level formatting helpers around `components.Table`, then migrate Provider, Config policy, Resources, Machines, and Restore views independently. Root owns the decision to route Tab to screens that visibly expose State/Capture/Restore; screens continue to own their local tab state.

**Tech Stack:** Go, Bubble Tea v2, Lip Gloss v2, existing `internal/tui/components.Table` and screen render tests.

**Spec:** User-provided PR7.5 brief in this conversation.

## Global Constraints

- Presentation only: no schema, workflow policy, provider identity, planning, verification, capture, restore, or CLI behavior changes.
- Config State stays category-specific and is the visual reference.
- No universal domain/table abstraction and no PR8 Capture review or Overview redesign.
- Safety decisions and desired absence/removal remain visible at narrow widths and without colour.
- Use literal, behavior-level assertions and watch each new regression fail before implementation.

## Interfaces

- Task 1 produces a root `Tab`-ownership interface consumed by Provider, Config, and Resources adapters.
- Task 2 produces small policy/state cell and responsive-column helpers consumed by Tasks 3 and 4.
- Task 3 preserves Provider selection/group row identity consumed by Details and policy mutations.
- Task 4 preserves Config/Resources selection and hierarchy consumed by their Details and mutations.
- Task 5 changes only Machines rendering; `PolicyNavigation` remains unchanged.
- Task 6 changes only Restore rendering; pending-plan and fresh-replan behavior remain unchanged.
- Task 7 consumes all prior views for consistency, responsive, and manual verification.

## Task 1: Give Visible Screen Tabs Root-Level Ownership

**Files:**
- Modify: `internal/tui/keys.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/screen_adapters.go`
- Test: `internal/tui/model_test.go`

1. Add root-model regressions that open Config and a generic Provider with Details focused, press Tab twice, and observe State → Capture → Restore while root focus stays put. Add an un-tabbed-screen regression proving Tab still cycles root focus.
2. Run `go test ./internal/tui -run 'Test.*Tab' -count=1`.
   Expected: new tabbed-screen tests fail because root focus consumes Tab; un-tabbed behavior passes.
3. Introduce the smallest root-facing capability for visible local tabs and route Tab/Shift+Tab to it before focus fallback, independent of current pane focus. Implement it for Provider, Config, and Resources only.
4. Run the focused tests again.
   Expected: PASS.
5. Commit: `fix: give screen tabs ownership of tab navigation`.

## Task 2: Add Shared Presentation Vocabulary and Responsive Helpers

**Files:**
- Create: `internal/tui/screens/presentation.go`
- Test: `internal/tui/screens/presentation_test.go`
- Modify if required: `internal/tui/components/table.go`
- Test if required: `internal/tui/components/table_test.go`

1. Add table-driven tests for user-facing desired/current labels, State/Capture/Restore decisions, concise policy sources (Default/Profile/This machine/Parent/Safety), blocked status, semantic styling in colour and readable text without colour, and responsive wide/medium/narrow column sets.
2. Run `go test ./internal/tui/screens -run 'TestPresentation' -count=1`.
   Expected: FAIL because helpers do not exist.
3. Implement small value/style/column helpers around `components.Table`; do not add a domain abstraction.
4. Run focused screen and component tests.
   Expected: PASS.
5. Commit: `feat: add coherent tui table presentation helpers`.

## Task 3: Render Generic Provider Categories as Structured Tables

**Files:**
- Modify: `internal/tui/screens/provider.go`
- Modify: `internal/tui/screens/provider_test.go`

1. Add representative Packages, Themes, Defaults, and Hooks render regressions asserting group headings, semantic column headings and cells, Blocked, desired absence, concise source labels, and no prose `item — desired ...` rows. Cover no-colour output and widths 140, 100, and 80.
2. Run `go test ./internal/tui/screens -run 'TestProvider' -count=1`.
   Expected: new table/render tests FAIL against prose lists.
3. Keep existing target/group/selection identity but render State with ITEM/DESIRED/CURRENT/STATUS and policy tabs with ITEM/current-or-desired/decision/source, degrading to decision-critical compact columns at narrow width. Preserve category grouping, collapse, mutation, Details, and Installed plugins enablement explanation.
4. Run all Provider tests.
   Expected: PASS.
5. Commit: `fix: present provider state and policy as tables`.

## Task 4: Align Config Policy and Resources With the Table Grammar

**Files:**
- Modify: `internal/tui/screens/config.go`
- Modify: `internal/tui/screens/config_test.go`
- Modify: `internal/tui/screens/resources.go`
- Modify: `internal/tui/screens/resources_test.go`

1. Add Config tests proving State's Path/State/Policy structure remains, Capture/Restore use PATH/decision/SOURCE tables, directory hierarchy and expansion survive, descendant override counts remain visible, and widths 140/100/80 retain the policy decision. Add Resources State and policy table tests plus the Exact-never-deletes warning at all widths.
2. Run `go test ./internal/tui/screens -run 'Test(Config|Resource)' -count=1`.
   Expected: new policy table assertions FAIL while existing Config State assertions remain green.
3. Migrate Config policy rows and Resources tabs to `components.Table` using shared presentation helpers, preserving all navigation, hierarchy, discovery, mutations, Details, and the Resource deletion guarantee.
4. Run the focused tests.
   Expected: PASS.
5. Commit: `fix: align config and resources policy tables`.

## Task 5: Make Machines Scannable Without Becoming a Policy Editor

**Files:**
- Modify: `internal/tui/screens/machines.go`
- Modify: `internal/tui/screens/machines_test.go`

1. Add render tests asserting MACHINE/ACTIVE/RESTORE DEFAULT/OVERRIDES information is vertically scannable, resource mappings remain structured, category override navigation remains visible, and narrow output retains active/default/override meaning.
2. Run `go test ./internal/tui/screens -run 'TestMachines' -count=1`.
   Expected: new machine-table tests FAIL against the prose/list summary.
3. Render machine/default/override facts as concise structured rows while preserving resource mapping and `PolicyNavigation` behavior.
4. Run Machines tests.
   Expected: PASS.
5. Commit: `fix: present machine defaults and overrides coherently`.

## Task 6: Structure the Current Restore Plan Without Weakening Authority

**Files:**
- Modify: `internal/tui/screens/restore.go`
- Modify: `internal/tui/screens/restore_test.go`

1. Add tests for Changes CATEGORY/TARGET/ACTION/RISK, skipped CATEGORY/TARGET/REASON, visibly destructive removal, Force/Exact settings, responsive 140/100/80 output, and unchanged pending-plan CanApply protection.
2. Run `go test ./internal/tui/screens -run 'TestRestore' -count=1`.
   Expected: new table assertions FAIL; pending-plan regressions remain green.
3. Render the plan using separate operation and skip tables, semantic action/risk styling, concise counts, and responsive columns. Keep planning requests, request IDs, option selection, confirmation, and apply-time replan untouched.
4. Run Restore tests.
   Expected: PASS.
5. Commit: `fix: make restore plans scannable`.

## Task 7: Consistency, Responsive, and Hands-On Verification

**Files:**
- Modify as needed: affected `internal/tui/screens/*_test.go`
- Modify if warranted: `docs/manual-test-checklist.md`

1. Search affected production views for concatenated em-dash policy prose, internal source-kind output in main panes, and stale product vocabulary. Add a failing regression before any behavioral correction found.
2. Exercise all affected render tests at 140, 100, and 80 columns; confirm no panic, malformed output, or hidden Blocked/Preserve/Skip/desired-absent/removal decisions.
3. Run the complete verification gate:
   - `go test ./internal/tui/... -count=1`
   - `go test ./... -count=1`
   - `go vet ./...`
   - `go build ./cmd/omarchy-blueprint`
   - `git diff --check`
   Expected: all commands succeed.
4. Run the real-profile TUI in a PTY at approximately 140, 100, and 80 columns and walk Packages, Themes, Config, Resources, Machines, and Restore without mutating state. Record hands-on findings in the ledger; fix important issues test-first.
5. Commit any final verified corrections: `fix: polish responsive tui presentation`.

## Review Focus

- Root Tab routing when Details or Sidebar owns focus, including Shift+Tab and compact layout.
- Selection-to-Details and selection-to-mutation identity after group rows and responsive column changes.
- Narrow layouts retaining Blocked, Preserve, Skip, desired absence, and removal signals.
- Policy source wording accurately distinguishing profile, machine, inherited parent, and default.
- Restore rendering remaining presentation-only with pending and fresh-replan guarantees intact.
- Config State remaining unchanged in structure and behavior.
