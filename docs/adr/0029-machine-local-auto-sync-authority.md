# ADR 0029 (Proposed, revision 3): Machine-Local Authority for Auto Sync

**Status:** Proposed — revision 3 for final review. Revision 1 was accepted on 2026-10-17; the Gate 0 review then changed its capability model, so this revision returns to Proposed until reviewed.\
**Date:** 2026-10-17\
**Roadmap:** Phase 11 — Auto Sync\
**Design:** [`docs/planning/specs/2026-10-17-auto-sync-phase-11-design.md`](../planning/specs/2026-10-17-auto-sync-phase-11-design.md)\
**Predecessor:** [ADR 0027 — Semantic Sync Reconciliation and Machine-Local Cursors](0027-semantic-sync-reconciliation-and-machine-local-cursors.md)\
**Capability audit:** [`docs/planning/audits/2026-10-17-auto-sync-restore-capabilities.md`](../planning/audits/2026-10-17-auto-sync-restore-capabilities.md)

> Accepting this ADR does not by itself authorize unattended mutation code, systemd unit installation, merging, release, or operation on a real profile; those remain behind the review gates in the implementation plan. Revision 2 (Gate 0 review) replaced the "deny almost everything" capability position with a three-tier, effect-based model and a staged route to automating every supported category. Revision 3 applies the final design corrections: positive evidence for Conservative/Balanced, theme activation in Hands-off by default, default apps in Balanced, privileged installs behind Hands-off or a named privileged-install permission, T3 separated from run-time blockers, non-atomic execution stated honestly, and manual-review coordination.

## Context

Sync Engine v1 compares the last successfully completed Sync (B), committed local profile (L), fetched shared profile (R), and actual machine state (M) using provider-owned semantic targets. Manual execution revalidates pinned revisions under a profile-scoped lock, re-plans and requests fresh Capture/Restore approvals, publishes before applying remote intent, verifies results, and only then advances the machine-local cursor.

Repeating `git pull && restore` on a timer would bypass these guarantees. Conversely, simply running manual `sync apply --yes` would pretend that an earlier interaction approved a *different, newly downloaded* operation set. Remote profile content can include executable files, service definitions, package names, commands, policy changes and other potentially consequential input. A shared profile must not be able to authorize its own unattended application.

Users also should not need to opt into every single configuration target. Phase 11 therefore needs presets, category defaults, target exceptions and optional linking to existing Restore choices, without confusing permission to Restore interactively with permission to execute automatically.

## Decision

### 1. Auto Sync is a consumer of Sync Engine, not a second reconciler

Auto Sync will use the existing Sync inspection, pinned semantic plans, provider projection, historical artifact checks, profile-Git primitives, Restore planning/verification, profile lock and cursor invariants. It must not implement raw filesystem mirroring or a second Git-based conflict algorithm.

The initial automated mutation capability is **Auto Follow**: applying a qualifying remote-only, fast-forward change on the selected computer. Automatic Capture/commit/push (**Auto Publish**) and automatic semantic merge (**Auto Reconcile**) require later, separately reviewed designs. The initial implementation may fetch and inspect, but it must not automatically publish a revision or resolve a conflict.

### 2. Unattended permission is independently granted and stored locally

An Auto Sync grant is a separate, versioned, machine-local authority record bound to a canonical profile root, locally verified host/installation identity, selected machine, branch, tracking upstream, sanitized remote identity, approved permission scope and audited capability version. It is written with private permissions and atomic replacement in the existing profile-keyed local state directory; it is **never** stored in the portable Git profile, captured machine overlay, shell/plugin configuration or cursor.

The initial setting is **Off**. Enabling or expanding unattended authority requires a local, interactive preview and confirmation. A downloaded profile, a configuration migration, a command-line `--yes`, or a copied file cannot by themselves enable or broaden a grant. An invalid, unsupported, identity-mismatched, substituted or unreadable grant fails closed. Grant revisions must be checked again under the execution lock and at the final pre-apply authority boundary.

