# Phase 11 — Auto Sync Design Specification

**Project:** Omarchy Blueprint (`Grenco/omarchy-blueprint`)\
**Status:** Approved (revision 4); ADR 0029 accepted. Implementation proceeds through the plan's per-PR review gates.\
**Draft date:** 2026-10-17\
**Reference baseline:** merged `main` `aa6f127e37f00bd38cf0c3d91bee02aacdfe5d88` (PR #74 merge; its tree is the reviewed PR head `db86c11`).\
**Basis:** `ROADMAP.md` §69; `docs/planning/specs/2026-10-03-sync-engine-v1-design.md` (especially §Future Phase 11 constraints); ADR 0027; existing workflow/profilegit/syncengine code.\
**Gate:** Approval of this spec authorizes drafting an ADR and implementation plan only. It does **not** authorize coding, creating timers, enabling unattended work, editing the live profile, modifying GitHub, merging, tagging or releasing.

## 1. Intent and user promise

Auto Sync extends the **existing semantic Sync Engine** so a selected Omarchy computer can periodically check a shared Blueprint profile and, when explicitly authorised, apply changes without requiring a fresh interactive confirmation every time.

**Product promise:** “Blueprint keeps an eye on your shared setup. It can automatically handle only the changes you have allowed on this computer. Anything uncertain waits for your review.”

Auto Sync is not a daemon that blindly runs `git pull`, `capture`, `restore` and `push`. It must reuse Sync's pinned Git history, machine-local last-successful-Sync state, provider-defined target semantics, policy gates, fresh Restore planning, verification, and failure rules. The existing machine remains the configuration editor; Git remains the shared profile history.

A preset provides *proposed automation permission*, not permission to bypass hard safety requirements. All installed automation is opt-in; the initial setting is **Off**, with read-only Notify only offered as the safest first choice. The suggested active preset is **Balanced**, linked to Restore settings, only after an explicit local preview and confirmation.

### 1.1 What a user should understand

- **Check for changes:** Look at the shared profile and this computer; do not change the live system.
- **Automatically apply:** Apply only explicitly allowed, supported, freshly checked changes from the saved profile to this computer.
- **Automatically share:** Save this computer's changes into the shared profile. This is a separate permission and is **not** included in the initial release.
- **Needs review:** Nothing dangerous has been guessed or silently skipped; open manual Sync to decide.
- **Last completed Sync:** A result verified by Sync Engine, not merely the time of the last background check.

## 2. Scope and release boundary

### 2.1 Phase 11 scope

Phase 11 delivers **Auto Follow** across all supported categories, in increments. The capability audit (`docs/planning/audits/2026-10-17-auto-sync-restore-capabilities.md`) is the source of truth for which operation reaches which preset in which increment.

1. Local, opt-in Auto Sync settings with presets, custom category/operation/target permissions, a separate per-category “may run code” authorisation, and three Restore-link modes.
2. Scheduled semantic Sync inspections, with an explicit fetch of the configured tracking branch under the profile lock (non-blocking).
3. Machine-local classification of what an inspected **entire plan** would be allowed to execute without interaction, from provider effect descriptors and a versioned capability registry.
4. Unattended **Auto Follow** for an eligible remote-only, fast-forward, conflict-free plan; all writes through the existing Sync execution architecture and fresh Restore planning.
5. Provider engineering that moves operations from “engineerable” to “delivered” (content classification, clone-at-revision installs, journal rollback, provenance records, an administrator-approved root helper).
6. Local status and bounded, sanitized run history; notifications for pending review, completion and actionable failure.
7. TUI settings/status/preview and scriptable CLI equivalents; managed `systemd --user` timer/service as scheduling mechanism.
8. Multi-host fake-PTY acceptance, failure-injection tests and real-machine acceptance gates per release.

**First public release** (after Phase 11 PR E, delivering I1 and I2 together): Notify only, plus Conservative, Balanced and Hands-off capabilities for Themes, Defaults, Config, Hooks and Resources file operations (increments I1 + I2), each preset strictly broader than the one below. **Later Phase 11 increments:** Services, Shell and Plugins (I3); Packages through a privileged-install authorisation and an administrator-installed helper, and provenance-checked removals in every category (I4). Categories not yet delivered appear as “not available yet”.

### 2.2 Explicitly deferred

- **Auto Publish:** unattended Capture, commits and push. A separately authorised later increment after Auto Follow acceptance; no functional toggle until implemented and approved.
- **Auto Reconcile:** unattended semantic merging of disjoint local/remote committed changes (roadmap Phase 12).
- **Partial-plan execution** (running eligible actions while others wait): only with the proof obligations in ADR 0029 §5; until then the whole run stops and notifies.
- Automatic selection of conflicting target intent, remembered conflict decisions, persisted live-observation caches, watchers or push subscriptions.
- Unattended first-time Sync setup, cursor reset or trust-on-first-use of new machine/profile/remote identity.
- Operations classified **T3** in the audit (interaction that needs a person, privilege through passwords or answered prompts, unpinned sources, conflict resolution, removal without provenance, authority expansion from portable policy). These are permanent exclusions, not deferrals.
- More than one configured tracking branch, force push, rebase or Git history rewriting.
- Profile cloning from a URL at initial setup and the optional Omarchy shell/bar plugin (compatible follow-on features).

