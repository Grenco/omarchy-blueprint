# Auto Sync Gate 0 — Restore Operation Capability Audit

**Status:** Approved (revision 4, Gate 0 passed 2026-10-17). Implemented as the versioned registry in `internal/autosync` (`DefaultRegistry`); every entry is undelivered until the PR that ships its engineering.\
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
- **T3 — Unsuitable:** cannot legitimately run unattended; reason given
  (§5). T3 is a property of an operation *form*. Conditions that stop one
  run without making a category unsuitable are **run-time blockers** (§5a).

### 2.3 Preset ceilings

Presets are ceilings over descriptors (spec §4.2); what actually runs is
`ceiling ∩ delivered registry entries ∩ local grant scope ∩ Restore Apply ∩
fresh checks ∩ no run-time blocker`.

| Preset | Ceiling |
|---|---|
| Notify only | Nothing applied. |
| Conservative | `add` only: new `data`/`config` from hash-verified `artifact` sources, with positive evidence (§2.4) that nothing loads or executes it as a result; activation `none`; `user`; no interaction. No update, replacement, removal or reload. |
| Balanced | Conservative, plus `update` of `data`/`config` (including fixed-vocabulary setters) with precondition and `backup`, and `reload`/activation **only where positive evidence (§2.4) establishes that the complete execution path runs no user-controlled code**. |
| Hands-off | Balanced, plus `exec-later`/`exec-now` from `artifact` or `pinned` sources, activation that runs user-controlled code (for example a `theme-set` hook), enabling/starting/loading, and provenance-checked removals with recovery — **only in categories with the local "may run code" authorisation** (ADR 0029 §3a). Privileged package installation additionally needs the **privileged install** authorisation and the root helper (§4.8). |
| Custom | User-chosen category × operation-family × target matrix, each cell capped by Hands-off and by the registry; privileged install is its own named cell. |

`Interaction: required`, `Source: unpinned`, unbounded or unverifiable effects
and anything in §5 are never within any ceiling.

**Hard denials are precise.** Prohibited in every preset: *unverified* remote
execution (code not hash-verified against the profile or exactly pinned, or
outside a locally authorised run-code scope) and *unproven* destructive
operations (removal or replacement without provenance match, precondition
and recovery, or outside an authorised scope). Specifically audited,
locally authorised code-running and provenance-verified removal classes in
this matrix are not hard-denied. **Precedence:** a matrix row (§4) states
the complete rule for its operation; where a general rule names an
exception (§2.4 item 5), the row defines it; anything a row does not
explicitly allow is review.

### 2.4 Positive evidence (what Balanced and Conservative require)

Content analysis identifies potentially executable content and helps choose
the permission an operation needs. It is **not** a security boundary by
itself. Absence of a detected Hyprland `exec`, shell command, Lua `require`
or similar is never proof of safety. Conservative or Balanced eligibility
requires all of:

1. **Supported format:** the file's format and loader are in the registry for
   the audited application version.
2. **Complete parsing:** the whole relevant configuration is parsed, including
   transitive includes (`source =` in Hyprland, sourced shell files, Lua
   `require`, `@import`); any unparsed, unresolved or dynamic include fails
   the evidence.
3. **Known effects:** the effect of loading the content and of any reload or
   activation command is documented for that version. For example,
   `hyprctl reload` re-reads the whole sourced graph and re-runs every `exec`
   directive in it (not `exec-once`), so a Balanced reload needs the
   **entire** graph to contain no `exec` directives, not just the changed
   file.
4. **Verified preconditions:** the live destination matches the recorded or
   expected state.
5. **Safe destination:** not an autostart, rc/profile, `PATH`, hook, unit or
   plugin location, and no executable bit. **One narrow, named exception:**
   an inert systemd user unit definition (§4.7) may be written to the user
   unit directory in Balanced only with the unit-inertness proof defined
   there; without that proof the operation is Hands-off.
6. **Verified reload/activation behaviour:** the post-apply check confirms
   the expected result.

Unknown or ambiguous formats go to review, or to a specifically supported
Hands-off execution path under the "may run code" authorisation. They are
never promoted to Balanced.

