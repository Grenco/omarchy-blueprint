# Phase 11 — Auto Sync Design Specification

**Project:** Omarchy Blueprint (`Grenco/omarchy-blueprint`)\
**Status:** Approved — ADR 0029 accepted 2026-10-17; implementation proceeds only through the plan's review gates\
**Draft date:** 2026-10-17\
**Reference baseline:** merged `main` `aa6f127e37f00bd38cf0c3d91bee02aacdfe5d88` (PR #74 merge; its tree is the reviewed PR head `db86c11`).\
**Basis:** `ROADMAP.md` §69; `docs/planning/specs/2026-10-03-sync-engine-v1-design.md` (especially §Future Phase 11 constraints); ADR 0027; existing workflow/profilegit/syncengine code.\
**Gate:** Approval of this spec authorizes drafting an ADR and implementation plan only. It does **not** authorize coding, creating timers, enabling unattended work, editing the live profile, modifying GitHub, merging, tagging or releasing.

## 1. Intent and user promise

Auto Sync extends the **existing semantic Sync Engine** so a selected Omarchy computer can periodically check a shared Blueprint profile and, when explicitly authorised, apply changes without requiring a fresh interactive confirmation every time.

**Product promise:** “Blueprint keeps an eye on your shared setup. It can automatically handle only the changes you have allowed on this computer. Anything uncertain waits for your review.”

Auto Sync is not a daemon that blindly runs `git pull`, `capture`, `restore` and `push`. It must reuse Sync's pinned Git history, machine-local last-successful-Sync state, provider-defined target semantics, policy gates, fresh Restore planning, verification, and failure rules. The existing machine remains the configuration editor; Git remains the shared profile history.

A preset provides *proposed automation permission*, not permission to bypass hard safety requirements. All installed automation is opt-in; the initial default is **Disabled**, with read-only Notify only offered as the safest first choice. The suggested active preset is **Balanced**, linked to Restore settings, only after an explicit local preview and confirmation.

### 1.1 What a user should understand

- **Check for changes:** Look at the shared profile and this computer; do not change the live system.
- **Automatically apply:** Apply only explicitly allowed, supported, freshly checked changes from the saved profile to this computer.
- **Automatically share:** Save this computer's changes into the shared profile. This is a separate permission and is **not** included in the initial release.
- **Needs review:** Nothing dangerous has been guessed or silently skipped; open manual Sync to decide.
- **Last completed Sync:** A result verified by Sync Engine, not merely the time of the last background check.

## 2. Scope and release boundary

### 2.1 Phase 11 first release (Auto Follow v1)

1. Local, opt-in Auto Sync settings with presets, custom overrides, and three Restore-link modes.
2. Read-only, scheduled semantic Sync inspections, with an explicit fetch of the configured tracking branch inside that operation only.
3. Machine-local classification of what an inspected **entire plan** would be allowed to execute without interaction.
4. Unattended **Auto Follow** for an eligible remote-only, fast-forward, conflict-free plan; all writes through the existing Sync execution architecture and fresh Restore planning.
5. Local status and bounded, sanitized run history; notifications for pending review, completion, and actionable failure.
6. TUI settings/status/preview and scriptable CLI equivalents; managed `systemd --user` timer/service as scheduling mechanism.
7. Multi-host fake-PTY acceptance, failure-injection tests and real-machine acceptance gates.

### 2.2 Explicitly deferred

- **Auto Publish:** unattended Capture, commits and push. Its permission remains independent; do not expose a functional toggle until implemented and separately approved.
- **Auto Reconcile:** unattended semantic merging of disjoint local/remote committed changes (the roadmap places broader bidirectional auto reconciliation later).
- Automatic selection of conflicting target intent, remembered conflict decisions, persisted live-observation caches, watchers or push subscriptions.
- Unattended first-time Sync setup, cursor reset or trust-on-first-use of new machine/profile/remote identity.
- Executing user-provided scripts, hooks, plugins, service activation/restart, high-risk removals, forced Restore or compatibility overrides without separately reviewed provider support; see §6.
- More than one configured tracking branch, force push, rebase or Git history rewriting.
- Profile cloning from a URL at initial setup and the optional Omarchy shell/bar plugin. Both are compatible **follow-on features**, not prerequisites.

### 2.3 Phase boundary