### 2.3 Phase boundary

**Phase 11 PR A:** authority model, grant store, effect descriptors and registry framework, presets, “may run code” authorisation, preview (no unattended mutation).\
**Phase 11 PR B:** scheduler, locked fetch/inspection, unified status, notifications (read-only).\
**Phase 11 PR C:** Auto Follow executor, snapshot-planning equivalence, increment I1 (first Conservative and Balanced capabilities).\
**Phase 11 PR D:** increment I2 (file-based Balanced and Hands-off capabilities, positive-evidence content analysis, best-effort journal rollback).\
**Phase 11 PR E:** TUI/CLI management, docs, two-machine and physical acceptance → first release.\
**Phase 11 PR F:** increment I3 (Services, Shell, Plugins).\
**Phase 11 PR G:** increment I4 (Packages: signed official-repository installs via a root helper under its own ADR; user-level mise tools; AUR and Omarchy recipes each conditional on their own reviewed mechanism; provenance-checked removals).\
Auto Publish receives its own reviewed design/PRs after Auto Follow acceptance.

## 3. Design principles and non-negotiable invariants

1. **Explicit local authority.** Portable profile data—including Capture/Restore policies—cannot enable, unpause or expand unattended execution on another computer.
2. **Intersect permissions, never union them.** Every mutation needs (a) existing effective Restore Apply, (b) the local Auto Sync grant, (c) a provider-supported unattended capability, and (d) current plan/Restore/safety/compatibility approval.
3. **Presets are ceilings, not guesses.** No preset authorises an unknown operation or a newly introduced provider operation type. Authority is computed from effect descriptors, not from `RiskLow`/`RiskHigh`.
4. **No interactive approval impersonation.** Do **not** automate the current `--yes` path or supply permissive fake `ApproveRestore`/`ApproveCapture` closures. Add a distinct Auto Sync execution-authority path that verifies the machine-local grant under the same lock and freshly validates operation-level scope.
5. **No first-run automation.** A manual Sync must already have completed successfully and established a valid machine-local base. Remote/branch/machine identity changes require a new manual review.
6. **All-or-nothing eligibility, not atomic execution.** Every change in the proposed plan must be eligible before any unattended mutation begins; an authorised subset is never executed while another required change is unresolved. Intentional, correctly policy-preserved differences with no proposed action do not block. Execution is not atomic across Git, files, external commands and machine state (§5.4).
7. **Fail closed.** Unknown safety facts, malformed policy, stale revisions, dirty profile checkout, divergent history, conflicting desired state, untrusted artifacts, missing credentials or interactive requirements stop execution.
8. **Exact history and ordering.** Revalidate pinned B/L/R/M/identity, keep the existing profile lock, update the checkout with the existing safe Git primitives, plan Restore freshly, verify, then advance the cursor only through Sync Engine's established success path.
9. **No hidden profile-management mutation during checks.** Inspections may fetch remote tracking refs as explicitly disclosed; they do not pull, commit, change profile files, apply settings or write the cursor.
10. **No privileged system service.** Run as the logged-in user without root escalation; never enable user lingering silently.
11. **Stop means stop.** Disabling or pausing prevents new automatic application, even if a timer remains scheduled or a profile requests otherwise. Recheck the local grant at the last authoritative boundary.
12. **Human-readable consequences.** The user sees what would change on this computer, why something was withheld, and how to turn automation off, without terms such as B/L/R/M, cursor, lineage or verified settlement.

## 4. User configuration model

### 4.1 One status model

Auto Sync keeps two separate things and never mixes them:

- **Setting** (chosen by the user, persisted locally): **Off** (initial) · **On** with a preset · **Paused**. A persisted **Suspended for safety** guard can be set by a failed or uncertain run; while set, nothing is applied until a person reviews it, whatever the setting says. “Notify only” is a preset of On, not a separate state.
- **Outcome of the last check** (reported, never a setting): **Up to date** · **Applied and verified** · **Needs review** · **Busy** (a manual Sync held the lock or a manual review was open; skipped) · **Offline** · **Sign-in needed** · **Incomplete** (sets the safety guard) · **Error**.

Status always shows the setting, the last check time and outcome, the next scheduled check, and the last **completed Sync** (verified by Sync Engine) separately. An outcome never rewrites the selected preset.

On first enablement: confirm valid machine/profile/upstream identity, previous manual Sync completion, usable background Git auth, and a dry-run evaluation of the exact current plan. No automatic Restore occurs simply because the user opens the settings screen or selects a preset.

### 4.2 Presets

| Preset | User-facing description | Machine-local ceiling (audit §2.3) |
|---|---|---|
| **Notify only** | “Check for changes and tell me when something needs attention. Never apply changes.” | Fetch/inspect/status/notify only. |
| **Conservative** | “Automatically add only the simplest, verified things.” | **Add only**: new themes not in use, and new configuration or files Blueprint can positively show nothing will load or run. No updates, replacements, removals, reloads, activation or code. |
| **Balanced** | “Automatically handle routine changes. Ask before anything significant.” | Conservative plus updates to ordinary configuration and fixed-choice settings (such as default apps) with a precondition and backup, and reloads or activation **only where Blueprint can positively show nothing user-controlled runs** (for example `hyprctl reload` when no `exec` line exists anywhere in the loaded Hyprland configuration). Recommended when opting in. |
| **Hands-off** | “Also handle changes that install or run code, in the categories I allow.” | Balanced plus code-bearing content (hooks, plugins, executable or unrecognised config, theme activation that runs a hook, service enable/start, AUR/mise packages) from hash-verified or exactly pinned sources, provenance-checked removals with recovery, and privileged package installs with the separate privileged-install permission — **only in categories given the “may run code” authorisation**. Hard safeguards still apply. |
| **Custom** | “Choose which changes may run automatically.” | Category × operation-family permissions with optional target exceptions, capped by Hands-off and the registry; **privileged install** is its own named permission. |

