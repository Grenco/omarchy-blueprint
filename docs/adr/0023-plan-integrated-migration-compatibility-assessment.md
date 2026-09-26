# ADR 0023: Plan-Integrated Migration Compatibility Assessment

## Status

Proposed

## Context

Omarchy Blueprint's core product promise is:

> I want this machine to behave like that machine.

Reconstruction Assurance (ADR 0021) proves that promise on two independent machines only after both machines have reached the same ready Omarchy runtime. ADR 0022 deliberately preserves that symmetry: the install substrate is pinned, each machine follows Omarchy's supported readiness/update path, and Capture and Restore occur against the same ready runtime.

That is a reconstruction guarantee, not a migration guarantee.

Migration Safety / Compatibility v1 answers the next question:

> When a Blueprint profile was captured under one Omarchy environment and is restored under another, what does Blueprint actually know about whether the selected intent is still safe and meaningful to apply?

The current product already contains category-specific compatibility evidence, but it is fragmented:

- Shell records an explicit schema version and refuses unsupported schema changes with a migration-required error.
- Config carries captured baseline evidence and inspects current supported surfaces, ownership and baseline state.
- Built-in Themes and Plugins can be compared with target availability.
- Defaults use Omarchy's semantic getter/setter contract and already reject non-portable raw desktop IDs.
- Packages distinguish physical presence from repository-origin knowledge; ADR 0022 makes incomplete origin/readiness explicit and reduces Exact deletion authority when knowledge is incomplete.
- Resources carry portable copy/Git provenance and filesystem preconditions that are largely independent of Omarchy runtime identity.
- Hooks carry strong content/path provenance but currently have weaker evidence that a changed Omarchy runtime still consumes the same lifecycle.

The shared Restore workflow already provides the safety boundary Blueprint needs:

```text
inspect
→ calculate
→ preview
→ approve
→ re-inspect/recalculate
→ require stable authority
→ apply
→ verify
```

`ApplyApprovedRestore` replans and requires exact plan equality before mutation. Restore requirements already make external remediation visible without performing it.

The profile manifest also records an Omarchy `captured_version` and `channel`, but those values are not authoritative provenance for every desired target. A partial Capture can refresh the profile-level Omarchy metadata while Capture Preserve leaves previously captured target state and artifacts untouched. Treating that one version as the source runtime for every desired item would therefore overstate what Blueprint knows.

Profile schema compatibility, Blueprint binary compatibility, Omarchy runtime compatibility and category/provider semantic compatibility are separate concerns and must not be collapsed into one version comparison.

## Decision

### 1. Compatibility is a semantic Restore preflight, not a global version verdict

Migration Safety v1 introduces a shared compatibility assessment as part of Restore planning.

The assessment answers whether the **effective selected Restore intent** is supported by the evidence Blueprint has for the current target.

Blueprint does not declare two machines or two Omarchy versions globally "compatible." Version and channel information are context and may support a provider-specific finding only where a real provider/domain rule makes them authoritative.

A rule such as `source_version == target_version` or `same major version == compatible` is not a Migration Safety guarantee.

### 2. Compatibility has evidence state and authority effect

Compatibility findings use three evidence states:

- **Supported** — affirmative provider/domain evidence supports the semantic applicability required by the selected intent.
- **Unknown** — Blueprint cannot establish a material compatibility fact.
- **Incompatible** — affirmative evidence says the selected intent is not safely meaningful on this target.

Separately, a finding has one authority effect:

- **Unchanged** — the finding adds no authority and removes none; existing provider safety, Safe/Force and Additive/Exact rules still apply.
- **Reduced** — incomplete compatibility evidence causes Blueprint to withhold authority it would otherwise have, especially destructive Exact authority.
- **Blocked** — the selected Restore scope is not applicable.

`Unknown` is never serialized or presented as `Supported`, but Unknown is not automatically a universal hard block.

`Incompatible` is always blocking for effective intent that would otherwise apply.

Compatibility never grants new mutation authority. It can only leave existing authority unchanged, reduce it, or block it.

