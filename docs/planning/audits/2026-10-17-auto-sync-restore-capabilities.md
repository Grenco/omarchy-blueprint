# Auto Sync Gate 0 — Restore Operation Capability Audit (draft)

**Status:** Draft for human review — Gate 0 of the Phase 11 implementation plan.
No PR A code until this audit, its allowlist and its default-deny cases are
approved.\
**Date:** 2026-10-17\
**Baseline:** `main` `aa6f127`\
**Plan:** [`2026-10-17-auto-sync-phase-11-implementation-plan.md`](../plans/2026-10-17-auto-sync-phase-11-implementation-plan.md) §2\
**ADR:** [0029](../../adr/0029-machine-local-auto-sync-authority.md)\
**Proposed capability version:** `restore-capabilities/1`

## 1. Method

Every `model.Operation` a Restore plan can contain was enumerated from the
provider planners and the workflow/app finalizers at the baseline
(`internal/providers/*/`, `internal/app/providers.go` theme activation
re-ordering, `internal/workflow/read_restore.go` fragment merge). No other
code path adds operations. For each operation family the table records the
command or executor action, what it writes, whether it executes code
(directly, or later because of what it writes), network use, privilege,
interactivity, reversibility, dependencies, and the current provider `Risk`.
The Omarchy commands invoked were read on this machine
(`/usr/bin/omarchy-*`) to establish their real side effects.

The classes are those of the plan: `requires_human` (default and catch-all),
`unattended_safe` (Conservative and above), `unattended_routine` (Balanced and
above) and `unattended_extended` (Hands-off only). A registry entry is keyed by
**provider + operation family + required conditions**; an operation that
matches no entry, or matches one whose conditions are not proven by fresh
evidence, is `requires_human`.

## 2. Threat model (summary)

- **Untrusted input:** everything in the shared profile — artifacts, desired
  values, Restore/Capture policy, `restore_convergence`, conflict and
  activation modes, Omarchy version claims — may have been written by another
  computer or by anyone with push access to the remote.
- **What unattended execution must not do:** run code that arrived in Git,
  download and run third-party code, change what runs at login or on an
  event, remove or overwrite user data, elevate privilege, or wait for input.
- **Code-by-proxy:** a file write is code execution when the file is later
  sourced or loaded (Hyprland `exec`, shell rc, Neovim Lua, theme hooks,
  systemd units, plugin QML). Such writes are classified as execution.
- **Out of scope:** a malicious process already running as the same user
  (plan A1: local state protects against other users and accidental import,
  not same-UID compromise).

## 3. Inventory and proposed classification

`R` = provider risk label today. "Exec" = executes code now or makes code run
later.

### 3.1 Themes (`internal/providers/themes/provider.go`, `internal/app/providers.go`)

| Family | Effect | R | Exec | Net | Proposed class |
|---|---|---|---|---|---|
| `themes.copy` (local) | Exclusive tree copy from `themes/local/<id>` into `~/.config/omarchy/themes/<id>`; fails if destination exists; no activation | low | No, while the theme is not active | No | **`unattended_safe` with conditions C-T1…C-T5** |
| `themes.copy` (overlay: same name as a built-in) | As above, but shadows a built-in theme name | low | Changes what a later `theme set` of a built-in name loads | No | `requires_human` |
| `themes.install` + `themes.pin` (Git) | `omarchy theme install <url>` clones a remote repository; `git checkout --detach` | low | Clone of third-party content | Yes | `requires_human` |
| `themes.activate` | `omarchy theme set <id>`: restarts components, runs post-theme commands in `bash -lc`, then `omarchy-hook theme-set` (user hooks); theme files such as `neovim.lua` become live code | low | **Yes** | No | `requires_human` |
| `themes.remove` (Exact only) | `omarchy theme remove` (unconditional `rm -rf`) | high | No | No | `requires_human` (hard deny: removal) |

Conditions for `themes.copy` (all must be proven by fresh evidence at the
locked boundary; any failure → `requires_human`):

- **C-T1** Theme type `local`, safe ID, destination absent (no overwrite).
- **C-T2** The same plan contains no `themes.activate` and the theme is not the
  desired current theme; a theme copy that is a dependency of activation is
  classified with the activation.
- **C-T3** The copy source satisfies the portable-snapshot contract
  (`internal/portabletree`) and its fingerprint equals the recorded profile
  hash. **Code change needed:** the unattended path must set
  `Copy.SourceHash` (verified by `copyTreeExclusive`) and
  `RejectSymlinkParents`, which `themes.copy` does not set today.
