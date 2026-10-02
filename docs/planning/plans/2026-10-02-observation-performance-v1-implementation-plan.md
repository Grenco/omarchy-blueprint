# Observation Performance v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for inline execution, or superpowers:subagent-driven-development only if the human selects delegation. Steps use checkbox (`- [ ]`) syntax for tracking. Do not execute this plan before explicit human approval of this plan and its spec.

**Goal:** Reduce read-only discovery subprocesses and repeated Services/Packages observations while preserving fresh Capture/Restore mutation authority.

**Architecture:** First implement bounded identity-aware Services batching. Then introduce an explicitly owned read cycle with provider-owned typed observation slots and narrow read views; keep authoritative Capture, Restore revalidation, and Verify separate and uncached. Re-measure before deciding whether to add a provider scheduler.

**Tech Stack:** Existing Go module (Go 1.25 floor; CI 1.25/1.26), Cobra, Bubble Tea, existing command Runner/SystemRunner, systemctl user-manager inspection, pacman/Mise probes, project-native synchronization. No new dependency or schema.

**Spec:** `docs/planning/specs/2026-10-02-observation-performance-v1-design.md`

**ADR:** `docs/adr/0026-shared-read-observations-and-fresh-mutation-authority.md`

**Approval/execution:** Approved on 2026-10-02. Tasks 1–3 / PR A implemented at `7def693779ce1cdad61f8b058e8e390dc54d4aaa`, pending human review/merge and hosted Reconstruction Assurance. The human subsequently authorized pushing A, committing/publishing these planning documents and opening a PR. See the spec's PR A evidence and the isolated worktree's ignored execution ledger. Tasks 4–10 have not begun.

**PR B execution update:** PR A #61 was approved/merged; B starts from current
main `bf6b816b75cffc79a30cdde7a7272ea902432745`. Tasks 4–9 are implemented and
locally verified, with [before/after evidence](../specs/2026-10-02-observation-performance-v1-pr-b-results.md).
Task 10 remains conditional and unstarted. PR B publication and merge remain
human gates; the earlier paragraph records the approval state at PR A handoff.

**Planning baseline:** `main`, `4b1617161eff583e01085ef85b270da04829ec48`, checked with successful `git pull --ff-only` on 2026-10-02. Execution starts from then-current main in an isolated worktree/branch after approval. Reconcile file locations and parallel changes before editing; do not overwrite user work.

## Global Constraints

- Services batch size: 64 eligible named operands; process count `2 + ceil(eligible / 64)`.
- Naked templates remain catalogue-only; output bound remains 4 MiB per command.
- Returned record position never establishes identity; map Id and Names independently of order.
- Preserve normalized Services state, ownership, policy, compatibility, approval, fingerprints, rollback, executor preconditions, and operation ordering.
- At most one Services inventory and one Packages Detect per participating logical read cycle.
- Typed defensive copies preserve live-only fields and concrete Mise values; no JSON copying.
- No package globals/TTL/persisted cache, watchers, subscriptions, daemon, scheduling, or auto-sync.
- ReadCycle has no mutation methods; mutation/Verify paths never bind to a read cycle.
- Keep PlanRestoreWithContext fresh for the CLI authority boundary.
- Resolve observational Services roots locally; prepared Capture state stays serialized and separate.
- No production concurrency or progressive TUI before A/B measurements justify a separately reviewed choice.
- Planning documents stayed uncommitted until separately authorized. Implementation approval alone does not authorize an automatic push/PR/merge; the human has now authorized A's push/PR and publication of its planning documents, but not merge or stacking B.

## Review Focus

1. Reordered/duplicate alias records with catalogue-state fallback must retain canonical normalization, including empty UnitFileState (Task 2).
2. Clone operations must preserve installed/semantic/Mise scratch maps excluded from JSON and nested concrete Mise values (Tasks 5 and 6).
3. A slot finishing after cancellation/Close must neither publish nor outlive its owner; a canceled waiter must not cancel other waiters (Task 4).
4. Profile/policy/binding edits and Reload during a read must not change that cycle or race its snapshot (Task 7).
5. CLI post-approval reinspection and Capture's post-stage recheck must stay fresh even when a read cycle remains alive (Task 8).

