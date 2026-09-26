# Migration Safety / Compatibility v1 Design

## Status

Proposed.

This design follows ADR 0023 and the approved conversational architecture review for the Migration Safety / Compatibility v1 roadmap tranche.

It is based on `main` after Reconstruction Assurance PR #44 landed. The session began from `61f0eb11151b7c0b1362d2856aeca62998161657`; before this document was written, `main` advanced to `a05025cd005fd89700bb96d16ce11ddfe10d0303` with Reconstruction Assurance harness/workflow/documentation changes. Those changes do not alter the functional Migration Safety domain design. The accepted ADR 0021/0022 contract remains authoritative: Reconstruction Assurance keeps Machine A and Machine B on the same ready Omarchy runtime, while this tranche adds cross-runtime migration safety as a separate product capability.

This document is a design specification, not an implementation plan. No production implementation, PR decomposition or migration acceptance harness change is authorized by this document alone.

## Summary

Migration Safety / Compatibility v1 answers:

> When a Blueprint profile was captured under one Omarchy environment and is being restored under another, what does Blueprint actually know about whether the selected intent is still safe and meaningful to apply?

The answer is not a global Omarchy version comparison.

Blueprint already has category-specific semantic evidence:

- profile schema loading/migration proves whether this Blueprint binary understands the portable profile representation;
- Shell records an explicit schema and captured/current baselines;
- Config records captured baselines and inspects current ownership/surface/baseline state;
- built-in Themes and Installed plugins can be compared with target availability;
- Packages distinguish presence, repository origin and readiness;
- Defaults already distinguish Omarchy-managed portable values from raw `.desktop` identifiers;
- Resources carry portable copy/Git provenance and filesystem preconditions largely independent of Omarchy runtime identity;
- Hooks carry content/path provenance but currently provide weaker proof that a changed Omarchy runtime still consumes the same lifecycle.

Migration Safety v1 unifies that evidence into the existing Restore lifecycle.

The core model is:

```text
profile desired state
+ effective Restore policy/intent
+ read-only target capability evidence
→ compatibility assessment
→ Restore plan
→ preview
→ approval
→ re-inspect/re-assess/replan
→ require identical authority
→ apply
→ verify with the same intent
```

The v1 guarantee is:

> Blueprint reports, before mutation, which selected Restore intent is supported by affirmative evidence, which compatibility facts are unknown, which intent is known incompatible, and whether that evidence leaves Restore authority unchanged, reduces it, or blocks the selected Restore scope.

Compatibility can never increase mutation authority.

## Background

### Reconstruction Assurance deliberately excludes migration

ADR 0021 proves same-runtime reconstruction through real Omarchy.

ADR 0022 amended the real-machine lifecycle so both machines:

```text
boot pinned install substrate
→ follow Omarchy's supported readiness/update path
→ reach the same ready Omarchy runtime
```

Only then do Capture and Restore occur.

That symmetry is intentionally part of the Reconstruction Assurance claim. It prevents the acceptance test from accidentally becoming a migration test.

Migration Safety v1 does not weaken that invariant.

A later migration acceptance scenario may reuse the Reconstruction Assurance harness after this domain behavior exists, but initial compatibility semantics do not depend on that harness.

### Profile-level Omarchy metadata is contextual, not per-target provenance

The profile manifest currently records:

```toml
[omarchy]
captured_version = "..."
channel = "..."
```

Restore plans expose that as `omarchy_from` and the current detected runtime as `omarchy_to`.

Those fields are useful context, but `captured_version` is not authoritative provenance for every desired target.

Capture supports partial/provider-scoped work and target-level Preserve. A successful Capture updates profile-level Omarchy metadata even when some desired state remains byte-for-byte preserved from an earlier capture.

Therefore a profile can contain portable desired state whose components were learned under different environments while carrying one current profile-level captured version.

Migration Safety must never infer:

```text
all desired state came from manifest.omarchy.captured_version
```

### Profile schema compatibility is already a different layer

The current profile schema is loaded and migrated by `internal/profile/`.

If the current Blueprint binary cannot understand a profile representation, compatibility assessment never begins.

The compatibility dimensions remain distinct:

```text
profile schema compatibility
Blueprint binary ability to load/plan current schema
Omarchy target runtime support
provider/category semantic applicability
target-specific capability/readiness
```

No v1 result collapses these into one "version compatible" boolean.

## Goals

1. Surface cross-environment compatibility evidence before Restore mutation.
2. Represent Supported, Unknown and Incompatible explicitly.
3. Distinguish compatibility knowledge from mutation-authority effect.
4. Reuse provider/domain evidence instead of building an Omarchy-version matrix.
5. Keep compatibility inside the shared Restore plan and approval lifecycle.
6. Preserve CLI/TUI parity and machine-readable JSON.
7. Ensure Unknown never becomes compatibility proof.
8. Avoid turning every Unknown into a universal hard block.
9. Ensure compatibility can only leave authority unchanged, reduce it, or block it.
10. Keep Force and compatibility separate.
11. Ensure Exact never gains destructive authority from incomplete compatibility knowledge.
12. Keep remediation explicit and external, following the ADR 0022 Requirement pattern.
13. Preserve existing profiles without forcing recapture.
14. Keep profiles human-readable and avoid opaque compatibility caches.
15. Keep Reconstruction Assurance's same-ready-runtime claim unchanged.
16. Provide a stable domain surface that a later cross-version migration acceptance scenario can exercise.

