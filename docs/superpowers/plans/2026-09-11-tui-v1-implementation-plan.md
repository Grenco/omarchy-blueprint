# TUI v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Omarchy Blueprint's primary interactive Bubble Tea interface—sidebar navigation, decision inbox, Config review, Resource discovery, machine mappings, shared diffs, normal-vs-force restore consequences, Profile Git Sync, specialist-tool handoff, and Omarchy-aware theming—without creating TUI-only semantics.

> **Implementation clarification (post-plan):** The shipped TUI adds a
> dedicated Capture screen between Overview and Packages. This records current
> behavior; the tasks below remain historical implementation planning.

**Architecture:** Extract presentation-independent orchestration from the current Cobra-centric app into `internal/workflow`, then build `internal/tui` as a native Bubble Tea v2 client of those workflows/providers. The CLI remains the authoritative headless surface; new inspection data needed by the TUI is exposed through `inspect` CLI/JSON before the TUI consumes it.

**Tech Stack:** Go 1.25+, Bubble Tea v2, Lip Gloss v2, Bubbles v2, Cobra, `golang.org/x/term`, `github.com/mattn/go-shellwords`, existing Blueprint providers/machine/restore/profilegit packages.

**Spec:** `docs/superpowers/specs/2026-09-11-tui-v1-design.md`

## Prerequisite

Profile Git Sync v1 must be merged first:

```text
docs/superpowers/specs/2026-09-11-profile-git-sync-v1-design.md
docs/superpowers/plans/2026-09-11-profile-git-sync-v1-implementation-plan.md
```

The TUI plan assumes `internal/profilegit` and `profile git ...` CLI commands exist and satisfy that spec.

## Global Constraints

- Bare `omarchy-blueprint` launches the TUI only when stdin and stdout are interactive terminals; non-TTY bare invocation prints CLI help without escape codes.
- `omarchy-blueprint tui` explicitly requests TUI and errors when no TTY is available.
- Existing explicit CLI subcommands retain their current behavior.
- TUI never shells out to the Blueprint executable or parses Blueprint human/JSON output.
- Any new Blueprint semantic information needed by TUI is implemented in core/workflow + CLI/JSON first or in the same task before TUI use.
- TUI v1 does not add a profile registry; it uses the same `--profile` path semantics as CLI.
- No network request on TUI startup; Profile Git fetch/pull/push are explicit.
- No embedded text editor or destructive general-purpose file manager.
- No merge/rebase/conflict-resolution Git UI.
- Restore always previews normal and forced plans separately; force never bypasses the existing core planner/executor.
- File/diff previews must hide sensitive, binary, special, or oversized content and sanitize terminal controls.
- TUI meaning never depends on colour alone.
- Missing Omarchy theme data or external tools must degrade gracefully, not block TUI startup.
- External processes are invoked directly, never via an interpolating shell.
- After any external editor/LazyGit/process handoff, refresh affected Blueprint state.

---

## File structure

New presentation-independent workflow files:

```text
internal/workflow/dependencies.go           provider/environment dependency DTO
internal/workflow/session.go                canonical profile + machine session
internal/workflow/providers.go              provider adapter/catalog moved out of Cobra layer
internal/workflow/status.go                 structured provider status/scan/runtime detail
internal/workflow/capture.go                shared capture orchestration
internal/workflow/restore.go                shared plan/apply + normal/force comparison
internal/workflow/inspect.go                path/config/resource focused inspections
internal/workflow/overview.go               decision-inbox aggregation
internal/workflow/*_test.go
```

New TUI package:

```text
internal/tui/run.go                         Bubble Tea program entry
internal/tui/model.go                       root model/update dispatch
internal/tui/layout.go                      responsive layout/focus
internal/tui/keys.go                        global/context key map
internal/tui/theme.go                       semantic palette + Omarchy reload
internal/tui/actions.go                     command-palette action registry
internal/tui/handoff.go                     editor/file-manager/browser/LazyGit/clipboard
internal/tui/messages.go                    async result messages/request IDs

internal/tui/components/sidebar.go
internal/tui/components/help.go
internal/tui/components/palette.go
internal/tui/components/diff.go
internal/tui/components/browser.go
internal/tui/components/confirm.go
internal/tui/components/statusbar.go

internal/tui/screens/overview.go
internal/tui/screens/provider.go
internal/tui/screens/config.go
internal/tui/screens/resources.go
internal/tui/screens/machines.go
internal/tui/screens/restore.go
internal/tui/screens/sync.go

internal/tui/**/*_test.go
```

Shared inspection/diff support is provided by the Profile Git Sync prerequisite:

```text
internal/inspection/diff.go                 bounded structured diff model/builder
internal/inspection/diff_test.go
```

TUI tasks extend its consumers but do not create a second diff representation.

CLI additions/refactors:

```text
internal/app/inspect.go                      `inspect path|config|resource`
internal/app/inspect_test.go
internal/app/app.go                          bare/TUI dispatch + command registration
internal/app/providers.go                    thin wrappers/deleted portions after workflow move
```

Do not put Bubble Tea types in `internal/workflow`, provider packages, profile model, or restore model.

---

### Task 1: Add Charm v2 dependencies and TTY-aware TUI entrypoint scaffold

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Create: `internal/tui/run.go`
- Create: `internal/tui/run_test.go`

**Interfaces:**

Produces:

```go
package tui

type Options struct {
    ProfileDir string
    Machine    string
}

type Dependencies struct {
    // populated with shared workflow dependencies in Task 2
}

func Run(ctx context.Context, opts Options, deps Dependencies) error
```

App test seam:

```go
type Dependencies struct {
    // existing fields...
    IsTTY  func() bool
    RunTUI func(context.Context, tui.Options, tui.Dependencies) error
}
```

or an equivalent injectable launcher without introducing an import cycle.

- [ ] **Step 1: Add failing root-dispatch tests**

Cases:

```go
func TestBareInteractiveInvocationLaunchesTUI(t *testing.T) { /* IsTTY=true; no args; assert launcher called once */ }
func TestBareNonTTYInvocationPrintsHelp(t *testing.T) { /* IsTTY=false; no args; assert launcher not called; help present */ }
func TestExplicitTUIRequiresTTY(t *testing.T) { /* `tui`, IsTTY=false → code 1/actionable error */ }
func TestExplicitCLICommandNeverAutoLaunchesTUI(t *testing.T) { /* `status`, IsTTY=true → normal CLI path */ }
```

Also cover `--profile /x --machine desktop` forwarding into `tui.Options`.

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./internal/app ./internal/tui -run 'TUI|BareInteractive|BareNonTTY' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Add Charm v2 dependencies**

Pin the stable versions verified during design review:

```bash
go get charm.land/bubbletea/v2@v2.0.9
go get charm.land/bubbles/v2@v2.2.1
go get charm.land/lipgloss/v2@v2.0.6
go get golang.org/x/term@v0.45.0
go get github.com/mattn/go-shellwords@v1.0.13
```