---

## PR decomposition and merge gates

| PR | Tasks | Deliverable | Next gate |
| --- | --- | --- | --- |
| A | 1–3 | Validated Services chunking and structural tests | Review/merge, then remeasure |
| B | 4–9 | Read-cycle lifecycle, Services/Packages integration, authority tests, diagnostics | Review/merge, then full profiling suite |
| C, conditional | 10 | Measured provider scheduling only if justified | Separate design/plan choice before implementation |

Each PR is independently tested and reviewed. Do not begin B before A is reviewed/merged, or C before B's measurements and human decision, unless stacking is explicitly authorized. No PR is created by this planning task.

## File and interface map

New files have one responsibility:

- `internal/providers/services/systemd_batch.go`: batch record parsing and identity mapping.
- `internal/providers/services/systemd_batch_test.go`: normalization/parser/process-count invariants.
- `internal/observation/cycle.go`, `slot.go`, and corresponding `_test.go`: owned lifecycle and typed singleflight; no provider imports.
- `internal/providers/services/observation.go`, `read.go`, and corresponding `_test.go`: immutable source/inventory and read projections; exclude prepared transaction state.
- `internal/providers/packages/observation.go`, `observation_test.go`: complete typed Packages snapshot/copying.
- `internal/app/packages_read.go`, `packages_read_test.go`: bind the existing Packages application adapter to a slot.
- `internal/app/read_providers.go`, `read_providers_test.go`: narrow adapters preserving specialized read capabilities without exposing Capture/Verify/transactions.
- `internal/profile/clone.go`, `clone_test.go`: typed copies of profile data and nested observation-bearing package values, preserving types/nil semantics.
- `internal/workflow/read_cycle.go`, `read_snapshot.go`, `read_restore.go`, and corresponding `_test.go`: cycle ownership, pure desired-state loading, preview orchestration.
- `internal/workflow/read_authority_test.go`: release-blocking read-A/mutate-B cases.
- `internal/diagnostics/observation.go`, `observation_test.go`: optional stderr counters/timing, without payloads.
- `internal/command/diagnostic_test.go`: command diagnostics/redaction/disabled-path coverage.
- `internal/workflow/observation_performance_test.go`, `internal/app/observation_performance_test.go`: opt-in read-only/live benchmark harnesses; never run apply/Capture against the host.

Existing files to touch are pinned in each task. Do not split unrelated large files or rewrite provider models wholesale.

### Read interfaces

Add `ReadProvider`, `ReadRestoreProvider`, and `ReadCycleBinder` exactly as defined in the spec to `internal/workflow/providers.go`. Provider embeds ReadProvider and keeps Capture; RestoreProvider keeps Plan/Verify. Optional category/scan/Git/resolver/validator capabilities are preserved by specialized read adapters, not silently removed.

PR B review addition: `read_snapshot.go` defines descriptive `ReadSnapshot` and
optional `ReadSnapshotBinder`; Resources receives frozen machine interpretation
without a live-facts cache. Snapshot/EffectivePolicy read methods let TUI/CLI
consumers render coherent result metadata without Session publication. Fresh
Capture desired-state reload and TUI profile-edit entry reload are required by
the same authority separation; see the spec and PR B results for regressions.

Add `RestorePreview { Plan model.RestorePlan; Options policy.RestoreOptions; Profile profile.Data; Machine machine.Selection }` in `read_restore.go`. Add the spec's BeginRead/Close/read methods. Add `Session.PreviewRestore(context.Context, string, *policy.RestoreOptions) (RestorePreview, error)`; Session.PlanRestore returns that preview's plan. Session.PlanRestoreWithContext keeps its existing signature and fresh-authority semantics.