## Non-goals

Migration Safety / Compatibility v1 does not add:

- automatic desired-state semantic migration;
- profile rewrite during compatibility inspection;
- automatic Omarchy update or downgrade;
- arbitrary downgrade support;
- rollback across Omarchy versions;
- cross-distro portability;
- online compatibility services;
- telemetry;
- a package rename database;
- a third-party theme/plugin compatibility registry;
- automatic replacement of disappeared Themes/Installed plugins/default values;
- provider-specific compatibility overrides;
- a global `--ignore-compatibility` flag;
- reinterpretation of `--force`;
- multiple historical Omarchy-version matrix CI;
- a separate migration execution subsystem;
- Services v1;
- Sync Engine v1;
- Auto Sync;
- bidirectional reconciliation.

A later tranche may add explicit historical provider protocol provenance if real migration cases demonstrate that it is useful.

## Product principles

### Compatibility describes evidence, not optimism

The product must never turn "Blueprint did not find a reason to object" into "Blueprint knows this is compatible."

Supported requires affirmative evidence.

Unknown stays Unknown.

### Compatibility is about selected intent

Blueprint does not need to prove that every profile target is compatible with a machine before doing any work.

It needs to prove or characterize compatibility for the effective Restore intent the user is asking it to apply.

A target with Restore Skip is not proposed mutation authority and therefore cannot make an otherwise scoped plan inapplicable.

### Category safety wins

First-party/safety-owned state keeps its existing protection. Compatibility never turns a provider-owned or Omarchy-owned safety boundary into user write authority.

A blocking compatibility finding in one effective selected category cannot disappear because other categories are supported.

A supported category is not automatically blocked merely because another category is incompatible.

The selected Restore scope is still atomic at apply time: a blocked plan applies nothing. Users may explicitly narrow the scope or change Restore policy and obtain a new plan.

### Read-only means read-only

Compatibility inspection can query, stat, hash, parse, classify and compare.

It cannot repair.

In particular it cannot:

- call `omarchy update`;
- call package synchronization/update/install/remove commands;
- invoke `sudo`;
- write profile files;
- migrate profile desired state in place;
- change policy;
- create target configuration as a probe.

### Compatibility and existing safety axes are orthogonal

Compatibility is not:

- Capture policy;
- Restore policy;
- Safe/Force conflict authority;
- Additive/Exact convergence;
- profile schema migration.

Those existing concepts keep their current meaning.

## Current architecture findings

### Shared Restore planning is the correct authority boundary

`workflow.Session.restorePlan` currently:

1. reloads the profile;
2. resolves effective Restore options;
3. selects captured providers or an explicitly scoped provider;
4. detects the current Omarchy runtime;
5. resolves each provider's machine-aware `RestoreContext`;
6. asks each provider to plan;
7. aggregates operations, skips and requirements;
8. validates/finalizes the plan.

`ApplyApprovedRestore` then recalculates and requires `reflect.DeepEqual(newPlan, approvedPlan)` before any mutation.

This gives compatibility exactly the stable-authority boundary it needs.

### Requirements already model external readiness

`model.Requirement` represents a precondition that operations need but Blueprint deliberately does not perform.

ADR 0022 uses this for package metadata readiness and the supported remediation `omarchy update`.

`CheckRestoreApplicable` refuses a plan with unmet requirements before creating a journal or mutating state.

Compatibility should compose with this model, not replace it.

### Target inspection already separates capability from permission

`workflow.TargetInspection` includes current/desired presence, Capture/Restore eligibility, provider safety reasons and structural capabilities.

Its contract explicitly says capability does not grant permission.

This provides the policy/compatibility boundary:

```text
provider target capability
≠ user Restore policy
≠ mutation authority
```

### Existing provider evidence is heterogeneous

That heterogeneity is a feature, not a flaw.

A Shell schema integer can be authoritative for Shell while meaningless for Resources.

A Config baseline can support a semantic merge while a Hooks content hash cannot prove Omarchy still invokes the same hook lifecycle.

The shared compatibility model must normalize outcomes, not force every provider to use identical evidence.

## Decision

### 1. Compatibility is a first-class part of Restore planning

`RestorePlan` gains:

```go
Compatibility CompatibilityReport `json:"compatibility"`
```

The compatibility report is always calculated for Restore planning, including dry-run.

It is not cached in the profile and is not accepted as independent mutation authority.

### 2. Environment context

Compatibility reports expose two pieces of environment context.

Conceptually:

```go
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

`ProfileLastCapture` is deliberately not called `Source`.

Its semantics are:

> the Omarchy environment recorded by the profile's most recent successful Capture metadata update.

It is contextual evidence only.

`Target` is the environment detected for this Restore plan. Restore planning already fails if current Omarchy detection itself fails or the binary-level supported-major requirement is not met.

For an old profile with no recorded Omarchy metadata:

```json
{
  "known": false
}
```

is the explicit representation.

The existing top-level `omarchy_from` / `omarchy_to` plan fields remain for backward compatibility in v1. Documentation clarifies that `omarchy_from` has the same profile-last-capture limitation.

### 3. Compatibility state

```go
type CompatibilityState string

