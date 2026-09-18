# Machine-Aware Capture and Restore Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add machine-aware Capture and Restore policy across Blueprint categories while preserving the simple default Capture → Restore workflow, provider-owned safety rules, and inspect → calculate → preview → approve → apply → verify.

**Architecture:** Introduce a small shared `internal/policy` domain that resolves sparse profile and machine overrides for provider-defined target keys. Workflow owns policy resolution and passes immutable Capture/Restore decisions to provider adapters; providers continue to own domain state, desired-absence representation, safety, reconstruction, Exact-removal eligibility, and verification. Persist portable policy separately from per-machine overrides, then expose the completed engine through consistent TUI State/Capture/Restore views and policy-aware Capture/Restore previews.

**Tech Stack:** Go 1.25+/1.26 CI matrix, Bubble Tea v2, Lip Gloss v2, pelletier/go-toml/v2, existing Blueprint profile/workflow/provider packages, Omarchy CLI helpers, Git-backed human-readable profiles.

**Spec:** `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`

## Global Constraints

- Implementation base is `main` after merged PR #28: `575f5ed4d25732b979e4be10c89ed83e07900d33`.
- Preserve the product model: desired state, Capture policy, Restore policy, and restore intent are separate concepts.
- Built-in defaults remain Capture Enabled, Restore Enabled, conflict handling Safe, convergence Additive unless provider safety/ownership excludes the target.
- Capture Disabled means preserve the currently saved desired state; it never means delete or stop managing.
- Restore Skip only narrows authority; Safe, Force, Additive, and Exact never override Restore Skip.
- Exact may remove only explicit desired-absence or authoritative Omarchy-managed intent that the provider can safely own; never infer deletion from arbitrary profile absence.
- Resources must never be deleted by Exact.
- Shell has whole-category Capture/Restore policy only and no Exact cleanup.
- Generic systemd capture/restore is out of scope.
- Do not capture credentials, authentication tokens, or Tailscale/tailnet identity.
- Config keeps its existing management policy and exact user-facing copy:
  - Auto — `Blueprint decides based on Omarchy defaults, ownership, and safety rules.`
  - Included — `You asked Config to manage this path when it is safe to do so.`
  - Excluded — `You asked Config to leave this path alone.`
  - Details must include `Included never overrides safety rules.`
- TUI screens never parse or edit policy/profile TOML directly; all mutations go through workflow/domain APIs.
- Plan and Verify must use the same effective Restore intent.
- Actual Capture and Restore must recalculate immediately before mutation; preview-time results are never write authority.
- Preserve the existing transactional Capture lifecycle: stage/install, combined profile save, commit/finalize, rollback on failure.
- User-facing terminology remains category, installed theme, Installed plugins, Omarchy default, and differences.
- Safety-critical TUI state must remain understandable at approximately 80 columns without depending on the Details pane.
- Every task follows TDD: write a focused failing test, run it and confirm the intended failure, implement the minimum behavior, rerun focused tests, then run the relevant package suite before committing.
- Do not broaden scope into navigation redesign, horizontal scrolling, generic service management, or unrelated refactors.

---

## File Structure and Ownership Map

The implementation should converge on these responsibilities:

- `internal/policy/types.go` — pure policy-domain enums and value types; no profile/workflow/provider imports.
- `internal/policy/resolve.go` — inheritance resolution and source reporting.
- `internal/policy/resolve_test.go` — exhaustive precedence/hierarchy tests.
- `internal/workflow/targets.go` — provider-neutral target inspection, capabilities, Capture/Restore contexts, inspection outcomes.
- `internal/workflow/policy.go` — workflow policy resolution/mutation APIs over loaded profile data.
- `internal/workflow/capture_inspection.go` — read-only Capture inspection and proposed outcomes.
- `internal/workflow/providers.go` — shared provider contracts only; keep provider-specific semantics out.
- `internal/workflow/capture.go` — orchestration/transaction only.
- `internal/workflow/restore.go` — restore option resolution, plan/apply/replan/verify orchestration.
- `internal/profile/policy.go` — policy/machine persistence, canonical sorting/validation, schema migration helpers.
- `internal/profile/profile.go` — core profile structs/schema and existing provider state; avoid adding resolver logic here.
- `internal/app/providers.go` — provider adapter construction and typed-domain adaptation. If target-aware methods make this file materially harder to review, move only the new target/merge helpers into `internal/app/provider_targets.go`; do not perform a broad provider refactor.
- `internal/app/restore_plan.go` — cross-provider Shell/plugin restore dependency finalization; consume restore conflict mode from the new context.
- `internal/providers/<category>/...` — provider-specific target identity, desired-absence merge, reconstruction, removal safety, and verification.
- `internal/tui/screens/<category>.go` — presentation of State/Capture/Restore views only.
- `internal/tui/components/` — reusable policy-tab/scope/status presentation primitives when at least two screens need the same interaction.
- `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md` — approved design.
- `docs/planning/plans/2026-09-18-machine-aware-capture-restore-policy-implementation-plan.md` — this plan.


## Execution Preflight

The GitHub integration used to author these documents could read the repository but could not create a branch. Before Task 1 in PR 1, the implementation worker must place the two approved files into the repository unchanged:

```text
docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md
docs/planning/plans/2026-09-18-machine-aware-capture-restore-policy-implementation-plan.md
```

Commit those planning files on the PR 1 branch as the first documentation-only commit:

```bash
git add docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md \
        docs/planning/plans/2026-09-18-machine-aware-capture-restore-policy-implementation-plan.md
git commit -m "docs: add machine-aware policy architecture plan"
```

Do not modify the approved semantics while copying the documents. If implementation uncovers a design contradiction, update the spec explicitly in a reviewed commit before changing behavior.

## Pull Request Topology

Implement and merge in this order. Start each PR from the latest merged `main`, not from the original base SHA.

1. `refactor: introduce target policy domain`
2. `refactor: make provider workflows target-aware`
3. `feat: add portable and machine capture policy`
4. `feat: add machine-aware restore policy`
5. `feat: add safe exact convergence`
6. `feat: restore Omarchy application intent`
7. `feat: expose machine policy controls in TUI`
8. `feat: add policy-aware capture review`

Do not begin PR N+1 until PR N is reviewed, CI-green, and merged. The plan deliberately puts destructive Exact behavior after target identity, policy resolution, restore planning, and verification alignment exist.

---

# PR 1 — `refactor: introduce target policy domain`

**PR goal:** Add pure policy types, restore-intent axes, inheritance resolution, hierarchy support, and workflow target value types without changing persistence or behavior.

**Must remain unchanged:** profile schema, serialized files, provider signatures, Capture/Restore behavior, CLI behavior, TUI behavior.

