# Sync Engine v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Use TDD. Do not collapse the PR review gates below.

**Goal:** Implement Roadmap Phase 10: an interactive semantic multi-machine Sync Engine that compares the machine-local reconciliation cursor, committed local profile, fetched upstream profile, and live machine state; lets the user resolve target-level conflicts; executes only through safe profile-Git/Capture/Restore authority; and advances the cursor only after verified settlement.

**Architecture:** Add a dependency-light `internal/syncengine` domain for cursor/semantic state/classification/plan types; extend `internal/profilegit` with exact-revision read and pinned ancestry primitives; add workflow-owned provider adapters that project and compose semantic profile targets; orchestrate inspection/execution in `internal/workflow`; expose the same plan through CLI and a dedicated TUI Sync workspace. Historical revisions are isolated filesystem materializations loaded with `profile.Load`. Divergent managed profile history is reconciled by generating the reviewed managed tree and committing it with `L` and `R` as parents; Git never chooses semantic content.

**Tech Stack:** Go 1.27-era repository baseline, existing `command.Runner`/`SystemRunner`, Git CLI without a shell, standard-library `archive/tar`, existing profile/policy/observation/workflow packages, Cobra CLI, Bubble Tea/Lip Gloss TUI, real temporary Git repositories in integration tests.

**Planning baseline:** approved spec was written against `main` `b01c18d2b5ce51f17ab97bfd66e4602d5838c306`. Before implementation, refresh `main` with `git pull --ff-only`, record the exact new base SHA in the execution ledger, and create an isolated worktree/branch. If main changed materially around profile Git, provider target identity, read cycles, Capture/Restore authority, or TUI screen registration, stop and reconcile this plan with the new code before implementation.

**Spec:** `docs/planning/specs/2026-10-03-sync-engine-v1-design.md`

**ADR:** `docs/adr/0027-semantic-sync-reconciliation-and-machine-local-cursors.md`

## Global constraints

- Phase 10 only. No Auto Follow, Auto Publish, Auto Reconcile, timers, daemons, systemd units, unattended permissions, persisted conflict-resolution database, or background scheduling.
- No profile schema bump solely for Sync Engine. Cursor state is machine-local, outside the portable profile.
- One current local branch and its one configured upstream tracking ref. No branch arbitration, peer-to-peer mode, multiple upstreams, force push, automatic rebase, or history rewrite.
- `sync inspect` may fetch. Inspection must not pull, merge, reset, change `HEAD`, modify profile files/index, or mutate the machine.
- A reviewed remote revision is always pinned to an exact SHA. Execution must not perform a generic `git pull` that can consume a newer unreviewed revision.
- Dirty worktree/index blocks Sync execution. Pre-existing staged changes remain a hard blocker.
- Divergent `B -> L` / `B -> R` history containing any committed non-Blueprint-managed path blocks semantic reconciliation.
- Git ancestry is transport evidence only. A generic textual Git merge must never choose Blueprint desired state.
- `Unmanaged`, `DesiredPresent`, and `DesiredAbsent` are distinct profile semantic states.
- Control-plane state (machine definitions, resource path overrides, portable/machine Capture/Restore policy, and other interpretation/authority inputs) is not silently reduced to runtime target state. Conflicting two-sided control-plane changes block unless explicitly supported by a reviewed control-plane resolver.
- Sync resolution choices choose intent only. Existing Capture/Restore compatibility, approval, exact fresh-plan comparison, Apply, and Verify remain authoritative.
- One logical Sync inspection owns one shared workflow read cycle. Do not implement Sync as repeated Overview/Status/preview calls that independently observe the machine.
- One local execution lock per canonical profile state root. Locking is local process coordination, not a substitute for Git/ref revalidation.
- Cursor advancement requires final published-revision reachability plus verified semantic settlement. Partial success never moves it forward.
- Reconstruction Assurance remains unchanged. Sync acceptance is separate and must not alter RA's same-runtime invariant.
- All network/Git/profile/machine mutations remain explicit interactive execution steps in v1.
- No new third-party dependency unless the human separately approves it.

## Delivery slices and review gates

Implement as four separately reviewable PRs. Do not start the next slice until the previous PR is reviewed and merged (or the human explicitly changes the gate).

1. **PR A — Reconciliation foundations:** exact revision primitives/materialization, machine-local cursor/lock, pure semantic reconciler. No Sync CLI/TUI and no profile mutation.
2. **PR B — Read-only Sync planning:** provider semantic adapters, historical Profile Views, workflow inspection/bootstrap planning, read-only CLI inspection. No Sync execution.
3. **PR C — Interactive execution:** pinned fast-forward, semantic reconciliation commit transaction, Capture/push/Restore orchestration, CLI resolution/apply. This is the first slice allowed to mutate profile/machine state.
4. **PR D — TUI and two-machine acceptance:** dedicated Sync workspace, transport-screen naming cleanup, end-to-end acceptance, docs/roadmap/manual checks.

Each PR must pass its focused tests, `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go build ./...` before review. Run existing shell/release/reconstruction helper tests when touched or at final integration. Hosted RA remains a merge/release assurance gate according to repository practice; Sync work must not modify its invariant.

---

# PR A — Reconciliation foundations

## Task 1: Add profile-scoped local state root, cursor store, and execution lock

**Files:**
- Create: `internal/machine/state_root.go`
- Create: `internal/machine/state_root_test.go`
- Modify: `internal/machine/binding.go`
- Create: `internal/syncengine/cursor.go`
- Create: `internal/syncengine/cursor_test.go`
- Create: `internal/syncengine/lock.go`
- Create: `internal/syncengine/lock_test.go`

**Interfaces:**

