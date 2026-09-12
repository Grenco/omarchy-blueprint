# Omarchy Blueprint

Use your system normally. Omarchy Blueprint remembers how to rebuild it.

This repository currently contains package reconstruction plus the first theme
vertical slice from [ROADMAP.md](ROADMAP.md). It captures explicitly
installed native and foreign packages, separates known machine-specific
hardware packages, shows semantic drift, creates a safe restore plan, installs
only missing portable packages through Omarchy, and verifies the result. It
never removes additional packages.

Theme state can also be captured, compared, restored, and verified. Built-ins
are referenced by name, clean Git themes retain their URL and revision, and
local themes or built-in overlays are copied into the profile.

First-party Omarchy plugin enablement is captured semantically. Clean Git
plugins retain their public URL and revision; modified, cloned, and local
plugins are snapshotted under `plugins/local/`. Restore uses Omarchy validation
and lifecycle commands, and marks executable third-party code as high risk.

Configuration is captured as a sparse, baseline-aware overlay. Blueprint first
classifies each top-level `~/.config` surface with a bounded metadata-only
probe: only `config-lean` surfaces are recursively auto-discovered;
`state-heavy`, `mixed`, and `sensitive` surfaces are reported once and skipped.
Browser/profile state is identified structurally rather than by product-name
blacklists. Modified Omarchy defaults, safe user-added files, and removed
shipped files are recorded, while unchanged defaults and generic backup
artifacts such as `.bak`, `.backup`, `.orig`, `*~`, and backup directories are
ignored. Baseline-backed paths remain exact-inspected even inside a skipped
surface, and semantic providers and tracked Resources own their paths ahead of
Config.

Use `omarchy-blueprint include config:.config/Typora/themes` to explicitly
discover a safe subpath of a skipped surface; inclusion does not override
sensitive-path/content checks, symlink or special-file refusal, backup
filtering, ownership, or size limits. For an intentionally authoritative bulk
 tree, track it as a Resource rather than weakening Config discovery. Unknown
baseline mismatches and baseline-only paths are review-only unless trusted
history or an explicit Config include supplies provenance. On an
Omarchy upgrade, independent text changes are three-way merged; conflicts
preserve target work unless `restore config --force` is explicitly approved,
with a recoverable backup.

Omarchy's semantic default applications — terminal, browser, editor, and
agent — are captured as plain values in `defaults/defaults.toml`. Restore
replays drifted values through the public `omarchy default <kind> <value>`
setter, and Omarchy itself validates the values. Defaults selects applications;
their installation belongs to the Packages provider. Unset defaults carry no
desired state, restore never unsets a machine-selected default, and values
Omarchy does not manage (raw `.desktop` IDs) are captured with a portability
warning but skipped by restore. The default agent is captured, diffed, and
verified, but never set automatically: Omarchy's agent setter launches the
selected agent, so restore reports it as skipped until a set-only path exists.

Customized Omarchy Shell state is captured relative to its Omarchy baseline.
Blueprint restores captured intent with a semantic merge: independent target
customization is preserved, while overlapping changes are reported as conflicts.
`restore --force` resolves supported Shell conflicts in favor of captured intent
without discarding unrelated target state. Once Shell state is captured, it owns
plugin enablement and layout; plugin restore still reconstructs source and
provenance, but does not enable plugins independently of Shell.

Bar widgets are reconciled across left, center, and right by widget ID. A
uniquely identifiable widget explicitly added, removed, or moved by the
captured profile follows that placement without `--force`; target-only widgets
remain. Duplicate widget IDs remain conservatively conflict-managed.

## Requirements

- Omarchy 4 or newer
- Go 1.25 or newer when building from source
- `pacman` and the public `omarchy` CLI on `PATH`

## Build and test