Do not replace these with prerelease, branch, pseudo-version, or unpinned `@latest` dependencies during this implementation slice. If dependency resolution proves an incompatibility, stop and document the exact conflict before choosing another tagged stable version.

- [ ] **Step 4: Implement TTY defaults**

Default `IsTTY` must require both real stdin and stdout files to be terminals:

```go
func defaultIsTTY() bool {
    return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
```

Do not infer interactivity from `$TERM` alone.

- [ ] **Step 5: Add root `RunE` and explicit `tui` command**

Root no-subcommand behavior:

```go
if deps.IsTTY() {
    return deps.RunTUI(ctx, tui.Options{ProfileDir: opt.profileDir, Machine: opt.machine}, ...)
}
return root.Help()
```

The explicit `tui` subcommand checks `IsTTY` and returns `tui requires an interactive terminal` when false.

Ensure existing persistent profile canonicalization still runs before launch.

- [ ] **Step 6: Implement minimal TUI program**

`Run` creates a Bubble Tea v2 program whose temporary model renders `Omarchy Blueprint` and exits on `q`/`ctrl+c`. Use alternate-screen mode.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/app ./internal/tui -run 'TUI|BareInteractive|BareNonTTY' -count=1
go build ./cmd/omarchy-blueprint
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/app/app.go internal/app/app_test.go internal/tui
git commit -m "feat: add tui entrypoint"
```

---

### Task 2: Extract shared session/provider orchestration below CLI

**Files:**
- Create: `internal/workflow/dependencies.go`
- Create: `internal/workflow/session.go`
- Create: `internal/workflow/providers.go`
- Create: `internal/workflow/status.go`
- Create: `internal/workflow/capture.go`
- Create: `internal/workflow/session_test.go`
- Create: `internal/workflow/status_test.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/tui/run.go`

**Interfaces:**

Produces neutral dependencies:

```go
type Dependencies struct {
    Runner            command.Runner
    Now               func() time.Time
    StateHome         func() (string, error)
    ThemeDirs         func() (string, string, error)
    PluginDir         func() (string, error)
    ConfigDirs        func() (string, string, error)
    BaselineHistory   func() configprovider.BaselineHistory
    ShellPaths        func() (string, string, error)
    HooksDir          func() (string, error)
    MiseGlobalConfig  func() (string, error)
    HomeDir           func() (string, error)
    Hostname          func() (string, error)
    ResourceLinkRoots func(string) []resourcesprovider.LinkSearchRoot
}

type Options struct {
    ProfileDir      string
    ExplicitMachine string
}

type Session struct { /* unexported provider adapters + resolved context */ }

func Open(deps Dependencies, opts Options) (*Session, error)
func (s *Session) Reload() error
func (s *Session) Profile() profile.Data
func (s *Session) Machine() machine.Selection
```

Status/capture:

```go
type ProviderStatus struct {
    ID              string
    Captured        bool
    Changes         []model.Change
    ConfigScan      *configprovider.ScanSummary
    ResourceGit     map[string]resourcesprovider.GitWorkingSummary
    Verification    *model.VerificationResult
}

type StatusReport struct {
    Profile   profile.Data
    Machine   machine.Selection
    Providers []ProviderStatus
}

type CaptureResult struct {
    Profile    profile.Data
    Providers  []string
    Changes    []model.Change
    ConfigScan *configprovider.ScanSummary
}

func (s *Session) Status(ctx context.Context, onlyProvider string) (StatusReport, error)
func (s *Session) Capture(ctx context.Context, onlyProvider string) (CaptureResult, error)
```

- [ ] **Step 1: Write parity tests around existing CLI behavior**

Before moving code, add/identify tests that freeze:

```text
status provider ordering
status exit code 2 on drift
status JSON config scan fields
status Resource Git working summaries
aggregate capture transaction rollback behavior
machine effective Resource paths
```

- [ ] **Step 2: Write workflow-level failing tests**

Construct injected dependencies and assert `Open` resolves canonical profile + machine selection, and `Status` returns the same semantic changes as current CLI for packages/config/resources fixture state.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/workflow ./internal/app -run 'Workflow|Status|Capture|Machine' -count=1
```

Expected: workflow package missing/FAIL.

- [ ] **Step 4: Move provider adapter contract out of `internal/app`**

Move the current `stateProvider`, category adapters, Resources provider construction/ownership, Config scan-diff extension, and provider ordering to `internal/workflow/providers.go` with dependencies renamed from app-specific types.

Keep provider-specific semantics byte-for-byte equivalent; this task is architectural extraction, not policy redesign.

- [ ] **Step 5: Implement `Open`/`Reload`**

`Open` canonicalizes `ProfileDir`, loads the profile, resolves local/explicit machine selection, and builds adapters. `Reload` repeats disk/profile/machine resolution because Git pull/external tools may change them.

- [ ] **Step 6: Move status/capture orchestration**

Move semantic result construction and capture transaction sequencing out of human/JSON rendering. `Capture` must preserve existing `CommitCapture`/`FinalizeCapture`/`RollbackCapture` boundaries.

Cobra handlers call the workflow then render the returned DTO.

- [ ] **Step 7: Map app dependencies into workflow dependencies once**

Add a helper in `internal/app`:

```go
func workflowDependencies(d Dependencies) workflow.Dependencies {
    return workflow.Dependencies{
        Runner: d.Runner,
        Now: d.Now,
        StateHome: d.StateHome,
        // ...every domain dependency exactly once
    }
}
```

Do not let TUI import `internal/app` to obtain defaults.

- [ ] **Step 8: Run parity tests**

```bash
go test ./internal/workflow ./internal/app -count=1
go test ./... -count=1
```

Expected: PASS and existing CLI JSON/human tests remain unchanged except intentional internal refactor.

- [ ] **Step 9: Commit**

```bash
git add internal/workflow internal/app/providers.go internal/app/app.go internal/app/app_test.go internal/tui/run.go
git commit -m "refactor: share blueprint workflows with tui"
```

---

### Task 3: Add focused `inspect` core/CLI capability

**Files:**
- Create: `internal/workflow/inspect.go`
- Create: `internal/workflow/inspect_test.go`
- Create: `internal/app/inspect.go`
- Create: `internal/app/inspect_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**

Produces:

```go
type GitInspection struct {
    Root      string `json:"root"`
    Remote    string `json:"remote,omitempty"`
    Revision  string `json:"revision,omitempty"`
    Branch    string `json:"branch,omitempty"`
    Staged    int    `json:"staged"`
    Unstaged  int    `json:"unstaged"`
    Untracked int    `json:"untracked"`
}