### Task 1: Add the pure policy value types

**Files:**
- Create: `internal/policy/types.go`
- Create: `internal/policy/types_test.go`

**Interfaces:**
- Produces:
  - `type Setting string`
  - `const SettingEnabled`, `SettingDisabled`
  - `type Axis string`
  - `const AxisCapture`, `AxisRestore`
  - `type Rule struct { Category, Target string; Setting Setting }`
  - `type Rules struct { Capture, Restore []Rule }`
  - `type SourceKind string`
  - `type Source struct { Kind SourceKind; Machine, Category, Target string }`
  - `type EffectiveSetting struct { Enabled bool; Source Source; Explicit bool }`
  - `type Effective struct { Capture, Restore EffectiveSetting }`
  - `type ConflictMode string` with `ConflictSafe`, `ConflictForce`
  - `type ConvergenceMode string` with `ConvergenceAdditive`, `ConvergenceExact`
  - `type RestoreOptions struct { Conflicts ConflictMode; Convergence ConvergenceMode }`
  - validation helpers `ValidateSetting`, `ValidateRestoreOptions`

- [ ] **Step 1: Write failing enum/default validation tests**

```go
func TestValidateRestoreOptions(t *testing.T) {
    valid := []policy.RestoreOptions{
        {Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceAdditive},
        {Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceAdditive},
        {Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceExact},
        {Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceExact},
    }
    for _, options := range valid {
        if err := policy.ValidateRestoreOptions(options); err != nil {
            t.Fatalf("valid options %+v: %v", options, err)
        }
    }
    if err := policy.ValidateRestoreOptions(policy.RestoreOptions{
        Conflicts: "unsafe", Convergence: policy.ConvergenceAdditive,
    }); err == nil {
        t.Fatal("expected invalid conflict mode")
    }
}
```

- [ ] **Step 2: Run the focused tests and confirm they fail because `internal/policy` does not exist**

```bash
go test ./internal/policy -run 'TestValidate' -count=1
```

Expected: package/type-not-found compile failure.

- [ ] **Step 3: Implement the types and validation**

```go
package policy

import "fmt"

type Setting string

const (
    SettingEnabled  Setting = "enabled"
    SettingDisabled Setting = "disabled"
)

type Axis string

const (
    AxisCapture Axis = "capture"
    AxisRestore Axis = "restore"
)

type Rule struct {
    Category string  `toml:"category" json:"category"`
    Target   string  `toml:"target,omitempty" json:"target,omitempty"`
    Setting  Setting `toml:"setting" json:"setting"`
}

type Rules struct {
    Capture []Rule `toml:"capture,omitempty" json:"capture,omitempty"`
    Restore []Rule `toml:"restore,omitempty" json:"restore,omitempty"`
}

type ConflictMode string

const (
    ConflictSafe  ConflictMode = "safe"
    ConflictForce ConflictMode = "force"
)

type ConvergenceMode string

const (
    ConvergenceAdditive ConvergenceMode = "additive"
    ConvergenceExact    ConvergenceMode = "exact"
)

type RestoreOptions struct {
    Conflicts   ConflictMode
    Convergence ConvergenceMode
}

func DefaultRestoreOptions() RestoreOptions {
    return RestoreOptions{Conflicts: ConflictSafe, Convergence: ConvergenceAdditive}
}
```

Implement validation with explicit accepted values and descriptive errors; empty persisted machine restore fields are normalized later by profile/workflow, not accepted here as a resolved option.

- [ ] **Step 4: Run package tests**

```bash
go test ./internal/policy -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/policy
git commit -m "refactor: add policy domain types"
```

### Task 2: Implement inheritance resolution and source reporting

**Files:**
- Create: `internal/policy/resolve.go`
- Create: `internal/policy/resolve_test.go`

**Interfaces:**
- Consumes: `policy.Rules`, `policy.Setting`
- Produces:

```go
type ResolveRequest struct {
    Axis           Axis
    Machine        string
    Category       string
    Target         string
    Ancestors      []string // nearest parent first
    MachineRules   Rules
    ProfileRules   Rules
    DefaultEnabled bool
}

func Resolve(request ResolveRequest) (EffectiveSetting, error)
```

**Required precedence:**

1. machine target
2. nearest matching machine ancestor
3. machine category
4. profile target
5. nearest matching profile ancestor
6. profile category
7. provider default

A category rule is represented by `Rule{Category: "...", Target: ""}`. A target rule requires a non-empty target.

- [ ] **Step 1: Write table-driven precedence tests**

Include machine target vs profile target, machine category vs profile target, nearest Config ancestor, profile target vs profile category, profile ancestor ordering, provider default fallback, Capture/Restore independence, and duplicate-rule rejection.

- [ ] **Step 2: Run tests and confirm failure**

```bash
go test ./internal/policy -run 'TestResolve' -count=1
```

Expected: undefined `Resolve` / missing source types.

- [ ] **Step 3: Implement source kinds and resolver**

```go
const (
    SourceMachineTarget   SourceKind = "machine-target"
    SourceMachineAncestor SourceKind = "machine-ancestor"
    SourceMachineCategory SourceKind = "machine-category"
    SourceProfileTarget   SourceKind = "profile-target"
    SourceProfileAncestor SourceKind = "profile-ancestor"
    SourceProfileCategory SourceKind = "profile-category"
    SourceDefault         SourceKind = "default"
)
```

Validate rules before resolution. Return `Explicit=true` for direct target/category rules at the selected scope; return `Explicit=false` for ancestor/default inheritance.

- [ ] **Step 4: Run all policy tests**

```bash
go test ./internal/policy -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/policy
git commit -m "refactor: resolve inherited target policy"
```

### Task 3: Add provider-neutral target and decision types

**Files:**
- Create: `internal/workflow/targets.go`
- Create: `internal/workflow/targets_test.go`

**Interfaces:**

```go
type TargetState string

const (
    TargetUnknown TargetState = "unknown"
    TargetPresent TargetState = "present"
    TargetAbsent  TargetState = "absent"
)

type TargetCapabilities struct {
    SupportsCapture        bool
    SupportsRestore        bool
    SupportsDesiredAbsence bool
    SupportsExactRemoval   bool
    Hierarchical           bool
}

type TargetInspection struct {
    Key             string
    Parent          string
    Label           string
    Desired         TargetState
    Current         TargetState
    CaptureEligible bool
    RestoreEligible bool
    Capabilities    TargetCapabilities
    SafetyReason    string
}

type CaptureDecision struct {
    Capture  bool
    Resolved bool
}

type RestoreDecision struct {
    Restore  bool
    Resolved bool
}

type CaptureContext struct {
    Machine string
    Targets map[string]CaptureDecision
}

type RestoreContext struct {
    Machine string
    Options policy.RestoreOptions
    Targets map[string]RestoreDecision
}
```

