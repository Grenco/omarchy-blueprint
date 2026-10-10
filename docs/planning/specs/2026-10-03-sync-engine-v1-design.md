# Sync Engine v1 Design

## Status and review gate

Approved by the human on 2026-10-03. This document records the approved design for Roadmap Phase 10, **Sync Engine**. It authorizes writing the associated ADR and implementation plan, but does not authorize product implementation, GitHub mutation, profile mutation, tags/releases, or unattended synchronization.

Planning baseline: repository `Grenco/omarchy-blueprint` `main` at `b01c18d2b5ce51f17ab97bfd66e4602d5838c306` (merge of PR #63, `perf: bound read-provider observation concurrency to four`) as observed on 2026-10-03. Refresh `main` after this spec is approved and before writing/executing an implementation plan; do not silently implement against a stale baseline.

The written-spec review gate is complete. The next gate is **human review of the implementation plan**. Production implementation remains separately gated after plan review and selection of the execution method.

## Intent

Make Blueprint able to reconcile multiple machines against one Git-backed profile as **semantic desired state**, without reducing the existing safety model to a background `pull -> restore` loop.

A machine keeps a local reconciliation cursor that means:

> **This machine is semantically settled through profile revision X.**

Against that cursor, Sync Engine v1 compares:

- committed local profile intent at local `HEAD`;
- committed shared profile intent at one configured upstream branch after fetch;
- current live machine state;
- the selected machine overlay and effective Capture/Restore policy.

It then produces a deterministic, target-level Sync Plan that the user can inspect, resolve, and execute interactively. Conflicting target intent is resolved as **Keep local**, **Accept remote**, or **Defer**. Execution reuses existing Capture, Restore, compatibility, verification, and Profile Git safety boundaries; the descriptive Sync Plan is never direct mutation authority.

The running system remains the configuration editor. Git is the shared desired-state history and transport, not the semantic merge engine.

## Phase boundary

This spec implements Roadmap Phase 10 only.

### In scope

- one Git-backed profile repository;
- one configured upstream tracking branch per machine;
- explicit fetch before remote-aware inspection;
- machine-local last-reconciled revision cursor;
- semantic profile views at cursor, local `HEAD`, and fetched upstream revision;
- one live machine observation cycle;
- deterministic target-level reconciliation;
- bootstrap when no cursor exists;
- interactive conflict resolution;
- CLI and TUI presentation of the same Sync Plan domain;
- interactive execution through existing Capture/Restore/Profile Git primitives;
- semantic reconciliation commits when local and remote committed intent both advanced;
- cursor advancement only after verified semantic settlement;
- focused two-machine sync acceptance coverage.

### Explicitly out of scope

- unattended timers, services, or daemon behavior;
- Auto Follow, Auto Publish, or Auto Reconcile policy;
- persisted per-target automation permission;
- persisted conflict-resolution databases;
- multiple upstream branches or competing remotes;
- peer-to-peer sync;
- force push, automatic rebase, or history rewriting;
- generic Git textual merge as semantic authority;
- automatic conflict choice;
- persisted live-observation caches, TTLs, watchers, or subscriptions;
- weakening Reconstruction Assurance or redefining its same-runtime invariant;
- Migration Acceptance activation work.

Phase 11 may automate outcomes only after this engine is trustworthy. Automation permission remains conceptually separate from Capture policy, Restore policy, compatibility, and safety. A future remote profile change must not be able to grant itself unattended authority on a machine.

## Existing architecture and constraints

The implementation must fit existing boundaries rather than bypass them:

- `internal/profile` owns profile loading, validation, schema compatibility, typed desired state, explicit desired-absence tombstones, machine overlays, and portable Capture/Restore policy.
- `internal/policy` resolves Capture and Restore as independent axes. Sync v1 consumes those axes; it does not add an automation axis.
- provider-owned target inspection already exposes target identities without making presentation layers understand profile serialization.
- `internal/observation` and workflow read cycles provide short-lived shared read observations. Sync should use one logical read cycle for the live machine and must not add a persisted observation cache.
- authoritative Capture/Restore paths already fresh-reload, re-plan, compare approvals, apply, and verify. Sync execution must preserve those fresh authority boundaries.
- `internal/profilegit` already provides exact-root Git status, explicit fetch, clean `--ff-only` pull, scoped commits, and push. Sync may add narrow Git primitives but must preserve their fail-closed behavior.
- existing machine binding is stored outside the portable profile in a state directory keyed by canonical profile root. The sync cursor should follow that precedent and remain machine-local.

## Alternatives considered

### A. Semantic three-way profile views plus live machine observation — selected

Materialize the cursor revision, local `HEAD`, and fetched upstream revision as isolated profile trees. Load them with the normal profile loader, project deterministic provider-owned semantic target facts, combine them with one live observation, and reconcile by semantic target identity.

Benefits:

- profile layout and Git paths do not become sync semantics;
- target-level policy and provider normalization remain reusable;
- explicit desired absence remains distinct from unmanaged state;
- future profile serialization changes do not require redesigning the reconciliation algorithm;
- later Auto Sync can consume the same trusted engine.

Cost: every current provider needs a deterministic sync-state projection, and historical profile trees must be materialized safely.

### B. Git-diff-driven reconciliation — rejected

Use `git diff cursor..HEAD` and `cursor..upstream`, then map changed paths back to semantic targets.

Rejected because one file may encode many targets, one target may span files, schema/layout changes leak into sync behavior, and Git-file conflicts are not the same as semantic desired-state conflicts.

### C. Persisted per-target change ledger/version vectors — deferred

Persist every semantic change as a new journal and reconcile journals.

This could provide richer provenance later but changes profile format, Capture transactions, migration requirements, and long-term storage semantics. It is not required for Phase 10.

## Core invariants

1. **Semantic truth, not filesystem truth.** Reconciliation operates on provider-normalized semantic target states, never raw Git path equality alone.
2. **Fetch is inspection preparation, not checkout mutation.** Sync inspection may update cached remote refs but must not pull, merge, rebase, reset, or modify profile files.
3. **Profile first, then machine.** Any applicable remote/local-profile intent accepted for machine mutation is represented in the checked-out profile before Restore mutates the machine. Targets explicitly inapplicable under reviewed machine policy may intentionally differ from portable desired state.
4. **Review is descriptive; mutation authority is fresh.** Execution revalidates reviewed Git/profile inputs and fresh-replans Capture/Restore actions before mutation.
5. **No silent conflict resolution.** Different local and remote semantic intent for the same target requires an explicit user choice or remains deferred.
6. **No silent lineage repair.** Missing/re-written cursor history, unrecognized ancestry, wrong upstream, or stale review stops for reinspection/rebaseline.
7. **Policy does not erase evidence.** Sync first classifies semantic change, then applies effective Capture/Restore policy to determine actionability or intentional inapplicability.
8. **Cursor advancement is earned.** Viewing, fetching, reviewing, pulling, committing, or partially applying work does not by itself advance the cursor.
9. **No force.** Sync v1 never force-pushes, rebases automatically, rewrites shared history, or uses a textual merge result as semantic authority.
10. **One execution per profile state root.** Interactive execution uses an exclusive local lock so two Sync transactions cannot mutate the same profile concurrently.

## Terminology and revision model

For one selected machine and one configured upstream branch:

- `B` — **base**, the profile revision recorded by the machine-local reconciliation cursor;
- `L` — **local**, current committed profile `HEAD`;
- `R` — **remote**, the exact fetched upstream revision reviewed by the Sync Plan;
- `M` — **machine**, current live semantic state observed for this machine.

The upstream is the branch tracked by the current local branch, normally `origin/main`. Sync v1 does not search or arbitrate among branches.

The cursor is a revision cursor, not an “inspected revision” marker. Unresolved/deferred work keeps it behind even if some other targets from later revisions have already converged.

## Machine-local cursor

Persist sync state next to the existing profile-keyed machine binding state, in a separate file such as:

```text
$STATE_HOME/omarchy-blueprint/profiles/<profile-root-hash>/sync.toml
```

The precise filename/API is implementation-plan detail, but the persisted state must be separate from the portable profile.

Minimum correctness fields:

- state format version;
- selected machine identity used for reconciliation;
- last reconciled commit SHA;
- local branch identity;
- configured upstream tracking ref;
- non-secret remote identity sufficient to detect switching the profile to a different upstream repository.

Do not persist credentials, raw authenticated URLs, access tokens, live observation data, conflict choices, or provider secrets in sync state.

Changing machine binding, branch/upstream identity, or remote repository identity invalidates automatic reuse of the cursor. The user must re-bootstrap or explicitly reconcile the new lineage.

The cursor commit must exist locally and must be an ancestor of both `L` and `R`. If not, Sync reports a history/baseline blocker. It must not infer a replacement base from merge-base or current `HEAD` without explicit user action.

## Bootstrap with no cursor

A machine without sync state has no trustworthy three-way base.

Sync presents an explicit initialization flow against the current fetched upstream revision:

1. **Reconcile this machine against the upstream revision.** Differences are inspected as Capture/Restore decisions. The cursor is written only after the resulting state is settled.
2. **Adopt current machine intent as the baseline.** This is not “mark HEAD reconciled.” Blueprint first captures/commits/publishes the reviewed local intent as required, then records the resulting shared revision after settlement.
3. **Cancel.** No state is written.

Blueprint may automatically initialize the cursor to the current shared revision only if it can prove that:

- local `HEAD` and the fetched upstream revision identify the same shared desired revision;
- the selected machine resolves coherently;
- the relevant machine state is semantically converged with that effective desired state; and
- there are no blockers or unresolved applicable targets.

Otherwise initialization remains explicit.

## Historical profile revision views

`profile.Load` is filesystem/directory based. Sync should preserve that contract.

A Git Revision Store materializes each required commit into an isolated temporary read-only profile tree, without changing the working tree or index. The normal profile loader then validates schema and desired state exactly as it does for the checked-out profile.

Requirements:

- resolve each reviewed revision to an exact commit SHA before loading;
- materialize only from the local object database after fetch;
- never checkout the historical revision into the user's profile directory;
- remove temporary materializations after the read lifecycle;
- fail if required profile files cannot be materialized or loaded;
- do not treat unsupported historical schema as empty/unmanaged state;
- preserve profile loader migration/read compatibility behavior without writing migrated data back to the commit snapshot.

The implementation may use Git archive/plumbing or an equivalent isolated mechanism. The implementation plan must choose and test the concrete primitive.

## Semantic target state

Every current captured provider participating in Sync v1 must expose a deterministic semantic snapshot for reconciliation. Unsupported captured categories are blockers, not silently ignored state.

Target identity is Blueprint's existing provider-owned semantic identity:

```text
(category, target)
```

Examples include package refs, service unit IDs, config logical paths, hook paths, resource IDs, default-app keys, theme/plugin IDs, and other provider-native target keys.

A profile target state has three logical forms:

```text
Unmanaged
DesiredPresent(value/fingerprint)
DesiredAbsent(value/provenance fingerprint)
```

`Unmanaged` means the profile expresses no desired state for this target. `DesiredAbsent` is explicit intent and must remain distinguishable from unmanaged state. Existing tombstone/provenance semantics remain provider-owned.

The live machine projection exposes the corresponding observed semantic facts needed to compare with profile desired state. Providers own normalization and semantic equality. Sync owns orchestration and classification; it must not reimplement package origin rules, service normalization, config hashing, resource Git state, or other provider semantics.

Semantic projections must be deterministic and insensitive to irrelevant serialization order. Volatile metadata such as timestamps is not a target value.

## Control-plane profile state

Not every profile change is a runtime target. Machine definitions, machine-specific path overrides, Capture/Restore policy, and other profile control-plane semantics can change how target state is interpreted.

Sync v1 must therefore distinguish:

- **runtime target state**, reconciled at `(category, target)` granularity; and
- **control-plane state**, which affects machine selection, policy, target interpretation, or profile validity.

One-sided control-plane changes may be reviewed and followed/published, but any change that alters effective authority or target interpretation must invalidate/rebuild the affected Sync Plan after the profile checkout changes. If both local and remote changed the same control-plane semantic from `B` to different values, v1 blocks for explicit resolution outside the automatic semantic merge path; it does not invent policy or use file-level Git conflict resolution. Identical two-sided control-plane results may converge.

Remote control-plane changes are never treated as pre-approval for future unattended authority. Phase 11 automation authority must remain separate and machine-local.

## Live observation lifecycle

A Sync inspection uses one explicit workflow read cycle for the current machine and derives the needed live semantic facts from that cycle. It should reuse the shared immutable observations and bounded read-provider scheduling already established by Observation Performance v1.

Do not implement Sync as repeated calls to Overview, Status, Capture preview, and Restore preview that independently re-observe the same machine.

The inspection read cycle is presentation/planning state only. Any later mutation must fresh-observe/reload/replan at the existing authority boundary.

## Desired-history classification

For each semantic target, first compare the desired-state history `B -> L` and `B -> R` independently of live machine drift:

| Local desired state | Remote desired state | History classification |
| --- | --- | --- |
| `L == B` | `R == B` | no desired-history change |
| `L != B` | `R == B` | local desired intent |
| `L == B` | `R != B` | remote desired intent |
| `L != B` | `R != B`, `L == R` | converged desired intent |
| `L != B` | `R != B`, `L != R` | sync conflict |

Equality is provider semantic equality, not byte equality.

Next compare live `M` to the relevant desired states to distinguish already-converged intent from machine drift. Representative outcomes:

- profile unchanged, machine changed -> Capture candidate;
- remote desired changed, machine still matches base -> Restore candidate;
- local `HEAD` changed and machine already matches local desired state -> Publish candidate;
- local `HEAD` changed, remote unchanged, and machine still matches base -> Publish plus Restore-local-profile candidate;
- local `HEAD` changed but machine differs from both base and local desired state -> local intent conflict requiring review between keeping machine state (Capture), keeping committed profile intent (Restore), or Deferring;
- both sides changed identically and machine matches the result -> converged;
- both sides changed differently -> explicit sync conflict before mutation.

The implementation plan must pin a full deterministic decision table and test it exhaustively for the supported semantic state forms.

## Policy application

Classification answers **what changed**. Policy answers **what Blueprint may do about it on this machine**.

Apply effective Capture and Restore policy only after semantic classification.

Examples:

- a remote desired change with Restore disabled remains visible but is intentionally inapplicable on this machine;
- machine drift with Capture disabled is not silently captured; if Restore is enabled, restoring the current desired state may be offered instead;
- policy must not turn a semantic conflict into convergence;
- policy-disabled applicable actions must be represented explicitly in the plan rather than dropped.

A target can count as settled for cursor advancement when its differing intent is explicitly inapplicable under the machine's reviewed effective policy. Where more than one policy-permitted resolution exists, Sync presents the alternatives rather than silently choosing one. A policy change that would alter either conclusion invalidates the approval and requires reinspection.

## Sync Plan

`SyncPlan` is an immutable review artifact produced from one consistent inspection snapshot.

At minimum it records:

- exact `B`, `L`, and `R` SHAs;
- selected machine identity;
- branch/upstream/remote identity;
- Git cleanliness/ahead/behind/divergence facts;
- policy/control-plane fingerprint sufficient to detect review invalidation;
- deterministic target items;
- blockers;
- whether bootstrap is required.

Each target item carries enough information for presentation and later revalidation:

- target key;
- history classification;
- base/local/remote semantic state summaries;
- live machine state summary;
- effective Capture policy;
- effective Restore policy;
- resolution: `none`, `keep-local`, `accept-remote`, or `defer` where applicable;
- resulting planned action such as `none`, `capture`, `publish`, or `restore`;
- provider detail handle/summary for richer inspection.

Resolution choices are session/invocation state in v1. They are not written to the cursor or a separate durable conflict database.

`Defer` leaves the conflict reproducible from durable truth on the next run. Deferred applicable intent prevents cursor advancement through the revision that introduced it.

## Conflict-resolution UX

Sync is a dedicated decision workspace, not merely another Profile Git status page.

The TUI should group target items into understandable sections such as:

- conflicts;
- remote/follow candidates;
- local/capture candidates;
- local committed/publish candidates;
- already converged/inapplicable items;
- blockers.

Selecting a target shows Base, Local, Remote, and Live Machine semantic detail. Providers may add richer presentation such as config text diff, service enablement state, package declaration differences, or resource revision details.

For a local-vs-remote semantic conflict the user may choose:

- **Keep local** -> local intent becomes the accepted desired outcome and is carried into Capture/profile reconciliation;
- **Accept remote** -> fetched upstream intent becomes the accepted desired outcome and is carried into Restore/profile reconciliation;
- **Defer** -> no mutation for that conflict and no cursor advancement past it.

For committed-local-profile intent that disagrees with current machine state while remote is unchanged, the equivalent choices are **Keep machine** (Capture into the profile), **Keep profile** (Restore the machine from committed local intent), or **Defer**. Sync must make that distinction explicit rather than treating every local `HEAD` edit as already accepted machine authority.

Choosing a resolution selects intent only. It does not immediately mutate the profile or machine.

After choices, the workspace shows a single execution summary: Git reconciliation required, targets to capture/publish/restore, deferred items, and blockers. Existing Restore options, compatibility, activation review, and approval UI remain authoritative where applicable.

CLI and TUI consume the same Sync Plan domain. Exact command spelling is implementation-plan detail; the CLI must support inspection, explicit target resolution, and interactive/explicit execution without creating a second semantic implementation.

## Git inspection behavior

A remote-aware Sync inspection:

1. validates exact profile Git repository and configured upstream;
2. fetches the configured remote explicitly;
3. resolves `L` and the upstream tracking ref to exact SHAs;
4. reads cursor `B`;
5. performs ancestry and cleanliness checks;
6. materializes/loads `B`, `L`, and `R` without modifying the checkout;
7. observes the machine and builds the Sync Plan.

Inspection never runs pull, merge, rebase, reset, checkout, or profile Save.

A dirty profile working tree blocks Sync execution. V1 does not attempt to infer whether uncommitted profile edits are valid desired state. The user commits or discards them first using existing tools.

Committed local profile changes ahead of upstream are first-class local desired-state intent. They must not be discarded merely because they did not originate from Blueprint Capture.

## Execution authority and staleness

The reviewed Sync Plan is never sufficient mutation authority by itself.

When the user chooses Execute, acquire an exclusive local per-profile lock and revalidate at least:

- cursor state unchanged;
- selected machine unchanged;
- branch/upstream/remote identity unchanged;
- local `HEAD` still equals reviewed `L`;
- working tree remains clean;
- fetched upstream tracking ref still equals reviewed `R`;
- relevant policy/control-plane inputs still match the reviewed plan.

If any reviewed input changed, stop before mutation and require reinspection. Do not automatically fetch a newer revision and continue under the old approval.

After each profile-history mutation, reload the profile and rebuild any subsequent authoritative machine plan from fresh current state.

## Execution: simple remote-ahead case

When local `HEAD` can be fast-forwarded exactly to the reviewed `R`, execution may:

1. fast-forward the checkout to **that exact reviewed SHA** without an implicit network fetch;
2. reload the profile;
3. fresh-observe and Capture only targets whose accepted resolution requires local intent to be represented in the updated profile;
4. commit managed profile changes if Capture changed desired state;
5. push if local desired history was created;
6. fresh-build the authoritative Restore plan for accepted remote intent;
7. show normal Restore approval/compatibility/safety information;
8. apply and verify through the existing Restore path;
9. verify upstream publication/reachability of the final shared revision;
10. advance the cursor only if all applicable intent through that revision is settled.

Do not use a generic `git pull --ff-only` that can fetch and advance to a newer unreviewed remote SHA during execution. Profile Git should gain a narrow pinned fast-forward primitive or equivalent exact-SHA operation.

## Execution: local and remote committed intent both advanced

If `L` and `R` both descend from `B` but neither can fast-forward to the other, ordinary `--ff-only` cannot preserve both histories.

After all semantic conflicts are explicitly resolved, Sync creates a **semantic reconciliation commit** with:

- parent 1: reviewed local commit `L`;
- parent 2: reviewed remote commit `R`;
- tree: canonical profile state generated from the accepted semantic resolution.

Git provides ancestry only. Git textual merge does not choose the tree contents.

Requirements:

- same-target differing semantic intent requires explicit resolution;
- disjoint semantic changes may be combined;
- identical semantic results are converged;
- control-plane differences must be deterministically resolvable or block;
- resulting profile must pass normal load/validation before commit;
- only Blueprint-managed profile paths are authored/staged by the reconciliation transaction;
- if divergent `L`/`R` histories contain committed differences outside Blueprint-managed profile paths, semantic reconciliation blocks and requires ordinary Git/user resolution rather than choosing or dropping those unrelated files;
- unrelated pre-existing staged or working-tree changes remain blockers;
- no force push or automatic rebase.

The implementation plan must choose a concrete safe mechanism for constructing the two-parent reconciliation commit and prove that unapproved/unmanaged files cannot enter its tree.

## Execution ordering

For mixed local and remote intent, the required high-level order is:

```text
fetch + inspect + resolve
        -> revalidate reviewed revisions
        -> reconcile profile history / represent accepted remote desired state
        -> reload profile
        -> fresh Capture accepted local machine intent
        -> commit resulting desired state
        -> push shared desired state
        -> fresh Restore plan for accepted remote intent
        -> approve
        -> apply
        -> verify
        -> advance cursor
```

The profile must represent the final accepted desired state before Restore applies that state to the machine.

Publish-before-follow is deliberate: if publication fails, remote desired state is not applied to the machine under a profile history that the shared upstream did not accept.

## Failure behavior

All failures are fail-closed with no cursor advancement.

| Failure | Required behavior |
| --- | --- |
| Fetch/network failure | no checkout/machine mutation; report remote unavailable |
| Cursor commit missing | baseline/history blocker; explicit rebootstrap required |
| Cursor not ancestor of `L` or `R` | history blocker; do not invent merge-base cursor |
| Dirty profile repository | execution blocker; user resolves Git state |
| Detached HEAD/wrong or absent upstream | Git configuration blocker |
| Historical profile cannot load | profile/history blocker |
| Unsupported captured provider sync projection | blocker; never ignore category |
| Unresolved semantic conflict | may remain deferred; prevents cursor advancement past affected revision |
| Reviewed `HEAD`/upstream/policy/machine changed | stale plan; stop and re-inspect |
| Push rejected because upstream advanced | stop; do not force/refetch-and-continue under old approval |
| Restore compatibility/safety blocker | preserve existing Restore behavior; cursor stays behind |
| Restore apply/verify failure after successful push | shared desired state remains published; machine remains unsettled; cursor stays behind |
| Cancellation | join/clean local operation lifetime; no partial result reported as settled |

If a push succeeds but subsequent Restore fails, the next Sync inspection must be able to recognize the already-published desired state and present only the remaining machine work rather than treating the published revision as lost.

## Cursor advancement

The cursor advances to revision `X` only if all of the following are true:

- `X` is the exact final desired-state revision produced/reviewed by the transaction;
- `X` is reachable from the configured upstream after push/verification;
- local profile checkout represents `X` or a later proven-equivalent shared reconciliation result defined by the transaction;
- every applicable target through `X` is semantically settled on this machine, or explicitly inapplicable under the reviewed effective machine policy;
- no deferred semantic conflict remains through `X`;
- required Restore verification succeeded;
- no transaction blocker remains.

Reviewing or partially executing a plan never advances the cursor.

Cursor writes must be atomic and machine-local. A crash before cursor write may cause safe repeated inspection/work; it must not cause Blueprint to claim settlement that was not recorded.

## TUI integration

Add a dedicated Sync workspace reachable from the main TUI rather than overloading Profile Git Sync status.

Presentation should preserve existing TUI conventions for:

- loading states;
- target lists/details;
- modal confirmation;
- Restore preview and compatibility presentation;
- keyboard navigation;
- cancellation.

Opening the TUI must remain fast. Sync remote fetch/inspection should begin only when the user enters/refreshes the Sync workspace or explicitly requests remote-aware sync inspection; it must not add a network dependency to normal Overview startup.

The current observation performance invariant remains: read-only Sync inspection should create one logical observation cycle, not serially rerun expensive provider observations for each view.

## CLI integration

Expose the same Sync domain through CLI commands suitable for scripting and tests. Human-readable and JSON output must include exact revision identities, machine identity, target classifications, blockers, and resolutions/actions.

CLI execution must require explicit resolution/approval inputs and must not silently choose conflict outcomes. Machine mutation continues to use existing Capture/Restore authority checks.

Exact subcommand names/flags and whether one-run resolution selections are encoded as repeated flags or an ephemeral plan artifact are implementation-plan decisions. V1 must not create a durable conflict-choice database.

## Concurrency and locking

Inspection may use existing bounded read-provider concurrency inside one read cycle.

Mutation is serialized per canonical profile state root. Acquire an exclusive local lock before execution revalidation and keep it across profile-history mutation, Capture, push, Restore, verification, and cursor advancement. Cancellation/error must release it.

This lock is local coordination, not distributed Git locking. Upstream races are still detected by ordinary push/ref checks and cause a stop/reinspection.

No global daemon/process-wide semaphore is introduced by this project.

## Security and privacy

- Never persist Git credentials or authenticated remote URLs in sync state.
- Reuse existing sanitized remote display behavior.
- Temporary historical profile materializations inherit profile sensitivity; create them with appropriately private permissions and remove them promptly.
- Debug diagnostics, if extended for Sync, must not print profile contents, target values containing secrets, Git credentials, environment variables, or command output bodies by default.
- A remote profile commit may express desired state and Capture/Restore policy, but v1 remains interactive. Future unattended authority must be separately granted locally and must not be remotely self-authorizing.

## Testing strategy

### Pure reconciliation tests

Build a table-driven/exhaustive suite around the pure semantic reconciler. Cover:

- `B/L/R/M` equality/difference combinations;
- unmanaged vs desired-present vs desired-absent;
- local-only, remote-only, identical two-sided, and divergent two-sided intent;
- machine drift relative to base/local/remote;
- Capture/Restore policy enabled/disabled combinations;
- inapplicable vs blocked vs deferred outcomes;
- deterministic ordering independent of map/serialization order;
- control-plane invalidation cases.

No subprocesses are needed for these tests.

### Provider contract tests

For every current captured provider, prove:

- stable semantic target identity;
- stable normalization/fingerprint for semantically equal state;
- explicit absence differs from unmanaged state where supported;
- projection of historical profile state requires no live mutation;
- live projection reuses read observations where available;
- output ordering is deterministic.

### Git integration tests

Use a temporary bare Git repository and at least two profile clones to exercise real ancestry:

- fetch without checkout mutation;
- bootstrap;
- cursor ancestry and missing cursor commit;
- remote-ahead pinned fast-forward;
- local-ahead publish;
- local+remote disjoint semantic reconciliation commit;
- divergent history with committed non-Blueprint-managed path differences blocks rather than dropping/merging them;
- same-target semantic conflict;
- push race after review;
- history rewrite/divergence;
- dirty/staged/untracked blockers;
- unmanaged-file exclusion from reconciliation commits;
- crash/error before cursor advancement.

### Workflow authority tests

Prove that:

- descriptive SyncPlan is not reused as Restore authority;
- execution reloads/reobserves after profile-history mutation;
- stale `HEAD`, upstream, machine binding, or policy invalidates execution;
- Capture selection cannot expand beyond accepted local targets;
- Restore selection cannot expand beyond accepted remote targets without new approval;
- compatibility and existing Restore safety behavior remain intact;
- cursor advances only after final verification.

### TUI/CLI tests

Cover:

- conflict grouping/detail and Keep local / Accept remote / Defer choices;
- bootstrap UX;
- no network fetch on normal TUI startup;
- exact revision/blocker presentation;
- cancel/re-enter reconstructs unresolved conflict from durable truth;
- CLI/JSON and TUI consume equivalent plan outcomes.

### Two-machine acceptance

Add focused Sync acceptance separate from Reconstruction Assurance.

Representative scenario:

1. machines A and B start settled at shared revision `X`;
2. A changes package `P`, Sync captures/publishes, cursor advances;
3. B fetches, sees remote-only `P`, follows/restores, cursor advances;
4. A changes config target `C` locally;
5. B changes the same `C` differently and publishes;
6. A sees a semantic conflict and chooses Keep local;
7. Sync builds/pushes the resolved shared profile revision and verifies A;
8. B follows the resolved intent;
9. both machines converge semantically and their cursors identify the shared settled revision.

This acceptance work may use two logical/test environments initially; any real-VM expansion must remain separate from the existing same-runtime Reconstruction Assurance contract.

## Reconstruction Assurance and Migration Safety

Reconstruction Assurance remains the same-runtime invariant and is not repurposed as a Sync test harness. Do not alter its source/target same-ready-runtime requirement for this project.

Migration Safety/Compatibility remains the cross-runtime authority boundary for Restore. Sync must surface and preserve compatibility blockers when following remote desired state; it does not override or reinterpret them.

A future multi-machine VM acceptance layer may share low-level helpers only after a separately reviewed design justifies that reuse.

## Performance expectations

No hard latency budget is established by this spec, but Sync inspection must preserve the architectural gains from Observation Performance v1:

- one logical machine observation cycle per inspection;
- no O(targets) Git checkout/materialization loop;
- profile revisions materialized once per unique SHA per plan;
- deterministic semantic projections may be derived from loaded profile data without re-reading the live machine;
- normal TUI startup remains free of Sync network work.

Performance regressions that cause repeated Services/Packages observations for one Sync inspection are architectural bugs, not acceptable trade-offs.

## Success criteria

Sync Engine v1 is complete when a user with two machines sharing one Git-backed Blueprint profile can, from CLI or TUI:

1. establish a trustworthy machine-local reconciliation baseline;
2. explicitly fetch and inspect local machine, committed local profile, and upstream desired-state changes against that baseline;
3. see changes at semantic target granularity, including explicit absence and machine-specific policy effects;
4. resolve same-target conflicts with Keep local, Accept remote, or Defer;
5. preserve committed manual local profile intent rather than discarding it;
6. reconcile disjoint committed local/remote desired state without using Git textual merge as semantic authority;
7. execute accepted intent through existing Capture/Restore compatibility, approval, fresh-planning, apply, and verification boundaries;
8. publish shared desired state without force push/rebase/history rewriting;
9. survive push races and partial failures without falsely advancing reconciliation state;
10. advance the machine-local cursor only after verified semantic settlement;
11. demonstrate multi-machine convergence in focused acceptance coverage while leaving Reconstruction Assurance unchanged.

## Future Phase 11 constraints

This spec intentionally stops before unattended operation. A later Auto Sync design may schedule repeated Sync Engine inspection/execution only if it preserves these constraints:

- automation permission is independent from Capture/Restore policy;
- unattended authority is locally granted and cannot be enabled solely by a remote profile commit;
- conflicting intent, unsafe/high-risk operations, compatibility blockers, Git divergence, stale plans, and interactive requirements stop for review;
- Auto Follow and Auto Publish consume the same semantic reconciler and fresh mutation authority rather than reimplementing sync as a timer around `pull/capture/restore/push`;
- systemd user timers/services, if used, are an execution mechanism rather than the synchronization architecture.

## Decision summary

Sync Engine v1 uses a machine-local last-reconciled revision cursor and four-way evidence (`B`, `L`, `R`, `M`) to produce a target-level semantic reconciliation plan. Historical profile revisions are loaded through isolated filesystem materializations; providers own target identity and semantic equality; the workflow observation engine supplies one short-lived live machine view. Inspection may fetch but never pulls or mutates the checkout. Execution pins reviewed revisions, revalidates staleness, represents accepted desired state in Git first, captures accepted local machine intent, publishes, then fresh-plans/restores accepted remote intent. When committed local and remote histories both advanced, Blueprint constructs a two-parent semantic reconciliation commit whose tree comes from resolved Blueprint intent rather than Git textual merge. The cursor advances only after the resulting shared revision is published and the machine is verified settled through it.
