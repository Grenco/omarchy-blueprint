# ADR 0026: Shared Read Observations and Fresh Mutation Authority

## Status

Accepted by the human on 2026-10-02 with the observation-performance spec and implementation plan. PR A is implemented; the human subsequently authorized pushing its branch, publishing these planning documents and opening a PR. PR B remains behind A's review/merge gate. No broader external repository mutations are authorized.

PR A #61 is now approved/merged. PR B is implemented locally; publication and
merge remain human gates. Review clarified that Resources' machine selection
and presentation metadata must consume the private read snapshot explicitly.

## Context

Services discovery is dominated by one serial systemctl show process per non-template identity. Packages and Services can also be discovered repeatedly while computing Diff, targets, and read-only Restore classification. Current main can conditionally spend about 10 seconds computing an Overview despite rendering its first output in under 100 ms.

Independent reinspection has a separate, essential role at Capture/Restore mutation boundaries. Capture compares approved reviews and performs a post-staging fingerprint recheck. TUI Restore freshly replans before applying. CLI Restore uses PlanRestoreWithContext both to present state and to acquire fresh authority after approval. Cached presentation facts must not replace those checks.

Session.Reload currently changes shared profile/machine state during lazy Overview Restore classification. A future read scheduler or auto-sync consumer needs an explicit observation lifetime without relying on shared mutable Session state.

## Decision

1. Replace Services per-unit shows with measured bounded chunks and identity-aware mapping. Catalogue parsing, templates, ownership and normalization remain authoritative domain rules. Id and Names map unordered records; operand order is not evidence of identity.
2. Introduce an explicit, short-lived workflow ReadCycle backed by a small provider-neutral observation lifecycle/typed-slot package. Bind Services and Packages first; other providers retain their existing detection algorithms through narrow read views.
3. A cycle privately loads and validates desired profile/machine state once. It owns immutable observations acquired lazily and shares one in-flight operation per provider. It never reloads or publishes mutable Session state.
4. Read views expose Diff, targets, and preview Plan, plus their read capabilities. They expose no Capture, Apply, Verify, or transaction methods and are not authoritative Provider/RestoreProvider implementations.
5. Cache facts and non-cancellation errors only until the owning cycle closes. No globals, TTLs, persisted cache, or ambient context cache. Close cancels and joins loaders. Typed defensive copies preserve all live-only fields.
6. Keep Capture, post-stage recheck, Apply, CLI PlanRestoreWithContext revalidation, execution preconditions, and Verify fresh and uncached. Approved values are comparison inputs, not execution authority. Shared read projections do not replace fresh state construction or exact approval equality.
7. Resolve Services roots as local immutable inputs. Prepared Capture state remains confined to its serialized transaction path.
8. Re-measure batching and reuse before adding a read-only bounded provider scheduler. Progressive/stale presentation and auto-sync remain separate future decisions.

PR B integration clarification: an optional `ReadSnapshotBinder` gives Resources
an explicit frozen machine selection, including no-machine, without adding a
Resources observation cache. Consumers retain descriptive copied Profile/Machine
metadata and resolved preview options, and resolve read policy in the same
cycle. Capture reloads/validates authoritative desired state at transaction
entry; TUI profile-edit actions similarly reload before editing. Removing read
publication must never leave writes starting from stale Session desired state.

## Consequences

Positive:

- Read consumers share costly facts without changing Capture/Restore authority.
- Services process count scales with batches, not units.
- Future auto-sync can explicitly own a read cycle without inventing another cache.
- Snapshot lifetime, failures, cancellation and in-flight sharing have testable semantics.
- Read classification no longer mutates Session through Reload.

Costs and limits:

- Two deliberately distinct orchestration paths remain: presentation and authority. They share projection/domain helpers, not observations across the approval boundary.
- Typed cloning must preserve nested slices/maps and Packages fields excluded from serialization.
- A cycle is not an atomic machine/filesystem snapshot. Source provenance/fingerprint checks and authoritative preconditions remain necessary.
- Systemd identity mapping and aliases add parser tests; unsupported or ambiguous identity evidence fails safely.
- Stable read configuration and local provider inputs require a targeted mutable-state audit. This ADR does not make every Session mutation concurrent-safe.
- Chunking alone may not meet the aspirational 500 ms Services target on the profiled machine.

Rejected alternatives:

- Process-global or TTL caches blur freshness and lifetime.
- Context-only memoization risks observation reuse through inherited mutation contexts.
- Memoized final Restore plans bypass fresh construction and cannot substitute for approval checks.
- A universal provider observation API is unnecessary before additional providers demonstrate a need.

## References

- [Design](../planning/specs/2026-10-02-observation-performance-v1-design.md)
- [Implementation plan](../planning/plans/2026-10-02-observation-performance-v1-implementation-plan.md)
- ADR 0020: CLI policy and Capture parity.
- ADR 0021: Real-Omarchy Reconstruction Assurance.
- ADR 0022: Fresh-package readiness and elevated Restore.
- ADR 0023: Plan-integrated migration compatibility assessment.
- ADR 0024: Semantic user-service reconstruction.