type PathInspection struct {
    Path              string         `json:"path"`
    Type              string         `json:"type"`
    Mode              string         `json:"mode,omitempty"`
    Size              int64          `json:"size,omitempty"`
    SymlinkTarget     string         `json:"symlink_target,omitempty"`
    OwnershipProvider string         `json:"ownership_provider,omitempty"`
    ResourceID        string         `json:"resource_id,omitempty"`
    ResourceDefault   string         `json:"resource_default_path,omitempty"`
    ResourceEffective string         `json:"resource_effective_path,omitempty"`
    Git               *GitInspection `json:"git,omitempty"`
    SuggestedStrategy string         `json:"suggested_strategy,omitempty"`
    StrategyReason    string         `json:"strategy_reason,omitempty"`
    BlockedReason     string         `json:"blocked_reason,omitempty"`
}

type ConfigInspection struct {
    Candidate    configprovider.Candidate `json:"candidate"`
    LivePath     string                   `json:"live_path,omitempty"`
    BaselinePath string                   `json:"baseline_path,omitempty"`
    ProfilePath  string                   `json:"profile_path,omitempty"`
    Managed      bool                     `json:"managed"`
}

type ResourceInspection struct {
    Resource      profile.Resource                    `json:"resource"`
    DefaultPath   string                              `json:"default_path"`
    EffectivePath string                              `json:"effective_path"`
    Machine       string                              `json:"machine,omitempty"`
    Git           *resourcesprovider.GitWorkingSummary `json:"git,omitempty"`
}

func (s *Session) InspectPath(ctx context.Context, path string) (PathInspection, error)
func (s *Session) InspectConfig(ctx context.Context, logical string) (ConfigInspection, error)
func (s *Session) InspectResource(ctx context.Context, id string) (ResourceInspection, error)
```

CLI:

```text
inspect path <path>
inspect config <logical-path>
inspect resource <id>
```

- [ ] **Step 1: Write `InspectPath` fixture tests**

Cover:

```text
regular file outside semantic owner → copy suggestion
directory clean portable Git root   → git suggestion
dirty portable Git root             → git+diff suggestion
tracked Resource effective root     → ResourceID/default/effective populated
Config-owned path                   → OwnershipProvider=config, blocked from generic Resource
symlink                             → type/target visible, BlockedReason for tracking
special file                        → blocked
```

Use existing Resources Git detection/policy helpers; do not implement a looser second Git portability check.

- [ ] **Step 2: Write focused Config/Resource inspection tests**

Config result must carry exact provider classification and `Reason`. Resource result must carry saved strategy plus runtime/effective-machine context.

- [ ] **Step 3: Write CLI JSON tests**

Assert:

```bash
omarchy-blueprint --json inspect path <fixture>
omarchy-blueprint --json inspect config .config/foo/bar
omarchy-blueprint --json inspect resource projects
```

expose the same structured fields without TUI concepts.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/workflow ./internal/app -run 'Inspect' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement inspection by composing existing providers**

Path ownership must use the same ownership index/effective Resource roots used by Resources tracking. Git inspection must be bounded and read-only.

Strategy recommendation rules:

```go
switch {
case blocked != "":                 suggestion = ""
case gitPortable && dirty:           suggestion = "git+diff"
case gitPortable && !dirty:          suggestion = "git"
default:                             suggestion = "copy"
}
```

Set `StrategyReason` with domain facts, not UI prose tied to a screen layout.

- [ ] **Step 6: Implement CLI renderers**

Human output is concise; JSON serializes DTO fields. Add root `inspect` command registration.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/workflow ./internal/app -run 'Inspect' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/workflow/inspect.go internal/workflow/inspect_test.go \
  internal/app/inspect.go internal/app/inspect_test.go internal/app/app.go
git commit -m "feat: inspect blueprint paths and resources"
```

---

### Task 4: Reuse the shared bounded diff model for Config and restore inspection

**Files:**
- Modify: `internal/inspection/diff.go`
- Modify: `internal/inspection/diff_test.go`
- Modify: `internal/workflow/inspect.go`
- Modify: `internal/workflow/inspect_test.go`

**Interfaces:**

Profile Git Sync v1 already provides `inspection.DiffDocument` and `BuildTextDiff`. Extend focused Config inspection with safe labelled documents:

```go
type ConfigInspection struct {
    Candidate           configprovider.Candidate  `json:"candidate"`
    LivePath            string                    `json:"live_path,omitempty"`
    BaselinePath        string                    `json:"baseline_path,omitempty"`
    ProfilePath         string                    `json:"profile_path,omitempty"`
    Managed             bool                      `json:"managed"`
    BaselineToLive      *inspection.DiffDocument `json:"baseline_to_live,omitempty"`
    ProfileToLive       *inspection.DiffDocument `json:"profile_to_live,omitempty"`
}
```

No new diff type is introduced.

- [ ] **Step 1: Add Config diff fixture tests**

Cases:

```text
baseline + modified live file → BaselineToLive with explicit baseline/live labels
managed profile + drifted live → ProfileToLive with explicit profile/live labels
binary/sensitive/oversized    → metadata-only shared document; no bytes exposed
missing side                  → new/deleted-style shared document
control characters           → no terminal escape byte in document lines
```

- [ ] **Step 2: Add shared-diff regression tests for TUI-specific edge cases**

Exercise very long labels/lines and empty files using the existing `inspection` builder. Do not change the Profile Git JSON shape except by backward-compatible metadata additions.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/inspection ./internal/workflow -run 'Diff|InspectConfig' -count=1
```

Expected: FAIL because Config inspection does not yet attach shared documents.

- [ ] **Step 4: Implement Config-side byte acquisition and shared-diff construction**

Use the provider's canonical live/baseline/profile paths and existing regular-file/sensitive helpers. Reuse `inspection.BuildTextDiff`; never invoke external `diff`, never parse ANSI/unified output, and never bypass the shared bounds.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/inspection ./internal/workflow -run 'Diff|InspectConfig' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/inspection/diff.go internal/inspection/diff_test.go \
  internal/workflow/inspect.go internal/workflow/inspect_test.go
git commit -m "feat: inspect config diffs safely"
```

---

### Task 5: Implement semantic theming and live Omarchy palette reload

**Files:**
- Create: `internal/tui/theme.go`
- Create: `internal/tui/theme_test.go`
- Create: `internal/tui/messages.go`
- Modify: `internal/tui/run.go`

**Interfaces:**

Produces:

```go
type Palette struct {
    Foreground, Muted, Accent string
    Success, Warning, Error string
    Added, Removed string
    Border, BorderFocused, Selection string
    ColorEnabled bool
}

type ThemeLoader struct {
    StateHome func() (string, error)
    NoColor   bool
}

func (l ThemeLoader) Load() Palette
func (l ThemeLoader) Fingerprint() string
```

- [ ] **Step 1: Write palette parsing tests**

