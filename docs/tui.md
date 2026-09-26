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
- `h` / `l` (or Left / Right) move between Navigation, the workspace, and
  Details.
- `Tab` switches a screen's State / Capture / Restore tabs, or moves between
  a screen's regions, while the workspace has focus. On screens without tabs
  in wider terminals it moves between the visible panes.
- `:` opens the command palette.
- `?` opens searchable, contextual help.
- `[` / `]` jump between sidebar sections or screen groups where supported.
- `Esc` closes overlays and transient views.

Screen-specific actions vary, so `?` is the canonical place to find them for
whatever screen you're on.

In narrow terminals, Navigation and Details become drawers over the
workspace: `h` opens Navigation, `l` opens Details, and `Esc` closes either.
A drawer only has focus while it is visible, and keys go to the focused pane,
so nothing changes behind an open drawer.

## Command palette and help

Typing in the `:` command palette filters actions immediately, and the arrow
keys move the selection — `j` and `k` remain ordinary searchable characters
there rather than navigation keys. `?` opens the same kind of searchable
overlay for contextual help. `Esc` closes either overlay.

## Workflow

### Overview

Overview is your starting point. It groups what differs between this machine
and your Blueprint profile:

- **Needs attention** — blocked, unsafe, or unresolved items that need a
  decision.
- **Changes available** — differences Capture or Restore would act on under
  current policy, plus profile changes waiting in Sync. Restore counts only
  when its real plan, under this machine's Restore options (Safe or Force,
  Additive or Exact), would change the item.
- **Intentional differences** — differences your Capture policy and Restore
  policy or options deliberately leave alone on this machine, such as an
  extra package Additive Restore keeps. These are not warnings, stay
  collapsed until you open them, and explain why.
- **No action needed** — categories with nothing to act on.

Open an item to jump to the screen where you can review or act on it.

### Capture

Capture reads the current system and saves selected categories into your
profile. Use it after you intentionally change your setup and want Blueprint
to remember those changes. `Space` toggles the highlighted category, `a`
selects changed categories, `c` reviews the selection, and `C` reviews every
category.

The review lists the proposed profile changes before anything is written,
grouped as **Changes** (Add, Update, Remember absent, Stop managing),
**Preserved by policy**, and **Blocked**; targets needing no action are only
counted. A fresh profile therefore shows its first-capture candidates here.
Outcomes are always for this machine. The review names the policy scope it
edits, Profile defaults or a machine, and `p` switches it; the CAPTURE column
and Details show the setting at that scope, alongside the setting this
machine actually applies when they differ. `Space` switches the selected
target between Include and Preserve at that scope and `x` resets it to the
inherited setting; either recalculates the review. Safety blocks cannot be
overridden from here.

`Enter` asks for approval with a summary, and `Esc` returns to the category
list. Approval is part of Capture itself: Capture re-checks the system and
your policy, runs with exactly the decisions it checked, and checks again
after staging, before anything is saved. If any outcome changed, or a value
Capture would write changed (for example a config file edited again), nothing
is written: the review is recalculated, says what changed, and needs
approving again. A successful capture refreshes Overview, category status,
and local profile Sync status, but never commits or pushes on its own.

### Restore

Restore compares your profile with this machine and shows one current plan
before anything is applied. Two independent settings shape it: conflict
handling (`f`) is **Safe**, which preserves supported conflicts, or **Force**,
which can resolve them in favour of the profile; convergence (`e`) is
**Additive**, which creates or updates desired state, or **Exact**, which can
also remove Blueprint-managed extras. Exact never deletes Resource data. These
toggles are one-run overrides and do not change the machine's saved defaults.
Review the consequences of a plan before approving it; approval replans
before applying, and refuses if the plan changed since you approved it.

Package changes that need administrator authentication are marked
interactive. When you approve such a plan, the interface steps aside and the
restore runs in the terminal so `sudo` can show its own prompt, then
Blueprint returns. On a freshly installed machine without a package database,
the plan shows **Requires before applying** and cannot be applied until you
run `omarchy update` in a terminal and open Restore again.

## Software

### Packages

Packages shows captured system packages, AUR packages, and global Mise
tools, plus differences on this machine. Capture and Restore policy decide,
for the profile or a single machine, which packages Blueprint records and
which it reinstalls, so a package can stay on one computer without being
restored everywhere.

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

Machines lets one computer differ from the portable profile without changing
it: its own Safe/Force and Additive/Exact restore defaults, Capture and
Restore policy overrides, and Resource paths for Resources that cannot use
one shared location. Select a machine to use its overlay, or leave the
portable defaults active when no override is needed. Mutations apply only to
the focused region, and `+ Add machine` at the bottom of the table creates a
new overlay.

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