**PR A:** authority model, presets, immutable eligibility descriptors and tests (no unattended mutation).\
**PR B:** scheduler, inspection, status and notifications (read-only).\
**PR C:** Auto Follow using the existing Sync transaction and independent local authority; exhaustive failure tests.\
**PR D:** TUI/CLI management and two-machine acceptance.\
Auto Publish receives its own reviewed design/PRs after Auto Follow acceptance; do not pre-enable unused publishing settings.

## 3. Design principles and non-negotiable invariants

1. **Explicit local authority.** Portable profile data—including Capture/Restore policies—cannot enable, unpause or expand unattended execution on another computer.
2. **Intersect permissions, never union them.** Every mutation needs (a) existing effective Restore Apply, (b) the local Auto Sync grant, (c) a provider-supported unattended capability, and (d) current plan/Restore/safety/compatibility approval.
3. **Presets are ceilings, not guesses.** No preset authorises an unknown operation or a newly introduced provider operation type.
4. **No interactive approval impersonation.** Do **not** automate the current `--yes` path or supply permissive fake `ApproveRestore`/`ApproveCapture` closures. Add a distinct Auto Sync execution-authority path that verifies the machine-local grant under the same lock and freshly validates operation-level scope.
5. **No first-run automation.** A manual Sync must already have completed successfully and established a valid machine-local base. Remote/branch/machine identity changes require a new manual review.
6. **Whole-plan preflight for v1.** If a proposed change needs review, has a blocker, or exceeds authority, do not partially execute the plan. Report what is ready and what is blocked. This excludes intentional, correctly policy-preserved differences with no proposed operation.
7. **Fail closed.** Unknown safety facts, malformed policy, stale revisions, dirty profile checkout, divergent history, conflicting desired state, untrusted artifacts, missing credentials or interactive requirements stop execution.
8. **Exact history and ordering.** Revalidate pinned B/L/R/M/identity, keep the existing profile lock, update the checkout with the existing safe Git primitives, plan Restore freshly, verify, then advance the cursor only through Sync Engine's established success path.
9. **No hidden profile-management mutation during checks.** Inspections may fetch remote tracking refs as explicitly disclosed; they do not pull, commit, change profile files, apply settings or write the cursor.
10. **No privileged system service.** Run as the logged-in user without root escalation; never enable user lingering silently.
11. **Stop means stop.** Disabling or pausing prevents new automatic application, even if a timer remains scheduled or a profile requests otherwise. Recheck the local grant at the last authoritative boundary.
12. **Human-readable consequences.** The user sees what would change on this computer, why something was withheld, and how to turn automation off, without terms such as B/L/R/M, cursor, lineage or verified settlement.

## 4. User configuration model

### 4.1 Lifecycle and defaults

States: **Disabled** (initial), **Checking only**, **Enabled**, **Paused**, **Needs review**, **Error**. “Needs review” or “Error” are run outcomes/statuses; they do not silently rewrite the user's selected preset. An explicit hard-failure guard may put unattended execution into a locally persisted **Suspended for safety** state until the user reviews the problem. Status distinguishes last *check* from last *completed Sync*.

On first enablement: confirm valid machine/profile/upstream identity, previous manual Sync completion, usable background Git auth, and a dry-run evaluation of the exact current plan. No automatic Restore occurs simply because the user opens the settings screen or selects a preset.

### 4.2 Presets

| Preset | User-facing description | Proposed machine-local ceiling |
|---|---|---|
| **Notify only** | “Check for changes and tell me when something needs attention. Never apply changes.” | Fetch/inspect/status/notify only; no profile checkout, live machine or cursor mutation. |
| **Conservative** | “Automatically handle only the simplest, explicitly supported changes.” | Restore-only, provider-audited unattended-low operations; no removals or side-effectful operations. |
| **Balanced** | “Automatically handle routine, supported changes. Ask before anything significant.” | Restore-only, provider-audited unattended-low and explicitly reviewed unattended-routine operations; no removals, code execution, unknown compatibility or service activation. Recommended when opting in. |
| **Hands-off** | “Handle the widest range of changes Blueprint can safely run without asking.” | Restore-only, locally authorised operations explicitly audited for unattended use, including separately allowed higher-risk operation classes if implemented. All hard safety gates still apply. No blanket Force/Exact/remove/script approval. |
| **Custom** | “Choose which changes may run automatically.” | A local allow/deny matrix of category, target exceptions, operation class and maximum audited risk, always capped by hard safety rules. |