- **C-T4** Restore policy for `theme:<id>` is Apply, and the target is within
  the local grant.
- **C-T5** The Sync artifact check (`ValidateSyncArtifacts`) passed for the
  pinned remote revision.

### 3.2 Defaults (`internal/providers/defaults/provider.go`)

| Family | Effect | R | Exec | Net | Proposed class |
|---|---|---|---|---|---|
| `defaults.set.{terminal,browser,editor}` | `omarchy default <kind> <value>`; Omarchy's setter accepts only a fixed list of values (`omarchy-default-*`), then writes `~/.config/xdg-terminals.list`, `xdg-settings set default-web-browser`, or `~/.local/state/omarchy/defaults/editor`, and sends a desktop notification | low | No (changes which installed app handles terminal, links or editing) | No | **`unattended_routine` with conditions C-D1…C-D3** |
| `defaults.set.agent` | Never planned: setter launches the agent (already skipped) | — | Yes | — | `requires_human` (hard deny) |
| raw `.desktop` values | Never planned (not portable; already skipped) | — | — | — | n/a |

- **C-D1** The value is one of the setter's accepted names (the plan already
  skips non-portable `.desktop` values; the registry pins the accepted set per
  capability version and treats a new name as unknown).
- **C-D2** **New evidence needed:** the chosen application is installed on this
  computer (its desktop entry resolves). Omarchy's setters do not check this,
  and setting an absent browser breaks link handling. Without the evidence →
  `requires_human`.
- **C-D3** Restore policy Apply for `default:<kind>`, within the local grant.

Routine rather than safe because the effect is user-visible behaviour (which
app opens), although bounded, noninteractive and reversible by setting the
previous value.

### 3.3 Config (`internal/providers/config/{provider,diff}.go`)

| Family | Effect | R | Exec | Proposed class |
|---|---|---|---|---|
| `config.write.*` (new, replace, migrated, forced) | Writes arbitrary files under the user config root, with backup | low–high | **Yes by proxy** (Hyprland `exec`, shell, Lua and other sourced configs) | `requires_human` |
| `config.delete.*` | Deletes a user config file (backup kept) | high | — | `requires_human` (hard deny: removal) |
| `config.reload` | `hyprctl reload` after Hypr writes; re-reads config and runs its `exec` lines | low | Yes | `requires_human` |

A future data-only allowlist (specific paths whose format cannot execute
code) would need its own audit; none is proposed for v1.

### 3.4 Shell and Hooks

| Family | Effect | R | Exec | Proposed class |
|---|---|---|---|---|
| `shell.write` | Replaces Omarchy Shell configuration (which plugins load, bar layout) | varies | Yes by proxy (enables installed plugin code) | `requires_human` |
| `shell.restart` | `omarchy-restart-shell` | medium | Yes | `requires_human` |
| `hooks.write` | Writes executable Omarchy hook scripts | high | **Yes** | `requires_human` (hard deny: executable payload) |
| `hooks.delete` | Deletes a hook | high | — | `requires_human` (hard deny: removal) |

### 3.5 Plugins (`internal/providers/plugins/provider.go`)

| Family | Effect | R | Exec | Net | Proposed class |
|---|---|---|---|---|---|
| `plugins.install` / `pin` / `validate` / `rescan` (Git) | `omarchy plugin add <url> --yes`, checkout, validate, `omarchy-shell shell rescanPlugins` | high/low | **Yes** (third-party code loaded) | Yes | `requires_human` (hard deny) |
| `plugins.validate` / `copy` / `rescan` (local) | Copies plugin code from the profile, then loads it | high | **Yes** | No | `requires_human` (hard deny) |
| `plugins.enable` / `disable` | `omarchy plugin enable/disable` | high/low | Enable runs plugin code | No | `requires_human` |
| `plugins.remove` (Exact) | `omarchy plugin remove --yes` | high | — | No | `requires_human` (hard deny: removal) |

### 3.6 Packages (`internal/providers/packages/provider.go`)