```go
// internal/machine
func ProfileStateDir(stateHome, profileRoot string) (string, error)

// internal/syncengine
const CursorFormatVersion = 1

type Cursor struct {
    Version           int
    Machine           string
    Revision          string
    Branch            string
    Upstream          string
    RemoteFingerprint string
}

type CursorStore struct {
    StateHome   string
    ProfileRoot string
}
func (s CursorStore) Load() (Cursor, bool, error)
func (s CursorStore) Save(Cursor) error
func (s CursorStore) Remove() error

type ProfileLock struct { /* private */ }
func AcquireProfileLock(stateHome, profileRoot string) (*ProfileLock, error)
func (l *ProfileLock) Close() error
```

- [ ] **Step 1: Extract/test the shared profile-state directory helper.**

Prove the existing machine-binding path remains byte-for-byte equivalent for canonical roots, including symlink resolution and state-home overrides. Preserve `0700` directory / `0600` file behavior.

```bash
go test ./internal/machine -run 'StateRoot|Binding' -count=1
```

Expected before implementation: new state-root tests FAIL. After extraction: PASS with existing binding tests unchanged.

- [ ] **Step 2: Write CursorStore tests before code.**

Cover missing file, round trip, malformed TOML, unsupported format version, missing/invalid SHA, empty machine/branch/upstream/fingerprint, atomic replacement, secure permissions, profile-root isolation, and no credential-bearing remote data in fixtures.

`Revision` must be a full lowercase Git object ID accepted by the repository's chosen hash format (current Git SHA-1 is 40 hex; do not make the parser silently accept arbitrary strings). If the repository later supports SHA-256 Git object IDs, widen deliberately with tests rather than guessing.

```bash
go test ./internal/syncengine -run 'Cursor' -count=1
```

Expected: FAIL because package/store does not exist; then PASS after minimal implementation.

- [ ] **Step 3: Add an exclusive local profile lock.**

Use an OS-level non-blocking/advisory file lock if the repository already has a standard helper; otherwise implement the smallest Linux user-session lock appropriate to Omarchy and document the platform assumption. Store the lock under the same profile-scoped state root. Never use “file exists” alone as a crash-unsafe lock.

Test two simultaneous acquisitions: first succeeds, second fails with a typed busy error; releasing permits a later acquisition. Cancellation/cleanup must not delete another process's live lock.

- [ ] **Step 4: Run race/security-focused tests.**

```bash
go test -race ./internal/machine ./internal/syncengine -run 'StateRoot|Binding|Cursor|Lock' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/machine/state_root.go internal/machine/state_root_test.go \
  internal/machine/binding.go internal/syncengine/cursor.go \
  internal/syncengine/cursor_test.go internal/syncengine/lock.go \
  internal/syncengine/lock_test.go
git commit -m "feat(sync): add machine-local reconciliation state"
```

## Task 2: Add exact Git revision, ancestry, history-path, and isolated materialization primitives

**Files:**
- Create: `internal/profilegit/revision.go`
- Create: `internal/profilegit/revision_test.go`
- Create: `internal/profilegit/archive.go`
- Create: `internal/profilegit/archive_test.go`
- Modify: `internal/profilegit/service.go`

**Interfaces:**

```go
type RevisionInfo struct {
    SHA      string
    Branch   string
    Upstream string
    OriginFingerprint string
}

func (s Service) ResolveCommit(ctx context.Context, ref string) (string, error)
func (s Service) CurrentRevisionInfo(ctx context.Context) (RevisionInfo, error)
func (s Service) UpstreamCommit(ctx context.Context) (string, error)
func (s Service) IsAncestor(ctx context.Context, base, tip string) (bool, error)
func (s Service) CommittedPaths(ctx context.Context, baseExclusive, tipInclusive string) ([]string, error)
func (s Service) MaterializeCommit(ctx context.Context, sha, dest string) error
```

- [ ] **Step 1: Write real-Git revision/ancestry tests.**

Use a temporary bare origin and two clones. Cover exact SHA resolution, missing object, annotated/non-commit ref rejection as appropriate, cursor ancestor true/false, local-ahead, remote-ahead, diverged history, detached HEAD, missing upstream, and origin changes.

Origin identity must be non-secret. Hash a canonical/sanitized remote identity rather than persisting credentials. Tests must prove `https://user:token@example/...` never appears in returned/persisted identity or error/display output.

- [ ] **Step 2: Test committed-path audit across the complete divergent range.**

`CommittedPaths` must inspect paths touched by commits in `(base, tip]`, not only endpoint tree differences, so a committed non-managed change that was later reverted still blocks semantic merge. Include rename/copy records and validate both source/destination names with `profile.IsManagedRepositoryPath` at the caller.

- [ ] **Step 3: Implement isolated materialization with `git archive` to a temporary tar file.**

Do **not** stream tar bytes through the string `Runner`. Run Git without a shell using an explicit output file:

```text
git -C <root> archive --format=tar --output=<temp.tar> <exact-sha>
```

Extract with standard-library `archive/tar` into a caller-owned empty destination. Reject absolute paths, `..` traversal, unsupported special file types, and archive entries escaping the destination. Preserve regular-file mode bits and symlinks without following them during extraction. Bound archive/extracted size to a documented sane limit derived from current profile expectations; fail closed rather than unboundedly materializing hostile history.

- [ ] **Step 4: Prove materialization has no checkout/index side effect.**

Before/after hashes/status of the real profile checkout must match exactly. Load the materialized directory with `profile.Load` in an integration test. Test old supported schema loads through existing loader behavior without writing migration results back to the commit/checkout.

```bash
go test ./internal/profilegit -run 'Revision|Ancestor|CommittedPaths|Materialize|Archive' -count=1
```

Expected: PASS after implementation.

- [ ] **Step 5: Commit.**

```bash
git add internal/profilegit/revision.go internal/profilegit/revision_test.go \
  internal/profilegit/archive.go internal/profilegit/archive_test.go \
  internal/profilegit/service.go
git commit -m "feat(profilegit): inspect exact profile revisions"
```

