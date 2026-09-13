# TUI v1 Design

**Status:** Approved design

**Date:** 2026-09-11

**ADR:** `docs/adr/0017-tui-interactive-client-architecture.md`

**Prerequisite:** `docs/superpowers/specs/2026-09-11-profile-git-sync-v1-design.md`

> **Historical design note:** This approved v1 design predates the dedicated
> Capture screen. The implementation clarification below records the current
> shipped navigation and interactions without changing the historical design.

## 1. Purpose

TUI v1 makes Omarchy Blueprint's mature state/safety model usable as a primary daily interactive experience without replacing the CLI, the running system, the user's editor, file manager, browser, or Git client.

The product question is not “how do we put CLI commands in boxes?” It is:

> How do we make Blueprint continuously answer what it sees, what is managed, what needs a decision, and exactly what will happen if the user acts?

The TUI is a native Go client of shared Blueprint workflow/domain operations. It never scrapes Blueprint CLI output.

## 2. Design principles

### 2.1 The running system is still the editor

Blueprint does not edit configuration file contents in the TUI. Users change Omarchy normally or launch their configured editor from a contextual action.

### 2.2 Stable navigation, fast workflows

Persistent sidebar navigation provides spatial stability. A `:` command palette provides fast access to global/current-screen actions without becoming the primary navigation model.

### 2.3 Consequences before destructive actions

For restore in particular, the UI must show the exact normal and forced plans before execution. “Force” is never a blind confirmation modifier.

### 2.4 Native core use, not command wrapping

The TUI calls Go workflow/domain APIs. It may launch external specialist applications, but it never shells out to `omarchy-blueprint` itself.

### 2.5 CLI semantic parity

If the TUI needs Blueprint information or semantics that do not exist yet, add them to core + CLI/JSON first or in the same implementation task before using them in the TUI.

UI-only conveniences—pane layout, clipboard, launching editor/file manager/browser/LazyGit—do not require equivalent Blueprint commands.

### 2.6 Omarchy/terminal-native appearance

The TUI uses semantic style roles, inherits terminal background, consumes the active Omarchy palette when safely available, and falls back to terminal ANSI/default colours without losing meaning.

### 2.7 No network side effects on startup

Opening the TUI never fetches Git remotes, installs packages, checks hosting APIs, or performs other network work automatically.

## 3. Technology

Use the current stable Charm v2 stack compatible with the repository's Go version:

```text
charm.land/bubbletea/v2
charm.land/lipgloss/v2
charm.land/bubbles/v2
```

Use `golang.org/x/term` or equivalent standard terminal detection for bare-command TTY dispatch.

Do not add a browser/file-manager framework or a second rendering framework.

A small pure-Go line-diff dependency is acceptable if needed for structured text diffs; it must not require external `diff` or Git for ordinary Config/restore text comparisons.

## 4. Process architecture

### 4.1 Layers

Target layering:

```text
┌─────────────────────────────────────────────┐
│ CLI (`internal/app`)                        │
│ Cobra parsing + text/JSON rendering         │
└──────────────────────┬──────────────────────┘
                       │
                shared workflow API
                       │
┌──────────────────────┴──────────────────────┐
│ TUI (`internal/tui`)                        │
│ Bubble Tea state + rendering + keybindings  │
└──────────────────────┬──────────────────────┘
                       │
                 workflow/domain
                       │
  providers / profile / machine / restore / profilegit
```

`internal/tui` must not import Cobra.

`internal/app` must not become a TUI component library.

### 4.2 Shared workflow package

Introduce a presentation-independent package such as:

```text
internal/workflow/
```

It owns composition currently trapped in CLI handlers when both clients need it.

Prefer focused services over one god object. Conceptually:

```go
type Session struct {
    ProfileDir string
    Machine    machine.Selection
    Home       string
}

type Inspector interface {
    Overview(ctx context.Context, session Session) (Overview, error)
    InspectPath(ctx context.Context, session Session, path string) (PathInspection, error)
    InspectConfig(ctx context.Context, session Session, path string) (ConfigInspection, error)
    InspectResource(ctx context.Context, session Session, id string) (ResourceInspection, error)
}

type RestoreWorkflow interface {
    Plan(ctx context.Context, session Session, force bool) (model.RestorePlan, error)
    Apply(ctx context.Context, session Session, plan model.RestorePlan) error
}
```

Exact interfaces can be split further, but shared workflows return structured domain data and never pre-rendered terminal strings.

