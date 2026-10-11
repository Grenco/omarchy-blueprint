# ADR 0027: Semantic Sync Reconciliation and Machine-Local Cursors

## Status

Accepted by the human on 2026-10-03 with the approved Sync Engine v1 design. The implementation plan is separately review-gated. This ADR does not authorize product implementation, repository mutation, profile mutation, unattended synchronization, tags, releases, or publication.

## Context

Profile Git Sync already provides safe Git transport for one Blueprint profile repository: explicit fetch, clean fast-forward pull, managed-path commits, and push. That is not sufficient for multi-machine synchronization. A timer that runs `pull -> restore` would erase the distinction between local machine intent, committed local profile intent, remote profile intent, policy, compatibility, and fresh mutation authority.

Sync Engine therefore needs a trustworthy reconciliation base. For one selected machine and one configured upstream branch it must reason about four states:

- `B`: the last profile revision through which this machine is known to be semantically settled;
- `L`: current committed local profile `HEAD`;
- `R`: the exact fetched upstream revision reviewed by the user;
- `M`: current live machine state.

Raw Git paths are not the product's semantic unit. Providers already own target identity, normalization, desired-absence provenance, Capture/Restore policy interpretation, planning, and verification. A Git textual merge can be correct as a file merge while still being wrong as desired state.

The reconciliation base is also machine-specific state. Sharing it through the portable profile would make one machine's settlement claim appear to be another machine's fact.

## Decision

1. **Reconcile semantic desired state, not Git files.** Sync compares provider-normalized target state at `B`, `L`, and `R`, plus live `M`, keyed by Blueprint semantic target identity `(category, target)`. Profile target state distinguishes `Unmanaged`, `DesiredPresent`, and `DesiredAbsent`.

2. **Keep one machine-local reconciliation cursor.** The cursor means: **“this machine is semantically settled through profile revision X.”** It is stored outside the portable profile beside the profile-keyed machine binding state. It records the reconciled commit and enough machine/branch/upstream/remote identity to reject accidental reuse after lineage changes. Review or fetch alone never advances it.

3. **Use one configured upstream branch.** Sync v1 fetches the current branch's configured upstream and resolves it to an exact commit SHA. Inspection may update cached remote refs, but it never pulls, merges, rebases, resets, or modifies the checked-out profile.

4. **Materialize historical revisions without changing the checkout.** `B`, `L`, and `R` are loaded through isolated profile trees and the normal profile loader. Unsupported or invalid historical profile state is a blocker, not empty state.

5. **Classify history before applying policy or machine drift.** For each target, compare `B -> L` and `B -> R` to identify local intent, remote intent, identical convergence, or semantic conflict. Then compare `M` and apply Capture/Restore policy to derive actionable work or intentional inapplicability. Policy never erases the fact that a semantic change occurred.

6. **Resolve conflicts explicitly at target granularity.** Interactive choices are `Keep local`, `Accept remote`, or `Defer`. Choosing a resolution selects desired intent; it is not mutation authority. Resolution choices are session-local in v1 and are recomputed from durable profile history and machine state on a later run.

7. **Revalidate exact reviewed inputs before mutation.** Sync execution takes an exclusive profile-scoped local lock and checks the cursor, selected machine, local `HEAD`, clean worktree/index, branch/upstream identity, and exact reviewed upstream SHA. Staleness stops execution and requires reinspection.

8. **Represent accepted desired state in the profile before following it on the machine.** A remote-only reviewed revision is applied with a pinned fast-forward to the exact reviewed SHA, not a generic networked pull. When both `L` and `R` advanced from `B`, Blueprint creates a semantic reconciliation merge commit whose parents are `L` and `R` but whose Blueprint-managed tree is generated from the reviewed semantic resolution. Git supplies ancestry only; a textual Git merge never chooses desired state.

9. **Block unsafe divergent non-managed history.** If divergent committed history contains changes outside Blueprint-managed profile paths, Sync v1 does not silently keep, drop, or merge them. The user must resolve that Git history outside the semantic Sync transaction and reinspect.

10. **Reuse fresh Capture/Restore authority.** After profile history is reconciled, accepted live local intent is freshly captured, committed, and pushed as required. Accepted remote intent is then freshly replanned through existing Restore compatibility/safety/approval and verification. The descriptive Sync Plan is never passed through as direct Apply authority.

11. **Publish before local follow in mixed transactions.** The final desired revision must be successfully published before Sync mutates the machine toward remote intent. If push loses a race, Sync stops before Restore. If Restore later fails, the published desired state remains valid but the cursor stays behind.

12. **Advance the cursor only after settlement.** The final revision must be reachable on the configured upstream and every applicable target through it must be converged, verified, or intentionally inapplicable by reviewed policy. Deferred conflicts, compatibility blockers, failed verification, stale inputs, or unfinished actions keep the cursor behind.

13. **Keep automation authority out of Phase 10.** Sync Engine v1 is interactive. Auto Follow, Auto Publish, schedules, timers, unattended permissions, and automatic conflict choice remain Phase 11+ concerns and must not be inferred from portable Capture/Restore policy.

## Consequences

Positive:

- Multi-machine sync uses the same semantic identities and safety boundaries as Capture/Restore instead of inventing a filesystem-sync model.
- Machine-local drift, committed local profile edits, and remote profile edits remain distinguishable.
- Explicit desired absence remains meaningful rather than collapsing into “not present in this file.”
- A reviewed remote SHA cannot silently change between preview and execution.
- Divergent but semantically compatible profile histories can converge without rebasing or force-pushing.
- Later Auto Sync can consume a deterministic reconciliation engine instead of automating Git commands directly.

Costs and limits:

- Every captured provider needs a deterministic Sync semantic projection and enough composition support to rebuild a reconciled managed profile tree.
- Historical revisions must be materialized and validated, increasing read-path complexity and temporary I/O.
- Semantic reconciliation commits need strict transaction/rollback tests because they alter Git ancestry and the checked-out profile.
- Cursor advancement can remain behind even after some newer target changes have converged; that conservatism is intentional.
- Sync v1 supports one tracking branch and does not solve arbitrary Git topology, non-managed divergent history, or unattended automation.
- Two authority layers intentionally remain: a descriptive Sync Plan and fresh Capture/Restore mutation authority.

## Rejected alternatives

- **Timer-driven `pull -> restore` / `capture -> push`:** conflates transport with semantic authority and can overwrite local intent.
- **Git-path-diff reconciliation:** profile layout is not the semantic domain and one target can span or share serialized files.
- **Generic Git textual merge:** Git cannot decide provider semantic conflicts or policy meaning safely.
- **Advance the cursor to the latest inspected revision:** would make unresolved work disappear from the reconciliation base.
- **Infer a new base from merge-base after rewritten/missing cursor history:** silently changes the meaning of “settled”; explicit re-bootstrap is required.
- **Persist conflict choices as a second durable database:** creates state that can itself diverge from profile history and live state; v1 recomputes instead.
- **Automatic rebase, force push, or history rewrite:** weakens review and can discard shared intent.
- **Per-target change ledger/version vectors:** potentially useful later, but adds profile-schema and capture-transaction complexity not required for Phase 10.

## References

- [Sync Engine v1 design](../planning/specs/2026-10-03-sync-engine-v1-design.md)
- [Sync Engine v1 implementation plan](../planning/plans/2026-10-03-sync-engine-v1-implementation-plan.md)
- ADR 0020: CLI policy and Capture parity.
- ADR 0023: Plan-integrated migration compatibility assessment.
- ADR 0024: Semantic user-service reconstruction.
- ADR 0026: Shared read observations and fresh mutation authority.
