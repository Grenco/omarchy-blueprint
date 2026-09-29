# Services v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add safe, semantic reconstruction of persistent custom `systemd --user` services, including reviewed ownership, persistent definitions/drop-ins, enablement and masks, templates/instances, bounded desired absence, migration compatibility, optional post-Restore activation, CLI/TUI parity, and real-Omarchy Reconstruction Assurance.

**Architecture:** Services becomes a normal Blueprint provider backed by a schema-14 human-readable profile representation. A provider-local systemd inspection layer discovers effective user units and maps them to persistent sources; Capture owns only reviewed targets, while Restore produces one shared `workflow.RestoreFragment` containing operations, requirements, skips and compatibility. Persistent desired state remains separate from active-at-capture evidence and post-Restore activation authority.

**Tech Stack:** Go, existing Blueprint provider/workflow/profile/policy abstractions, `systemctl --user` and authoritative non-mutating systemd verification facilities available on supported Omarchy, Bubble Tea TUI, Cobra-style existing CLI/application layer, real Omarchy Reconstruction Assurance VM harness.

**Spec:** `docs/planning/specs/2026-09-27-services-v1-design.md`

**ADR:** `docs/adr/0024-semantic-user-service-reconstruction.md`

**Baseline:** `main` at or after `20c9c7d614b5bd9ce964c18d7c5198617dff7ff4`. Before implementation, rebase/refresh against current `main` and preserve any parallel Reconstruction Assurance or Migration Acceptance additions.

## Global Constraints

- Services v1 manages only persistent `systemd --user` intent.
- System/root services and `/etc/systemd/system` are out of scope.
- Discovery never implies Blueprint ownership.
- New top-level Services candidates require reviewed Capture before adoption.
- Selecting a unit may preselect related custom unit dependencies; users can deselect them.
- `.service` and `.timer` are recommended types.
- `.socket`, `.path`, `.target`, and `.slice` are advanced explicit selections.
- `.scope`, `.device`, `.mount`, `.automount`, `.swap`, generated units, transient units, and unknown unsupported kinds are not portable Services desired state.
- Exact authored unit/drop-in bytes remain human-readable in the profile.
- Package-, distro-, Omarchy-, and otherwise external base unit definitions remain external.
- Blueprint may manage user-owned drop-ins, masks, and safe selected user-level state layered over an external base without adopting that base.
- `disabled` is positive desired state, not desired absence.
- Exact removes only explicit desired absence for exact previously Blueprint-managed service artifacts.
- Force controls conflicts only. It does not bypass ownership, compatibility, sensitivity, Exact authority, or activation authority.
- Active-at-capture is evidence, not desired-state convergence.
- Inactive-at-capture never authorizes stopping a destination unit.
- Services v1 does not automatically restart/reload running application processes.
- Post-Restore activation is independent explicit authority.
- Headless Restore never gains activation authority implicitly.
- Services must contribute deliberate Migration Safety compatibility evidence; omission never means Supported.
- Read-only inspection and validation must not mutate the system, enable linger, invoke `sudo`, or repair state.
- Plan intent and Verify intent must remain identical.
- Approved Restore recalculation must include Services compatibility and activation effects.
- CLI and TUI use shared workflow/provider domain results.
- User-facing copy explains consequences in ordinary language before raw systemd terminology.
- Services v1 is not complete until real-Omarchy Reconstruction Assurance covers it.

## Review Focus

1. **Application-installed units in user-writable paths:** they must remain candidates/external state rather than being silently acquired because a path looks user-owned. Task 3 pins ambiguous provenance behavior.
2. **External base with user drop-in or mask:** Restore must mutate only the selected overlay and never copy/delete the base. Tasks 4 and 6 pin this.
3. **Active-at-capture plus headless Restore:** active evidence must not cause an unattended process start without explicit run authority. Task 8 pins this.
4. **Exact with unknown/changed provenance:** compatibility uncertainty must withhold deletion rather than broadening authority. Tasks 6 and 7 pin this.
5. **Changed service already running:** Blueprint must update persistent state without silently restarting the process and must verify only what it promised. Tasks 8 and 9 pin this.

---

## Proposed implementation / PR sequence

The tasks below are individually reviewable. The recommended PR grouping is:

```text
PR A — Services profile + domain model
  Tasks 1–2

PR B — Read-only discovery + reviewed Capture ownership
  Tasks 3–5

PR C — Persistent Restore + compatibility + Verify
  Tasks 6–7

PR D — Activation authority + CLI/TUI parity
  Tasks 8–10

PR E — Real-Omarchy Reconstruction Assurance + docs closeout
  Task 11
```

Do not combine PR C and PR D merely to reduce PR count. Activation is a distinct side-effect authority and deserves an independent review boundary.

---

### Task 1: Add schema-14 Services profile state and persistence

**Files:**

- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/provider_state.go`
- Modify: `internal/profile/managed_paths.go`
- Modify: `internal/profile/profile_test.go`
- Modify: `internal/profile/managed_paths_test.go`
- Create: `internal/profile/services.go`
- Create: `internal/profile/services_test.go`

**Interfaces:**

- Consumes: existing profile load/save, schema migration, atomic-write and desired-state validation conventions.

- Produces:

  - `profile.Services`
  - `profile.ServiceUnit`
  - normalized enums/types for management mode, desired presence, persistent start state, activation preference
  - `Data.Services`
  - `Manifest.Capture.Services`
  - schema-14 loader/saver for `services/services.toml`
  - managed authored files rooted under `services/units/`

- [ ] **Step 1: Write failing schema/profile round-trip tests**

Add tests covering:

```go
func TestServicesRoundTrip(t *testing.T)
func TestServicesExternalCustomizationDoesNotRequireBaseDefinition(t *testing.T)
func TestServicesTemplateDefinitionStoredOnce(t *testing.T)
func TestServicesDesiredAbsenceCannotAlsoBePresent(t *testing.T)
func TestSchema13ProfileLoadsWithEmptyServices(t *testing.T)
```

Assertions must prove:

- schema increments from `13` to `14`;

- a managed definition round-trips;

- an overlay-only unit round-trips without a copied base definition;

- enabled, disabled, masked, indirect/not-managed state can be represented without conflating absence;

- `ObservedActive` and activation preference remain distinct from persistent desired state;

- explicit absence cannot coexist with the same exact managed artifact as present state;

- old profiles load with empty Services state.

- [ ] **Step 2: Run profile tests and verify failure**

Run:

```bash
go test ./internal/profile -run 'TestServices|TestSchema13ProfileLoadsWithEmptyServices'
```

Expected: FAIL because Services profile types/schema persistence do not exist.

- [ ] **Step 3: Define focused Services profile types**

Create `internal/profile/services.go`.

Use explicit normalized concepts rather than raw `systemctl` strings.

Required semantic fields:

```go
type ServiceManagementMode string
type ServicePresence string
type ServiceStartIntent string
type ServiceActivationPreference string

type ServiceUnit struct {
    Name                 string
    Kind                 string
    Management           ServiceManagementMode
    Presence             ServicePresence
    StartIntent          ServiceStartIntent
    ActivationPreference ServiceActivationPreference
    ObservedActive       bool
    Definition           string
    DropIns              []ServiceArtifact
    Instances            []ServiceInstance
    LinkedSource         string
}

type Services struct {
    Units []ServiceUnit
}
```

Exact field spelling may follow repository JSON/TOML style, but semantic distinctions above are required.

`ServiceStartIntent` must distinguish at least:

```text
enabled
disabled
masked
indirect
not-managed
```

`Presence` must not use `disabled` as absence.

- [ ] **Step 4: Add schema-14 profile loading/saving**

Modify `internal/profile/profile.go` so:

```go
const Schema = 14
const servicesSchema = 14
```

Add:

```go
Services bool `toml:"services"`
```

to `CaptureMeta` and:

```go
Services Services `json:"services"`
```

to `Data`.

Persist metadata at:

```text
services/services.toml
```

Persist exact managed authored files only under:

```text
services/units/
```

Do not store external base definitions.

- [ ] **Step 5: Extend desired-state validation and deterministic sorting**

Modify `validateProviderDesiredState` / `sortProviderDesiredState`.

Validation must reject:

- empty unit identity;

- duplicate unit identity;

- present and explicitly absent definition for the same artifact;

- duplicate drop-in paths;

- present + absent same drop-in;

- duplicate template instances;

- invalid management/start-intent combinations such as an unmanaged external base carrying Blueprint-owned definition content.

- [ ] **Step 6: Extend managed-path accounting**

Ensure Services authored content participates in profile managed-path validation/cleanup without making arbitrary external service paths profile-managed.

- [ ] **Step 7: Run profile tests**

Run:

```bash
go test ./internal/profile
```

Expected: PASS.

- [ ] **Step 8: Run repository tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/profile
git commit -m "feat: add services profile state"
```

---

### Task 2: Introduce the Services provider domain and systemd abstraction

**Files:**

- Create: `internal/providers/services/provider.go`
- Create: `internal/providers/services/systemd.go`
- Create: `internal/providers/services/model.go`
- Create: `internal/providers/services/systemd_test.go`
- Create: `internal/providers/services/provider_test.go`

**Interfaces:**

- Consumes: `workflow.Provider`, `workflow.RestoreProvider`, `workflow.TargetInspection`, existing command execution abstractions.

- Produces:

  - `services.Provider`
  - provider ID `"services"`
  - an injectable read-only/mutating `Systemd` interface
  - normalized discovered-unit domain types independent of command output formatting

- [ ] **Step 1: Write failing parser/adapter tests**

Tests:

```go
func TestInspectNormalizesFragmentDropInsEnablementAndActiveState(t *testing.T)
func TestInspectSeparatesRuntimeGeneratedFromPersistentSources(t *testing.T)
func TestInspectTemplateAndInstances(t *testing.T)
func TestInspectDoesNotTreatRawSystemctlStateAsProfileEnum(t *testing.T)
```