Each preset is strictly broader than the one below in every release. Where a preset's ceiling admits operations that are not yet delivered, the preview lists them as “not available yet” instead of implying they run, and Hands-off is never presented as equal to Balanced.

**Positive evidence.** Content analysis finds potentially executable content and chooses the permission an operation needs; it is not a security boundary. Conservative and Balanced eligibility requires positive evidence (audit §2.4): supported format and loader, complete parsing including transitive includes, known effects of loading and of any reload or activation command, verified preconditions, a safe destination, and verified post-apply behaviour. Failing to detect an `exec` line, shell command or Lua import is never proof of safety; unknown or ambiguous content goes to review or to a supported Hands-off path.

**Implementation detail:** the preset-to-operation mapping is a *versioned capability registry* keyed by provider and effect-descriptor shape, with conditions. Unknown or new operation types default to review. The audit classifies every current operation into T1 (ready), T2 (engineerable, with named work) or T3 (unsuitable, with reason), assigns preset eligibility and delivery increment, and lists run-time blockers separately.

### 4.2a Permission to run code

Selecting Hands-off, or a Custom cell that includes code-bearing operations, opens a separate local confirmation per category: what kind of code could run (for example “hook scripts from your shared profile run when Omarchy events happen”), from which sources (profile files verified by fingerprint, or exactly pinned repositories), and that future changes in that category are covered. It is independent of Capture and Restore permissions, bound to this computer's profile and repository identity, revocable at once, and rechecked on every run. Downloaded profiles and portable policy never create or widen it. It never stores passwords, answers prompts or bypasses privilege requirements.

**Privileged install** is a further named permission (Hands-off and Custom only) for package installs whose scripts and package-manager hooks run as root, even from signed repositories. Each kind of installation needs its own reviewed mechanism: signed official-repository packages use an administrator-installed root helper with its own ADR, which accepts exact package requests from allowlisted signed repositories with provenance checks, runs noninteractively, and never executes arbitrary privileged commands; AUR packages need a separately reviewed build, source-verification and installation design; Omarchy recipes need per-recipe reviewed support. User-level mise tools need the Packages run-code authorisation but no privileged authority.

### 4.3 Custom overrides

The interface starts with **categories**, each collapsible into **operation types** (add, update, remove, save definition, activate, run code) and **targets**. Users can grant or withhold permission for a category and optionally override specific targets; no need to visit every target. A target deny always beats a category allow. A target allow may expand the selected preset only after explicit local confirmation and never beyond provider/hard-safety limits. A custom change that widens unattended authority requires a new local preview and confirmation. Display category counts and examples rather than dumping all targets.

Independent dimensions: category/target scope, operation family (`add`, `update`, `remove`, `activate`, `execute`, etc.), provider-audited unattended class, permission to update local profile checkout, and (in a future release) permission to Capture/publish. A category setting does not implicitly authorise running arbitrary source-controlled code.

### 4.4 Restore linkage

Three modes:

1. **Keep linked** (recommended): effective Restore settings continue constraining automatic application. The user locally approves an **authority ceiling** computed from the chosen preset/overrides and Restore applicability when enabling or changing permissions. More restrictive Restore policies immediately reduce the Auto Sync-eligible set. Any remote or local Restore-policy change that would **expand** what Auto Sync could apply beyond the last locally confirmed ceiling is *pending local review*, never adopted automatically.
2. **Copy once:** initialise local Auto Sync scope from the current effective Restore settings and freeze that local scope. Later Restore policy changes do not rewrite this Auto Sync scope. **Current Restore Skip still always vetoes actual Restore.** Widening beyond the locally copied scope requires local approval.
3. **Keep separate:** configure local Auto Sync scope without importing Restore's choices. Existing Restore Apply/Skip is still an independent mandatory gate; Auto Sync cannot override a Skip.

“Linked” means linked *restrictions*, not delegated permission creation. Local approval binds the ceiling to the selected profile-root identity, machine name, branch/upstream and remote identity, explicit grant version, selected preset, override set and audited capability version. Do not require exact profile SHA to remain constant (normal syncing changes it); track permission-expanding policy diffs against the confirmed grant. If that comparison cannot be proved safely, stop and ask for reconfirmation.

**Examples:**
- Themes Restore=Skip → Auto Sync cannot apply that theme regardless of preset.
- Later change to Restore=Apply → eligible automatically **only if** this specific operation/target was within the preconfirmed local authority ceiling; otherwise needs local confirmation.
- New target added to a category → no automatic authority unless its category/class was explicitly covered in the locally confirmed grant *and* it passes the same audited, bounded effects and provider checks. Unknown/new operation classes always need review.
- Profile contains new/changed Restore, Capture, resource mapping or provider selection rules → Sync's normal control-plane safety still applies. Automation must separately check whether local authority would widen; if uncertain, stop.

