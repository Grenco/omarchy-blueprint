# TUI Comprehension & Navigation Design

## Scope

This design covers the first targeted comprehension PR after TUI v1. It changes TUI information architecture and presentation only. It must not change provider semantics, profile schema, capture semantics, restore semantics, or CLI capability.

Follow-up PRs will separately handle:
1. user-facing README/docs restructuring;
2. internal TUI file decomposition and duplication cleanup;
3. onboarding/empty-state polish discovered through use.

## Navigation structure

Overview remains the ungrouped front door.

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

Section headings are visual structure, not selectable screens. Normal `j`/`k` navigation continues to move only between screens.

Section meanings:
- Workflow — Save the current system to the profile or rebuild a system from it.
- Software — Installed and selected software Blueprint can recreate.
- Customisation — Dotfiles, shell behaviour, hooks, and other files that make the system yours.
- Profile — How this Blueprint adapts to a machine and syncs through Git.

## Screen descriptions

The short description is always shown directly under the active workspace title. It is muted, wrapped, followed by one blank line, and is part of the workspace height calculation.

The long description is pinned at the top of `?` help above the searchable key reference. Searching help filters commands/keys only; it never hides the screen explanation.

### Overview

Short:
> See what differs, what needs attention, and where to go next.

Long:
> Overview is your starting point. It summarizes differences between this machine and your Blueprint profile, warnings, blocked work, and profile sync state. Open an item to jump to the screen where you can review or act on it.

### Capture

Short:
> Update your Blueprint profile from the system you're using now.

Long:
> Capture reads the current system and saves selected categories into your profile. Use it after you intentionally change your setup and want Blueprint to remember those changes. Review the categories, choose what to update, then capture.

### Restore

Short:
> Preview and apply what your Blueprint profile would recreate on this machine.

Long:
> Restore compares your profile with this machine and shows what would change before anything is applied. Normal mode avoids supported conflicts; Forced mode can resolve some conflicts in favour of the profile. Review the consequences before applying a plan.

### Packages

Short:
> Packages and tools Blueprint expects to be installed.

Long:
> Packages shows captured system packages, AUR packages, and global Mise tools, plus differences on this machine. Included items are part of portable restore; excluded or machine-specific items are remembered without being restored everywhere.

### Themes

Short:
> Themes Blueprint remembers, including your selected theme.

Long:
> Themes shows the themes saved in your profile and the active theme Blueprint observed. Built-in themes are referenced by name, installed themes retain their source and revision when possible, and local or modified themes are copied into the profile.

### Plugins

Short:
> Omarchy plugins Blueprint remembers and can reconstruct.

Long:
> Plugins shows plugin code and sources saved in the profile. Built-in plugins are referenced by name, installed plugins retain their source when possible, and local or modified plugins are copied. If Shell state is captured, Shell controls plugin enablement while this screen describes how the plugin itself is reconstructed.

### Defaults

Short:
> Your chosen default terminal, browser, editor, and agent.

Long:
> Defaults records the applications Omarchy considers your terminal, browser, editor, and agent. Restore can set supported defaults through Omarchy, but installing those applications belongs to Packages. Agent selection is remembered and checked but is not automatically changed.

### Config

Short:
> Review dotfiles and other system/app configuration Blueprint can remember and restore.

Long:
> Config is about your dotfiles and other application/system configuration — primarily files under `~/.config`, plus supported home dotfiles — not settings for Blueprint itself. Blueprint compares them with Omarchy's defaults so it can save meaningful customisations without copying everything. State tells you what Blueprint found; Policy tells you whether Blueprint should decide automatically, include the path, or leave it out. Some configuration is handled by more specific Blueprint features and is shown as Handled elsewhere so it is not captured twice.

### Shell

Short:
> Your customised Omarchy shell, bar, and layout state.

Long:
> Shell captures supported Omarchy shell customisations relative to the normal Omarchy setup. Restore preserves unrelated changes where possible and reports overlapping changes as conflicts instead of silently replacing them.

### Hooks

Short:
> Scripts Omarchy runs automatically at supported hook points.

Long:
> Hooks are scripts Omarchy runs when particular supported events occur. This screen shows which hook files Blueprint has saved and whether this machine differs. Hook source is stored as written, so credentials and secrets should stay outside the scripts.

### Resources

Short:
> Files, folders, and Git projects you explicitly asked Blueprint to carry.

Long:
> Resources are things outside Blueprint's normal categories that you deliberately want to reconstruct. They can be copied, rebuilt from Git, or rebuilt from Git plus selected local changes. A Resource can also have a different path on different machines.

### Machines

Short:
> Per-machine locations for Resources that cannot use one shared path.

Long:
> Machines lets a portable Resource use a different path on a particular computer without changing its shared definition or contents. Select a machine to use its mappings, or leave the portable defaults active when no override is needed.

### Sync

Short:
> Git status and safe sync actions for the Blueprint profile itself.

Long:
> Sync manages the Git repository containing your Blueprint profile; it does not manage your application or Resource repositories. It handles the common safe workflow for committing, fetching, pulling, and pushing Blueprint-managed files. Use Git or LazyGit for more advanced repository work.