`ReadCycle` owns `*observation.Cycle`, a private desired-state snapshot, `[]ReadProvider` in established order, a lazy `omarchy.Info` metadata slot for preview environment, and the read finalizer. It never owns authoritative providers as publicly accessible capabilities. Each provider view owns its typed slot; there is no global key registry or provider-ID cache map.

## Task 1: Reconfirm live batching evidence on the execution base (PR A)

**Files:** Inspect `internal/providers/services/systemd.go`, `model.go`, `systemd_test.go`; update the evidence section of the spec. Temporary probes stay outside the worktree and uncommitted.

**Interfaces:** Consume unchanged `Systemctl.InspectUserUnits`, `normalizeObservedUnit`, and `userUnitProperties`. Produce reproducible evidence for the proposed Id/Names parser and chunk size 64, not a product API.

- [ ] Refresh main, record SHA/status/runtime, and create the isolated worktree through the worktree skill. Confirm read-only systemd access. Do not install tooling if unavailable; use wait4/resource counters where needed.
- [ ] Reproduce at least five serial inventory samples and five samples for all operands/16/64/128. Record listings, union, templates, canonical/alias counts, command counts, wall time, and output sizes without retaining command payloads in published evidence.
- [ ] Compare full normalized inventories against the unchanged implementation; request Names, reverse decoded records deliberately, and test forward/reversed operands. Report volatile differences separately and establish equality on stable evidence. Stop if identity cannot be resolved or normalization diverges.
- [ ] Confirm the selected bound of 64 remains close to larger chunks and record the output/argument bounds. If current measurements warrant a different size, amend spec/plan for human review before replacement.

Expected: every stable comparison equal; no positional mapping. The planning probe already passed 20 Id/Names comparisons on the recorded baseline; it observed unordered Requires lists, which normalized duplicate comparison must accept.

## Task 2: Implement bounded Services inventory with pinned semantics (PR A)

**Files:** Create `systemd_batch.go`, `systemd_batch_test.go`; modify `systemd.go`, `systemd_test.go`. Keep `model.go` observation semantics unchanged.

**Interfaces:** `parseUnitRecords(output string) ([]unitRecord, error)`; `mapUnitRecords(requested []string, records []unitRecord) (map[string]map[string]string, error)`; private `unitRecord` contains parsed properties and canonical/alias identity evidence. Existing `InspectUserUnits(context.Context) ([]ObservedUnit, error)` remains public and unchanged. Existing normalization/catalogue helpers remain shared.

- [ ] Write `TestParseUnitRecordsBoundariesAndIdentity`: multiple records, blank delimiters, trailing whitespace, missing/empty/duplicate Id, malformed records, escaped systemd name tokens; fail safely on unusable identity.
- [ ] Write `TestMapUnitRecordsReorderedAliases`: reordered records, canonical Id differing from operand, duplicate aliases represented by one or several records, unexpected canonical record, alias collision, missing operand. Assert operand-state fallback is independent of output order.
- [ ] Write `TestInspectUserUnitsBatchesPreserveNormalization`: catalogue-only templates, loaded-only identities, masked units, generated/runtime/transient, linked SourcePath, drop-ins, all dependency/trigger properties, instances, deterministic final sorting, empty UnitFileState with alias/catalogue fallback, and nil/empty slice parity.
- [ ] Write `TestDuplicateAliasRecordsCompareNormalizedFacts`: dependency/Names token order differences are accepted; conflicting normalized duplicate facts within/across chunks fail; sorted catalogue fallback preserves the old canonical winner.
- [ ] Write `TestInspectUserUnitsCommandCountBounded`: 512 eligible units plus naked templates yield exactly ten calls (two catalogues, eight shows), each show has at most 64 eligible operands. Test 0/1/63/64/65/513 boundaries. The test must fail on 2+N regression, even if commands are fast.
- [ ] Write `TestInspectUserUnitsBatchFailureIsFatal` and `TestInspectUserUnitsBatchCancellation`: failed/partial batch, output limit, canceled Runner call, no later batches, no partial successful inventory.
- [ ] Run `go test ./internal/providers/services -run 'Test(ParseUnitRecords|MapUnitRecords|InspectUserUnits|DuplicateAlias)' -count=1`; verify new cases fail for the intended missing batch behavior.
- [ ] Implement record validation/mapping, add Names to the property request, batch sorted eligible operands by 64, coalesce/validate identity evidence, normalize in original sorted operand order, retain template catalogue rows/final sorting. Keep the 4 MiB per-command limit and fail without per-unit fallback.
- [ ] Run all Services tests and rerun Task 1's live equality comparison against the actual production parser. Existing normalization/ownership assertions must remain unchanged; update fake command expectations only where batching changes arguments/counts.
- [ ] Commit the focused production change in the approved implementation branch; do not push/create a PR without the separately authorized external workflow.

