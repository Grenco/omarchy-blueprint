# Auto Sync Gate 0 — Restore Operation Capability Audit

**Status:** Revision 2, for human review (Gate 0 of the Phase 11 plan). No
production code until this matrix, the revised spec, ADR 0029 and the plan
are reviewed.\
**Date:** 2026-10-17\
**Baseline:** `main` `aa6f127`\
**Plan:** [`2026-10-17-auto-sync-phase-11-implementation-plan.md`](../plans/2026-10-17-auto-sync-phase-11-implementation-plan.md)\
**Spec:** [`2026-10-17-auto-sync-phase-11-design.md`](../specs/2026-10-17-auto-sync-phase-11-design.md) §6\
**ADR:** [0029](../../adr/0029-machine-local-auto-sync-authority.md)

Revision 1 asked which operations are safe to run unattended *today* and
concluded almost none. That answered the wrong question. This revision asks,
for every Restore operation: what can run unattended with existing verified
mechanisms, what can after named engineering work and explicit local
authorisation, and what can never run unattended and why. Risk and the need
for engineering work are recorded as such; they are not treated as permanent
prohibitions.

## 1. Method

Every `model.Operation` a Restore plan can contain was enumerated from the
provider planners and finalizers at `aa6f127` (`internal/providers/*/`, theme
activation re-ordering in `internal/app/providers.go`, fragment merge in
`internal/workflow/read_restore.go`); no other path adds operations. The
Omarchy commands invoked were read on a live install (`/usr/bin/omarchy-*`)
to establish real side effects. Existing executor mechanisms were read in
`internal/restore/executor.go` and `journal.go`.

Provider `RiskLow`/`RiskHigh` labels were recorded but **not used** for
classification: they describe interactive Restore risk, not unattended
authority (Resources labels `git clone` low; Packages labels an
elevation-prompting install low).

## 2. Vocabulary

### 2.1 Effect descriptor (per operation)

Every operation is described on these independent dimensions (spec §6.1).
Classification and preset eligibility are computed from the descriptor, never
from the operation ID or risk label.

| Dimension | Values |
|---|---|
| Change | `add` (destination absent) · `update` (replace with expected-state precondition) · `remove` |
| Payload | `data` · `config` (ordinary configuration) · `exec-later` (content that runs when loaded, sourced, enabled or activated) · `exec-now` (the operation itself runs third-party or profile-supplied code) |
| Source | `artifact` (profile artifact verified against its recorded hash) · `pinned` (exact remote revision or checksum) · `signed` (repository signature, e.g. pacman) · `closed-set` (value from a fixed vocabulary) · `unpinned` |
| Activation | `none` · `reload` (re-read configuration) · `activate` (enable, start, set active, load) |
| Privilege | `user` · `root-helper` (a locally installed, administrator-approved helper) |
| Interaction | `none` · `possible` · `required` |
| Recovery | `backup` (journalled backup restorable) · `reinstall` · `none` |
| Verification | post-apply check exists: `yes` / `no` |

### 2.2 Readiness tiers

- **T1 — Ready:** existing executor and provider mechanisms already enforce
  the required controls; needs only a registry entry, conditions and tests.
- **T2 — Engineerable:** suitable for unattended execution after the named
  engineering work and controls, under explicit local authorisation.
- **T3 — Unsuitable:** cannot legitimately run unattended; reason given. Kept
  deliberately small (§5).

### 2.3 Preset ceilings

Presets are ceilings over descriptors (spec §4.2); what actually runs is
`ceiling ∩ delivered registry entries ∩ local grant scope ∩ Restore Apply ∩
fresh checks`.

| Preset | Ceiling |
|---|---|
| Notify only | Nothing applied. |
| Conservative | `add` of `data`/`config` from `artifact` or `closed-set`; activation `none`; `user`; no interaction. |
| Balanced | Conservative, plus `update` of `data`/`config` with precondition and `backup`; `reload` activation; activation of content classified as non-executable; `signed` installs through a root helper once delivered. |
| Hands-off | Balanced, plus `exec-later`/`exec-now` from `artifact` or `pinned` sources; `activate` (enable, start, load); `remove` with provenance match and `backup`/`reinstall` recovery. **Requires a per-category local "may run code" authorisation** (ADR 0029 §3a). |
| Custom | User-chosen category × operation-family × target matrix, each cell capped by Hands-off and by the registry. |