Use deterministic fixture output rather than the host user manager.

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/providers/services
```

Expected: FAIL because provider/systemd adapter is absent.

- [ ] **Step 3: Define the systemd boundary**

Create an injectable interface with explicit read/write separation.

Conceptually:

```go
type Systemd interface {
    InspectUserUnits(ctx context.Context) ([]ObservedUnit, error)
    VerifyUnitSet(ctx context.Context, proposed ProposedUnitSet) error

    DaemonReload(ctx context.Context) error
    Enable(ctx context.Context, names ...string) error
    Disable(ctx context.Context, names ...string) error
    Mask(ctx context.Context, names ...string) error
    Unmask(ctx context.Context, names ...string) error
    Start(ctx context.Context, names ...string) error
}
```

Implementation may split read/write interfaces if that better matches existing dependency injection.

Do not expose `Restart`, `Stop`, or root/system operations in the v1 interface.

- [ ] **Step 4: Normalize observed unit state**

`ObservedUnit` must provide enough information for later tasks:

```text
unit name
unit kind
fragment path
drop-in paths
persistent/generated/runtime classification
raw enablement evidence
normalized persistent start state
active evidence
template/instance relationship
linked-source evidence
relevant systemd-unit dependency names
```

Do not put ownership decisions into the command adapter.

An uninstantiated template is visible in `list-unit-files` but cannot be
passed to `systemctl show`. Task 2 records its unit-file state and identity
with `TopologyKnown=false`; an empty fragment/drop-in list here is **unknown**,
not proof of no persistent source. Do not parse `systemctl cat` filename
comments as structured topology: authored unit comments have the same shape.
Task 3 must resolve effective template sources/drop-ins authoritatively before
offering ownership or Capture.

- [ ] **Step 5: Implement provider skeleton**

`Provider.ID()` returns:

```text
services
```

`CategoryEnabled()` follows existing category-provider convention.

At this task, `Captured`, `InspectTargets`, `Capture`, `Diff`, `Plan`, and `Verify` may return intentionally minimal deterministic values needed for compilation, but no production behavior should claim support that later tasks have not implemented.

- [ ] **Step 6: Verify no mutating command is used during inspection**

Pin this with a fake executor test that fails if discovery invokes:

```text
daemon-reload
enable
disable
mask
unmask
start
sudo
loginctl enable-linger
```

- [ ] **Step 7: Run provider tests**

```bash
go test ./internal/providers/services
```

Expected: PASS.

- [ ] **Step 8: Run repository tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/services
git commit -m "feat: add services provider domain"
```

---

### Task 3: Implement provenance-aware user-service discovery and target inspection

**Files:**

- Create: `internal/providers/services/discovery.go`
- Create: `internal/providers/services/discovery_test.go`
- Modify: `internal/providers/services/provider.go`
- Modify: `internal/workflow/capture_inspection.go` only if additional provider-neutral descriptive metadata is required
- Modify/Test: `internal/workflow/capture_inspection_test.go` only for provider-neutral additions

**Interfaces:**

- Consumes: `ObservedUnit` from Task 2 and profile.Services from Task 1.

- Produces:

  - deterministic Services `TargetInspection`s
  - provenance classification
  - recommended vs advanced classification
  - external-overlay targets
  - stable fingerprints for approval stability

- [ ] **Step 1: Write failing discovery tests**

Tests must cover Review Focus item 1:

```go
func TestDiscoveryStrongUserConfigIsCandidateNotOwned(t *testing.T)
func TestDiscoveryUserDataApplicationUnitIsAmbiguousCandidate(t *testing.T)
func TestDiscoveryPackageBaseIsExternal(t *testing.T)
func TestDiscoveryExternalBaseWithUserDropInOffersCustomization(t *testing.T)
func TestDiscoveryPersistentUserMaskOffersCustomization(t *testing.T)
func TestDiscoveryGeneratedAndRuntimeUnitsAreExcluded(t *testing.T)
func TestDiscoveryAdvancedKindsAreMarkedAdvanced(t *testing.T)
func TestDiscoveryScopesDevicesMountsAndUnknownKindsAreExcluded(t *testing.T)
func TestDiscoveryLinkedUnitRecordsExternalSource(t *testing.T)
func TestDiscoveryUnresolvedTemplateCannotBeAdopted(t *testing.T)
func TestDiscoveryTemplateCommentsCannotInventDropIns(t *testing.T)
func TestDiscoveryTemplateAliasesAndBroadDropInsRemainUnknownUntilResolved(t *testing.T)
```

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/providers/services -run TestDiscovery
```

Expected: FAIL.

- [ ] **Step 3: Implement provenance classification**

Introduce a provider-local enum such as:

```go
type ProvenanceClass string

const (
    ProvenanceUserConfig
    ProvenanceUserDataAmbiguous
    ProvenanceExternal
    ProvenanceRuntime
)
```

Classification may use:

- effective fragment path;
- target XDG/home roots;
- package ownership evidence where authoritative;
- known Omarchy ownership evidence;
- generated/transient classification;
- link/source relationships.

Path alone is evidence, not proof.

For `TopologyKnown=false` templates, resolve effective source/drop-in paths
using structured systemd metadata or another independently authoritative
read-only mechanism that covers aliases, instance/template drop-ins,
dash-prefix rules and type-wide directories. Never infer topology from
authored `# /path` comments, and never treat an empty unresolved list as
evidence that no drop-ins exist. If a safe resolver is unavailable, keep
the template visible as Unknown/ineligible for first adoption without
aborting the whole Services inventory; loaded instances may still use their
own structured `show` evidence.

- [ ] **Step 4: Map discovered units to stable workflow targets**

Target key is based on user-unit identity, not source path.

Recommended forms:

```text
backup.service
backup.timer
backup@.service
backup@photos.service
pipewire.service
```

Use deterministic subordinate metadata/fingerprints for definition/drop-in changes.

- [ ] **Step 5: Make reasons user-understandable**

Target labels/reasons should use product language such as:

```text
Custom user service
Application-installed user service
Managed customization
External service
Definition stored elsewhere
Blocked from starting
```

Do not require the UI to reinterpret raw `enabled`, `static`, `linked`, etc.

- [ ] **Step 6: Verify inspection remains read-only**

Run:

```bash
go test ./internal/providers/services -run 'TestDiscovery|TestInspect'
```

Expected: PASS with no mutating executor calls.

- [ ] **Step 7: Run workflow inspection tests**

```bash
go test ./internal/workflow -run CaptureInspection
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/services internal/workflow
git commit -m "feat: discover user service candidates"
```

---

### Task 4: Implement reviewed ownership, dependencies, and Services Capture