## Task 3: Implement the pure semantic reconciliation domain

**Files:**
- Create: `internal/syncengine/types.go`
- Create: `internal/syncengine/reconcile.go`
- Create: `internal/syncengine/reconcile_test.go`

**Core types:**

```go
type TargetKey struct { Category, Target string }

type StateKind string
const (
    StateUnmanaged StateKind = "unmanaged"
    StatePresent   StateKind = "present"
    StateAbsent    StateKind = "absent"
)

type State struct {
    Kind        StateKind
    Fingerprint string
    Summary     string // presentation-safe, non-authoritative
}

type HistoryClass string
// unchanged, local, remote, converged, conflict

type Resolution string
// none, keep-local, accept-remote, defer

type PlannedAction string
// none, capture, publish, restore

type Item struct { /* B/L/R/M + class/policy/resolution/action */ }
type Blocker struct { Code, Message string; Target *TargetKey }
type Plan struct { /* revisions, machine, git state, items, blockers */ }
```

Keep the pure domain independent of `profile`, providers, Git execution, Cobra, and TUI.

- [ ] **Step 1: Write exhaustive desired-history truth-table tests.**

For normalized states `B/L/R`, prove the five approved history outcomes, including `Unmanaged` vs `DesiredAbsent` and same-kind/different-fingerprint conflicts. Equality ignores `Summary` and uses `Kind + Fingerprint` only.

- [ ] **Step 2: Add machine-drift/action tests.**

Cover unchanged profile + machine drift => Capture candidate; remote-only + machine at base => Restore candidate; local profile ahead + machine matching local => Publish; local profile vs machine disagreement => explicit review state (`keep-machine`, `keep-profile`, `defer` at workflow resolution layer); Capture disabled; Restore disabled; identical local/remote desired result; already-converged live machine.

Do not let “disabled” erase the history classification. It may make a target intentionally inapplicable or a blocker according to the approved spec, but the semantic evidence stays in the Item.

- [ ] **Step 3: Add deterministic ordering and validation tests.**

Sort plans by stable category/target identity. Reject duplicate target keys, invalid states, invalid resolution for a non-conflict, and missing revision identity. No map iteration may affect JSON/human ordering.

```bash
go test ./internal/syncengine -run 'Reconcile|History|Machine|Plan' -count=1
go test -race ./internal/syncengine -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit.**

```bash
git add internal/syncengine/types.go internal/syncengine/reconcile.go \
  internal/syncengine/reconcile_test.go
git commit -m "feat(sync): classify semantic reconciliation state"
```

## Task 4: PR A verification and review gate

- [ ] Run:

```bash
go test ./internal/machine ./internal/profilegit ./internal/syncengine -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

- [ ] Confirm by test and `git status` that no Sync inspection primitive modifies the active profile checkout.
- [ ] Record exact implementation SHA, commands, and results in the execution ledger.
- [ ] Open/review PR A only after human publication authorization.
- [ ] **STOP. Do not begin PR B until PR A is reviewed and merged or the human explicitly changes this gate.**

---

# PR B — Read-only Sync planning

## Task 5: Define provider Sync projection/composition contracts

**Files:**
- Modify: `internal/workflow/providers.go`
- Create: `internal/workflow/sync_provider.go`
- Create: `internal/workflow/sync_provider_test.go`
- Modify/add focused files/tests under each captured provider:
  - `internal/providers/packages/`
  - `internal/providers/themes/`
  - `internal/providers/plugins/`
  - `internal/providers/config/`
  - `internal/providers/defaults/`
  - `internal/providers/shell/`
  - `internal/providers/hooks/`
  - `internal/providers/resources/`
  - `internal/providers/services/`

**Contract:** provider code owns semantic identity/value normalization. Workflow/syncengine must not infer provider meaning from serialized file paths.

Use two deliberately separate capabilities:

```go
type SyncDesiredProvider interface {
    ID() string
    SyncDesired(profile.Data) ([]syncengine.DesiredTarget, error)
}

type SyncComposeProvider interface {
    SyncDesiredProvider
    // Overlay exactly one target's desired state from src onto dst staging tree/data.
    // src may express Unmanaged, Present, or Absent.
    OverlaySyncTarget(ctx context.Context, dst SyncProfileBuilder, src SyncProfileView, target string) error
}
```

The exact helper structs may be adjusted to fit package dependency direction, but preserve these responsibilities: **project semantic facts** and **copy/clear one provider-owned target including every referenced managed artifact**. Do not put target-specific switch statements in `syncengine`.

- [ ] **Step 1: Add a target-identity contract test across all captured providers.**

Extend/reuse `internal/app/target_identity_contract_test.go` so Sync target keys match existing policy/inspection target identity. A target shown in Sync must be addressable by the same category/target identity used by Capture/Restore policy.

- [ ] **Step 2: Implement projection provider-by-provider with table tests.**

For each provider cover:

```text
uncaptured/unmanaged        -> Unmanaged
managed present             -> Present(stable fingerprint)
explicit tombstone/absence  -> Absent(stable provenance fingerprint) where supported
serialization reordering    -> same fingerprint
meaningful desired change   -> different fingerprint
volatile live-only fields   -> excluded
```

Fingerprints must be canonical semantic hashes, not `fmt.Sprintf` of maps. Reuse existing hashes/normalization where they already encode exact intent.

- [ ] **Step 3: Implement target overlay tests before overlay code.**

Build two profile fixtures with different state for one target. Starting from the remote fixture, overlay the local target and prove:

- only that provider target's semantic state changes;
- `profile.Load` of the staged tree succeeds;
- referenced config/hook/resource/service artifacts required by the chosen state are present;
- Unmanaged removes the target's desired state cleanly rather than creating an absence tombstone unless the source itself expresses DesiredAbsent;
- unrelated provider/target/control-plane bytes remain semantically unchanged.

- [ ] **Step 4: Run focused provider tests.**