| Family | Effect | R | Exec | Net | Interactive | Proposed class |
|---|---|---|---|---|---|---|
| official/AUR install | Package manager install, privilege elevation, install scripts, AUR builds | low–high | **Yes** | Yes | Yes | `requires_human` (hard deny) |
| Omarchy semantic recipes | `omarchy-install-*` setup scripts, may wait for sign-in (`AwaitsYou`) | varies | **Yes** | Yes | Often | `requires_human` (hard deny) |
| preinstalls install/remove | `sh -c "omarchy-install-preinstalls && …"`, `omarchy-pkg-add/drop` | high/low | **Yes** | Yes | Yes | `requires_human` (hard deny) |
| `packages.mise.configure` / `install` / `verify` / `remove` | Writes the mise global config, downloads and installs tools; `sh -c` hash guard | low–high | **Yes** (downloads executables) | Yes | No | `requires_human` (hard deny) |
| package removals | `pacman`/`yay`/recipe removal | high | Yes | — | Some | `requires_human` (hard deny: removal) |

### 3.7 Services (`internal/providers/services/{plan,plan_files,activation}.go`)

| Family | Effect | R | Exec | Proposed class |
|---|---|---|---|---|
| unit/drop-in file writes | Writes systemd user unit files | medium | **Yes by proxy** (`ExecStart`) | `requires_human` (hard deny: service unit changes) |
| `link` (enable symlinks) / `enable` / `disable` / `mask` / `unmask` | `systemctl --user <verb>` and wants-symlinks | medium | Changes what runs at login | `requires_human` |
| `start` (activation) | `systemctl --user start` (approved activation only) | medium | **Yes** | `requires_human` (hard deny: activation) |

### 3.8 Resources (`internal/providers/resources/plan.go`)

| Family | Effect | R | Exec | Net | Proposed class |
|---|---|---|---|---|---|
| file / copy / directory / symlink | Writes arbitrary user files and trees (could be scripts, rc files, autostart entries) | low | Yes by proxy | No | `requires_human` |
| `git clone` / `git checkout` / staged and unstaged patch / untracked file | Clones arbitrary repositories and applies patches | low | Possible (repository content) | Yes | `requires_human` |

Resources is the clearest case of `RiskLow` not meaning unattended-safe.

## 4. Proposed v1 registry

| Key | Class | Conditions |
|---|---|---|
| `themes` + `copy` + type `local` | `unattended_safe` | C-T1…C-T5 |
| `defaults` + `set` + kind ∈ {terminal, browser, editor} | `unattended_routine` | C-D1…C-D3 |
| everything else | `requires_human` | — |

No `unattended_extended` entry is proposed, so in v1 **Hands-off behaves
exactly like Balanced**, and the UI must say so rather than imply more.
Conservative can follow only new local themes; Balanced and Hands-off can also
follow default-app changes.

Hard denies that no preset, Custom rule or grant can lift in v1: removal or
delete of any kind; executable or code-by-proxy payloads (hooks, plugins,
Services units, Config, Shell, Resources); package installation; network
download or clone; service activation and enablement; theme activation;
interactive or elevated operations; Force or Exact Restore (whether from CLI
flags or the portable profile); unknown operations; capability-version
mismatch.

## 5. Anticipated reachable Auto Follow cases

1. **New local theme:** host A captures and publishes a new local theme that is
   not active; host B (Conservative or above, themes within its grant) fetches,
   fast-forwards, copies the theme and verifies it. B's active theme is
   unchanged; applying it remains a manual choice.
2. **Default app change:** host A switches the default browser to Firefox; B
   (Balanced, Firefox installed) follows. If Firefox is not installed on B,
   the whole run is Needs review.

Both are narrow but useful, and are exactly what the plan's two-host fixture
(C5) and physical acceptance (D4) should exercise.

## 6. Expected default-deny cases (for C5 tests)

A plan containing any of these must be Needs review, with no partial
application: theme activation (including a new theme that is also the desired
current theme), a Git theme, an overlay theme, a theme whose artifact fails
its fingerprint, `defaults.set.agent`, a default whose app is absent, any
Config write or `hyprctl reload`, any Shell, Hooks, Plugins, Packages,
Services or Resources operation, any removal (including a remote change to
`restore_convergence = "exact"`), and any operation family unknown to
`restore-capabilities/1`.

## 7. Questions for the reviewer

1. Approve `themes.copy` (local, inactive) as `unattended_safe`, and default
   apps as `unattended_routine` rather than safe?
2. Accept that Hands-off equals Balanced in v1, disclosed in the UI?
3. Accept the two code preconditions (copy `SourceHash` and
   `RejectSymlinkParents` for unattended theme copies; installed-application
   evidence for defaults) as PR A/C scope?
4. Any other operation you want considered before PR A, such as a data-only
   Config path allowlist? It would need its own audit and is not proposed here.
