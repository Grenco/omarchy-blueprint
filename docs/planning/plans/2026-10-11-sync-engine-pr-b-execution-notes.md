# Sync Engine PR B execution notes

Scope: approved implementation plan tasks 5–9, read-only Sync planning.

- Starting main: `4fb4e55438f0e38455c90d7a36fa5a8af983c183` (PR A restored by #70).
- Worktree: isolated `sync-engine-pr-b`, branch `feat/sync-engine-read-planning`.
- Pre-review implementation head: `e4fe89073edcfffaf3d3d13b6d09046bd8c73894`.
- Runtime used for local verification: Go 1.27.1, linux/amd64.
- Original implementation SHA: `b8b982fad4ff6f6b77f0829fc01a6edd6a059fcc`.
- Final implementation SHA after external review fixes:
  `5fd7bb29e3c939d103befb0379c4029a33c4b6c7`.
- Fresh-context review: five Important findings, all addressed in one TDD fix
  pass; no Critical findings or deferred Minor findings.

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

## External review follow-up (PR #71)

Reviewed publication head: `59bc32f1e015a6c46220f6a4f0e875718e372ba1`.
Starting main remains `4fb4e55438f0e38455c90d7a36fa5a8af983c183`.
Both reported P2 findings were reproduced and addressed within PR B:

- Providers now bind Restore safety evidence to each historical semantic
  desired state, derived from the same frozen live facts. Reconciliation selects
  evidence for the effective desired outcome, not the first local inspection.
  Missing evidence fails closed. When equal intent has differing safety evidence
  across historical views, every matching view must allow Restore (conservative
  blocking, not new authority). Capture evidence remains separate.
- Sync uses the existing bounded read-provider scheduler with its approved
  four-worker limit. Results aggregate in provider order. Meaningful failures
  cancel workers; all launched workers and shared slot loaders are joined before
  returning, without partial observations. Capture/Restore scheduling is unchanged.

The remote-only Defaults `firefox` → `custom.desktop` regression fails with the
old safety selection and passes with `restore-ineligible` using one Defaults
scan and one Packages detection. Its disposable fixture verifies unchanged HEAD,
index, checkout and cursor. Services inventory-sharing tests remain green.
Scheduler regressions prove peak work of four, deliberately reversed completion
with deterministic results, meaningful-error preservation, caller cancellation,
loader joining and rejection of late successful results after cycle termination.

Fresh verification on the final code: focused suites, `go test ./... -count=1`,
`go test -race ./... -count=1`, `go vet ./...`, `go build ./...`, and all 66
release/helper tests passed (Go 1.27.1 linux/amd64). No additional review agent
was launched; external review remains the human's workflow. Hosted checks must
rerun on the new publication head. Reconstruction Assurance is unchanged.
No real user profile or machine state was inspected or mutated. PR C has not begun.

## Tests and evidence before final review

Passed before review and again on the complete post-review implementation:

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
10. Legacy package identities use one provider-owned canonicalization context
    from the immutable live catalogue, consistently across B/L/R/M, policy alias
    precedence and disposable composition. Historical metadata is normalized in
    detached values, not rewritten on disk. Cost: semantic key interpretation is
    bound to this inspection's catalogue rather than independent per-revision
    catalogues; fresh future execution must establish its own matching context.
11. Changed resource interpretation blocks the read-only plan pending reinspection
    under the selected mapping. Cost: even a one-sided remap requires external
    control-plane reconciliation/new inspection in this slice; no target may be
    declared settled from the old location.

## Final review fixes

- Preinstall children remain managed/absent independently of group management;
  copying a child never enables the group.
- Legacy official/AUR aliases and tombstones share canonical preinstall IDs.
  Existing canonical policy rules win over legacy aliases; exclusions remain
  unmanaged. CLI coverage exercises a legacy historical declaration with one
  shared Packages detection.
- Overlay themes retain their snapshots; composed trees rerun provider-owned
  artifact validation, not just metadata projection.
- Services gather template instance identities and owned artifact evidence across
  all captured histories without another inventory, including remote-added
  instances whose actual state already matches upstream.
- Resource remaps cannot produce settlement using an observation bound to old
  roots. The plan blocks before returning misleading target results.

Each finding has a regression observed failing before its fix and passing after.
Post-fix focused suites, full tests, full race tests, vet and build all passed.
The unchanged release/helper suite passed 66 tests. Hosted checks are not claimed
as complete locally and must run on the eventual PR head.

PR C has not begun and remains gated on human review and merge of PR B.