```bash
go test ./internal/providers/... ./internal/workflow -run 'Sync|TargetIdentity' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit in small provider groups rather than one giant commit.**

Suggested sequence:

```text
feat(sync): project package and service desired state
feat(sync): project config hooks and shell desired state
feat(sync): project themes plugins defaults and resources
feat(sync): compose provider targets into profile trees
```

Each commit must leave all touched package tests green.

## Task 6: Build historical Profile Views and deterministic managed-tree composition

**Files:**
- Create: `internal/workflow/sync_profile_view.go`
- Create: `internal/workflow/sync_profile_view_test.go`
- Create: `internal/workflow/sync_compose.go`
- Create: `internal/workflow/sync_compose_test.go`

**Interfaces:**

```go
type SyncProfileView struct {
    Revision string
    Root     string       // isolated materialization root; private lifecycle
    Profile  profile.Data
    Targets  []syncengine.DesiredTarget
    Control  SyncControlState
}

func (s *Session) loadSyncProfileView(ctx context.Context, git profilegit.Service, sha string) (SyncProfileView, func(), error)
func composeSyncProfileTree(ctx context.Context, base, local, remote SyncProfileView, resolutions map[syncengine.TargetKey]syncengine.Resolution) (stagingRoot string, cleanup func(), err error)
```

- [ ] **Step 1: Write view lifecycle tests.**

Load B/L/R from real commits, verify exact SHA association, profile schema validation, deterministic target sorting, and cleanup. A load error in any captured provider fails the complete view; never publish a partial map.

- [ ] **Step 2: Define/control-plane semantic state.**

At minimum include selected-machine definition, machine resource-path overrides, portable policy, selected-machine policy/defaults, and any profile metadata that changes target interpretation/authority. Ignore timestamps that do not change desired semantics.

Tests:

- one-sided local control-plane change is carried into composed result;
- one-sided remote change stays from remote baseline;
- identical two-sided change converges;
- different two-sided change yields a blocker and composition refuses;
- changing selected machine identity invalidates cursor reuse/bootstrap assumptions.

- [ ] **Step 3: Compose from the exact remote tree, then overlay reviewed local semantic intent.**

Use the materialized `R` tree as the full managed-tree baseline so remote artifacts are preserved. For every local-only desired change and every conflict resolved Keep Local, invoke the provider overlay contract using `L` as source. Apply one-sided local control-plane state through a dedicated control-plane composition helper. Accept Remote leaves the `R` target unchanged. Deferred/unresolved items make composition non-executable.

After composition, load the complete staging tree with `profile.Load`, re-project semantic targets, and assert the result exactly matches the plan's resolved desired state. This round-trip is a required invariant, not optional debug validation.

- [ ] **Step 4: Add artifact/deletion regression tests.**

Include config content changes/deletes, hook deletes, resource metadata, service tombstones/activation metadata where applicable, and a target becoming Unmanaged. Prove stale files from the losing side do not accidentally remain authoritative.

```bash
go test ./internal/workflow ./internal/providers/... -run 'SyncProfile|SyncCompose|SyncControl' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/workflow/sync_profile_view.go internal/workflow/sync_profile_view_test.go \
  internal/workflow/sync_compose.go internal/workflow/sync_compose_test.go
git commit -m "feat(sync): compose semantic profile revisions"
```

## Task 7: Add workflow Sync inspection, bootstrap planning, and cursor validation

**Files:**
- Create: `internal/workflow/sync.go`
- Create: `internal/workflow/sync_test.go`
- Create: `internal/workflow/sync_bootstrap.go`
- Create: `internal/workflow/sync_bootstrap_test.go`
- Modify: `internal/workflow/read_cycle.go` / `read_snapshot.go` only if a narrow reusable live semantic projection hook is required.

**Interfaces:**

```go
type SyncInspectOptions struct {
    Fetch bool
}

func (s *Session) InspectSync(ctx context.Context, opts SyncInspectOptions) (syncengine.Plan, error)
func (s *Session) InspectSyncBootstrap(ctx context.Context, opts SyncInspectOptions) (SyncBootstrapPlan, error)
```

- [ ] **Step 1: Write a two-clone read-only inspection fixture.**

Create bare origin + clones A/B, profiles at common B, local committed change L, remote committed change R, and fake/read providers for live M. Assert the exact B/L/R SHA fields and target classifications.

- [ ] **Step 2: Fetch once, then pin R.**

`InspectSync(Fetch:true)` may call existing `profilegit.Fetch` once, then resolve the configured upstream SHA. All later historical reads use that exact SHA. No hidden network call in Profile View loading or reconciliation.

- [ ] **Step 3: Validate cursor lineage before target reconciliation.**

Block on missing cursor commit, cursor not ancestor of L, cursor not ancestor of R, changed branch/upstream/remote fingerprint, changed selected machine, detached/no-upstream state, dirty profile only as an execution blocker (inspection may still report it), invalid historical profile, or unsupported captured provider.

Do not substitute merge-base for a bad cursor.

- [ ] **Step 4: Acquire one live read cycle and derive M once.**

Bind existing providers to one `Session.BeginRead` lifecycle. Add only the minimal provider live-semantic projection needed by Sync. Reuse Services/Packages shared observations; no persistent cache. Add counters in tests proving one logical Sync inspection does not repeatedly rediscover Services/Packages.

- [ ] **Step 5: Implement bootstrap plans.**

Cover:

1. proven convergence at shared `L == R` => eligible automatic cursor initialization;
2. reconcile machine against fetched upstream;
3. adopt current machine intent => Capture/publish required before cursor;
4. cancel/no write;
5. no cursor write from inspection alone.

- [ ] **Step 6: Run read-only workflow tests and prove no mutation.**

```bash
go test ./internal/workflow ./internal/profilegit ./internal/syncengine -run 'Sync|Bootstrap' -count=1
go test -race ./internal/workflow -run 'Sync' -count=1
```

Record before/after `git status --porcelain=v2` and profile-tree hash in the real-Git fixture; they must match except cached remote refs after explicit Fetch.

- [ ] **Step 7: Commit.**

```bash
git add internal/workflow/sync.go internal/workflow/sync_test.go \
  internal/workflow/sync_bootstrap.go internal/workflow/sync_bootstrap_test.go