**Files:**

- Create: `internal/providers/services/capture.go`
- Create: `internal/providers/services/capture_test.go`
- Create: `internal/providers/services/dependencies.go`
- Create: `internal/providers/services/dependencies_test.go`
- Modify: `internal/providers/services/provider.go`
- Modify: `internal/app/capture_review_cli.go`
- Modify: `internal/app/change_targets.go` if target-selection mapping requires Services support
- Modify/Test: corresponding app tests

**Interfaces:**

- Consumes: discovery targets from Task 3 and existing `workflow.CaptureInspection` / `CaptureApproved`.

- Produces:

  - first-adoption selection semantics
  - dependency recommendations
  - Capture merge behavior for already-managed Services state
  - reviewed explicit absence

- [ ] **Step 1: Write failing ownership tests**

Required tests:

```go
func TestPlainCaptureDoesNotAdoptNewServiceCandidate(t *testing.T)
func TestReviewedCaptureCanAdoptSelectedDefinition(t *testing.T)
func TestSelectingUnitPreselectsCustomDependencies(t *testing.T)
func TestDependencyCanBeDeselectedAndRemainsExternal(t *testing.T)
func TestCaptureUpdateRefreshesAlreadyManagedService(t *testing.T)
func TestCapturePreserveFreezesManagedService(t *testing.T)
func TestReviewedRemovalRecordsExactDesiredAbsence(t *testing.T)
func TestMissingManagedUnitWithoutReviewedRemovalPreservesDesiredState(t *testing.T)
func TestOverlayCaptureStoresDropInWithoutExternalBase(t *testing.T)
```

- [ ] **Step 2: Run failing tests**

```bash
go test ./internal/providers/services -run 'TestPlainCapture|TestReviewedCapture|TestSelecting|TestDependency|TestCapture|TestOverlay'
```

Expected: FAIL.

- [ ] **Step 3: Add provider-neutral Capture selection only if required**

The existing `capture --review` currently previews and approves a complete `CaptureInspection`.

Services needs reviewed include/exclude choices, not just yes/no approval of every default outcome.

Prefer the smallest provider-neutral extension that can represent:

```go
type CaptureSelection struct {
    Category string
    Key      string
    Include  bool
}
```

or equivalent deterministic authority.

Do not implement Services-only approval logic inside the TUI/CLI.

Approval stability must compare the selected candidate set as well as live fingerprints.

- [ ] **Step 4: Implement Services dependency recommendations**

Use authoritative unit relationships from inspected systemd topology.

Rules:

- parent selected → relevant custom dependencies default selected;

- user can deselect each;

- external units are explanatory context, not silently selected ownership;

- no package/executable inference from `ExecStart`.

- [ ] **Step 5: Implement Capture merge**

Capture selected persistent state:

- exact definition bytes when Blueprint manages the definition;
- selected exact drop-ins;
- persistent normalized enablement/mask state;
- template/instance intent;
- active-at-capture evidence;
- activation preference;
- linked source relationship without copying an external linked source.

Use existing sensitive-content checks before committing authored bytes.

- [ ] **Step 6: Implement reviewed explicit absence**

Only previously Blueprint-managed:

```text
definition
drop-in
mask/overlay artifact
template instance
```

may acquire desired-absence intent.

Unmanaged missing candidates never become tombstones.

Record the prior managed definition hash and drop-in hash/mode with each
reviewed absence. A previously selected mask/instance needs equally explicit
managed identity evidence. Schema 14 may load an incomplete historical or
hand-edited tombstone, but its presence alone is not deletion proof.

- [ ] **Step 7: Pin approval-change behavior**

Add test:

```go
func TestReviewedCaptureRejectsChangedServiceCandidateAfterApproval(t *testing.T)
```

Change a fragment/drop-in/dependency/fingerprint between preview and apply.

Expected: existing Capture approval-change error.

- [ ] **Step 8: Run provider/workflow/app tests**

```bash
go test ./internal/providers/services ./internal/workflow ./internal/app
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/services internal/workflow internal/app
git commit -m "feat: capture reviewed user services"
```

---

### Task 5: Wire Services into application provider construction, status, diff, policy, and basic TUI inventory

**Files:**

- Modify: `internal/app/app.go`
- Modify: application dependency/provider-construction helpers
- Modify: `internal/app/inspect.go`
- Modify: `internal/app/policy_cli.go` only where category registration is explicit
- Modify: relevant app tests
- Modify: `internal/tui/...` provider/category inventory files
- Modify: relevant TUI tests

**Interfaces:**

- Consumes: `services.Provider` from Tasks 2–4.

- Produces: Services as a first-class category everywhere generic provider registration should expose it.

- [ ] **Step 1: Write failing provider-registration tests**

Assert:

- `services` appears in enabled provider IDs;

- category-scoped `capture services`, `status`, `diff`, and policy resolution find it;

- old profiles without Services do not incorrectly report captured Services state;

- CLI and TUI use the same provider ID/target inventory.

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/app ./internal/tui
```

Expected: new registration tests FAIL.

- [ ] **Step 3: Wire `services.Provider` at the application boundary**

Follow existing construction style.

Provider-specific systemd dependencies remain injected from the app boundary rather than created inside workflow.

- [ ] **Step 4: Extend category labels/help**

Primary name:

```text
Services
```

Avoid raw “systemd provider” terminology in ordinary category selection.

- [ ] **Step 5: Run app/TUI tests**

```bash
go test ./internal/app ./internal/tui
```

Expected: PASS.

- [ ] **Step 6: Run repository tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app internal/tui
git commit -m "feat: register services category"
```

