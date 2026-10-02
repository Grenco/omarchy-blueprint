# Observation Performance v1 Design

## Status and review gate

Approved by the human on 2026-10-02. PR A is implemented locally; review/merge is required before PR B. Concurrency remains separately gated.

This spec, ADR 0026, and the accompanying implementation plan were approved together. The human subsequently authorized committing/pushing these planning documents alongside PR A and opening that PR. Tags, releases, settings changes, user-profile mutation and beginning PR B before A's review/merge gate remain outside that authorization.

Planning baseline: latest local `main` after a successful `git pull --ff-only` on 2026-10-02, `4b1617161eff583e01085ef85b270da04829ec48` (already up to date). Refresh `main` again after approval and create an isolated worktree/branch from that then-current SHA. Do not silently reuse a profiling checkout or promote temporary prototypes.

## Intent and scope

Make read-only observation efficient enough for interactive use and a future auto-sync engine without changing ownership, policy, compatibility, approval, invalidation, or mutation authority. Optimize Services subprocess count first, then share Services and Packages facts within an explicit read cycle, then re-measure. Provider concurrency is conditional on those measurements. Progressive TUI behavior is a later decision, not initial scope.

No persisted observations, cache schema, TTLs, watchers, subscriptions, package hooks, daemon, scheduling, or auto-sync implementation. No package-origin rewrite, Git optimization, rendering optimization, new dependency, or profile-schema migration.

## Current code and measured evidence

The original report named `7076cbe62b36132cdf809669bca583583f075800`. A subsequent five-run clean rerun tested `4b1617161eff583e01085ef85b270da04829ec48`. Use exact measured SHAs, not timing constants, when reporting improvements.

Machine: AMD Ryzen 7 5800HS, 16 logical CPUs, Linux 7.1.9-arch1-2, Go 1.27.1 linux/amd64, systemd 261.2. Warm repeated measurements on `omarchy-profile`:

| Measurement | Median |
| --- | ---: |
| First normal TUI output | 46 ms |
| Normal TUI populated Overview | 3.943 s |
| Instrumented TUI populated Overview | 3.909 s |
| Full Status process | 3.201 s |
| Services-only Status process | 2.291 s |
| Blueprint-launched commands per Overview | 471 |
| systemctl commands per Overview | 375 |
| Packages Detect calls per Overview | 2 |
| Services inventories per actual Overview | 1 |

Current behavior:

- `internal/workflow/status.go`: captured providers are diffed serially, with special Config scan and Resources Git paths.
- `internal/providers/services/systemd.go`: two catalogue commands, sorted union of identities, then one `show` per non-template identity. Templates are already catalogue-only.
- `internal/workflow/overview.go`: lazy target attribution and lazy Restore candidate classification. Capture candidacy can short-circuit planning. Restore classification calls `restorePlan`, which currently calls `Session.Reload`.
- `internal/providers/services/discovery.go`, `plan.go`, and `verify.go`: root resolution writes `p.Roots`; read paths must stop retaining resolved roots on the shared provider.
- `internal/app/providers.go`: Packages targets, Diff, and Plan independently construct a detector and call Detect.
- `internal/workflow/capture_inspection.go`: Capture preview independently calls targets and Diff; `InspectCaptureMany` loops over `InspectCapture`.
- `internal/workflow/capture.go`: approved Capture compares a fresh review, reads/stages live data, then independently rechecks fingerprints before saving; rollback is essential.
- `internal/workflow/restore.go`: `ApplyApprovedRestore` freshly replans and requires exact equality. `PlanRestoreWithContext` also serves authoritative CLI reinspection, not just presentation.
- `internal/app/app.go`: CLI Restore replans after approval and executes/verifies using that fresh plan's contexts.
- `internal/tui/screens/restore.go`: one Force preview can calculate a second Safe plan. These two projections can share observations during that one refresh.

A throwaway in-memory active-state drift plus Capture-preserve scenario exercised Services Diff, attribution targets, Restore-context targets, and Plan: four inventories, 1,488 systemctl calls, median 10.774 s over three runs. This is a reproduced conditional slow path, not the actual profile's normal path.

Typed copying is required: `profile.Packages` includes `Installed`, `MiseInstalled`, and semantic-state maps tagged `json:"-"`. JSON round trips lose these facts. The earlier prototype's matching Overview hashes do not prove its copying strategy safe for Capture/Restore. Production must not use that copying strategy.