`Interaction: required`, `Source: unpinned` and anything in §5 are never
within any ceiling.

### 2.4 Delivery increments

| Increment | Delivered in | Content |
|---|---|---|
| I1 | Phase 11 PR C | Executor and the first Conservative capabilities |
| I2 | Phase 11 PR D | File-based Balanced and Hands-off capabilities |
| — | Phase 11 PR E | TUI/CLI and acceptance; **first release = I1 + I2** |
| I3 | Phase 11 PR F | Services, Shell, Plugins |
| I4 | Phase 11 PR G | Packages (root helper, separate ADR) and cross-category removal |

## 3. Shared engineering (prerequisite for every category)

| ID | Work | Needed by |
|---|---|---|
| E1 | Effect descriptors on every provider operation, with a versioned registry; unknown → requires review | I1 |
| E2 | Snapshot-root planning proven equivalent to planning from the selected profile (machine/identity injection, profile-relative artifact paths) | I1 |
| E3 | Locked fresh-plan graph equality between preview and execution | I1 |
| E4 | Unattended executor path: no prompts, no approval callbacks, per-operation authorisation from the locked grant | I1 |
| E5 | Mandatory `SourceHash` (copies and writes) and `RejectSymlinkParents` on unattended operations | I1 |
| E6 | Journal-backed rollback of `backup` operations after failed verification | I2 |
| E7 | Content classifier: per-format detection of executable content (Hyprland `exec`/`exec-once`, shell rc and profile files, Lua/Vim/QML/JS, desktop autostart entries, executable bit) | I2 |
| E8 | Per-category "may run code" local authorisation and its preview copy | I2 |
| E9 | Provenance records for removal (Blueprint-installed marker or recorded fingerprint) | I4 |
| E10 | Root helper with a fixed command vocabulary, installed only by an administrator (separate ADR) | I4 |

## 4. Provider matrix

Columns: **Current** = what the operation does today and its existing
safeguards. **Target** = unattended form. **Controls** = per-run safeguards
beyond the global ones (identity, lock, plan equality, Restore Apply,
verification, cursor rules). **Work** = engineering beyond §3.
**Preset** = lowest preset whose ceiling admits it. **Inc.** = increment.

### 4.1 Themes

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Copy local theme, not active | Exclusive tree copy, refuses existing destination; no hash or symlink-parent check set | add · data · artifact | T1 | Portable fingerprint = recorded hash; destination absent; not desired current | E5 | Conservative | I1 |
| Copy overlay theme (shadows a built-in name) | As above | add · exec-later · artifact | T2 | As above; overlay disclosed in preview | E7 (shadowed built-in counted as later code) | Hands-off | I2 |
| Activate theme with no executable content | `omarchy theme set` restarts components, runs post-theme commands and the `theme-set` hook | update · config · artifact · activate | T2 | Theme content classified non-executable; local hooks unchanged in this plan; verify current theme afterwards | E7, E6 | Balanced | I2 |
| Activate theme with executable content (e.g. `neovim.lua`) | As above | exec-now · artifact · activate | T2 | As above; category "may run code" authorised | E7, E8 | Hands-off | I2 |
| Git theme install + pin | `omarchy theme install <url>` then detach to recorded revision; installs tip first | add · exec-later · pinned | T2 | Clone verified at the exact recorded revision before it is placed | Clone-at-revision install path (no tip window) | Hands-off | I3 |
| Remove theme (Exact) | `rm -rf` via `omarchy theme remove`; provenance and active-theme guards exist | remove · reinstall | T2 | Provenance match; never the active theme; local grant includes removal | E9 | Hands-off | I4 |

### 4.2 Defaults

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Set terminal / browser / editor | Omarchy setter accepts a fixed vocabulary, writes one user file or `xdg-settings`, notifies | update · config · closed-set | T1 | Value in the pinned vocabulary; chosen app installed; verify via getter | Installed-app evidence | Conservative | I1 |
| Set agent | Not planned: setter launches the agent | update · exec-now | T2 | — | Set-only Omarchy API (upstream) or a Blueprint setter that does not launch | Hands-off | Later (upstream) |