## Task 3: Verify and measure PR A; stop at its review gate

**Files:** Create opt-in live inventory benchmarks in `internal/providers/services/observation_performance_test.go`; record evidence in the spec. No cycle/scheduler edits.

- [ ] Run the full verification matrix below and relevant race coverage. Preserve Services reconstruction tests and require the existing Reconstruction Assurance workflow before merge through the authorized review workflow.
- [ ] Measure five warm Services-only/Overview runs with normal build and lightweight temporary command timing; compare commands and normalized results, not just time.
- [ ] Present PR A evidence and stop for review/merge. After merge, refresh then-current main and create PR B's isolated branch. Do not stack by default.

## Task 4: Add owned lazy observation slots and typed copies (PR B)

**Files:** Create `internal/observation/{cycle,slot}.go` and tests; `internal/profile/clone.go`, `clone_test.go`. No provider integration yet.

**Interfaces:** `observation.New(context.Context) *Cycle`; `(*Cycle).Close()`; `NewSlot[T any](*Cycle, func(context.Context) (T,error), func(T) T) *Slot[T]`; `(*Slot[T]).Get(context.Context) (T,error)`; `observation.ErrCycleClosed`. `profile.CloneData(profile.Data) profile.Data`, `profile.ClonePackages(profile.Packages) profile.Packages`; private recursive Mise-value clone preserves supported concrete TOML value types.

- [ ] Write `TestSlotSharesConcurrentLoad`, `TestSlotCachesFailureUntilNextCycle`, and `TestSlotReturnsDefensiveCopyToFirstAndLaterCaller`. Use channels to prove exactly one loader and independent slots, not elapsed-time guesses.
- [ ] Write `TestSlotCanceledWaiterDoesNotCancelSharedLoad`, `TestAlreadyCanceledCallerDoesNotStartLoad`, `TestCycleCancelPreventsPublication`, `TestCycleCloseJoinsAndRejectsLateResult`, `TestConcurrentGetAndClose`, and `TestCycleCloseIdempotent`. Assert no loader survives Close and a new cycle performs a new load.
- [ ] Write `TestClonePackagesPreservesLiveOnlyFacts` for every Packages field, including Installed, MiseInstalled, semantic maps, preinstalls, missing metadata, absent/excluded/machine-specific lists, nested Mise arrays/maps and numeric/time values. Mutate each returned nested object and prove the source is unchanged. Pin nil-versus-empty semantics.
- [ ] Write `TestCloneDataIsolatesProfilePolicyAndMachine`: all nested profile collections, Services instances/artifacts, Resources links/Git state, Config metadata, Shell maps, and machine policy/root mappings remain independent.
- [ ] Run `go test ./internal/observation ./internal/profile -count=1`; observe intended failures before implementing.
- [ ] Implement the spec's lifecycle: one lazy goroutine per requested slot, registration/closure lock, no cycle-wide lock held across load, cache non-cancellation errors, check cancellation/closed state before publication, typed copies on every return, Close cancellation/join/clear. Do not add a dependency.
- [ ] Run targeted tests and `go test -race ./internal/observation ./internal/profile`; commit the focused change.