## Alternatives and decision

1. **Explicit cycle and optional read-provider binding (selected).** A short-lived owner holds lazy immutable observations and exposes only read projections. Services and Packages participate first. Fresh authority remains separate and uncached. This gives auto-sync a reusable observation boundary without forcing every provider into a new data model.
2. **Context-carried memoization around Detect/InspectUserUnits (rejected).** Small changes, but scope and authority depend on inherited contexts; a mutation caller can accidentally retain presentation observations. It also hides configuration identity and lifetime.
3. **Universal Observe/Project interface for every provider (deferred).** Uniform but requires an unnecessary conversion of Config, Resources, Shell, and other inexpensive providers. Optional read binding gives a later extension point without that rewrite.

PR B implementation measurements are recorded separately in
[PR B results](2026-10-02-observation-performance-v1-pr-b-results.md), including
conditional classification and actual TUI navigation. They do not authorize
the conditional PR C workstream.

## A. Services batching

Keep the two existing catalogue commands, parsing rules, identity union, catalogue-only templates, normalization helper, and final sorting. Split the sorted eligible operands into chunks of **64**. Commands remain serial within one inventory: `2 + ceil(eligible / 64)` processes, zero shows when eligible is empty. Never introduce a goroutine per unit.

Add `Names` to the existing property request strictly as identity-mapping evidence. Do not include it in normalized desired state or ownership decisions.

### Record mapping independent of output order

Parse blank-line-delimited records, checking that each has exactly one usable `Id`. Build an identity index from canonical `Id` and the tokenized `Names` aliases. Never pair output records with operands by position.

Every requested operand must resolve to exactly one canonical identity. Reject conflicting alias membership, missing operands, records unrelated to every requested operand, malformed/duplicate identity properties, and unusable IDs. A canonical Id need not itself appear among the operands when a requested alias maps to it through Names. Identical duplicate alias records may coalesce. Compare duplicates using existing normalized properties, not raw property text: systemd can reorder dependency lists without changing facts. Conflicting normalized duplicate facts fail the inventory; no arbitrary winner or silent omission.

After mapping, process requested names in the original globally sorted order, using each requested name's catalogue state in `normalizeObservedUnit`. Preserve existing last-sorted-operand deduplication semantics and the catalogue fallback when `UnitFileState` is empty. Conflicting duplicates must also be checked across chunks. Preserve naked template records and final canonical-ID sorting.

Failure of a catalogue command, any batch, output limit, mapping, or cancellation fails the inventory. Preserve existing higher-level unavailable-manager/compatibility handling. Do not retry per-unit shows or introduce an unbounded fallback that recreates O(units) process count. If a supported runtime cannot expose sufficient identity evidence, stop for review.

Retain the 4 MiB per-command output bound. Preserve source, drop-in, dependencies, triggers, mask, template/instance, generated/runtime/transient, linked-source, and persistence normalization. Inspection never grants ownership.

### Live spike evidence and size choice

A new throwaway probe on this planning baseline requests `Names`, deliberately reverses parsed records, maps by identity, and compares complete normalized inventories with the unchanged per-unit implementation. All 20 comparisons passed. Both forward and reversed operand lists also resolved all six aliases correctly.

| Show strategy | Processes, excluding catalogues | Median show-phase wall | Output bytes |
| --- | ---: | ---: | ---: |
| All eligible operands | 1 | 0.695 s | 119,907 |
| Chunks of 16 | 24 | 0.795 s | 119,884 |
| Chunks of 64 | 6 | 0.706 s | 119,902 |
| Chunks of 128 | 3 | 0.694 s | 119,905 |

The machine had 132 catalogue entries, 333 loaded entries, union 383, 14 naked templates, 369 show operands, and 377 normalized identities. Serial complete inventory was 2.145 s in this probe. These batch times exclude the two catalogues (approximately 0.097 s together). Size 64 was within roughly 12 ms of 128 while halving operands per invocation and further bounding output and failure scope; size 16 incurred measurable extra process overhead. Unlimited batching offered little additional benefit.

Temporary evidence: `/tmp/blueprint-names-spike.u7QgZS/names-batching-results.txt`. Artifacts can expire; the table and method above are the durable evidence. Repeat the equality spike against the implementation base and proposed production parser before replacement. The temporary parser is not production code and lacks the full malformed-output test suite.