```sh
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

## First workflow

Create and capture a profile:

```sh
mkdir -p ~/omarchy-profile
omarchy-blueprint init ~/omarchy-profile --name main
omarchy-blueprint --profile ~/omarchy-profile capture
```

After cloning that profile on another Omarchy machine:

```sh
omarchy-blueprint check
omarchy-blueprint status
omarchy-blueprint restore --dry-run
omarchy-blueprint restore
```

## Interactive TUI

When stdin and stdout are terminals, the bare command opens the keyboard-first
TUI. Use an explicit command when preferred, or select a profile directly:

```sh
omarchy-blueprint                    # TUI when interactive
omarchy-blueprint tui
omarchy-blueprint --profile ~/omarchy-profile
```

The Overview is a decision inbox. Use the sidebar, `j`/`k`, `Tab`, or the `:`
command palette to navigate. Config review shows each discovery classification
and reason, then lets you choose include, exclude, or automatic policy.
Resources supports non-destructive discovery and strategy selection; Machines
shows portable paths and per-machine overrides. Restore compares Normal and
Forced plans before approval, including consequence changes. Sync exposes the
safe profile-Git workflow without fetching automatically at startup. Editor,
file-manager, browser, clipboard, and LazyGit actions hand off to external
tools and refresh when they return.

Without an interactive terminal, bare invocation prints normal CLI help and
does not emit terminal control sequences. Set `NO_COLOR=1` to disable colour;
selection, warning, diff, and restore semantics remain visible as text.

Category-less `status`, `diff`, and `restore` operate on every captured
provider. Use `status packages`, `status themes`, `restore packages`, or
`restore themes` when you want to target one category. Configuration, defaults,
and Shell and Hooks state support the same explicit categories, for example
`omarchy-blueprint status config`, `omarchy-blueprint restore defaults --dry-run`,
`omarchy-blueprint restore shell --dry-run`, or `omarchy-blueprint restore hooks --dry-run`.

Category-less `capture` captures every supported provider. Use
`capture packages`, `capture themes`, `capture config`, `capture defaults`, or
`capture shell`
for a targeted refresh. Only files that differ from the Omarchy baseline are
captured; clean defaults stay out of the profile.

The non-interactive form is `omarchy-blueprint restore --yes`. Combine `--json`
with `--dry-run` or `--yes`; JSON restores never wait for a prompt.

### Profile Git sync

Blueprint can manage the simple capture-to-commit-to-push workflow for its own
profile files:

```sh
omarchy-blueprint --profile ~/omarchy-profile profile git init
omarchy-blueprint --profile ~/omarchy-profile profile git remote set <remote>
omarchy-blueprint --profile ~/omarchy-profile profile git status
omarchy-blueprint --profile ~/omarchy-profile profile git diff
omarchy-blueprint --profile ~/omarchy-profile profile git commit
omarchy-blueprint --profile ~/omarchy-profile profile git push
```

Only Blueprint-managed profile paths are staged and committed; unrelated files
in the same repository remain untouched. Status never fetches implicitly, pull
is fast-forward-only and requires a clean repository, and existing staged work
blocks Blueprint commits. Use Git or LazyGit for conflicts, rebases, history
editing, interactive staging, or other advanced repository work.

Package profiles are human-readable:

```text
profile.toml
packages/official.txt
packages/aur.txt
packages/mise.toml
packages/machine-specific.txt
packages/excluded.txt
themes/themes.toml
themes/local/<theme>/
plugins/plugins.toml
plugins/local/<plugin-id>/
config/config.toml
config/files/hypr/<file>.lua
config/baseline/hypr/<file>.lua
defaults/defaults.toml
shell/shell.toml
shell/shell.json
shell/baseline.json
hooks/hooks.toml
hooks/files/<event>
hooks/files/<event>.d/<hook>
resources/resources.toml
resources/files/<resource-id>/
resources/git-state/<resource-id>/
```

Machine-specific entries retain provenance, for example
`official:nvidia-open` or `aur:nvidia-580xx-dkms`.

Mise global `[tools]` declarations are captured as a third Packages source.
Blueprint preserves configured requests and options rather than resolved cache
versions, and leaves global Mise environment, settings, tasks, and project-local
configuration outside the profile. Restore appends only missing declarations,
preserves target-only tools and conflicting target declarations, then runs
`mise -C / install` for safely added tools.

## Portable resources

Track an explicit file, directory, or Git worktree under your home directory.
Choose the reconstruction policy explicitly when its semantics matter:

```sh
omarchy-blueprint --profile ~/omarchy-profile track ~/dotfiles --strategy git
omarchy-blueprint --profile ~/omarchy-profile track ~/dev/hacky-repo --strategy git+diff --include-untracked notes.md
omarchy-blueprint --profile ~/omarchy-profile track ~/some-local-tree --strategy copy
omarchy-blueprint --profile ~/omarchy-profile tracked
omarchy-blueprint --profile ~/omarchy-profile untrack dotfiles
```

Omitting `--strategy` keeps the safe default: a portable Git worktree uses
`git`; other supported resources use `copy`. `git` stores only the portable
origin and exact HEAD. Local staged, unstaged, and untracked state is reported
as informational but is not drift or captured. `git+diff` also preserves staged
and unstaged tracked changes as separate patches plus only explicitly selected
untracked regular files; it never auto-selects untracked files. `copy` stores
filesystem content under `resources/files/`; when copying a Git worktree it
excludes `.git` administration data.

Git-state artifacts for `git+diff` are stored under `resources/git-state/`.
Missing Git resources can be reconstructed from their pinned remote/HEAD and,
for `git+diff`, their saved overlay. Existing differing destinations are
reported as conflicts and left untouched. Tracking a resource also adopts
symlinks in `$HOME`, `$HOME/.config`, and `$HOME/.local/bin` that point into it.
For example, `~/.config/hypr/overrides.lua -> ~/dotfiles/hypr/overrides.lua` is
restored as a target-machine-relative link.

`capture resources`, `status resources`, and `restore resources` participate
in the normal provider lifecycle. Restore is additive: existing differing
files, directories, Git worktrees, and links are reported as conflicts and are
never replaced automatically. Blueprint does not recursively track an
untracked symlink target.

### Machine resource paths

Resources retain a portable default `path`, but a selected machine overlay can
place a whole Resource root somewhere else on that machine. The mapping affects
capture, status, restore, and dependent links; it never changes the portable
Resource path or its saved content.

```sh
omarchy-blueprint --profile ~/omarchy-profile machine add
omarchy-blueprint --profile ~/omarchy-profile machine map resource:projects ~/Code
omarchy-blueprint --profile ~/omarchy-profile machine use framework
omarchy-blueprint --profile ~/omarchy-profile machine rename framework work-laptop
omarchy-blueprint --profile ~/omarchy-profile --machine desktop restore resources
omarchy-blueprint --profile ~/omarchy-profile machine remove work-laptop
```

An omitted `machine add` name prompts with a hostname-based suggestion; pass a
name for non-interactive use (required with `--json`). `--machine` selects an
overlay for one command and does not change the local binding. `machine clear`
returns the profile to its portable Resource paths. Rename and remove change
only portable placement policy, never live Resource bytes.
Mappings accept `~/...` or absolute paths and apply only to complete Resource
roots.

## Package exclusions

Exclude a package that should remain outside portable restore:

```sh
omarchy-blueprint --profile ~/omarchy-profile exclude package:dislocker-git
```

`package:<name>` resolves against the captured profile. `official:<name>` and
`aur:<name>` can be used explicitly. Exclusions persist across capture, do not
produce drift, and are shown as skipped by `check` and restore dry-runs.

Put an excluded package back into the managed set with:

```sh
omarchy-blueprint --profile ~/omarchy-profile include aur:dislocker-git
```

Restore journals are written to
`$XDG_STATE_HOME/omarchy-blueprint/restores/`, falling back to
`~/.local/state/omarchy-blueprint/restores/`.

## Current boundaries

This milestone does not yet distinguish packages added by the user from other
explicit packages. Theme restore installs only missing themes. An existing
theme with different provenance or content is reported as a conflict and is
never overwritten automatically. Internal symlinks and special files are
rejected during local-theme capture. Config restore never overwrites a target
that differs from both the desired content and the current Omarchy baseline,
and cross-version baseline changes are reported as migration-required rather
than auto-merged. Defaults restore is additive: it never unsets a
machine-selected default. Generic backup providers, advanced Git workflows,
broader machine-specific state, migration UI, and agent assistance remain
postponed.

Capture and inspect theme state explicitly with:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture themes
omarchy-blueprint --profile ~/omarchy-profile status themes
omarchy-blueprint --profile ~/omarchy-profile restore themes --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore themes
```

