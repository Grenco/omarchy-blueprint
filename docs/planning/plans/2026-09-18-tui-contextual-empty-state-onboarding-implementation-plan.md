# TUI Contextual Empty-State Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make empty and unconfigured TUI sections explain what they are for, why Blueprint supports them, whether emptiness is normal, and what optional next action exists, without adding a wizard or changing capture/restore/domain semantics.

**Architecture:** Keep onboarding entirely inside the existing TUI presentation layer. Add one small `internal/tui/screens` empty-state presentation helper/catalogue, derive guidance only from state screens already own (`Manifest.Capture`, `ProviderStatus`, Config candidates/filter, Resources, Machines, Profile Git), and wire that helper into existing screen `View` methods without changing workflow APIs or screen interaction state machines.

**Tech Stack:** Go 1.25+, Bubble Tea v2, Lip Gloss v2, existing `internal/tui`, `internal/tui/screens`, `internal/tui/components`, `internal/workflow`, and `internal/profile`; no new runtime or test dependencies.

**Spec:** `docs/planning/specs/2026-09-18-tui-contextual-empty-state-onboarding-design.md`

**ADR:** `docs/adr/0019-tui-contextual-empty-state-onboarding.md`

## Global Constraints

- Contextual onboarding only: no wizard, onboarding checklist, persisted dismissal state, onboarding-complete flag, tutorial mode, or new profile metadata.
- Empty-state guidance must reassure rather than create homework; optional features must not look incomplete, unhealthy, or required.
- Explain purpose before action: what the feature is, why Blueprint supports it, whether emptiness is normal, then an optional existing next action.
- Never hide useful pre-capture/live information. Capture keeps its full category table; Config keeps discovered candidates/policy controls; Resources keeps Discover; populated saved/difference/restore views remain primary.
- Do not claim knowledge Blueprint does not have. Uncaptured does not mean "nothing found on this machine."
- Distinguish uncaptured state from captured-and-empty state wherever the existing state can prove the difference.
- Use existing user-facing terminology: `category`, `installed theme`, `Installed plugins`, `Omarchy default`, and `differences`.
- Config policy wording remains exact:
  - Auto — `Blueprint decides based on Omarchy defaults, ownership, and safety rules.`
  - Included — `You asked Config to manage this path when it is safe to do so.`
  - Excluded — `You asked Config to leave this path alone.`
  - Detail must still include: `Included never overrides safety rules.`
- Keep guidance in normal workspace content; do not use modals, toasts, new screens, new palette entries, or dismissible banners.
- Existing keybindings, actions, command palette commands, focus behavior, modal precedence, responsive breakpoints, and terminal-too-small behavior remain unchanged.
- Do not change profile schema, `internal/workflow` APIs/semantics, provider semantics, capture semantics, restore semantics, Config classifications/policies, Resources tracking, machine overlays, Profile Git operations, CLI behavior, or JSON behavior.
- Do not add pre-capture candidate preview or an ad hoc TUI-only inspection path. That is the next design effort.
- Reuse `components.WrapText` for prose wrapping; do not add a text-wrapping dependency.
- Avoid brittle full-screen golden tests. Test state-specific copy and preservation of useful content/actions.
- Every implementation task must leave `go test ./internal/tui/... -count=1` green before commit.

---

## Target file structure

Expected final changes:

```text
docs/
├── adr/
│   └── 0019-tui-contextual-empty-state-onboarding.md
└── planning/
    ├── plans/
    │   └── 2026-09-18-tui-contextual-empty-state-onboarding-implementation-plan.md
    └── specs/
        └── 2026-09-18-tui-contextual-empty-state-onboarding-design.md

internal/tui/screens/
├── empty_state.go            # shared presentation-only copy/state helpers
├── empty_state_test.go
├── overview.go
├── overview_test.go
├── capture.go
├── capture_test.go
├── restore.go
├── restore_test.go
├── provider.go
├── provider_test.go
├── config.go
├── config_test.go
├── resources.go
├── resources_test.go
├── machines.go
├── machines_test.go
├── sync.go
└── sync_test.go
```

Do not create a new package. `empty_state.go` is a package-local presentation helper, not a new onboarding subsystem.

---

### Task 1: Add the approved docs and shared empty-state presentation contract

**Files:**
- Create: `docs/planning/specs/2026-09-18-tui-contextual-empty-state-onboarding-design.md`
- Create: `docs/adr/0019-tui-contextual-empty-state-onboarding.md`
- Create: `docs/planning/plans/2026-09-18-tui-contextual-empty-state-onboarding-implementation-plan.md`
- Create: `internal/tui/screens/empty_state.go`
- Create: `internal/tui/screens/empty_state_test.go`

**Interfaces:**
- Consumes: `profile.Data`, `workflow.ProviderStatus`, `components.Styles`, `components.WrapText`.
- Produces:
  - `type emptyStateCopy struct { Heading, Explanation, Guidance string }`
  - `func renderEmptyState(styles components.Styles, width int, copy emptyStateCopy) string`
  - `func profileHasCapturedState(data profile.Data) bool`
  - `func providerEmptyState(id string, status workflow.ProviderStatus, tab string) (emptyStateCopy, bool)`
- These helpers are presentation-only and package-private.

- [ ] **Step 1: Copy the approved spec, ADR, and this plan into the repository**

Use the supplied documents without rewriting their decisions during implementation.

Run:

```sh
test -f docs/planning/specs/2026-09-18-tui-contextual-empty-state-onboarding-design.md
test -f docs/adr/0019-tui-contextual-empty-state-onboarding.md
test -f docs/planning/plans/2026-09-18-tui-contextual-empty-state-onboarding-implementation-plan.md
```

Expected: all commands exit 0.

- [ ] **Step 2: Write failing helper tests for capture-state detection and uncaptured-vs-empty copy**

Create `internal/tui/screens/empty_state_test.go`:

```go
package screens

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestProfileHasCapturedState(t *testing.T) {
	fresh := profile.New("fresh", time.Unix(0, 0))
	if profileHasCapturedState(fresh) {
		t.Fatal("fresh profile reported captured state")
	}

	fresh.Manifest.Capture.Hooks = true
	if !profileHasCapturedState(fresh) {
		t.Fatal("captured hooks were not detected")
	}
}

func TestProviderEmptyStateDistinguishesUncapturedFromKnownEmptyHooks(t *testing.T) {
	uncaptured, ok := providerEmptyState("hooks", workflow.ProviderStatus{ID: "hooks"}, "Saved")
	if !ok || uncaptured.Heading != "Hooks are not saved in this profile yet" {
		t.Fatalf("uncaptured hooks copy=%#v ok=%v", uncaptured, ok)
	}
	if strings.Contains(strings.ToLower(uncaptured.Explanation+" "+uncaptured.Guidance), "no hooks found") {
		t.Fatalf("uncaptured copy claims live inspection: %#v", uncaptured)
	}

	empty, ok := providerEmptyState("hooks", workflow.ProviderStatus{
		ID:       "hooks",
		Captured: true,
		Snapshot: profile.Hooks{},
	}, "Saved")
	if !ok || empty.Heading != "No hooks to carry" {
		t.Fatalf("captured-empty hooks copy=%#v ok=%v", empty, ok)
	}
	if !strings.Contains(empty.Guidance, "completely normal") {
		t.Fatalf("known-empty hooks do not reassure: %#v", empty)
	}
}

func TestProviderEmptyStateDoesNotReplaceCapturedChangesTab(t *testing.T) {
	_, ok := providerEmptyState("hooks", workflow.ProviderStatus{
		ID:       "hooks",
		Captured: true,
		Snapshot: profile.Hooks{},
	}, "Changes")
	if ok {
		t.Fatal("captured Changes tab must keep normal difference presentation")
	}
}

func TestRenderEmptyStateWrapsToWorkspaceWidth(t *testing.T) {
	view := renderEmptyState(
		components.NewStyles(components.ThemePalette{}),
		32,
		emptyStateCopy{
			Heading:     "No hooks to carry",
			Explanation: "Hooks are scripts Omarchy runs automatically at supported events.",
			Guidance:    "Having no hooks is completely normal. There is nothing to set up here.",
		},
	)
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 32 {
			t.Fatalf("line width=%d > 32: %q", lipgloss.Width(line), line)
		}
	}
}
```

- [ ] **Step 3: Run the new tests and verify they fail because the helpers do not exist**

Run:

```sh
go test ./internal/tui/screens -run 'Test(ProfileHasCapturedState|ProviderEmptyState|RenderEmptyState)' -count=1
```

Expected: FAIL with undefined helper/type errors.

- [ ] **Step 4: Implement the shared renderer and profile capture-state helper**

Create `internal/tui/screens/empty_state.go`:

```go
package screens

import (
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type emptyStateCopy struct {
	Heading     string
	Explanation string
	Guidance    string
}

func renderEmptyState(styles components.Styles, width int, copy emptyStateCopy) string {
	if width <= 0 {
		width = 80
	}
	heading := components.WrapText(copy.Heading, width)
	for i := range heading {
		heading[i] = styles.Accent(heading[i])
	}
	lines := append([]string(nil), heading...)
	for _, paragraph := range []string{copy.Explanation, copy.Guidance} {
		if paragraph == "" {
			continue
		}
		lines = append(lines, "", strings.Join(components.WrapText(paragraph, width), "\n"))
	}
	return strings.Join(lines, "\n")
}

func profileHasCapturedState(data profile.Data) bool {
	captured := data.Manifest.Capture
	return captured.Packages ||
		captured.Themes ||
		captured.Plugins ||
		captured.Config ||
		captured.Defaults ||
		captured.Shell ||
		captured.Hooks ||
		captured.Resources
}
```

Do not add an onboarding state object or store this result on `model`.

- [ ] **Step 5: Implement category-specific uncaptured and known-empty copy**

Add `providerEmptyState` and semantic empty checks in `empty_state.go`:

```go
func providerEmptyState(id string, status workflow.ProviderStatus, tab string) (emptyStateCopy, bool) {
	if !status.Captured {
		copy, ok := uncapturedProviderCopy(id)
		return copy, ok
	}
	if tab == "Changes" {
		return emptyStateCopy{}, false
	}

	switch value := status.Snapshot.(type) {
	case profile.Packages:
		if len(value.Official) == 0 &&
			len(value.AUR) == 0 &&
			len(value.Mise) == 0 &&
			len(value.MachineSpecific) == 0 &&
			len(value.Excluded) == 0 {
			return emptyStateCopy{
				Heading:     "No packages or tools to carry",
				Explanation: "Packages is where Blueprint remembers portable system packages, AUR packages, and global Mise tools so they can be installed again on another machine.",
				Guidance:    "If this profile intentionally has no portable packages or tools, there is nothing to do here.",
			}, true
		}
	case profile.Themes:
		if value.Current == "" && len(value.Items) == 0 {
			return emptyStateCopy{
				Heading:     "No themes to carry",
				Explanation: "Themes lets Blueprint remember your selected Omarchy theme and reconstruct installed or local themes when needed.",
				Guidance:    "If there is no theme state Blueprint needs to carry, this section can stay empty.",
			}, true
		}
	case profile.Plugins:
		if len(value.Items) == 0 {
			return emptyStateCopy{
				Heading:     "No plugins to carry",
				Explanation: "Plugins are Omarchy extensions Blueprint can reconstruct, including installed and local plugins.",
				Guidance:    "Having no plugins is completely normal. There is nothing to configure here.",
			}, true
		}
	case profile.Shell:
		if value.Hash == "" {
			return emptyStateCopy{
				Heading:     "No Blueprint-managed Shell customisation",
				Explanation: "Shell is for supported customisations to the Omarchy shell, bar, and layout.",
				Guidance:    "If you use the normal Omarchy shell setup here, there is no extra Shell state to carry.",
			}, true
		}
	case profile.Hooks:
		if len(value.Items) == 0 {
			return emptyStateCopy{
				Heading:     "No hooks to carry",
				Explanation: "Hooks are scripts Omarchy runs automatically at supported events. Blueprint can carry those scripts so the same automation is available when you rebuild another machine.",
				Guidance:    "Having no hooks is completely normal. There is nothing to set up here.",
			}, true
		}
	}

	return emptyStateCopy{}, false
}

func uncapturedProviderCopy(id string) (emptyStateCopy, bool) {
	switch id {
	case "packages":
		return emptyStateCopy{
			Heading:     "Packages are not saved in this profile yet",
			Explanation: "Packages is where Blueprint remembers portable system packages, AUR packages, and global Mise tools so they can be installed again on another machine.",
			Guidance:    "Capture Packages when you want that software set to become part of the profile.",
		}, true
	case "themes":
		return emptyStateCopy{
			Heading:     "Themes are not saved in this profile yet",
			Explanation: "Themes lets Blueprint remember your selected Omarchy theme and reconstruct installed or local themes when needed.",
			Guidance:    "Capture Themes when you want that appearance state to become part of the profile.",
		}, true
	case "plugins":
		return emptyStateCopy{
			Heading:     "Plugins are not saved in this profile yet",
			Explanation: "Plugins are Omarchy extensions Blueprint can reconstruct, including installed and local plugins.",
			Guidance:    "Capture Plugins when you want those extensions to become part of the profile.",
		}, true
	case "defaults":
		return emptyStateCopy{
			Heading:     "Defaults are not saved in this profile yet",
			Explanation: "Defaults records the terminal, browser, editor, and agent choices Omarchy knows about.",
			Guidance:    "Capture Defaults when you want Blueprint to remember those selections.",
		}, true
	case "shell":
		return emptyStateCopy{
			Heading:     "Shell state is not saved in this profile yet",
			Explanation: "Shell is for supported customisations to the Omarchy shell, bar, and layout.",
			Guidance:    "Capture Shell when you want Blueprint to remember those changes.",
		}, true
	case "hooks":
		return emptyStateCopy{
			Heading:     "Hooks are not saved in this profile yet",
			Explanation: "Hooks are scripts Omarchy runs automatically at supported events. Blueprint can carry those scripts so the same automation is available when you rebuild another machine.",
			Guidance:    "Capture Hooks only when you want Blueprint to remember them.",
		}, true
	default:
		return emptyStateCopy{}, false
	}
}
```