Fixture `colors.toml` includes `foreground`, `accent`, `selection_background`, `color1`, `color2`, `color3`, `color8`. Assert semantic mapping.

Also test malformed/missing file returns ANSI/default fallback without error.

- [ ] **Step 2: Write `NO_COLOR` test**

With `NoColor=true`, `ColorEnabled=false` and styles retain non-colour distinctions.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/tui -run 'Theme|Palette|NoColor' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement XDG Omarchy palette locator**

Resolve:

```text
<StateHome>/omarchy/current/theme/colors.toml
```

where normal production `StateHome` is the user's XDG state home. Treat symlink/target replacement as normal; read regular file safely. Missing/unreadable is fallback.

- [ ] **Step 5: Implement fallback styles**

Use terminal default background and ANSI semantic colours rather than fixed RGB. Do not paint a global background.

- [ ] **Step 6: Add one-second theme fingerprint tick**

Bubble Tea command emits `themeTickMsg`. If fingerprint changed, reload `Palette`; otherwise keep styles. Tick must not perform a full provider refresh.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/tui -run 'Theme|Palette|NoColor' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/theme.go internal/tui/theme_test.go internal/tui/messages.go internal/tui/run.go
git commit -m "feat: theme tui from omarchy palette"
```

---

### Task 6: Build the root TUI shell, sidebar, focus, help, and command palette

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/layout.go`
- Create: `internal/tui/keys.go`
- Create: `internal/tui/actions.go`
- Create: `internal/tui/model_test.go`
- Create: `internal/tui/components/sidebar.go`
- Create: `internal/tui/components/statusbar.go`
- Create: `internal/tui/components/help.go`
- Create: `internal/tui/components/palette.go`
- Create: `internal/tui/components/confirm.go`
- Modify: `internal/tui/run.go`

**Interfaces:**

Stable screen IDs:

```go
type ScreenID string
const (
    ScreenOverview  ScreenID = "overview"
    ScreenCapture   ScreenID = "capture"
    ScreenPackages  ScreenID = "packages"
    ScreenConfig    ScreenID = "config"
    ScreenResources ScreenID = "resources"
    ScreenThemes    ScreenID = "themes"
    ScreenPlugins   ScreenID = "plugins"
    ScreenShell     ScreenID = "shell"
    ScreenHooks     ScreenID = "hooks"
    ScreenDefaults  ScreenID = "defaults"
    ScreenMachines  ScreenID = "machines"
    ScreenSync      ScreenID = "sync"
    ScreenRestore   ScreenID = "restore"
)
```

- [ ] **Step 1: Write root-model navigation tests**

Drive `Update` with `tea.WindowSizeMsg` and key messages. Assert sidebar selection, screen ID, `Tab` focus cycle, `:` palette open/filter/activate, `?` help open/close, `Esc`, and `q` semantics.

- [ ] **Step 2: Write responsive layout tests**

Assert layout mode:

```text
140x40 → three-pane
100x30 → two-pane
80x24  → compact
60x15  → too-small
```

No negative widths/panics.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/tui -run 'Navigation|Layout|CommandPalette|Help' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement root model with screen interface**

Use a narrow internal interface:

```go
type screen interface {
    ID() ScreenID
    SetSize(width, height int)
    Update(tea.Msg) tea.Cmd
    View() string
    Actions() []Action
}
```

Root model owns global focus/transients/palette/theme and routes unconsumed messages to active screen.

- [ ] **Step 5: Implement sidebar/statusbar/help**

Sidebar always uses stable screen ordering. Statusbar accepts contextual actions and renders only those valid for current state.

Current order starts Overview, Capture, Packages. On Capture, `Space` toggles
the highlighted provider, `a` selects changed providers, `c` opens confirmation
for selected-provider capture, and `C` opens confirmation for aggregate Capture
All. `Enter` confirms and `Esc` cancels. Successful capture reloads the profile
and refreshes Overview, provider status, and local Profile Git/Sync status.

State symbols include text characters (`!`, `~`, `✓`, `×`, `?`) in addition to colour.

- [ ] **Step 6: Implement action registry/palette**

```go
type Action struct {
    ID, Label, Shortcut string
    Enabled bool
    DisabledReason string
    Run func() tea.Cmd
}
```

Palette fuzzy-filters labels/IDs with simple subsequence matching; do not add a large fuzzy library for v1.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/tui -run 'Navigation|Layout|CommandPalette|Help' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/layout.go internal/tui/keys.go internal/tui/actions.go \
  internal/tui/model_test.go internal/tui/components internal/tui/run.go
git commit -m "feat: add tui navigation shell"
```

---

### Task 7: Add external-tool handoff and shared diff viewer

**Files:**
- Create: `internal/tui/handoff.go`
- Create: `internal/tui/handoff_test.go`
- Create: `internal/tui/components/diff.go`
- Create: `internal/tui/components/diff_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Produces:

```go
type Handoff struct {
    LookupPath func(string) (string, error)
    Visual     string
    Editor     string
}

func (h Handoff) EditorCmd(path string) (*exec.Cmd, error)
func (h Handoff) FileManagerCmd(path string, isDir bool) (*exec.Cmd, error)
func (h Handoff) BrowserCmd(rawURL string) (*exec.Cmd, error)
func (h Handoff) LazyGitCmd(repo string) (*exec.Cmd, error)
```

Diff component consumes `inspection.DiffDocument` only.

- [ ] **Step 1: Write handoff construction tests**

Cases:

```text
VISUAL="nvim" → exec nvim <path>
VISUAL="code --wait" → exec code --wait <path>
EDITOR fallback → used when VISUAL empty
no editor → disabled error
file path → xdg-open parent directory
directory → xdg-open directory
browser accepts https only
browser rejects file:/javascript:/unknown schemes
lazygit missing → disabled error
lazygit present → command Dir == repo, no shell
```

Parse `$VISUAL`/`$EDITOR` with `shellwords.NewParser()` from `github.com/mattn/go-shellwords` with `ParseEnv=false` and `ParseBacktick=false`. Require at least one parsed word; resolve only argv[0] with `LookupPath`, append the selected path as the final argument, and execute directly. Tests must prove quoted arguments survive and shell metacharacters/backticks are never evaluated.

- [ ] **Step 2: Write diff viewer tests**

Given structured hunks, assert unified rendering contains labels/line markers, hunk navigation changes viewport, `s` toggles side-by-side only when width threshold allows, and sensitive/binary/too-large states render metadata instead of content.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/tui -run 'Handoff|DiffViewer' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement process suspension with Bubble Tea**

Convert commands to `tea.ExecProcess(cmd, callback)` so alt screen/input state is restored after subprocess exits. Callback emits a typed `handoffFinishedMsg{Kind, Err}` that triggers screen refresh.

- [ ] **Step 5: Implement clipboard adapter**