### 3. Compatibility is part of `RestorePlan`

`RestorePlan` gains a deterministic, machine-readable compatibility report.

Conceptually:

```go
type CompatibilityState string

const (
    CompatibilitySupported    CompatibilityState = "supported"
    CompatibilityUnknown      CompatibilityState = "unknown"
    CompatibilityIncompatible CompatibilityState = "incompatible"
)

type CompatibilityAuthority string

const (
    CompatibilityUnchanged CompatibilityAuthority = "unchanged"
    CompatibilityReduced   CompatibilityAuthority = "reduced"
    CompatibilityBlocked   CompatibilityAuthority = "blocked"
)

type CompatibilityEvidence struct {
    Kind    string `json:"kind"`
    Summary string `json:"summary"`
}

type CompatibilityFinding struct {
    Code          string                 `json:"code"`
    Target        string                 `json:"target,omitempty"`
    State         CompatibilityState     `json:"state"`
    Authority     CompatibilityAuthority `json:"authority"`
    Summary       string                 `json:"summary"`
    Evidence      []CompatibilityEvidence `json:"evidence,omitempty"`
    RequirementID string                 `json:"requirement_id,omitempty"`
}

type CompatibilityCategory struct {
    Category  string                  `json:"category"`
    Applies   bool                    `json:"applies"`
    State     CompatibilityState      `json:"state,omitempty"`
    Authority CompatibilityAuthority  `json:"authority"`
    Evidence  []CompatibilityEvidence `json:"evidence,omitempty"`
    Findings  []CompatibilityFinding  `json:"findings,omitempty"`
}

type CompatibilityEnvironment struct {
    Known          bool   `json:"known"`
    OmarchyVersion string `json:"omarchy_version,omitempty"`
    OmarchyChannel string `json:"omarchy_channel,omitempty"`
}

type CompatibilityReport struct {
    ProfileLastCapture CompatibilityEnvironment `json:"profile_last_capture"`
    Target             CompatibilityEnvironment `json:"target"`
    Categories         []CompatibilityCategory  `json:"categories"`
}
```

The exact Go placement may be refined during implementation, but the JSON/domain semantics are architectural.

Every selected provider contributes one category entry. `applies: false` means effective Restore policy selected no intent for that category; it is not a compatibility verdict and carries no Supported/Unknown/Incompatible state. `applies: true` categories aggregate the most conservative evidence state in effective intent: Incompatible, then Unknown, then Supported. Category authority is Blocked, then Reduced, then Unchanged.

Supported categories must carry affirmative category evidence or supported findings; an empty result never means Supported.

Ordering is deterministic so JSON, approval comparison and tests are stable.

### 4. Providers own compatibility evidence; workflow owns aggregation and applicability

Compatibility is not a second planner.

Each selected Restore provider must deliberately contribute compatibility evidence for the same effective Restore intent it plans. Provider evidence should be derived from the same live inspection used for planning wherever practical so Blueprint does not create two inconsistent views of the target.

The shared workflow owns:

- profile-last-capture and target environment context;
- deterministic aggregation;
- validation of compatibility result shape;
- public plan representation;
- plan applicability;
- approval stability.

Providers own:

- what evidence is meaningful for their category;
- target-level applicability;
- semantic capability probes;
- when incomplete evidence merely reduces authority versus blocks it;
- alignment between any compatibility-driven reduction and verification.

A newly added Restore category must not silently receive an implicit "compatible" result because it forgot to participate.

### 5. No new persisted compatibility provenance is added in v1

Migration Safety v1 does not add per-category/per-target compatibility provenance to the profile and does not bump the profile schema solely for compatibility.

The existing manifest fields:

```toml
[omarchy]
captured_version = "..."
channel = "..."
```

remain **profile last-capture context**.

They are not the authoritative source runtime for every desired target, because partial Capture may preserve desired state captured earlier.

Blueprint producer/binary version is not persisted as a compatibility proxy.

If later migration cases prove that a historical provider protocol or semantic contract must be retained, that fact should be introduced explicitly and narrowly rather than inferred from a binary or global Omarchy version.