Do not include Resources here; it has a dedicated screen.

Do not add a captured-empty tutorial for Defaults. Its concrete rows remain the normal representation once captured.

- [ ] **Step 6: Run helper tests**

Run:

```sh
go test ./internal/tui/screens -run 'Test(ProfileHasCapturedState|ProviderEmptyState|RenderEmptyState)' -count=1
```

Expected: PASS.

- [ ] **Step 7: Run the complete TUI package tests**

Run:

```sh
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit the docs and shared presentation contract**

```sh
git add \
  docs/planning/specs/2026-09-18-tui-contextual-empty-state-onboarding-design.md \
  docs/adr/0019-tui-contextual-empty-state-onboarding.md \
  docs/planning/plans/2026-09-18-tui-contextual-empty-state-onboarding-implementation-plan.md \
  internal/tui/screens/empty_state.go \
  internal/tui/screens/empty_state_test.go
git commit -m "feat: add contextual empty-state guidance contract"
```

---

### Task 2: Add fresh-profile guidance to Overview, Capture, and Restore

**Files:**
- Modify: `internal/tui/screens/overview.go`
- Modify: `internal/tui/screens/overview_test.go`
- Modify: `internal/tui/screens/capture.go`
- Modify: `internal/tui/screens/capture_test.go`
- Modify: `internal/tui/screens/restore.go`
- Modify: `internal/tui/screens/restore_test.go`

**Interfaces:**
- Consumes: `profileHasCapturedState`, `renderEmptyState`.
- Produces no new public interface.
- Capture determines fresh-profile state from its existing `[]workflow.ProviderStatus`; it must not request new workflow data.

- [ ] **Step 1: Add failing Overview tests for a completely fresh profile and the first captured category**

Add to `overview_test.go`:

```go
func TestOverviewExplainsCompletelyUncapturedProfile(t *testing.T) {
	session, _ := newSyncSession(t)
	screen := NewOverview(session)

	view := screen.View()
	for _, want := range []string{
		"Nothing has been captured yet",
		"Capture is where you choose which parts of this machine Blueprint should remember.",
		"open Capture",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh overview missing %q:\n%s", want, view)
		}
	}
}

func TestOverviewFreshProfileGuidanceDisappearsAfterCapture(t *testing.T) {
	session, _ := newSyncSession(t)
	if err := session.SetProviderCaptured(context.Background(), "hooks", true); err != nil {
		t.Fatal(err)
	}
	screen := NewOverview(session)

	if view := screen.View(); strings.Contains(view, "Nothing has been captured yet") {
		t.Fatalf("fresh-profile guidance remained after capture:\n%s", view)
	}
}
```

Add `context` to the imports.

- [ ] **Step 2: Run the Overview tests and verify the fresh-profile assertions fail**

Run:

```sh
go test ./internal/tui/screens -run 'TestOverview(ExplainsCompletelyUncapturedProfile|FreshProfileGuidanceDisappearsAfterCapture)' -count=1
```

Expected: first test FAIL because the orientation copy is absent.

- [ ] **Step 3: Implement the Overview fresh-profile branch**

At the start of `Overview.View()`, after the existing error check and before row rendering:

```go
if s.session != nil && !profileHasCapturedState(s.session.Profile()) {
	return renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "Nothing has been captured yet",
		Explanation: "Capture is where you choose which parts of this machine Blueprint should remember. You can still explore the other sections to understand what each category covers.",
		Guidance:    "Next: open Capture when you're ready to save something.",
	})
}
```

Do not add a selectable row or an `OverviewTarget`; this is orientation content only.

- [ ] **Step 4: Add failing Capture tests proving guidance does not replace the category table**

Add to `capture_test.go`:

```go
func TestCaptureFreshProfileAddsGuidanceWithoutHidingCategories(t *testing.T) {
	screen := &Capture{
		width: 80,
		statuses: []workflow.ProviderStatus{
			{ID: "packages"},
			{ID: "themes"},
			{ID: "hooks"},
		},
		chosen: map[string]bool{},
	}

	view := screen.View()
	for _, want := range []string{
		"Choose what this profile should remember",
		"you do not need to capture everything",
		"Category",
		"packages",
		"themes",
		"hooks",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh Capture missing %q:\n%s", want, view)
		}
	}
}