### 2.5 Delivery increments

| Increment | Delivered in | Content |
|---|---|---|
| I1 | Phase 11 PR C | Executor; first Conservative and Balanced capabilities |
| I2 | Phase 11 PR D | Positive-evidence analysis; file-based Balanced and Hands-off capabilities |
| — | Phase 11 PR E | TUI/CLI and acceptance; **first public release = I1 + I2 together** |
| I3 | Phase 11 PR F | Services, Shell, Plugins |
| I4 | Phase 11 PR G | Packages (privileged install, root helper under its own ADR) and provenance-checked removals |

## 3. Shared engineering (prerequisite for every category)

| ID | Work | Needed by |
|---|---|---|
| E1 | Effect descriptors on every provider operation, with a versioned registry; unknown → requires review | I1 |
| E2 | Snapshot-root planning proven equivalent to planning from the selected profile (machine/identity injection, profile-relative artifact paths) | I1 |
| E3 | Locked fresh-plan graph equality between preview and execution | I1 |
| E4 | Unattended executor path: no prompts, no approval callbacks, per-operation authorisation from the locked grant | I1 |
| E5 | Mandatory `SourceHash` (copies and writes) and `RejectSymlinkParents` on unattended operations | I1 |
| E6 | Best-effort journal-backed rollback of `backup` operations after a failure; reported as best-effort unless an operation has a proven transactional guarantee | I2 |
| E7 | Positive-evidence content analysis (§2.4): supported formats and loaders, complete transitive-include parsing, documented reload/activation effects, destination classes; identifies executable content, never certifies safety by non-detection | I1 (destination classes, theme inertness) · I2 (Hyprland and other formats) |
| E8 | Per-category "may run code" local authorisation and its preview copy | I2 |
| E9 | Provenance records for removal (Blueprint-installed marker or recorded fingerprint) | I4 |
| E10 | Signed-repository root helper (separate ADR): installed only by an administrator; accepts only exact package-install (and, with E9, removal) requests for packages from allowlisted signed repositories, with provenance checks, noninteractively; no arbitrary privileged commands, no locally built packages | I4 |
| E12 | AUR build-and-install design (separate review): source verification (PKGBUILD and sources at a recorded commit, source checksums enforced), isolated unprivileged build, and a reviewed way to install the resulting package with privilege that E10 does **not** provide | I4, conditional |
| E13 | Per-recipe execution support for Omarchy recipes: declared effects, noninteractive mode and privilege needs per recipe, each reviewed; privileged steps only through a reviewed mechanism for that recipe, never a general root command | I4, conditional |
| E11 | Manual-review coordination: review lease while a manual Sync review is open, and safe refresh when the reviewed revision goes stale | Phase 11 PR B |

## 4. Provider matrix

Columns: **Current** = what the operation does today and its existing
safeguards. **Target** = unattended form. **Controls** = per-run safeguards
beyond the global ones (identity, lock, plan equality, Restore Apply,
verification, cursor rules). **Work** = engineering beyond §3.
**Preset** = lowest preset whose ceiling admits it. **Inc.** = increment.

### 4.1 Themes

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Copy local theme, not active | Exclusive tree copy, refuses existing destination; no hash or symlink-parent check set | add · data · artifact | T1 | Portable fingerprint = recorded hash; destination absent; not desired current; audited Omarchy version (evidence: an inactive user theme is only listed by name and its preview images shown by `omarchy-theme-switcher`) | E5, E7 (theme inertness pinned to audited Omarchy scripts) | Conservative | I1 |
| Copy overlay theme (shadows a built-in name) | As above | add · exec-later · artifact | T2 | As above; overlay disclosed in preview | E7 (shadowed built-in counted as later code) | Hands-off | I2 |
| Activate theme (default) | `omarchy theme set` restarts components, runs post-theme commands in `bash -lc`, then `omarchy-hook theme-set` (user hooks); theme files such as `neovim.lua` become live code | update · exec-now · artifact · activate | T2 | Themes "may run code" authorised; **if a `theme-set` hook exists, Hooks "may run code" authorised as well**; verify current theme afterwards | E6, E8 | **Hands-off** (or Custom with both permissions) | I2 |
| Activate theme, positively established inert | As above | update · config · artifact · activate | T2 | Balanced **only if** positive evidence shows the complete activation path is non-executable: audited Omarchy `theme-set` version with fixed post-theme commands, **no** `theme-set` hook present, and every theme file in a supported inert format. Any user-controlled hook makes the whole action Hands-off/Custom; a separate hook permission never downgrades it to Balanced | E7 | Balanced | I2 |
| Git theme install + pin | `omarchy theme install <url>` then detach to recorded revision; installs tip first | add · exec-later · pinned | T2 | Clone verified at the exact recorded revision before it is placed | Clone-at-revision install path (no tip window) | Hands-off | I3 |
| Remove theme (Exact) | `rm -rf` via `omarchy theme remove`; provenance and active-theme guards exist | remove · reinstall | T2 | Provenance match; never the active theme; local grant includes removal | E9 | Hands-off | I4 |