### 4.5 Where permissions live

Persist Auto Sync configuration **only in user-private, machine-local state**, in the existing profile-root-keyed state directory (same isolation and identity discipline as the Sync cursor), not in the Git profile, machine overlay, or provider-captured files. Suggested versioned schema (illustrative, to be finalised in the implementation plan):

```toml
version = 1
enabled = false
mode = "notify-only"       # notify-only | conservative | balanced | hands-off | custom
restore_link = "linked"   # linked | copy-once | separate
interval_minutes = 30
pause = false
# locally approved authority envelope, target denies, audited capability version,
# and machine/profile/upstream identity are stored here; never Git credentials
```

All reads must validate schema, ownership, root identity and file type, enforce size limits and reject symlink traversal. Atomic private writes (`0600` file in private state directory); no permissive fallback for malformed, missing or untrusted grants. Changes to grant or permission version must be protected against concurrent run races. The *configuration file is not itself a remotely importable grant*. A local interactive confirmation is required for expansion; direct CLI edits require an explicit, locally authenticated approval workflow or are rejected for widening.

**Upgrade rule:** New Blueprint versions that introduce new effect classes or change their meaning cannot silently inherit broader grants; unknown/missing versioned classifications block affected automation until re-reviewed.

## 5. Background operation and headless execution

### 5.1 Scheduler

Use one Blueprint-owned `systemd --user` oneshot service triggered by a `systemd --user` timer. The units **schedule** work; the Go workflow decides everything. Initial suggested default: check roughly every **30 minutes** while the user manager is running, with modest jitter and coalescing, configurable within a bounded range (e.g. 15 minutes to 24 hours). A check after user-manager startup may be scheduled with a short delay. No root system service, wake-from-suspend request, implicit linger, or per-profile proliferating daemons.

`systemd` calendar/monotonic timers support coalescing and optional catch-up; the implementation plan must select precise unit semantics and test login/logout/sleep. Do not silently enable lingering to make checks occur while logged out. Report that behaviour accurately in the UI.

Scheduled checks and previews take the existing profile lock **non-blocking** around fetch and inspection, because a fetch moves Git refs; a held lock records a skipped **Busy** check and never waits for or interferes with a manual Sync. The lock does not protect a manual review a person is still reading: an open manual Sync review holds a bounded, heartbeated **review lease** in local state, and scheduled checks skip fetching (Busy) while a live lease exists; the lease lapses on its own if the TUI exits uncleanly. Because the remote can still move (a manual `git fetch`, another tool, or a check after a lapsed lease), the review re-checks the tracking revision before confirmation and, if it moved, says “New changes arrived from your shared profile while you were reviewing. Your decisions are kept where they still apply.” and offers **Refresh** instead of failing at execution. Background invocations must set noninteractive Git/network execution settings, bounded timeouts, cancellation and bounded retries (with backoff; no aggressive polling). Authentication requiring a prompt stops as **Needs attention**, not as a hanging background process. Do not log token-bearing URLs or subprocess stdout/stderr. Reuse remote identity sanitisation. Offline checks never mark the computer up to date.

### 5.2 Two-phase run

**Phase 1 — Inspect:** open the configured machine/profile; fetch the configured upstream; validate the last completed Sync, Git state and remote identity; perform a single logical live observation; compute the *unmodified* Sync plan; evaluate blockers, every target, required actions and all locally approved capabilities. Save an ephemeral, bounded, non-sensitive result for UI/notification. No execute authority is gained merely by inspecting.

**Phase 2 — Apply (only if entire plan qualifies):** begin an Auto Sync execution path with the existing exclusive profile lock. Reload local grant and identity. Re-fetch/reinspect or verify the exact pinned remote revision as appropriate; revalidate authoritative target decisions/Restore eligibility and every *fresh* operation against the grant. Reuse the existing safe fast-forward/checkout, scope planning, publication verification, Restore execution and post-apply verification, retaining cursor semantics. If any new operation appears or any fact becomes stale/uncertain, abort before a machine mutation and report Needs review. The plan and grant are never independent authority to skip fresh checks.

The existing `ExecuteSync` requires fresh interactive approval callbacks and assumes a broader Capture/publish route. **Do not pass dummy yes callbacks.** Add an explicit `ExecuteAutoFollow`/authority abstraction (actual API to be designed) that reuses shared internal execution primitives but authorises each operation from a verified machine-local grant under lock. Initially reject any plan with `ActionCapture`, unpublished local committed changes, semantic merges, machine-choice/history-choice requirements, `Defer`, or a Restore operation not supported and locally granted unattended. Existing Sync identity, lock, publication-before-Restore and cursor checks remain mandatory. For remote-only fast-forward, distinguish validating remote reachability from *pushing*; avoid needless outgoing Git writes, but maintain the existing exact-publication verification contract.

A **no-op** check never claims to have executed changes. A remote fast-forward with entirely intentionally preserved targets may record a completed Sync only if the existing engine proves settlement; do not invent a shortcut that simply advances the cursor. An ordinary local profile worktree/index edit is a blocker, not something to stash or discard.

### 5.3 Whole-plan decision algorithm