func TestCaptureFreshProfileGuidanceDisappearsOnceAnyCategoryIsCaptured(t *testing.T) {
	screen := &Capture{
		statuses: []workflow.ProviderStatus{
			{ID: "packages", Captured: true},
			{ID: "themes"},
		},
		chosen: map[string]bool{},
	}

	if view := screen.View(); strings.Contains(view, "Choose what this profile should remember") {
		t.Fatalf("fresh hint remained after capture:\n%s", view)
	}
}
```

- [ ] **Step 5: Run the new Capture tests and verify they fail**

Run:

```sh
go test ./internal/tui/screens -run 'TestCaptureFreshProfile' -count=1
```

Expected: FAIL because the new hint is absent.

- [ ] **Step 6: Add the Capture hint while preserving the existing table**

In `Capture.View()`, after error/capturing/busy handling and before building rows:

```go
lines := []string{}
if len(s.statuses) > 0 {
	anyCaptured := false
	for _, status := range s.statuses {
		if status.Captured {
			anyCaptured = true
			break
		}
	}
	if !anyCaptured {
		hintWidth := s.width
		if hintWidth <= 0 || hintWidth > 60 {
			hintWidth = 60
		}
		lines = append(lines, renderEmptyState(s.styles, hintWidth, emptyStateCopy{
			Heading:     "Choose what this profile should remember",
			Explanation: "Nothing has been captured yet. Select the categories you want in this profile; you do not need to capture everything.",
		}), "")
	}
}
```

Render the current table exactly as before and append it to `lines`:

```go
table := s.table.Render(
	[]components.Column{
		{Title: "", MinWidth: 3},
		{Title: "Category", MinWidth: 12},
		{Title: "Changes", MinWidth: 8},
	},
	rows,
	60,
	max(2, len(rows)+1),
	s.styles,
)
lines = append(lines, table)
return strings.Join(lines, "\n")
```

Add `strings` to `capture.go` imports if not already present.

Do not change category selection, `a`, `c`, `C`, refresh, confirmation, or capture calls.

- [ ] **Step 7: Add failing Restore tests distinguishing uncaptured from captured-and-clean**

Add to `restore_test.go`:

```go
func TestRestoreExplainsWhenNothingHasEverBeenCaptured(t *testing.T) {
	session, _ := newSyncSession(t)
	screen := NewRestore(session)

	view := screen.View()
	for _, want := range []string{
		"Nothing to restore yet",
		"Restore recreates state that is already saved",
		"Capture the parts of this machine",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("fresh Restore missing %q:\n%s", want, view)
		}
	}
}

func TestRestoreCapturedButCleanKeepsNormalZeroOperationState(t *testing.T) {
	session, _ := newSyncSession(t)
	if err := session.SetProviderCaptured(context.Background(), "hooks", true); err != nil {
		t.Fatal(err)
	}
	screen := NewRestore(session)

	view := screen.View()
	if strings.Contains(view, "Nothing to restore yet") {
		t.Fatalf("captured clean profile shown as uncaptured:\n%s", view)
	}
	if !strings.Contains(view, "No restore operations required.") {
		t.Fatalf("captured clean restore lost normal zero-state:\n%s", view)
	}
}
```

Add `context` to imports.

- [ ] **Step 8: Run the Restore tests and verify the first fails**

Run:

```sh
go test ./internal/tui/screens -run 'TestRestore(ExplainsWhenNothingHasEverBeenCaptured|CapturedButCleanKeepsNormalZeroOperationState)' -count=1
```

Expected: first test FAIL before implementation.

- [ ] **Step 9: Implement the Restore fresh-profile branch**

In `Restore.View()`, after error/busy/diff handling and before mode/count rendering:

```go
if s.session != nil && !profileHasCapturedState(s.session.Profile()) {
	return renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "Nothing to restore yet",
		Explanation: "Restore recreates state that is already saved in this Blueprint profile.",
		Guidance:    "Capture the parts of this machine you want Blueprint to remember before using Restore.",
	})
}
```

Do not alter `compare()`, `apply()`, Normal/Forced mode, consequence rendering, or confirmation.

- [ ] **Step 10: Run targeted and full TUI tests**

Run:

```sh
go test ./internal/tui/screens -run 'Test(Overview|Capture|Restore)' -count=1
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit the workflow-screen guidance**

```sh
git add \
  internal/tui/screens/overview.go \
  internal/tui/screens/overview_test.go \
  internal/tui/screens/capture.go \
  internal/tui/screens/capture_test.go \
  internal/tui/screens/restore.go \
  internal/tui/screens/restore_test.go
git commit -m "feat: guide fresh profile workflows"
```

---

### Task 3: Replace generic category ambiguity with category-specific uncaptured and known-empty guidance

**Files:**
- Modify: `internal/tui/screens/provider.go`
- Modify: `internal/tui/screens/provider_test.go`
- Modify: `internal/tui/screens/empty_state_test.go`

**Interfaces:**
- Consumes: `providerEmptyState`, `renderEmptyState`.
- Existing tabs, capture key, item toggles, grouping, and `ProviderStatus` semantics remain unchanged.

- [ ] **Step 1: Add failing provider view tests**

Add to `provider_test.go`:

```go
func TestProviderUncapturedHooksExplainsPurposeWithoutClaimingKnownEmpty(t *testing.T) {
	screen := &Provider{
		id:     "hooks",
		width:  80,
		status: workflow.ProviderStatus{ID: "hooks"},
	}

	view := screen.View()
	for _, want := range []string{
		"Hooks are not saved in this profile yet",
		"scripts Omarchy runs automatically",
		"Capture Hooks only when you want Blueprint to remember them.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("uncaptured hooks missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "No hooks to carry") {
		t.Fatalf("uncaptured hooks were presented as known-empty:\n%s", view)
	}
}

func TestProviderCapturedEmptyHooksReassuresUser(t *testing.T) {
	screen := &Provider{
		id:    "hooks",
		width: 80,
		status: workflow.ProviderStatus{
			ID:       "hooks",
			Captured: true,
			Snapshot: profile.Hooks{},
		},
	}

	view := screen.View()
	for _, want := range []string{
		"No hooks to carry",
		"same automation is available",
		"completely normal",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("captured-empty hooks missing %q:\n%s", want, view)
		}
	}
}

func TestProviderUncapturedChangesTabDoesNotClaimNoDifferences(t *testing.T) {
	screen := &Provider{
		id:     "plugins",
		tab:    "Changes",
		width:  80,
		status: workflow.ProviderStatus{ID: "plugins"},
	}

	view := screen.View()
	if !strings.Contains(view, "Plugins are not saved in this profile yet") {
		t.Fatalf("uncaptured Changes tab lost guidance:\n%s", view)
	}
	if strings.Contains(view, "No differences detected") {
		t.Fatalf("uncaptured category falsely claims a completed diff:\n%s", view)
	}
}

func TestProviderCapturedEmptyShellUsesShellZeroState(t *testing.T) {
	screen := &Provider{
		id:    "shell",
		width: 80,
		status: workflow.ProviderStatus{
			ID:       "shell",
			Captured: true,
			Snapshot: profile.Shell{Version: 1},
		},
	}

	view := screen.View()
	if !strings.Contains(view, "No Blueprint-managed Shell customisation") ||
		!strings.Contains(view, "normal Omarchy shell setup") {
		t.Fatalf("shell zero-state missing:\n%s", view)
	}
}

func TestProviderCapturedEmptyDefaultsKeepsConcreteRows(t *testing.T) {
	screen := &Provider{
		id:    "defaults",
		width: 80,
		status: workflow.ProviderStatus{
			ID:       "defaults",
			Captured: true,
			Snapshot: profile.Defaults{},
		},
	}

	view := screen.View()
	if strings.Contains(view, "nothing to do here") {
		t.Fatalf("Defaults received a generic captured-empty tutorial:\n%s", view)
	}
	if !strings.Contains(view, "Applications") || !strings.Contains(view, "Terminal:") {
		t.Fatalf("Defaults concrete rows disappeared:\n%s", view)
	}
}
```

- [ ] **Step 2: Run the provider tests and verify the new cases fail**

Run:

```sh
go test ./internal/tui/screens -run 'TestProvider(Uncaptured|CapturedEmpty)' -count=1
```

Expected: FAIL because `Provider.View()` still uses generic "No saved state yet" / "No differences detected" messaging.

- [ ] **Step 3: Wire `providerEmptyState` into `Provider.View()`**

Keep the tab bar and package-selection legend exactly as they are.

After appending `"Loading..."` when busy, but before appending `"No saved state yet."` or building fallback rows, add:

```go
if !s.busy {
	if copy, ok := providerEmptyState(s.id, s.status, s.activeTab()); ok {
		lines = append(lines, renderEmptyState(s.styles, s.width, copy))
		return strings.Join(lines, "\n")
	}
}
```

Delete the generic:

```go
if !s.status.Captured {
	lines = append(lines, "No saved state yet.")
}
```

Change `emptyTabMessage` so it no longer attempts to explain uncaptured categories. Its responsibility becomes captured normal-tab emptiness only:

```go
func emptyTabMessage(tab string, captured bool) string {
	if !captured {
		return ""
	}
	if tab == "Changes" {
		return "✓ No differences detected."
	}
	return "No saved items."
}
```

When `rows` is empty, only append a fallback row when the returned message is non-empty:

```go
rows := s.rows()
if len(rows) == 0 {
	if message := emptyTabMessage(s.tab, s.status.Captured); message != "" {
		rows = []providerRow{{value: message}}
	}
}
```

The uncaptured path should already have returned through `providerEmptyState`.

Do not change `refresh()`, capture/toggle operations, tabs, group collapse, or selection.

- [ ] **Step 4: Expand helper tests to cover every shipped generic category copy**

Add a table-driven test in `empty_state_test.go`:

```go
func TestUncapturedProviderCopyCoversGenericCategoryScreens(t *testing.T) {
	tests := map[string]string{
		"packages": "Packages are not saved in this profile yet",
		"themes":   "Themes are not saved in this profile yet",
		"plugins":  "Plugins are not saved in this profile yet",
		"defaults": "Defaults are not saved in this profile yet",
		"shell":    "Shell state is not saved in this profile yet",
		"hooks":    "Hooks are not saved in this profile yet",
	}
	for id, heading := range tests {
		copy, ok := uncapturedProviderCopy(id)
		if !ok || copy.Heading != heading {
			t.Errorf("%s copy=%#v ok=%v", id, copy, ok)
		}
	}
}
```

Add captured-empty semantic tests for Packages/Themes/Plugins/Hooks/Shell and verify populated values return `ok=false`.

- [ ] **Step 5: Run targeted and complete screen tests**

Run:

```sh
go test ./internal/tui/screens -run 'Test(Provider|UncapturedProviderCopy)' -count=1
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit category guidance**

```sh
git add \
  internal/tui/screens/provider.go \
  internal/tui/screens/provider_test.go \
  internal/tui/screens/empty_state_test.go
git commit -m "feat: explain empty category states"
```

---

### Task 4: Make Config and Resources explain legitimate emptiness without hiding live controls

**Files:**
- Modify: `internal/tui/screens/config.go`
- Modify: `internal/tui/screens/config_test.go`
- Modify: `internal/tui/screens/resources.go`
- Modify: `internal/tui/screens/resources_test.go`

**Interfaces:**
- Consumes: `renderEmptyState`.
- Config must distinguish `len(s.candidates) == 0` from `len(s.rows()) == 0` caused by an active filter.
- Resources guidance applies only to the Tracked view when `len(s.items) == 0`.

- [ ] **Step 1: Add failing Config tests for true-empty versus filter-empty**

Add to `config_test.go`:

```go
func TestConfigEmptyCandidatesExplainPurpose(t *testing.T) {
	screen := &Config{width: 80}

	view := screen.View()
	for _, want := range []string{
		"No configuration needs review",
		"dotfiles and application/system configuration",
		"nothing suitable for Config to manage",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty Config missing %q:\n%s", want, view)
		}
	}
}

