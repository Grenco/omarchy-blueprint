# Phase 11 — Auto Sync Implementation Plan

> **Agent execution instruction:** Follow this plan task by task using red→green tests and isolated fixtures. Do not merge, start the next PR, enable a real timer, modify a user's profile, tag or release without the corresponding human gate. **This is a planning artifact, not implementation authorization.**

**Status:** ADR accepted; Gate 0 (capability audit) in progress. No PR A code before Gate 0 approval.\
**Date:** 2026-10-17\
**Design:** `docs/planning/specs/2026-10-17-auto-sync-phase-11-design.md`\
**ADR:** `docs/adr/0029-machine-local-auto-sync-authority.md` (Accepted 2026-10-17)\
**Baseline:** merged `main` `aa6f127e37f00bd38cf0c3d91bee02aacdfe5d88` (PR #74 merge of reviewed head `db86c11`). Recorded as the Phase 11 start SHA on 2026-10-17.\
**Objective:** Deliver opt-in, machine-local, preset-driven scheduled checks and safe Auto Follow through the **existing** semantic Sync Engine, with a separately audited unattended authority, no automatic Capture/push, clear status and a recoverable two-host workflow.

## 0. Review gates and non-negotiable constraints

1. This plan is **not** an instruction to start coding. Seek approval of the design, ADR, this plan and the **concrete capability audit** separately. PR D of Phase 10 must be merged and real-world assumptions checked before integrating Phase 11 onto `main`.
2. **Never** implement Auto Sync as `git pull && restore`, a shell loop, manual `sync apply --yes`, or dummy always-approve callbacks. Manual Sync remains interactive and unchanged by default.
3. The background path cannot create/extend its own local grant; profile-supplied Capture/Restore settings and other control-plane data do not confer unattended permission.
4. **V1 runs whole plans, or nothing.** Any review decision, deferred item, unknown effect, missing authority, invalid snapshot, unsafe or interactive action prevents unattended mutation. Correctly policy-preserved items with no proposed action do not count as a block.
5. **V1 Auto Follow only.** No automatic Capture, local commit, push, semantic merge, force push, rebase, reset, first-time Sync bootstrap, cursor reset, service activation or unreviewed executable payload. The follow-only Git path must not call `PublishSyncRevision` as an outgoing push.
6. Enforce pinned SHA, clean index/worktree, profile root and machine identity, remote identity, Restore Apply/Skip, provider safety, fresh operation graph equality, lock and verified settlement/cursor constraints.
7. Unknown category, target source, provider operation, provider effect or capability version means **requires human**. Generic `RiskLow` and `Reversible` do not imply unattended safety.
8. Systemd only schedules. No root service, `sudo`, implicit lingering, hidden desktop prompt, hardcoded credentials, extra permanent daemon, or surprise capture of Blueprint-owned systemd units.
9. Status and `?` help use plain English. B/L/R/M, hashes and transport details belong in Advanced/JSON where safe. UI never mistakes a *check* for a *completed Sync*.
10. No unrequested dependencies; prefer current Go, Cobra, Bubble Tea, `systemctl --user`, OS timers and existing repo utilities. Dependency changes are an independent review decision.
11. Preserve Reconstruction Assurance's same-runtime scope. Keep Phase 11 acceptance separate; do not alter RA to make a failing Auto Sync case pass.

**Review stages:**

- **Gate 0:** Approve ADR/plan and PR A's explicit audit/authority schema. No unattended code until audit is independently reviewed.
- **PR A:** Pure permissions, grant store, effect audit/classifier, preview with tests. **No scheduling or unattended execution.**
- **PR B:** Read-only background checker, status/history/notification transport and timer, disabled by default. **No unattended mutation.**
- **PR C:** Audited Auto Follow through a separate fresh-authority execution path; failure-injection/two-host tests.
- **PR D:** Full TUI/CLI setup, help, documentation, physical two-machine acceptance and release hardening.

Require separate review/merge per PR. Auto Publish and Auto Reconcile require **later design approval**, not a concealed Phase 11 option.

## 1. Architecture and code baseline to verify

The following source anchors were inspected at PR #74 `db86c11`; reconcile them with merged `main` first:

| Existing owner | Relevant code | Required reuse / gap |
|---|---|---|
| `internal/syncengine` | `cursor.go`, `lock.go`, `reconcile.go`, `types.go` | Machine-local cursor, canonical profile-root lock, semantic actions. Extend with *separate* Auto authority types, not an alternate base. |
| `internal/workflow` | `sync.go`, `sync_bootstrap.go`, `sync_execute.go`, `sync_safety.go` | `InspectSync` and execution's pinned fresh checks. Current `ExecuteSync` requires approvals and always goes through `PublishSyncRevision`: **must split follow-only publication verification from outgoing push**. |
| `internal/profilegit` | `revision.go`, `fetch_upstream.go`, `sync.go`, `sync_publish.go` | Exact fetched tracking revision, fast-forward, remote verification. Never use generic `pull` or push in Auto Follow. |
| `internal/app` | `sync.go`, `sync_apply.go`, `app.go`, `providers.go` | Cobra/JSON CLI, session construction, provider adapters and support for dry-run/fresh scoped Restore. |
| `internal/tui` | `welcome.go`, `catalog.go`, `modals.go`, `screens/reconcile*.go` | Existing plain-English help, action palette and collapsible groups. Add Auto Sync interface without duplicate navigation. |
| `internal/machine` | State-root and binding helpers | The profile-keyed private state directory already used by the cursor and lock. |
| Restore provider implementations | `internal/providers/{packages,defaults,themes,plugins,config,shell,hooks,services,resources}/` | Enumerate every current concrete operation, nested dependency, activation and external side effect before auditing safe automatic classes. |
| Acceptance | `test/sync/run.sh`, `test/sync/test_acceptance.py` | Existing two-host isolation/PTY conventions. Do not touch the developer's real home/user manager. |

### Architectural layers

```text
TUI / CLI / systemd user oneshot
        |
        v
machine-local Auto Sync settings + grant + safe status (never from Git)
        |
        v
existing InspectSync(fetch=true) -> whole-plan eligibility classifier
        |
      no? ----------> needs review / no writes / notify
        |
       yes
        v
profile lock -> reload local grant -> fresh pinned Sync/Restore plan
        |
        v
operation-level authority graph comparison -> exact fast-forward
        |
        v
fresh scoped Restore -> check results -> verified settlement -> cursor
```

The classifier is descriptive during preview. Only the locked **fresh grant + operation graph** constitutes unattended authority. The restored configuration remains provider-owned; Auto Sync does not patch arbitrary files outside provider Restore.

## 2. Gate 0 — Complete the concrete provider operation audit **before PR A code**

The approved design intentionally did not label arbitrary existing operations as unattended-safe. The audit must be a **reviewed deliverable**, not a guess made while writing the automation executor.

**Proposed artifact:** `docs/planning/audits/2026-10-17-auto-sync-restore-capabilities.md` (create only after gate approval).

### Task 0.1 — Inventory real provider operations

Enumerate every executable Restore `model.Operation` emitted by current providers and adapter/finalizer paths, including dynamically batched commands and dependent operations. Record:

- concrete category, operation ID/family, target mapping (`Items` / `Resource`) and dependencies;
- file copy/write/remove, command invocation, command implementation, executable payload, network download, package-manager hook, activation/reload/restart, service changes, privilege requests;
- compatibility requirement, risk and reversibility, output that might reveal secret values, prompt possibility, unsafe failure/retry behaviour;
- whether a new remote artifact could turn an otherwise safe-looking file write into code execution later;
- whether a low-risk, noninteractive, repeatable, *bounded* operation with stable dependency semantics can be proved unattended-safe.

**Source observations, not approvals:** `defaults.Plan` emits `omarchy default ...` with `RiskLow`; Themes has copy, activation and removal; Config can write files and call `hyprctl reload`; Packages has installs/removals and `sh -c` operations; Plugins has installs/copy/activation paths. These facts do **not** prove unattended safety.

### Task 0.2 — Approve an explicit v1 capability registry

Submit a table keyed by **provider + stable action/effect family + dependency contract**, with decisions:

- `requires_human` (**default and catch-all**);
- `unattended_safe` (Conservative, Balanced, Hands-off);
- `unattended_routine` (Balanced, Hands-off);
- `unattended_extended` (Hands-off only, where separately bounded and approved).

**Initial mandatory denies for all presets:** unreviewed executable or script-bearing profile content, hooks/shell code, plugin code/download/enablement, package installs/removals and post-install hooks, unreviewed service unit changes/activation, arbitrary Resource writes/projects, delete/removal, unknown compatibility, migration, Force/Exact override, interactive/elevation and unknown operations. A higher preset never overrides a hard deny. For any proposed exception, include concrete evidence and regression tests for why it is truly safe.

**No production allow-by-category shortcut.** If no operation passes the audit, Conservative/Balanced/Hands-off **behave as checking/notification only** until a separately reviewed allowlist is delivered. Do not quietly widen the classes to manufacture an Auto Follow demo. PR C is not called complete until at least one real provider effect has been independently audited, approved, and validated in a two-host fixture.

**Gate 0 acceptance:** Human reviewer approves the exact allowlist, threat model, schema, effect versioning, anticipated reachable Auto Follow case and expected default-deny cases. Otherwise stop; proceed with read-only/checking work only if explicitly authorized.

## 3. PR A — Local authority domain, safe previews and test suite

### Task A1 — Versioned, profile-root-isolated grant store

**Candidate files:**

- Create `internal/autosync/authority.go`, `authority_test.go`, `store.go`, `store_test.go` (new dependency-light domain).
- Reuse/extract `internal/machine` private state helpers and `internal/syncengine/cursor.go` state-directory validation with behavioural parity tests. Avoid copying subtle path/permission checks.

**Illustrative model (field names subject to local schema review):**

```go
type Mode string // notify-only, conservative, balanced, hands-off, custom
type RestoreLink string // linked, copy-once, separate

type Grant struct {
    Version int
    Enabled, Paused, Suspended bool
    Mode Mode
    RestoreLink RestoreLink
    IntervalMinutes int
    Identity IdentityBinding // canonical root, local host/installation ID, selected machine, branch, upstream, sanitized remote identity
    AllowedScopes []Scope // category, bounded target selector, operation family, audited class
    TargetDenies []TargetSelector
    CapabilityVersion string
    Generation uint64
    ApprovedCeilingDigest string // deterministic, non-secret digest over the locally reviewed grant
}
```

`ApprovedCeilingDigest` is a **consistency check**, not a security barrier against malicious processes already running as the same Unix user. State permissions protect against other users and accidental import, not same-UID compromise. Do not claim local confirmation is cryptographically tamper-proof. The grant's identity must fail closed after host/installation, profile-root, selected machine, branch or remote changes; normal SHA progression does not invalidate it. Bind to an OS-backed, injectable local installation identity (for example a validated host machine ID or a separately held installation identifier); a grant copied to another host must not become live just because both use the same profile path and selected machine label. Never expose the raw host identifier in status/history.

**Tests first:** round trip, default disabled, malformed/unknown version, invalid scope, broad wildcard bypass attempt, profile-root and different-host isolation, copied grant on a second computer, symlink at file and parent, path traversal, non-regular files, foreign owner/mode where supported, 0700/0600, file size cap, fsync/atomic rename, removal and recovery. Verify never serialize credentials, raw remote URLs, target values, raw exceptions, or private file contents. No state file may be directly loaded from the Git checkout.

### Task A2 — Exact monotone permission algebra, presets and Restore linkage

**Candidate files:** `internal/autosync/preset.go`, `policy.go`, `policy_test.go`.

An operation is eligible only if **ALL** these are true:

```text
installed and enabled AND not paused/suspended
AND identity and grant/version match
AND current effective Restore == Apply for its target
AND operation lies within locally approved category/target/effect ceiling
AND no matching target/category deny
AND provider capability version is recognised and operation audited
AND fresh provider/Restore compatibility & safety permit it
AND no interactive, remote-executable, destructive or otherwise hard-denied effect
```

Preset mapping: Notify=none, Conservative=`safe`, Balanced=`safe|routine`, Hands-off=`safe|routine|extended` (only entries explicitly approved in Gate 0); Custom=explicit locally reviewed scope. A Custom exception may widen the selected preset's suggestion only after an explicit local confirmation and must never exceed capability/hard safety limits. A deny always wins.

- `linked`: capture a local grant **ceiling** with exactly the scopes/class families confirmed by the user. Current Restore rules reduce it. A changed policy can widen actual execution only if the resulting scope was *already* within that ceiling; uncertain expansion blocks and requests reconfirmation.
- `copy-once`: translate current Restore selections into a **frozen local scope** at confirmation time. Later Restore edits do not mutate it.
- `separate`: scope comes solely from locally confirmed automation settings, but current Restore Skip still vetoes action.
- New targets are allowed only by a locally approved bounded category/class wildcard *and* their freshly audited operations. New/unclassified actions always deny. A remote profile policy broadening cannot extend the local envelope.

The permission comparison must be monotonic under added denies/restrictions. If an evidence value becomes `unknown`, eligibility cannot increase.

**Tests:** full preset × link mode × Restore Apply/Skip × category/target exception × operation family × capability version table; changes to remote policy, new target, force/exact policy, machine switch, capability registry upgrade/downgrade; confirm no `Restore Skip` bypass.

### Task A3 — Provider effect descriptors and whole-plan evaluator

**Candidate files:** `internal/autosync/effects.go`, `effects_test.go`, `classify.go`, `classify_test.go`; narrow provider adapter methods where the audit proves necessary.

Design a versioned, deterministic **effect graph** over fresh Restore `model.Operation`s. Use provider-owned target lookup/operation metadata, not user-edited `Operation.ID` strings alone. For each operation retain: target(s), effect class, action kind, command/executable/network/privilege flags, file write/remove/overwrite, activation, interactive requirement, compatibility, dependency IDs and fingerprint of *non-secret authority-relevant shape*. An unknown provider command or dependency fails closed. Bundled operations cannot disguise a stronger child effect.

**Precise requirement:** the preview must evaluate a **representative executable Restore plan**, not only the Sync `ActionRestore` target list. If the existing planning API requires the profile checkout to be updated first, introduce an isolated read-only planning context for the pinned remote profile view. It must not write `HEAD`, profile files, cursor or machine; prove that it uses the same provider planners and semantics as the eventual fresh post-fast-forward plan. The final locked operation graph must equal or be a **strict subset that the reviewer explicitly allowed**; unexpected new operations, targets, dependencies, commands, risk/effects or compatibility changes block. A smaller set is not automatically safe if dependencies or intended outcomes have changed.

**Tests:** unknown op/class defaults deny; multi-target/multi-operation mapping; missing/cyclic dependency, shared dependency, one high-risk child in an otherwise low-risk bundle, secret-argument hashing/redaction, changed command args where meaning matters, provider capability version change, executable content appearing after preview, stale policy; no profile/machine mutation from evaluation.

### Task A4 — Read-only Auto Sync preview and reason model

**Candidate files:** `internal/workflow/auto_sync.go`, `auto_sync_test.go` and thin `internal/app/auto_sync.go` inspection commands as needed.

Preview calls the existing `InspectSync(ctx, SyncInspectOptions{Fetch: true})` or explicitly no-fetch variant, then classifies the **entire** plan against local authority and scoped Restore effects. No eligible preview is authority to execute later. Return typed, bounded reason codes and counts for: eligible, review-required, already matching, policy-preserved, error; distinguish manual-baseline-missing, dirty profile, diverged/committed local changes, conflict/defer, wrong identity, stale cursor, unsupported effect, changed Restore policy, incompatible Restore, auth/offline and invalid grant.

**Tests:** no cursor/no bootstrap, valid prior manual Sync, clean/dirty Git, remote fetch, behind-only and both-side changes, protected targets, local change wanting Capture, no actions, unknown artifact, nested provider plan differences. `preview` does not write grants, checkout, cursor or live machine. Fetch is labelled explicitly.

**PR A checkpoint:** new pure permission and preview tests + full Go/race/vet/build; independently review the capability matrix and every local-authority expansion path. **No timer installation or unattended writes.**

## 4. PR B — Scheduled inspection, non-secret status, notifications

### Task B1 — Pure background cycle and bounded exit behaviour

**Candidate files:** `internal/autosync/cycle.go`, `cycle_test.go`, `internal/workflow/auto_check.go`.

A cycle loads the local binding/grant; if Disabled/Paused it reports that status without fetching. Otherwise it fetches once via existing `InspectSync`, classifies and stores a sanitized report. **No calls to `FastForwardTo`, `ExecuteSync`, `CaptureApprovedTargets`, `PublishSyncRevision`, `ApplyApprovedRestoreScope` or `CursorStore.Save`** in PR B. A read-only check must not create a grant implicitly. Ensure one logical provider read cycle, bounded timeouts and safe cancellation.

Tests for no network/credentials, stale fetch, duplicate concurrent checks, repository lock held by manual Sync (read-only checks may coexist if safe), and repeated failures without escalating to write calls.

### Task B2 — Local status, history and notification adapters

**Candidate files:** `internal/autosync/status.go`, `status_test.go`, `notify.go`, `notify_test.go`.

Store independent `last_checked_at`, `last_completed_sync_at` (read from verified Sync evidence, **never** inferred from latest fetch), status/reason, safe counts, next scheduled check and bounded recent activity. Do not persist live semantic fingerprints, raw target data, profile contents, token-bearing remote URLs, environment or subprocess stderr. Limit both record count and bytes; write private state atomically, recover safely if history is corrupt. Notification failure never changes Sync success.

Use an injectable `Notifier` interface, no dependency on a desktop session for correctness. Coalesce repeated identical reasons, including recurring auth/offline warnings. Offer a safe CLI/TUI route to inspect pending changes; never expose secrets via notification markup or external commands.

### Task B3 — systemd user timer and service

**Candidate files:** `internal/autosync/systemd.go`, `systemd_test.go`, `internal/app/auto_sync_systemd.go`, test fixtures in `test/autosync/`.

Proposed unit names: `omarchy-blueprint-auto-sync.service` and `.timer` (verify no collision with existing managed user units). **One selected profile binding per user timer** in v1; switching profiles requires a fresh local preview/confirmation and must not cause a stale timer to run on an abandoned profile. Keep profile selection in private local state; do not embed token-bearing remotes in unit `ExecStart`. If the implementation embeds an absolute profile path, use rigorously escaped systemd arguments and verify identity at runtime. Use the resolved installed executable path, never `sh -c` around interpolated paths.

Proposed semantics to confirm with real `systemd --user` tools before freezing the unit template: `Type=oneshot`, short delay after user-manager startup (`OnStartupSec`), a bounded `OnUnitInactiveSec` interval (~30m default), `RandomizedDelaySec`, bounded `TimeoutStartSec`, and **no `Persistent=true` claim of missed-run catch-up** when using monotonic timers. Do not set `WantedBy=default.target` for a mutating service directly; enable/disable the timer only. No linger, root service, machine wakeup or background daemon.

Unit generator must be testable with a fake user-manager and isolated `XDG_CONFIG_HOME`; never write a real `~/.config/systemd/user` during tests. On enable: write private grant/binding, generate/validate unit, reload and enable timer with rollback if any step fails. On Pause: deny execution in the grant immediately, optionally stop/pause timer, **do not assume stopping a timer cancels an already-running job**. On Disable: atomically revoke local permission before disabling the timer; status remains readable. On Resume: check identity and grant version, and require preview/reconfirmation for any expansion.

Prevent Blueprint's Services provider from treating its own auto-sync units as portable user Services: establish explicit ownership/exclusion with tests in `internal/providers/services` or adapter. Check timer login/logout/suspend behaviour; if user manager is absent, show a useful message rather than trying root privileges.

### Task B4 — CLI checking and management plumbing (read-only stage)

Register `auto-sync` under Cobra, initially exposing `status`, `check`, `preview`, and `history` with JSON parity and exact safe effect descriptions. `check` explicitly fetches; `status` and `history` never fetch. The internal scheduled checker uses the same service API, not shell invocation of interactive `sync apply`.

PR B leaves `run` explicitly unavailable for mutations. All states are opt-in; `notify-only` may be enabled for checks after local confirmation but does not itself authorize applying changes. Keep user-facing examples honest about background login requirements and credentials.

**PR B checkpoint:** unit snapshots/dry-run tests, read-only observation and no-write spies, auth/offline/cancellation, no leakage, full Go/race/vet/build and hosted checks. Real timers are **not installed** during automated CI.

## 5. PR C — Auto Follow execution through existing trusted transaction

### Task C1 — Refactor shared Sync phases without weakening interactive approvals

**Candidate files:** `internal/workflow/sync_execute.go`, new `internal/workflow/auto_follow.go`, corresponding tests, narrow `internal/profilegit` helper if needed.

Extract shared internal functions for locked Sync revalidation, exact pinned fast-forward, fresh Restore scope planning, post-apply verification and cursor settlement, while keeping manual `ExecuteSync`'s callbacks mandatory. Do not duplicate the entire orchestration. Choose API boundaries such that no unattended caller can supply a permissive approval callback or invoke an unguarded Restore execution directly.

A suggested new private authority object is created **only by** the locked, revalidated local grant evaluator (not deserialized from a reviewed plan or accepted from CLI JSON). A public `ExecuteAutoFollow(ctx, ReviewedAutoPlan)` entry point must itself reload the grant and inspect/authorize everything; the supplied preview is advisory. Use the existing profile-scoped `AcquireProfileLock` for all writes; never acquire a different independent mutation lock that deadlocks with manual Sync.

### Task C2 — Pinned remote-only fast-forward with no outgoing publication

Require: valid manual Sync cursor, clean checkout/index, same identity, single upstream, no divergence, `L == B` (or another **explicitly proved no-unpublished-local** condition), fetched `R` an ancestor-descendant safe fast-forward, no Capture/Publish action, no committed local unshared intent, no conflict or `Defer`. `R` is pinned to the inspected SHA; the origin identity and remote ref are checked again under lock. A new commit arriving after preview either forces a fresh evaluation or stops; **never follow an unreviewed newer ref**.

The existing manual executor currently calls `PublishSyncRevision` even for follow-only plans. Introduce a **read-only exact-remote-revision verification** branch for Auto Follow and ensure an outbound push can **never** be reached through it. A remote-only plan may safely fast-forward local profile checkout using the existing exact `FastForwardTo` primitive, then perform the authorized Restore. The operation must never create a local reconciliation commit or send one to remote.

Before fast-forward, inspect/validate the pinned remote's provider artifacts and generate an eligible scoped Restore **operation** plan in an isolated read-only context; after fast-forward, fresh-plan again and verify equality of authority-relevant operations/dependencies/compatibility *before touching the machine*. If the pre-fast-forward operation plan cannot be built faithfully, **do not** permit automatic application. A blocked post-fast-forward replan must report partial profile-only progress, keep the cursor and suspend if needed; do not automatically roll back a shared profile checkout by rewriting Git.

### Task C3 — Fresh, locked grant check at each mutation boundary

Under the same lock:

1. Reload and validate local grant, phase generation, selected profile/machine/upstream and approved capability version.
2. Re-fetch or revalidate pinned remote and entire semantic Sync plan with shared read cycle; compare with reviewed plan.
3. Reconstruct the proposed executable Restore operation graph and verify entire plan eligible by preset + overrides + current Restore policy + provider effects/compatibility.
4. Recheck *before profile checkout*; perform exact pinned fast-forward only if allowed. No outgoing push.
5. Reload profile; fresh-plan scoped Restore; recheck graph/identity/grant and require no wider/new effects; fail before machine mutation if changed.
6. Run the supported noninteractive Restore path with default safe/additive bounds; no interactive elevation, no Force/Exact override or unreviewed service activation.
7. Verify Restore and complete Sync semantic settlement; advance the existing cursor only if every applicable target meets current settlement requirements.
8. Record sanitized success/incomplete result and send best-effort notification.

Pause or Disable must be checked again at the last safe boundary. If they occur **mid-irreversible operation**, do not misrepresent them as rollback; cancellation propagates when safe and a partial transaction requires manual review. Every noninteractive executable must have bounded duration and credential prompting disabled.

### Task C4 — Failure, locking and retries

Before code, build failure-injection fixtures for: race with manual Sync; changed target/new dependency; changed remote SHA; changed Restore policy and a re-enabled target; edited local grant; invalid local state; off-network/background auth prompt; timed-out Git; cancellation at each stage; failed fast-forward; failed Restore after local checkout changed; verification failed; cursor-save failed; and corrupt status files.

No failure may move the cursor ahead of verification. A dangerous partial state sets **Suspended for safety**, prevents further unattended mutation, and provides an exact manual `Sync` recovery path. A busy lock skips rather than escalating or spinning. Timer retries have bounded backoff; no aggressive loop, no automatic repeat of non-idempotent operations.

### Task C5 — Two-host acceptance

Extend `test/sync/` or add `test/autosync/` using two isolated fake home directories, disposable Git remote and fake providers/PTY. Tests must include:

- Host A manually completes Sync and publishes a specifically **audited safe** change; Host B has already manually established its cursor and locally enabled Auto Follow; B's background cycle fetches, fast-forwards, applies and verifies it with no prompt or push; B's cursor advances only after success.
- Unapproved target/category, Restore Skip, unknown op, remote Restore-permission expansion, conflict, dirty profile, local unpublished change, and disallowed executable change: **no unattended mutation** and visible Needs review.
- A mixed allowed/disallowed plan: **no** partially applied subset.
- Failure after profile checkout but before Restore completion: truthful partial state, cursor behind, automatic application suspended.
- Concurrent manual and scheduled run: one writer; no race, deadlock or double Restore.
- Note that the background service is allowed to **fetch**, but never allowed to `push` or `commit`.

Run existing 11 Sync/PTy acceptance scenarios unchanged and ensure no RA modifications.

**PR C checkpoint:** all command spies prove zero fake manual approvals; integration and race tests green; failure recovery reviewed before enabling actual mutating timer. No system tests write into the developer's real `$HOME`.

## 6. PR D — User controls, documentation, and physical acceptance

### Task D1 — CLI permission management and precise effects

**Candidate files:** `internal/app/auto_sync.go`, `auto_sync_test.go`, root registration in `internal/app/app.go`.

Final command surface (names proposed by spec; freeze/help-test in PR D):

```text
omarchy-blueprint auto-sync status
omarchy-blueprint auto-sync check
omarchy-blueprint auto-sync preview
omarchy-blueprint auto-sync enable
omarchy-blueprint auto-sync configure
omarchy-blueprint auto-sync pause
omarchy-blueprint auto-sync resume
omarchy-blueprint auto-sync disable
omarchy-blueprint auto-sync run
omarchy-blueprint auto-sync history
```

`enable`/`configure` must show the effective scope, all denied/unknown classes, Restore-link mode and a preview of the exact current plan; require **local interactive confirmation** for new or expanded authority. Non-TTY/JSON/`--yes` does not silently mint or widen it. `run` executes only under an already-confirmed matching local grant and can never override policy. `pause`/`disable` revoke new automatic execution quickly, in the private state, without exposing the grant through Git. Status/history are read-only. Check/preview explicitly fetch and inspect; they cannot apply.

Report configuration validity, actual selected profile/machine, last check, last verified completed Sync, next check, and typed reasons. Errors never contain raw Git URLs/credentials or raw file contents.

### Task D2 — TUI Auto Sync subview

**Candidate files:** `internal/tui/screens/auto_sync.go`, `_view.go`, `_test.go`, registration/action adapters, `internal/tui/catalog.go`, `internal/tui/modals.go`; reuse current collapsible groups, layout primitives, keyboard navigation and help conventions.

Required collapsible sections: **Status**, **How automatic?**, **Use Restore settings**, **What can run automatically?**, **Needs review**, **Recent activity**. Counts and the consequences of granting permission appear **before** the detailed list. Opening/previewing changes nothing. Changes to mode/linking/overrides are *staged UI choices* until local confirmation; custom deny beats allow and linked Restore policy cannot silently widen authority.

Required copy (from design; deviations require documented truthfulness):

- Title: **Auto Sync**.
- Intro: “Blueprint can check your shared setup in the background and automatically handle changes you allow on this computer. Anything that needs a decision will wait for you.”
- Disabled: “Auto Sync is off. Your computer will not check for or apply shared changes in the background.”
- Preview heading: **What Auto Sync would do**.
- Preview: “These changes could happen automatically. Everything else will wait for your review.”
- Link: “Use my Restore choices to limit what Auto Sync can change.”
- Confirmation: “Turn on Auto Sync with these permissions on this computer?”
- Notice: “Changes downloaded from your shared profile cannot give Blueprint new permission to run automatically.”
- Help: “Automatic checking is different from automatic application. You can pause or turn off Auto Sync at any time.”

Show modes **Notify only / Conservative / Balanced / Hands-off / Custom**, with Disabled the default and Balanced merely suggested for an actively consenting user. Show **Keep linked / Copy once / Keep separate** and clarify their different future behaviour. Show category groups first, individual targets and operation types collapsed beneath them. Show blocked examples but avoid 100-row dumps. High-risk and potential executable changes require explicit review. Add actions **Preview**, **Enable**, **Pause**, **Resume**, **Check now**, **Review pending changes**, **Change permissions**, **Turn off**, **View recent activity**. Help/footer/action palette agree, and pressing Enter on a group header never enables automation.

### Task D3 — Documentation and operational recovery

**Candidate files:** `docs/guide.md`, `docs/tui.md`, `docs/cli.md`, `docs/manual-test-checklist.md`, `ROADMAP.md`, spec/ADR/plan links. Document:

- Difference between checking, automatically applying and future automatically sharing.
- Login-only timer semantics; `systemd --user` dependency and how to view timer/status, pause and disable without guessing.
- Credential-unavailable/offline behaviour, bounded retry and no password prompt.
- Why Restore Skip beats all presets; what linked/copy-once/separate mean.
- How to review pending changes with manual Sync; difference between last check and last completed Sync.
- Why remote policy changes never expand unattended authority; why scripts/packages/services remain human-reviewed unless separately audited.
- Manual recovery from partially completed Auto Follow; don't offer reset/rebootstrap as an automatic fix.
- Scoped log/notification privacy and future Auto Publish / profile-from-URL / optional Omarchy plugin, with no functional controls promised before their own PRs.

### Task D4 — Physical acceptance and release gate

Use **two disposable/appropriately backed-up real Omarchy computers**, the actual Git transport and actual user-manager behaviour. Human reviewer executes and reports:

1. Both machines have completed normal manual Sync and the same selected upstream; Auto Sync remains off until locally enabled.
2. Choose Balanced + Keep linked, inspect the preview, and verify the classified safe operation matches the approved audit.
3. A permitted change published manually on host A reaches host B on a scheduled check; B applies/verifies it with no interactive prompt and no outgoing push.
4. A disallowed, executable, incompatible or conflicting change on A is **not** applied on B; notification and manual review are clear.
5. Restore Skip on B wins even when Auto Sync would otherwise permit the change; changing Restore policy to expand permission cannot silently widen B's approved ceiling.
6. Verify Pause/Disable before queued runs; offline/auth unavailable status, login/logout/suspend resume and no implicit lingering.
7. Verify a lock collision with manual Sync and a safe injected failure/repair case; ensure cursor stays behind after incomplete results.
8. Open Status/Help/History in 140-, 100-, 80-column and compact terminals. Confirm plain-English instructions and no huge uncollapsed target list.
9. Reopen manual Sync on both machines after success and confirm no unexpected pending work beyond intentional protected differences.

Do not run destructive failure injection on an unbacked-up real machine. Hosted CI, AUR Package Validation and Reconstruction Assurance must all pass on the exact reviewed head. Complete peer code review and require explicit human merge/release permission.

## 7. Test commands and hygiene for **every** PR

From an isolated worktree and clean baseline:

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
bash test/sync/run.sh
git diff --check
```

Add dedicated Auto Sync tests and run them in PR A onward. Keep existing release and reconstruction helper suites when touched, and hosted checks at the final commit SHA. Before tests, inject `XDG_STATE_HOME`, `XDG_CONFIG_HOME`, Git config, `HOME`, fake services/commands and fake user manager; **assert no real-profile, real-systemd, pacman or remote writes**. Write red regression tests before implementation and preserve previous safety regressions.

PR reviewer checklist:

- [ ] Source SHA exact and diff limited to reviewed PR.
- [ ] All new authority grants cannot be created or expanded through downloaded profile content.
- [ ] Provider effect table complete and versioned; unknown effects default deny.
- [ ] Whole-plan eligibility and fresh graph comparison proven; no fake approvals.
- [ ] No `push`, `commit`, Capture or semantic merge reachable from Auto Follow.
- [ ] Same lock and cursor contract as manual Sync; failure can't claim success.
- [ ] Background prompt cannot hang; timeouts, cancellation and offline/backoff tested.
- [ ] Private local state/root binding, atomic writes and secret redaction tested.
- [ ] Timer disabled by default and never requires lingering/root escalation.
- [ ] TUI, CLI and systemd share one policy engine; `?` explains behaviour plainly.
- [ ] Multi-host and real-machine test evidence; hosted CI/AUR/RA on reviewed SHA.

## 8. Deliberate exclusions and follow-on work

| Follow-on | Relationship to Phase 11 | Not authorized by this plan |
|---|---|---|
| **Auto Publish** | Separate locally approved authority to Capture, commit and push. Requires fresh Capture audit and push-race/recovery design. | No automatic outbound writes in Auto Follow. |
| **Auto Reconcile** | Future automatic resolution of strictly disjoint semantic history, gated under the roadmap's later bidirectional work. | No automatic history conflict choice or semantic merge. |
| **Get my profile from Git** | Future welcome option: safe clone/validate, machine selection and reviewed manual bootstrap. | No auto-clone or trust-on-first-use automation. |
| **Omarchy Blueprint plugin** | Optional status/control UI reading the same local Auto Sync APIs; can request checks and open manual Sync. | No plugin-held secret/permission or separate Sync engine. |

## 9. Completion definition and remaining review decisions

Phase 11 Auto Follow v1 is **complete** only when the independently approved provider effect matrix allows a genuinely useful narrowly bounded case; machine-local authority cannot be remotely enlarged; scheduled inspection and automatic execution reuse the same safe Sync semantics; every unsupported/partial case stops with an understandable reason; a two-host end-to-end run and physical-machine acceptance pass; and PRs A–D plus their CI/RA checks are reviewed and merged with explicit human permission.

**Before PR A starts**, request approval of the exact capability allowlist and authority schema. **Before PR C starts**, independently review the exact follow-only executor refactor, operation-planning equivalence and no-push invariants. Any discovery that requires weakening the spec is a new design decision; stop rather than choosing a permissive fallback.

No background service or user profile has been altered by this plan.

## 10. Acceptance review notes (2026-10-17)

Feasibility review of the spec, ADR 0029 and this plan against merged `main`
`aa6f127`. Every §1 source anchor exists; ADR number 0029 is free (0028 is the
latest); `test/sync/` (11 acceptance tests) is present. Confirmed source
claims: `ExecuteSync` calls `PublishSyncRevision` unconditionally, which runs
`git push` even when there is no new commit; `InspectSync{Fetch: true}` runs a
plain `git fetch`; `FastForwardTo`, `AcquireProfileLock` and the Services
`ReviewActivation` callback exist as described.

No finding changes an ADR decision. These conflicts and gaps are resolved as
plan requirements:

1. **Roadmap scope.** `ROADMAP.md` "Auto Sync v1" lists Auto Follow *and*
   Auto Publish; ADR 0029 defers Auto Publish. The ADR governs. Task D3 must
   update the roadmap so Phase 11 v1 is Auto Follow, with Auto Publish a
   separately designed follow-on.
2. **Fetch is not read-only with respect to Git refs, and runs outside the
   profile lock.** `InspectSync` fetches inside a read cycle guarded only by an
   in-process mutex. A scheduled check in another process can therefore move
   the remote-tracking ref (a) while a person is reviewing a manual Sync, so
   execution then refuses with "Sync plan changed; reinspect" (safe but
   confusing), or (b) concurrently with manual Sync's `FastForwardTo` or its
   post-push fetch and reachability check. Requirement for decision (vi) and
   Task B1: scheduled `check`/`preview` take the profile lock **non-blocking**
   around fetch + inspection and skip as "busy" when it is held. Spec §9's
   "read-only checks may coexist" applies only to no-fetch `status`/`history`.
   Manual Sync copy should explain a review invalidated by a background check.
3. **Isolated pre-fast-forward planning (A3, C2) is not free.** The machine
   binding (`machine.toml`) and cursor are keyed by canonical profile root,
   and providers build artifact paths from `ProfileDir` (for example Restore
   `Copy` sources under `themes/local` and `plugins/local`). A session over a
   materialized snapshot needs explicit machine/identity injection, and the
   operation-graph comparison must use profile-relative artifact paths, not
   absolute ones. Prove this equivalence in PR A (A3), because if it cannot be
   proved, C2 forbids automatic application and Auto Follow is unreachable.
4. **Portable Restore policy can request removal.** The profile's
   `restore_convergence` (`exact`), conflict mode and activation mode are
   portable, remotely editable inputs, in addition to the CLI's
   `--force`/`--exact`/`--activation`. Auto Follow must treat them as
   untrusted: removals and overwrites stay hard-denied whatever the profile
   requests, and a remote change to these settings is a policy change that
   needs review, not something to adopt.
5. **Reachable-case risk.** The mandatory denies cover most current operations
   (packages, plugins, services, hooks/shell, resources, removals, config
   files that are later sourced as code, and theme activation, which runs
   Omarchy theme hooks). Plausible `unattended_safe` candidates are narrow
   (for example installing a non-active local theme's files, or a default-app
   change) and each needs evidence. "All presets check and notify only" is an
   acceptable, honestly disclosed v1 outcome, but it must be known at Gate 0,
   before PR C effort.
6. **One state model.** Spec §4.1's states (Disabled, Checking only, Enabled,
   Paused, Suspended for safety) and the five presets overlap: "Checking only"
   is the Notify only preset. A1 defines one enabled/paused/suspended + preset
   model, and run outcomes (Needs review, Error, Offline…) live only in status.
7. **Naming.** Phase 10 also used PR A–D. Branches, execution notes and PR
   titles say "Phase 11 PR A" (etc.) to avoid ambiguity.