Pseudocode (conceptual):

```text
if disabled or paused: report and stop
if no valid prior manual Sync or changed machine/profile/remote identity: needs_review
plan = inspect_sync(fetch=true)
if inspection error, blocker, conflict, defer or uncertain eligibility: needs_review
if plan requires capture, automatic publish, semantic merge or local-resolution choice: needs_review
if any operation is not permitted by current Restore Apply: needs_review
if any operation is outside the locally confirmed Auto Sync envelope: needs_review
if any operation lacks a delivered registry entry or fresh safety evidence: needs_review
if any code-bearing operation lacks the local "may run code" authorisation for its category: needs_review
if any operation needs a prompt/Force/Exact override/unknown compatibility: needs_review
if no actions needed: report checked; mark settled only through existing verified logic
else: execute pinned Auto Follow under lock, reauthorising the fresh full operation set
if execution/verification fails: report incomplete; never advance cursor prematurely
```

Reason codes are machine-readable and mapped to safe, plain-English help; profile contents and credentials are not stored in run history.

### 5.4 Failure semantics

**Run-time blockers** stop a run before any unattended mutation and never make an operation or category unsuitable: missing or invalid completed-Sync record; changed identity; stale reviewed revisions; changed or revoked local permissions or capability version; dirty, diverged or incompatible profile history; unsupported compatibility; missing artifacts; ambiguous target ownership; unavailable credentials or network; Busy lock or live review lease; the Suspended-for-safety guard; verification that cannot complete. The same operation runs on a later check once the condition clears.

**Execution is not atomic.** All-or-nothing applies to eligibility, not to execution across Git, files, external commands and machine state.

- Network/auth failure: no checkout/application/cursor update; backoff and notify once rather than spam.
- Busy profile lock or live review lease: skip; never fight with a manual Sync.
- Changed profile SHA or grant while executing: abort/reinspect before the next mutation; **do not** continue on stale authorisation.
- Policy expansion since local approval: hold the affected automatic action and request local confirmation.
- Failure after mutation began (after fast-forward, or part-way through Restore): record exactly what completed, failed and was not attempted; set **Suspended for safety**; leave the completed-Sync record behind; never report “up to date”. Best-effort rollback from journalled backups runs only for operations with backup recovery and is reported as best-effort, unless an operation has a proven transactional guarantee. Never automatically repeat a non-idempotent operation; an automatic retry re-runs only operations whose idempotence is established, after the guard is cleared.
- Recovery: the notification and status point to manual Sync review, which re-plans from the current state of the profile and this computer; the user clears the guard there.
- Unexpected error: sanitized reason code and safe remediation; do not present raw Git/provider errors or leak paths by default.
- Ctrl-C/systemd stop: propagate cancellation; release locks and preserve Sync's failure rules. Disabling automation cannot retroactively undo a mutation already completed.

## 6. Operation audit, risk and content trust

### 6.1 Effect descriptors

Unattended eligibility is derived from the **fresh executable Restore plan**. Every operation carries a provider-supplied effect descriptor: change (`add`, `update`, `remove`), payload (`data`, `config`, `exec-later`, `exec-now`), source trust (`artifact`, `pinned`, `signed`, `closed-set`, `unpinned`), activation (`none`, `reload`, `activate`), privilege (`user`, `root-helper`), interaction, recovery and verification. The strongest effect across dependencies and bundles wins; an unknown dimension means review. `RiskLow`/`RiskHigh` are not inputs.

### 6.2 Readiness tiers and category routes

The audit classifies every operation as **T1 Ready**, **T2 Engineerable** or **T3 Unsuitable**. Current interactivity or the ability to run code identifies engineering work and the need for the §4.2a authorisation; it does not by itself make an operation T3. T3 also covers operations whose steps, relevant dependencies or effects Blueprint cannot bound or verify with a supported mechanism. Run-time blockers (§5.4) are separate and never make an operation T3. Every category has a route to meaningful automation:

| Category | Route (audit §4) |
|---|---|
| Themes | New inactive local themes (Conservative); activation in Hands-off by default because it runs the `theme-set` hook and post-theme commands, Balanced only with positive proof that the complete path is non-executable (no hook present; a separately authorised hook still makes it Hands-off/Custom); executable, overlay and pinned Git themes (Hands-off); removal with provenance (Hands-off, I4). |
| Defaults | Terminal, browser and editor from Omarchy's fixed vocabulary when the app is installed (Balanced: switching a default is an update); agent after a set-only setter exists. |
| Config | New positively inert files (Conservative); updates with positive evidence, precondition and backup (Balanced); `hyprctl reload` in Balanced only when the whole sourced graph has no `exec` directive; executable, unknown or ambiguous files (Hands-off); deletes with provenance and backup (Hands-off, I4). |
| Shell | Config and restart with first-party plugins only (Balanced); anything loading third-party plugin code (Hands-off). |
| Hooks | Hook writes from hash-verified profile content with backup (Hands-off); deletes with provenance (I4). |
| Plugins | Local and exactly pinned Git plugins with validation and load (Hands-off); disable (Balanced); remove with provenance (I4). |
| Packages | All Hands-off (or Custom), I4: signed official-repository installs through the administrator-installed helper with the privileged-install permission; pinned mise tools at user level (run-code permission only); AUR once its own build/verify/install design is reviewed; individual Omarchy recipes once each is supported; removals with provenance through the mechanism that installed the package. Recipes that need sign-in never run unattended. |
| Services | Saving a `.service` definition with a complete unit-inertness proof (audit §4.7; the one named exception to the safe-destination rule) in Balanced; any other definition, drop-in, enable or start in Hands-off. |
| Resources | New positively inert files (Conservative); inert replacements with backup (Balanced); any other files, pinned Git projects and patches (Hands-off). |

