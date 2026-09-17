# TUI guide

The interactive interface is the fastest way to see what differs, decide
what to capture or restore, and act on it without memorizing commands. This
guide explains navigation and what each screen is for; use the in-app `?`
help for the exact actions available on a given screen.

## Launching the TUI

```sh
omarchy-blueprint
omarchy-blueprint tui
omarchy-blueprint --profile ~/omarchy-profile
```

When stdin and stdout are both terminals, the bare command opens the TUI.
`tui` opens it explicitly, and `--profile` selects a profile directly.
Without an interactive terminal, bare invocation prints normal CLI help
instead and does not emit terminal control sequences.

If the resolved profile path has no `profile.toml`, interactive startup
offers Create, Open, and Quit, even when the current directory contains
unrelated files. Create defaults to a separate destination and profile name;
Open accepts an existing profile path. Neither choice touches the current
directory unless you explicitly select it. An explicitly supplied invalid
`--profile` path remains a strict error rather than opening the chooser.

## Navigation

A few navigation primitives stay the same across every screen:

- `j` / `k` move through lists and navigation.
- `Tab` moves focus where a screen has more than one focusable area.
- `:` opens the command palette.
- `?` opens searchable, contextual help.
- `[` / `]` jump between sidebar sections or screen groups where supported.
- `Esc` closes overlays and transient views.

Screen-specific actions vary, so `?` is the canonical place to find them for
whatever screen you're on.

## Command palette and help

Typing in the `:` command palette filters actions immediately, and the arrow
keys move the selection — `j` and `k` remain ordinary searchable characters
there rather than navigation keys. `?` opens the same kind of searchable
overlay for contextual help. `Esc` closes either overlay.

## Workflow

### Overview

Overview is your starting point. It summarizes differences between this
machine and your Blueprint profile, warnings, blocked work, and profile sync
state. Open an item to jump to the screen where you can review or act on it.

### Capture

Capture reads the current system and saves selected categories into your
profile. Use it after you intentionally change your setup and want Blueprint
to remember those changes. `Space` toggles the highlighted category, `a`
selects changed categories, `c` captures the selection, and `C` runs
aggregate Capture All. Both capture actions require confirmation: `Enter`
confirms and `Esc` cancels. A successful capture refreshes Overview, category
status, and local profile Sync status, but never commits or pushes on its
own.

### Restore

Restore compares your profile with this machine and shows what would change
before anything is applied. Normal mode avoids supported conflicts; Forced
mode can resolve some conflicts in favour of the profile. Review the
consequences of a plan before approving it.

## Software

### Packages

Packages shows captured system packages, AUR packages, and global Mise
tools, plus differences on this machine. Included items are part of
portable restore; excluded or machine-specific items are remembered without
being restored everywhere.

### Themes

Themes shows the themes saved in your profile and the active theme Blueprint
observed. Built-in themes are referenced by name, installed themes retain
their source and revision when possible, and local or modified themes are
copied into the profile.

### Plugins

Plugins shows plugin code and sources saved in the profile. Built-in plugins
are referenced by name, installed plugins retain their source when possible,
and local or modified plugins are copied. If Shell state is captured, Shell
controls plugin enablement while this screen describes how the plugin itself
is reconstructed.

### Defaults

Defaults records the applications Omarchy considers your terminal, browser,
editor, and agent. Restore can set supported defaults through Omarchy, but
installing those applications belongs to Packages. Agent selection is
remembered and checked but is not automatically changed.

## Customisation

### Config

Config is about your dotfiles and other application/system configuration —
primarily files under `~/.config`, plus supported home dotfiles — not
settings for Blueprint itself. Blueprint compares them with Omarchy's
defaults so it can save meaningful customisations without copying
everything. State tells you what Blueprint found; Policy tells you whether
Blueprint should decide automatically, include the path, or leave it out.
Some configuration is handled by more specific Blueprint features and is
shown as Handled elsewhere so it is not captured twice.

### Shell

Shell captures supported Omarchy shell customisations relative to the normal
Omarchy setup. Restore preserves unrelated changes where possible and
reports overlapping changes as conflicts instead of silently replacing them.

### Hooks

Hooks are scripts Omarchy runs when particular supported events occur. This
screen shows which hook files Blueprint has saved and whether this machine
differs. Hook source is stored as written, so credentials and secrets should
stay outside the scripts.

### Resources

Resources are things outside Blueprint's normal categories that you
deliberately want to reconstruct. They can be copied, rebuilt from Git, or
rebuilt from Git plus selected local changes. A Resource can also have a
different path on different machines. Machines changes per-machine Resource
locations here, not Resource content — see the [Resources guide](resources.md)
for the full strategy and safety picture.

## Profile

### Machines

Machines lets a portable Resource use a different path on a particular
computer without changing its shared definition or contents. Select a
machine to use its mappings, or leave the portable defaults active when no
override is needed.

### Sync

Sync manages the Git repository containing your Blueprint profile; it does
not manage your application or Resource repositories. It handles the common
safe workflow for committing, fetching, pulling, and pushing Blueprint-managed
files, without fetching automatically at startup. Use Git or LazyGit for more
advanced repository work.

## Config states and policies

Config findings are grouped as:

```text
Needs review
Changes
No action needed
Not managed by Config
```

Each path also carries a policy:

- **Auto** — Blueprint decides based on Omarchy defaults, ownership, and safety rules.
- **Included** — You asked Config to manage this path when it is safe to do so.
- **Excluded** — You asked Config to leave this path alone.

Included never overrides safety rules.

An Omarchy default is Omarchy's own shipped value for a path; Config's job is
to notice when your system differs from that default in a way worth saving.

## Small terminals and NO_COLOR

Compact layouts preserve the same semantics with less simultaneous detail
rather than hiding information outright. Setting `NO_COLOR=1` disables
colour without making warning, diff, or selection meaning depend on colour
alone.