The same pattern applies to configuration state:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture config
omarchy-blueprint --profile ~/omarchy-profile status config
omarchy-blueprint --profile ~/omarchy-profile restore config --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore config
```

And to defaults:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture defaults
omarchy-blueprint --profile ~/omarchy-profile status defaults
omarchy-blueprint --profile ~/omarchy-profile restore defaults --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore defaults
```

And to Shell state:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture shell
omarchy-blueprint --profile ~/omarchy-profile status shell
omarchy-blueprint --profile ~/omarchy-profile restore shell --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore shell
```

And to Hooks state:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture hooks
omarchy-blueprint --profile ~/omarchy-profile status hooks
omarchy-blueprint --profile ~/omarchy-profile restore hooks --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore hooks
```

Hook source is stored verbatim in the profile. Hooks intended for version
control should read credentials from environment variables, a password manager,
or another external secret source rather than embedding secret values directly.

## Exit codes

- `0`: success or state matches
- `1`: command, validation, or environment error
- `2`: drift detected by `status` or `diff`

## Safety

- Restore previews are non-mutating.
- Apply requires confirmation or `--yes`.
- Commands are executed without a shell.
- Local theme snapshots exclude Git internals and reject internal symlinks.
- Theme restore never overwrites an existing user theme directory.
- Additional packages are never removed.
- Known GPU-driver, CPU-microcode, and fingerprint-stack packages are recorded
  as machine-specific and skipped during portable restore.
