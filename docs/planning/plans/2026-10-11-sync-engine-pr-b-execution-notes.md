# Sync Engine PR B execution notes

Scope: approved implementation plan tasks 5–9, read-only Sync planning.

- Starting main: `4fb4e55438f0e38455c90d7a36fa5a8af983c183` (PR A restored by #70).
- Worktree: isolated `sync-engine-pr-b`, branch `feat/sync-engine-read-planning`.
- Pre-review implementation head: `e4fe89073edcfffaf3d3d13b6d09046bd8c73894`.
- Runtime used for local verification: Go 1.27.1, linux/amd64.
- Final implementation SHA and review disposition: pending final review.

## Implemented boundary

Provider-owned desired/live projections preserve unmanaged, desired presence,
and explicit desired absence. Semantic composition writes only owned temporary
staging trees and reloads them to verify projected intent/control fingerprints.
Historical B/L/R are exact pinned revisions materialized using PR A's isolated
Git primitives and loaded through `profile.Load`.

One logical Sync inspection owns **one shared workflow ReadCycle**. Services and
Packages consume their existing request-local observation slots; historical
projections, policy and reconciliation do not call Status, Overview, Capture or
Restore planning independently.

Inspection **does not mutate the active checkout, index, profile or machine**.
An explicit inspection fetch can update Git objects/remote-tracking refs only.
Sync's read-only Git status disables optional index refresh. There are no cursor
writes/advancement, Capture/Restore execution, pinned fast-forward, merge commit,
background fetch, timers or Sync TUI.

Control-plane state is fingerprinted separately. Two-sided disagreement blocks.
Sync semantic deferral is distinct from Restore execution-scope deferral.
Descriptive action candidates and convergence eligibility are not execution or
cursor-settlement authority. Later execution still requires fresh authoritative
planning, approval, no deferred/skipped/blocked work, and fresh verification.

## Tests and evidence before final review

Passed on pre-review implementation head:

- `go test ./... -count=1`
- `go test -race ./... -count=1`
- `go vet ./...`
- `go build ./...`

Focused Sync/provider/workflow and Profile Git tests passed. Release/helper
tests: `python3 -m unittest discover -s scripts/release/tests -v`, 66 passed.
No Reconstruction Assurance workflow or acceptance criteria changed. Hosted
Reconstruction Assurance must still run on the published PR head.

Real disposable Git fixtures prove local-only, remote-only, same-result and
conflict classifications with exact SHA identities; active HEAD, index tree,
porcelain status, full profile-tree hash and cursor remain unchanged.
A separate index-byte regression covers stat-only file changes.
CLI fixtures prove configured non-origin upstream fetch, offline no-fetch,
full SHA JSON without transport URLs/private staging paths, one Packages
detection per bootstrap, no machine-local writes, and rejection of execution
commands/flags. Provider tests prove shared Services/Packages observations.
Cancellation tests prove cycle close cancels and joins forwarded work.

All tests use temporary repositories and synthetic machine state. No inspection
or mutation of the user's active Blueprint profile or real machine was performed.

## Execution-time decisions

1. User amendments override the older plan, including independent Sync/Restore
   deferral semantics.
2. Composition is disposable historical staging only, never an active-profile
   write.
3. A small `internal/syncprofile` builder/view package avoids provider/workflow
   import cycles and makes staging ownership explicit.
4. Legacy Packages declarations remain intent regardless of Capture flag,
   matching current Status/Restore behavior.
5. A narrow `FetchUpstream` uses the configured remote without changing Git
   authentication or global command-runner behavior.
6. Bootstrap has an explicitly baseless model rather than fabricated B.
7. Sync requires a named selected machine, consistent with PR A cursor identity.
   Portable-only users must define/select one before Sync.
8. Historical artifact validation is snapshot-only; live ownership/tool checks
   remain fresh execution concerns rather than historical normalization probes.
9. Sync uses a narrow optional-lock-free status path; ordinary Profile Git status
   behavior is unchanged.

PR C has not begun and remains gated on human review and merge of PR B.