## Task 5: Make Services observations local and bind read projections (PR B)

**Files:** Create Services `observation.go`, `read.go`, `observation_test.go`, `read_test.go`; modify `provider.go`, `discovery.go`, `capture.go`, `plan.go`, `plan_files.go`, `verify.go`; add ReadProvider interfaces in workflow `providers.go`.

**Interfaces:** private `readSource` stores only Systemd, fallback Roots, root resolver, and ProfileDir; `func (src readSource) observe(context.Context) (Observation,error)`; `Observation.Clone() Observation`; `func (p *Provider) BindReadCycle(*observation.Cycle) workflow.ReadProvider`. Observation owns roots and cloned units. Shared private `diffObserved`, `targetsObserved`, and `planObserved` accept `(context.Context, profile.Data, Observation)`; plan also accepts `omarchy.Info` and `workflow.RestoreContext`, returning existing domain types.

- [ ] Write `TestServicesReadViewObservesOnceForDiffTargetsAndPlan`: exercise all projections and concurrent slot requests; compare results with fresh projections over the same fake facts. Cover no selected units and unavailable manager both with/without saved units.
- [ ] Write `TestServicesObservationCopiesNestedUnitsAndRoots`, `TestServicesReadViewHasNoMutationCapabilities`, and `TestServicesReadRootsDoNotMutateProvider`: vary resolver results between independent cycles and ensure no shared Roots assignment or prepared-state access.
- [ ] Write overlapping-cycle tests with thread-safe fake Systemd/root resolvers; one slot per cycle, one inventory per slot. Verify template/drop-in/sensitivity/fingerprint/provenance parity.
- [ ] Run new tests to fail, then construct read views from explicit immutable source fields, never a whole shared Provider copy containing prepared state. Implement common projections with operation-local resolved provider/input values.
- [ ] Make discovery, authoritative Plan, Verify, and Capture helpers use local roots. Prepared state is still assigned only on the original Capture transaction object; do not move Commit/Rollback/Finalize onto a read view. Authoritative calls use fresh source observations without a slot.
- [ ] Run `go test ./internal/providers/services ./internal/workflow` and `go test -race ./internal/providers/services`. Existing service approval/reconstruction assertions must remain intact; commit.

## Task 6: Bind complete Packages observations in the application adapter (PR B)

**Files:** Create Packages `observation.go`, `observation_test.go`, app `packages_read.go`, `packages_read_test.go`; modify Packages `provider.go` only to expose snapshot construction and app `providers.go` only to extract shared projection helpers.

**Interfaces:** `packages.Provider.Observe(context.Context) (packages.Observation,error)` calls unchanged Detect; `Observation.Clone() Observation`; `Observation.Packages() profile.Packages` returns a full clone. Application read binding lazily resolves detector/Mise configuration once and holds one typed slot. Existing packagesStateProvider authoritative methods remain fresh.

- [ ] Write `TestPackagesReadViewDetectsOnceForDiffTargetsAndPlan` and `TestPackagesReadViewNewCycleDetectsAgain`; preserve compatibility, missing-sync-database handling, canonical preinstall ownership, semantic recipes, machine-specific exclusions, installed Mise versions, and Exact deletion safety.
- [ ] Write `TestPackagesReadPlanPreservesLiveScratchFields` with nonempty Installed/semantic/Mise maps so losing JSON-excluded facts changes the plan and fails the test. Cover OriginUnavailable with UnclassifiedExplicit and MissingSyncDatabases.
- [ ] Run failing tests; extract `targetsFromPackages` and `planFromPackages` shared helpers retaining the existing filterPackagesForRestoreSkip/compatibility/planner logic. Fresh methods call Observe without slots; read methods use the slot and full typed copies.
- [ ] Run `go test ./internal/providers/packages ./internal/app` and targeted race tests. Assert no pacman query combining, new metadata reads, or origin semantics change; commit.

## Task 7: Integrate explicit cycles into workflow and read clients (PR B)

