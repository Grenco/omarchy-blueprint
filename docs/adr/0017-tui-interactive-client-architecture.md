# ADR 0017: TUI Interactive Client Architecture and Capability Parity

## Status

Accepted

## Context

Omarchy Blueprint has intentionally built its semantic state model, safety rules, Config policy, Portable Resources, dirty Git state, and machine path overlays before adding its primary interactive experience. The roadmap has always described the initial product as CLI + TUI and identifies Go + Bubble Tea as the preferred TUI stack.

The TUI now needs to make those capabilities substantially easier to discover and operate without creating a second configuration model or a second implementation of Blueprint semantics.

Several product requirements shape the architecture:

- the running Omarchy system remains the configuration editor;
- the TUI should help users inspect, understand, select policy, capture, plan, restore, and synchronize the profile;
- a user who installs or uses only the CLI must still have access to every Blueprint semantic capability and every piece of Blueprint state the TUI relies on;
- the TUI may compose multiple existing operations into a better workflow, but it must not hide policy or safety rules that exist only in the UI;
- filesystem browsing, diffs, restore consequences, machine mappings, and profile Git state need richer presentation than line-oriented CLI output;
- editing files, advanced file management, browser navigation, and complex Git operations are better delegated to specialist tools;
- Omarchy users expect the interface to respect the active Omarchy/terminal theme;
- restore can affect valuable user files, so the UI must answer “if I do this, what exactly will happen?” before approval.

The current CLI orchestration lives primarily under `internal/app` and is coupled to Cobra rendering. A TUI must not shell out to `omarchy-blueprint --json` or scrape CLI output to reuse behavior. Shared behavior belongs below both presentation layers.

## Decision

### 1. The TUI is the primary interactive client

Use **Go + Bubble Tea v2**, with Lip Gloss v2 and Bubbles v2 where useful.

The TUI is a full-screen, keyboard-first interface with a persistent left sidebar, a main workspace, optional detail/preview panes, and contextual key hints.

The stable top-level navigation is:

```text
Overview
Packages
Config
Resources
Themes
Plugins
Shell
Hooks
Defaults
Machines
Sync
Restore
```

Screens may be hidden or marked unavailable when the associated provider is unsupported, but navigation order remains stable.

### 2. Bare interactive invocation launches the TUI

When both stdin and stdout are attached to a terminal:

```bash
omarchy-blueprint
```

launches the TUI.

The explicit form is also supported:

```bash
omarchy-blueprint tui
```

All existing subcommands keep their current behavior.

When bare `omarchy-blueprint` is executed without an interactive terminal, it must not attempt to start a TUI. It prints normal CLI help and exits successfully. Explicit `omarchy-blueprint tui` without a usable terminal fails with actionable guidance.

TUI v1 uses the same profile-path resolution as the CLI. It does not introduce a profile registry or automatic profile discovery beyond the active `--profile` path.

### 3. CLI capability parity is a product invariant

Blueprint semantic capabilities and Blueprint-derived information must be available through core APIs and CLI/JSON before or alongside their TUI presentation.

Examples:

```text
TUI Config policy change
→ same Config auto/include/exclude operations exposed by CLI

TUI Resource strategy selection
→ same track/update semantics used by CLI

TUI machine selection/mapping
→ same machine commands/domain operations

TUI restore consequence view
→ same restore planners used by CLI dry-run and --force

TUI Sync screen
→ same profile-Git status/init/remote/commit/fetch/pull/push operations exposed by CLI
```

The TUI is allowed to compose multiple primitives into one workflow. “Commit & push” may call the same commit and push operations sequentially without requiring a dedicated combined CLI command.

Presentation conveniences are not new Blueprint semantics and are exempt from one-for-one CLI commands. These include:

- opening a file or directory in the user’s editor;
- opening a directory in the desktop file manager;
- opening a Git remote in the browser;
- suspending into LazyGit for advanced Git work;
- copying a displayed path or URL to the clipboard.

The underlying path, remote, Git state, and other Blueprint information must still be visible through CLI/JSON.

### 4. CLI and TUI share native Go workflows, not subprocess protocol

Introduce presentation-independent workflow/application services below `internal/app` and `internal/tui`.