Use Bubble Tea v2's native `tea.SetClipboard(text)` OSC52 command. Treat copy as a convenience request: emit a short non-fatal notification, and do not add a required `wl-copy`/X11 dependency. Keep displayed semantics independent of clipboard success because OSC52 support is terminal-dependent.

- [ ] **Step 6: Implement diff component**

Support scrolling, `{`/`}` hunk movement, unified/side-by-side toggle, whitespace markers, and bounded line wrapping. Never interpret diff text as ANSI.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/tui -run 'Handoff|DiffViewer' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/handoff.go internal/tui/handoff_test.go \
  internal/tui/components/diff.go internal/tui/components/diff_test.go internal/tui/model.go go.mod go.sum
git commit -m "feat: add tui diff and tool handoff"
```

---

### Task 8: Build the Yazi-inspired filesystem browser/picker

**Files:**
- Create: `internal/tui/components/browser.go`
- Create: `internal/tui/components/browser_test.go`
- Modify: `internal/tui/messages.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Produces:

```go
type BrowserMode string
const (
    BrowseResource BrowserMode = "resource"
    PickDirectory  BrowserMode = "directory"
    BrowseReadOnly BrowserMode = "read-only"
)

type BrowserEntry struct {
    Name, Path, Type string
    Hidden bool
}

type Bookmark struct { Label, Path string }
```

Browser accepts an inspection callback command factory:

```go
func inspectPathCmd(requestID uint64, path string) tea.Cmd
```

that ultimately calls `workflow.Session.InspectPath`.

- [ ] **Step 1: Write navigation/listing tests**

Fixture contains dotfiles, dirs, regular files, symlink. Assert:

```text
start path HOME
dotfiles visible
directories ordered before files
Enter directory navigates in
Backspace/h navigates parent
can reach /
/ filter affects current directory only
Esc clears filter before closing browser
```

- [ ] **Step 2: Write bookmark tests**

Given session/profile/resources, build deterministic bookmarks for Home, Config, Profile, tracked/effective roots, existing `~/Projects`/`~/Code`, existing mount-convention roots, and session-recent dirs. Deduplicate canonical paths.

- [ ] **Step 3: Write async stale-preview test**

Selection A emits request 10, selection B emits request 11. Deliver result 10 after B; browser must ignore it. Deliver 11; preview updates.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/tui -run 'Browser|Bookmark|StalePreview' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement bounded directory listing**

Use `os.ReadDir` on only the current directory. Do not recursively index. Lstat entries; do not follow symlink directories for navigation automatically.

Permission errors render as non-fatal browser state.

- [ ] **Step 6: Implement semantic preview**

On selection change, issue workflow inspection asynchronously with request ID. Right/detail pane renders ownership, tracked Resource, Git summary, suggested strategy/reason, and blockers.

- [ ] **Step 7: Implement mode-specific selection**

`PickDirectory` accepts directories only. `BrowseResource` can select regular file/directory but disabled when `BlockedReason` prevents tracking. `BrowseReadOnly` never produces a mutation action.

- [ ] **Step 8: Run tests**

```bash
go test ./internal/tui -run 'Browser|Bookmark|StalePreview' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/components/browser.go internal/tui/components/browser_test.go \
  internal/tui/messages.go internal/tui/model.go
git commit -m "feat: add blueprint filesystem browser"
```

---

### Task 9: Implement Config review screen and policy workflow

**Files:**
- Create: `internal/tui/screens/config.go`
- Create: `internal/tui/screens/config_test.go`
- Modify: `internal/providers/config/policy.go`
- Modify: `internal/providers/config/policy_test.go`
- Modify: `internal/workflow/inspect.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Screen consumes Config scan/status and exposes actions through shared workflow/core policy calls.

Add focused workflow mutation if not already presentation-independent:

```go
func (s *Session) SetConfigPolicy(ctx context.Context, logical string, policy string) error
```

Provider support added in this task:

```go
func ClearPolicy(saved profile.Configs, input string) (profile.Configs, bool, error)
```

where policy is `auto`, `include`, or `exclude`. `include` and `exclude` use the existing provider functions. `auto` calls `ClearPolicy`, removing an exact explicit include and/or exact exclusion for that canonical path without removing ancestor/descendant policy records. The headless CLI exposes the same operation as:

```text
omarchy-blueprint auto config:<path>
```

- [ ] **Step 1: Write Config screen model tests**

Fixture candidates include modified, ambiguous, excluded, delegated, sensitive, oversized. Assert list labels/reasons and initial focus.

- [ ] **Step 2: Write policy transition tests**

From ambiguous candidate:

```text
Space/action → Include confirmation/action
workflow mutation succeeds
screen issues rescan
same logical path remains focused
classification becomes provider-returned managed/modified state
```

Also test Exclude and Auto/default removal of exact explicit policy. Add provider/CLI assertions proving `auto config:<path>` removes an exact include/exclude, leaves ancestor/descendant policies untouched, rescans under normal automatic rules, and is available in JSON/human CLI before the TUI action calls it.

- [ ] **Step 3: Write diff/action tests**

`d` opens labelled baseline ↔ live or profile ↔ live diff supplied by workflow. `e`, `o`, `y` actions are enabled only when a valid live path exists.

Sensitive candidate diff opens metadata-only viewer.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/tui ./internal/workflow -run 'ConfigScreen|ConfigPolicy' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement `ClearPolicy`, CLI `auto`, and shared policy workflow**

Implement `configprovider.ClearPolicy` with canonical path normalization and exact-record removal. Register root `auto` for `config:<path>` using the same profile load/save/update-time behavior as existing include/exclude commands. Then implement `Session.SetConfigPolicy` over `AddInclusion`, `AddExclusion`, and `ClearPolicy`.

Do not manipulate `profile.Configs.Included/Excluded` directly in TUI. The workflow owns load → validate policy → save → reload.

- [ ] **Step 6: Implement screen**

Three-pane when wide, compact detail/diff transitions when narrow. Render exact candidate `Reason` returned by provider.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/tui ./internal/workflow -run 'ConfigScreen|ConfigPolicy' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/screens/config.go internal/tui/screens/config_test.go \
  internal/providers/config/policy.go internal/providers/config/policy_test.go \
  internal/workflow/inspect.go internal/app/app.go internal/app/app_test.go internal/tui/model.go
git commit -m "feat: review config policy in tui"
```

---

### Task 10: Implement Resources Tracked/Discover workflows

**Files:**
- Create: `internal/tui/screens/resources.go`
- Create: `internal/tui/screens/resources_test.go`
- Create: `internal/workflow/resources.go`
- Create: `internal/workflow/resources_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Produces focused workflow actions:

```go
type TrackRequest struct {
    Path             string
    ID               string
    Strategy         string
    IncludeUntracked []string
    ExcludeUntracked []string
}