A machine must first complete a valid **manual** Sync. Auto Sync must not perform first-time bootstrap, reset a missing or invalid cursor, infer a new base, or silently adopt a changed machine/profile/remote identity.

### 3. Presets are local ceilings over an effect model

Unattended authority is decided per operation from a provider-supplied **effect descriptor**, never from operation IDs or the existing `RiskLow`/`RiskHigh` labels (which describe interactive Restore risk). A descriptor records: change (`add`, `update`, `remove`), payload (`data`, `config`, `exec-later`, `exec-now`), source trust (`artifact` verified by recorded hash, exact `pinned` revision or checksum, `signed` repository, `closed-set` value, `unpinned`), activation (`none`, `reload`, `activate`), privilege (`user`, administrator-approved `root-helper`), interaction, recovery and post-apply verification. A versioned **capability registry** lists which descriptor shapes each provider can execute unattended and under which conditions. Unknown descriptors, unregistered operations and capability-version mismatches require review.

Every operation is classified into one of three tiers:

- **T1 Ready:** existing verified mechanisms already enforce the required controls.
- **T2 Engineerable:** suitable after named engineering work, safeguards and explicit local authorisation. Requiring interactive approval today, or being able to execute code, places an operation here, not in T3.
- **T3 Unsuitable:** cannot legitimately run unattended (interaction that needs a person, privilege via passwords or answered prompts, arbitrary privileged commands, unpinned or unverifiable sources, operations whose steps, relevant dependencies or effects Blueprint cannot bound or verify with a supported mechanism, conflict resolution, removal without provenance, authority expansion from portable policy). This set is kept small and each entry carries its reason.

**Run-time blockers** are different: an invalid or missing completed-Sync record, stale reviewed revisions, changed local permissions, dirty or incompatible profile history, unsupported compatibility, missing artifacts, ambiguous target ownership, unavailable credentials, uncertain outcome of a previous run, or verification that cannot complete. Each stops the current run before unattended mutation; none makes an operation or category unsuitable.

The user sees five presets. Each is a ceiling over descriptors; what runs is the intersection of the ceiling, delivered registry entries, the local grant's scope, current Restore Apply and fresh safety checks.

| Preset | Authority ceiling |
|---|---|
| Notify only | Fetch, inspect, report, notify. No checkout, Restore or cursor write. |
| Conservative | The narrowest verified operations: **adding** data or ordinary configuration from hash-verified profile artifacts, with positive evidence that nothing loads or runs it as a result. No update, replacement, removal, reload, activation or code. |
| Balanced | Conservative plus routine changes: updating ordinary configuration and fixed-vocabulary settings (such as default apps) with an expected-state precondition and backup, and reload or activation **only where positive evidence establishes that the complete execution path runs no user-controlled code**. |
| Hands-off | Balanced plus supported higher-risk operations: content that runs code now or later from hash-verified or exactly pinned sources, activation that runs user-controlled code (for example theme activation, which runs the `theme-set` hook), enabling/starting/loading, privileged package installation (with the privileged-install authorisation, §3a), and provenance-checked removals with recovery. Only within categories the user has separately authorised to run code (§3a). Hands-off is always strictly broader than Balanced; where its extra operations are not yet delivered, the UI says which are pending rather than presenting it as equal to Balanced. |
| Custom | Category × operation-family permissions with optional individual-target exceptions, each capped by the Hands-off ceiling and the registry; privileged install is its own named permission. Target deny beats category allow. |

**Positive evidence.** Content analysis identifies potentially executable content and selects the permission an operation needs; it is not a security boundary. Conservative and Balanced eligibility requires positive evidence: a supported format and loader, complete parsing including transitive includes, documented effects of loading and of any reload or activation command (for example, `hyprctl reload` re-runs every `exec` directive in the whole sourced graph), verified preconditions, a safe destination, and verified post-apply behaviour. Non-detection of executable content is never proof of safety; unknown or ambiguous content goes to review or to a supported Hands-off path.