Conceptually:

```text
providers / profile / restore / machine / profile-git
                     ↑
              workflow services
               ↑             ↑
             CLI             TUI
          Cobra/text       Bubble Tea
```

The TUI must not invoke the Blueprint binary to perform Blueprint operations.

The CLI should progressively move orchestration that the TUI also needs into shared workflow services while retaining Cobra argument parsing and human/JSON rendering in `internal/app`.

### 5. Overview is a decision inbox

The landing screen answers:

```text
Which profile is active?
Which machine overlay is active?
Is the machine in sync with the profile?
What needs a user decision?
What is drifted?
What is unsafe or blocked?
What profile-Git work is pending?
```

Overview aggregates already-available provider status, Config ambiguity, Resources state, machine warnings, restore/check information, and local profile-Git status. Selecting an item navigates to the owning screen rather than duplicating its full interaction.

No network request is performed merely because Overview opened. Remote Git freshness requires an explicit refresh/fetch action.

### 6. Restore is consequence-first and compares normal versus force

Restore is the strongest safety surface in the TUI.

The screen computes both plans from the same underlying planner:

```text
normal plan  = force false
forced plan  = force true
```

The user can toggle between **Normal** and **Forced** modes before applying anything.

For the selected operation/resource, the UI explains concrete consequences:

- what will be created;
- what will be modified;
- what will be replaced;
- what will be deleted;
- what will remain untouched;
- what will be skipped;
- what conflicts remain;
- why normal and force differ.

Where bytes are safely previewable, the same shared diff viewer shows current versus desired content. Sensitive, binary, oversized, or otherwise unsafe content is represented by metadata and explanation instead of raw bytes.

Force is never represented as a generic “more aggressive” switch. The user sees the changed plan before approval.

### 7. Filesystem browsing is Yazi-inspired but Blueprint-specific

Provide a reusable browser/picker with fast spatial navigation, visible dotfiles, filter/jump support, bookmarks, and a semantic preview pane.

The browser is not a file manager. It does not copy, move, rename, delete, archive, or edit arbitrary filesystem entries.

The right-hand preview answers “what does this path mean to Blueprint?” and may show:

- path type and mode;
- known provider ownership;
- tracked Resource identity;
- portable and effective Resource paths;
- Git worktree/remote/revision/dirty summary;
- Resource strategy recommendation and reason;
- Config ownership/delegation where relevant;
- safety blockers.

The same component is reused for Resource discovery and machine Resource-path selection with mode-specific allowed actions.

### 8. Diff is a shared presentation capability

Provide one bounded, control-character-safe diff model/viewer for Config, Resources where appropriate, restore consequences, and profile-Git changes.

Unified diff is the default because it works at narrower terminal widths. Side-by-side view is available when the terminal is wide enough.

The component explicitly represents:

```text
text diff
new file
deleted file
binary change
permission/mode change
symlink-target change
sensitive content hidden
preview too large
content unavailable
```

No screen invents its own raw diff renderer.

### 9. Specialist tools are first-class handoff targets

Blueprint owns policy and orchestration, not editing or advanced Git/file-management features.

Contextual actions may suspend the TUI and launch:

```text
$VISUAL / $EDITOR   edit file or directory
xdg-open            open/reveal path using desktop defaults
xdg-open            open convertible HTTP(S) Git remote
lazygit             advanced Git work in selected repository
```

Subprocesses are executed directly without an interpolating shell. Returning from the subprocess causes the relevant Blueprint state to refresh.

If a tool is unavailable, the TUI remains functional and displays a non-fatal actionable message.

### 10. Profile Git sync is first-class but deliberately shallow

TUI v1 includes a Sync screen backed by an independently implemented Profile Git Sync v1 capability.

Blueprint handles simple, safe profile-repository operations:

```text
status
initialize repository
show/set/remove origin
fetch
commit Blueprint-managed profile changes
fast-forward-only pull
push / establish upstream
```

Blueprint does not implement merge conflict resolution, rebasing, interactive staging, branch surgery, or history rewriting. Complex repository state is explained and handed off to Git/LazyGit.

Profile Git synchronization is an implementation prerequisite for the Sync screen and is specified/planned separately from the TUI itself.