const (
    CompatibilitySupported    CompatibilityState = "supported"
    CompatibilityUnknown      CompatibilityState = "unknown"
    CompatibilityIncompatible CompatibilityState = "incompatible"
)
```

#### Supported

Supported means:

> Blueprint has affirmative evidence that the semantic contract required for this selected intent is available on the target.

Supported is not a promise that an external network, repository or unrelated runtime dependency will never fail during execution.

It is a claim about the compatibility evidence Blueprint is responsible for knowing before mutation.

#### Unknown

Unknown means:

> Blueprint cannot establish a material compatibility fact from the portable desired state and read-only target evidence available to it.

Unknown never means compatible.

Unknown may have different authority effects depending on the provider operation.

#### Incompatible

Incompatible means:

> Blueprint has affirmative evidence that the selected intent is not safely meaningful under the target contract.

Examples include a recorded Shell schema that the current Shell implementation does not support, or a first-party target that current Omarchy definitively no longer provides.

For effective intent, Incompatible is always blocking.

### 4. Authority effect

```go
type CompatibilityAuthority string

const (
    CompatibilityUnchanged CompatibilityAuthority = "unchanged"
    CompatibilityReduced   CompatibilityAuthority = "reduced"
    CompatibilityBlocked   CompatibilityAuthority = "blocked"
)
```

#### Unchanged

Compatibility adds no new authority and removes none.

The operation still remains subject to:

- provider safety;
- Restore Apply/Skip;
- Safe/Force;
- Additive/Exact;
- operation preconditions;
- Requirements;
- interactive authority;
- verification.

Unknown + Unchanged is allowed only when the unknown fact is not required to grant the mutation authority being used.

This is not a compatibility endorsement; it is an explicit statement that the unknown fact is informational for this operation rather than its authority basis.

#### Reduced

Blueprint withholds authority because evidence is incomplete.

Canonical example: Exact removal that would require a classification/provenance fact Blueprint cannot establish.

Reduced authority must be visible in both:

- the compatibility report; and
- concrete plan/verification behavior, normally through a provider skip or disabled destructive operation.

#### Blocked

The effective selected Restore scope cannot be applied.

A blocked plan still renders completely but `CheckRestoreApplicable` refuses before approval/journal/mutation.

### 5. Finding model

Conceptually:

```go
type CompatibilityEvidence struct {
    Kind    string `json:"kind"`
    Summary string `json:"summary"`
}