No preset, Custom rule or grant overrides the hard safeguards: integrity (hash, revision or signature verified), compatibility, conflict (never Force a differing target), provenance for removal, credentials and privilege (no stored passwords, no answered prompts, root only through an administrator-installed helper), interaction, post-apply verification, identity binding, plan equality and the profile lock. Unknown/new operations default to review. Expanding a preset, target override or capability interpretation requires a fresh local review.

A category-level grant covers eligible **future** targets and changes within that category and operation scope; users are not required to approve individual targets.

### 3a. Permission to run code is a separate, deliberate local choice

Operations whose payload is `exec-later` or `exec-now`, or whose activation enables or loads such content, require an additional **"may run code" authorisation** per category (and optionally per operation family). It is:

- granted only locally, through an interactive preview that names the category, explains what kind of code could run and from which sources, and states that changes downloaded later are covered;
- scoped to the chosen categories and operation types, and independent of Capture and Restore permissions;
- bound to the grant's profile, host, machine, branch, upstream and remote identity;
- revocable immediately (pause, disable, or removing the category), and re-validated under the lock on every run.

**Privileged installation** (package installs whose scripts and package-manager hooks run as root) needs, in addition, a named **privileged-install** authorisation, available in Hands-off and Custom only, and an administrator-installed root helper defined by its own ADR. The helper accepts only exact package-install requests from allowlisted signed repositories with provenance checks, runs noninteractively, and never executes arbitrary privileged commands. A repository signature establishes origin, not that root-run scripts are harmless.

Downloading a profile or changing portable policy never creates or widens these authorisations. It never stores passwords, answers interactive prompts, bypasses privilege requirements or forces operations blocked for safety; an operation needing credentials or interaction it cannot legitimately obtain stops for review.

### 4. Restore linkage constrains Auto Sync; it never manufactures authority

Users can choose **Keep linked**, **Copy once**, or **Keep separate**:

- **Keep linked (recommended):** current effective Restore rules continuously restrict Auto Sync. The local confirmation records a bounded authority ceiling, including any expressly approved category-wide future-target scope. Tightening Restore rules takes effect immediately. Loosening rules may only authorize an operation if it was *already* within that locally confirmed ceiling; otherwise local reconfirmation is required.
- **Copy once:** initialize a local Auto Sync scope from current effective Restore rules and then manage that scope separately. Later Restore edits never update the local Auto Sync grant automatically.
- **Keep separate:** use independently configured local Auto Sync scope; effective Restore Skip still always blocks actual Restore.

In **all** modes, execution requires the intersection of the current Restore Apply/Skip decision, local automation grant, provider-audited unattended capability, and fresh Restore applicability/safety checks. A remote change to Restore policy that *might* expand authority but cannot be proven within the confirmed local ceiling requires review. Portable policy never grants permission to run code; only the local §3a authorisation can, and portable Exact, Force or activation settings never widen unattended authority.

### 5. Whole-plan authorization, no partial unattended transactions in v1

A run with **any** unresolved conflict, deferred decision, missing artifact, invalid control/history state, local Capture/Publish/merge requirement, unsupported operation, uncertain compatibility, interactive prompt, or unauthorized effect is not auto-executable. It reports **Needs review** without beginning unattended mutation. Correctly preserved differences with no proposed action do not themselves block.

Auto authorization is computed against the **entire fresh executable operation/dependency graph**, not just summary target keys, labels, `RiskLow`/`RiskHigh`, or a previous inspection. Bundled operations take the strongest effect across their dependencies; unknown effects require a human.