**Implementation detail:** The preset-to-operation mapping is a *versioned capability allowlist*. These names are not a promise that arbitrary providers currently expose safe unattended operations. For each provider, an engineering audit must explicitly assign `unattended_safe`, `unattended_routine`, `unattended_extended` or `requires_human`; unknown/new operation types default to `requires_human`. Do not rely solely on an existing generic RiskLow/RiskHigh label. These flags describe execution effects, not security guarantees.

The initial PR must provide an audited capability matrix for every existing category and each concrete operation type (add/update/remove, executable payload, shell command, service activation, external download, schema migration, Force/Exact effect). **If no mutation passes the documented audit for a preset, disclose that the preset currently checks and notifies; do not silently broaden permission.** The exact initial matrix is an implementation-plan review gate, not an assumption in this spec.

### 4.3 Custom overrides

The interface starts with **categories**, each collapsible into **targets** and operation classes. Users can grant or withhold permission for a category and optionally override specific targets; no need to visit every target. A target deny always beats a category allow. A target allow may expand the selected preset only after explicit local confirmation and never beyond provider/hard-safety limits. A custom change that widens unattended authority requires a new local preview and confirmation. Display category counts and examples rather than dumping all targets.

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

Background invocations must set noninteractive Git/network execution settings, bounded timeouts, cancellation and bounded retries (with backoff; no aggressive polling). Authentication requiring a prompt stops as **Needs attention**, not as a hanging background process. Do not log token-bearing URLs or subprocess stdout/stderr. Reuse remote identity sanitisation. Offline checks never mark the computer up to date.

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
if any operation lacks provider-audited unattended support or fresh safety evidence: needs_review
if any operation needs a prompt/Force/Exact override/unknown compatibility: needs_review
if no actions needed: report checked; mark settled only through existing verified logic
else: execute pinned Auto Follow under lock, reauthorising the fresh full operation set
if execution/verification fails: report incomplete; never advance cursor prematurely
```

Reason codes are machine-readable and mapped to safe, plain-English help; profile contents and credentials are not stored in run history.

### 5.4 Failure semantics

- Network/auth failure: no checkout/application/cursor update; backoff and notify once rather than spam.
- Busy profile lock: skip or retry later, never fight with a manual Sync.
- Changed profile SHA or grant while executing: abort/reinspect; **do not** continue on stale authorisation.
- Policy expansion since local approval: pause the affected automatic action and request local confirmation.
- Any uncertain Restore applicability, provider compatibility, dependency resolution, deferred operation or interactive service activation: stop before automatic mutation.
- Partial Restore failure after checkout/publication: report the exact partial state, leave cursor behind and **suspend further auto-application** until the user inspects/repairs it; never claim success or auto-repeat potentially non-idempotent operations.
- Unexpected error: sanitized reason code and safe remediation; do not present raw Git/provider errors or leak paths by default.
- Ctrl-C/systemd stop: propagate cancellation; release locks and preserve Sync's failure/rollback rules. Disabling automation cannot retroactively undo a mutation already committed.

## 6. Operation audit, risk and content trust

Unattended eligibility must be derived from the **fresh executable Restore plan** as well as target-level semantics. Require provider-specific effect descriptors (or an equivalent auditable registry), describing at minimum: category/target, operation type, file write/removal, package install/remove, executable content, external command or network request, restart/activation, privilege escalation, compatibility, reversibility and user interaction. Use the strongest effect across dependencies and bundled operations; unknown effect → requires human.

Initial category review should explicitly consider:

| Category | Particular hazards to account for before permitting unattended mutation |
|---|---|
| Themes | Activation side effects, locally supplied theme scripts, removals, modified theme contents. |
| Defaults | Whether switching apps can launch actions or change security-sensitive handlers. |
| Config | Files used as executable code, shell startup files, credentials, replacement/removal semantics. |
| Shell & Hooks | Executable shell/script content and event-triggered execution; default to human review unless a narrowly auditable operation is established. |
| Plugins | Third-party code, enablement and external downloads; default to human review. |
| Packages | Install/removal scripts, post-install hooks, AUR execution, repository provenance; do not equate generic low risk with unattended safety. |
| Services | Unit definitions, automatic enabling/activation and code execution; default to human review. |
| Resources | Arbitrary user files, arbitrary Git projects and deletion/overwrite boundaries; default to human review for unsafe/unclassified effects. |

**Critical remote-content rule:** A version-controlled profile can contain scripts, executable configuration, service units, URLs or commands. A remote change must not cause those to execute unattended merely because its target belongs to an enabled category. Presets cannot grant a generic “execute whatever arrived in Git” privilege. If safely applying a type inherently entails executing unreviewed external code, treat it as requiring human review until a specifically bounded trust model exists. Hands-off remains subject to the same prohibition.

Likewise, a command that is marked RiskLow by a provider is not automatically eligible for Auto Sync. Any Auto Sync allowlist change, capability downgrade/upgrade, or broadened meaning requires tests and local-authority versioning.

## 7. User interface and CLI contract

### 7.1 TUI structure and exact primary copy

Add an **Auto Sync** panel reachable from Sync (a dedicated settings view or subtab, to match established TUI navigation). Use collapsible sections: **Status**, **How automatic?**, **Use Restore settings**, **What can run automatically?**, **Needs review**, **Recent activity**. Large lists collapse by default, with blockers visible first. Never expose B/L/R/M, cursor, lineage or Git hash in primary copy; technical details may show them on demand.

**Title:** “Auto Sync”\
**Intro:** “Blueprint can check your shared setup in the background and automatically handle changes you allow on this computer. Anything that needs a decision will wait for you.”\
**Disabled:** “Auto Sync is off. Your computer will not check for or apply shared changes in the background.”\
**Preview heading:** “What Auto Sync would do”\
**Preview line:** “These changes could happen automatically. Everything else will wait for your review.”\
**Linking:** “Use my Restore choices to limit what Auto Sync can change.”\
**Confirmation:** “Turn on Auto Sync with these permissions on this computer?”\
**Important notice:** “Changes downloaded from your shared profile cannot give Blueprint new permission to run automatically.”\
**Help:** “Automatic checking is different from automatic application. You can pause or turn off Auto Sync at any time.”

Required actions: **Preview**, **Enable**, **Pause**, **Resume**, **Check now**, **Review pending changes**, **Change permissions**, **Turn off**, **View recent activity**. Any widening of permission requires a local confirmation showing specific new categories/classes/targets. No quick action may make an unattended change before the authoritative full-plan preview gate. All screens must say whether an action checks only, changes the saved profile, or changes the live machine.

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

Distinguish **Up to date**, **Checked—nothing new**, **Applied and verified**, **Needs review**, **Paused**, **Offline**, **Authentication needed**, **Suspended after incomplete Sync** and **Error**. A check timestamp is not a success stamp. Display safe counts and links/commands for review; never unbounded profile details in notifications.

## 8. Onboarding and optional Omarchy plugin (future, not prerequisites)

### 8.1 Profile from URL

The current welcome chooser supports **Create a new profile**, **Open an existing profile**, or **Quit**. Later add **“Get my profile from Git”**. Let the user supply an SSH/HTTPS Git URL and local destination; validate and clone safely to a temporary location; load/validate the profile and commit history; atomically establish the destination only on success; select a machine; start normal first-time Sync review. Don't automatically Restore or enable Auto Sync. Do not persist credentials or reveal token-bearing URL values in errors. Treat cloned profile content as untrusted until normal safety checks pass.

### 8.2 Omarchy plugin

A later optional Omarchy shell/bar plugin shows status, notifications, next check, Pause/Resume, Check now and **Open Blueprint → Sync**. It is a UI client of the same machine-local Auto Sync state/CLI; it neither stores permissions in shared shell state nor has independent execution authority. Uninstalling it must not disable the underlying scheduler, and its presence must never be required for background checks.

## 9. Security, reliability and concurrency requirements

- Protect local grants with the same profile-root isolation, ownership, private state permissions and symlink defence used by the cursor. Forbid transfer of unattended authority by copying the Git profile or selecting a different machine.
- Bind approved permission to exact local machine and remote identity, and make grant changes atomic. Reconfirm after profile root relocation/remote replacement and any grant schema change that cannot be proven non-expansive.
- Use the profile-scoped lock for any mutation, while allowing read-only checks to coexist according to existing read-cycle protections. Do not invent a second competing lock scheme.
- Gate the entire fresh operation dependency graph; a bundled operation cannot hide an unpermitted side effect.
- Deny background interactive prompts, privilege escalation, service activation requiring review, and unbounded operations. Neither environment variables nor CLI flags may smuggle `--force`, `--exact` or fresh approvals into the auto path.
- Keep source/version credentials outside the state file and run logs. Use bounded/redacted error classes for Git, provider and remote failures.
- Do not let Blueprint capture its own timer/service units as portable user Services. Reinstall/reconfigure them only from local Auto Sync controls.
- Read-only status must not inadvertently fetch; **Check now** may fetch because the UI labels that effect. On a power-off laptop there are no guarantees of continuous checking; do not enable lingering or network wake-up by default.
- Limit run-history retention by both age and bytes; ensure history writes cannot corrupt the cursor or permissions. Status is informative, not mutation authority.
- Changing a preset does not instantly execute; preview and explicit local confirmation first. Pause/disable wins over a queued timer execution. After a partially completed run, require a manual recovery path.

## 10. Testing and independent acceptance

### 10.1 Permission/preset matrix

Test the cross-product of preset × Restore-link mode × effective Restore policy × target/category exceptions × operation class × requested risk × provider safety × history status, including new targets and newly introduced operation types. Assert monotonic restrictions: any denied input cannot become allowed because another layer grants access. A policy expansion from Git cannot widen locally approved authority. A policy restriction takes effect immediately.

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
**D. One “anything goes” switch — rejected.** Cannot faithfully represent unknown operations, executable remote content, conflicts and irreversible side effects.\
**E. Local preset + custom caps + Restore-policy intersection — selected.** Usable defaults while keeping unattended authority local, bounded, auditable and reviewable.\
**F. Automatic partial-plan execution — deferred.** It introduces partial-intent and recovery complexity; v1 stops the whole run if any proposed action needs review.

## 13. Decisions and review gates

### Proposed decisions to approve

- Five presets, with Disabled as the initial state and **Balanced** as the suggested opt-in choice.
- Three Restore linkage modes; **Keep linked** suggested, implemented as restrictions within a locally confirmed ceiling.
- Category-level defaults with collapsible per-target and operation overrides.
- A separate, machine-local, versioned automation grant; profile changes cannot broaden it.
- Phase 11 initially implements scheduled checks and **Auto Follow** only; Auto Publish and Auto Reconcile wait for separate review.
- No fake interactive approval; a distinct audited, locked, fresh-authority execution path sharing Sync Engine primitives.
- Whole-plan eligibility in the initial release; unknown, unsafe, interactive or partially blocked plans stop for manual review.
- A `systemd --user` timer/service as a replaceable scheduler, not as the Sync engine.
- Readable status, notifications, inspection preview, and real-machine acceptance before release.

### Implementation-plan decisions (not permission to guess)

Before PR A implementation begins, the architecture/implementation plan must nail down: (i) concrete per-provider operation audit and the exact initial Conservative/Balanced/Hands-off allowlists, (ii) whether a trusted low-risk operation is available for each preset, (iii) the machine-local authority record/consent-envelope schema, versioning and escalation diff, (iv) exact safe Auto Follow execution refactor and local fast-forward verification, (v) precise timer semantics/timeouts/login behaviour, (vi) profile-lock and grant-lock ordering, (vii) notification and run-history API, and (viii) PR-level acceptance fixtures and rollback tests. These decisions need separate review **before implementation**, not ad-hoc broadening of this specification.

**Next formal step:** review/approve this design; then write the Phase 11 ADR and detailed implementation plan. No repo, machine, timer or GitHub mutation is authorised by this draft.

## 14. References

- Project `ROADMAP.md`, especially §4.7 and §69 Phase 11.
- `docs/planning/specs/2026-10-03-sync-engine-v1-design.md`, especially §Future Phase 11 constraints.
- `docs/adr/0027-semantic-sync-reconciliation-and-machine-local-cursors.md`.
- Existing `internal/workflow/sync_execute.go`, `internal/workflow/sync.go`, `internal/syncengine/cursor.go`, `internal/syncengine/lock.go` and `internal/profilegit/`.
- systemd `systemd.timer(5)` and `loginctl(1)` documentation for user timers, coalescing, catch-up and linger behavior: https://github.com/systemd/systemd/blob/main/man/systemd.timer.xml and https://github.com/systemd/systemd/blob/main/man/loginctl.xml .