Missing keys from helper accessors return enabled + unresolved so PR 2 can preserve behavior before policy persistence activates.

- [ ] **Step 1: Write failing tests for explicit disabled and missing-default-enabled decisions**

- [ ] **Step 2: Run focused tests**

```bash
go test ./internal/workflow -run 'Test.*ContextDecision' -count=1
```

- [ ] **Step 3: Implement target/context types with no provider interface change yet**

- [ ] **Step 4: Run workflow tests**

```bash
go test ./internal/workflow -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/workflow/targets.go internal/workflow/targets_test.go
git commit -m "refactor: add workflow target contexts"
```

### Task 4: PR 1 verification gate

- [ ] **Step 1: Run all tests**

```bash
go test ./... -count=1
```

- [ ] **Step 2: Run static/build checks**

```bash
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 3: Confirm no schema or provider behavior changed**

```bash
git diff main...HEAD -- internal/profile internal/providers internal/tui internal/app
```

- [ ] **Step 4: Open PR 1**

Title:

```text
refactor: introduce target policy domain
```

PR body states: no persistence, schema, CLI, TUI, Capture, or Restore semantic change.

---

# PR 2 — `refactor: make provider workflows target-aware`

**PR goal:** Make providers inspect targets and consume immutable Capture/Restore contexts while feeding all-enabled Safe+Additive defaults so current behavior is preserved.

### Task 5: Extend shared provider contracts

**Files:**
- Modify: `internal/workflow/providers.go`
- Modify: `internal/workflow/capture_test.go`
- Modify: `internal/workflow/restore_test.go`
- Modify workflow test fakes implementing Provider/RestoreProvider.

**Interfaces:**

```go
type Provider interface {
    ID() string
    Captured(profile.Data) bool
    InspectTargets(context.Context, profile.Data) ([]TargetInspection, error)
    Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error)
    Diff(context.Context, profile.Data) ([]model.Change, error)
}

type RestoreProvider interface {
    Provider
    Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (model.RestorePlan, error)
    Verify(context.Context, profile.Data, RestoreContext) (model.VerificationResult, error)
}
```

- [ ] **Step 1: Update workflow test fakes first and retain last received Capture/Restore contexts**

- [ ] **Step 2: Run workflow/app tests and confirm provider compile failures**

```bash
go test ./internal/workflow ./internal/app -count=1
```

- [ ] **Step 3: Update interfaces and orchestration to pass explicit compatibility defaults**

- [ ] **Step 4: Commit contract/fake changes once the branch compiles**

```bash
git add internal/workflow
git commit -m "refactor: make provider contracts target-aware"
```

### Task 6: Add read-only Capture inspection and default-context orchestration

**Files:**
- Create: `internal/workflow/capture_inspection.go`
- Create: `internal/workflow/capture_inspection_test.go`
- Modify: `internal/workflow/capture.go`
- Modify: `internal/workflow/restore.go`

**Interfaces:**

```go
type CaptureOutcome string

const (
    CaptureOutcomeAdd      CaptureOutcome = "add"
    CaptureOutcomeUpdate   CaptureOutcome = "update"
    CaptureOutcomeAbsent   CaptureOutcome = "absent"
    CaptureOutcomePreserve CaptureOutcome = "preserve"
    CaptureOutcomeBlocked  CaptureOutcome = "blocked"
    CaptureOutcomeNoop     CaptureOutcome = "noop"
)

type CaptureTarget struct {
    Category   string
    Inspection TargetInspection
    Policy     policy.EffectiveSetting
    Decision   CaptureDecision
    Outcome    CaptureOutcome
}

type CaptureInspection struct {
    Categories map[string][]CaptureTarget
}

func (s *Session) InspectCapture(ctx context.Context, onlyProvider string) (CaptureInspection, error)
```

In PR 2 every eligible target resolves to in-memory default enabled; blocked targets remain blocked.

- [ ] **Step 1: Write failing fake-provider tests for Add, Blocked, Noop, and no persistence**

- [ ] **Step 2: Implement inspection and compatibility contexts**

- [ ] **Step 3: Change restore verification to receive the same RestoreContext used by planning**

- [ ] **Step 4: Add a regression test proving Plan and Verify see identical options/decisions**

- [ ] **Step 5: Run workflow tests after provider adapter tasks**

### Task 7: Add target inventories for Packages, Config, and Defaults

**Files:**
- Modify: `internal/app/providers.go`
- Optionally create focused helpers: `internal/app/provider_targets.go`
- Test: `internal/app/app_test.go`
- Test provider packages.

**Targets:**
- Packages: `official:<name>`, `aur:<name>`, `mise:<id>`.
- Hardware/machine-specific packages remain protected inspection metadata, not normal portable targets.
- Config: canonical managed paths, with meaningful parent chain.
- Defaults: `terminal`, `browser`, `editor`, `agent`.

- [ ] **Step 1: Write inventory tests for desired/current state and capabilities**

- [ ] **Step 2: Implement `InspectTargets` using existing provider Detect/scan functions**

- [ ] **Step 3: Update Capture/Plan/Verify adapter signatures but preserve existing behavior**

- [ ] **Step 4: Run focused tests**

```bash
go test ./internal/app ./internal/providers/packages ./internal/providers/config ./internal/providers/defaults -count=1
```

### Task 8: Add target inventories for Themes, Plugins, and Hooks

**Files:**
- Modify: `internal/app/providers.go`
- Modify focused helper if created.
- Test theme/plugin/hook provider suites and app tests.

**Targets:**
- Themes: `active`, `theme:<id>` for non-built-in availability.
- Plugins: `plugin:<id>` for third-party source availability.
- Hooks: managed hook path; unmanaged symlink stays non-capturable safety state.

- [ ] **Step 1: Write inventory tests before methods**

- [ ] **Step 2: Implement inventories**

- [ ] **Step 3: Preserve Shell ownership of plugin enablement**

- [ ] **Step 4: Run focused tests**

### Task 9: Add target inventories for Shell and Resources

**Files:**
- Modify: `internal/app/providers.go`
- Test Shell/Resources/app packages.

**Targets/capabilities:**
- Shell: exactly `state`, no desired absence, no Exact removal.
- Resources: `resource:<id>`, no desired absence, no Exact removal.

- [ ] **Step 1: Write capability tests**

- [ ] **Step 2: Implement target inventories and context-accepting adapters**

- [ ] **Step 3: Add Resource test proving missing local state is not `TargetAbsent` deletion intent**

- [ ] **Step 4: Run focused tests**

### Task 10: Update cross-provider restore finalization

**Files:**
- Modify: `internal/app/restore_plan.go`
- Modify: `internal/app/providers.go`
- Test: `internal/app/app_test.go`

- [ ] **Step 1: Write a regression proving Shell required-plugin analysis receives Force only from conflict mode**

- [ ] **Step 2: Change finalizer input to RestoreContext/RestoreOptions**

- [ ] **Step 3: Keep Exact irrelevant to Shell/plugin dependency conflict resolution**

- [ ] **Step 4: Run app tests**

### Task 11: PR 2 behavior-parity gate

- [ ] **Step 1: Run all tests/checks**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 2: Confirm serialized profile fixtures are unchanged**

- [ ] **Step 3: Open PR 2**

Title:

```text
refactor: make provider workflows target-aware
```

State explicitly that inspection is read-only and effective policy remains current default behavior.

---

# PR 3 — `feat: add portable and machine capture policy`

**PR goal:** Perform the v0.4 schema cutover, persist sparse policy, activate Capture Update/Preserve, add provider-owned desired absence, migrate legacy package exclusions, and preserve conservative Safe+Additive restore behavior.

### Task 12: Add profile policy persistence and schema migration

**Files:**
- Create: `internal/profile/policy.go`
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/managed_paths.go`
- Test: `internal/profile/profile_test.go`
- Add a schema-11 migration fixture under `internal/profile/testdata/`.

