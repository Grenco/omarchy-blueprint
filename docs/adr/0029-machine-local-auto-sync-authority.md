# ADR 0029: Machine-Local Authority for Auto Sync

**Status:** Accepted\
**Date:** 2026-10-17 (accepted 2026-10-17)\
**Roadmap:** Phase 11 — Auto Sync\
**Design:** [`docs/planning/specs/2026-10-17-auto-sync-phase-11-design.md`](../planning/specs/2026-10-17-auto-sync-phase-11-design.md)\
**Predecessor:** [ADR 0027 — Semantic Sync Reconciliation and Machine-Local Cursors](0027-semantic-sync-reconciliation-and-machine-local-cursors.md)\

> Accepting this ADR does not by itself authorize unattended mutation code, systemd unit installation, merging, release, or operation on a real profile; those remain behind the review gates in the implementation plan (Gate 0 capability audit first). Acceptance review notes are recorded in the implementation plan §10.

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

The initial state is **Disabled**. Enabling or expanding unattended authority requires a local, interactive preview and confirmation. A downloaded profile, a configuration migration, a command-line `--yes`, or a copied file cannot by themselves enable or broaden a grant. An invalid, unsupported, identity-mismatched, substituted or unreadable grant fails closed. Grant revisions must be checked again under the execution lock and at the final pre-apply authority boundary.

A machine must first complete a valid **manual** Sync. Auto Sync must not perform first-time bootstrap, reset a missing or invalid cursor, infer a new base, or silently adopt a changed machine/profile/remote identity.

### 3. Presets are local ceilings; custom exceptions narrow or deliberately extend them

The user sees five presets:

| Preset | Authority ceiling |
|---|---|
| Notify only | Fetch, inspect, report, notify. No checkout, Restore or cursor write. |
| Conservative | Restore-only operations explicitly audited as `unattended_safe`. No removal or execution-capable side effects. |
| Balanced | `unattended_safe` and explicitly audited `unattended_routine` operations. No removal, unreviewed execution, unknown compatibility or service activation. Recommended opt-in configuration, not enabled by default. |
| Hands-off | Widest *audited* locally granted Restore-only effects, including `unattended_extended` where separately supported. Does not override hard safeguards. |
| Custom | Category, target and operation-family allow/deny rules, always limited by hard safeguards and audited capability support. |

Presets and exceptions are **upper bounds**, not a guarantee that any given provider operation is safe. Unknown/new operations default to `requires_human`. Target deny wins over category allow. Expanding a preset, target override or capability interpretation requires a fresh local authority review. The implementation plan must obtain explicit review of the initial operation allowlist before unattended mutation code is authorized.

### 4. Restore linkage constrains Auto Sync; it never manufactures authority

Users can choose **Keep linked**, **Copy once**, or **Keep separate**:

- **Keep linked (recommended):** current effective Restore rules continuously restrict Auto Sync. The local confirmation records a bounded authority ceiling, including any expressly approved category-wide future-target scope. Tightening Restore rules takes effect immediately. Loosening rules may only authorize an operation if it was *already* within that locally confirmed ceiling; otherwise local reconfirmation is required.
- **Copy once:** initialize a local Auto Sync scope from current effective Restore rules and then manage that scope separately. Later Restore edits never update the local Auto Sync grant automatically.
- **Keep separate:** use independently configured local Auto Sync scope; effective Restore Skip still always blocks actual Restore.

In **all** modes, execution requires the intersection of the current Restore Apply/Skip decision, local automation grant, provider-audited unattended capability, and fresh Restore applicability/safety checks. A remote change to Restore policy that *might* expand authority but cannot be proven within the confirmed local ceiling requires review. Portable policy does not grant remote code execution, even under Hands-off.

### 5. Whole-plan authorization, no partial unattended transactions in v1

A run with **any** unresolved conflict, deferred decision, missing artifact, invalid control/history state, local Capture/Publish/merge requirement, unsupported operation, uncertain compatibility, interactive prompt, or unauthorized effect is not auto-executable. It reports **Needs review** without beginning unattended mutation. Correctly preserved differences with no proposed action do not themselves block.

Auto authorization is computed against the **entire fresh executable operation/dependency graph**, not just summary target keys, labels, `RiskLow`/`RiskHigh`, or a previous inspection. Provider-owned or reviewed capability descriptors must account for scripts/executable content, network, command execution, package install/removal, service activation, file writes/removal, overwrites, reversibility, interactivity and dependencies. Bundled operations take the strongest effect; unknown effects require a human.

The unattended path must not supply fake `ApproveRestore`/`ApproveCapture` callbacks to `ExecuteSync`, and must not use `sync apply --yes` as an approval substitute. It must use a distinct authority path with the existing lock, pinned revision revalidation, fresh operation planning, execution and verification. Stop if the operation graph or grant changes between eligibility preview and execution.

### 6. Follow-only history handling is deliberate