The unattended path must not supply fake `ApproveRestore`/`ApproveCapture` callbacks to `ExecuteSync`, and must not use `sync apply --yes` as an approval substitute. It must use a distinct authority path with the existing lock, pinned revision revalidation, fresh operation planning, execution and verification. Stop if the operation graph or grant changes between eligibility preview and execution.

**All-or-nothing eligibility, not atomic execution.** Every change in the proposed plan must be eligible before any unattended mutation begins; an authorised subset is never executed while another required change is unresolved. Execution itself is **not** atomic across Git, files, external commands and machine state: a failure after mutation can leave partial work. In that case the run reports exactly what completed, failed or was not attempted, sets the Suspended-for-safety guard, never advances the completed-Sync record, never reports "up to date", and never automatically repeats a non-idempotent operation. Rollback from journalled backups is best-effort, and is only described as a guarantee for an operation with a proven transactional mechanism. Recovery is a manual Sync review, which re-plans from the current state; a retry re-runs only operations whose idempotence is established.

Partial execution by design (running eligible actions while others wait) is **not** part of Phase 11. It may be proposed later only with a proof that the partial state is recorded truthfully, cannot repeat destructive or non-idempotent operations, never advances the completed-Sync record, and never reports "up to date".

### 6. Follow-only history handling is deliberate

Phase 11 Auto Follow may use the existing **exact pinned fast-forward** primitive and reachability/identity checks, but it must **not push** to the shared repository in a follow-only run. Manual Sync currently calls `PublishSyncRevision` even when it has no Capture work; the Auto Follow implementation must explicitly separate *verify an already-published remote commit* from *publish new local intent*. A timer may not accidentally turn a follow into a write to remote history.

No automatic rebase, force push, generic `git pull`, local profile commit or semantic merge is permitted. Cursor advancement occurs only through the existing verified-settlement contract; a read-only check never moves it. If profile checkout has advanced but Restore fails, preserve truthful partial-state reporting and leave the cursor unchanged.

### 7. Scheduling is replaceable and not a source of authority

Use an opt-in `systemd --user` oneshot service and timer as the scheduling mechanism, not a long-running daemon or independent scheduler engine. The Go command owns Sync decisions. Default checking interval is approximately 30 minutes while the user manager runs, with bounded configuration and jitter; no root unit, unrequested user lingering, wake-from-suspend, or uncontrolled retry loops. A user-facing **Check now** is an explicit fetch/inspect; **Status** is read-only.

A fetch updates Git refs, so scheduled checks and previews take the existing profile lock **non-blocking** around fetch and inspection and record a skipped "busy" check when it is held; they never fight a manual Sync. Only no-fetch status and history reads run without the lock.

The lock covers execution, not a person reading a review. A manual Sync review in the TUI holds no lock while the user decides, so a scheduled fetch could otherwise make it stale. Two mechanisms address this: an open manual review holds a bounded, heartbeated **review lease** in local state, and scheduled checks skip fetching while a live lease exists (the lease expires on its own if the TUI exits uncleanly); and the review still detects a moved remote revision (a fetch by Git, another tool, or a check after the lease lapsed) before confirmation, explains in plain language that new shared changes arrived, keeps decisions that still apply, and offers a refresh, rather than failing at execution.

Running jobs must be noninteractive, timeout/cancellation-bound and safe when credentials or network are unavailable. Disabling or pausing prevents queued and newly starting automatic mutations; an operation already committed cannot be retroactively undone.

### 8. Results and recovery remain truthful

One status model (spec §4.1) separates the user's **setting** (Off, On with a preset, Paused, plus a persisted Suspended-for-safety guard) from the **outcome of the last check** (Up to date, Applied and verified, Needs review, Busy, Offline, Sign-in needed, Incomplete, Error). Machine-local, bounded and sanitized status/history distinguishes last check from last **verified completed Sync**. Notifications are best effort and deduplicated; they are not the only visible record of a problem. A partial Restore, failed verification, uncertain repository outcome, or unsafe recovery state suspends future automatic application until a person inspects/repairs it. No cursor advancement, silent retry of side-effectful operations or false “up to date” status is allowed.