CLI wrappers map these results to existing human/JSON envelopes. TUI updates map them to Bubble Tea messages.

## 5. Entrypoint behavior

### 5.1 Bare command

When stdin and stdout are interactive terminals:

```bash
omarchy-blueprint
```

launches the TUI using `--profile .` unless overridden by the existing global flag.

Examples:

```bash
omarchy-blueprint --profile ~/omarchy-profile
omarchy-blueprint --machine desktop --profile ~/omarchy-profile
```

launch the TUI with those existing global contexts.

### 5.2 Explicit command

```bash
omarchy-blueprint tui
```

always requests the TUI and fails cleanly when no terminal is available.

### 5.3 Non-interactive bare command

Bare invocation without a usable terminal prints normal root help and exits 0. It never emits escape sequences.

### 5.4 Existing commands

Any explicit existing subcommand keeps current behavior regardless of TTY.

## 6. Global layout

Default wide layout:

```text
┌ Blueprint ─ profile: main ─ machine: framework ─ 3 need review ─────────────┐
├──────────────┬───────────────────────────────────────┬───────────────────────┤
│ Overview     │                                       │                       │
│ Capture      │                                       │                       │
│ Packages     │                                       │                       │
│ Config       │              workspace                │       details         │
│ Resources    │                                       │       / preview       │
│ Themes       │                                       │                       │
│ Plugins      │                                       │                       │
│ Shell        │                                       │                       │
│ Hooks        │                                       │                       │
│ Defaults     │                                       │                       │
│ Machines     │                                       │                       │
│ Sync         │                                       │                       │
│ Restore      │                                       │                       │
├──────────────┴───────────────────────────────────────┴───────────────────────┤
│ ? help   / filter   : commands                         contextual actions     │
└──────────────────────────────────────────────────────────────────────────────┘
```

The header always identifies:

```text
profile name/path context
active machine overlay or portable-default mode
attention/drift summary
busy/error state when relevant
```

The footer is contextual and shows only currently valid shortcuts plus `?` and `:`.

The current sidebar order begins Overview, Capture, Packages. Capture is a
dedicated aggregate workflow: `Space` toggles the highlighted provider, `a`
selects changed providers, `c` captures the selected providers, and `C` runs
aggregate Capture All. Both capture actions require confirmation; `Enter`
confirms and `Esc` cancels.

## 7. Responsive behavior

The interface must not assume one desktop-terminal width.

Recommended breakpoints:

```text
>= 120 columns   sidebar + workspace + details
90–119           sidebar + workspace; details opens as focused pane/overlay
70–89            collapsible sidebar; one content pane at a time
< 70 or < 18 rows
                 non-crashing “terminal too small” view with required dimensions
```

Do not truncate safety-critical consequence text without a way to scroll/view it.

Use terminal cell width-aware measurement for Unicode.

## 8. Focus and navigation model

Global keys:

```text
j / k / arrows    move in active list
h / l             adjacent pane where applicable
Tab / Shift+Tab   cycle focusable panes
Enter             inspect / activate
Space             toggle policy/selection when screen defines it
/                 filter active list
:                 command palette
?                 contextual help
Esc               close palette/modal/detail or go back one transient level
q                 quit when no transient UI consumes it
ctrl+c            quit safely
```

On Capture, `Space` toggles the highlighted provider; `a` selects changed
providers; `c` captures the selected providers; and `C` runs Capture All. The
capture modal accepts `Enter` and cancels with `Esc`.

Do not bind destructive operations to an unconfirmed single keypress.

The command palette lists global and current-screen actions with fuzzy filtering. It invokes the same action functions as shortcuts; no action exists only inside the palette.

Mouse input is deferred from v1.

## 9. Asynchronous work model

Filesystem scans, Git inspection, provider detection, diffs, and restore planning may be slow.

Rules:

- run slow work as Bubble Tea commands, not inside `View`;
- tag requests with an incrementing generation/request ID;
- ignore stale results when selection changed before completion;
- propagate contexts so expensive inspections can be cancelled when practical;
- never mutate profile/live state from background goroutines outside the serialized workflow action;
- show a spinner/status line for meaningful waits;
- errors are state/messages, not process crashes.

No periodic full-system status scan. Refresh intelligently after relevant actions and provide an explicit refresh key.

## 10. Overview screen

Overview is a **decision inbox**, not a metrics dashboard.