Phase 11 Auto Follow may use the existing **exact pinned fast-forward** primitive and reachability/identity checks, but it must **not push** to the shared repository in a follow-only run. Manual Sync currently calls `PublishSyncRevision` even when it has no Capture work; the Auto Follow implementation must explicitly separate *verify an already-published remote commit* from *publish new local intent*. A timer may not accidentally turn a follow into a write to remote history.

No automatic rebase, force push, generic `git pull`, local profile commit or semantic merge is permitted. Cursor advancement occurs only through the existing verified-settlement contract; a read-only check never moves it. If profile checkout has advanced but Restore fails, preserve truthful partial-state reporting and leave the cursor unchanged.

### 7. Scheduling is replaceable and not a source of authority

Use an opt-in `systemd --user` oneshot service and timer as the scheduling mechanism, not a long-running daemon or independent scheduler engine. The Go command owns Sync decisions. Default checking interval is approximately 30 minutes while the user manager runs, with bounded configuration and jitter; no root unit, unrequested user lingering, wake-from-suspend, or uncontrolled retry loops. A user-facing **Check now** is an explicit fetch/inspect; **Status** is read-only.

Running jobs must be noninteractive, timeout/cancellation-bound and safe when credentials or network are unavailable. A busy local profile lock produces a skipped/deferred check, not a second mutation. Disabling or pausing prevents queued and newly starting automatic mutations; an operation already committed cannot be retroactively undone.

### 8. Results and recovery remain truthful

Machine-local, bounded and sanitized status/history distinguishes last check from last **verified completed Sync**. Notifications are best effort and deduplicated; they are not the only visible record of a problem. A partial Restore, failed verification, uncertain repository outcome, or unsafe recovery state suspends future automatic application until a person inspects/repairs it. No cursor advancement, silent retry of side-effectful operations or false “up to date” status is allowed.

Auto Sync uses plain-English TUI/CLI copy and previews showing counts of automatic versus review-only changes and what can change on this computer. Technical identities and reason codes remain available in an advanced section and structured CLI JSON without credentials, secret values or raw provider output.

## Consequences

**Benefits:** Usable preset-based opt-in; predictable integration with Restore Skip; remote profile cannot expand unattended authority; unattended execution reuses the audited Sync domain; checking remains useful even when no operation is yet safe to apply; a safe notification/recovery story.

**Costs:** Extra machine-local grant, capability-audit and versioning burden; a distinct Auto Follow execution-authority path; operation-graph preflight; special care around grant changes and profile locking; many proposed changes correctly remain review-only, even in Hands-off. Whole-plan refusal can delay otherwise eligible work until a user resolves an unrelated blocker.

**Compatibility:** Existing Sync and Restore interactions are unchanged by default. No profile format bump is required for the local grant. If another app or version lacks the grant, it must not infer or recreate unattended permission. A copied grant is not portable authority: a changed local host/installation identity requires reconfirmation, even when paths, machine labels and remote match. Ordinary Git profile changes may continue across revisions, but cannot widen a previously confirmed grant silently.

## Rejected or deferred alternatives

1. **Timer around `git pull && restore`, or `capture && push`:** loses semantic provenance and safety checks.
2. **Reuse Restore Apply as automatic approval:** lets remote profile edits manufacture unattended authority.
3. **Auto-confirm manual Sync callbacks:** uses stale approval for an unknown future operation set.
4. **Require every target to be opted in:** impractical for real profiles; allow category grants and target exceptions.
5. **One unbounded “anything goes” switch:** cannot safely express unreviewed scripts, executable remote content, service activation or unknown effects.
6. **Automatically execute the permitted subset of a blocked plan:** deferred until partial-settlement semantics and recovery can be explicitly designed.
7. **A persistent daemon and always-on root service:** unnecessary; `systemd --user` scheduling is sufficient.
8. **Auto Publish/Auto Reconcile now:** separately gated because outgoing Git writes and automated conflict outcomes require their own authority and failure review.

## Validation and decision gates

- A reviewed operation capability matrix is a hard **pre-implementation gate**. No broadening a permission preset because a test fixture happens to pass.
- Tests must prove local grant isolation, monotonic permissions, stale-plan/grant refusal, remote policy changes, unknown operation denial, exact fast-forward without push, zero fake approvals, full graph checks, failure/suspension and cursor immobility on incomplete runs.
- PR A (permissions + audit), PR B (read-only scheduling), PR C (Auto Follow) and PR D (TUI/CLI and acceptance) have separate reviews and hosted checks.
- Two-host acceptance, physical-machine acceptance, reconstruction assurance and an explicit human merge/release decision remain required.

## References

- `ROADMAP.md`, §4.7 and §69 (Phase 11 Auto sync).
- `docs/planning/specs/2026-10-17-auto-sync-phase-11-design.md`.
- ADR 0027 and `docs/planning/specs/2026-10-03-sync-engine-v1-design.md` §Future Phase 11 constraints.
- `internal/workflow/sync.go`, `internal/workflow/sync_execute.go`, `internal/syncengine/{cursor,lock}.go`, and `internal/profilegit/`.