Defaults are `update` but sit in Conservative because the source is a closed
vocabulary, the setter is idempotent and the previous value is a valid
setting.

### 4.3 Config

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Write missing file, non-executable class | FileWrite with `ExpectedMissing`, `SourceHash`, symlink-parent rejection | add · config · artifact | T1/T2 | Classifier says non-executable | E7 | Conservative | I1 (E7 subset for path classes) |
| Update file, non-executable class | FileWrite with expected-hash precondition and backup | update · config · artifact | T2 | Live file equals recorded baseline/previous hash (precondition), backup kept | E6, E7 | Balanced | I2 |
| `hyprctl reload` after non-executable Hypr changes | Command after writes | reload | T2 | Only when the change adds no `exec` directives; verify Hyprland still answers | E7 | Balanced | I2 |
| Write/update executable-class file (Hypr `exec`, shell rc, Lua) | As above | exec-later · artifact | T2 | Category "may run code" authorised; precondition; backup | E7, E8 | Hands-off | I2 |
| Delete file | FileDelete with precondition and backup | remove · backup | T2 | Precondition; backup | E6 | Hands-off | I4 |
| Force replace/delete of unknown or differing target | `--force` path | — | T3 | — | — | Never | — |

### 4.4 Shell

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Write Shell config referencing only installed, unchanged plugins | File write (backup when existing) | update · config · artifact | T2 | Every referenced plugin is installed and unchanged by this plan | Reference analysis | Balanced | I3 |
| Shell restart after that write | `omarchy-restart-shell` | reload | T2 | Verify the shell is running afterwards | Health check | Balanced | I3 |
| Shell config enabling a newly installed or changed plugin | As above | exec-now (loads plugin code) | T2 | Plugins "may run code" authorised | E8 | Hands-off | I3 |

### 4.5 Hooks

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Write hook | FileWrite with mode, hash, backup when existing | add/update · exec-later · artifact | T2 | Hooks "may run code" authorised; precondition; backup | E6, E8 | Hands-off | I2 |
| Delete hook | FileDelete with backup | remove · backup | T2 | Provenance; backup | E9 | Hands-off | I4 |

### 4.6 Plugins

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Local plugin copy + validate + rescan | Copy from profile (no hash set), `omarchy plugin validate`, rescan loads it | add · exec-now · artifact | T2 | Portable fingerprint verified; validate succeeds; plugins "may run code" authorised | E5, E8 | Hands-off | I3 |
| Git plugin install + pin + validate + rescan | `omarchy plugin add <url> --yes` then detach to revision | add · exec-now · pinned | T2 | Clone verified at exact revision before placement; remote identity recorded | Clone-at-revision install path | Hands-off | I3 |
| Enable / disable | `omarchy plugin enable/disable` | activate | T2 | Enable: authorised as above; disable: Balanced | E8 | Hands-off (enable) / Balanced (disable) | I3 |
| Remove (Exact) | `omarchy plugin remove --yes`; provenance guard exists | remove · reinstall | T2 | Provenance; pinned source recorded for reinstall | E9 | Hands-off | I4 |

### 4.7 Services

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Save unit/drop-in definition, not enabled | File writes, `daemon-reload` | add/update · exec-later · artifact | T2 | Unit not enabled or started by this plan; backup | E6 | Balanced | I3 |
| Enable / link / unmask | `systemctl --user` verbs and wants-symlinks | activate | T2 | Services "may run code" authorised; verify unit state | E8 | Hands-off | I3 |
| Start (activation) | `systemctl --user start`, currently individually reviewed | exec-now · activate | T2 | As above; activation preference recorded; bounded start timeout | Unattended activation path replacing per-unit review | Hands-off | I3 |
| Disable / mask / remove definition | systemctl and file delete | remove | T2 | Provenance; backup | E9 | Hands-off | I4 |
| Blueprint's own Auto Sync units | Would be discovered as user units | — | — | Must be excluded from Capture and Restore | Ownership exclusion | n/a | Phase 11 PR B |