It answers:

```text
What needs my attention?
What is drifted?
What is blocked?
What is clean?
What profile Git work remains?
```

Suggested sections:

```text
Needs review
  Config       3 ambiguous candidates
  Resources    repo has uncaptured local state under strategy git

Drift
  Packages     2 differences
  Config       1 managed file differs

Profile sync
  4 managed profile changes
  1 commit ahead (last fetched state)

Healthy
  Themes
  Plugins
  Shell
  Hooks
```

### 10.1 Attention item model

Use structured navigation targets:

```go
type AttentionItem struct {
    Severity string
    Provider string
    Kind     string
    Ref      string
    Summary  string
    Screen   ScreenID
}
```

Selecting an item navigates to and focuses the owning object.

### 10.2 Sources

Overview may compose:

- provider status/diff already calculated by Blueprint;
- Config scan classifications and reasons;
- Resource runtime state;
- machine selection/dormant mapping information;
- local Profile Git status;
- check warnings that are already available without destructive work.

It does not fetch remote Git state automatically.

## 11. Generic provider screens

Packages, Themes, Plugins, Shell, Hooks, and Defaults share a common provider-screen vocabulary where possible:

```text
saved / desired
current
state / provenance
semantic drift
available actions
```

These screens should not invent provider-specific mutation capabilities that the CLI does not have.

They may offer:

```text
r / refresh
c / capture provider using existing capture semantics
d / inspect semantic diff/detail
R / navigate to restore plan scoped to provider when supported
```

Any capture action writes the profile only after a confirmation dialog that names the provider and semantic changes currently observed. TUI v1 does not introduce a new long-lived capture staging protocol; it uses the same current-state inspection and capture operation as CLI and refreshes after completion.

### 11.1 Current Capture screen clarification

The shipped dedicated Capture screen composes provider capture operations into
one aggregate workflow. `c` captures only the selected providers; `C` performs
aggregate Capture All. Neither action auto-commits or pushes. After a successful
capture, reload the profile, refresh provider status and Overview, and refresh
local Profile Git/Sync status so newly managed profile changes are immediately
visible.

## 12. Config screen

Config is a first-class review screen because Config v2 exposes discovery/classification and explicit policy.

### 12.1 Questions answered

```text
What configuration did Blueprint inspect?
How was this path classified?
Why?
Is it currently managed, included, excluded, delegated, sensitive, volatile, or ambiguous?
How does live content differ from baseline/profile content?
What policy can I safely choose?
```

### 12.2 Layout

Wide view:

```text
Candidates                    Details                         Diff
────────────────────          ───────────────────────         ──────────────
! .zshrc                      classification: ambiguous       ...
✓ .config/hypr/...            reason: baseline uncertain
× .config/chromium/...        policy: auto
                              hash/mode metadata
                              [Include] [Exclude]
```

### 12.3 Classification display

Every candidate shows classification text/icon and the provider-generated `Reason`. Do not reverse-engineer a second reason in TUI code.

At minimum visually distinguish:

```text
managed/customized
unchanged baseline
added
deleted
ambiguous
explicitly included
excluded
delegated
volatile
sensitive
oversized
unsupported
unmanaged symlink
```

### 12.4 Policy operations

Space or action palette may change policy only through shared Config policy operations:

```text
Auto/default
Include
Exclude
```

`Include` and `Exclude` use the existing Config provider policy. TUI v1 adds one missing headless primitive for parity:

```bash
omarchy-blueprint auto config:<path>
```

`Auto/default` removes only the exact explicit include/exclude record for that canonical path; it does not erase ancestor or descendant policy records. The candidate is then governed by normal automatic discovery/classification. If an ancestor exclusion still applies, the UI explains that inherited policy rather than pretending the path became automatic.

If a classification cannot legally be included, the action is disabled with the provider reason rather than attempted optimistically.

After a policy mutation, rescan and preserve focus on the same logical path where possible.

### 12.5 Config diff

For a selected candidate, choose the most informative safe comparison:

```text
baseline ↔ live       capture-review context
profile desired ↔ live status/drift context
profile baseline ↔ current baseline when explaining upgrades where data exists
```

The detail pane labels both sides explicitly; never show an unlabeled diff where “before” could mean either baseline or saved desired state.

Sensitive/binary/oversized content uses metadata-only states.

### 12.6 External actions

For live regular files:

```text
e   open in editor
 o  open containing directory in file manager
 y  copy live path
```