type CompatibilityFinding struct {
    Code          string                  `json:"code"`
    Target        string                  `json:"target,omitempty"`
    State         CompatibilityState      `json:"state"`
    Authority     CompatibilityAuthority  `json:"authority"`
    Summary       string                  `json:"summary"`
    Evidence      []CompatibilityEvidence `json:"evidence,omitempty"`
    RequirementID string                  `json:"requirement_id,omitempty"`
}
```

`code` is a stable machine-readable identifier such as:

```text
shell.schema.unsupported
themes.builtin.unavailable
packages.origin.unknown
config.baseline.ambiguous
hooks.lifecycle.unestablished
```

Codes are public JSON contract once released.

`target` uses the provider's existing stable policy target identity where there is one. UI labels remain human-facing and are not stable API keys.

`evidence` is concise, deterministic and non-secret.

Evidence must not include:

- timestamps;
- temp paths;
- nondeterministic command output;
- secrets;
- credentials;
- volatile ordering.

`requirement_id` links a finding to an existing `Requirement` when supported external remediation is what can change the evidence.

### 6. Category aggregation

```go
type CompatibilityCategory struct {
    Category  string                  `json:"category"`
    Applies   bool                    `json:"applies"`
    State     CompatibilityState      `json:"state,omitempty"`
    Authority CompatibilityAuthority  `json:"authority"`
    Evidence  []CompatibilityEvidence `json:"evidence,omitempty"`
    Findings  []CompatibilityFinding  `json:"findings,omitempty"`
}
```

Every selected provider contributes exactly one category entry.

`applies: false` means effective Restore policy contains no Restore Apply intent for that provider. It is a scope statement, not a compatibility evidence state:

```json
{
  "category": "hooks",
  "applies": false,
  "authority": "unchanged"
}
```

For `applies: false`:

- `state` is omitted;
- compatibility findings are empty;
- authority is Unchanged;
- the provider's normal Restore-policy skips remain the user-facing explanation.

For `applies: true`, aggregation order is conservative.

State:

```text
incompatible
> unknown
> supported
```

Authority:

```text
blocked
> reduced
> unchanged
```

Invariants:

- Incompatible effective intent implies Blocked.
- Supported cannot itself grant destructive or conflict authority.
- Supported requires affirmative category evidence or supported findings; an empty result is not compatibility proof.
- A category with a Reduced finding is never summarized as Unchanged.
- Empty/missing provider compatibility output is an orchestration/provider error, never implicit Supported.

All categories and findings use deterministic ordering:

```text
category
→ target
→ code
```

### 7. Compatibility scope follows effective Restore intent

Compatibility is calculated after `RestoreContext` has resolved machine/profile Restore policy.

For each provider, compatibility evaluates:

- desired state whose effective decision is Restore Apply;
- support operations required by those targets;
- Exact desired-absence work only when Exact is selected;
- existing target state needed to verify the selected intent.

Targets with Restore Skip do not become compatibility blockers.

They remain visible through the provider's normal Skipped/policy presentation. If all provider targets resolve to Skip, the category reports `applies: false` rather than pretending the provider is Supported.

This rule is essential for machine-specific intent. A laptop-specific incompatible application should not block a desktop if the desktop's effective policy deliberately skips that target.

### 8. Provider planning and compatibility must share evidence

The preferred provider-local planning contract is conceptually:

```go
type RestoreFragment struct {
    Operations    []Operation
    Skipped       []Skipped
    Requirements  []Requirement
    Compatibility CompatibilityCategory
}
```

and:

```go
type RestoreProvider interface {
    Provider
    Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (RestoreFragment, error)
    Verify(context.Context, profile.Data, RestoreContext) (model.VerificationResult, error)
}
```

The exact type names may move during implementation, but the architectural constraint is fixed:

> one provider-local planning pass returns both its concrete Restore effects and its compatibility assessment from the same effective intent and, where practical, the same detected target state.

A separate UI-only compatibility service is rejected.

A separate global version-policy engine is rejected.

If a provider already performs one detection pass inside Plan, compatibility should be derived from that detection rather than rerunning a logically equivalent probe and risking within-plan disagreement.

### 9. Provider compatibility is mandatory

At the end of the v1 implementation, every selected Restore provider must participate.

Workflow validation fails if a selected provider returns:

- no category assessment;
- a category ID that does not equal the provider ID;
- invalid state/authority combinations;
- duplicate finding codes for the same target where uniqueness is required;
- a `requirement_id` that does not exist in the same plan;
- nondeterministic/invalid ordering after normalization.

This prevents future categories from becoming "compatible by omission."

## Target capability inspection

### General rule

Target capability inspection reuses semantic probes already authoritative for the provider.

Examples of good evidence:

- parsing the current Shell baseline schema;
- current built-in Theme/Installed plugin inventory;
- current Config baseline and ownership state;
- package metadata readiness/origin state;
- current Omarchy semantic getter behavior;
- Resource copy/Git provenance plus current filesystem preconditions.

Examples of weak evidence that must not become a v1 guarantee by itself:

- matching Omarchy version strings;
- file existence without ownership/semantic meaning;
- successful execution of an unrelated command;
- "this worked on one historical release";
- a package/theme/plugin name merely appearing in the profile.

### Probe failure classification

Three cases stay distinct.

#### Fundamental inspection failure

Examples:

- `omarchy version` fails;
- a required profile snapshot is corrupt;
- a provider cannot parse trusted desired state;
- filesystem permission errors prevent basic inspection.

These remain ordinary errors. Blueprint cannot build a trustworthy plan.

They are not softened into Unknown.

#### Expected incomplete knowledge

Examples:

- package origin metadata is not present;
- Config baseline history cannot establish whether bytes match a known historical baseline;
- no authoritative read-only hook-lifecycle probe exists.

These become structured Unknown findings where the provider can still characterize the condition safely.

#### Affirmative negative evidence

Examples:

- current Shell schema is unsupported;
- a first-party built-in target is definitively absent from the current authoritative inventory.

These become Incompatible.

### Read-only invariant

Compatibility tests must assert that inspection contains no:

```text
-S
-Sy
-Syu
install
remove
drop
update
sudo
```

or other provider-specific mutating action.

A query that cannot be guaranteed non-mutating is not an inspection probe in v1.

## Category-specific v1 semantics

The following defines the minimum v1 compatibility claim. Providers may use stronger existing evidence where it is already authoritative, but may not silently weaken these rules.

### Packages

Relevant dimensions:

- physical package presence;
- repository-origin knowledge;
- package metadata readiness;
- Blueprint's semantic package recipe ownership for special packages;
- Exact removal provenance.

ADR 0022 remains authoritative.

Rules:

1. Physical presence remains independent of origin classification.
2. Missing sync metadata remains explicit package-origin Unknown.
3. Compatibility inspection never runs `pacman -Sy` or `omarchy update`.
4. If missing metadata is required by planned package operations, the existing package metadata Requirement remains the apply blocker.
5. Exact never removes an official/AUR package whose required origin/provenance cannot be established.
6. Compatibility records this as Unknown + Reduced when safe non-destructive work remains valid.
7. A semantic recipe known to Blueprint is assessed against its current required mechanism; recipe absence from Blueprint's own current supported set is a profile/binary compatibility concern, not inferred from version strings.
8. v1 does not introduce a package rename database.
9. v1 does not claim that identical package names have identical upstream application behavior across all historical releases. It assesses Blueprint/package-authority applicability, not application-level semantic equivalence.

Where current read-only package metadata can authoritatively establish target identifier availability without mutation, providers may use it. Where no such evidence is safe/reliable, the result remains Unknown rather than guessed.

### Themes

Separate first-party and captured third-party evidence.

For a first-party/built-in Theme:

- current authoritative target availability is the key evidence;
- definitively unavailable target Theme is Incompatible + Blocked when effective Restore intent requires it.

For Git/local portable Theme state:

- captured source/hash/revision provenance is stronger than source Omarchy version;
- target-side Omarchy semantic capability still must be available;
- absence of a reliable read-only compatibility proof for a third-party contract remains Unknown.

V1 does not maintain a historical Theme rename/replacement map.

### Installed plugins

First-party and third-party Installed plugins are assessed separately.

For first-party plugins:

- current target inventory is authoritative for availability;
- definitively unavailable first-party plugin required by effective Restore intent is Incompatible + Blocked.

For third-party plugins:

- captured Git/local provenance remains relevant;
- target validation capability is the semantic authority when it can be exercised read-only before mutation;
- if compatibility cannot be established until after an install/copy mutation, inspection reports Unknown rather than claiming Supported.

Whether such Unknown is Blocked or Unchanged depends on whether the existing operation sequence can remain safe without using the unknown fact as write authority. The provider must justify that in tests; Force cannot change it.

Shell-owned enablement remains Shell-owned and is not duplicated as a plugin compatibility override.

### Resources

Resources are the clearest example of category portability independent from Omarchy release identity.

For a Resource whose desired state is fully represented through an existing supported strategy:

- `copy`;
- `git`;
- `git+diff`;

compatibility can be Supported when current Resource metadata, artifact integrity, path resolution and filesystem safety checks all establish that the existing Resource semantics are applicable.

The compatibility evidence is the portable Resource representation and current target preconditions, not Omarchy version equality.

Resource machine path mappings remain part of existing machine policy.

Resources remain non-destructive under Exact.

An unrelated Omarchy runtime mismatch does not globally block a Supported Resource Restore.

### Config

Config compatibility is based on the current supported configuration surface and baseline/ownership evidence.

Relevant evidence includes:

- whether the logical surface still exists and is supported;
- current stronger-owner/delegation classification;
- captured desired and baseline snapshots;
- current baseline identity;
- trusted historical baseline evidence where available;
- explicit `ConfigDelete` desired-absence intent.

Rules:

1. `Included` never manufactures compatibility evidence or overrides safety.
2. `Excluded` means leave alone, not desired absence.
3. `ConfigDelete` is the explicit desired-absence representation; compatibility never infers deletion intent from Excluded or from a missing unmanaged path.
4. A target delegated to a stronger owner remains governed by existing ownership safety.
5. A current unsupported surface required by effective Restore intent is Incompatible + Blocked.
6. Ambiguous baseline knowledge is Unknown.
7. Unknown baseline evidence may reduce destructive/overwrite authority or block the target depending on the existing Config merge safety rule.
8. Exact never treats Config uncertainty as additional deletion proof.
9. Compatibility does not rewrite captured desired/baseline snapshots to the new Omarchy format.

### Omarchy default

Defaults are meaningful only through Omarchy's semantic default contract.

Existing portability rules remain:

- empty = unmanaged;
- raw `.desktop` values are not considered portable setter values;
- agent remains excluded from automatic set-only Restore because the setter launches the selected agent.

Compatibility evidence is derived from the current semantic getter/setter surface and the captured value's existing portability classification.

If current read-only target evidence cannot establish that a captured portable value remains an accepted semantic choice, the result is Unknown rather than inferring support from the Omarchy version.

V1 does not add an allowlist or historical mapping of default choices merely for compatibility.

### Shell

Shell has the strongest existing explicit compatibility contract.

Relevant evidence:

- captured Shell schema version;
- captured Shell baseline;
- desired Shell document;
- current Omarchy Shell baseline schema;
- existing semantic three-way merge behavior.

Rules:

1. Matching the supported Shell schema plus valid captured/current baseline documents is affirmative evidence for the current merge protocol.
2. A current unsupported Shell schema or recorded unsupported Shell schema is Incompatible + Blocked.
3. Existing "Shell schema changed; migration required" semantics become a structured compatibility finding rather than only a planning error where the desired state itself is otherwise valid.
4. V1 does not automatically migrate a Shell document between schemas.
5. Shell has no desired absence.
6. Shell verification remains the same semantic intent produced by merge planning.

### Hooks

Hooks carry:

- canonical path;
- captured content hash;
- mode metadata;
- desired-absence provenance where supported;
- current filesystem conflict/precondition evidence.

Those facts establish artifact integrity and safe filesystem replacement/removal. They do not, by themselves, prove that a changed Omarchy runtime still invokes the same hook lifecycle.

Therefore:

- where the current target exposes authoritative read-only hook-lifecycle evidence, the provider may use it;
- otherwise lifecycle compatibility remains Unknown;
- content/path integrity is still reported as evidence rather than being discarded;
- v1 does **not** automatically block bounded Additive hook reconstruction solely because historical hook-lifecycle provenance was never persisted;
- such Additive work is `Unknown + Unchanged` only when the existing provider path, ownership/conflict checks, captured bytes/mode and write preconditions remain sufficient safety authority for the file operation itself;
- any lifecycle-dependent Exact/destructive authority may only remain unchanged when independently proven, otherwise it is Reduced or Blocked.

This is intentionally honest rather than pretending the lifecycle is Supported. It preserves ordinary reconstruction while making the unresolved semantic-contract evidence visible, and it leaves room for future stronger provenance/probing if real migration workflows require it.

## Interaction with Restore policy and intent

### Restore Apply / Skip

Order:

```text
InspectTargets
→ resolve sparse machine/profile Restore policy
→ establish effective Apply/Skip
→ assess compatibility for effective Apply intent
→ plan
```

A policy Skip cannot be re-enabled by compatibility.

A compatibility Supported result cannot override a policy Skip.

### Safe / Force

Safe/Force remains conflict authority.

Compatibility never reads Force as permission to ignore a finding.

Examples:

```text
existing Config differs
+ Force
+ compatibility Supported
→ Force may affect the existing conflict rule