func (s *Session) TrackResource(ctx context.Context, request TrackRequest) (profile.Resource, []model.Change, error)
func (s *Session) UntrackResource(ctx context.Context, id string) ([]string, error)
```

Use existing `resourcesprovider.TrackOptions`; no new strategy semantics.

- [ ] **Step 1: Write tracked-list/detail tests**

Fixture `copy`, `git`, `git+diff` resources with machine mapping. Assert portable/effective paths, dirty informational label for `git`, and separate staged/unstaged/selected-untracked summaries for `git+diff`.

- [ ] **Step 2: Write Discover workflow model test**

Open browser, choose dirty portable Git fixture, receive `InspectPath` recommendation `git+diff`, display all three strategy choices with explanations, and require explicit user choice/confirmation.

- [ ] **Step 3: Write untracked-selection test**

Eligible exact files can toggle. Unsafe/sensitive/ignored/oversized/symlink/directory entries render disabled with reason. Resulting `TrackRequest` contains exact selected paths only.

- [ ] **Step 4: Write update-strategy/untrack tests**

Updating existing Resource passes its effective root into existing track semantics and preserves portable `Resource.Path`. Untrack confirmation explicitly asserts live path remains untouched.

- [ ] **Step 5: Run tests to verify failure**

```bash
go test ./internal/tui ./internal/workflow -run 'ResourceScreen|TrackResource|DiscoverResource' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Implement workflow actions transactionally**

Reuse provider `PrepareTrack` / prepared capture boundaries and app's proven save/commit/finalize pattern. On profile Save failure, roll back staged artifacts.

- [ ] **Step 7: Implement screen + browser integration**

Subtabs `Tracked` and `Discover`; use shared browser, diff, confirmation, and handoff components.

- [ ] **Step 8: Run tests**

```bash
go test ./internal/tui ./internal/workflow -run 'ResourceScreen|TrackResource|DiscoverResource' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/screens/resources.go internal/tui/screens/resources_test.go \
  internal/workflow/resources.go internal/workflow/resources_test.go internal/tui/model.go
git commit -m "feat: manage portable resources in tui"
```

---

### Task 11: Implement Machines screen and simple provider screens

**Files:**
- Create: `internal/tui/screens/machines.go`
- Create: `internal/tui/screens/machines_test.go`
- Create: `internal/tui/screens/provider.go`
- Create: `internal/tui/screens/provider_test.go`
- Create: `internal/workflow/machines.go`
- Create: `internal/workflow/machines_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Workflow methods wrap existing Machine Overlay operations:

```go
func (s *Session) AddMachine(ctx context.Context, name string, use bool) error
func (s *Session) UseMachine(ctx context.Context, name string) error
func (s *Session) ClearMachine(ctx context.Context) error
func (s *Session) RenameMachine(ctx context.Context, oldName, newName string) error
func (s *Session) RemoveMachine(ctx context.Context, name string) error
func (s *Session) MapResource(ctx context.Context, machineName, resourceID, path string) error
func (s *Session) UnmapResource(ctx context.Context, machineName, resourceID string) error
```

- [ ] **Step 1: Write machine lifecycle parity tests**

Call workflow methods and assert identical portable TOML/local-binding semantics already covered by CLI machine tests: add/use/clear/rename/remove/map/unmap, including ownership/overlap rejection.

- [ ] **Step 2: Write machine screen tests**

Assert selected machine marker, default vs override path labels, dormant mappings, inline add-name suggestion, rename/remove confirmation, and directory-picker mapping action.

- [ ] **Step 3: Write generic provider screen tests**

For Packages/Themes/Plugins/Shell/Hooks/Defaults, feed `ProviderStatus` and assert semantic changes/detail plus Capture action uses workflow `Capture(providerID)` and refreshes. Do not invent unsupported per-item policy.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/tui ./internal/workflow -run 'MachineScreen|MachineWorkflow|ProviderScreen' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Extract machine mutations from Cobra into workflow**

Move reusable validation/save/binding logic from machine handlers. Cobra handlers become thin wrappers over these functions while preserving existing output/tests.

- [ ] **Step 6: Implement Machines screen**

Mapping invokes shared `PickDirectory` browser, then workflow validation. Never move Resource bytes when mapping/renaming/removing machine policy.

- [ ] **Step 7: Implement generic provider screens**

Common list/detail/capture behavior; provider-specific data remains typed in workflow DTOs where needed rather than `map[string]any` inside TUI.

- [ ] **Step 8: Run tests**

```bash
go test ./internal/tui ./internal/workflow ./internal/app -run 'Machine|ProviderScreen' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/screens/machines.go internal/tui/screens/machines_test.go \
  internal/tui/screens/provider.go internal/tui/screens/provider_test.go \
  internal/workflow/machines.go internal/workflow/machines_test.go internal/tui/model.go internal/app/app.go
git commit -m "feat: manage machines and providers in tui"
```

---

### Task 12: Add shared restore planning and normal-vs-force consequence UI

**Files:**
- Create: `internal/workflow/restore.go`
- Create: `internal/workflow/restore_test.go`
- Create: `internal/tui/screens/restore.go`
- Create: `internal/tui/screens/restore_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Produces:

```go
type RestoreMode string
const (
    RestoreNormal RestoreMode = "normal"
    RestoreForced RestoreMode = "forced"
)

type Outcome string
const (
    OutcomeCreate  Outcome = "create"
    OutcomeModify  Outcome = "modify"
    OutcomeReplace Outcome = "replace"
    OutcomeDelete  Outcome = "delete"
    OutcomeSkip    Outcome = "skip"
    OutcomeNoop    Outcome = "noop"
)

type Consequence struct {
    Provider string
    Resource string
    OperationID string
    Normal Outcome
    Forced Outcome
    Difference string
    Risk model.Risk
    Diff *inspection.DiffDocument
}

type RestoreComparison struct {
    Normal       model.RestorePlan
    Forced       model.RestorePlan
    Consequences []Consequence
}

type RestoreResult struct {
    Mode         RestoreMode
    Plan         model.RestorePlan
    Execution    restore.Result
    Verification model.VerificationResult
    Journal      string
    Applied      bool
}

func (s *Session) CompareRestore(ctx context.Context, onlyProvider string) (RestoreComparison, error)
func (s *Session) ApplyRestore(ctx context.Context, onlyProvider string, mode RestoreMode) (RestoreResult, error)
```

- [ ] **Step 1: Add workflow parity tests against existing CLI planners**

For same fixture/profile:

```text
CompareRestore.Normal == existing plan force=false
CompareRestore.Forced == existing plan force=true
```

Compare semantically stable operation fields, not pointer identity.

- [ ] **Step 2: Write consequence classification tests**

Fixtures must include:

```text
normal skip → forced FileWrite ReplaceExisting
normal skip → forced FileDelete
operation identical in both modes
Git clone/checkout operations
symlink replacement
unknown/skip remains skip under force
```