These do not change Config policy automatically.

## 13. Shared diff model and viewer

### 13.1 Core representation

Introduce presentation-independent bounded diff structures, for example:

```go
type DiffKind string

const (
    DiffText       DiffKind = "text"
    DiffBinary     DiffKind = "binary"
    DiffSensitive  DiffKind = "sensitive"
    DiffTooLarge   DiffKind = "too-large"
    DiffMetadata   DiffKind = "metadata"
    DiffUnavailable DiffKind = "unavailable"
)

type DiffDocument struct {
    Kind      DiffKind
    Title     string
    OldLabel  string
    NewLabel  string
    Hunks     []DiffHunk
    Metadata  []DiffFact
}

type DiffLine struct {
    Kind    string // context | add | remove
    OldLine int
    NewLine int
    Text    string
}
```

The domain builder owns content bounds/sensitivity classification. The TUI owns viewport, wrapping, and styling.

### 13.2 Safety

Before text rendering:

- use Blueprint sensitive scanning where content is local/untrusted;
- cap input at 2 MiB per side and 20,000 lines;
- treat NUL-containing content as binary;
- strip/escape C0/C1 terminal control characters except safe tab/newline processing;
- do not emit raw ANSI escape sequences from file contents;
- truncate individual pathological lines with an explicit marker while keeping full-file preview unavailable rather than hanging rendering.

### 13.3 Views

Default: unified.

Optional: side-by-side when enough width exists.

Keys:

```text
j/k or arrows   scroll
{ / }           previous / next hunk
s               unified / side-by-side
w               toggle visible whitespace markers
Home/End         start/end
Esc              return
```

Added/removed meaning is shown through `+`/`-` plus styling; colour alone is never required.

## 14. Filesystem browser / picker

The browser is inspired by Yazi's speed and spatial model but intentionally much smaller.

### 14.1 Modes

```text
BrowseResource    inspect/select path to track or update
PickDirectory     choose a machine Resource mapping destination
BrowseReadOnly    inspect an affected path from another screen
```

Actions are mode-specific.

### 14.2 Start and free navigation

Resource discovery starts at `$HOME`.

The user can navigate upward to `/` and anywhere permitted by normal filesystem permissions.

Hidden entries are visible by default because dotfiles are central to Blueprint.

Directories are listed before files, then case-insensitive natural name order where practical.

### 14.3 Bookmarks / jumps

`g` opens a jump list built from paths Blueprint already knows:

```text
Home
Config (~/.config when present)
Profile root
Tracked portable Resource roots
Selected-machine effective Resource roots
~/Projects or ~/Code when present
/mnt, /media, /run/media/$USER when present
Recently visited directories for this TUI session
```

Bookmarks are UI convenience state. Custom persisted bookmarks are deferred.

### 14.4 Filtering

`/` filters the current directory only. It does not recursively index the user's home directory.

A jump-to-path action may accept a typed absolute or `~/...` path and then navigate there after canonical validation.

### 14.5 Semantic preview

Selecting an entry asynchronously requests `PathInspection`.

Representative model:

```go
type PathInspection struct {
    Path              string
    Type              string
    Mode              string
    Size              int64
    SymlinkTarget     string
    OwnershipProvider string
    ResourceID        string
    ResourceDefault   string
    ResourceEffective string
    Git               *GitInspection
    SuggestedStrategy string
    StrategyReason    string
    BlockedReason     string
}
```

Git inspection for a directory may show:

```text
worktree root or not
origin (sanitized)
HEAD
branch
staged count
unstaged count
untracked count
conflict/operation blockers relevant to Resource capture
```

The strategy suggestion uses existing Resources semantics:

```text
portable clean Git root       → git
portable dirty Git root       → git+diff recommendation
non-Git regular tree/file     → copy
unsupported/sensitive/owned   → blocked/explained
```

A suggestion is never auto-applied without the user choosing/confirming the strategy.

### 14.6 Browser does not mutate filesystem

No delete, rename, copy, move, chmod, archive, extraction, or arbitrary file creation actions.

The only mutations initiated from browser context are Blueprint policy operations such as tracking the selected Resource or setting a machine mapping after validation.

## 15. CLI inspection capability required by the browser

Before relying on rich `PathInspection` data exclusively in TUI, expose it through core + CLI/JSON:

```bash
omarchy-blueprint inspect path <path>
omarchy-blueprint inspect config <logical-path>
omarchy-blueprint inspect resource <id>
```