**Files:** Create workflow `read_cycle.go`, `read_snapshot.go`, `read_restore.go` and tests; app `read_providers.go` and tests. Modify workflow `session.go`, `providers.go`, `status.go`, `overview.go`, `inspect.go`, `policy.go`, `capture_inspection.go`, `restore.go`; app `app.go`, `restore_plan.go`, `change_targets.go`; TUI `screens/restore.go` and tests.

**Interfaces:** ReadCycle APIs and RestorePreview from the spec. `loadReadSnapshot(ctx context.Context, cfg readConfig) (profile.Data, machine.Selection,error)` is private/pure. Add `Session.SetReadRestoreFinalizer(func(context.Context, profile.Data, []string, *model.RestorePlan, policy.RestoreOptions) error)` alongside unchanged authoritative SetRestoreFinalizer. Extract app `finalizeRestorePlanForIDs` with `(context.Context, Dependencies, *options, profile.Data, []string, *model.RestorePlan, restorePlanOptions) error`; keep existing wrapper/tests compatible.

- [ ] Write `TestReadCycleSnapshotUnaffectedByReloadAndProfileEdits` and `TestOverlappingReadCyclesOwnIndependentSnapshots`: fresh profile/policy/binding loads, target validation, machine defaults and canonical roots match existing loader rules; existing cycles do not observe later changes. Exercise Reload concurrently without reading its mutable fields.
- [ ] Write `TestReadAdaptersPreserveCapabilities`: Config DiffWithScan, Resources DiffWithGitWorkingState, category enablement, validation, ChangeTargetResolver, RestoreTargetResolver, and deterministic provider order are preserved. Read view type assertions to mutation/transaction interfaces must fail.
- [ ] Write `TestOverviewReadCycleCaptureShortCircuit` and `TestOverviewReadCycleRestoreClassification`: for both participating providers, Diff+targets+classification use exactly one observation; RestorePlan remains lazy and no Session.Reload occurs during classification. Retain fail-conservative behavior for unattributable changes and failed planning.
- [ ] Write `TestCapturePreviewManySharesCycle` and `TestRestorePreviewSafeAndForceShareObservation`: independent plans/outcomes/context decisions/order match fresh same-state calculations; mutating a returned result cannot change a later projection.
- [ ] Run targeted tests to fail. Add immutable read configuration protected by a narrow mutex; BeginRead copies it and independently loads desired state rather than calling Reload. Generalize pure policy/target validation helpers to narrow read contracts while authoritative callers retain identical validation.
- [ ] Bind read providers once at BeginRead; introduce read projection helpers and Session convenience wrappers with deferred Close. Metadata Omarchy info is a lazy cycle slot; other providers need no slots. Clone desired inputs/results. Overview classification calls that same cycle's preview helper.
- [ ] Keep restorePlan/PlanRestoreWithContext uncached. Change CLI initial/dry-run rendering to PreviewRestore; keep authoritative replan/equality/execution/Verify. Preserve no-op verification with fresh authority contexts and reject a changed plan. Keep terminal/elevation/activation behavior identical.
- [ ] Update one TUI Restore refresh to own one cycle for effective and Safe previews, then Close before returning. Human activation choices/new refresh/navigation start new cycles. Keep request-ID rejection and add canceled/late preview tests. Do not introduce cross-screen stored cycles or progressive UI.
- [ ] Run workflow/app/TUI tests plus race tests; commit only after Task 8's authority coverage is green.

## Task 8: Prove fresh mutation authority remains release-blocking (PR B)

**Files:** Create `workflow/read_authority_test.go`; extend `capture_approval_test.go`, `approved_restore_test.go`, app `capture_fingerprint_test.go`, `services_capture_test.go`, `services_restore_test.go`, `app_test.go`; retain Services activation/execution tests.

**Interfaces:** private `inspectCaptureManyFresh(context.Context, []string) (CaptureInspection,error)` used only by Capture approval comparison; `recheckCapture` remains fresh. Existing Capture/Apply/PlanRestoreWithContext signatures unchanged.