---

### Task 6: Implement persistent Restore planning, conflicts, Exact bounds, and systemd validation

**Files:**

- Create: `internal/providers/services/plan.go`
- Create: `internal/providers/services/plan_test.go`
- Create: `internal/providers/services/validate.go`
- Create: `internal/providers/services/validate_test.go`
- Modify: `internal/providers/services/provider.go`
- Modify: shared operation/executor files only if Services needs new operation kinds that cannot be represented safely by existing ones
- Test: corresponding restore/executor tests

**Interfaces:**

- Consumes: `workflow.RestoreContext`, Services desired state, live discovery, systemd validation abstraction.

- Produces: `workflow.RestoreFragment` with persistent Services operations, skips, requirements and placeholder/real compatibility supplied in Task 7.

- [ ] **Step 1: Write failing persistent-plan tests**

Cover:

```go
func TestPlanCreatesMissingManagedDefinition(t *testing.T)
func TestPlanUpdatesManagedDropIn(t *testing.T)
func TestPlanConvergesEnabledDisabledAndMaskedValues(t *testing.T)
func TestPlanSafePreservesUnexpectedDefinitionCollision(t *testing.T)
func TestPlanForceMayReplaceSafeUserOwnedManagedConflict(t *testing.T)
func TestPlanForceCannotReplaceExternalBase(t *testing.T)
func TestPlanAdditiveDoesNotApplyDesiredAbsence(t *testing.T)
func TestPlanExactRemovesOnlyExplicitManagedAbsence(t *testing.T)
func TestPlanExactMissingTombstoneProvenanceWithholdsRemoval(t *testing.T)
func TestPlanExactNeverDeletesUnmanagedUnit(t *testing.T)
func TestPlanExternalOverlayNeverCopiesOrDeletesBase(t *testing.T)
func TestPlanInvalidProposedUnitBlocksBeforeMutation(t *testing.T)
func TestPlanUnavailableUserManagerCreatesRequirement(t *testing.T)
```

Review Focus items 2 and 4 must be explicit.

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/providers/services -run TestPlan
```

Expected: FAIL.

- [ ] **Step 3: Implement proposed-set validation**

Construct a proposed effective Services set from:

```text
desired managed content
+ external base/current content needed for validation
+ selected mutations
```

Call the authoritative non-mutating systemd verifier through Task 2's interface.

No temporary target writes or daemon-reload as a validation probe.

If supported systemd tooling requires temporary files, they must live in an isolated temp directory and must not alter user-manager state.

- [ ] **Step 4: Implement Safe/Force semantics**

Safe:

- collision → skip/conflict;
- external base remains protected.

Force:

- may replace an explicitly managed user-controlled conflict;

- may not seize an external base;

- may not override validation/sensitivity/compatibility.

- [ ] **Step 5: Implement Additive/Exact semantics**

Positive values converge in either mode when safe:

```text
enabled
disabled
masked
```

Desired absence is removal authority only under Exact.

Deletion target must match an exact previously Blueprint-managed service artifact.
Missing prior definition hash or drop-in hash/mode, or unproved mask/instance
ownership, grants **zero** Exact deletion authority: keep a visible skip and
leave Verify non-converged when the selected exact target is still present.
An already-absent target needs no deletion authority. Do not infer proof from
a tombstone's existence or from a matching filename.

- [ ] **Step 6: Plan semantic systemd operations**

Persistent operation order must support:

```text
file writes/removals
→ daemon-reload
→ enable/disable/mask/unmask
```

Do not add activation in this task.

- [ ] **Step 7: Ensure journal/executor support is explicit**

If new operation types are required, they must:

- serialize deterministically in plan JSON;

- appear in approved-plan equality;

- execute only after journal creation;

- fail closed;

- be testable without real systemd.

- [ ] **Step 8: Run Services/restore/workflow tests**

```bash
go test ./internal/providers/services ./internal/restore ./internal/workflow
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/services internal/restore internal/workflow internal/model
git commit -m "feat: plan persistent service restore"
```

---

### Task 7: Add Services Migration Safety evidence and persistent Verify

**Files:**

- Create: `internal/providers/services/compatibility.go`
- Create: `internal/providers/services/compatibility_test.go`
- Create: `internal/providers/services/verify.go`
- Create: `internal/providers/services/verify_test.go`
- Modify: `internal/providers/services/plan.go`
- Modify: `internal/providers/services/provider.go`

**Interfaces:**

- Consumes: the merged `workflow.RestoreFragment.Compatibility` contract.

- Produces:

  - deliberate Services compatibility category
  - Services persistent Verify result aligned with the plan

- [ ] **Step 1: Write failing compatibility tests**

Required:

```go
func TestCompatibilitySupportedRequiresAffirmativeSystemdEvidence(t *testing.T)
func TestCompatibilitySkippedServiceIntentIsNotFalselySupported(t *testing.T)
func TestCompatibilityMissingExternalBaseBlocksAffectedOverlay(t *testing.T)
func TestCompatibilityUnknownDependencyReducesActivationOrExactAuthority(t *testing.T)
func TestCompatibilityUnknownNeverGrantsExactRemoval(t *testing.T)
func TestCompatibilityInvalidUnitIsIncompatible(t *testing.T)
func TestCompatibilityUserManagerRequirementLinksFinding(t *testing.T)
```

No empty/default compatibility result may serialize as Supported.

- [ ] **Step 2: Run compatibility tests and verify failure**

```bash
go test ./internal/providers/services -run TestCompatibility
```

Expected: FAIL.

- [ ] **Step 3: Implement compatibility assessment from the same inspected intent as planning**

Evidence may include:

- usable user manager;
- supported unit kind;
- parser/validator support;
- required external base present;
- linked source present;
- selected dependency available/managed;
- semantic operation supported.

Version equality alone is not evidence.

- [ ] **Step 4: Tie reductions to real plan behavior**

When compatibility reduces authority, the plan must actually omit/skip/reduce the affected operation.

Never produce:

```text
compatibility = reduced
```

while still planning the same destructive/activation effect.

- [ ] **Step 5: Write failing Verify tests**

Required:

```go
func TestVerifyDefinitionAndDropIns(t *testing.T)
func TestVerifyExactDesiredAbsence(t *testing.T)
func TestVerifyEnabledDisabledAndMaskedIntent(t *testing.T)
func TestVerifyTemplateInstanceIntent(t *testing.T)
func TestVerifyIgnoresUnrelatedUserUnits(t *testing.T)
func TestVerifyDoesNotRequireRuntimeActiveForPersistentOnlyIntent(t *testing.T)
```

- [ ] **Step 6: Implement persistent Verify**

Verify the same resolved target scope and semantic values the plan applied.

Do not compare entire user-unit namespace.

Do not require running state in this task.

- [ ] **Step 7: Add plan-stability test**

Modify provider state/compatibility evidence after approval and before Apply.

Expected:

```text
approved Restore rejected because recalculated plan differs
```

- [ ] **Step 8: Run compatibility/workflow/provider tests**

```bash
go test ./internal/providers/services ./internal/workflow ./internal/app
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/services
git commit -m "feat: verify service restore compatibility"
```

---

### Task 8: Add explicit post-Restore activation authority

**Files:**

- Create: `internal/providers/services/activation.go`
- Create: `internal/providers/services/activation_test.go`
- Modify: `internal/providers/services/plan.go`
- Modify: `internal/providers/services/verify.go`
- Modify: `internal/workflow/restore.go` / Restore options model as required
- Modify: `internal/model/...` for public plan representation if needed
- Modify corresponding workflow tests

**Interfaces:**

- Consumes:

  - `ObservedActive`
  - per-target activation preference
  - run-level Restore activation mode
  - safety/compatibility result

- Produces:

  - run-level activation mode
  - explicit activation operations in the approved Restore plan

- [ ] **Step 1: Define run-level activation mode**

Use a distinct type, not booleans mixed into convergence:

```go
type ActivationMode string