`inspect path` returns filesystem type, Blueprint ownership, existing Resource identity, bounded Git summary, suggested Resource strategy/reason, and blockers.

`inspect config` returns the Config candidate classification/reason and relevant live/baseline/profile metadata.

`inspect resource` returns saved Resource state plus effective path/runtime Git/copy status and selected machine context.

Human output is concise; JSON carries structured data used for automation. TUI calls the same Go workflow directly, not these commands.

## 16. Resources screen

Resources has two subviews:

```text
Tracked
Discover
```

### 16.1 Tracked list

Show:

```text
ID
strategy
portable path
effective path when different
machine overlay
runtime satisfaction/drift
Git dirty summary where relevant
```

Selecting a Resource opens details appropriate to strategy.

For `git+diff`, details separately show:

```text
remote
revision
staged tracked changes
unstaged tracked changes
selected untracked files
other untracked files (informational)
```

For `git`, dirty local state is clearly labelled informational/unmanaged by that strategy.

For `copy`, show snapshot/runtime hashes and filesystem metadata rather than pretending it has Git provenance.

### 16.2 Discover flow

From `Discover`, open the shared browser.

Choosing a path leads to inspection before any tracking action.

Example:

```text
~/dotfiles

Detected
  Git repository
  origin: github.com/example/dotfiles
  HEAD: abc123...
  staged: 1
  unstaged: 3
  untracked: 5

Recommended: git+diff
Reason: portable Git provenance exists and local tracked state differs from HEAD

( ) git
(*) git+diff
( ) copy
```

Descriptions explain each strategy in product terms.

### 16.3 Untracked selection

When `git+diff` is selected, show exact eligible untracked regular files with checkboxes.

Selection obeys existing exact-path semantics:

- no globs;
- no directory-recursive shorthand;
- sensitive/ignored/unsafe/oversized/symlink/special entries disabled with reason;
- new untracked files are not silently selected on recapture.

Commit tracking through the same `TrackOptions` semantics as CLI.

### 16.4 Contextual actions

Representative actions:

```text
d   Blueprint diff/details
p   change Resource strategy/policy
u   untrack (confirmation; does not delete live bytes)
m   edit current-machine mapping
 e  open root in editor
 o  open root in file manager
 g  open repository in LazyGit when Git-backed
 r  open remote in browser when convertible
 y  copy effective path
```

Changing strategy or untracking shows a confirmation summarizing profile-policy consequences.

## 17. Machines screen

### 17.1 Questions answered

```text
Which machine overlay is selected locally?
What other overlays exist?
Where does each Resource live by default versus on this machine?
Which mappings are dormant or invalid in current context?
```

### 17.2 Layout

```text
Machines                      Resource paths
──────────────────            ───────────────────────────────────────
> framework  selected         dotfiles   ~/dotfiles  → ~/dotfiles
  desktop                     projects   ~/Projects  → ~/Code
  work-laptop                 music      ~/Music     → /mnt/media/music
```

Default/no-override rows are labelled `default`; mapped rows are labelled `override`.

### 17.3 Operations

Use existing Machine Overlay operations:

```text
create
use
clear selection
rename
remove
map Resource root
unmap Resource root
```

Create uses the same hostname-derived suggestion as CLI and an inline one-line input, not a full editor.

Mapping opens the shared directory picker in `PickDirectory` mode and then runs the same overlap/ownership validation as CLI.

Removal/rename wording makes clear that the operation changes portable placement policy and never moves/deletes live Resource bytes.

## 18. Restore consequence model

### 18.1 Compute both modes

On entering Restore or explicit refresh, calculate:

```go
normal := Plan(force=false)
forced := Plan(force=true)
```

from the same freshly loaded profile/current state.

Neither plan mutates the machine.

### 18.2 Compare by stable operation identity

Operations already carry stable IDs. Build a comparison model keyed by operation ID/resource plus skipped/conflict identity.

Conceptually:

```go
type RestoreComparison struct {
    Normal model.RestorePlan
    Force  model.RestorePlan
    Items  []Consequence
}

type Consequence struct {
    Provider      string
    Resource      string
    NormalOutcome Outcome
    ForceOutcome  Outcome
    Difference    string
    Risk          model.Risk
}
```

Outcomes include:

```text
create
modify
replace
delete
skip
conflict/no-operation
unchanged/no-op
```