### 6.3 Content trust

A version-controlled profile can contain scripts, executable configuration, service units, URLs or commands. Such content runs unattended only when (a) the user granted the §4.2a authorisation for that category, (b) the content is exactly what the profile recorded (hash-verified artifact, exact revision, or repository signature), (c) a supported, noninteractive execution mechanism exists, and (d) post-apply verification exists. Floating or unverifiable sources never run unattended. A command marked RiskLow is not thereby eligible, and any registry change, capability upgrade/downgrade or broadened meaning requires tests and a capability-version bump that invalidates broader grants.

## 7. User interface and CLI contract

### 7.1 TUI structure and exact primary copy

Add an **Auto Sync** panel reachable from Sync (a dedicated settings view or subtab, to match established TUI navigation). Use collapsible sections: **Status**, **How automatic?**, **Use Restore settings**, **What can run automatically?**, **Needs review**, **Recent activity**. Large lists collapse by default, with blockers visible first. Never expose B/L/R/M, cursor, lineage or Git hash in primary copy; technical details may show them on demand.

**Title:** “Auto Sync”\
**Intro:** “Blueprint can check your shared setup in the background and automatically handle changes you allow on this computer. Anything that needs a decision will wait for you.”\
**Off:** “Auto Sync is off. Your computer will not check for or apply shared changes in the background.”\
**Preview heading:** “What Auto Sync would do”\
**Preview line:** “These changes could happen automatically. Everything else will wait for your review.”\
**Linking:** “Use my Restore choices to limit what Auto Sync can change.”\
**Confirmation:** “Turn on Auto Sync with these permissions on this computer?”\
**Important notice:** “Changes downloaded from your shared profile cannot give Blueprint new permission to run automatically.”\
**Run-code confirmation:** “Allow Auto Sync to install and run {category} from your shared profile on this computer? This covers future changes to {category}. Only content that exactly matches what your profile recorded is used, and you can turn this off at any time.”\
**Not yet available:** “Hands-off will also handle {operations} in a later Blueprint version. Until then they wait for your review.”\
**Privileged install confirmation:** “Allow Auto Sync to install packages from {repositories} as administrator on this computer? Package installers can run their own scripts with full system access. Blueprint only installs the exact packages your profile lists, through the helper your administrator set up.” (AUR and recipe wording is defined with their mechanisms.)\
**Review went stale:** “New changes arrived from your shared profile while you were reviewing. Your decisions are kept where they still apply.” (action: **Refresh**)\
**Help:** “Automatic checking is different from automatic application. You can pause or turn off Auto Sync at any time.”

Required actions: **Preview**, **Enable**, **Pause**, **Resume**, **Check now**, **Review pending changes**, **Change permissions**, **Turn off**, **View recent activity**. Any widening of permission requires a local confirmation showing specific new categories/operation types/targets; code-bearing scopes use the run-code confirmation per category. No quick action may make an unattended change before the authoritative full-plan preview gate. All screens must say whether an action checks only, changes the saved profile, or changes the live machine.

### 7.2 CLI (proposed command surface; names to be finalised in implementation plan)

- `auto-sync status` — read-only status/JSON.
- `auto-sync check` — explicit fetch and inspect; read-only aside from tracking refs; no unattended execute.
- `auto-sync preview` — inspect and classify the entire proposed plan against the current local grant.
- `auto-sync enable` / `configure` — local interactive preview/confirmation; CLI JSON/non-TTY cannot silently grant broader authority via `--yes`.
- `auto-sync pause` / `resume` / `disable` — local controls; disable preserves safe history but removes execution permission.
- `auto-sync run` — execute only an already confirmed auto grant; it is not an interactive Sync substitute.
- `auto-sync history` — sanitized bounded reports.

Always expose machine/profile identity and last check vs last completed Sync. JSON uses typed status codes, counts and decision reasons; never include raw credentials or secret-bearing target values. No command should make “automatic” a synonym for `sync apply --yes`.

### 7.3 Notifications and status

Notifications should be best-effort desktop notifications (when supported); they must not be the only way to find blocked changes. Local status persists safely across notification failures. Coalesce repeated identical “needs review” states; notify again when changed or after a reasonable reminder interval. Example:

> **Blueprint: Changes need review**\
> 3 changes were found in your shared setup. 2 need your approval, so nothing was applied automatically. Open Blueprint → Sync to review them.

Use the §4.1 status model: the setting (**Off**, **On** with preset, **Paused**, and the **Suspended for safety** guard) and the last-check outcome (**Up to date**, **Applied and verified**, **Needs review**, **Busy**, **Offline**, **Sign-in needed**, **Incomplete**, **Error**). A check timestamp is not a success stamp. Display safe counts and links/commands for review; never unbounded profile details in notifications.

## 8. Onboarding and optional Omarchy plugin (future, not prerequisites)