const (
    ActivationPersistentOnly
    ActivationRestoreWorkingState
    ActivationReview
)
```

Exact package placement should follow Restore option ownership.

Do not add activation to `policy.ConflictMode` or `policy.ConvergenceMode`.

- [ ] **Step 2: Write failing activation-authority tests**

Required:

```go
func TestPersistentOnlyNeverStartsService(t *testing.T)
func TestRestoreWorkingStateRequiresObservedActiveEvidence(t *testing.T)
func TestPerTargetPreferenceCanWithholdActivation(t *testing.T)
func TestCompatibilityCanWithholdActivationWithoutBlockingPersistentRestore(t *testing.T)
func TestInactiveAtCaptureNeverPlansStop(t *testing.T)
func TestHeadlessRestoreDoesNotImplicitlyActivate(t *testing.T)
func TestChangedAlreadyRunningServiceIsNotRestarted(t *testing.T)
```

This pins Review Focus items 3 and 5.

- [ ] **Step 3: Implement activation authority intersection**

An activation operation is legal only when all required factors allow it:

```text
observed active at capture
AND target activation preference
AND current Restore run activation permission
AND service safety
AND compatibility/readiness
```

No factor independently grants authority.

- [ ] **Step 4: Implement semantic entry-point selection**

Tests:

```go
func TestTimerActivationStartsTimerNotService(t *testing.T)
func TestSocketActivationStartsSocketNotService(t *testing.T)
func TestPathActivationStartsPathNotService(t *testing.T)
```

Targets should follow systemd activation semantics.

- [ ] **Step 5: Exclude stop/restart/reload authority**

The v1 Systemd mutator must still have no automatic:

```text
Stop
Restart
Reload application process
```

If existing generic executor can represent them, Services simply never plans them.

- [ ] **Step 6: Extend Verify for approved activation**

Verify activation only when the plan contained activation.

Do not assume every successful unit remains long-lived `active`.

Tests must include a oneshot-like service where successful activation is not represented by a permanently running process.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/providers/services ./internal/workflow
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/services internal/workflow internal/model
git commit -m "feat: add service restore activation"
```

---

### Task 9: Expose Services activation and explanations in CLI/JSON

**Files:**

- Modify: `internal/app/app.go`
- Modify: Restore option parsing/rendering files
- Modify: `internal/app/capture_review_cli.go`
- Modify: `internal/app/change_targets.go` if selection UI uses it
- Modify: relevant app tests
- Modify: JSON golden/contract tests where present

**Interfaces:**

- Consumes: shared Services candidate/plan/activation domain.

- Produces:

  - reviewed CLI Capture selections
  - Restore activation option
  - human-readable service explanations
  - stable JSON plan/candidate output

- [ ] **Step 1: Write failing CLI tests**

Cover:

```text
capture --review services
capture --dry-run services
--json capture --review services
restore services --dry-run
restore services --dry-run --json
headless restore services --yes
explicit activation-mode restore
```

- [ ] **Step 2: Pin headless activation behavior**

Without an explicit activation option, non-interactive mutating Restore must choose/require:

```text
Persistent state only
```