Do not infer destructive meaning only from the word `force`; derive it from concrete typed operations (`FileWrite`, `FileDelete`, `SymlinkWrite`, `Copy`, Git operations) and skipped reasons.

### 18.3 Screen

Header toggle:

```text
Restore mode:  [ Normal ]   Forced
```

Switching changes the displayed plan immediately without recomputing when both cached plans are still current.

Summary:

```text
Normal:  12 ready   2 skipped   1 blocked
Forced:  14 ready   1 skipped   3 replacements
```

Resource/provider list indicates items whose outcome differs between modes.

Selecting an item shows exact consequences and, when safe, current-versus-desired diff.

### 18.4 Approval

Applying restore always applies the currently visible mode's freshly validated plan.

Before execution show a confirmation with counts of:

```text
creates
modifications
replacements
deletes
external commands
```

If the selected mode contains a deletion/replacement/high-risk operation, require an explicit confirmation step; Enter on the list itself never applies.

After confirmation but before first mutation, regenerate/validate the selected plan or otherwise enforce existing executor preconditions so stale preview state cannot silently authorize different target bytes.

### 18.5 CLI parity

The normal plan equals existing CLI dry-run without `--force`.

The forced plan equals existing CLI dry-run with `--force`.

No TUI-only restore authority exists.

## 19. Sync screen

Requires Profile Git Sync v1.

### 19.1 Questions answered

```text
Is this profile a Git repository?
What managed/unmanaged changes exist?
Is there an origin/upstream?
What branch/HEAD am I on?
Am I ahead/behind according to the last fetched refs?
What can Blueprint safely do here?
```

### 19.2 No repository

```text
Profile is not version-controlled with Git

[Initialize Git repository]
```

After initialization:

```text
Origin: not configured
[Set origin]
```

One-line URL input is acceptable; Blueprint does not implement hosting-provider login/repository creation.

### 19.3 Repository view

```text
Repository
  Branch       main
  Origin       git@github.com:example/omarchy-profile.git
  Upstream     origin/main
  HEAD         abc1234

Working tree
  4 managed changes
  1 unmanaged change

Remote (last fetched)
  1 ahead
  0 behind
```

Actions:

```text
d   review managed profile diff
c   commit managed changes
p   push
f   fetch/refresh remote refs
l   pull (only when safe/ff-only)
r   open origin in browser
 g  open profile repo in LazyGit
```

The screen reflects restrictions from the profile-Git core rather than reimplementing Git state policy.

### 19.4 Commit flow

Show changed managed paths and a one-line editable commit-message field prefilled with the deterministic suggested message.

Commit calls the core managed-only commit operation.

After success, refresh status and offer Push.

“Commit & push” is a TUI composition of core Commit then Push and stops if Commit fails/no safe push state exists.

### 19.5 Advanced Git state

When staged state, divergence, detached HEAD, conflicts, or other unsupported workflow blocks a simple action:

```text
Blueprint cannot safely perform this Git workflow.
Reason: repository already has staged changes.

[Open LazyGit]
[Copy repository path]
```

Do not offer hidden force/reset/stash actions.

## 20. External tool handoff

### 20.1 Editor

Preference:

```text
$VISUAL
$EDITOR
```

If neither is set, disable the action with guidance rather than selecting an arbitrary editor in Blueprint policy.

Parse configured command/arguments without invoking a shell. Append the selected path as an argument.

For directories, editors that support directory roots receive the directory path.

### 20.2 File manager

Use desktop XDG handling by default:

```text
xdg-open <directory>
```

For a file, open its containing directory rather than treating `xdg-open file` as a guaranteed “reveal” operation.

A future explicit Blueprint file-manager command preference may replace this; it is not required for v1.

### 20.3 Browser

Use `xdg-open` only with validated HTTP(S) URL derived from known remote metadata.

### 20.4 LazyGit

If `lazygit` is on PATH, suspend the TUI and run it with working directory set to the selected Git repository.

No shell interpolation.

On return, refresh the relevant Resource/Sync state because the external tool may have changed it.

### 20.5 Clipboard

Provide a narrow clipboard adapter.

Prefer terminal-native/OSC52 support available through the chosen TUI stack when usable; an Omarchy/Wayland fallback may use `wl-copy` if present. Failure is non-fatal and never blocks inspection.

Clipboard payloads are plain displayed values such as path/URL/commit hash, never hidden sensitive contents.

## 21. Theming