## PR A execution evidence (2026-10-02)

Before: `4b1617161eff583e01085ef85b270da04829ec48`; after: `7def693779ce1cdad61f8b058e8e390dc54d4aaa`, local branch `perf/services-observation-batching`. Same machine/runtime as above, normal `go build` binaries. Instrumented logical probes inject a counting Runner only; no production timing hooks, caching, concurrency or mutation changes. Five warm runs per reported path, serial non-overlapping suites.

| Metric (median) | Before | PR A after | Change |
| --- | ---: | ---: | ---: |
| Initial normal TUI output proxy | 44.8 ms | 44.4 ms | unchanged |
| Normal TUI populated Overview | 3.729 s | 2.375 s | -36% |
| Session.Overview calculation | 3.630 s | 2.320 s | -36% |
| Session.Status calculation | 3.035 s | 1.689 s | -44% |
| Services-only CLI Status process | 2.135 s | 0.837 s | -61% |
| Packages-only Session.Status | 0.634 s | 0.637 s | unchanged |
| Overview then completed Restore preview, logical probe | 9.847 s | 5.853 s | -41% |
| Commands per Overview | 471 | 108 | -77% |
| systemctl calls per Overview | 375 | 12 | -97% |
| Commands per Services inventory | 371 | 8 | -98% |
| Services inventories per Overview | 1 | 1 | unchanged |
| Packages Detect calls per Overview | 2 | 2 | unchanged |
| Services inventories, Overview + Restore preview | 3 | 3 | unchanged |
| Packages Detect calls, Overview + Restore preview | 4 | 4 | unchanged |
| TUI total user+system CPU, including children | 2.718 s | 1.768 s | -35% |
| Largest-process maximum RSS, normal TUI | 162.28 MiB | 162.35 MiB | unchanged |
| Simultaneously executing Blueprint Runner calls | 1 | 1 | unchanged |

Normal TUI populated ranges were 3.709–3.743 s before and 2.359–2.394 s after. Session.Overview ranges were 3.622–3.643 s and 2.312–2.338 s. Overview Go self CPU was 0.248/0.121 s; child CPU was 2.358/1.601 s. These are subprocess/I/O savings, not a rendering redesign. RSS is a maximum for a single process, not a sum of a process tree. `/usr/bin/time` is absent; wait4/Getrusage counters were used without installing anything.

| Overview command family | Calls before/after | Cumulative elapsed before/after |
| --- | ---: | ---: |
| systemctl | 375 / 12 | 2.121 / 0.823 s |
| pacman | 32 / 32 | 1.031 / 1.021 s |
| pacman-conf | 4 / 4 | 0.024 / 0.024 s |
| mise | 2 / 2 | 0.022 / 0.022 s |
| git | 36 / 36 | 0.053 / 0.053 s |
| omarchy | 6 / 6 | 0.147 / 0.147 s |
| sh | 12 / 12 | 0.158 / 0.158 s |
| tailscale | 2 / 2 | 0.014 / 0.013 s |
| cat | 2 / 2 | 0.002 / 0.002 s |
| systemd-analyze | 0 / 0 | 0 / 0 s |

The final opt-in live test passed five complete normalized inventory comparisons against the serial reference, deliberately reversing returned records: 377 normalized units, 371 versus 8 processes, serial median 2.107 s versus batched 0.801 s. The execution-base all/16/64/128 spike also passed all 20 comparisons; median show times were 0.701/0.804/0.731/0.722 s. Chunks of 64 remain selected; the 4 MiB command limit is unchanged. Separate command-marker traces confirm unchanged observation counts in the table: one list-unit-files per inventory and one pacman -Qq per successful Detect on these paths. No read-cycle reuse has been implemented.