### 6. Target inspection is read-only

Compatibility inspection must not:

- update Omarchy;
- update package databases;
- install or remove anything;
- rewrite or migrate the profile;
- mutate Capture/Restore policy;
- invoke `sudo`;
- turn a compatibility check into a hidden readiness action.

Providers should first reuse the read-only evidence they already need for Detect, InspectTargets, Plan and Check.

A new probe is appropriate only when it is authoritative enough to improve the compatibility claim and is itself non-mutating.

If Blueprint cannot establish a fact without mutation, it reports Unknown rather than performing the mutation or guessing.

### 7. Restore policy is resolved before compatibility becomes blocking authority

Compatibility applies only to effective Restore intent.

A target that resolves to Restore Skip does not block the selected plan merely because it would be incompatible to mutate; Blueprint is not proposing to apply that target. A provider with no effective Restore Apply intent still contributes `applies: false` so omission cannot be mistaken for compatibility.

Provider safety and policy remain independent:

- policy may narrow effective intent;
- compatibility may reduce or block authority for effective intent;
- neither policy nor compatibility may override provider safety.

### 8. Selected-scope Restore remains all-or-nothing for blockers

A blocking compatibility finding makes the selected Restore plan inapplicable before approval/journal creation/mutation.

Blueprint may still render all planned operations, skips, requirements and compatibility findings so the user can understand the whole situation.

Blueprint does not silently drop the blocked category and apply the remainder.

A user who wants unaffected work to proceed must explicitly narrow or change intent and obtain a newly calculated plan, for example:

```text
restore resources
```

or by configuring the affected target/category as Restore Skip.

This matches ADR 0022's readiness precedent.

### 9. Force does not override compatibility

`Force` continues to control conflict authority only.

Migration Safety v1 adds no `--ignore-compatibility`, does not redefine `--force`, and does not allow compatibility blocks to disappear because Force is selected.

If a future compatibility override is justified, it requires its own explicit authority model, preview, approval, JSON contract and tests.

### 10. Exact never gains authority from compatibility uncertainty

Compatibility cannot make an Exact removal legal.

Unknown compatibility may reduce Exact authority or block the selected intent. It can never promote unknown state into desired-absence proof or ownership proof.

Where an existing provider already safely preserves additive work while withholding deletion authority under incomplete knowledge, compatibility records that reduction rather than inventing a broader block.

### 11. Requirements remain the model for supported external remediation

`Requirement` continues to represent a concrete machine precondition the user can satisfy through a supported external action.

Compatibility findings may reference a `requirement_id` when incomplete compatibility knowledge is caused by that unmet readiness condition.

Compatibility inspection never executes the remediation.

After remediation, the old plan is discarded and a new plan is inspected and calculated.

### 12. Compatibility is part of approval stability

Because compatibility is part of `RestorePlan`, post-approval recalculation re-inspects and re-assesses it.

Any change to a compatibility finding, evidence state, authority effect, requirement linkage or target environment changes the plan and causes the existing plan-stability rule to reject the old approval.

Approval never silently authorizes a newly different compatibility state.

### 13. Verification uses the same effective intent

Compatibility decisions that reduce or block planning authority must be reflected in provider verification semantics.

Plan intent and Verify intent remain the same contract.

Compatibility must not suppress an operation while leaving verification expecting that suppressed operation to have converged.

### 14. CLI, JSON and TUI use the same compatibility report

Migration Safety v1 does not add a separate `compat` command.

Compatibility is shown through:

- `restore --dry-run`;
- normal Restore preview/approval;
- `restore --dry-run --json` and other Restore JSON plan output;
- TUI Restore review.

Human-readable compatibility appears before mutation operations.

The TUI renders the same shared workflow/domain result. It does not calculate compatibility independently.

`status` and `diff` remain factual desired-vs-current views. `check` remains profile/environment validation and may continue to expose provider readiness notes, but the authoritative migration decision is the Restore compatibility report.

### 15. Existing profiles remain usable

