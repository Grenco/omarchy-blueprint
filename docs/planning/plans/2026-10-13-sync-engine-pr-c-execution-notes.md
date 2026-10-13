# Sync Engine PR C execution notes

## Scope and baseline

Interactive execution slice, tasks 10–13 of the approved Sync Engine v1 implementation plan. PR A and PR B are already merged. PR D, Sync TUI and automation are outside this branch.

Starting main: `73f00cd45430d5a1a0a1008e14d6f9971a395a17` (PR #71 merge).

Worktree: `.worktrees/sync-engine-pr-c`; branch: `feat/sync-engine-interactive-execution`.

Final implementation SHA: `8191cfd57c4c696555842fd116214fa5dbad9228`.
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