### 4.2 Defaults

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Set terminal / browser / editor | Omarchy setter accepts a fixed vocabulary, writes one user file or `xdg-settings`, notifies | update · config · closed-set | T1 | Value in the pinned vocabulary; chosen app installed; previous value recorded; verify via getter | Installed-app evidence | **Balanced** | I1 |
| Set agent | Not planned: setter launches the agent | update · exec-now | T2 | — | Set-only Omarchy API (upstream) or a Blueprint setter that does not launch | Hands-off | Later (upstream) |

Switching a default app replaces an existing choice, so it is an `update` and
belongs in Balanced. There is no genuinely additive form: a default is a
single value, and Omarchy has no "add a candidate without selecting it"
operation.

### 4.3 Config

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Write missing file, positively inert | FileWrite with `ExpectedMissing`, `SourceHash`, symlink-parent rejection | add · config · artifact | T2 | §2.4 evidence: supported format and loader, no include of it from an executable context, safe destination; no reload | E7 | Conservative | I1 (destination classes) · I2 (formats) |
| Update file, positively non-executable | FileWrite with expected-hash precondition and backup | update · config · artifact | T2 | §2.4 evidence for the file and everything that includes it; precondition; backup | E6, E7 | Balanced | I2 |
| `hyprctl reload` | Command after Hypr writes; re-reads the whole sourced graph and re-runs every `exec` directive | reload | T2 | Balanced only if the **entire** sourced graph parses completely and contains no `exec` directive; otherwise Hands-off with Config "may run code"; verify Hyprland answers afterwards | E7 | Balanced / Hands-off | I2 |
| Write/update executable-class, unknown or ambiguous file | As above | exec-later · artifact | T2 | Config "may run code" authorised through a supported path; precondition; backup | E7, E8 | Hands-off | I2 |
| Delete file | FileDelete with precondition and backup | remove · backup | T2 | Precondition; backup; provenance | E6, E9 | Hands-off | I4 |
| Force replace/delete of unknown or differing target | `--force` path | — | T3 | — | — | Never | — |