### 21.1 Semantic roles

```go
type Palette struct {
    Foreground    string
    Muted         string
    Accent        string
    Success       string
    Warning       string
    Error         string
    Added         string
    Removed       string
    Border        string
    BorderFocused string
    Selection     string
}
```

### 21.2 Omarchy source

Current Omarchy versions expose the active theme palette through a `colors.toml` in the current theme state. TUI v1 should use an injectable locator that first checks the current XDG-state Omarchy location and is easy to update when Omarchy changes internals.

Known current convention at design time:

```text
$XDG_STATE_HOME/omarchy/current/theme/colors.toml
```

with normal XDG fallback equivalent to `~/.local/state/...`.

Do not make this path part of Blueprint profile schema or correctness logic.

Read only known colour keys such as:

```text
background
foreground
accent
color1..color8
selection_background
```

Unknown keys are ignored.

### 21.3 Mapping

Suggested mapping:

```text
foreground      → foreground
accent          → accent
success/added   → color2 / green
warning         → color3 / yellow
error/removed   → color1 / red
muted/border    → color8 / dim fallback
selection       → selection_background when usable, otherwise reverse/accent
```

Avoid painting the whole terminal background. Inherit it.

### 21.4 Fallback

If palette file is missing/malformed/unreadable:

- use terminal default foreground/background;
- use ANSI semantic colours, which naturally follow the terminal's configured palette;
- never fail startup.

If `NO_COLOR` is non-empty, disable semantic colour use and rely on symbols/attributes/layout.

### 21.5 Live reload

Poll a cheap fingerprint of the palette path/target no more frequently than once per second while TUI is active. If it changes, reload and rerender styles.

Do not require installing a theme-set hook.

A future Omarchy hook may send a signal/event to improve immediacy without changing the palette API.

## 22. Help and discoverability

`?` opens context-sensitive help rather than one giant key dump.

Help includes:

```text
navigation
current-screen actions
global actions
meaning of state symbols
safety notes where relevant
```

The command palette can search action names such as:

```text
Add resource
Review config candidates
Switch machine
Preview restore
Open in editor
Open LazyGit
Refresh Git remote status
```

Disabled actions remain searchable only when showing the reason is useful; otherwise hide them to reduce noise.

## 23. Notifications and errors

Use a small non-blocking message/toast area for successful convenience actions:

```text
Copied ~/Code/project
Opened LazyGit
Machine switched to desktop
```

Domain errors that need a decision use an inline panel/modal with exact reason and next safe actions.

Never hide a provider/core error behind a generic “operation failed” when a structured reason is available.

Do not log full sensitive paths/content into a persistent TUI log by default.

## 24. State refresh after actions

After any mutation:

```text
Config policy        → reload profile + rescan Config
Resource track       → reload profile + refresh Resources/Overview
Machine operation    → reload machine selection + Resources/Overview
Capture              → reload profile + provider status + Overview + Sync local status
Restore              → rerun verification/status + Overview
Profile Git action   → refresh Profile Git status; pull also reloads entire profile/session
External editor      → refresh owning provider/path on return
LazyGit              → refresh Git inspection/Profile Git state on return
```

Do not assume in-memory profile structs remain authoritative after Git pull or external tools.

## 25. Testing strategy

### 25.1 Domain/workflow tests

All new inspection, restore comparison, and shared workflow logic is tested without terminal rendering.

### 25.2 Bubble Tea update tests

Drive model `Update` with deterministic key/window/data messages and assert:

- selected screen/focus;
- commands requested;
- modal state;
- restore mode toggle;
- stale async result rejection;
- action disabled/enabled behavior.

Avoid brittle full-screen golden tests as the only coverage.

### 25.3 Rendering snapshots

Use targeted text snapshots for stable small components:

```text
header
sidebar
restore consequence summary
diff metadata state
terminal-too-small state
```

Normalize ANSI where tests need semantic text rather than exact colour escape sequences.

### 25.4 Real integration tests

Use temporary filesystem/Git fixtures for:

```text
Resource browser → inspect dirty Git → choose git+diff → track
machine picker → map root → effective path reflected
restore normal/force plans differ → toggle shows correct consequences
profile Git Sync screen state from real temp repository
external process command construction (fake executable, no shell)
```

Do not launch real editors/browsers/file managers in CI.

## 26. Manual acceptance journeys

### 26.1 Bare entry