- [ ] Write Services and Packages variants of `TestPreviewAThenApprovedCaptureObservesB` and `TestPreviewAThenApprovedRestoreObservesB`: keep the cycle for A alive, change fake live state to B, pass approval toward mutation, assert fresh probe count increases and the new review/plan is compared. Changed authority refuses with zero profile/executor effects.
- [ ] Write `TestCLIApprovalRecheckBypassesReadCycle` using a fake runner that changes state while reading approval; assert fresh PlanRestoreWithContext detects B before journal/execution. Cover --yes as well as interactive approval and activation selection.
- [ ] Write `TestCapturePostStageRecheckStillObservesC`: preview A, matching fresh pre-stage A, change to C during staging; assert independent post-stage inventory/fingerprint check and rollback with unchanged profile. Include same outcome but changed content/mode/provenance.
- [ ] Write `TestVerifyDoesNotUseReadSnapshot` and `TestChangedMiseDeclarationInvalidatesAuthority`; preserve plan intent versus verification intent, compatibility/readiness/activation checks, and executor file preconditions. Tests use fake runners and temporary roots only.
- [ ] Run tests to establish the stale-path failure if wired incorrectly. Split public read inspection from private fresh orchestration; ensure no authority entry passes a bound read provider or a cycle to execution. Do not add reuse within authority phases.
- [ ] Run all approval/fingerprint/Services/Packages tests, `go test ./...`, and race coverage. Any authority weakening stops the work; do not waive a failure to improve timings.

## Task 9: Lightweight diagnostics, PR B verification, and performance gate

**Files:** Create diagnostics/command tests and opt-in workflow/app performance tests listed in the file map; modify `internal/command/runner.go`, workflow `dependencies.go`/`read_cycle.go`, app `app.go`; update `docs/cli.md` for the debug switch and evidence in the spec.

**Interfaces:** `diagnostics.WithObservation(ctx context.Context, out io.Writer) (context.Context, func())` returns unchanged context/no-op when disabled; `diagnostics.StartCommand(context.Context, string, []string) func()` returns nil when disabled. Enable only `BLUEPRINT_DEBUG_OBSERVATION=1`; inject stderr through `Dependencies.DiagnosticWriter`. Callback output uses whitelisted family/verb, duration/counts only. Provider observation logging is driven by slot load boundaries, not every projection.

- [ ] Write `TestObservationDiagnosticsDisabled`, `TestObservationDiagnosticsRedactsPayloads`, and `TestObservationDiagnosticsPreservesJSONStdout`: feed secret-looking args, environment, output, paths and names and prove none appear. Pin no disabled-path collector allocation; avoid stack walking/persisted logs.
- [ ] Implement the small stderr-only facility and tests without a metrics dependency/framework. Retain Runner behavior/output limits/error/cancellation semantics.
- [ ] Run the verification matrix and relevant race tests. Document any pre-existing race failures separately; changes must introduce none. Require existing Services Reconstruction Assurance coverage and same-runtime result before merge.
- [ ] Run at least five warm measurements for every path in the spec, including real TUI Overview followed by completed Restore preview, default and Force/Safe refreshes, and the conditional classification scenario. Verify output equivalence, observation counts and child-process structure before interpreting speedups.
- [ ] Present the final before/after table with exact SHA/runtime/instrumentation state. Record Services batch size and rationale, no persisted caching, whether concurrency/progressive UI remain unnecessary, and any target missed. Review/merge B, then stop for the concurrency decision; never silently proceed to Task 10.

## Task 10: Conditional provider scheduling decision (not approved implementation)

PR C execution update: the human authorized spike and implementation after
PR B #62 merged. Base is `7a0c16ec9c937a2545301b9369b710ed44eaba95`.
The [serial/2/4 spike](../specs/2026-10-02-observation-performance-v1-pr-c-spike.md)
is complete with 105 successful samples and equal normalized results. Bound 2
and explicit cycle worker-lifetime integration are proposed for this task's
measured-choice review gate; production implementation has not started.