**Interfaces:**
- Bump `profile.Schema` from `11` to `12` once for this v0.4 cutover.
- Add `Policy policy.Rules` to `profile.Data`.
- Extend `profile.Machine` with restore defaults + `Policy policy.Rules`.
- Persist portable rules at `policy/policy.toml`.
- Persist machine rules/defaults in existing `machines/<name>.toml`.
- Missing policy file means no overrides.
- Empty machine restore fields normalize to Safe/Additive.

- [ ] **Step 1: Write failing round-trip/validation/migration tests**

Cover portable rules, machine rules, deterministic sorting, missing file, invalid setting, duplicate rule, schema-11 load without disk mutation.

- [ ] **Step 2: Implement load/save/validation**

Profile validates serialization; `internal/policy` owns semantic enum validation.

- [ ] **Step 3: Add current-schema pruning of obsolete package exclusion/machine-specific persistence**

- [ ] **Step 4: Run profile tests**

### Task 13: Add workflow policy resolution and mutation APIs

**Files:**
- Create: `internal/workflow/policy.go`
- Create: `internal/workflow/policy_test.go`
- Modify: `internal/workflow/machines.go`
- Modify: `internal/workflow/machines_test.go`

**Interfaces:**

```go
type PolicyScope struct {
    Machine string
}

func (s *Session) EffectivePolicy(ctx context.Context, scope PolicyScope, category string, target TargetInspection) (policy.Effective, error)
func (s *Session) SetPolicy(scope PolicyScope, axis policy.Axis, category, target string, setting policy.Setting) error
func (s *Session) ClearPolicy(scope PolicyScope, axis policy.Axis, category, target string) error
func (s *Session) SetMachineRestoreDefaults(machine string, options policy.RestoreOptions) error
```