### 4.4 Shell

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Write Shell config referencing only first-party, installed plugins | File write (backup when existing) | update · config · artifact | T2 | §2.4 evidence: every referenced plugin is first-party (Omarchy-shipped) and unchanged by this plan | Reference analysis | Balanced | I3 |
| Write Shell config that loads third-party plugin code | As above | exec-now · artifact | T2 | Shell/Plugins "may run code" authorised | Reference analysis, E8 | Hands-off | I3 |
| Shell restart | `omarchy-restart-shell` reloads the shell and every enabled plugin | reload | T2 | Balanced only if every enabled plugin is first-party; otherwise Hands-off (restart runs installed third-party plugin code); verify the shell is running afterwards | Health check | Balanced / Hands-off | I3 |
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
| Save unit definition, proven inert (§2.4 exception) | File writes, `daemon-reload` (re-reads definitions; runs no `ExecStart`) | add/update · exec-later · artifact | T2 | **Unit-inertness proof**, all required: a new or updated `.service` file only (not a drop-in for another unit, not a timer, socket, path, target or generator); not enabled, not active, no instance of it running; no `[Install]` alias or `Also=` that would make it reachable; not named by `Wants`/`Requires`/`BindsTo`/`PartOf`/`Upholds`/`OnSuccess`/`OnFailure`/`Triggers` or a `.wants`/`.requires` link of any loaded unit, timer, socket or path unit (checked through `systemctl --user` after `daemon-reload` and before reporting success); not a template whose instances are referenced; precondition; backup. Any check that cannot complete → Hands-off | E6, unit dependency analysis | Balanced | I3 |
| Any other unit or drop-in definition (enabled, referenced, drop-in, timer/socket/path, or proof incomplete) | As above | exec-later · artifact | T2 | Services "may run code" authorised; backup | E8 | Hands-off | I3 |
| Enable / link / unmask | `systemctl --user` verbs and wants-symlinks | activate | T2 | Services "may run code" authorised; verify unit state | E8 | Hands-off | I3 |
| Start (activation) | `systemctl --user start`, currently individually reviewed | exec-now · activate | T2 | As above; activation preference recorded; bounded start timeout | Unattended activation path replacing per-unit review | Hands-off | I3 |
| Disable / mask / remove definition | systemctl and file delete | remove | T2 | Provenance; backup | E9 | Hands-off | I4 |
| Blueprint's own Auto Sync units | Would be discovered as user units | — | — | Must be excluded from Capture and Restore | Ownership exclusion | n/a | Phase 11 PR B |

### 4.8 Packages

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| Official repository install | pacman/`omarchy pkg add` via sudo in a terminal; install scripts and package-manager hooks run as root | add · exec-now · signed · root-helper | T2 | Privileged-install authorisation; signature verification; repository allowlist; exact package set; provenance; noninteractive; verify installed. A signature proves origin, not that root-run scripts are harmless | E10 (separate ADR) | **Hands-off** (or Custom "privileged install") | I4 |
| AUR install | yay builds a PKGBUILD as the user, then sudo | add · exec-now · pinned · root (E12) | T2 | Packages "may run code" and privileged-install authorisations; PKGBUILD and sources verified at a recorded commit; isolated build; privileged install only through the E12 mechanism. A pinned PKGBUILD is **not** a signed repository package, so E10 does not cover it | Record AUR revision at Capture; E12 | Hands-off (or Custom with both permissions) | I4, **only once E12 is reviewed and delivered** |
| mise tool install / config | Writes the user mise config; downloads tools into the user's home; **no root needed** | add/update · exec-later · pinned · user | T2 | Packages "may run code" authorisation only (no privileged-install permission); exact versions; checksum or signature verification where the backend supports it, otherwise review | Version-pin enforcement; backend verification registry | Hands-off | I4 |
| Omarchy semantic recipe, noninteractive | `omarchy-install-*` scripts | exec-now | T2 | Packages "may run code"; recipe individually supported under E13 with declared effects and noninteractive mode; privileged steps only through that recipe's reviewed mechanism (and the privileged-install authorisation) | E13 | Hands-off | I4, **per recipe, once supported** |
| Omarchy recipe needing sign-in (`AwaitsYou`) | Waits for a person | — | T3 | — | — | Never | — |
| Package removal | `pacman -R`/recipes, high risk | remove · reinstall | T2 | Provenance; not a dependency of an installed package; through the mechanism that installed it (E10 for signed-repository packages, E12/E13 otherwise, mise without root) | E9 + matching mechanism | Hands-off | I4, conditional on that mechanism |

### 4.9 Resources

| Operation | Current | Target | Tier | Controls | Work | Preset | Inc. |
|---|---|---|---|---|---|---|---|
| File/directory/symlink add, positively inert | Writes with `ExpectedMissing`, hash, symlink-parent checks | add · data · artifact | T2 | §2.4 evidence: destination in a registered inert class (for example wallpapers, documents), no executable bit, not an autostart, rc, `PATH`, hook, unit or plugin location; symlink targets inside the same class | E7 (destination classes) | Conservative | I1 |
| File replace with backup, positively inert | Precondition and backup | update · data · artifact | T2 | §2.4 evidence as above; precondition; backup | E6, E7 | Balanced | I2 |
| Any other Resources file | As above | exec-later · artifact | T2 | Resources "may run code" authorised | E8 | Hands-off | I2 |
| Git project clone + checkout at revision | `git clone`, `git checkout --detach <rev>` | add · data/exec-later · pinned | T2 | Clone verified at exact revision; no submodule or LFS fetch from unrecorded remotes | Clone-at-revision path | Hands-off | I3 |
| Apply staged/unstaged patch, untracked files | GitPatch with source hash | update · artifact | T2 | Repository HEAD at recorded revision; precondition | E6 | Hands-off | I3 |