Shell schema incompatible
+ Force
→ still blocked
```

### Additive / Exact

Additive/Exact remains convergence intent.

Compatibility controls whether the evidence needed for an operation exists.

Examples:

```text
package origin unknown
+ Additive
→ may still allow safe presence work

package origin unknown
+ Exact
→ no origin-dependent removal authority
```

Exact compatibility assessment includes desired-absence work only when Exact is actually selected.

Additive planning does not need to block on compatibility facts relevant only to a deletion it will not attempt.

## Plan applicability and partial Restore

### No silent partial apply

A plan with any `CompatibilityBlocked` finding is inapplicable.

Conceptually, `CheckRestoreApplicable` becomes:

```text
if blocking compatibility:
    refuse
if requirements:
    refuse
if interactive operation without terminal:
    refuse
otherwise:
    applicable
```

The exact error ordering should be deterministic. Human output should show all preflight facts before apply is attempted.

### Explicit narrowing remains available

If:

```text
Packages   blocked
Resources  supported
Config     unknown + blocked
```

then unscoped Restore applies nothing.

The user may explicitly request:

```text
restore resources
```

That creates a different plan, compatibility report and approval boundary.

Likewise, an explicit Restore Skip rule can intentionally remove a target from effective Restore intent.

This is not Blueprint silently choosing partial migration; it is user-selected scope.

## Requirements and remediation

Compatibility and Requirements have different jobs.

Compatibility says:

> what does Blueprint know about semantic applicability?

Requirement says:

> what supported external precondition must be satisfied before this plan can apply?

A finding may link to a Requirement.

Example:

```json
{
  "code": "packages.origin.unknown",
  "target": "official:example",
  "state": "unknown",
  "authority": "blocked",
  "requirement_id": "packages.metadata"
}
```

After the user runs the named remediation:

```text
old plan is discarded
→ target is re-inspected
→ new compatibility is calculated
→ new plan is previewed/approved
```

Blueprint never approves one compatibility state and applies after silently repairing to another.

## Approval and recalculation

Compatibility is included in `RestorePlan`.

Therefore the existing approval-stability rule applies without a second token system.

Example:

```text
Preview:
  Shell schema supported