### 4.8 Packages

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Official repository install | pacman/`omarchy pkg add` via sudo in a terminal | add · exec-now · signed · root-helper | T2 | Signature verification; repository allowlist; noninteractive; verify installed | E10 (separate ADR) | Balanced | I4 |
| AUR install | yay builds a PKGBUILD as the user, then sudo | add · exec-now · pinned · root-helper | T2 | PKGBUILD at a recorded commit; build isolation; plugins "may run code" equivalent for Packages | Record AUR revision at Capture; E10 | Hands-off | I4 |
| mise tool install / config | Writes the mise config; downloads tools | add/update · exec-later · pinned | T2 | Exact versions; mise checksum verification where the backend supports it | Version-pin enforcement | Hands-off | I4 |
| Omarchy semantic recipe, noninteractive | `omarchy-install-*` scripts | exec-now | T2 | Recipe marked noninteractive in Omarchy's catalogue; root helper if needed | Recipe interaction metadata | Hands-off | I4 |
| Omarchy recipe needing sign-in (`AwaitsYou`) | Waits for a person | — | T3 | — | — | Never | — |
| Package removal | `pacman -R`/recipes, high risk | remove · reinstall | T2 | Provenance; not a dependency of an installed package; root helper | E9, E10 | Hands-off | I4 |

### 4.9 Resources

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| File/directory/symlink add, non-executable | Writes with `ExpectedMissing`, hash, symlink-parent checks | add · data · artifact | T1/T2 | Classifier: not executable, not an autostart or rc location | E7 (path classes) | Conservative | I1 |
| File replace with backup | Precondition and backup | update · data · artifact | T2 | Precondition; backup | E6 | Balanced | I2 |
| Executable or autostart file | As above | exec-later · artifact | T2 | Resources "may run code" authorised | E7, E8 | Hands-off | I2 |
| Git project clone + checkout at revision | `git clone`, `git checkout --detach <rev>` | add · data/exec-later · pinned | T2 | Clone verified at exact revision; no submodule or LFS fetch from unrecorded remotes | Clone-at-revision path | Hands-off | I3 |
| Apply staged/unstaged patch, untracked files | GitPatch with source hash | update · artifact | T2 | Repository HEAD at recorded revision; precondition | E6 | Hands-off | I3 |

## 5. T3 — never unattended

| Case | Why it cannot run unattended |
|---|---|
| Interaction required (sign-in links, device pairing, prompts with no noninteractive form) | Needs a person present; answering for them would be impersonation |
| Credentials or privilege via stored passwords, cached sudo timestamps or answered elevation prompts | Bypasses the privilege boundary instead of using an administrator-approved mechanism (E10 is the legitimate route) |
| Unpinned or unverifiable sources (floating branch or URL, no hash, revision or signature) | Content is not what the profile recorded; the remedy is pinning at Capture (T2), not automating the floating form |
| Conflict resolution: Force replace or delete of a target that differs from both the recorded and the expected state | That is a decision about whose change wins, not an application of recorded intent |
| Removal without matching provenance | Could destroy something Blueprint did not create |
| Portable policy expanding authority (remote `restore_convergence = exact`, conflict or activation modes, Restore Apply widening) | A downloaded profile must not grant itself unattended permission |

## 6. Delivered capability by release

| Preset | First release (I1 + I2) | After I3 | After I4 |
|---|---|---|---|
| Conservative | New inactive local themes; default apps; new non-executable config and resource files | — | — |
| Balanced | + non-executable config updates with backup and `hyprctl reload`; non-executable theme activation; resource file updates | + saved service definitions; Shell config with trusted plugins and restart; plugin disable | + official-repository installs via root helper |
| Hands-off | + hooks; executable config (Hypr `exec`, shell rc); executable or overlay themes; executable resource files — each per-category authorised | + plugins (local and pinned Git); service enable/start; Git themes and resources | + AUR/mise/recipes; provenance-checked removals |

Each preset is strictly broader than the one below in every release.

## 7. Questions for the reviewer

1. Is the I1–I4 split right, in particular Packages (root helper, separate
   ADR) and all removals in I4?
2. Should non-executable theme activation be Balanced, given that it runs the
   *local, unchanged* `theme-set` hook?
3. Approve the content classifier (E7) as the boundary between Balanced and
   Hands-off for Config, Themes and Resources?
4. Is the T3 list complete?