## 5. T3 — never unattended

| Case | Why it cannot run unattended |
|---|---|
| Interaction required (sign-in links, device pairing, prompts with no noninteractive form) | Needs a person present; answering for them would be impersonation |
| Credentials or privilege via stored passwords, cached sudo timestamps or answered elevation prompts | Bypasses the privilege boundary instead of using an administrator-approved mechanism (E10, E12 or E13, each reviewed for its own operations) |
| Arbitrary privileged commands | Each privileged mechanism accepts only its reviewed operation shape; a general root command path would make Auto Sync a root shell |
| Unpinned or unverifiable sources (floating branch or URL, no hash, revision or signature) | Content is not what the profile recorded; the remedy is pinning at Capture (T2), not automating the floating form |
| Operations whose steps, relevant dependencies or effects Blueprint cannot bound or verify with a supported mechanism | Without bounded effects and verification, Blueprint cannot know what it authorised or whether it succeeded |
| Conflict resolution: Force replace or delete of a target that differs from both the recorded and the expected state | That is a decision about whose change wins, not an application of recorded intent |
| Removal without matching provenance | Could destroy something Blueprint did not create |
| Portable policy expanding authority (remote `restore_convergence = exact`, conflict or activation modes, Restore Apply widening) | A downloaded profile must not grant itself unattended permission |

## 5a. Run-time blockers (stop this run; not a classification)

These stop a run before any unattended mutation and report Needs review (or
the matching status). They never make a category or operation unsuitable;
the same operation runs on a later check once the condition clears.

- Missing or invalid completed-Sync record; changed machine, branch,
  upstream or remote identity.
- Stale reviewed revisions: the remote, local profile or plan changed since
  inspection.
- Changed or revoked local permissions, or a capability-version mismatch.
- Dirty, diverged or otherwise incompatible profile history; unpublished
  local commits.
- Unsupported or unknown compatibility for this Omarchy version.
- Missing or invalid profile artifacts.
- Ambiguous target ownership (e.g. a destination that differs from both
  recorded and expected states, or no provenance for a removal).
- Unavailable credentials or network; a held profile lock (Busy) or an
  active manual review lease.
- Uncertain outcome of a previous run (the Suspended-for-safety guard).
- Verification that cannot complete.

## 6. Delivered capability by release

| Preset | First public release (I1 + I2) | After I3 | After I4 |
|---|---|---|---|
| Conservative | New inactive local themes; new config and resource files with positive evidence | — | — |
| Balanced | + default apps; config updates with positive evidence and backup; `hyprctl reload` when the whole graph has no `exec`; inert resource replacements with backup; theme activation only where positively established inert | + inert service definitions; Shell config and restart with first-party plugins only; plugin disable | — |
| Hands-off | + theme activation (default path, runs the `theme-set` hook); hooks; executable, unknown or ambiguous config; overlay themes; other resource files — each per-category authorised | + plugins (local and pinned Git); Shell with third-party plugins; service enable/start and enabled-unit definitions; Git themes and resources | + signed official-repository installs (E10, privileged-install authorisation) and user-level mise tools; AUR (once E12) and individual recipes (once E13); provenance-checked removals |

Each preset is strictly broader than the one below in every release. Later
categories appear in the UI as "not available yet" until delivered.

## 7. Review status

Revision 2 questions are resolved by the final review: I1–I4 staging approved
(Packages and removals in I4; first public release delivers I1 and I2
together); default apps are Balanced; theme activation is Hands-off unless
positively established inert; content analysis is evidence, not a boundary;
T3 extended and run-time blockers separated; privileged package installs need
Hands-off or the named privileged-install permission.