It must not infer Restore Working State.

- [ ] **Step 3: Add explicit CLI activation option**

Choose final spelling during implementation inspection, but the contract must support all shared modes.

Recommended shape:

```text
--activation persistent
--activation working
--activation review
```

Do not overload:

```text
--force
--exact
--yes
```

- [ ] **Step 4: Improve reviewed Capture UX**

New top-level candidates:

```text
unselected
```

After parent selection:

```text
recommended dependency = selected by default
```

Users must be able to turn recommended dependencies off.

CLI copy must explain:

```text
Custom user service
Managed customization
External service
Definition stored elsewhere
Starts automatically
Available but not automatic
Blocked from starting
Currently running
Restore working state
Persistent state only
```

- [ ] **Step 5: Render activation consequences in Restore preview**

Examples to test:

```text
Will activate after Restore because it was active when captured.
Will not start during Restore.
Already running; Blueprint will not restart it automatically.
Activation withheld because an external dependency is missing.
```

- [ ] **Step 6: Ensure JSON uses machine-readable semantic values**

Human labels do not replace stable JSON enums/fields.

- [ ] **Step 7: Run app tests**

```bash
go test ./internal/app
```

Expected: PASS.

- [ ] **Step 8: Run repository tests**

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/app
git commit -m "feat: expose services in cli"
```

---

### Task 10: Add full TUI Services review and Restore parity

**Files:**

- Modify/Create focused files under `internal/tui/`
- Modify: shared TUI Restore review rendering
- Modify: shared Capture review rendering
- Add: TUI tests for Services interaction/parity

**Interfaces:**

- Consumes: exactly the same candidate, policy, compatibility, plan and activation objects as CLI.

- Produces: Services-specific presentation only; no duplicate safety/business logic.

- [ ] **Step 1: Write failing TUI tests**

Cover:

- new service candidate begins unselected;

- selecting parent preselects recommended dependency;

- dependency can be deselected;

- advanced units hidden until expanded;

- managed external customization clearly distinguishes base vs overlay;

- mask copy says “Blocked from starting”;

- linked source says “Definition stored elsewhere”;

- activation mode selection maps to the shared Restore activation mode;

- compatibility finding appears from shared Restore plan;

- changed running service displays no-restart warning.

- [ ] **Step 2: Run TUI tests and verify failure**

```bash
go test ./internal/tui
```

Expected: FAIL.

- [ ] **Step 3: Implement Services Capture review presentation**

Do not compute dependency recommendations in the TUI.

Render provider-supplied candidate/dependency facts.

- [ ] **Step 4: Implement Restore activation controls**

TUI choices correspond exactly to shared modes:

```text
Restore working state
Persistent state only
Review activation
```

No TUI-only default may grant more authority than the CLI/domain model.

- [ ] **Step 5: Render provider-supplied reasons**

Avoid duplicating systemd classification logic in view code.

- [ ] **Step 6: Add CLI/TUI parity test**

Given the same inspected profile/machine fixture, assert the two surfaces expose the same:

```text
target identities
candidate selection facts
persistent operations
compatibility state
activation operations
```

Presentation text may differ in layout.

- [ ] **Step 7: Run app/TUI/workflow tests**

```bash
go test ./internal/app ./internal/tui ./internal/workflow
```

Expected: PASS.

- [ ] **Step 8: Run complete static verification**

```bash
go test ./...
go vet ./...
go build ./...
```

Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui internal/app
git commit -m "feat: add services tui workflow"
```

---

### Task 11: Extend real-Omarchy Reconstruction Assurance for Services v1

**Files:**

- Modify: Reconstruction Assurance fixture/customization scripts
- Modify: RA Machine A Capture setup
- Modify: RA Machine B Restore/assertion scripts
- Modify: RA workflow/harness files as required
- Modify: RA evidence/documentation
- Modify: `ROADMAP.md` only when completion evidence exists

**Interfaces:**

- Consumes: fully integrated Services provider.

- Produces: independent proof that Services desired state reconstructs on a fresh Omarchy Machine B.

- [ ] **Step 1: Add deterministic custom user-service fixture on Machine A**

Use a non-networked, non-privileged fixture.

Recommended fixture:

```text
blueprint-ra-marker.service
blueprint-ra-marker.timer
blueprint-ra-marker.service.d/10-ra.conf
```

The service should produce a deterministic harmless user-file marker or equivalent observable result.

Avoid wall-clock-dependent assertions.

- [ ] **Step 2: Make Capture explicitly adopt the fixture**

The RA workflow must exercise the first-adoption path rather than pre-seeding profile state by hand.

If reviewed selection is awkward in the VM harness, use the same PTY helper pattern already established for interactive Capture.

- [ ] **Step 3: Assert profile artifacts independently**

After Machine A Capture, independently verify:

```text
Services metadata exists
definition bytes captured
drop-in captured
timer/service relationship represented
enablement intent captured
external/unmanaged units were not swept into the profile
```

- [ ] **Step 4: Destroy Machine A and Restore on Machine B**

Retain the canonical RA invariant:

```text
only profile crosses A → B
```

- [ ] **Step 5: Add independent Machine B persistent-state assertions**

Use native/systemd checks independent of Blueprint Verify to confirm:

```text
definition
drop-in
persistent timer enablement
systemd loadability
```

- [ ] **Step 6: Exercise activation deterministically**

If the service fixture can make activation deterministic:

- Capture it active on A.
- Restore with explicit Restore Working State on B.
- Assert the correct entry point was activated.
- Assert the expected marker/result.

Do not make the gate depend on timer wall-clock firing.