`EffectivePolicy` takes `scope` explicitly rather than resolving against whichever machine the session currently has selected: the TUI must be able to inspect Profile defaults and a named machine independently (per the design's "Policy scope" section), and an implicit current-machine resolution would tie every read to session state instead of the scope the caller is actually viewing. An empty `PolicyScope.Machine` means Profile-defaults scope; this must be passed through to `policy.ResolveRequest.Machine` unchanged so `Resolve`'s scope-relative `Explicit` reporting (see `internal/policy/resolve.go`) stays correct for both scopes.

- [ ] **Step 1: Write mutation/resolution tests**

- [ ] **Step 2: Implement provider target validation before target-specific persistence**

- [ ] **Step 3: Use existing machine selection/context rather than hostname inference**

- [ ] **Step 4: Run workflow tests**

### Task 14: Introduce provider-owned desired-absence profile state

**Files:**
- Modify: `internal/profile/profile.go` or create `internal/profile/provider_state.go`
- Test: `internal/profile/profile_test.go`

**Concrete shape:**

```go
type PackageAbsence struct {
    Ref  string   `json:"ref" toml:"ref"`
    Mise MiseTool `json:"mise,omitempty" toml:"mise,omitempty"`
}

type Packages struct {
    Official  []string
    AUR       []string
    Mise      MiseTools
    Absent    []PackageAbsence
    Installed []string
}

type Themes struct {
    Current string
    Source  string
    Items   []Theme
    Absent  []Theme
}

type Plugins struct {
    Items  []Plugin
    Absent []Plugin
}

type Hooks struct {
    Items  []Hook
    Absent []Hook
}
```

Reusing prior Theme/Plugin/Hook metadata in tombstones preserves provenance. Mise absence keeps its prior validated declaration.

Persist package tombstones in a dedicated human-readable `packages/absent.toml` rather than overloading the existing line-oriented package files. Use one `[[package]]` entry per canonical ref and include the prior Mise declaration only for `mise:<id>` entries. Theme/Plugin/Hook tombstones live alongside their existing provider metadata using explicit `absent` tables/arrays; captured artifact trees continue to contain desired-present bytes only.

- [ ] **Step 1: Write round-trip tests**

- [ ] **Step 2: Write validation that the same target cannot be both desired-present and desired-absent**

- [ ] **Step 3: Implement serialization/validation**

- [ ] **Step 4: Run profile tests**

### Task 15: Migrate legacy Package exclusions and hardware state

**Files:**
- Modify: `internal/profile/policy.go`
- Modify: `internal/providers/packages/provider.go`
- Test profile/package migration cases.

**Migration semantics:**
- legacy Excluded ref → portable Capture Disabled + Restore Disabled;
- excluded target has no desired state after migration;
- excluded Mise declaration does not need permanent legacy preservation;
- MachineSpecific is runtime inspection/safety metadata only and is not re-saved.

- [ ] **Step 1: Write schema-11 migration fixture test**

- [ ] **Step 2: Implement migration and save cleanup**

- [ ] **Step 3: Run profile/package tests**

### Task 16: Activate policy-aware Capture resolution in workflow

**Files:**
- Modify: `internal/workflow/capture.go`
- Modify: `internal/workflow/capture_inspection.go`
- Modify related tests.

**Behavior:**
- inspect targets;
- resolve Capture policy;
- blocked safety always wins;
- actual Capture re-inspects before mutation.

- [ ] **Step 1: Write the laptop/desktop fake-provider regression**

Saved X present + desktop X absent + Capture Disabled → saved X remains present.

- [ ] **Step 2: Add inspection Preserve/Blocked tests**

- [ ] **Step 3: Implement context construction**

- [ ] **Step 4: Run workflow tests after provider merges land**

### Task 17: Implement Packages Capture merge and absence transitions

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/mise.go`
- Modify: `internal/app/providers.go`
- Test package/Mise/app suites.

**Transitions:**
- unknown + present + enabled → present;
- previous present + absent + enabled → tombstone;
- previous absent + present + enabled → present;
- disabled → preserve previous present/absent;
- unknown + present + disabled → unmanaged;
- hardware protected → not portable by default.

- [ ] **Step 1: Write table-driven official/AUR/Mise merge tests**

- [ ] **Step 2: Add changed-local-Mise + Capture Disabled preservation test**

- [ ] **Step 3: Implement typed merge helper; do not translate Disabled to legacy Excluded**

- [ ] **Step 4: Run focused tests**

### Task 18: Implement Themes and Plugins Capture merge/absence

**Files:**
- Modify theme provider capture files.
- Modify: `internal/providers/plugins/provider.go`
- Modify: `internal/app/providers.go`
- Test provider/app suites.

**Rules:**
- built-ins never become removable tombstones;
- active theme independent from theme availability;
- plugin enablement remains Shell-owned;
- local artifact staging merges preserved disabled targets rather than wholesale replacing them.

- [ ] **Step 1: Write disabled local artifact preservation tests**

- [ ] **Step 2: Write absent transition tests**

- [ ] **Step 3: Implement merged staged generations**

- [ ] **Step 4: Run focused tests**

### Task 19: Implement Config hierarchical Capture Preserve

**Files:**
- Modify: `internal/providers/config/capture.go`
- Modify: `internal/app/providers.go`
- Test Config/app suites.

- [ ] **Step 1: Write directory disabled + child enabled exception test**

- [ ] **Step 2: Write regression: Config Excluded prunes, generic Capture Disabled preserves**

- [ ] **Step 3: Implement generic policy merge after safe candidate classification**

- [ ] **Step 4: Run tests**

### Task 20: Implement Hooks provenance tombstones

**Files:**
- Modify: `internal/providers/hooks/provider.go`
- Modify: `internal/app/providers.go`
- Test hook suites.

- [ ] **Step 1: Write present→absent, absent→present, disabled-preserve, unmanaged-symlink tests**

- [ ] **Step 2: Implement merge/staged snapshot cleanup**

- [ ] **Step 3: Run tests**

### Task 21: Implement Defaults, Shell, and Resources Capture semantics

**Files:**
- Modify adapters/provider helpers for Defaults/Shell/Resources.
- Test each provider and app layer.

**Rules:**
- Defaults slot disabled preserves previous slot; enabled + current empty stops managing slot.
- Shell disabled preserves whole Shell desired state/artifact.
- Resources disabled preserves prior generation; missing local Resource preserves prior state and never creates desired absence.

- [ ] **Step 1: Write one preservation test per provider**

- [ ] **Step 2: Implement minimal typed merge**

- [ ] **Step 3: Add explicit Resource missing no-tombstone test**

- [ ] **Step 4: Run focused tests**

### Task 22: Add Stop Managing semantics

**Files:**
- Modify: `internal/workflow/policy.go`
- Modify provider-specific cleanup helpers as needed.
- Test workflow/provider artifact cleanup.

**Interface:**

```go
func (s *Session) StopManaging(ctx context.Context, category, target string) error
```

Semantics:
- Packages/Themes/Plugins/Hooks: remove desired present/absent state, sole target artifact, and target-specific profile/machine overrides.
- Config: reject generic action and retain Config's own policy controls.
- Resources: reject and direct to Untrack.
- Defaults: provider-specific slot clearing.
- Shell: reject unless an existing safe clear-management operation already exists.

- [ ] **Step 1: Write package/plugin cleanup tests**

- [ ] **Step 2: Write Config/Resources rejection tests**

- [ ] **Step 3: Implement cleanup through workflow while preserving transactional artifact handling**

- [ ] **Step 4: Run tests**

### Task 23: PR 3 end-to-end gate

- [ ] **Step 1: Add workflow scenarios for preserve, desired absence, new-disabled, Config hierarchy, missing Resource, and safety blocking**

- [ ] **Step 2: Prove new non-Config tombstones are non-destructive under Safe+Additive before PR 4**

- [ ] **Step 3: Run full checks**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 4: Open PR 3**

Title:

```text
feat: add portable and machine capture policy
```

---

# PR 4 — `feat: add machine-aware restore policy`

**PR goal:** Activate Restore Apply/Skip, persisted machine defaults, one-run overrides, independent Safe/Force + Additive/Exact options, explicit policy skips, and policy-aware verification without broad new destructive Exact behavior.

### Task 24: Replace restore mode internals with two-axis RestoreOptions

**Files:**
- Modify: `internal/workflow/restore.go`
- Modify: `internal/workflow/restore_test.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/restore_plan.go`

**Interfaces:**

```go
func (s *Session) PlanRestore(ctx context.Context, onlyProvider string, options *policy.RestoreOptions) (model.RestorePlan, error)
func (s *Session) ApplyRestore(ctx context.Context, onlyProvider string, options *policy.RestoreOptions) (RestoreResult, error)
```

`nil` means selected machine defaults, falling back Safe+Additive. `RestoreResult` stores resolved RestoreOptions.

- [ ] **Step 1: Write default-resolution and four-combination tests**

- [ ] **Step 2: Implement option resolution and change provider Force booleans to conflict-mode checks**

- [ ] **Step 3: Keep Exact independent from Force**

- [ ] **Step 4: Keep a temporary legacy `CompareRestore` adapter for the PR-28 TUI, comparing Safe+Additive vs Force+Additive only**

```go
func (s *Session) CompareRestore(ctx context.Context, onlyProvider string) (RestoreComparison, error) {
    safe := policy.RestoreOptions{Conflicts: policy.ConflictSafe, Convergence: policy.ConvergenceAdditive}
    force := policy.RestoreOptions{Conflicts: policy.ConflictForce, Convergence: policy.ConvergenceAdditive}
    // Build the existing comparison shape from those two plans.
}
```

PR 7 removes this adapter once the TUI is redesigned.

- [ ] **Step 5: Run workflow/app tests**

### Task 25: Resolve Restore policy into contexts and explicit skips

**Files:**
- Modify: `internal/workflow/restore.go`
- Modify: `internal/workflow/policy.go`
- Modify restore tests.

- [ ] **Step 1: Write Restore Skip matrix under all four restore option combinations**

- [ ] **Step 2: Implement policy resolution before provider planning**

- [ ] **Step 3: Standardize visible policy skip reason**

Example:

```text
restore disabled for machine "desktop"
```

- [ ] **Step 4: Run workflow tests**

### Task 26: Make every provider Plan and Verify honor RestoreContext

**Files:**
- Modify provider adapters in `internal/app/providers.go`.
- Modify provider Plan/Verify functions for all eight categories.
- Test corresponding provider suites.

**Invariant:** Restore Skip produces no operation and no verification failure.

**PR 4 Exact behavior:** pass the mode through, but do not yet add new theme/plugin/hook/package removals; existing Config deletes remain existing semantics.

- [ ] **Step 1: Add one Plan+Verify Skip test per provider**

- [ ] **Step 2: Implement filtering before operations**

- [ ] **Step 3: Ensure Additive desired-absent generic package does not require absence in Verify**

- [ ] **Step 4: Run focused tests**

### Task 27: Add CLI one-run restore overrides

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**CLI:**
- `--force` → Force conflict shorthand;
- `--exact` → Exact convergence shorthand;
- `--conflicts safe|force`;
- `--convergence additive|exact`;
- explicit flags override machine defaults for one run;
- contradictory shorthand/explicit values error.

- [ ] **Step 1: Write parsing tests**

- [ ] **Step 2: Implement parsing and pass optional RestoreOptions**

- [ ] **Step 3: Run app tests**

### Task 28: PR 4 restore invariants gate

- [ ] **Step 1: Add scenarios: Skip beats Force+Exact; Shell only responds to conflict axis; Additive keeps generic desired-absent package; one-run override does not persist; Plan=Verify intent**

- [ ] **Step 2: Run full checks**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 3: Open PR 4**

Title:

```text
feat: add machine-aware restore policy
```

---

# PR 5 — `feat: add safe exact convergence`

**PR goal:** Activate provider-owned Exact removal for Themes, Plugins, and Hooks; integrate Config explicit deletion semantics; prove Resources and Shell remain non-destructive.

### Task 29: Add safe Exact removal for user themes

**Files:**
- Modify theme planning file(s).
- Modify: `internal/app/providers.go`
- Test theme plan/provider suites.

**Rules:**
- only tombstoned user-managed themes;
- Restore enabled + Exact;
- built-ins never removed;
- active theme requires safe replacement first or skip;
- use Omarchy native theme removal;
- Force does not broaden scope.

- [ ] **Step 1: Write negative tests first**

Built-in, Restore Skip, Additive, active-without-replacement, changed/unowned local theme.

- [ ] **Step 2: Write positive Exact-removal test**

- [ ] **Step 3: Implement dependency ordering after active theme set**

- [ ] **Step 4: Run theme tests**

### Task 30: Add safe Exact removal for third-party plugins

**Files:**
- Modify: `internal/providers/plugins/provider.go`
- Test plugin/app suites.

**Rules:**
- only tombstoned third-party managed plugin;
- first-party never removed;
- use `omarchy plugin remove <id> --yes`;
- provenance mismatch skips;
- Restore Skip/Additive suppress removal;
- Shell dependency finalization cannot reintroduce skipped plugin.

- [ ] **Step 1: Write negative provenance/policy tests**

- [ ] **Step 2: Write positive Exact test**

- [ ] **Step 3: Implement removal planning/verification**

- [ ] **Step 4: Run tests**

### Task 31: Add provenance-matched Exact deletion for Hooks

**Files:**
- Modify: `internal/providers/hooks/plan.go`
- Modify verify helper if separate.
- Test: `internal/providers/hooks/plan_test.go`

**Rules:**
- only Hooks.Absent;
- current hash/mode must match tombstone;
- already missing satisfied;
- changed file skips;
- unmanaged symlink never deleted;
- Additive never deletes.

- [ ] **Step 1: Write match/mismatch/missing/symlink/Skip/Additive tests**

- [ ] **Step 2: Implement delete operation with existing restore filesystem preconditions**

- [ ] **Step 3: Update verification expectations for actionable Exact only**

- [ ] **Step 4: Run hook tests**

### Task 32: Lock no-delete guarantees for Resources and Shell; integrate Config

**Files:**
- Test: `internal/providers/resources/plan_test.go`
- Test: `internal/providers/shell/plan_test.go`
- Test Config plan suite.
- Modify Config only if context integration requires it.

- [ ] **Step 1: Add Safe+Exact and Force+Exact Resource tests asserting zero delete operations**

- [ ] **Step 2: Add Shell Exact tests asserting identical scope to Additive for same conflict mode**

- [ ] **Step 3: Add Config test proving only explicit ConfigDelete can delete; profile absence never sweeps `.config`**

- [ ] **Step 4: Run tests**

### Task 33: PR 5 destructive-safety gate

- [ ] **Step 1: Add aggregate workflow test with theme/plugin/hook tombstones plus Resource extra data**

- [ ] **Step 2: Inspect every new delete/remove path for ownership/provenance/native remover**

- [ ] **Step 3: Run full checks**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 4: Open PR 5**

Title:

```text
feat: add safe exact convergence
```

---

# PR 6 — `feat: restore Omarchy application intent`

**PR goal:** Add authoritative Omarchy preinstall intent, semantic application recipes, and safe Exact package removal without generic service capture.

### Task 34: Add Omarchy preinstall catalogue and intent detection

**Files:**
- Create: `internal/omarchy/preinstalls.go`
- Create: `internal/omarchy/preinstalls_test.go`
- Modify package adapter/provider inventory.
- Test packages/app.

**Interface:**

```go
type PreinstallState struct {
    RemovedAll bool
    Items      map[string]bool
}

func DetectPreinstalls(ctx context.Context, runner command.Runner) (PreinstallState, error)
```

Detection reads authoritative installed Omarchy catalogue and `~/.local/state/omarchy/preinstalls-removed`; do not hard-code a permanent Blueprint catalogue.

- [ ] **Step 1: Write fixture tests for marker, catalogue parsing, and item presence**

- [ ] **Step 2: Implement detection in `internal/omarchy`**

- [ ] **Step 3: Expose package subgroup keys `preinstalls` and `preinstall:<id>`**

- [ ] **Step 4: Run tests**

### Task 35: Capture and restore Omarchy preinstall intent

**Files:**
- Modify profile package state as required.
- Modify package Capture/Plan/Verify.
- Test profile/packages/app.

**Semantics:**
- portable group opt-out + individual known preinstall state;
- supported preinstall absence may apply during ordinary Additive restore;
- group-level uses Omarchy native install/remove preinstall commands where appropriate;
- individual behavior uses authoritative Omarchy operations;
- unrelated extras remain untouched.

- [ ] **Step 1: Write remove-all and reinstalled-one scenarios**

- [ ] **Step 2: Implement capture representation/native plan**

- [ ] **Step 3: Implement verification**

- [ ] **Step 4: Run tests**

### Task 36: Add semantic application recipe selection

**Files:**
- Create: `internal/omarchy/apps.go`
- Create: `internal/omarchy/apps_test.go`
- Modify package planning.

**Interface:**

```go
type AppRecipe struct {
    ID          string
    Install     []string
    Remove      []string
    Interactive bool
}

func SemanticRecipe(id string) (AppRecipe, bool)
```

Use a small adapter around authoritative Omarchy semantic commands; first acceptance case is Tailscale. Do not create generic systemd/service capture.

- [ ] **Step 1: Write Tailscale semantic-vs-raw test**

- [ ] **Step 2: Write ordinary package fallback test**

- [ ] **Step 3: Implement recipe selection**

- [ ] **Step 4: Surface interactive/auth requirements without storing credentials**

- [ ] **Step 5: Run tests**

### Task 37: Activate safe Exact package/Mise removal

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/mise.go`
- Modify app adapter.
- Test package/Mise/app suites.

**Rules:**
- explicit Packages.Absent only;
- Restore enabled + Exact except special preinstall semantics;
- hardware protected never removable;
- dependencies are not removed by profile absence;
- official/AUR use Omarchy package removal;
- semantic app uses semantic remover;
- Mise current declaration must match tombstone before removal.

- [ ] **Step 1: Write protected/Skip/Additive/changed-Mise/unknown-extra negative tests**

- [ ] **Step 2: Write positive official/AUR/Mise/semantic-app Exact tests**

- [ ] **Step 3: Implement operations with explicit risk/preconditions**

- [ ] **Step 4: Verify absence only for actionable Exact tombstones**

- [ ] **Step 5: Run focused tests**

### Task 38: Real-Omarchy behavior checkpoint

- [ ] **Step 1: Record Blueprint/Omarchy versions and selected machine**

- [ ] **Step 2: Exercise package discovery + preinstall remove/reinstall**

- [ ] **Step 3: Exercise semantic installer such as Tailscale where practical; do not capture auth**

- [ ] **Step 4: Exercise theme/plugin Exact removal + hook provenance**

- [ ] **Step 5: Exercise Safe/Force and Additive/Exact**

- [ ] **Step 6: Confirm Resource directories untouched under Exact**

- [ ] **Step 7: Run full checks and open PR 6**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Title:

```text
feat: restore Omarchy application intent
```

Do not proceed to PR 7 until this checkpoint is satisfactory.

---

# PR 7 — `feat: expose machine policy controls in TUI`

**PR goal:** Expose the completed policy engine consistently while keeping State as default and preserving the simple path.

### Task 39: Add reusable policy tab/scope/status presentation

**Files:**
- Create focused shared component(s) under `internal/tui/components/` only if reused.
- Modify shared screen helpers as needed.
- Add component/screen tests.

**UI:**
- `State | Capture | Restore`;
- State default;
- explicit scope: Profile defaults or Machine;
- inherited quieter and text/symbol-coded;
- reset removes override;
- Capture Include/Ignore;
- Restore Apply/Skip;
- Blocked distinct from intentional Skip/Ignore.

- [ ] **Step 1: Write tab/scope/toggle/reset/80-column tests**

- [ ] **Step 2: Implement reusable presentation with workflow callbacks**

- [ ] **Step 3: Ensure no profile serialization imports in components**

- [ ] **Step 4: Run TUI component tests**

### Task 40: Add policy tabs to Packages, Themes, Plugins, Defaults, Shell, Hooks, Resources

**Files:**
- Modify corresponding `internal/tui/screens/*.go`
- Modify screen tests.

**Presentation requirements:**
- Packages groups Official/AUR/Mise/Omarchy preinstalls and shows desired-absent.
- Themes separates Active and Available.
- Plugins explains Shell-owned enablement.
- Defaults shows four slots and Agent unavailable when unsafe.
- Shell one whole-state target.
- Hooks shows path/tombstone.
- Resources states Exact never deletes Resource data.

- [ ] **Step 1: Add one screen at a time with failing State/Capture/Restore tests**

- [ ] **Step 2: Wire mutations only via workflow SetPolicy/ClearPolicy**

- [ ] **Step 3: Add Details for desired/current/effective/source/capabilities**

- [ ] **Step 4: Run screen tests after each category**

### Task 41: Add hierarchical Config policy UI

**Files:**
- Modify: `internal/tui/screens/config.go`
- Modify: `internal/tui/screens/config_test.go`

- [ ] **Step 1: Write collapsed/expanded/child-override/nearest-parent tests**

- [ ] **Step 2: Write exact copy regression tests for Config management policy**

- [ ] **Step 3: Implement tree projection from workflow targets**

- [ ] **Step 4: Run Config tests at 140/100/80 widths**

### Task 42: Add machine restore defaults and override summary

**Files:**
- Modify: `internal/tui/screens/machines.go`
- Modify: `internal/tui/screens/machines_test.go`

**Machines owns:**
- Resource mappings;
- Conflicts Safe/Force;
- Convergence Additive/Exact;
- override counts + navigation, not duplicate editors.

- [ ] **Step 1: Write mutation completion/default-edit tests**

- [ ] **Step 2: Write override count/navigation tests**

- [ ] **Step 3: Implement workflow calls**

- [ ] **Step 4: Run Machines/root event tests; preserve PR #27 origin delivery**

### Task 43: Redesign Restore screen around current run settings

**Files:**
- Modify: `internal/tui/screens/restore.go`
- Modify: `internal/tui/screens/restore_test.go`
- Modify `internal/tui/catalog.go` where current Restore help is now wrong.
- Remove temporary legacy workflow comparison adapter after callers are gone.

**UI:**
- selected machine;
- Conflicts Safe/Force;
- Convergence Additive/Exact;
- one-run override indication;
- one current plan that replans when toggles change;
- no four-column combination matrix;
- Exact persistent warning;
- approval counts removals/policy skips/forced overrides.

- [ ] **Step 1: Write machine-default + temporary override tests**

- [ ] **Step 2: Write 80-column Exact/removal visibility tests**

- [ ] **Step 3: Implement controls/single-plan projection**

- [ ] **Step 4: Remove legacy CompareRestore/RestoreComparison when unused**

- [ ] **Step 5: Run workflow/TUI tests**

### Task 44: PR 7 responsive/safety gate

- [ ] **Step 1: Run TUI/full checks**

```bash
go test ./internal/tui/... -count=1
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 2: Verify 80-column safety information is not sidebar-only**

- [ ] **Step 3: Open PR 7**

Title:

```text
feat: expose machine policy controls in TUI
```

---

# PR 8 — `feat: add policy-aware capture review`

**PR goal:** Complete first-capture candidate preview, policy-adjust-and-recalculate, intentional-difference Overview semantics, and final UX integration.

### Task 45: Enrich Capture inspection with stable review grouping

**Files:**
- Modify: `internal/workflow/capture_inspection.go`
- Modify: `internal/workflow/capture_inspection_test.go`

**Groups:** Changes, Preserved by policy, Blocked, No action needed.

**Outcomes:** Add, Update, Remember absent, Preserve, Blocked, Noop.

- [ ] **Step 1: Write state/policy outcome table tests**

- [ ] **Step 2: Implement deterministic grouping/sorting**

- [ ] **Step 3: Add re-inspection test after policy mutation**

- [ ] **Step 4: Run workflow tests**

### Task 46: Add Capture review flow to TUI

**Files:**
- Modify: `internal/tui/screens/capture.go`
- Modify: `internal/tui/screens/capture_test.go`
- Modify existing modal/event helpers only where required.

**Flow:** choose categories → inspect → review → optional policy change → recalculate → approve → command re-inspects → transactional Capture.

- [ ] **Step 1: Write fresh-profile candidate preview test**

- [ ] **Step 2: Write Add→Ignore→Preserve recalculation test**

- [ ] **Step 3: Write blocked safety cannot be overridden test**

- [ ] **Step 4: Implement review state without treating cached preview as write authority**

- [ ] **Step 5: Run Capture tests**

### Task 47: Make Overview distinguish actionable vs intentional differences

**Files:**
- Modify: `internal/workflow/overview.go`
- Modify: `internal/workflow/overview_test.go`
- Modify: `internal/tui/screens/overview.go`
- Modify: `internal/tui/screens/overview_test.go`

**Domain:** factual Difference, Capture candidate, Restore candidate, Intentional difference.

**Presentation:** Needs attention, Changes available, Intentional differences, No action needed.

- [ ] **Step 1: Write Firefox present-profile/absent-desktop/Ignore+Skip classification test**

- [ ] **Step 2: Implement classification**

- [ ] **Step 3: Add non-warning intentional-difference TUI tests**

- [ ] **Step 4: Run tests**

### Task 48: Finalize help/catalogue/docs copy

**Files:**
- Modify: `internal/tui/catalog.go`
- Modify: `internal/tui/catalog_test.go`
- Update README/user guides only where stale behavior is described.

- [ ] **Step 1: Write catalogue terminology/copy tests**

- [ ] **Step 2: Update Safe/Force + Additive/Exact explanations and remove obsolete Normal/Forced copy**

- [ ] **Step 3: Search stale user-facing terms**

```bash
rg -n 'Normal mode|Forced mode|excluded package|machine-specific items|drift|provider|git theme|Git plugins' README.md docs internal/tui
```

Review hits manually; keep intentional internal identifiers.

- [ ] **Step 4: Run tests and diff check**

### Task 49: Full UX and real-Omarchy acceptance checkpoint

- [ ] **Step 1: Run automated verification**

```bash
go test ./internal/tui/... -count=1
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

- [ ] **Step 2: Fresh profile at ~140 columns: first Capture preview, sensible defaults, no policy setup required**

- [ ] **Step 3: Repeat ~100 and ~80 columns: scope, Exact/Force, removals, skips, approval summary understandable**

- [ ] **Step 4: Too-small condition: graceful constrained rendering, no panic/overflow**

- [ ] **Step 5: Machine-specific Capture/Restore: laptop contributes X, desktop preserves/skips X, Overview intentional**

- [ ] **Step 6: One-run override: verify persisted machine default unchanged**

- [ ] **Step 7: Config hierarchy + Theme active/available split**

- [ ] **Step 8: Revisit Compact navigation ergonomics only as a separate approved follow-up if real use shows it is still cumbersome**

- [ ] **Step 9: Open PR 8**

Title:

```text
feat: add policy-aware capture review
```

PR 8 plus this real-machine checkpoint is the v0.4 feature-complete boundary for this architecture phase.

---

# Cross-PR Architectural Scenario Suite

Maintain these as regression scenarios as soon as their prerequisites exist:

1. Laptop contributes X; desktop skips X.
2. Desktop Capture Disabled preserves laptop's saved X.
3. Desktop Capture Enabled deletion creates desired absence.
4. Additive restore leaves desired-absent generic package installed.
5. Exact proposes only safe explicit removal, never arbitrary extras.
6. Restore Skip beats Force + Exact.
7. Force changes Shell conflict handling without broadening convergence.
8. Missing Resource during Capture preserves previous desired state.
9. Exact never deletes Resource data.
10. Config child generic policy overrides parent generic policy while management policy remains separate.
11. Safety blocking wins over explicit Include/Apply.
12. Plan intent equals Verify intent.
13. Migration never infers desired absence from live state.
14. Omarchy semantic reconstruction beats raw package reconstruction when known.
15. Stop managing is distinct from Capture Preserve and desired absence.

# Per-PR Review Checklist

Before requesting review on every PR:

- [ ] Rebase/update from latest merged `main`.
- [ ] Run focused tests first.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `go vet ./...`.
- [ ] Run `go build ./cmd/omarchy-blueprint`.
- [ ] Run `git diff --check`.
- [ ] Review `git diff --stat` for accidental scope expansion.
- [ ] Review every new mutation path for preview/replan/precondition behavior.
- [ ] Review every new delete/remove operation for provider ownership and safety.
- [ ] Confirm TUI changes do not implement domain semantics.
- [ ] Confirm no secret/authentication state was added to profile serialization.
- [ ] Confirm user-facing terms follow established vocabulary.
- [ ] Ensure PR body states which architecture invariants changed and which later capabilities remain intentionally disabled.

# Execution Notes for Agentic Workers

- Implement one task at a time and commit after each independently testable task unless adjacent tasks cannot compile separately.
- For every failing test, confirm it fails for the intended missing behavior rather than a broken fixture.
- Never weaken an existing safety test to make the architecture pass; change production behavior unless the approved spec explicitly changes that behavior.
- When provider safety conflicts with generic policy, provider safety wins and the reason must remain visible.
- When Omarchy behavior is uncertain, inspect the current installed/upstream Omarchy source before encoding the assumption.
- Do not begin destructive Exact implementation until PR 4 policy and verification semantics are merged.
- Do not build generic systemd/service capture as part of semantic application recipes.
- Keep intermediate PR behavior conservative rather than enabling future destructive behavior early.