git commit -m "feat(sync): inspect multi-machine reconciliation"
```

## Task 8: Add read-only CLI Sync inspection

**Files:**
- Create: `internal/app/sync.go`
- Create: `internal/app/sync_test.go`
- Modify: `internal/app/app.go`
- Modify: `docs/cli.md`

**v1 read-only commands in PR B:**

```text
omarchy-blueprint sync inspect [--no-fetch] [--json]
omarchy-blueprint sync bootstrap inspect [--no-fetch] [--json]
```

Use naming consistent with the existing root command conventions; if the binary omits the `omarchy-` prefix in help text, follow current behavior. Do not add execution flags in this PR.

- [ ] **Step 1: Write Cobra/human/JSON tests.**

Human output groups blockers/conflicts/local/remote/converged items deterministically and prints exact short SHAs plus branch/upstream. JSON contains full SHAs and typed states but no credentials, raw secrets, or temp materialization paths.

- [ ] **Step 2: Prove `--no-fetch` is offline/local-ref only.**

A counting runner must see no `git fetch`; default inspect sees exactly the expected explicit fetch. Status/Overview behavior remains unchanged and does not gain implicit network access.

- [ ] **Step 3: Commit.**

```bash
git add internal/app/sync.go internal/app/sync_test.go internal/app/app.go docs/cli.md
git commit -m "feat(cli): inspect semantic sync plans"
```

## Task 9: PR B verification and review gate

Run:

```bash
go test ./internal/providers/... ./internal/syncengine ./internal/profilegit ./internal/workflow ./internal/app -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

Acceptance evidence for this PR must show two clones producing local-only, remote-only, same-result, and conflict classifications while the active checkout and machine remain untouched.

**STOP after PR B review publication. Do not begin mutation/execution PR C until PR B is reviewed and merged.**

---

# PR C — Interactive execution

## Task 10: Add pinned fast-forward and semantic merge transaction primitives

**Files:**
- Create: `internal/profilegit/reconcile.go`
- Create: `internal/profilegit/reconcile_test.go`
- Modify: `internal/profilegit/commit.go` only to factor shared staged-path validation; preserve existing public behavior.
- Modify: `internal/profilegit/sync.go` only if shared exact-ref helpers belong there; do not weaken generic Pull.

**Interfaces:**

```go
func (s Service) FastForwardTo(ctx context.Context, reviewedSHA string) (Result, error)

type SemanticMergeRequest struct {
    BaseSHA       string
    LocalSHA      string
    RemoteSHA     string
    StagingRoot   string // complete validated Blueprint-managed tree
    Message       string
}
func (s Service) CommitSemanticMerge(ctx context.Context, req SemanticMergeRequest) (Result, error)
```

- [ ] **Step 1: Test pinned fast-forward.**

Require clean entire repo, current `HEAD` exactly the reviewed local SHA supplied by workflow preconditions, and reviewed remote SHA already present locally and descendant of `HEAD`. Revalidate the configured upstream ref equals the reviewed SHA before mutation. Execute a no-network fast-forward (for example `git merge --ff-only <reviewedSHA>`). Assert no fetch/pull command occurs.

Race test: move upstream after review without fetching locally; execution must still act only on reviewed cached SHA and later publication/reinspection handles the new upstream. If the local cached upstream ref itself changed from the reviewed SHA, fail before mutation.

- [ ] **Step 2: Test divergent committed-path blocker.**

Before semantic merge, audit all committed paths in `B..L` and `B..R`. Any source/destination path outside `profile.IsManagedRepositoryPath` returns a typed blocker. Include a non-managed change that was committed and later reverted; it must still block.

- [ ] **Step 3: Implement semantic merge as ancestry-only Git merge plus complete managed-tree replacement.**

Safe transaction:

1. verify clean repo and exact `HEAD == L`;
2. verify `B` ancestor of both `L` and `R` and `L/R` truly divergent;
3. verify divergent committed paths are all Blueprint-managed;
4. validate `StagingRoot` with `profile.Load` before touching the checkout;
5. start `git merge --no-commit --no-ff -s ours <R>` (or an equivalent plumbing sequence) so Git records `R` as the second parent but does not choose remote file content;
6. replace **all** Blueprint-managed top-level paths in the checkout with the complete validated staging tree, including deletions;
7. `git add -A -- <managed paths>` and verify every staged path is managed;
8. validate the checkout with `profile.Load` and re-project/compare the expected semantic tree if the caller supplies its fingerprint;
9. commit the merge even when the resulting tree equals one parent;
10. verify the new commit has exactly parents `L` and `R` and the committed managed tree equals the staged tree.

On every pre-commit failure after merge start, run `git merge --abort` and verify `HEAD`, index, and working tree return to the exact pre-transaction state. If rollback itself fails, return a compound high-severity error and never continue to Capture/Push/Restore.

Do not use normal Git merge output as the final tree. The `ours` strategy/plumbing exists only to establish ancestry before Blueprint writes the semantic tree.

- [ ] **Step 4: Add crash/resume blocker behavior.**

A repository already in merge/rebase/cherry-pick state at Sync execution entry must block with repair guidance; do not attempt to adopt or auto-complete another Git transaction.

```bash
go test ./internal/profilegit -run 'FastForwardTo|SemanticMerge|CommittedPath|Rollback' -count=1
```

- [ ] **Step 5: Commit.**

```bash
git add internal/profilegit/reconcile.go internal/profilegit/reconcile_test.go \
  internal/profilegit/commit.go internal/profilegit/sync.go
git commit -m "feat(profilegit): reconcile reviewed profile history"
```