func TestConfigEmptyFilterDoesNotClaimGlobalEmptyState(t *testing.T) {
	screen := &Config{
		width:  80,
		filter: "does-not-match",
		candidates: []config.Candidate{
			{Path: ".config/nvim/init.lua", Classification: config.ConfigAdded},
		},
	}

	view := screen.View()
	if !strings.Contains(view, "No configuration matches this filter.") {
		t.Fatalf("filter-empty message missing:\n%s", view)
	}
	if strings.Contains(view, "nothing suitable for Config to manage") {
		t.Fatalf("filter-empty view claimed globally empty Config:\n%s", view)
	}
}
```

- [ ] **Step 2: Run the Config tests and verify they fail**

Run:

```sh
go test ./internal/tui/screens -run 'TestConfig(EmptyCandidates|EmptyFilter)' -count=1
```

Expected: FAIL because current zero rows always render the bare global message.

- [ ] **Step 3: Implement Config's two empty-state branches**

In `Config.View()`, after `rows := s.rows()` and before table construction:

```go
if len(s.candidates) == 0 {
	lines = append(lines, renderEmptyState(s.styles, s.widthOrDefault(), emptyStateCopy{
		Heading:     "No configuration needs review",
		Explanation: "Config is for dotfiles and application/system configuration that differs meaningfully from Omarchy defaults.",
		Guidance:    "If there is nothing suitable for Config to manage, there is nothing to do here.",
	}))
	if s.filtering || s.filter != "" {
		lines = append(lines, "Filter: "+s.filter)
	}
	return strings.Join(lines, "\n")
}

if len(rows) == 0 && s.filter != "" {
	lines = append(lines, "No configuration matches this filter.")
	if s.filtering || s.filter != "" {
		lines = append(lines, "Filter: "+s.filter)
	}
	return strings.Join(lines, "\n")
}
```

Then remove the later fallback:

```go
if len(tableRows) == 0 {
	lines = append(lines, "✓ No configuration needs review.")
}
```

The normal populated table path remains unchanged.

Do not touch `configPresentationFor`, policy state, inspection, diff, filtering input behavior, handoff, or policy mutations.

- [ ] **Step 4: Add failing Resources tests**

Add to `resources_test.go`:

```go
func TestResourcesEmptyTrackedViewExplainsOptionalPurpose(t *testing.T) {
	screen := &Resources{
		width: 80,
		phase: resourceBrowse,
		items: nil,
	}

	view := screen.View()
	for _, want := range []string{
		"No extra resources tracked",
		"files, folders, or Git projects",
		"do not fit one of its normal categories",
		"Press Tab to Discover one.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty Resources missing %q:\n%s", want, view)
		}
	}
}

func TestResourcesDiscoverDoesNotShowTrackedEmptyGuidance(t *testing.T) {
	screen := &Resources{
		width:    80,
		phase:    resourceBrowse,
		discover: true,
	}

	view := screen.View()
	if strings.Contains(view, "No extra resources tracked") {
		t.Fatalf("Tracked empty-state leaked into Discover:\n%s", view)
	}
	if !strings.Contains(view, "Discover") {
		t.Fatalf("Discover tab missing:\n%s", view)
	}
}
```

- [ ] **Step 5: Run the Resources tests and verify the first fails**

Run:

```sh
go test ./internal/tui/screens -run 'TestResources(EmptyTrackedView|DiscoverDoesNotShowTrackedEmptyGuidance)' -count=1
```

Expected: first test FAIL because current screen renders a one-cell `"No tracked resources."` table row.

- [ ] **Step 6: Replace only the Tracked empty table with contextual guidance**

In `Resources.View()`, keep the tab bar and any error line.

After:

```go
if s.discover {
	return strings.Join(lines, "\n")
}
```

add:

```go
if len(s.items) == 0 {
	lines = append(lines, renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "No extra resources tracked",
		Explanation: "Resources are files, folders, or Git projects you want Blueprint to carry when they do not fit one of its normal categories.",
		Guidance:    "You only need this when there is something extra you want to reconstruct. Press Tab to Discover one.",
	}))
	return strings.Join(lines, "\n")
}
```

Delete the later synthetic row:

```go
if len(s.items) == 0 {
	rows = append(rows, components.Row{Cells: []string{"No tracked resources."}})
}
```

Do not alter Discover, browser, tracking strategy, untracked selection, or tracked Resource actions.

- [ ] **Step 7: Run targeted and complete TUI tests**

Run:

```sh
go test ./internal/tui/screens -run 'Test(Config|Resource)' -count=1
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit Config and Resources onboarding**

```sh
git add \
  internal/tui/screens/config.go \
  internal/tui/screens/config_test.go \
  internal/tui/screens/resources.go \
  internal/tui/screens/resources_test.go
git commit -m "feat: explain optional config and resources states"
```

---

### Task 5: Explain Machines and Sync only when their optional capability is actually unconfigured

**Files:**
- Modify: `internal/tui/screens/machines.go`
- Modify: `internal/tui/screens/machines_test.go`
- Modify: `internal/tui/screens/sync.go`
- Modify: `internal/tui/screens/sync_test.go`

**Interfaces:**
- Consumes: `renderEmptyState`.
- Machines derives context from `s.session.Profile().Resources.Items` and existing machine overlays.
- Sync derives context from existing `profilegit.Status.Repository`.
- Existing actions/keybindings stay authoritative.

- [ ] **Step 1: Add failing Machines tests for no Resources and portable-path-only state**

Add to `machines_test.go`:

```go
func TestMachinesWithoutResourcesExplainsWhyScreenIsEmpty(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(
		workflow.Dependencies{
			StateHome: func() (string, error) { return stateHome, nil },
			Hostname:  func() (string, error) { return "desktop", nil },
		},
		workflow.Options{ProfileDir: profileDir},
	)
	if err != nil {
		t.Fatal(err)
	}

	view := NewMachines(session).View()
	for _, want := range []string{
		"No machine-specific paths needed",
		"only matters when a Resource needs a different location",
		"There is nothing to configure on this screen.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty Machines missing %q:\n%s", want, view)
		}
	}
}

func TestMachinesWithPortableResourcesExplainsOverridesWithoutHidingResource(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Resources.Items = []profile.Resource{
		{ID: "projects", Path: "~/Projects"},
	}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := workflow.Open(
		workflow.Dependencies{
			StateHome: func() (string, error) { return stateHome, nil },
			Hostname:  func() (string, error) { return "desktop", nil },
		},
		workflow.Options{ProfileDir: profileDir},
	)
	if err != nil {
		t.Fatal(err)
	}

	view := NewMachines(session).View()
	for _, want := range []string{
		"Portable paths are in use",
		"machine-specific mapping only when one computer needs a different location",
		"projects",
		"~/Projects",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("portable Machines missing %q:\n%s", want, view)
		}
	}
}
```