User approves

Before apply:
  Omarchy update or external change replaces Shell baseline/schema

Replan:
  Shell schema incompatible

DeepEqual(newPlan, approvedPlan) = false
→ ErrRestorePlanChanged
→ no mutation
```

The same is true when:

- a first-party Theme disappears;
- package readiness changes;
- a Config surface changes ownership;
- a requirement becomes satisfied;
- a target environment version/channel changes;
- compatibility evidence becomes stronger or weaker.

A newly better compatibility state also invalidates old approval. The user approves the new plan rather than letting Blueprint expand authority silently.

## Verification

Verification must judge the same effective Restore intent and safety reductions used for planning.

The existing workflow already passes the exact `RestoreContext` from planning into Verify.

Compatibility adds the following invariant:

> If compatibility caused planning to suppress, reduce or block an operation, verification cannot pretend the suppressed mutation was part of the successfully applied intent.

Examples:

- Additive never verifies Exact-only absence.
- Origin-unknown Exact removals withheld by compatibility remain not converged under Exact according to the provider's existing/updated verification rule.
- A blocked plan never reaches mutation or post-apply verification.
- A Restore Skip target is not judged as failed convergence.

Providers must test plan/verify alignment for every Reduced compatibility rule.

## Public CLI behavior

### No new top-level compatibility command in v1

The primary surfaces are:

```text
omarchy-blueprint restore --dry-run
omarchy-blueprint restore
omarchy-blueprint restore <category> --dry-run
```

Compatibility is preflight for real Restore authority, so putting the authoritative result there minimizes overlapping concepts.

### Human rendering

Compatibility appears before Requirements/Operations.

Illustrative output:

```text
Compatibility

Profile last capture
  Omarchy: 4.0.4 (stable)

Target
  Omarchy: 4.1.0 (stable)

Packages
  Unknown — destructive authority reduced
  ! package origin cannot be established for some managed state

Resources
  Supported

Shell
  Incompatible — restore blocked
  ! Shell schema 2 is not supported; captured schema is 1