The real parser revealed quoted/backslash-escaped Names tokens missed by the throwaway probe. Decode their outer quoting once while retaining literal systemd unit-name escapes. This follows the upstream [string-vector formatter](https://github.com/systemd/systemd/blob/v261.2/src/shared/bus-print-properties.c) and [shell quoting implementation](https://github.com/systemd/systemd/blob/v261.2/src/basic/escape.c); existing dependency/drop-in normalization is deliberately unchanged. Independent review found catalogue-linked duplicate SourcePath conflicts and final cancellation publication gaps; both were reproduced by failing tests and fixed. Ambiguous/missing/failed inventories still fail closed.

All five before/after logical Overview, Status, Services, Packages and Restore-preview output hashes matched, including Restore operation ordering. The combined probe invokes Overview then PlanRestore; it is not an automated real TUI navigation/Force-Safe measurement. PTY readiness measurements send q after populated output; both baseline and after exit 1 on shutdown, consistent with the existing TUI cancel-before-Quit path, whereas all logical probes and CLI Services runs exit 0. Do not mistake this readiness proxy for proof of clean terminal shutdown.

Local verification passed: full Go test/race suites, vet/build, 61 release-helper tests, 103 reconstruction-helper tests, and required shell syntax checks. Hosted same-runtime Reconstruction Assurance remains required before merge and has not been triggered. User profile and main production source were not changed; no external repository mutation occurred.

PR A misses the final <500 ms Services and <1.5 s Overview goals. Packages detection/repeated Services preview observations remain for PR B; no provider concurrency, progressive loading, or persisted caching was introduced. Raw sanitized metric logs and the execution ledger are in the PR A worktree's ignored `.superpowers/sdd/2026-10-02-observation-performance-v1-implementation-plan/` directory. An intermediate shell-exec resource-counter anomaly was discarded; final after probes use explicit per-process launches.

## B. Observation lifecycle and read interfaces

Introduce a small dependency-neutral `internal/observation` package. It knows lifecycle and typed lazy slots, not provider/domain types, profile serialization, policy, or mutation. The workflow package owns a public `ReadCycle`. No ambient context marker, global cache, or map keyed only by provider ID.

Proposed interfaces (names and signatures are pinned by the implementation plan):

```go
// internal/observation
func New(ctx context.Context) *Cycle
func (c *Cycle) Close()
func NewSlot[T any](c *Cycle, load func(context.Context) (T, error), clone func(T) T) *Slot[T]
func (s *Slot[T]) Get(ctx context.Context) (T, error)

// internal/workflow
type ReadProvider interface {
    ID() string
    Captured(profile.Data) bool
    Diff(context.Context, profile.Data) ([]model.Change, error)
    InspectTargets(context.Context, profile.Data) ([]TargetInspection, error)
}
type ReadRestoreProvider interface {
    ReadProvider
    Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (RestoreFragment, error)
}
type ReadCycleBinder interface {
    BindReadCycle(*observation.Cycle) ReadProvider
}
// Optional desired-state inputs for Resources' machine-root interpretation.
type ReadSnapshot struct {
    Profile profile.Data
    Machine machine.Selection
}
type ReadSnapshotBinder interface {
    BindReadSnapshot(*observation.Cycle, ReadSnapshot) ReadProvider
}
func (s *Session) BeginRead(ctx context.Context) (*ReadCycle, error)
func (r *ReadCycle) Close()
func (r *ReadCycle) Snapshot(ctx context.Context) (ReadSnapshot, error)
func (r *ReadCycle) EffectivePolicy(ctx context.Context, scope PolicyScope, category string, target TargetInspection) (policy.Effective, error)
func (r *ReadCycle) Status(ctx context.Context, only string) (StatusReport, error)
func (r *ReadCycle) CaptureStatus(ctx context.Context) (StatusReport, error)
func (r *ReadCycle) Overview(ctx context.Context) (Overview, error)
func (r *ReadCycle) PolicyTargets(ctx context.Context, category string) ([]TargetInspection, error)
func (r *ReadCycle) InspectCapture(ctx context.Context, only string) (CaptureInspection, error)
func (r *ReadCycle) InspectCaptureMany(ctx context.Context, ids []string) (CaptureInspection, error)
func (r *ReadCycle) PreviewRestore(ctx context.Context, only string, options *policy.RestoreOptions) (RestorePreview, error)
```

`RestorePreview` contains `Plan model.RestorePlan`, `Options policy.RestoreOptions`, `Profile profile.Data`, and `Machine machine.Selection`. It contains no mutation providers or authority-bearing execution contexts. `ReadCycle` has no Capture, Apply, Verify, Reload, or setters. Returned read-provider concrete types must not implement `Provider`, `RestoreProvider`, or capture-transaction methods; do not embed an authoritative provider publicly.

Existing Provider embeds ReadProvider while retaining Capture; RestoreProvider retains Plan and Verify. Existing providers need not gain observation slots. Services and the application Packages adapter implement optional binding. Other providers receive narrow forwarding read views. Preserve Config scan, Resources Git, category-enable, target-validation, change-target, and Restore-operation-target capabilities; do not lose specialized behavior through a generic wrapper.

### Desired-state snapshot and Session ownership

`BeginRead` captures immutable session configuration (canonical root, explicit machine option, dependency functions, provider bindings, Profile Git service, read finalizer), then loads and validates a private profile/machine selection once. Extract the existing Reload loading/validation/binding logic into a pure helper. `BeginRead` never publishes into `s.profile`, mutates options, or calls Session.Reload. Nested classification uses this snapshot throughout.

Protect the captured configuration with a narrow configuration mutex used by Open, SetProviders, and finalizer setters; copy the registry slice. Existing cycles are unaffected by later registry changes. Binding copies only immutable provider configuration, never prepared Capture state. No claim is made that all Session mutations become concurrent-safe; mutations remain serialized. Overlapping cycles own independent desired state and slots, and do not race a Reload by reading its mutable fields.

Use typed defensive copies for observed and derived values, including every nested slice/map and live-only Packages field. Loaders transfer owned values into a slot; callers receive clones, including the first caller. Clone policy/machine/profile values passed to projection code or returned to consumers so modifying one result cannot modify the cycle's saved state. Preserve concrete TOML/Mise value types and nil-versus-empty semantics. Do not use JSON or serialization as a cloning shortcut.

A cycle is a stable set of per-provider observations acquired lazily at potentially different times, not an atomic machine snapshot. Services v1 reuse covers its resolved roots and normalized systemd inventory. Existing filesystem sensitivity, provenance, content and fingerprint checks remain in projection/authority helpers; this tranche does not snapshot all filesystem bytes. No cross-screen, cross-refresh, or human-pause reuse is implicit.

The cycle also owns a lazy omarchy.Info metadata slot for preview environment checks. Multiple preview projections share this read-only metadata; authoritative planning continues to obtain fresh metadata through its existing path.

PR B review clarification: Resources needs the optional snapshot-aware binding
even though its discovery is not cached, because its previous adapter reloaded
the machine binding inside every projection. Freeze the explicit selection,
including portable/no-machine selection, in a private read worker. Do not copy
its prepared Capture state. TUI/CLI consumers must render the result's desired
profile, machine and resolved options, and resolve policy through the same
cycle, instead of combining fresh facts with old Session metadata. Presentation
may retain copied result metadata after Close; it never grants write authority.

### Slot semantics

- One slot per participating provider binding per cycle; one lazy load at most, including concurrent callers. Clone/loader closures stay provider-owned.
- Cache successful facts and non-cancellation failures for the cycle. All projections see the same failure; retry only in a new cycle. Do not cache fallback inventories manufactured by policy-dependent callers.
- Concurrent waiters share one in-flight load using the cycle's context. A canceled waiter returns its own context error without canceling other waiters. An already-canceled caller does not start work.
- Parent-cycle cancellation reaches the loader and SystemRunner subprocess; no successful value is published after cancellation or closure.
- Explicit Close is idempotent, marks the cycle closed before canceling, joins every launched loader, and releases retained facts. Calls after Close return `observation.ErrCycleClosed`; parent cancellation before Close returns the parent context error.
- Registration/launch and closure share a lock so a WaitGroup Add cannot race Close's Wait. No loader runs under the cycle-wide lock; different provider slots can load independently. No goroutine is started until a slot is first requested.
- Caller owners always defer Close. A late TUI result is rejected by existing request IDs; cycle cancellation also prevents it becoming reusable state. All projection outputs retain current stable ordering.

### Provider integration

Services gets a private immutable read-source configuration and an Observation containing resolved roots and cloned normalized units. Read-bound Diff/targets/Plan acquire that slot and invoke shared projection helpers. Helpers operate with local resolved roots; discovery never assigns the shared provider's Roots. Artifact reads and prepared Capture transactions remain distinct.

Packages gets an Observation representing the full Detect result and resolved detector configuration. Resolve the Mise path and run Detect once lazily per bound view. Diff, targets, and preview Plan use that observation, preserving missing-database/origin-unavailable, preinstall ownership, semantic recipes, hardware exclusions, installed Mise state, and Exact safety. Do not combine or replace pacman queries in this tranche.

Share projections, not final plans: Safe and Force previews are independently derived from the same facts and options. Lazy Overview classification remains lazy; no eager targets or Restore plans for healthy/short-circuited categories.

## C. Fresh mutation authority

Read convenience methods on Session create, use, and close a new ReadCycle. `Session.PlanRestore` can return the plan from a new preview; add `Session.PreviewRestore` for callers needing preview metadata. Explicit owners can share several projections using BeginRead, notably one Restore refresh's effective/Safe comparisons.

Keep these authority paths separate:

| Boundary | Required behavior |
| --- | --- |
| Capture / CaptureMany | Freshly reload/validate authoritative desired profile and machine at transaction entry, then fresh target resolution and provider Capture observation; no ReadCycle binding |
| CaptureApproved | Private fresh inspection for approval comparison, then fresh provider reads/staging, then independent fresh post-stage fingerprint recheck/rollback |
| ApplyRestore / ApplyApprovedRestore | Existing Reload and uncached fresh plan, approval comparison, applicability/invariants, executor preconditions, fresh Verify |
| CLI post-approval recheck | `PlanRestoreWithContext` remains authoritative and uncached; returns fresh providers/contexts for execution and Verify |
| CLI no-op verification | Obtain authoritative fresh providers/contexts and preserve no-op verification; never Verify through a preview view |

Reload at the action boundary for TUI profile/policy edits too: an accepted
presentation snapshot must not cause unrelated external profile edits to be
overwritten. This is not a promise of atomicity against concurrent external
writes during a transaction, nor permission to run mutations concurrently.

Move preview orchestration to read helpers without changing `restorePlan` into an ambient cache. `captureMany` must use a new private `inspectCaptureManyFresh` rather than the public read convenience methods. `recheckCapture` and Verify stay uncached. Preserve exact plan equality, selections, fingerprints, staged rollback, compatibility, readiness, operation ordering, activation authority, elevation/terminal handling, and journal/executor preconditions.

The CLI initially calls PreviewRestore for rendering/dry-run. Any activation-choice pause creates a fresh preview. After approval it follows the existing PlanRestoreWithContext recalculation and equality check. If a preview contains no operations, acquire fresh authority contexts before the existing verification path and refuse a materially changed plan. Do not refactor CLI execution into a different workflow in this tranche.

Add a read-only finalizer callback accepting selected IDs, not authoritative provider objects. Share the existing Shell/plugin dependency finalization algorithm through an ID-based helper; preserve the authoritative finalizer's signature and compatibility. Neither callback grants an observation cycle mutation methods. Shell/plugin probes may remain fresh because they are not first-tranche slot participants.

## D. Measurement gate and conditional concurrency

Re-measure after PR A, and again after PR B, before deciding PR C. At least five warm runs: first rendered frame/output proxy, full Overview, full Status, Services-only, Packages-only, and Overview followed by opening Restore with a completed preview. Measure default preview plus Force's Safe comparison and a scenario requiring lazy Restore classification. Separate calculation and rendering, and fresh independent navigation cycles from an explicitly shared single refresh.

Record exact source SHA, local diff/instrumentation status, build flags, machine/runtime, command family counts/cumulative/median/max times, observation counts, process count, CPU self/children, largest-process max RSS, and peak active Blueprint-launched children. RSS is not a summed concurrent-process measurement. Avoid overlapping benchmark suites; initial profiler runs overlapped and were discarded. No developer-machine Capture/Restore apply or state-changing probes.

Targets: first frame <100 ms, Services-only <500 ms, full Overview <1.5 s, Overview-to-Restore <3 s. These are engineering targets, never elapsed-time CI assertions. Existing chunks-plus-catalogue data suggests roughly 0.8 s Services observation: do not promise the 500 ms target or change inventory semantics to meet it. Failure to meet a target triggers analysis, not automatic concurrency or a broader discovery rewrite.

If A+B leave material Status/Overview latency, separately prototype coarse provider scheduling serial/2/4. Choose the smallest useful bound after current measurements and review. Retained throwaway results (not production promises): batch64 Overview 2.429 s serial, 1.836 s limit 2, 1.614 s limit 4; combining batching/reuse with independent slot locks and limit 4 gave 0.967 s. This warrants evaluation after A+B, not concurrency now.

An approved scheduler operates only on read-cycle providers, writes preallocated report indexes in configured order, cancels siblings on the first meaningful failure, joins launched workers, and preserves the initiating error over secondary context-canceled failures. Use a deterministic lowest-provider-index choice for simultaneous independent non-cancellation failures. No Reload, Capture, Apply, or Verify runs through it. Do not parallelize classification or finalization merely because Status was parallelized. Audit provider read inputs first and run race tests.

No progressive/stale UI is planned. If meaningful latency remains after backend work, propose a separate reviewed presentation change. Any displayed stale data remains presentation, never authority.

PR C approval update: after PR B #62 merged, the human authorized the
[serial/2/4 spike](2026-10-02-observation-performance-v1-pr-c-spike.md) and selected
**limit 4** over the initial limit-2 recommendation for its additional latency
gain. Production scheduling is confined to read-cycle Status/Diff; classification,
Restore preview planning and finalization remain serial. Register scheduled
Status work with the cycle so Close joins forwarded providers as well as slot
loaders. On a meaningful failed refresh, cancel/join local workers, release that
registration, then Close/join the failed cycle and preserve the initiating error.
Cancellation of an individual projection waiter remains local unless its cycle
parent is canceled; no stale result is returned as success. A signal-killed or
unstarted negative-exit command after scheduler cancellation is a secondary
cancellation, not a new lower-index meaningful failure.

## E. Diagnostics and verification

Add only lightweight opt-in stderr diagnostics, off by default, e.g. `BLUEPRINT_DEBUG_OBSERVATION=1`. Record provider ID, observation count/duration, total refresh time, command family/verb counts and elapsed time; no paths, unit/package names, arguments, environment, output bodies, URLs, fingerprints, or secrets. No files, network, telemetry store, or metrics dependency. Preserve human/JSON stdout. Disabled code must avoid stack walking, log formatting, and allocation-heavy collectors.

Release-blocking tests cover batching structure/semantics, one observation across each lazy classifier path, unchanged projection outcomes, defensive copies, concurrent singleflight, failure caching, cancellation/Close joining, independent cycles, and fresh A-to-B mutation authority for both Services and Packages. A stale preview passed toward Capture/Restore must never prevent fresh observation of B; changed plans/reviews must refuse with zero effects. Separately change state during staging to prove the existing second fingerprint check still rejects/rolls back.

After every implementation PR: `go test ./...`, `go vet ./...`, `go build ./...`, race tests (full `go test -race ./...` preferred), release-helper and reconstruction-helper unit tests, and existing shell syntax checks. Read CONTRIBUTING and workflow commands at execution time. Preserve CI Go 1.25/1.26 compatibility and Reconstruction Assurance's same-runtime guarantees. Services reconstruction coverage must remain intact; require the existing RA workflow result before merge, without changing its runtime contract or applying Restore to the developer machine.

## Delivery and departures from the brief

1. **PR A:** Services bounded identity-aware batching, parser/normalization/structural tests, live equivalence evidence. No cycle or scheduler redesign.
2. **PR B:** explicit cycle/slots, local Services roots, Services/Packages read views, preview integration, typed copies, fresh-authority and race tests, lightweight diagnostics. No scheduler or progressive UI.
3. **PR C only after measurement and review:** bounded provider scheduler if justified; a further presentation proposal only if still needed. Same-cycle singleflight belongs in B for correctness, not a mandatory C.

Merge/review each PR before starting the next unless stacking is explicitly authorized. Planning creates none of them.

Current code requires four clarifications, not broader scope: `Names` replaces the prototype's positional alias mapping; typed copies replace its unsafe JSON copy; PlanRestoreWithContext remains uncached due to CLI authority; BeginRead loads a private desired-state snapshot rather than calling Reload inside classification. This last change makes read refreshes coherent across projections and is tested against externally edited profile/policy/binding files. It does not publish changed Session state or relax mutation validation.

Stop for review on unmappable identity, normalized-semantic divergence, any weaker freshness guarantee, unsafe shared Session ownership, persisted authority, or materially changed Capture/Restore behavior. No temporary profiling code is promoted wholesale.

## References

- [ADR 0026](../../adr/0026-shared-read-observations-and-fresh-mutation-authority.md)
- [Implementation plan](../plans/2026-10-02-observation-performance-v1-implementation-plan.md)
- ADRs 0020, 0021, 0022, 0023, and 0024.
