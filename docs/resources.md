# Resources

Resources are the escape hatch for anything you want Blueprint to carry that
doesn't fit one of its normal categories. This guide covers when to use
them, how each reconstruction strategy behaves, and how conflicts and
per-machine paths are handled.

## When to use Resources

Config selectively discovers configuration worth remembering by comparing it
with Omarchy defaults. Resources are explicit files, folders, or Git
projects you deliberately ask Blueprint to reconstruct rather than something
Blueprint discovers on its own. Their strategy determines whether Blueprint
records an authoritative Git source, selected local Git changes, or copied
filesystem content.

## Track and untrack

```sh
omarchy-blueprint --profile ~/omarchy-profile track ~/dotfiles --strategy git
omarchy-blueprint --profile ~/omarchy-profile tracked
omarchy-blueprint --profile ~/omarchy-profile untrack dotfiles
```

`track` adds a path under your home directory to the profile. `tracked`
lists what's currently tracked, and `untrack` stops tracking a Resource
without touching the files it currently points at. Omitting `--strategy`
keeps the safe default: a portable Git worktree uses `git`; other supported
paths use `copy`.

## Strategy: `git`

```sh
omarchy-blueprint --profile ~/omarchy-profile track ~/dotfiles --strategy git
```

Blueprint stores only the portable remote origin and the exact commit HEAD.
Local staged, unstaged, and untracked state is reported as informational but
is not treated as a difference and is not captured. On restore, a missing
Resource is reconstructed by cloning that origin at that revision.

## Strategy: `git+diff`

```sh
omarchy-blueprint --profile ~/omarchy-profile track ~/dev/hacky-repo --strategy git+diff --include-untracked notes.md
```

This also preserves staged and unstaged tracked changes as saved patches,
plus only the untracked regular files you explicitly select with
`--include-untracked`. It never auto-selects untracked files. Use
`--exclude-untracked` to remove a file from that saved state later. These
Git-state artifacts are stored under `resources/git-state/` in the profile,
and a missing Resource is reconstructed from the pinned remote/HEAD plus the
saved overlay.

## Strategy: `copy`

```sh
omarchy-blueprint --profile ~/omarchy-profile track ~/some-local-tree --strategy copy
```

Blueprint stores the filesystem content itself under `resources/files/`.
When the tracked path is a Git worktree, `.git` administration data is
excluded from the copy.

## Symlink adoption

Tracking a Resource also adopts symlinks in `$HOME`, `$HOME/.config`, and
`$HOME/.local/bin` that point into it — for example,
`~/.config/hypr/overrides.lua -> ~/dotfiles/hypr/overrides.lua`. Restore
recreates that link relative to the target machine. Blueprint does not
recursively track an untracked symlink's target.

## Conflicts and restore behavior

`capture resources`, `status resources`, and `restore resources` participate
in the normal category lifecycle. Restore is additive: an existing file,
directory, Git worktree, or link that differs from the desired state is
reported as a conflict and is never replaced automatically.

## Per-machine Resource paths

A Resource keeps one portable default `path`, but a machine overlay can
place a whole Resource root somewhere else on a particular computer without
changing that portable path or the saved content:

```sh
omarchy-blueprint --profile ~/omarchy-profile machine add
omarchy-blueprint --profile ~/omarchy-profile machine map resource:projects ~/Code
omarchy-blueprint --profile ~/omarchy-profile machine use framework
omarchy-blueprint --profile ~/omarchy-profile machine rename framework work-laptop
omarchy-blueprint --profile ~/omarchy-profile --machine desktop restore resources
omarchy-blueprint --profile ~/omarchy-profile machine remove work-laptop
```

An omitted `machine add` name prompts with a hostname-based suggestion; pass
a name for non-interactive use (required with `--json`). `--machine`
selects an overlay for a single command without changing the local binding.
`machine clear` returns the profile to its portable Resource paths. Rename
and remove change only placement policy, never live Resource bytes. Mappings
accept `~/...` or absolute paths and apply to complete Resource roots only.

## Safety boundaries

Resources never overwrite a differing target automatically — conflicts are
reported and left for you to resolve. Copied Git worktrees exclude `.git`
administration data rather than treating it as portable content. Machine
path mappings only ever change where a Resource is read from and written to
on one machine; they never change the profile's portable definition or the
bytes Blueprint has saved.