Restore cannot be applied with this scope.
Change Restore scope/policy or resolve the blocking condition, then plan again.
```

Exact wording may follow current CLI rendering conventions, but the distinctions Supported / Unknown / Incompatible and Unchanged / Reduced / Blocked must remain visible.

### JSON

Restore plan JSON gains:

```json
{
  "profile_schema": 13,
  "omarchy_from": "4.0.4",
  "omarchy_to": "4.1.0",
  "compatibility": {
    "profile_last_capture": {
      "known": true,
      "omarchy_version": "4.0.4",
      "omarchy_channel": "stable"
    },
    "target": {
      "known": true,
      "omarchy_version": "4.1.0",
      "omarchy_channel": "stable"
    },
    "categories": [
      {
        "category": "resources",
        "applies": true,
        "state": "supported",
        "authority": "unchanged",
        "evidence": [
          {
            "kind": "portable-resource-representation",
            "summary": "captured Resource strategy and current target preconditions are supported"
          }
        ]
      },
      {
        "category": "shell",
        "applies": true,
        "state": "incompatible",
        "authority": "blocked",
        "findings": [
          {
            "code": "shell.schema.unsupported",
            "target": "state",
            "state": "incompatible",
            "authority": "blocked",
            "summary": "captured Shell schema is not supported by the target"
          }
        ]
      }
    ]
  },
  "operations": [],
  "requirements": []
}
```

JSON consumers must not infer category applicability from `omarchy_from == omarchy_to`.

### Apply errors

A blocking plan returns a typed workflow/domain error comparable in role to `UnmetRequirementsError`.

The error should carry blocking findings so CLI and TUI can render from structure rather than parse text.

The plan remains available to the caller for preview.

## TUI behavior

ADR 0020 parity is mandatory.

The TUI uses the same `workflow.Session.PlanRestore*` result.

Restore review shows:

- profile-last-capture context;
- current target context;
- category compatibility summary;
- expandable/detail findings where appropriate;
- requirements;
- operations/skips;
- effective Safe/Force + Additive/Exact intent.

A blocked compatibility report disables/refuses Apply through the shared workflow applicability check.

The TUI does not contain an independent version-comparison rule.

The TUI does not rewrite profile state to make a finding disappear.

## `check`, `status` and `diff`

### `check`

`check` remains:

> validate the profile and current environment.

Existing degraded-but-safe provider notes remain appropriate there.

Migration Safety may reuse those underlying facts, but v1 does not turn `check` into a second compatibility-plan command.

This avoids contradictory results such as:

```text
check says compatible
restore says incompatible
```

because only Restore owns the migration applicability verdict.

### `status` and `diff`

`status` and `diff` remain factual desired-vs-current views.

A difference can exist even when Restore is blocked.

A machine can be "in sync" for one category without proving that a future different desired value would be compatible.

Compatibility therefore is not folded into drift semantics.

## Capture and provenance

### No new profile fields in v1

Capture behavior remains unchanged except documentation must clarify the semantic meaning of existing Omarchy metadata.

No new files such as:

```text
compatibility.toml
capabilities.json
source-environment.bin
```

are introduced.

### Capture Preserve

Because v1 persists no new compatibility provenance, Capture Preserve needs no compatibility-specific merge rule.

The existing desired state and artifacts remain preserved exactly according to provider/Capture policy semantics.

This avoids the false precision of refreshing category provenance when the category content was not actually recaptured.

### Git history

Profile Git history continues to show actual desired-state/profile changes.

Compatibility inspection produces no profile diff.

A user's compatibility result is runtime planning data, like current target inspection, not portable desired state.

## Backward compatibility

### Older profiles without Omarchy provenance

They remain loadable if their profile schema is supported/migratable.

Compatibility report:

```json
"profile_last_capture": {
  "known": false
}
```

Providers then assess from category evidence.

Missing global provenance does not automatically block:

- Resources with valid portable representation;
- Shell with sufficient recorded schema/baseline evidence;
- Config where snapshots/current surfaces give enough evidence.

Other facts may remain Unknown.

### Load-time profile migrations

Existing load-time schema migration remains separate.

Migration Safety does not replace or formalize the profile migration registry in v1.

If implementation work needs to add profile-schema changes for an unrelated reason, that change requires its own explicit migration treatment; the compatibility feature itself should not force a schema bump.

### Blueprint producer version

Not added.

A producer binary version would answer "which executable wrote the profile?" but not "which semantic contract does this target require?"

Persisting it would invite the same version-string proxy problem.

## Domain validation rules

A pure validator should reject malformed compatibility plans before presentation or execution.

Minimum invariants:

1. target environment is known for a valid Restore plan;
2. profile-last-capture may be unknown;
3. category IDs are unique;
4. finding `(target, code)` keys are unique within a category;
5. `applies: false` categories omit State, carry Unchanged authority and have no findings;
6. `applies: true` categories use known State/Authority enums;
7. Incompatible findings are Blocked;
8. Supported categories have affirmative evidence and never arise from an empty assessment;
9. category State equals the conservative aggregate of findings/evidence;
10. category Authority equals the conservative aggregate;
11. every `requirement_id` resolves to a same-plan Requirement;
12. finding target keys, when supplied, belong to the provider's inspected/planned target namespace;
13. order is normalized deterministically;
14. every selected provider contributes one category; providers with no effective Apply intent use `applies: false` rather than omitting the category.

The validator is pure and should receive the heaviest unit coverage.

## Error semantics

Do not encode expected compatibility outcomes as arbitrary error strings.

Use structured findings for:

- known incompatibility;
- expected unknown state;
- reduced authority;
- requirement-linked uncertainty.

Reserve ordinary errors for inability to construct a trustworthy plan at all.

This distinction is important for JSON/TUI parity.

## Security and privacy

Compatibility evidence is public plan/profile-adjacent output.

It must not expose:

- command stderr containing credentials;
- environment variables;
- tokens;
- Git credentials;
- raw sensitive Config contents;
- plugin URLs containing credentials;
- temporary paths that reveal unrelated state.

Use sanitized identifiers and concise semantic summaries.

Compatibility inspection does not add network telemetry.

## Determinism

The compatibility report participates in plan approval equality, so it must be stable when the machine has not materially changed.

Providers must normalize:

- category ordering;
- finding ordering;
- evidence ordering;
- set/list values in summaries where included.

Do not include timestamps, random IDs or temp filenames.

A provider may hash stable evidence when necessary, but user-facing evidence should stay understandable.

## Testing strategy

### 1. Pure model/domain tests

Cover:

- state ordering;
- authority ordering;
- aggregate category calculation;
- invalid enum combinations;
- Incompatible => Blocked;
- requirement linkage;
- duplicate finding rejection;
- deterministic normalization;
- JSON serialization;
- unknown profile-last-capture context.

These tests should not require Omarchy.

### 2. Provider compatibility tests

For every current category, add table-driven cases for:

```text
supported
unknown + unchanged
unknown + reduced
unknown + blocked (where relevant)
incompatible + blocked
policy Skip excludes irrelevant finding
```

Not every provider must use every combination.

Provider tests also assert compatibility inspection does not run mutating commands.

### 3. Restore workflow tests

At minimum:

- blocking finding makes `CheckRestoreApplicable` fail before journal creation;
- no operation runner is invoked for a blocked plan;
- Force does not clear a compatibility block;
- Exact does not gain removal authority from Unknown;
- scoped Restore of a Supported category can proceed when another unselected category is incompatible;
- Restore Skip removes an incompatible target from effective intent;
- requirement-linked compatibility remains blocked until external remediation and replanning;
- post-approval compatibility change returns `ErrRestorePlanChanged`;
- post-approval improvement also returns `ErrRestorePlanChanged`;
- verification uses the same RestoreContext and compatibility-driven intent.

### 4. CLI tests

Human:

- compatibility renders before operations;
- Unknown is visibly distinct from Supported;
- blocked reason is actionable;
- profile last capture is labeled as context, not authoritative source runtime.

JSON:

- exact stable keys;
- no omission of Unknown;
- deterministic arrays;
- old `omarchy_from`/`omarchy_to` remain present;
- compatibility object is present on Restore plans.

### 5. TUI tests

- review displays the same category states as workflow;
- blocking compatibility prevents Apply;
- details use shared finding data;
- no TUI-only version comparison;
- terminal/interactive behavior from ADR 0022 remains unchanged.

### 6. Profile backward-compatibility fixtures

Cover:

- current schema profile with normal Omarchy metadata;
- supported older schema fixture after existing load-time migration;
- profile with blank/missing Omarchy metadata;
- partial-Capture scenario where manifest last-capture version differs from historical target provenance.

The partial-Capture test is critical: it must prove the implementation never treats global captured version as per-target source authority.

### 7. Mutation-safety tests

Instrument fake runners/filesystems so compatibility inspection fails the test if it observes a mutating command.

Specifically guard against:

- package sync/update/install/remove;
- `omarchy update`;
- `sudo`;
- profile Save;
- policy mutation.

### 8. Real migration acceptance

Deferred to a separate final slice after the functional domain implementation is stable.

The acceptance scenario should:

1. retain Reconstruction Assurance's same-runtime scenario unchanged;
2. add a distinct migration scenario;
3. use two intentionally different supported ready Omarchy runtimes/substrates only when the harness can provision them reproducibly;
4. assert compatibility preview before any migration mutation;
5. independently verify both permitted and blocked/reduced outcomes.

Real-Omarchy CI is not required for each pure compatibility logic iteration.

## Documentation changes in implementation

Expected public documentation updates include:

- `docs/cli.md` — Restore compatibility rendering/JSON/applicability;
- `docs/tui.md` — Restore review parity;
- provider/category docs where compatibility evidence needs explanation;
- ADR 0023 references;
- this design spec.

Reconstruction Assurance docs are not rewritten to make cross-version migration part of its existing claim.

A later migration acceptance slice may amend acceptance docs separately.

## Likely implementation boundaries

This is not the implementation plan, but the architecture suggests reviewable boundaries.

A likely sequence is:

1. compatibility domain model, validator and RestorePlan JSON;
2. provider-local compatibility contributions plus workflow applicability/approval integration;
3. CLI/TUI parity and public docs;
4. later migration acceptance scenario after functional behavior is stable.

The implementation plan may split these differently once exact file/test dependencies are enumerated.

No implementation slice should require the Reconstruction Assurance harness to define the core domain behavior.

## Success criteria

Migration Safety / Compatibility v1 is complete when:

1. every current Restore category deliberately reports compatibility for effective intent;
2. Restore preview exposes Supported/Unknown/Incompatible before mutation;
3. Unknown never renders as compatible;
4. blocking compatibility makes the selected plan inapplicable before mutation;
5. explicit scoped Restore remains available;
6. Force cannot bypass compatibility;
7. Exact gains no authority from unknown compatibility;
8. external remediation requires replanning;
9. approval covers the exact compatibility state that will be applied;
10. verification uses the same effective intent;
11. CLI JSON and TUI represent the same shared result;
12. older profiles remain usable without recapture;
13. read-only inspection remains non-mutating;
14. Reconstruction Assurance's same-ready-runtime invariant remains unchanged.

## Open decisions

No product-level design decisions remain open after conversational approval.

Implementation planning still needs to determine exact code-file boundaries and the smallest provider probe changes needed to satisfy each category's evidence contract without creating redundant detection.

Those are implementation choices, not permission to revisit the approved product semantics silently.