### 11. Theming uses semantic roles with graceful fallback

The TUI defines semantic roles rather than a fixed Blueprint colour scheme:

```text
foreground
muted
accent
selected
success
warning
error
added
removed
border
border-focused
```

Rendering order:

1. when a valid current Omarchy palette is safely discoverable, map semantic roles from its `colors.toml` values;
2. otherwise use terminal-native ANSI/default colours so the terminal theme remains authoritative;
3. when `NO_COLOR` is set or colour capability is unavailable, preserve meaning through text markers, attributes, and layout without relying on colour.

The main canvas does not paint a hard-coded background colour. It inherits the terminal background.

Omarchy’s current palette is treated as best-effort UX context, not a correctness dependency. Missing, malformed, or relocated Omarchy theme state must not prevent the TUI from starting.

The TUI observes the palette path for changes using a lightweight periodic stat/reload mechanism while running. It does not require users to install a Blueprint theme hook. A future Omarchy-native hook can optimize notification without changing the semantic-role API.

### 12. Keyboard-first interaction is stable and discoverable

Global conventions:

```text
j/k or arrows   move selection
h/l             move between panes where applicable
Tab             cycle focus
Enter           inspect / activate
Space           toggle selection/policy where applicable
/               filter current list
:               command palette
?               contextual help
Esc             close/back/cancel current transient UI
q               quit when no modal/transient view consumes it
```

Contextual actions such as edit/open/copy/diff/LazyGit appear in the bottom key-hint bar and command palette instead of being required knowledge.

Mouse support is not required for v1.

## Scope and sequencing

Implementation is split into two independently reviewable vertical slices:

```text
Profile Git Sync v1
        ↓
TUI v1
```

Profile Git Sync v1 provides the core + CLI capability required by the TUI Sync screen.

TUI v1 then introduces shared workflow extraction, entrypoint behavior, theming, navigation, browser, diff viewer, provider screens, machine management, restore consequence comparison, external-tool handoff, Sync UI, and the Overview decision inbox.

No profile schema bump is required merely to add the TUI or local Git metadata; Git repository configuration lives in `.git/` and TUI runtime state is non-portable UI state.

## Consequences

### Positive

- the TUI becomes a native Blueprint client rather than a wrapper around CLI output;
- CLI-only users retain full semantic capability;
- restore makes destructive consequences explicit before approval;
- one file browser and one diff model can be reused throughout the product;
- Omarchy users get a theme-respecting interface without a hard dependency on a particular theme implementation detail;
- specialist tools remain available without bloating Blueprint into an editor, file manager, or full Git client;
- profile Git workflows complete the natural capture → review → commit → push loop;
- the architecture remains suitable for a future Omarchy Shell client because presentation-independent workflows already exist.

### Negative

- extracting workflow services from the current Cobra-centric app is meaningful architectural work before screens can be rich;
- TUI testing requires deterministic model/update tests in addition to existing provider/CLI tests;
- asynchronous filesystem/Git inspection adds cancellation and stale-result concerns;
- palette discovery must tolerate Omarchy implementation changes;
- two implementation slices are required before the full TUI experience is complete.

## Rejected alternatives

- **TUI shells out to `omarchy-blueprint --json`:** creates subprocess/protocol coupling, duplicates error handling, and prevents native composition.
- **TUI-only capabilities:** violates headless/scriptability goals and creates a second product API.
- **Command-palette-only navigation:** good as a power feature, but too transient for a state-heavy product where stable provider locations matter.
- **Wizard-only UI:** makes repeat inspection and cross-provider navigation cumbersome.
- **Build a Blueprint editor:** contradicts “the running system is the configuration editor” and duplicates mature specialist tools.
- **Build a full file manager:** unnecessary and increases destructive filesystem authority.
- **Build a full Git client:** conflict resolution/history manipulation are better handled by Git/LazyGit.
- **Hard-code a Blueprint colour palette:** fights Omarchy/terminal theming and performs poorly across light/dark themes.
- **Require a user-installed Omarchy theme hook:** makes optional appearance integration a runtime prerequisite.
- **Automatic network fetch on startup:** introduces latency, prompts, and network side effects into basic inspection.