## Search aliases

- Overview: `home summary attention warning difference drift`
- Capture: `save remember update profile`
- Restore: `rebuild recreate apply recover`
- Packages: `software apps aur pacman mise tools install installed`
- Themes: `appearance theme installed`
- Plugins: `extensions plugin installed`
- Defaults: `terminal browser editor agent default apps applications`
- Config: `dotfiles dot files configuration settings .config`
- Shell: `shell bar waybar layout widgets`
- Hooks: `scripts events automation`
- Resources: `files folders directories projects git copy`
- Machines: `computer laptop desktop host paths mappings`
- Sync: `git commit push pull fetch remote profile repository`

## User-facing terminology

Presentation only:
- TUI `provider` → `category` when talking to users.
- Theme/plugin source type `git` → `installed`.
- Plugin group `Git plugins` → `Installed plugins`.
- Config `baseline` → `Omarchy default`.
- Prefer `differences` to `drift` in explanatory prose.
- Internal provider IDs, source enums, JSON fields, profile schema, and workflow types stay unchanged.

## Group navigation

Use single `[` and `]`.

Workspace grouped screens:
- `[` moves to the nearest visible group header strictly above the current row.
- From a child row, `[` therefore moves to that current group's header.
- From a group header, `[` moves to the preceding group header.
- `]` moves to the next visible group header strictly below.
- No wrap.
- Jumping never expands/collapses a group.
- Enter remains expand/collapse.
- Active text/filter/modal interactions retain key ownership.

Sidebar:
- When sidebar focus is active, `[` selects the first screen of the previous section.
- `]` selects the first screen of the next section.
- Overview behaves as a one-item navigation section only for motion; it has no visible heading.
- No wrap.
- Headings are never selectable.

Help labels are contextual:
- sidebar: Previous sidebar section / Next sidebar section
- grouped workspace: Previous group / Next group

These bindings appear in help but not the normal footer.

## Config information architecture

State = what Blueprint found.
Policy = what the user told Blueprint to do.

Exact broad groups:
1. Needs review — expanded by default.
2. Changes — expanded by default.
3. No action needed — collapsed by default.
4. Not managed by Config — collapsed by default.

| Internal classification | Group | State | Meaning |
| --- | --- | --- | --- |
| ambiguous-baseline | Needs review | Needs review | This file differs, but Blueprint cannot safely tell whether it is your change or an Omarchy-version difference. |
| ambiguous-deletion | Needs review | Removal needs review | An Omarchy default is missing, but Blueprint cannot safely tell whether you intentionally removed it. |
| modified-baseline | Changes | Changed from Omarchy default | You changed an Omarchy-provided configuration file. Blueprint can remember the change. |
| deleted-baseline | Changes | Omarchy default removed | An Omarchy-provided file is absent and Blueprint has enough information to treat that removal as intentional. |
| added | Changes | Added configuration | This configuration file exists on your system but is not part of Omarchy's defaults. |
| unchanged-baseline | No action needed | Matches Omarchy default | This file is unchanged from Omarchy, so there is nothing to save. |
| historical-baseline | No action needed | Matches a previous Omarchy default | This looks like an older Omarchy version rather than a personal customisation. Blueprint leaves it alone unless you explicitly include it. |
| delegated | Not managed by Config | Handled elsewhere | Another Blueprint feature owns this path, so Config will not capture it again. |
| excluded | Not managed by Config | Excluded | You told Config to leave this path alone. |
| volatile | Not managed by Config | Not suitable for capture | This looks like application/runtime state rather than portable configuration. |
| sensitive | Not managed by Config | Sensitive | Blueprint detected content that should not be saved into the profile. |
| oversized | Not managed by Config | Too large | This file exceeds Config's safe automatic capture limit. |
| unsupported | Not managed by Config | Unsupported | This filesystem entry cannot be handled safely by Config. |
| unmanaged-symlink | Not managed by Config | Symlink not managed here | This path is a symbolic link that Config deliberately does not own. |

Policy wording:
- Auto — Blueprint decides based on Omarchy defaults, ownership, and safety rules.
- Included — You asked Config to manage this path when it is safe to do so.
- Excluded — You asked Config to leave this path alone.

Included never overrides safety rules.

Config summary:
```text
Review  2 need review · 7 changes · 18 no action · 14 not managed
```

Config detail pane:
```text
Path: .config/example/settings.toml
State: Changed from Omarchy default
Policy: Auto
Meaning: You changed an Omarchy-provided configuration file. Blueprint can remember the change.
Saved in profile: Yes
Live file: /home/user/.config/example/settings.toml
Omarchy default: /...
Saved copy: /...
```

Omit empty path lines. Remove the current `Effective policy`, `Provider reason`, and `Available actions` lines.

## Non-goals

- No profile-schema changes.
- No Config semantic changes.
- No CLI/JSON field-name changes.
- No README/docs rewrite in this PR.
- No `model.go` decomposition in this PR.
- No broad empty-state overhaul in this PR.
- Sidebar sections are not collapsible.
- No new network behaviour.
