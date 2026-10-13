# Sync Engine PR C execution notes

## Scope and baseline

Interactive execution slice, tasks 10–13 of the approved Sync Engine v1 implementation plan. PR A and PR B are already merged. PR D, Sync TUI and automation are outside this branch.

Starting main: `73f00cd45430d5a1a0a1008e14d6f9971a395a17` (PR #71 merge).

Worktree: `.worktrees/sync-engine-pr-c`; branch: `feat/sync-engine-interactive-execution`.

Initial PR C implementation SHA: `8191cfd57c4c696555842fd116214fa5dbad9228`.
The subsequent execution-notes commit changes documentation only; the PR head
identifies that documentation-inclusive review revision.

Runtime: Linux amd64, Go `go1.27.1`. Hosted CI additionally exercises the
repository's unchanged Go 1.25/1.26 matrix.

All execution tests use disposable local Git repositories and profiles. No user profile or machine state has been mutated. Reconstruction Assurance is unchanged. Independent review will be supplied by the human; no review agent is dispatched.

## Interface reconciliation

- `FastForwardTo` takes both expected local SHA and reviewed remote SHA. The plan's one-argument sketch cannot independently enforce its own stated exact-local-HEAD precondition. This is a narrow internal signature amendment, not a change in Sync intent.
- Workflow orchestration needs fresh Capture and Restore approvals after history reconciliation. Resolution choices must not stand in for those approvals.
- Existing category-scoped Capture and Restore APIs cannot expand target-level Sync intent to every target in a category. Narrow target-scoped authority support remains required before execution integration.
- The execution request therefore includes fresh Capture/Restore approval callbacks, terminal availability, and optional one-run Restore/activation review intent. These are not durable conflict choices or cached mutation authority.
- Baseless divergent histories require external Git reconciliation. Reconcile-upstream also refuses differing local-ahead intent rather than silently overwriting it. Bootstrap requires a missing cursor; an existing or invalid cursor is not silently replaced.
- Some providers finalize Capture artifacts inside their own Capture implementation without an outer transaction hook. The new target-scoped entry point keeps a complete private managed-artifact checkpoint until staging, fingerprint rechecks and Save succeed. It rolls back failed scoped Capture without changing ordinary Capture behavior.

## Git transaction work

Pinned fast-forward validates exact local/cached upstream identities, clean checkout/index, ancestry, cancellation and absence of unfinished Git transactions. It performs no fetch or pull. A real Git test advances the server after review and proves execution still follows only the reviewed cached SHA.

Semantic reconciliation uses an ancestry-only `ours` merge and replaces the complete managed tree from a private validated staging copy, including deletions. It audits every divergent commit, including reverted unmanaged changes. Staged and committed trees are independently compared with staged blob identities, executable bits and unchanged unmanaged content. Same-tree reconciliation still records both parents.

Pre-commit failure triggers bounded rollback with cancellation detached only for local recovery; Git transport and SystemRunner are unchanged. Recovery checks HEAD, index tree and clean worktree. Uncertain committed outcomes and rollback failures require repository repair rather than continued execution.

Safety tests identified and closed two additional cases: ignored managed files cannot be overwritten, and unsupported symlink rollback trees fail before mutation.

Publication explicitly names the final commit and configured tracking branch,
rejects different fetch/push repository identities, and does not inherit matching
push refspecs, mirror pushes or implicit tag publication. A subsequent fetch
proves reachability only: it does not make a newer remote tip execution authority.
Existing Git authentication and SystemRunner behavior are unchanged.

## Workflow authority and settlement

`ExecuteSync` holds the exclusive per-profile Sync lock through fresh plan
revalidation, publication, execution and settlement. It rejects stale cursor,
machine, HEAD, branch/upstream/repository identity, dirty checkout, changed live
intent or unresolved/semantic-deferred work. Identity is checked again after
fresh human approvals before proceeding to Capture/Restore.

One logical Sync inspection still owns one shared ReadCycle and the existing
four-worker bounded read scheduler. Execution deliberately creates separate
fresh authority inspections and a fresh final settlement cycle; the original
read observation is never execution authority. Read cycles are closed before
human pauses and mutation.

Capture uses the existing approved fingerprint, post-stage reinspection and
provider transactions, narrowed to the selected semantic target set. A semantic
boundary check rejects unselected changes across all participating providers;
private checkpoint recovery also removes failed staged artifacts. Scoped
Capture clones desired data rather than publishing partial mutable state.

Restore uses the existing fresh plan, approval equality, compatibility,
Safe/Exact, activation, apply and verification paths. Provider-owned operation
attribution rejects operations outside selected targets/categories. Target
narrowing does not manufacture Restore category deferrals or conflate them with
Sync semantic Defer. Services review selection remains interactive and run-local.

Mixed execution is: exact reviewed history → fresh selected Capture → managed
commit → explicit push/publication proof → fresh approved selected Restore →
fresh semantic settlement → cursor save. Push failure cannot start Restore.
Skipped-by-user, blocked, failed or category-deferred work cannot advance the
cursor, even if Restore itself returns no error.

Divergent composition applies provider normalization to exact materialized
B/L/R and compares those facts with the reviewed plan before history mutation.
It cannot reinterpret a changed normalization context as new approval. Freshly
approved Capture owns newly captured values and absence provenance; a
descriptive M fingerprint is not substituted for Capture authority. Settlement
then compares persisted final semantic intent against fresh live facts.

The cursor records the exact final settled, published commit, not a later
unreviewed upstream tip. Post-publication failures leave history reviewable and
the cursor behind. The final cursor-write gate reports storage failure without
claiming cursor advancement.

## CLI slice

Added explicit `sync apply`, repeated one-run Keep Local / Accept Remote /
Keep Machine / Keep Profile / Defer flags, dry-run, user-triggered fetch/offline
inspection, fresh confirmations and typed JSON execution receipts. Noninteractive
execution requires `--yes`; this does not bypass authority checks. Human review
shows exact B/L/R and ordered stages. Bootstrap offers explicit reconcile-upstream,
adopt-machine and no-write cancel. No arbitrary mark-HEAD command or persisted
resolution store exists. Private paths, raw profile bodies and Git transport
output are not emitted by new execution failure receipts.

## TDD evidence

Observed failing tests before fixes for:

- Missing pinned fast-forward/semantic merge execution and CLI execution surface.
- Staged semantic tree/ancestry selection, reverted unmanaged history and commit-failure recovery.
- Ignored managed content, unsupported rollback trees and unapproved committed content.
- Fresh approval followed by a changed HEAD, which must not authorize machine mutation.
- Failed-provider artifact leakage and unselected cross-category Capture changes.
- Divergent composition bypassing provider normalization.
- The final cursor-write error handling boundary.

Regression tests additionally cover remote-only follow, local-only publish,
divergent and same-tree histories, mixed remote+machine intent, stale live facts,
Capture failure and changed approval, cancellation/lock release, push rejection
and racing remote history, failed Verify, skipped interactive work, invalidated
cursor state, bootstrap convergence/staleness, publication destination isolation,
duplicate/unknown/meaningless resolutions, typed dry-run JSON and no-write cancel.
Git integration tests use real temporary bare remotes and two clones; all machine
effects are fake. Cursor write failure is injected at the final storage gate;
invalidated cursor storage is separately exercised through full orchestration.

## Verification

All commands below passed on the implementation tree:

```sh
go test ./internal/workflow ./internal/profilegit ./internal/syncengine -run 'Sync|SemanticMerge|FastForward|TargetScope' -count=1
go test ./internal/app -run Sync -count=1
go test -race ./internal/workflow ./internal/profilegit -run 'Sync|TargetScope|SemanticMerge|FastForward' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py'
python3 -m unittest discover -s test/reconstruction/tests -p 'test_*.py'
git diff --check
```

Release/helper suite: 66 tests. Reconstruction helper suite: 103 tests.
No race failures were observed. Reconstruction Assurance workflow, same-runtime
requirements and fixture contracts are unchanged; local helper success is not a
claim that the hosted VM reconstruction gate has already completed.

## Review handoff

Commits separate Git history primitives, authority orchestration, and CLI/docs.
Raise PR C against main for the user's independent reviewer. Do not merge and
do not begin PR D/TUI/acceptance or auto-sync. Preserve the worktree for feedback.

## PR #73 review remediation — safe execution failures

Reviewed starting head: `134bd35279ca67538cfed09f85ff2ccc537cda48`.
Remediated implementation SHA: `cab53e5956eb3b2efe75b9ed83a7ecba0f884607`.
The following notes-only commit identifies the documentation-inclusive PR head.

The reviewer identified that the CLI replaced every ExecuteSync error with the
same generic receipt, suppressing repository repair instructions after failed
semantic merge recovery. Repair signals now have identifiable error markers,
preserved through wrapping/joining rather than inferred from error strings.
The CLI maps those markers to fixed, safe diagnostics and additive JSON
`data.failure` fields: code, severity, message, guidance and repair_required.

Ordinary execution failure remains distinct from repository rollback failure,
uncertain reconciliation outcome and scoped Capture recovery failure.
Repair-required diagnostics explicitly say not to retry Sync until manual repair
and repository/profile validation. They take precedence over cancellation and
remain available on stderr if writing the failure receipt itself fails.
Raw causes, private paths, credentials and subprocess output are never rendered
by this failure boundary. Cursor and execution-authority behavior are unchanged.

Regression evidence:

- Real CLI tests run disposable bare remotes/two clones with the actual package
  adapters and fake machine facts. Injected commit failure with successful abort
  leaves original HEAD and clean index/worktree; injected abort failure leaves
  MERGE_HEAD and reports high-severity repair-required guidance.
- Both human and JSON cases keep the cursor unchanged and omit injected private
  paths, credentials and raw output. The old CLI handler was restored temporarily
  after fixture validation: all four cases failed for missing classification and
  repair guidance; restoring the fix made them pass.
- Wrapped/joined errors retain repair classification over cancellation. A raw
  error merely containing matching repair words cannot impersonate a marker.
- A failing output writer was reproduced red before fixing its stderr fallback;
  repair guidance survives without printing the writer's private error body.

Final focused and full verification passed:

```sh
go test ./internal/app ./internal/profilegit ./internal/workflow -run 'Sync|SemanticMerge|CaptureTargetScope' -count=1
go test ./internal/app -run 'SyncFailure|SyncApplyReports' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py'
python3 -m unittest discover -s test/reconstruction/tests -p 'test_*.py'
git diff --check
```

Helper results remain 66 release tests and 103 Reconstruction tests. No race
failures were observed. RA contracts/workflow, SystemRunner, read-cycle ownership,
Capture/Restore authority and cursor settlement gates were not changed.
Push the remediation to PR #73 for independent re-review; do not merge or start
PR D. No user profile or machine state was mutated.

## Re-review remediation — post-commit verification requires repair

Reviewed starting head: `1561df266d2e9d9de14952c06e3e939325e7ce28`.
Implementation SHA: `115e6341179d661215f88da929f1e25d4ee96644`.

Every error after successful semantic commit now carries
`ErrReconciliationPostCommitVerificationFailed`, including failed parent/tree/
checkout verification probes and cancellation. No committed history is silently
rolled back. The safe CLI classification is
`repository-post-commit-verification-failed`, severity `high`, repair required.
Human and JSON receipts explicitly explain that a commit may already exist and
was not rolled back, and require manual repository repair/HEAD/index/worktree
validation before retrying. Only allowlisted text is rendered.

TDD evidence: before changing production code, both new real CLI human/JSON
post-commit cases failed because they returned `execution-stopped` without a
repair warning. Disposable Git repositories actually committed injected
unapproved content; the tests verify the changed HEAD and committed blob rather
than relying only on injected runner errors. With the fix, they pass and verify
the original cursor remains unchanged, publication has not occurred, and private
paths, credentials and raw output are absent. The existing Git primitive
regression now asserts the repair marker and retained unapproved commit.
Wrapped/joined marker tests prove repair guidance takes precedence over
cancellation without leaking causes.

Fresh verification passed on Linux amd64 / Go 1.27.1:

```sh
go test ./internal/app ./internal/profilegit ./internal/workflow -run 'Sync|SemanticMerge|CaptureTargetScope' -count=1
go test ./internal/app -run 'SyncFailure|SyncApplyReports' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py'
python3 -m unittest discover -s test/reconstruction/tests -p 'test_*.py'
git diff --check
```

Release helpers: 66 tests; Reconstruction helpers: 103 tests. No race failures.
Execution authority, rollback decisions, cursor settlement, observation-cycle
ownership, SystemRunner and RA contracts are unchanged. No user profile/machine
mutation, merge or PR D work. Push to existing PR #73 for independent re-review;
hosted checks must run again on the updated head.

## Integration with latest main

Merged `main` at `da8f76f1e50eeed241d7fa7f844f58528831ee55` into the
reviewed PR head `41072a8a7d335ee6708cf1d3ba0c7f9a0df1b5e9` without rewriting
the reviewed commits. The only textual conflict was in
`internal/workflow/capture.go`: preserve main's unconditional private copy of
the freshly loaded profile and PR C's independent pre-Capture snapshot for
target-boundary validation. Main's mutation serialization, durable-save handling
and resource transaction changes remain intact; scoped approval and reinspection
remain intact. This integration adds no new Sync feature or execution authority.

Verification on the merged tree: full `go test ./... -count=1`, full
`go test -race ./... -count=1`, `go vet ./...`, `go build ./...`, release
helpers (66 tests), Reconstruction helpers (103 tests), and `git diff --check`
all passed. No unresolved conflict markers remain. Hosted checks rerun after
pushing; the PR remains open for review/merge by the human.