Auto Sync uses plain-English TUI/CLI copy and previews showing counts of automatic versus review-only changes and what can change on this computer. Technical identities and reason codes remain available in an advanced section and structured CLI JSON without credentials, secret values or raw provider output.

## Consequences

**Benefits:** Usable preset-based opt-in that grows to meaningful automation in every supported category; predictable integration with Restore Skip; remote profile cannot expand unattended authority; a deliberate, scoped way to allow code-running operations; unattended execution reuses the audited Sync domain; checking is useful from the first release.

**Costs:** Effect descriptors and a versioned registry for every provider; provider engineering per category (content classification, clone-at-revision installs, provenance records, journal rollback, an administrator-approved root helper under its own ADR); a distinct Auto Follow execution-authority path; operation-graph preflight and snapshot-planning equivalence; grant and lock coordination. Whole-plan refusal can delay otherwise eligible work until a user resolves an unrelated blocker.

**Compatibility:** Existing Sync and Restore interactions are unchanged by default. No profile format bump is required for the local grant. If another app or version lacks the grant, it must not infer or recreate unattended permission. A copied grant is not portable authority: a changed local host/installation identity requires reconfirmation. A registry upgrade that changes an operation's meaning cannot inherit a broader grant.

## Rejected or deferred alternatives

1. **Timer around `git pull && restore`, or `capture && push`:** loses semantic provenance and safety checks.
2. **Reuse Restore Apply as automatic approval:** lets remote profile edits manufacture unattended authority.
3. **Auto-confirm manual Sync callbacks:** uses stale approval for an unknown future operation set.
4. **Require every target to be opted in:** impractical for real profiles; allow category grants and target exceptions.
5. **One unbounded “anything goes” switch:** cannot express scoped, informed permission for code-running operations, unknown effects or hard safeguards; replaced by Hands-off/Custom with per-category “may run code” authorisation.
6. **Automatically execute the permitted subset of a blocked plan:** deferred until partial-settlement semantics and recovery can be explicitly designed and proved (§5).
9. **Permanently limit automation to a few inherently inert operations:** rejected at Gate 0 review. Risk and current interactivity identify engineering work and the need for explicit local authorisation, not permanent prohibition.
7. **A persistent daemon and always-on root service:** unnecessary; `systemd --user` scheduling is sufficient.
8. **Auto Publish/Auto Reconcile now:** separately gated because outgoing Git writes and automated conflict outcomes require their own authority and failure review.

## Validation and decision gates

- The capability audit (provider matrix, tiers, preset eligibility, increments) is a hard **pre-implementation gate**, reviewed together with this ADR, the spec and the plan.
- Each delivery increment adds registry entries only with their engineering work, controls and tests; no entry is broadened because a fixture passes.
- Tests must prove local grant isolation, the separate "may run code" authorisation, monotonic permissions, stale-plan/grant refusal, remote policy changes (including Exact/Force), unknown operation denial, exact fast-forward without push, zero fake approvals, full graph checks, failure/suspension and cursor immobility on incomplete runs.
- Phase 11 PR A–G have separate reviews and hosted checks; the first release follows PR E, with later increments released separately.
- Two-host acceptance, physical-machine acceptance, Reconstruction Assurance and an explicit human merge/release decision remain required.

## References

- `ROADMAP.md`, §4.7 and §69 (Phase 11 Auto sync).
- `docs/planning/specs/2026-10-17-auto-sync-phase-11-design.md`.
- ADR 0027 and `docs/planning/specs/2026-10-03-sync-engine-v1-design.md` §Future Phase 11 constraints.
- `internal/workflow/sync.go`, `internal/workflow/sync_execute.go`, `internal/syncengine/{cursor,lock}.go`, and `internal/profilegit/`.