- Missing native packages and missing AUR packages are each installed in a
  single transaction.
- Interactive restores print an immediate operation message and an elapsed-time
  heartbeat every five seconds while package tools are running.
- AUR packages restore as separate operations. Failures are journaled, later
  packages continue, and the final summary lists successes and failures.
- Installed package names satisfy the profile regardless of whether a machine
  currently classifies them as official or foreign/AUR; captured provenance is
  used only when an installation is actually required.
- Packages already present as dependencies also satisfy the profile. Capture
  still records only explicitly installed packages, avoiding dependency noise.
- Additional explicit packages remain visible as status drift, but restore
  reports them as intentionally left installed because removal is disabled.
- Package installs are journaled and are not automatically rolled back.
- Config capture rejects symlinks and special files for both baseline and user
  paths and never captures clean Omarchy defaults.
- Config restore writes with a validated hash, an atomic rename, and a backup
  beside the restore journal. A target carrying user work or a changed Omarchy
  baseline is skipped, never overwritten.
- Hyprland is reloaded only after every config write succeeds.
- Defaults restore replays captured values through Omarchy's public
  `omarchy default <kind> <value>` commands. Defaults selects applications;
  packages are installed by the Packages provider. Omarchy validates values and
  Blueprint never maintains its own allowlist of valid choices.
- The default agent is never set automatically: Omarchy's agent setter
  launches the selected agent, so restore skips it with an explicit reason.
- Shell capture rejects symlinks, special files, unsupported JSON versions, and
  third-party plugin references without captured provenance. Shell restore
  preserves independent target state, writes atomically, and restarts only after
  a planned merge write.
- Hooks capture only flat hooks and immediate `.d/` children. Runtime symlink
  entries are never followed or captured; they are reported as unmanaged and
  left untouched. Snapshots are inert at mode `0644`. Restore is high-risk,
  never executes hook source, restores recorded modes atomically, and never
  overwrites differing target contents or removes extra hooks.