### 8.1 Profile from URL

The current welcome chooser supports **Create a new profile**, **Open an existing profile**, or **Quit**. Later add **“Get my profile from Git”**. Let the user supply an SSH/HTTPS Git URL and local destination; validate and clone safely to a temporary location; load/validate the profile and commit history; atomically establish the destination only on success; select a machine; start normal first-time Sync review. Don't automatically Restore or enable Auto Sync. Do not persist credentials or reveal token-bearing URL values in errors. Treat cloned profile content as untrusted until normal safety checks pass.

### 8.2 Omarchy plugin

A later optional Omarchy shell/bar plugin shows status, notifications, next check, Pause/Resume, Check now and **Open Blueprint → Sync**. It is a UI client of the same machine-local Auto Sync state/CLI; it neither stores permissions in shared shell state nor has independent execution authority. Uninstalling it must not disable the underlying scheduler, and its presence must never be required for background checks.

## 9. Security, reliability and concurrency requirements

- Protect local grants with the same profile-root isolation, ownership, private state permissions and symlink defence used by the cursor. Forbid transfer of unattended authority by copying the Git profile or selecting a different machine.
- Bind approved permission to exact local machine and remote identity, and make grant changes atomic. Reconfirm after profile root relocation/remote replacement and any grant schema change that cannot be proven non-expansive.
- Use the profile-scoped lock for any mutation **and for any fetch** (non-blocking for scheduled checks; skip as Busy). Only no-fetch status and history reads run without it. Do not invent a second competing lock scheme.
- Gate the entire fresh operation dependency graph; a bundled operation cannot hide an unpermitted side effect.
- Deny background interactive prompts, privilege escalation, service activation requiring review, and unbounded operations. Neither environment variables nor CLI flags may smuggle `--force`, `--exact` or fresh approvals into the auto path.
- Keep source/version credentials outside the state file and run logs. Use bounded/redacted error classes for Git, provider and remote failures.
- Do not let Blueprint capture its own timer/service units as portable user Services. Reinstall/reconfigure them only from local Auto Sync controls.
- Read-only status must not inadvertently fetch; **Check now** may fetch because the UI labels that effect. On a power-off laptop there are no guarantees of continuous checking; do not enable lingering or network wake-up by default.
- Limit run-history retention by both age and bytes; ensure history writes cannot corrupt the cursor or permissions. Status is informative, not mutation authority.
- Changing a preset does not instantly execute; preview and explicit local confirmation first. Pause/disable wins over a queued timer execution. After a partially completed run, require a manual recovery path.

## 10. Testing and independent acceptance

### 10.1 Permission/preset matrix

Test the cross-product of preset × Restore-link mode × effective Restore policy × target/category exceptions × effect-descriptor dimension × “may run code” authorisation × registry entry/version × provider safety × history status, including new targets and newly introduced operation types. Assert monotonic restrictions: any denied input cannot become allowed because another layer grants access. A policy expansion from Git cannot widen locally approved authority. A policy restriction takes effect immediately.

### 10.2 Transaction and failure tests

- Empty/missing/malformed/symlinked/foreign or wrong-profile grant files always fail closed.
- No cursor/no manual baseline, machine switch, branch/remote switch and mismatched identity require manual Sync.
- Dirty profile checkout, diverged/unpublished local history, history conflict, deferred item, invalid historical artifact, control-plane change and profile interpretation differences stop unattended execution.
- Remote change between inspection and apply; grant edit between inspection and apply; revised Restore plan, changed effective policy or new dependency/operation; all stop before unapproved mutation.
- Interactive Git credentials, missing network, Git push/ref race, lock busy, cancellation, shutdown/suspend, provider error, partial Restore, failed verification and cursor-save failure preserve the existing failure contract.
- No fake approvals, no unexpected Capture/commit/push, no Force/Exact bypass, no services activation and no remote self-authorization.
- Provider capability allowlists cover every currently supported operation; newly added operation defaults deny until reviewed.
- Notifications deduplicate and redact; JSON/history never leaks authenticated URLs or secret content.

### 10.3 Systemd and UI tests

Use isolated `XDG_CONFIG_HOME`/`XDG_STATE_HOME`/user-manager fixtures; never install a real timer in the developer's environment. Test unit generation, first enable, pause, resume, disable, upgrade, missing service manager, repeated firings, race with manual Sync, and clean teardown. Test TUI at widths 140/100/80 and smaller terminals, keyboard focus, collapsible lists, meaningful help and no hidden default-action activation.

### 10.4 Acceptance gates

1. All Go tests, race, vet, build, current 11 Sync/two-host tests, and Reconstruction Assurance remain green.
2. A two-host acceptance scenario: complete manual Sync on host A; opt into Auto Follow on host B; publish a permitted change from A manually; B receives/applies it on a scheduled check and verifies it, with no explicit approval prompt; both have correct state and cursors.
3. An unpermitted or conflict change from A results in **no** unattended mutation on B and a clear “Needs review” status.
4. A remote edit that widens Restore permissions cannot extend B's unattended grant.
5. Test a partial failure after remote profile state changes: B does not claim completion and suspends automatic mutation until inspected/repaired.
6. Real-machine physical validation: permissions preview, normal scheduled change, paused mode, no network, login/logout, manual Sync concurrency and a sensitive change requiring review.
7. Independent code review (findings-first severity), CI, AUR checks, Reconstruction Assurance, and explicit merge/release approval remain separate.