Assert `Difference` describes concrete effect and risk is inherited from typed operation.

- [ ] **Step 3: Write restore screen toggle tests**

Initial Normal; pressing mode-toggle key selects Forced and changes displayed counts/items from cached comparison without invoking apply/replan.

Selecting an item with safe file bytes opens shared diff. Sensitive/binary item opens metadata-only detail.

- [ ] **Step 4: Write approval/staleness test**

When user approves visible Normal mode, `ApplyRestore` must regenerate/validate the normal plan immediately before execution through existing restore preconditions. Change fixture state between preview/approval and assert stale assumptions do not silently overwrite unknown data.

- [ ] **Step 5: Run tests to verify failure**

```bash
go test ./internal/workflow ./internal/tui -run 'RestoreComparison|Consequence|RestoreScreen' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Move restore orchestration from app into workflow**

Extract provider plan aggregation, execution, and verification while keeping CLI dry-run/force human/JSON output stable. Preserve `restorePlanOptions{Force}` semantics as `workflow.RestoreMode`/bool mapping.

- [ ] **Step 7: Implement consequence derivation from typed operations**

Do not parse human `Action` strings alone. Inspect `File`, `Delete`, `Symlink`, `Copy`, `GitPatch`, `Command`, `ReplaceExisting`, and `Skipped` fields to classify outcomes.

- [ ] **Step 8: Implement Restore screen**

Show Normal/Forced toggle, summary counts, provider/resource list, changed-mode marker, exact detail/diff, and confirmation counts for create/modify/replace/delete/commands. High-risk/replacement/delete requires explicit confirmation modal.

- [ ] **Step 9: Run tests**

```bash
go test ./internal/workflow ./internal/tui ./internal/app -run 'Restore|Consequence' -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/workflow/restore.go internal/workflow/restore_test.go \
  internal/tui/screens/restore.go internal/tui/screens/restore_test.go \
  internal/app/app.go internal/tui/model.go
git commit -m "feat: preview restore consequences in tui"
```

---

### Task 13: Implement Sync screen over Profile Git Sync v1

**Files:**
- Create: `internal/tui/screens/sync.go`
- Create: `internal/tui/screens/sync_test.go`
- Modify: `internal/workflow/session.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

`Session` exposes the already-built Profile Git service through focused methods without duplicating semantics:

```go
func (s *Session) ProfileGitStatus(ctx context.Context) (profilegit.Status, error)
func (s *Session) ProfileGitDiff(ctx context.Context, path string) (profilegit.Diff, error)
func (s *Session) ProfileGitInit(ctx context.Context) (profilegit.Result, error)
func (s *Session) ProfileGitRemote(ctx context.Context) (string, error)
func (s *Session) ProfileGitSetRemote(ctx context.Context, rawURL string) (profilegit.Result, error)
func (s *Session) ProfileGitRemoveRemote(ctx context.Context) (profilegit.Result, error)
func (s *Session) ProfileGitFetch(ctx context.Context) (profilegit.Result, error)
func (s *Session) ProfileGitCommit(ctx context.Context, message string) (profilegit.Result, error)
func (s *Session) ProfileGitPull(ctx context.Context) (profilegit.Result, error)
func (s *Session) ProfileGitPush(ctx context.Context) (profilegit.Result, error)
```

- [ ] **Step 1: Write Sync screen state tests**

States:

```text
not repository
repo no origin
clean tracked repo
managed dirty + unmanaged dirty
no upstream
ahead
behind
diverged
detached HEAD
pre-existing staged state
```

Assert valid actions and disabled reasons match Profile Git core policy.

- [ ] **Step 2: Write explicit-refresh test**

Entering Sync calls local `Status` only. Pressing Fetch invokes `profilegit.Fetch`, then refreshes status. No network action occurs during root TUI initialization.

- [ ] **Step 3: Write commit flow test**

Open managed diff → commit action → one-line message prefilled from `SuggestedCommitMessage` → edit text → confirm → `Commit` → refreshed status → Push offered.

- [ ] **Step 4: Write Commit & Push composition test**

Commit succeeds then Push runs; if Commit returns error, Push command is never created. If Commit is no-op but there are existing ahead commits, Push remains allowed based on refreshed status.

- [ ] **Step 5: Run tests to verify failure**

```bash
go test ./internal/tui -run 'SyncScreen|ProfileGit' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Implement Sync screen**

Use Profile Git DTOs directly through workflow. Display “last fetched” beside ahead/behind. Advanced/unsupported state offers LazyGit/copy-path handoff, never reset/stash/rebase buttons.

- [ ] **Step 7: Implement remote browser conversion**

Use Profile Git helper to convert known HTTPS/scp/ssh remotes to validated HTTP(S). Unknown remote stays display/copy only.

- [ ] **Step 8: Run tests**

```bash
go test ./internal/tui -run 'SyncScreen|ProfileGit' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/screens/sync.go internal/tui/screens/sync_test.go \
  internal/workflow/session.go internal/tui/model.go
git commit -m "feat: sync profile git from tui"
```

---

### Task 14: Add Overview decision inbox and cross-screen navigation

**Files:**
- Create: `internal/workflow/overview.go`
- Create: `internal/workflow/overview_test.go`
- Create: `internal/tui/screens/overview.go`
- Create: `internal/tui/screens/overview_test.go`
- Modify: `internal/tui/model.go`

**Interfaces:**

Produces:

```go
type AttentionSeverity string
const (
    AttentionDecision AttentionSeverity = "decision"
    AttentionWarning  AttentionSeverity = "warning"
    AttentionDrift    AttentionSeverity = "drift"
    AttentionInfo     AttentionSeverity = "info"
)

type AttentionItem struct {
    Severity AttentionSeverity
    Provider string
    Kind string
    Ref string
    Summary string
    Target string
}

type Overview struct {
    ProfileName string
    MachineName string
    Items []AttentionItem
    Healthy []string
    ProfileGit *profilegit.Status
}

func (s *Session) Overview(ctx context.Context) (Overview, error)
```

- [ ] **Step 1: Write aggregation tests**

Fixture includes ambiguous Config, package drift, dirty `git` Resource informational state, dormant machine mapping, clean providers, and managed Profile Git change. Assert deterministic severity/group ordering and no network fetch.

- [ ] **Step 2: Write navigation target tests**

Selecting items routes:

```text
config candidate → Config focused logical path
resource item    → Resources focused ID
machine item     → Machines focused mapping
profile Git      → Sync
restore/check    → Restore/provider as appropriate
```

If target disappears after refresh, destination screen opens normally without panic.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/workflow ./internal/tui -run 'Overview|Attention' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement aggregation from existing structured results**

Do not parse human summaries when typed classification exists. `model.Change.Summary` may be displayed, but severity/target derives from provider/config/resource state.

- [ ] **Step 5: Implement Overview screen**

Sections `Needs review`, `Drift`, `Profile sync`, `Healthy`; Enter navigates through root model target message.

- [ ] **Step 6: Add capture-complete routing**

After a provider/global capture initiated from TUI, refresh Overview and local Profile Git status. If managed profile files changed, show non-modal actions `Review profile diff`, `Commit`, `Commit & push` that route to Sync rather than auto-committing.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/workflow ./internal/tui -run 'Overview|Attention|CaptureComplete' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/workflow/overview.go internal/workflow/overview_test.go \
  internal/tui/screens/overview.go internal/tui/screens/overview_test.go internal/tui/model.go
git commit -m "feat: add tui decision inbox"
```