If reliable activation cannot be made deterministic without compromising the harness, keep activation covered in provider/integration tests and document why RA gates persistent reconstruction only.

- [ ] **Step 7: Run Blueprint Verify**

Assert Services verification completes successfully as part of the normal RA Verify phase.

- [ ] **Step 8: Run one complete local/canonical RA pass**

Expected: all canonical phases PASS.

- [ ] **Step 9: Run CI-required Reconstruction Assurance**

Use the existing required workflow path.

Expected:

```text
Reconstruction Assurance = success
```

Any parallel migration-VM testing may remain a separate lane; Services completion does not require merging unrelated migration-acceptance expansion unless branch protection/current roadmap explicitly makes it a dependency.

- [ ] **Step 10: Update roadmap/docs only after evidence passes**

Mark Services v1 complete only after:

```bash
go test ./...
go vet ./...
go build ./...
```

and required RA evidence are green.

- [ ] **Step 11: Commit**

```bash
git add .github scripts docs ROADMAP.md
git commit -m "test: assure services reconstruction"
```

---

## Whole-branch verification

After all tasks:

- [ ] Run unit/integration tests:

```bash
go test ./...
```

Expected: PASS.

- [ ] Run static checks:

```bash
go vet ./...
```

Expected: PASS.

- [ ] Build all packages:

```bash
go build ./...
```

Expected: PASS.

- [ ] Verify formatting:

```bash
test -z "$(gofmt -l .)"
```

Expected: no output.

- [ ] Verify profile migration compatibility with pre-schema-14 fixtures.

Expected: old profiles load and behave exactly as before with Services uncaptured.

- [ ] Verify `restore --dry-run --json` contains deliberate Services compatibility state whenever Services applies.

Expected: no empty/implicit Supported compatibility.

- [ ] Verify approved-plan stability includes:

```text
definition content
drop-ins
ownership/provenance
external-base availability
dependency classification
compatibility
enablement/mask conflict state
activation eligibility
```

- [ ] Verify no Services code path automatically invokes:

```text
sudo
loginctl enable-linger
systemctl --system
systemctl --user stop
systemctl --user restart
```

- [ ] Verify Exact cannot delete an unrelated user service.

- [ ] Verify Force cannot replace an externally owned base unit.

- [ ] Verify unattended Restore cannot activate without explicit activation authority.

- [ ] Verify TUI does not shell out to the Blueprint CLI.

- [ ] Run required Reconstruction Assurance on the final exact head.

Expected: PASS.

---

## Final review checklist

A final reviewer should explicitly inspect these architectural invariants rather than only reading green tests:

### Ownership

- Does any path/location heuristic accidentally become automatic ownership?
- Can an app-created unit under a user path be swept into desired state without reviewed Capture?
- Can managing a drop-in accidentally copy or delete its external base?

### Exact

- Is every Services removal traceable to an explicit exact managed absence?
- Can Exact walk a systemd directory and remove unknown units?
- Does Unknown compatibility ever leave destructive authority intact when the missing fact is required for that authority?

### Force

- Is Force limited to conflict resolution?
- Can Force bypass external ownership, secret handling, validation, compatibility, Exact rules, or activation?

### Activation

- Is active-at-capture still evidence rather than convergence state?
- Can headless Restore start a process merely because `ObservedActive=true`?
- Can source inactivity cause a destination stop?
- Can changed running units be restarted implicitly?
- Do timers/sockets/paths activate their own entry point rather than flattening to their service?

### Approval stability

- Does every material service effect appear in the approved plan?
- Does recalculation reject approval if provenance/dependencies/compatibility/activation authority changed?

### Verification

- Does Verify inspect only the intent Restore actually promised?
- Can persistent-only Restore pass without a running unit?
- Does activation Verify handle non-long-lived unit semantics?
- Are unrelated systemd units ignored?

### UX

- Can a non-systemd expert understand why Blueprint recommends/protects/skips a unit?
- Are raw terms like `masked`, `static`, `indirect`, and `linked` secondary rather than the sole explanation?
- Does CLI/TUI tell the user when restored persistent state will not immediately affect an already-running process?

---

## Recommended worker-agent handoff

Use a fresh implementation worktree based on latest `main` immediately before Task 1.

Because the tasks define interfaces consumed by later tasks, execution should remain sequential at the task/PR level:

```text
Tasks 1–2
↓ review
Tasks 3–5
↓ review
Tasks 6–7
↓ review
Tasks 8–10
↓ review
Task 11
↓ whole-branch review
```

Within a task, independent test-fixture work may run in parallel when it does not change shared interfaces.

Do not parallelize separate workers implementing:

```text
profile Services schema
provider target identity
Capture selection model
Restore activation options
```

before their preceding interface task is merged/accepted; those are shared contracts and parallel invention is likely to create avoidable reconciliation work.

## Completion definition

Services v1 is ready to merge as a completed roadmap tranche only when:

- schema-14 profiles persist Services human-readably;
- existing profiles continue loading;
- Services is discoverable without silent ownership;
- first adoption is reviewed;
- dependency recommendations are selectable;
- definitions/drop-ins/masks/templates/instances reconstruct safely;
- external bases remain external;
- Additive/Exact and Safe/Force retain existing meanings;
- Migration Safety compatibility is first-class in the Services plan;
- approved-plan equality protects Services mutation authority;
- optional activation is explicit and independently authorized;
- unattended mutation cannot implicitly start user programs;
- Verify matches planned intent;
- CLI/TUI parity is demonstrated;
- required real-Omarchy Reconstruction Assurance passes on the exact final head.