The second test intentionally requires the existing Resource row to remain visible. This enforces "guidance supplements useful content; it does not replace it."

- [ ] **Step 2: Run Machines tests and verify both new expectations fail**

Run:

```sh
go test ./internal/tui/screens -run 'TestMachines(WithoutResources|WithPortableResources)' -count=1
```

Expected: FAIL because current screen only renders empty pane placeholders / normal rows.

- [ ] **Step 3: Implement Machines context without changing mapping semantics**

At the start of `Machines.View()`, after error/browser/edit-mode handling:

```go
if s.session != nil && len(s.session.Profile().Resources.Items) == 0 {
	return renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "No machine-specific paths needed",
		Explanation: "Machines only matters when a Resource needs a different location on one computer. There are no Resources here that need mapping yet.",
		Guidance:    "There is nothing to configure on this screen.",
	})
}
```

For Resources that exist but no machine has any `ResourcePaths`, create a boolean:

```go
hasOverrides := false
if s.session != nil {
	for _, machine := range s.session.Profile().Machines.Items {
		if len(machine.ResourcePaths) > 0 {
			hasOverrides = true
			break
		}
	}
}
```

Build the existing two-pane output exactly as today. Before returning it, prepend:

```go
if !hasOverrides {
	guidance := renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "Portable paths are in use",
		Explanation: "Resources normally use the same portable path on every machine. Add a machine-specific mapping only when one computer needs a different location.",
		Guidance:    "If the normal Resource paths work here, there is nothing to configure.",
	})
	return guidance + "\n\n" + existingView
}
return existingView
```

Refactor the final joined pane output into a local `existingView` variable if needed. Do not change `mappingRows`, Add/Use/Rename/Remove/Map actions, selection, active-machine logic, or dormant mapping behavior.

- [ ] **Step 4: Add failing Sync tests for optional Git onboarding and normal clean repo**

Add to `sync_test.go`:

```go
func TestSyncNonRepositoryExplainsOptionalProfileGit(t *testing.T) {
	screen := &Sync{
		width:  80,
		status: profilegit.Status{},
	}

	view := screen.View()
	for _, want := range []string{
		"Profile Git is not set up",
		"Sync is optional.",
		"version it and share it between machines",
		"Press i to initialize a repository",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("non-repo Sync missing %q:\n%s", want, view)
		}
	}
	if action := syncActionsByID(screen.Actions())["sync.init"]; !action.Enabled {
		t.Fatalf("initialize action disabled in matching empty state: %#v", action)
	}
}

func TestSyncCleanRepositoryKeepsNormalRepositoryStatus(t *testing.T) {
	screen := &Sync{
		width: 80,
		status: profilegit.Status{
			Repository: true,
			Branch:     "main",
			Head:       "abc",
		},
	}

	view := screen.View()
	if strings.Contains(view, "Profile Git is not set up") {
		t.Fatalf("repository shown as unconfigured:\n%s", view)
	}
	for _, want := range []string{"Repository", "Branch", "Working tree", "No changes."} {
		if !strings.Contains(view, want) {
			t.Fatalf("clean repo missing %q:\n%s", want, view)
		}
	}
}
```

- [ ] **Step 5: Run Sync tests and verify the non-repo case fails**

Run:

```sh
go test ./internal/tui/screens -run 'TestSync(NonRepository|CleanRepository)' -count=1
```

Expected: first test FAIL because current view is only `"Profile is not a Git repository."`.

- [ ] **Step 6: Replace the Sync one-line non-repository message**

In `Sync.View()` replace:

```go
if !s.status.Repository {
	return "Profile is not a Git repository."
}
```

with:

```go
if !s.status.Repository {
	return renderEmptyState(s.styles, s.width, emptyStateCopy{
		Heading:     "Profile Git is not set up",
		Explanation: "Sync is optional. It keeps the Blueprint profile itself in Git so you can version it and share it between machines.",
		Guidance:    "Press i to initialize a repository if you want to use Sync.",
	})
}
```

Do not change `Actions()`, `allowed`, init/fetch/commit/push/pull/remote operations, handoffs, or session reload behavior.

- [ ] **Step 7: Run targeted and complete TUI tests**

Run:

```sh
go test ./internal/tui/screens -run 'Test(Machine|Machines|Sync)' -count=1
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit Machines and Sync guidance**

```sh
git add \
  internal/tui/screens/machines.go \
  internal/tui/screens/machines_test.go \
  internal/tui/screens/sync.go \
  internal/tui/screens/sync_test.go
git commit -m "feat: explain optional machine and sync states"
```

---

### Task 6: Regression, responsive acceptance, and scope verification

**Files:**
- Modify only if a verification failure demonstrates a regression introduced by Tasks 1-5.
- Do not add unrelated cleanup.

**Interfaces:**
- No new interfaces.
- This task proves PR #28 stayed presentation-only.

- [ ] **Step 1: Run all TUI tests**

Run:

```sh
go test ./internal/tui/... -count=1
```

Expected: PASS.

- [ ] **Step 2: Run the complete repository tests**

Run:

```sh
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run static analysis**

Run:

```sh
go vet ./...
```

Expected: exit 0.

- [ ] **Step 4: Build the CLI/TUI binary**

Run:

```sh
go build ./cmd/omarchy-blueprint
```

Expected: exit 0.

- [ ] **Step 5: Check whitespace and patch hygiene**

Run:

```sh
git diff --check
```

Expected: no output.

- [ ] **Step 6: Verify the PR did not cross presentation boundaries**

Run:

```sh
git diff --name-only "$(git merge-base HEAD main)"...HEAD
```

Expected changed code is limited to:

```text
internal/tui/screens/empty_state.go
internal/tui/screens/empty_state_test.go
internal/tui/screens/overview.go
internal/tui/screens/overview_test.go
internal/tui/screens/capture.go
internal/tui/screens/capture_test.go
internal/tui/screens/restore.go
internal/tui/screens/restore_test.go
internal/tui/screens/provider.go
internal/tui/screens/provider_test.go
internal/tui/screens/config.go
internal/tui/screens/config_test.go
internal/tui/screens/resources.go
internal/tui/screens/resources_test.go
internal/tui/screens/machines.go
internal/tui/screens/machines_test.go
internal/tui/screens/sync.go
internal/tui/screens/sync_test.go
docs/adr/0019-tui-contextual-empty-state-onboarding.md
docs/planning/specs/2026-09-18-tui-contextual-empty-state-onboarding-design.md
docs/planning/plans/2026-09-18-tui-contextual-empty-state-onboarding-implementation-plan.md
```

If `internal/workflow`, `internal/profile` production code, provider implementations, CLI commands, or schema files changed, stop and remove those changes unless a separately approved requirement demands them.

- [ ] **Step 7: Search for prohibited scope expansion**

Run:

```sh
git diff "$(git merge-base HEAD main)"...HEAD -- \
  internal/workflow \
  internal/profile \
  internal/providers \
  internal/cli \
  cmd
```

Expected: no production-code diff.

- [ ] **Step 8: Manually exercise the fresh-profile journey at the established widths**

Run the TUI against a disposable fresh profile and check each width:

```text
140 columns
100 columns
80 columns
terminal-too-small (<70 columns or <18 rows)
```

At 140/100/80 verify:

- Overview explains that nothing has been captured without creating a wizard.
- Sidebar/navigation remains fully available.
- Capture still shows every category and selection controls.
- Capture hint does not push the table completely out of view.
- Restore says there is nothing to restore only while the profile is completely uncaptured.
- Hooks uncaptured copy does not claim "no hooks found."
- Config still exposes live candidates and policy controls when candidates exist.
- Resources empty guidance leaves `Tab` Discover available.
- Machines guidance does not hide portable Resource rows when Resources exist.
- Sync non-repo guidance matches the enabled `i` initialization action.
- Long prose wraps inside the workspace; no horizontal overflow appears.

At terminal-too-small verify the existing root message remains unchanged and no screen onboarding content leaks through.

This is a local responsive acceptance pass, not the later real-Omarchy acceptance checkpoint.

- [ ] **Step 9: Manually exercise legitimate empty states**

Using a disposable profile/session, verify:

```text
Hooks: captured with zero hook items
Plugins: captured with zero plugin items
Themes: captured with no current/saved themes
Shell: captured with empty Hash
Resources: no tracked Resources
Machines: Resources absent
Machines: Resources present, no overrides
Sync: profile is not a Git repository
Config: no candidates
Config: candidates exist but active filter matches none
```

Expected:

- each optional zero-state explains purpose without warning/error language;
- captured-empty state does not tell the user to recapture;
- filter-empty Config does not claim global Config emptiness;
- populated states suppress onboarding copy.

- [ ] **Step 10: Verify deferred candidate-preview work did not leak into the PR**

Search the diff for new workflow inspection/candidate APIs:

```sh
git diff "$(git merge-base HEAD main)"...HEAD | grep -Ei 'candidate preview|preview capture|inspect capture|would capture|CapturePreview|PreviewCapture' || true
```

Expected: no new implementation API. Documentation may mention the explicitly deferred future work.

Also verify `workflow.CaptureStatus` remains semantically unchanged.

- [ ] **Step 11: Inspect commit history for reviewable task boundaries**

Run:

```sh
git log --oneline "$(git merge-base HEAD main)"..HEAD
```

Expected implementation commits correspond approximately to:

```text
feat: add contextual empty-state guidance contract
feat: guide fresh profile workflows
feat: explain empty category states
feat: explain optional config and resources states
feat: explain optional machine and sync states
```

Do not squash during implementation unless the repository's PR process explicitly requires it.

- [ ] **Step 12: Commit any verification-only test correction, if one was necessary**

Only if Steps 1-10 exposed a PR #28 regression and a focused fix was required:

```sh
git add <only-the-files-needed-for-the-fix>
git commit -m "test: cover contextual empty-state regression"
```

If no correction was needed, do not create an empty commit.

- [ ] **Step 13: Final clean-state verification**

Run:

```sh
go test ./internal/tui/... -count=1
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
git status --short
```

Expected:

- all commands before `git status` succeed;
- `git status --short` is empty.

---

## Reviewer checklist for PR #28

Use this checklist during final review:

- [ ] Fresh profile remains a normal, fully navigable profile; no wizard/checklist state exists.
- [ ] Capture remains populated with every available category before first capture.
- [ ] No screen says "nothing found" solely because a category is uncaptured.
- [ ] Hooks clearly distinguishes uncaptured from captured-and-empty.
- [ ] Captured-empty optional features are reassuring, not warnings or required tasks.
- [ ] Defaults retains concrete application rows rather than receiving a generic empty tutorial.
- [ ] Config distinguishes truly empty candidate state from a filter with zero matches.
- [ ] Config exact policy wording and `Included never overrides safety rules.` remain unchanged.
- [ ] Resources explains optional use and keeps Discover intact.
- [ ] Machines explains its dependency on Resources and does not hide useful portable-path rows.
- [ ] Sync explicitly says Git is optional and only mentions `i` when initialization is available.
- [ ] Populated screens suppress empty-state onboarding.
- [ ] Existing keybindings, actions, modal behavior, and responsive breakpoints are unchanged.
- [ ] No profile schema, workflow, provider, capture, restore, Resources, Machines, Profile Git, CLI, or JSON semantics changed.
- [ ] No pre-capture candidate-preview implementation was added.
- [ ] `go test ./internal/tui/... -count=1` passes.
- [ ] `go test ./... -count=1` passes.
- [ ] `go vet ./...` passes.
- [ ] `go build ./cmd/omarchy-blueprint` passes.
- [ ] `git diff --check` passes.
- [ ] Local responsive acceptance passes at 140/100/80/too-small.
- [ ] The later real-Omarchy acceptance checkpoint remains a separate post-PR #28 activity.

## Follow-up after PR #28

The next product/design effort is the explicitly deferred **pre-capture candidate preview**: allowing users to inspect and control the full state Blueprint would capture before the first capture where the workflow can safely calculate it.

That work must be designed at the workflow/domain level rather than inferred in the TUI, and should preserve:

**inspect → calculate → preview → approve → apply → verify**