---

### Task 15: Full integration, accessibility, docs, and manual acceptance

**Files:**
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Modify: `docs/manual-test-checklist.md`
- Modify: `docs/adr/0017-tui-interactive-client-architecture.md` only for implementation-required clarification
- Modify/Create: focused `internal/tui/*_test.go`, `internal/tui/components/*_test.go`, and `internal/tui/screens/*_test.go` integration coverage
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Acceptance/documentation only; no new product semantics.

- [ ] **Step 1: Add end-to-end model integration fixture**

Build a temp profile/home with:

```text
ambiguous Config candidate
dirty Git Resource
second Resource with machine path override
normal/force restore difference
initialized profile Git repository with managed changes
```

Drive root model through messages (without terminal subprocesses) and prove:

```text
Overview → Config policy → Resources inspect → Machines → Restore toggle → Sync
```

uses shared workflow data and survives refreshes.

- [ ] **Step 2: Add non-TTY regression**

Pipe/stdin-buffer bare invocation and assert zero ANSI escape bytes and help text. Explicit existing CLI commands remain unchanged in both TTY modes.

- [ ] **Step 3: Add no-colour/accessibility assertions**

Render Overview/Restore/Config with `NO_COLOR`; assert semantic markers/words still distinguish warning, add/remove, selected mode, and destructive consequence.

- [ ] **Step 4: Add resize/fuzz-style update safety test**

Feed a fixed matrix of small/large `WindowSizeMsg` plus navigation keys and assert no panic/negative slice dimensions. Add `FuzzLayoutDimensions` with seed cases for the four documented breakpoints; normal `go test` executes the seed corpus without requiring a long fuzzing run.

- [ ] **Step 5: Update README**

Document:

```bash
omarchy-blueprint                    # TUI when interactive
omarchy-blueprint tui
omarchy-blueprint --profile ~/omarchy-profile
```

Explain navigation, Config review, Resource discovery, Machines, Restore Normal/Forced comparison, Sync, and external-tool handoffs.

Also document the dedicated Capture screen, its Overview → Capture → Packages
sidebar position, `Space`/`a`/`c`/`C` controls, confirmation/cancel behavior,
aggregate Capture All, and post-capture refreshes.

- [ ] **Step 6: Update ROADMAP**

Mark TUI v1 delivered only after all screens/flows in this plan are implemented. Keep deferred generic backup providers, advanced Git, broader machine-specific state, migration UI, and agent assistance pending.

- [ ] **Step 7: Expand manual checklist**

Add the seven journeys from the TUI design spec:

```text
entrypoint
Config review
Resource discovery
machine mapping
restore consequence comparison
profile Git sync
Omarchy theme/fallback
```

Include terminal widths 140, 100, 80, and too-small state.

- [ ] **Step 8: Run full verification**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Expected: all PASS.

- [ ] **Step 9: Run real Omarchy manual acceptance**

On a disposable profile/profile clone:

```text
launch bare TUI under current Omarchy theme
change Omarchy theme while TUI runs and confirm style reload/fallback
review a real Config ambiguity without displaying sensitive contents
browse to a safe Git repo and track/update it
use editor/file manager/LazyGit/browser handoffs and return
map a test Resource path and confirm effective path everywhere
use Capture to select one provider and Capture All; confirm `Enter` approves,
`Esc` cancels, and Overview/provider/Sync state refreshes after success
compare Normal/Forced restore on a deliberately conflicting disposable target
apply only after reading consequences and verify actual operation matches selected plan
initialize/commit/push disposable profile repo; fetch/pull safe case
confirm staged/diverged Git state hands off rather than being modified
```

- [ ] **Step 10: Commit docs/integration tests**

```bash
git add README.md ROADMAP.md docs/manual-test-checklist.md \
  docs/adr/0017-tui-interactive-client-architecture.md internal/tui internal/app/app_test.go
git commit -m "docs: complete tui v1 workflow"
```

---

## Final review checklist

Before raising the final TUI v1 PR/PR series, verify every item explicitly:

```text
[ ] Profile Git Sync v1 prerequisite is merged and green
[ ] bare interactive command launches TUI
[ ] bare non-TTY command emits normal help and no escape sequences
[ ] explicit CLI commands never auto-launch TUI
[ ] TUI does not invoke omarchy-blueprint as a subprocess
[ ] shared workflow layer contains presentation-independent orchestration
[ ] CLI human/JSON regressions remain compatible
[ ] inspect path/config/resource exists in CLI/JSON
[ ] Overview is a typed decision inbox with navigation targets
[ ] persistent sidebar and command palette share the same actions
[ ] responsive layouts do not panic at small terminal sizes
[ ] Config screen shows provider classification and reason
[ ] Config Auto/Include/Exclude are all available through core + CLI and TUI uses those same semantics
[ ] shared diff hides sensitive/binary/oversized/control content
[ ] filesystem browser is non-destructive and non-recursive by default
[ ] browser path preview uses shared Blueprint ownership/Git semantics
[ ] Resources supports tracked/discover, all three strategies, exact untracked selection
[ ] Resources preserves portable/effective path semantics
[ ] Machines uses existing lifecycle/mapping validation and shared directory picker
[ ] generic provider screens do not invent unsupported per-item policies
[ ] Restore caches/displays both Normal and Forced plans
[ ] Restore clearly marks operations whose consequence changes under force
[ ] Restore approval regenerates/validates selected mode before mutation
[ ] no TUI-only restore authority exists
[ ] Sync performs no automatic fetch at startup
[ ] Sync uses Profile Git core and hands complex state to LazyGit
[ ] external editor/file-manager/browser/LazyGit commands use no shell
[ ] clipboard failure is non-fatal
[ ] TUI inherits terminal background
[ ] valid Omarchy palette maps semantic roles
[ ] missing/malformed palette falls back safely
[ ] NO_COLOR remains fully understandable
[ ] theme reload does not require installing a hook
[ ] network operations occur only by explicit action
[ ] manual acceptance covers disposable destructive targets only
[ ] go test ./... -count=1
[ ] go vet ./...
[ ] go build ./cmd/omarchy-blueprint
[ ] git diff --check
```