Approval update: the human reviewed the comparison and explicitly selected
**limit 4**. Implement the scheduler/Status integration using TDD. Add
`Cycle.BeginWork() (func(), error)` for synchronized, idempotently released
projection lifetime registration; use existing Close rather than a separate
Cancel API for terminal failed-cycle loader joining. Exact authority entry points
remain unchanged. The spike report above retains the original recommendation
as historical evidence, not the selected production bound.

**Files if separately approved:** `internal/workflow/read_scheduler.go`, `read_scheduler_test.go`, read-cycle Status aggregation; no authority-path scheduler.

**Proposed interface:** private `observeProviders(ctx context.Context, providers []ReadProvider, limit int, observe func(context.Context, ReadProvider) (ProviderStatus,error)) ([]ProviderStatus,error)`; explicit limit selected by measurement, not a default decided here.

- [ ] Only if A+B remain materially slow, prototype serial/2/4 against the current base, recording wall/CPU/RSS/active and total children, command-family interference, race results and exact output equality. No unit/file/package goroutine fanout.
- [ ] Present the smallest useful bound for human review. If serial already meets practical goals, omit PR C. Do not use a scheduler to claim the Services-only <500 ms target was met.
- [ ] If separately approved, use TDD for indexed deterministic ordering, no-op empty sets, delayed A/early B completion, initiating-error preservation, simultaneous-error tie-breaking, sibling cancellation, joined workers, leak-free Close, no Reload, and rejection of mutation providers.
- [ ] Run all verification and measurements again. Progressive/stale TUI requires a separate proposal if latency still warrants it; this plan does not authorize that implementation.

## Verification matrix after each implementation PR

```sh
go test ./...
go vet ./...
go build ./...
go test -race ./...
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s test/reconstruction/tests -p 'test_*.py' -v
bash -n scripts/release/*.sh
bash -n test/reconstruction/run.sh test/reconstruction/soak.sh test/reconstruction/lib/common.sh test/reconstruction/vm/*.sh test/reconstruction/scenario/*.sh test/reconstruction/verify/*.sh
```

If a full race run is operationally impractical, report why and run at least `go test -race ./internal/observation ./internal/profile ./internal/providers/services ./internal/providers/packages ./internal/workflow ./internal/app ./internal/tui/...` after B. Before B the observation package does not exist; omit that path for A. Distinguish pre-existing failures. Recheck current CONTRIBUTING/workflows for added helper requirements. Do not run developer-machine apply/Capture or the VM provisioning harness as an unreviewed substitute for CI. Observe the authorized Reconstruction Assurance workflow, preserving its binary/runtime identity and Services scenario contract.

## Final report template

| Metric | Before | After | Change |
| --- | --- | --- | --- |
| Services-only Status | measured | measured | delta |
| Full Status | measured | measured | delta |
| Overview | measured | measured | delta |
| Overview to completed Restore preview | measured | measured | delta |
| Commands per Overview | measured | measured | delta |
| systemctl calls | measured | measured | delta |
| Packages detections | measured | measured | delta |
| Services inventories | measured | measured | delta |
| Maximum RSS | measured | measured | delta |

Report exact SHA, runtime, normal/instrumented build distinction, five samples/medians/ranges, CPU self/children, active child bound, batch size/rationale, concurrency choice, progressive TUI choice, and persisted caching (none). Timing goals are never CI assertions. The table above is an execution template, not a claim of implemented results.

## Self-review and handoff

Check every spec requirement against these tasks; resolve missing signatures/paths and contradictory authority boundaries before handoff. Verify only planning files changed, all document links resolve, ADR 0026 is still the next available number, and no production prototype was copied into the worktree.

The human reviews the spec, ADR and this plan together. After approval, execute A first with TDD in an isolated worktree, then stop at each merge/measurement gate. Choose inline execution unless delegation is explicitly selected. No production implementation begins in the planning turn.