## 11. Acceptance criteria expressed as user questions

A first-time user can answer from the TUI alone:

- Is Auto Sync on or off? When will it check next?
- What would it automatically apply, and what would need my permission?
- Does it use my Restore settings? What happens if I change those settings later?
- What is the difference between **Notify only**, **Balanced** and **Hands-off**?
- Will it change my computer, my Git profile, or both?
- Can another computer turn Auto Sync on or grant itself more permission? (**No**.)
- Did the last run merely check, complete a verified Sync, or stop with work pending?
- How do I pause, disable and recover safely?

The interface should never require users to interpret “cursor”, “upstream”, “lineage”, “verified settlement” or an undecoded risk number to answer these questions.

## 12. Alternatives and rationale

**A. Blind timed `git pull && restore` — rejected.** It bypasses target-level reconciliation and safe history/approval boundaries.\
**B. Opt in separately to every target — rejected as default UX.** Difficult to configure and maintain; support per-target exceptions without requiring them.\
**C. Reuse Restore policy as unattended permission — rejected.** A portable profile edit could expand unattended authority on another computer.\
**D. One “anything goes” switch — rejected.** Cannot faithfully represent unknown operations, scoped permission to run code, conflicts and irreversible side effects.\
**D2. Permanently limit automation to inherently inert operations — rejected at Gate 0 review.** Risk and current interactivity identify engineering work and the need for explicit local authorisation, not permanent prohibition.\
**E. Local preset + custom caps + Restore-policy intersection — selected.** Usable defaults while keeping unattended authority local, bounded, auditable and reviewable.\
**F. Automatic partial-plan execution — deferred.** It introduces partial-intent and recovery complexity; v1 stops the whole run if any proposed action needs review.

## 13. Decisions and review gates

### Proposed decisions to approve

- Five presets, with Off as the initial setting and **Balanced** as the suggested opt-in choice; each preset strictly broader than the one below.
- Presets as ceilings over provider effect descriptors and a versioned capability registry, not over risk labels.
- A separate, local, per-category “may run code” authorisation for code-bearing operations.
- Three readiness tiers (Ready, Engineerable, Unsuitable) with a small, reasoned T3 set, and an audited route to automation for every supported category.
- Three Restore linkage modes; **Keep linked** suggested, implemented as restrictions within a locally confirmed ceiling.
- Category-level permissions covering future targets, with operation-type and target exceptions.
- A separate, machine-local, versioned automation grant; profile changes cannot broaden it.
- Auto Follow only in Phase 11, delivered in increments I1–I4; Auto Publish and Auto Reconcile wait for separate review.
- No fake interactive approval; a distinct audited, locked, fresh-authority execution path sharing Sync Engine primitives.
- All-or-nothing eligibility throughout Phase 11, with honest non-atomic execution: partial work after a failure is reported, suspends automation, never advances the completed-Sync record, and rollback is best-effort unless proven transactional; partial execution by design only with a later proof.
- Positive evidence, not non-detection, for Conservative and Balanced; theme activation Hands-off whenever a user-controlled hook runs (Balanced only with proof the whole path is non-executable); default apps Balanced; privileged installs behind Hands-off or the named privileged-install permission, with a separately reviewed mechanism per kind of installation; precise hard denials (unverified execution, unproven destruction).
- T3 (unsuitable forms) kept separate from run-time blockers.
- A review lease and stale-review refresh in addition to the execution lock.
- Locked, non-blocking scheduled fetches; one status model.
- A `systemd --user` timer/service as a replaceable scheduler, not as the Sync engine.

### Implementation-plan decisions (not permission to guess)

Before Phase 11 PR A implementation begins, the architecture/implementation plan must nail down: (i) the per-provider effect descriptors, tiers and increment assignments (audit), (ii) the first-release registry entries per preset, (iii) the machine-local authority record/consent-envelope schema, versioning and escalation diff, (iv) exact safe Auto Follow execution refactor and local fast-forward verification, (v) precise timer semantics/timeouts/login behaviour, (vi) profile-lock and grant-lock ordering, (vii) notification and run-history API, and (viii) PR-level acceptance fixtures and rollback tests. These decisions need separate review **before implementation**, not ad-hoc broadening of this specification.

**Next formal step:** review/approve this design; then write the Phase 11 ADR and detailed implementation plan. No repo, machine, timer or GitHub mutation is authorised by this draft.

## 14. References

- Project `ROADMAP.md`, especially §4.7 and §69 Phase 11.
- `docs/planning/specs/2026-10-03-sync-engine-v1-design.md`, especially §Future Phase 11 constraints.
- `docs/adr/0027-semantic-sync-reconciliation-and-machine-local-cursors.md`.
- Existing `internal/workflow/sync_execute.go`, `internal/workflow/sync.go`, `internal/syncengine/cursor.go`, `internal/syncengine/lock.go` and `internal/profilegit/`.
- systemd `systemd.timer(5)` and `loginctl(1)` documentation for user timers, coalescing, catch-up and linger behavior: https://github.com/systemd/systemd/blob/main/man/systemd.timer.xml and https://github.com/systemd/systemd/blob/main/man/loginctl.xml .