Profiles created before Migration Safety provenance exists continue to load and restore.

Missing profile-last-capture Omarchy metadata is explicit unknown context, not proof of compatibility and not an automatic global block.

Providers assess what they can from the desired-state representation and current target:

- a Resource may be fully assessable without Omarchy source provenance;
- Shell carries its own schema/baseline evidence;
- Config carries captured baseline evidence;
- Themes/Plugins carry item provenance and can inspect target availability;
- other category facts may remain Unknown.

Profile schema compatibility is handled separately by profile loading/migration. A profile the current Blueprint binary cannot understand never reaches runtime compatibility assessment.

## Category evidence in v1

The detailed provider rules live in the design spec. At the ADR level:

- **Packages:** build on ADR 0022. Physical presence, origin knowledge and readiness remain distinct. Unknown origin can reduce Exact authority. No hidden sync/update occurs.
- **Themes:** built-in target availability is affirmative evidence; captured third-party provenance is assessed separately.
- **Installed plugins:** first-party availability, captured source provenance and current validation capability are the relevant evidence.
- **Resources:** copy/Git provenance, current filesystem safety and path mapping are generally sufficient without a global Omarchy-version rule.
- **Config:** current surface support, ownership and baseline evidence determine applicability. `ConfigDelete` remains the only explicit Config desired-absence representation; Included never overrides safety and Excluded is never inferred as desired absence.
- **Omarchy default:** current semantic default contract and the portability of the captured value determine applicability.
- **Shell:** the recorded/current Shell schema contract is authoritative; unsupported schema changes are incompatible until a migration exists.
- **Hooks:** content/path provenance is not automatically proof that a changed Omarchy runtime still consumes the same hook lifecycle; insufficient lifecycle evidence remains explicit Unknown. V1 surfaces that uncertainty without automatically blocking bounded Additive hook reconstruction solely because historical lifecycle provenance was never persisted; any compatibility-dependent destructive authority may only stay the same or be reduced.

## Consequences

### Positive

- Blueprint surfaces what it knows and does not know before migration mutation.
- Compatibility is provider/domain evidence rather than a version heuristic.
- Existing Restore approval/recalculation semantics become the compatibility authority boundary.
- CLI and TUI parity follows from one shared plan.
- Existing profiles do not need to be recreated.
- Resources and other portable state are not globally blocked merely because Omarchy versions differ.
- Exact deletion authority remains conservative.
- Reconstruction Assurance keeps its same-runtime invariant.
- A later migration acceptance scenario can test this domain model without defining it.

### Negative

- v1 will sometimes report Unknown where richer historical provenance could eventually produce a stronger answer.
- Some providers need new explicit compatibility evidence plumbing.
- A provider with no authoritative read-only proof may block or reduce Restore more conservatively than the pre-v1 behavior.
- Human/JSON Restore output becomes larger.
- Provider tests must cover compatibility, planning and verification as one contract.

## Rejected alternatives

- **Global version equality or major-version compatibility:** the existing profile version is too coarse and partial Capture makes it invalid as per-target provenance; different providers have different semantic contracts.
- **Persist a full capture-time capability manifest in v1:** provenance update/preserve semantics become complex before the project has evidence that those facts predict compatibility.
- **Treat Unknown as Supported:** converts lack of evidence into mutation authority.
- **Treat every Unknown as a hard global block:** discards strong category-specific portability evidence and makes unrelated safe work impossible.
- **Silently apply compatible categories from a blocked plan:** violates the existing selected-scope approval contract and creates implicit partial reconstruction.
- **Reuse `--force`:** conflict authority and migration compatibility are independent axes.
- **Automatically run remediation/update:** violates read-only inspection and plan-stability rules.
- **Add a separate compatibility command with separate logic:** risks drift from the actual Restore authority boundary.

## References

- ADR 0020: CLI policy and Capture parity
- ADR 0021: Real-Omarchy Reconstruction Assurance
- ADR 0022: Fresh-package readiness and elevated Restore
- `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`
- `docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md`
- `docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md`