## Task 11: Implement authoritative Sync execution orchestration

**Files:**
- Create: `internal/workflow/sync_execute.go`
- Create: `internal/workflow/sync_execute_test.go`
- Modify: `internal/workflow/capture.go` only for narrow reusable selected-target transaction entry points if needed.
- Modify: `internal/workflow/restore.go` only for narrow reusable approved/fresh plan entry points if needed.

**Interfaces:**

```go
type SyncExecutionRequest struct {
    ReviewedPlan syncengine.Plan
    Resolutions  []syncengine.TargetResolution
    Bootstrap    *SyncBootstrapChoice
}

type SyncExecutionResult struct {
    FinalRevision string
    Captured      []syncengine.TargetKey
    Restored      []syncengine.TargetKey
    Published     bool
    CursorAdvanced bool
}

func (s *Session) ExecuteSync(ctx context.Context, req SyncExecutionRequest) (SyncExecutionResult, error)
```

- [ ] **Step 1: Write a mutation-order ledger test before implementation.**

Using fakes around profilegit/Capture/Restore authority, assert exact order for mixed reconciliation:

```text
lock
revalidate cursor/machine/L/R/clean Git
compose/validate desired tree if divergent
pinned fast-forward OR semantic merge
reload profile
fresh Capture selected Keep-Machine/local-live targets
managed commit if Capture changed profile
push final desired revision
confirm upstream reachability/publication
fresh Restore plan for Accept-Remote targets
normal Restore approval/equality/apply/verify path
fresh settlement verification
save cursor
unlock
```

There must be no Restore before successful push of the final desired revision.

- [ ] **Step 2: Revalidate the reviewed plan before first mutation.**

Acquire local profile lock, then reload cursor/binding/profilegit status. Reject changed cursor, machine binding, `HEAD`, branch/upstream/remote identity, dirty repo, changed cached upstream SHA, or now-invalid profile. Reinspection is required; do not silently rewrite the reviewed plan.

- [ ] **Step 3: Handle execution shapes explicitly.**

Cover at least:

- remote-only: pinned fast-forward -> fresh Restore;
- local-profile-only, machine already matches: Push -> verify reachability -> cursor;
- machine-only local drift: Capture -> Commit -> Push -> cursor if no Restore remains;
- remote + machine-local accepted local intent: fast-forward R -> Capture local targets -> Commit -> Push -> fresh Restore remote targets;
- divergent L/R managed history: compose semantic tree -> semantic merge commit -> optional fresh Capture -> Push -> fresh Restore;
- same-result L/R: no semantic merge needed if ancestry permits normal transport; otherwise create ancestry reconciliation only when required to publish a single shared history;
- Keep Profile when machine disagrees with committed local profile: no recapture; Restore machine toward the chosen profile intent through normal fresh planning;
- Keep Machine: fresh Capture that target after profile history is safely based on the reviewed remote/shared state;
- Defer/unresolved: execution refuses cursor advancement and does not claim complete reconciliation.

- [ ] **Step 4: Reuse existing Capture authority instead of writing profile bytes from M directly.**

If current Capture APIs cannot target the selected Sync target set without broad capture, factor the smallest reusable selected-target transaction around existing review/fingerprint/staging/rollback logic. Preserve current CLI/TUI Capture behavior and tests. Never bypass post-stage fingerprint recheck.

- [ ] **Step 5: Reuse existing Restore authority with fresh planning.**

After profile mutation/push, reload the profile and fresh-observe/replan. Apply only the reviewed resolution's applicable Restore targets through existing compatibility, Safe/Exact, activation, approval equality, Apply, and Verify behavior. The earlier Sync Plan is only a selection constraint; it is not the RestorePlan to execute.

- [ ] **Step 6: Implement publication and cursor advancement rules.**

After push, fetch/inspect only as necessary to prove the final commit is reachable at the configured upstream without consuming a newer revision as execution authority. Cursor records the exact final settled commit only after fresh semantic verification of all applicable targets through that revision. If upstream advanced beyond the just-pushed commit concurrently but still contains it, the cursor may record the final commit actually settled, not the unreviewed newer tip; the next Sync run handles later changes.

- [ ] **Step 7: Add partial-failure matrix tests.**

At minimum:

| Failure point | Expected invariant |
| --- | --- |
| stale input before mutation | no profile/machine change, cursor unchanged |
| pinned FF failure | no machine change, cursor unchanged |
| semantic merge composition failure | checkout untouched |
| semantic merge post-start failure | merge aborted, original L restored |
| Capture failure | no push/Restore/cursor; existing Capture rollback holds |
| push rejected/upstream raced | no Restore/cursor; committed local result remains reviewable |
| Restore planning compatibility blocker | published profile may remain; machine unchanged by Restore; cursor unchanged |
| Restore Apply/Verify failure | published profile remains; cursor unchanged; next Sync shows remaining drift |
| cursor save failure after verified settlement | report explicit local-state error; never pretend cursor advanced |

- [ ] **Step 8: Run workflow mutation tests under race detector.**

```bash
go test ./internal/workflow ./internal/profilegit ./internal/syncengine -run 'SyncExecute|SemanticMerge|FastForward' -count=1
go test -race ./internal/workflow ./internal/profilegit -run 'Sync' -count=1
```

- [ ] **Step 9: Commit.**

```bash
git add internal/workflow/sync_execute.go internal/workflow/sync_execute_test.go \
  internal/workflow/capture.go internal/workflow/restore.go
git commit -m "feat(sync): execute reviewed reconciliation safely"
```

## Task 12: Add interactive CLI resolution and apply

**Files:**
- Modify: `internal/app/sync.go`
- Modify: `internal/app/sync_test.go`
- Modify: `docs/cli.md`

Avoid a durable `sync resolve` state database. The CLI must either:

- accept explicit one-shot resolutions on `sync apply` (repeatable `--keep-local category:target`, `--accept-remote category:target`, `--defer category:target`), or
- carry a signed/validated ephemeral plan artifact whose exact B/L/R/machine identity is revalidated and never becomes durable reconciliation truth.

Prefer the one-shot form unless existing CLI ergonomics strongly favor a temporary artifact.

- [ ] **Step 1: Write parsing/ambiguity tests.**

Reject duplicate/conflicting resolutions, unknown target IDs, resolution of non-conflicts when not meaningful, and application when the inspected plan has changed.

- [ ] **Step 2: Add explicit confirmation summary.**

Before execution human mode must show the reviewed exact revisions and ordered stages: profile history action, Capture target count, commit/push, Restore target count, deferred count, blockers. Reuse existing noninteractive/JSON confirmation conventions; do not invent unattended approval.

- [ ] **Step 3: Add bootstrap execution choices.**

Expose explicit reconcile-upstream/adopt-machine bootstrap commands/options. “Mark this HEAD reconciled” is not a command unless convergence has already been proven by `InspectSyncBootstrap`.

- [ ] **Step 4: Test JSON output and failure exit semantics.**

Never print credentials, temp paths, raw sensitive file content, or unbounded diffs.

```bash
go test ./internal/app -run 'Sync' -count=1
```

- [ ] **Step 5: Commit.**

```bash
git add internal/app/sync.go internal/app/sync_test.go docs/cli.md
git commit -m "feat(cli): resolve and apply semantic sync plans"
```

## Task 13: PR C verification and review gate

Run full Go tests/race/vet/build plus existing helper suites. Add a real temporary Git two-clone execution test that exercises a remote-only follow, local-only publish, and divergent managed semantic reconciliation without touching the developer profile.

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

Also run repository-standard shell/helper tests documented in the current Makefile/docs. Record exact commands/results; do not guess historical counts.

**STOP. PR C is the first mutation-capable Sync code and requires explicit review/merge before TUI/acceptance PR D.**

---

# PR D — TUI and two-machine acceptance

## Task 14: Add a dedicated semantic Sync TUI workspace

**Files:**
- Keep/modify existing: `internal/tui/screens/sync.go`, `internal/tui/screens/sync_test.go` only for the existing **Profile Git transport** screen behavior/naming.
- Create: `internal/tui/screens/reconcile.go`
- Create: `internal/tui/screens/reconcile_test.go`
- Modify: `internal/tui/screen_registry.go`
- Modify: `internal/tui/screen_adapters.go`
- Modify: `internal/tui/model.go`, `events.go`, `messages.go`, `actions.go`, `modals.go` only as required by the existing architecture.
- Modify: `docs/tui.md`

There is already a TUI `screens/sync.go` for Profile Git transport. Do not overload it with semantic reconciliation. Rename its **user-facing label** to `Profile Git`/`Git` (choose the wording consistent with current navigation), and make the new workspace the user-facing `Sync` screen. File renaming is optional; behavior/mental model separation is required.

- [ ] **Step 1: Write screen-registry/navigation tests.**

The TUI must expose distinct destinations for Profile Git transport and semantic Sync. No duplicate labels/keys.

- [ ] **Step 2: Implement read-only Sync overview first.**

Show machine, B/L/R short SHAs, branch/upstream, counts and grouped items: blockers, conflicts, remote, local, converged/deferred. Opening the screen runs one asynchronous inspection and retains the TUI's fast first-frame behavior; do not block model construction on fetch/observation.

- [ ] **Step 3: Add semantic conflict detail/resolution UI.**

Conflict modal/detail supports Keep Local, Accept Remote, Defer, and provider-rich details where existing inspection/diff APIs safely expose them. Resolution changes only TUI session state and recomputes presentation; it performs no profile/machine mutation.

- [ ] **Step 4: Add execution handoff using existing approval patterns.**

`Execute reconciliation` displays the stage summary, then calls workflow `ExecuteSync`. Any Restore review/compatibility details that existing TUI Restore requires must still be presented; do not suppress a second authority confirmation merely because the user resolved Sync intent.

After execution, always refresh with a brand-new Sync inspection. Never paint the old plan as success.

- [ ] **Step 5: Add cancellation/error tests.**

Fetch cancellation, lost network, stale plan, busy profile lock, compatibility blocker, push rejection, Restore failure, and cursor-save failure must return to an understandable Sync screen with no false “synced” state.

```bash
go test ./internal/tui/... -run 'Sync|Reconcile|ProfileGit' -count=1
```

- [ ] **Step 6: Commit.**

```bash
git add internal/tui docs/tui.md
git commit -m "feat(tui): add semantic sync workspace"
```

## Task 15: Add focused two-machine Sync acceptance coverage

**Files:**
- Create: `test/sync/run.sh`
- Create: `test/sync/lib/common.sh` (only if helpers warrant a split)
- Create: `test/sync/README.md`
- Create fixtures/scripts under `test/sync/` as required.
- Modify: `docs/manual-test-checklist.md`
- Modify: `ROADMAP.md`

Do **not** refactor `test/reconstruction/` into a generic VM framework for this task. RA remains unchanged.

Start with a deterministic disposable-host acceptance harness using two isolated HOME/state/profile roots and a shared bare Git remote. Use real `git` and the real Blueprint binary; provider/system mutation may use controlled fixtures/fakes where exercising real Omarchy state would make the test destructive. The acceptance boundary is semantic engine + Git + cursor + CLI transaction behavior, not a second full OS reconstruction suite.

Required scenarios:

1. A and B bootstrap converged at X; both cursors X.
2. A changes/captures package-like target P and publishes Y; B inspects remote-only P, follows through Restore fixture, verifies, cursor Y.
3. A changes target C locally; B changes same C differently and publishes; A sees conflict, Keep Local, creates semantic reconciliation commit, publishes; B follows resolved intent.
4. Disjoint A/B target changes create a semantic reconciliation commit with both intents and two parents.
5. Deferred conflict performs no false cursor advancement.
6. Dirty profile blocks execution.
7. Upstream changes after review causes stale/push-safe failure; no unreviewed Restore.
8. Divergent committed non-managed path blocks.
9. Restore failure after successful publication leaves cursor behind and next inspect shows remaining machine drift.
10. Switching machine binding/upstream/remote identity invalidates cursor reuse.

- [ ] **Step 1: Make each scenario self-contained and cleanup-safe.**

No test touches the developer's real `$HOME`, state directory, active profile, system user services, package database, or public remote.

- [ ] **Step 2: Add acceptance to the appropriate CI/helper layer only after local reliability.**

Do not create a new VM workflow merely because Sync exists. If the existing CI can run `test/sync/run.sh` safely on Ubuntu/Arch-neutral fixtures, wire it there in the smallest existing job. If it requires an Omarchy VM for meaningful coverage, document the gap and propose a separate review rather than silently expanding RA.

- [ ] **Step 3: Update roadmap accurately.**

Mark Phase 10 complete only after the engine, CLI/TUI, execution path, and acceptance tests are merged. Keep Phase 11 Auto Sync explicitly pending.

- [ ] **Step 4: Commit.**

```bash
git add test/sync docs/manual-test-checklist.md ROADMAP.md
git commit -m "test(sync): assure two-machine semantic reconciliation"
```

## Task 16: Final documentation, invariants, and verification

**Files:**
- Modify if needed: `README.md`
- Modify: `docs/cli.md`
- Modify: `docs/tui.md`
- Ensure ADR/spec/plan links are correct.

- [ ] **Step 1: Document user mental model.**

Keep the distinction crisp:

```text
Profile Git = transport/history operations
Sync        = semantic B/L/R/M reconciliation
Auto Sync   = future unattended use of Sync decisions
```

Document cursor meaning and first-run bootstrap. Warn that deferred conflicts intentionally keep the cursor behind.

- [ ] **Step 2: Run complete verification from a clean worktree.**

At minimum:

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
bash test/sync/run.sh
```

Also run current repository release-helper, reconstruction-helper, shell syntax, and packaging checks according to their present documented entry points. Do not alter RA to make Sync tests pass.

- [ ] **Step 3: Manual TUI acceptance.**

Using disposable profiles/remotes, verify:

- initial TUI renders promptly before Sync fetch finishes;
- Profile Git and Sync are clearly distinct;
- conflicts show semantic target IDs and meaningful local/remote details;
- Keep Local / Accept Remote / Defer survive in-session navigation but not app restart;
- execution summary matches actual mutation stages;
- stale plan causes reinspection, not execution;
- failed Restore/push never displays settled cursor;
- successful final run refreshes to no unresolved applicable work.

- [ ] **Step 4: Security/safety audit before final PR approval.**

Search production code/tests for accidental credential/raw URL persistence, temp-path leakage in JSON, shell-invoked Git, `--force`, `rebase`, `reset --hard`, implicit fetch in ordinary Status/Overview, bypassed Capture/Restore approval, or cursor writes before Verify.

Suggested checks (adapt exact grep tool to repo environment):

```bash
git grep -nE 'push .*--force|reset --hard|git rebase|sh -c.*git' -- '*.go' '*.sh'
git grep -n 'CursorStore.*Save\|Save(.*Cursor' -- '*.go'
```

Manually inspect every cursor-save call; it must be after settlement verification or proven bootstrap convergence.

- [ ] **Step 5: Final review gate.**

Record exact final SHA and verification evidence. Request code review with findings-first severity. No tag, GitHub Release, AUR publication, systemd timer, or Auto Sync work is authorized by completion of Phase 10.

---

## Implementation notes for worker agents

- Prefer pure domain helpers and narrow provider adapters over adding Sync conditionals to existing Capture/Restore implementations.
- Keep files focused. If `sync.go` grows into inspection + composition + execution + rendering, split it rather than creating a second monolith.
- Use real temporary Git repositories for ancestry/merge/transport tests; use fakes only to isolate provider/live-system behavior.
- When a test exposes ambiguity in target identity or semantic fingerprinting, stop and resolve the provider contract rather than encoding an unstable workaround in Sync.
- Preserve exact error provenance. A cancellation caused by a higher-level stale/failure condition must not replace the initiating meaningful error.
- Treat profile materialization and staged semantic merge trees as untrusted filesystem inputs: traversal, symlink, size, cleanup, and permission behavior need explicit tests.
- Do not optimize Sync observation beyond using the existing read-cycle sharing/bounded scheduling until correctness and acceptance are established.
- Do not add Phase 11 configuration fields “for later.” Automation permission gets its own reviewed design.

## Done definition

Phase 10 is complete only when:

- a machine can establish or explicitly bootstrap a trustworthy cursor;
- Sync inspection deterministically compares B/L/R/M at semantic target granularity;
- committed local profile intent, live machine drift, and remote desired intent remain distinct;
- conflicts are resolvable as Keep Local / Accept Remote / Defer in CLI and TUI;
- reviewed remote revisions are pinned and stale plans fail closed;
- divergent Blueprint-managed history can produce a semantic two-parent reconciliation commit without generic textual merge authority;
- divergent committed non-managed history blocks;
- mixed reconciliation publishes final desired state before following it on the machine;
- Capture and Restore retain fresh authority, compatibility, Apply, and Verify boundaries;
- cursor advancement occurs only after final published-revision reachability and verified semantic settlement;
- two-machine acceptance covers local-only, remote-only, conflict, disjoint merge, stale race, defer, and partial failure;
- Profile Git transport remains distinct from Sync semantics;
- RA remains unchanged;
- no unattended Auto Sync behavior has been introduced.