```text
interactive bare command → TUI
explicit `tui` → same TUI
piped/non-TTY bare command → CLI help, no escape codes
```

### 26.2 Config review

```text
Overview shows ambiguous Config
→ Enter navigates to candidate
→ reason visible
→ inspect baseline/live diff
→ Include
→ candidate refreshes as managed
→ launch editor and return
```

### 26.3 Resource discovery

```text
Resources → Discover
→ free-browse from HOME with dotfiles visible
→ jump to bookmark
→ select dirty Git repo
→ preview shows Git state + git+diff recommendation
→ select eligible untracked files
→ track
→ portable path/strategy visible in Tracked list
```

### 26.4 Machine mapping

```text
Machines
→ choose desktop
→ map Resource using shared directory picker
→ portable/effective paths shown side by side
→ Resources screen immediately uses mapped root
```

### 26.5 Restore consequence comparison

```text
Restore
→ normal plan shows skip/conflict
→ toggle Forced
→ changed operations/replacements/deletions explicit
→ inspect safe file diff
→ toggle back Normal
→ approve Normal
→ execution matches visible Normal plan semantics
```

Repeat on disposable fixture approving Forced and confirm it matches visible forced semantics.

### 26.6 Profile Git

```text
Sync → initialize
→ set origin
→ capture profile change
→ review managed diff
→ commit with edited one-line message
→ push
→ explicit fetch refresh
→ open LazyGit and return
```

### 26.7 Theme

```text
launch under dark Omarchy theme
→ semantic palette fits terminal
change Omarchy theme while TUI remains open
→ palette reloads without restart
remove/make palette unreadable in fixture
→ TUI falls back and remains usable
NO_COLOR=1
→ meaning remains legible without colour
```

## 27. CLI capability matrix

| TUI capability | Required core/CLI surface |
| --- | --- |
| provider drift/status | existing `status` / `diff` + shared workflow extraction |
| Config classification/reason | existing Config scan JSON; `inspect config` for focused detail |
| Config Auto/Include/Exclude | existing include/exclude policy + new `auto config:<path>` exact-policy reset |
| Resource inspect/recommendation | new `inspect path` / `inspect resource` |
| Resource track/update/untracked selection | existing `track` options |
| Resource untrack | existing `untrack` |
| machine lifecycle/mapping | existing `machine` commands |
| restore normal consequence plan | existing `restore --dry-run` semantics |
| restore force consequence plan | existing `restore --dry-run --force` semantics |
| profile repository status/diff/sync | Profile Git Sync v1 CLI |
| editor/file manager/browser/LazyGit/clipboard | UI convenience; underlying paths/remotes are exposed by CLI |

## 28. Scope boundaries for TUI v1

Included:

- bare-command interactive launch and explicit `tui` command;
- persistent sidebar + command palette + contextual help;
- Overview decision inbox;
- provider status/capture screens;
- Config review/policy/diff;
- Resources tracked/discovery/strategy/untracked workflow;
- Yazi-inspired bounded filesystem browser;
- Machines management/mapping UI;
- shared bounded diff viewer;
- restore normal/forced consequence comparison and approval;
- Profile Git Sync screen;
- editor/file-manager/browser/LazyGit/clipboard handoff;
- Omarchy/terminal semantic theming with live best-effort reload;
- responsive terminal layouts;
- testable async refresh/error handling.

Deferred:

- mouse-first UX;
- custom persisted filesystem bookmarks;
- recursive whole-home fuzzy index;
- embedded editor;
- file copy/move/delete/archive features;
- merge/rebase/conflict-resolution Git UI;
- GitHub/GitLab account/repository creation APIs;
- Google Drive/Dropbox backup;
- agent-generated explanations/commit messages;
- machine-specific packages/monitor/hardware overlays beyond existing path mappings;
- multiple profile registry/switcher;
- Omarchy Shell-native graphical client.

## 29. Success criteria

TUI v1 is successful when a user can complete the following without memorizing Blueprint commands while the same semantics remain scriptable from CLI:

```text
open Blueprint
→ understand what needs attention
→ review ambiguous Config and set policy
→ discover and track a Resource through a filesystem browser
→ understand Git strategy choices
→ edit machine path mapping
→ compare normal versus forced restore consequences
→ execute an approved restore
→ review profile Git changes
→ commit and push
→ hand off to editor/file manager/browser/LazyGit when specialist work is needed
```

At every destructive boundary, the user can answer **what will happen before they approve it**.
