# Migration Safety / Compatibility v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add plan-integrated, category-specific migration compatibility assessment to Restore so Blueprint can surface Supported / Unknown / Incompatible evidence before mutation, reduce or block authority safely, and expose the same result through CLI JSON/human output and the TUI.

**Architecture:** Keep compatibility inside the existing Restore lifecycle. Add shared compatibility result types plus a pure normalizer/validator, make each current Restore category contribute one assessment derived from its own semantic evidence, attach the resulting report to the authoritative `model.RestorePlan`, and make `CheckRestoreApplicable` reject blocking findings before approval/journal/mutation. Preserve provider ownership of semantics, machine-aware Restore policy, Safe/Force and Additive/Exact independence, and approval stability through the existing replan + `reflect.DeepEqual` path.

**Tech Stack:** Go, existing `internal/model`, `internal/workflow`, `internal/providers/*`, Cobra CLI, Bubble Tea TUI, TOML profile fixtures/tests, existing fake command runners/filesystems; no new external runtime dependency.

**Spec:** `docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md`

**ADR:** `docs/adr/0023-plan-integrated-migration-compatibility-assessment.md`

## Global Constraints

- Implementation starts from the latest merged `main`; at plan authoring `main` is `a05025cd005fd89700bb96d16ce11ddfe10d0303`. Do not pin later PRs to this SHA: every PR starts from the latest merged `main`.
- Before any code change, use an isolated worktree. Detect whether the environment already placed the worker in a linked worktree; otherwise create a dedicated worktree/branch such as `feature/migration-safety-v1`.
- Never work in or depend on the Reconstruction Assurance worktree/branch. PR #44 has merged; preserve its accepted same-ready-runtime/readiness contract and do not rewrite the harness during the functional slices in this plan.
- Before changing anything, run exactly: `go mod download`, `go test ./...`, `go vet ./...`, `go build ./cmd/omarchy-blueprint`, `git status --short`. If this baseline is not clean, stop and report the pre-existing failure. Do not fix the known Git temporary-directory cleanup flake unless the human explicitly expands scope.
- The approved ADR and design spec are authoritative. If implementation exposes a contradiction, stop that task and amend the design through a separate reviewed documentation change rather than silently changing semantics.
- Compatibility inspection is read-only. It must not run `omarchy update`, package synchronization/update/install/remove, `sudo`, profile Save, policy mutation, or any write-as-probe behavior.
- Compatibility evidence is public plan output: never include command stderr containing credentials, environment variables, tokens, Git credentials, raw sensitive Config contents, credential-bearing plugin URLs, unrelated temporary paths, timestamps, random IDs, or other unstable/private data.
- Profile schema compatibility, Blueprint binary compatibility, Omarchy runtime compatibility, and category/provider applicability remain distinct concepts.
- `manifest.omarchy.captured_version` is profile-last-capture context only. Never treat it as per-target or per-category source provenance.
- No profile schema bump and no new persisted compatibility provenance in v1.
- Effective Restore policy is resolved before compatibility. Targets whose effective decision is Restore Skip do not block compatibility; a provider with no effective Apply intent reports `applies: false`.
- Compatibility state values are exactly `supported`, `unknown`, `incompatible`.
- Compatibility authority values are exactly `unchanged`, `reduced`, `blocked`.
- Incompatible effective intent always blocks. Unknown is never rendered/serialized as Supported.
- `--force` remains conflict authority only. It never bypasses compatibility.
- Additive/Exact remains convergence intent. Unknown compatibility can only leave destructive authority unchanged when independently justified, reduce it, or block it; it can never grant new Exact removal authority.
- A plan with any blocking compatibility finding is inapplicable as a whole. Blueprint never silently applies only the compatible subset. Explicit category scoping or Restore Skip creates a new plan instead.
- Requirements remain external readiness/remediation. Compatibility findings may link to a same-plan Requirement by ID; Blueprint never performs remediation during inspection or mid-Restore.
- Compatibility is part of `RestorePlan`, so any compatibility change after approval must produce `ErrRestorePlanChanged`, including an improvement from blocked/unknown to supported.
- Verification uses the exact effective Restore intent used for planning. Any compatibility-driven authority reduction must remain reflected in provider Verify behavior.
- CLI/TUI parity is mandatory. The TUI must render and gate on the shared workflow/domain report; it must not implement its own version comparison.
- Keep existing top-level JSON fields `profile_schema`, `omarchy_from`, and `omarchy_to`. Add compatibility; do not repurpose the existing fields into a compatibility verdict.
- No new top-level `compat` command in v1.
- Services, Sync Engine, Auto Sync, bidirectional reconciliation, automatic semantic migration, package rename databases, third-party compatibility registries, telemetry, and cross-distro portability are out of scope.
- Real cross-version VM acceptance is a separate follow-up slice after the functional feature is stable. This plan does not modify `.github/workflows/reconstruction-assurance.yml` or `test/reconstruction/`.
- Every production-code task follows TDD: add the focused failing test, run it and confirm the expected failure, implement the minimum behavior, run the focused test, then the relevant wider package suite, then commit.

## Review Focus

1. **A profile was partially recaptured on a newer Omarchy runtime:** global `captured_version` must change only the displayed profile-last-capture context; category/target compatibility must still come from category evidence, never from assuming every target originated on that runtime. Task 8 pins this.
2. **A category contains an incompatible target that effective Restore policy skips:** the category must not block because that target is outside effective Apply intent; if all targets are skipped the category reports `applies: false`. Tasks 4, 6 and 7 pin this.
3. **Compatibility becomes more permissive after approval:** a newly Supported/reduced-unblocked plan is still a different plan and must return `ErrRestorePlanChanged` rather than silently expanding authority. Task 7 pins this.
4. **Exact Restore encounters incomplete package/config/hook knowledge:** destructive work must be withheld and verification must not report false convergence. Tasks 2, 4 and 6 pin this.
5. **A compatibility probe accidentally mutates, elevates, leaks secrets, or injects unstable evidence:** provider tests must fail on mutating commands/writes and must assert sanitized deterministic findings. Tasks 2-6 pin this category by category; Task 1 pins deterministic normalization.

---

## File Structure and Ownership Map

The implementation should converge on these files and responsibilities:

- `docs/adr/0023-plan-integrated-migration-compatibility-assessment.md` — approved architectural decision; copy unchanged at execution preflight.
- `docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md` — approved design; copy unchanged at execution preflight.
- `docs/planning/plans/2026-09-26-migration-safety-compatibility-v1-implementation-plan.md` — this approved plan; copy unchanged at execution preflight.
- `internal/model/compatibility.go` — public-internal JSON/domain structs and enums for compatibility environment, evidence, findings, category and report.
- `internal/compatibility/report.go` — pure category construction, deterministic normalization, validation, aggregation and blocking-finding helpers.
- `internal/compatibility/report_test.go` — exhaustive pure tests for aggregation, invalid combinations, deterministic ordering and requirement/target validation.
- `internal/model/model.go` — add `Compatibility` to `RestorePlan` only when workflow integration is activated.
- `internal/workflow/restore.go` — build environment context, aggregate provider categories, validate the full report, attach compatibility to provider Verify context, block apply, and preserve approval stability.
- `internal/workflow/targets.go` — extend `RestoreContext` with the planned compatibility category used by Verify after planning.
- `internal/workflow/providers.go` — define `RestoreFragment` and change `RestoreProvider.Plan` to return operations/skips/requirements plus exactly one category assessment.
- `internal/workflow/compatibility_test.go` — workflow tests for policy scope, applicability, approval stability, requirements and Verify alignment.
- `internal/providers/packages/provider.go` plus `fresh_test.go` / `provider_test.go` — package origin/readiness/Exact compatibility evidence, reusing ADR 0022 state.
- `internal/providers/themes/provider.go` plus `provider_test.go` — built-in/current availability versus Git/local Theme evidence.
- `internal/providers/plugins/provider.go` plus `provider_test.go` — first-party current inventory versus third-party provenance/validation uncertainty.
- `internal/providers/resources/plan.go` plus existing plan/resource tests — affirmative portable Resource representation evidence; no Exact deletion authority.
- `internal/providers/config/provider.go` plus `provider_test.go` and focused compatibility tests — current surface/ownership/baseline compatibility findings without changing Config Auto/Included/Excluded semantics.
- `internal/providers/defaults/provider.go` plus `provider_test.go` — current semantic default surface/value applicability.
- `internal/providers/shell/provider.go` plus `provider_test.go` — structured Shell schema compatibility, replacing schema-mismatch planning strings with findings where planning remains trustworthy.
- `internal/providers/hooks/provider.go` plus `provider_test.go` — Unknown lifecycle evidence with bounded Additive authority and reduced/blocked destructive authority where applicable.
- `internal/app/app.go` — CLI Restore applicability ordering and human compatibility rendering before requirements/operations.
- `internal/app/migration_compatibility_test.go` — focused CLI human/JSON/public-contract tests rather than growing the already-large generic app test further.
- `internal/tui/screens/restore.go` — render compatibility context/categories/findings and gate Apply through shared applicability semantics.
- `internal/tui/screens/restore_compatibility_test.go` — TUI parity and blocked-Apply tests.
- `docs/cli.md` — Restore compatibility human/JSON/applicability contract.
- `docs/tui.md` — Restore review parity and blocked Apply behavior.

Prefer focused new compatibility test files over adding large unrelated sections to already-large generic test files.

## Execution Preflight

Before Task 1, use the worktree workflow and copy the approved documents into the repository unchanged.

Expected repository paths:

```text
docs/adr/0023-plan-integrated-migration-compatibility-assessment.md
docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md
docs/planning/plans/2026-09-26-migration-safety-compatibility-v1-implementation-plan.md
```

After the clean baseline succeeds, commit only those documents:

```bash
git add docs/adr/0023-plan-integrated-migration-compatibility-assessment.md \
        docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md \
        docs/planning/plans/2026-09-26-migration-safety-compatibility-v1-implementation-plan.md
git commit -m "docs: define migration safety compatibility v1"
```

Do not mutate GitHub, create a PR, merge, or alter branch protection without explicit human authorization.

## Pull Request Topology

Implement and review in this order. Each PR starts from latest merged `main` and is independently testable. Do not begin the next PR until the previous one is reviewed and merged unless the human explicitly authorizes stacked work.

1. **PR A — `feat: add compatibility domain model`**
   - Land approved ADR/spec/plan.
   - Add compatibility domain types, pure builder/normalizer/validator, deterministic ordering and tests.
   - No Restore behavior changes yet.
   - Success: compatibility semantics exist as a tested reusable domain model with no provider/UI dependency.

2. **PR B — `feat: assess core restore compatibility`**
   - Add provider-local assessment for Packages, Resources, Config and Shell.
   - Keep each assessment derived from the same provider state/baselines used for planning; no version heuristics.
   - Do not make compatibility publicly authoritative yet; tests exercise provider-local results.
   - Success: the four highest-safety/evidence categories have deterministic assessment behavior and mutation-safety tests.

3. **PR C — `feat: enforce migration compatibility in restore planning`**
   - Add Themes, Installed plugins, Omarchy default and Hooks assessments.
   - Switch the Restore provider contract to return effects plus exactly one compatibility category.
   - Aggregate/validate the report in workflow, add plan JSON, block applicability, link Requirements, carry category into Verify context, and pin approval stability/backward-compatibility behavior.
   - Success: every current Restore category participates; workflow is authoritative and CLI JSON already exposes the shared report even before human/TUI formatting work.

4. **PR D — `feat: surface restore compatibility in cli and tui`**
   - Add CLI human rendering and typed blocked-error handling.
   - Add TUI review presentation and shared Apply gating.
   - Update `docs/cli.md` and `docs/tui.md`.
   - Success: human CLI, JSON and TUI have feature parity and no client contains independent compatibility logic.

A real cross-version migration acceptance scenario is intentionally not a fifth PR in this plan. After PR D is stable, write a small follow-up acceptance design/plan amendment that selects reproducible distinct supported Omarchy runtimes and extends the now-merged Reconstruction Assurance harness without weakening its original same-runtime scenario.

---

### Task 1: Add the compatibility domain model and pure validation

**PR:** A

**Files:**
- Create: `internal/model/compatibility.go`
- Create: `internal/compatibility/report.go`
- Create: `internal/compatibility/report_test.go`
- Test: `internal/model/model_test.go` only if JSON zero-value behavior needs a focused assertion

**Interfaces:**
- Produces these model types and exact JSON shape:

```go
type CompatibilityState string
type CompatibilityAuthority string

type CompatibilityEnvironment struct {
    Known          bool   `json:"known"`
    OmarchyVersion string `json:"omarchy_version,omitempty"`
    OmarchyChannel string `json:"omarchy_channel,omitempty"`
}

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
    Category  string                 `json:"category"`
    Applies   bool                   `json:"applies"`
    State     CompatibilityState     `json:"state,omitempty"`
    Authority CompatibilityAuthority `json:"authority"`
    Evidence  []CompatibilityEvidence `json:"evidence,omitempty"`
    Findings  []CompatibilityFinding `json:"findings,omitempty"`
}

type CompatibilityReport struct {
    ProfileLastCapture CompatibilityEnvironment `json:"profile_last_capture"`
    Target             CompatibilityEnvironment `json:"target"`
    Categories         []CompatibilityCategory  `json:"categories"`
}
```
- Produces these constants exactly:
  - `CompatibilitySupported = "supported"`
  - `CompatibilityUnknown = "unknown"`
  - `CompatibilityIncompatible = "incompatible"`
  - `CompatibilityUnchanged = "unchanged"`
  - `CompatibilityReduced = "reduced"`
  - `CompatibilityBlocked = "blocked"`
- Produces pure package APIs:
  - `func BuildCategory(category string, applies bool, evidence []model.CompatibilityEvidence, findings []model.CompatibilityFinding) (model.CompatibilityCategory, error)`
  - `func NormalizeReport(report model.CompatibilityReport) model.CompatibilityReport`
  - `func ValidateReport(report model.CompatibilityReport, selectedCategories []string, targets map[string]map[string]struct{}, requirements []model.Requirement) error`
  - `func BlockingFindings(report model.CompatibilityReport) []model.CompatibilityFinding`

- [ ] **Step 1: Write failing table tests for category aggregation and enums**

Add tests named:

```go
func TestBuildCategorySupportedNeedsAffirmativeEvidence(t *testing.T)
func TestBuildCategoryUnknownReducedAggregatesConservatively(t *testing.T)
func TestBuildCategoryIncompatibleMustBlock(t *testing.T)
func TestBuildCategoryNotApplicableHasNoStateOrFindings(t *testing.T)
```

Assertions must pin the exact state/authority strings and reject an applicable empty assessment as implicit support.

- [ ] **Step 2: Run the focused tests and confirm RED**

Run:

```bash
go test ./internal/compatibility -run 'TestBuildCategory' -count=1
```

Expected: FAIL because the package/functions do not exist.

- [ ] **Step 3: Implement the model types and `BuildCategory`**

`BuildCategory` must sort evidence by `(kind, summary)`, findings by `(target, code)`, derive category State using `incompatible > unknown > supported`, derive Authority using `blocked > reduced > unchanged`, enforce `Incompatible => Blocked`, and return `applies:false` with omitted State, Unchanged authority and no findings.

- [ ] **Step 4: Add failing validator tests**

Add tests for:

```go
func TestValidateReportRequiresKnownTargetEnvironment(t *testing.T)
func TestValidateReportAllowsUnknownProfileLastCapture(t *testing.T)
func TestValidateReportRejectsDuplicateCategories(t *testing.T)
func TestValidateReportRejectsDuplicateTargetCode(t *testing.T)
func TestValidateReportRejectsUnknownRequirementReference(t *testing.T)
func TestValidateReportRejectsUnknownTargetKey(t *testing.T)
func TestValidateReportRequiresEverySelectedCategory(t *testing.T)
func TestNormalizeReportIsDeterministic(t *testing.T)
```

- [ ] **Step 5: Run validator tests and confirm RED**

```bash
go test ./internal/compatibility -run 'TestValidateReport|TestNormalizeReport' -count=1
```

Expected: FAIL for missing validation/normalization behavior.

- [ ] **Step 6: Implement `NormalizeReport`, `ValidateReport`, and `BlockingFindings`**

Validation must implement the design's domain rules without importing providers, CLI or TUI. `targets` is the inspected target namespace keyed by category. Empty finding target is allowed; a non-empty target must exist in that category's set.

- [ ] **Step 7: Run focused and package tests**

```bash
go test ./internal/compatibility ./internal/model -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/model/compatibility.go internal/compatibility internal/model/model_test.go
git commit -m "feat: add restore compatibility domain model"
```

---

### Task 2: Packages compatibility uses ADR 0022 knowledge without gaining authority

**PR:** B

**Files:**
- Modify: `internal/providers/packages/provider.go`
- Modify: `internal/providers/packages/fresh_test.go`
- Modify: `internal/providers/packages/provider_test.go`
- Create: `internal/providers/packages/compatibility_test.go`

**Interfaces:**
- Consumes: Task 1 model types and `compatibility.BuildCategory`.
- Produces a provider-local function that the later workflow wiring can call from the same detected `profile.Packages` state used by Plan. Use this signature:

```go
func RestoreCompatibility(saved, current profile.Packages, applyTargets map[string]bool, exact bool, requirements []model.Requirement) (model.CompatibilityCategory, error)
```

- [ ] **Step 1: Write failing tests for package-origin Unknown and Exact reduction**

Cover:

```go
func TestRestoreCompatibilityOriginUnknownIsNotSupported(t *testing.T)
func TestRestoreCompatibilityOriginUnknownExactIsReduced(t *testing.T)
func TestRestoreCompatibilityMetadataRequirementLinksFinding(t *testing.T)
func TestRestoreCompatibilityKnownOriginCanBeSupportedForPackageAuthority(t *testing.T)
```

Pin `packages.origin.unknown` as the stable finding code. Cover three distinct cases: installed desired package + Additive => Unknown + Unchanged with no requirement; installed desired-absent package + Exact => Unknown + Reduced with no removal; missing desired package whose install needs metadata => Unknown + Blocked with `RequirementID == "packages.metadata"`. Exact origin-dependent removals remain withheld rather than authorized.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/providers/packages -run 'TestRestoreCompatibility' -count=1
```

- [ ] **Step 3: Implement `RestoreCompatibility` from existing detected package state**

Use `OriginUnavailable`, `MissingSyncDatabases`, physical `Installed`, current official/AUR classification, semantic recipe knowledge and existing Requirement data. Do not add an Omarchy version-range rule or package rename table.

- [ ] **Step 4: Add a mutation-safety regression**

Extend the fresh-package fake runner test so producing compatibility evidence from already-detected state causes no additional mutating command; explicitly reject `-S`, `-Sy`, `-Syu`, `omarchy update`, `sudo`, `install`, `remove`, and `drop` in the compatibility path.

- [ ] **Step 5: Prove plan/verify Exact behavior remains fail-safe**

Add/extend a test showing an installed desired-absent official/AUR package with unknown origin:

- produces no removal operation;
- produces a visible skip;
- yields `Unknown + Reduced` compatibility for Exact;
- remains not converged under Exact verification.

- [ ] **Step 6: Run provider and ADR 0022 regressions**

```bash
go test ./internal/providers/packages -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/packages
git commit -m "feat: assess package restore compatibility"
```

---

### Task 3: Resources produce affirmative portability evidence independent of Omarchy version

**PR:** B

**Files:**
- Modify: `internal/providers/resources/plan.go`
- Create: `internal/providers/resources/compatibility_test.go`

**Interfaces:**
- Consumes: Task 1 compatibility model/builder.
- Produces:

```go
func RestoreCompatibility(items []profile.Resource, applyTargets map[string]bool, plan model.RestorePlan) (model.CompatibilityCategory, error)
```

Derive evidence from the Resource representation plus the already-built provider plan/preconditions passed in `plan`; do not add a second filesystem inspection pass solely for compatibility. A Resource is Supported only when its representation is one of the portable strategies and its planned effect is already safe under existing Resource rules.

- [ ] **Step 1: Write failing tests for supported portable Resource strategies**

Cover one `copy`, one clean `git`, and one `git+diff` Resource whose effective Restore decision is Apply. Assert `Supported + Unchanged` with evidence kind `portable-resource-representation` and no dependence on Omarchy version equality.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/providers/resources -run 'Compatibility' -count=1
```

- [ ] **Step 3: Implement Resource assessment from existing plan inputs/preconditions**

Do not create a version comparison. Machine path mappings stay ordinary planning input. Resources remain non-destructive under Exact.

- [ ] **Step 4: Add policy-scope tests**

Assert a skipped Resource target contributes no blocking finding, and all Resource targets skipped later aggregate as `applies:false` once workflow supplies the effective category scope.

- [ ] **Step 5: Run the full Resource package**

```bash
go test ./internal/providers/resources -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/providers/resources
git commit -m "feat: assess resource restore compatibility"
```

---

### Task 4: Config compatibility follows surface, ownership and baseline evidence

**PR:** B

**Files:**
- Modify: `internal/providers/config/provider.go`
- Create: `internal/providers/config/compatibility_test.go`

**Interfaces:**
- Consumes: Task 1 model/builder and Config's existing scan/classification/baseline structures.
- Produces provider-local compatibility from the same classifications already used to choose Config operations/skips. Use this signature so the state-provider layer can pass the exact scan used for planning rather than rescanning:

```go
func RestoreCompatibility(saved profile.Configs, scan ScanSummary, applyTargets map[string]bool, exact bool) (model.CompatibilityCategory, error)
```

Stable finding codes required by this task:
  - `config.surface.unsupported`
  - `config.baseline.ambiguous`
  - `config.owner.delegated` only if a finding is needed beyond the existing safety skip; do not invent a blocking finding for an effective policy Skip.

- [ ] **Step 1: Write failing table tests for current Config classifications**

Cases must include:

- current supported surface + usable baseline evidence => Supported;
- unsupported effective Apply surface => Incompatible + Blocked;
- ambiguous baseline => Unknown;
- `Included` does not turn Unknown/Incompatible into Supported;
- `Excluded` / effective Restore Skip excludes the target from compatibility authority;
- `ConfigDelete` is the only desired-absence input; missing unmanaged/Excluded paths never become deletion proof.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/providers/config -run 'Compatibility' -count=1
```

- [ ] **Step 3: Implement assessment by reusing Config scan/restore classification**

No profile rewrite, no baseline migration in place, no write probe. Historical baseline ambiguity remains Unknown.

- [ ] **Step 4: Add Exact safety/Verify alignment test**

For ambiguous baseline evidence under Exact, assert the planner does not gain a delete/overwrite operation from uncertainty, compatibility is Reduced or Blocked according to the existing Config safety rule, and Verify does not report false convergence for withheld Exact intent.

- [ ] **Step 5: Run Config package tests**

```bash
go test ./internal/providers/config -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/providers/config
git commit -m "feat: assess config restore compatibility"
```

---

### Task 5: Shell turns schema migration errors into structured compatibility

**PR:** B

**Files:**
- Modify: `internal/providers/shell/provider.go`
- Create: `internal/providers/shell/compatibility_test.go`
- Modify: `internal/providers/shell/provider_test.go` only where an existing schema-error assertion must change to structured compatibility

**Interfaces:**
- Consumes: Task 1 model/builder and Shell's recorded/current schema/baseline documents.
- Produces stable finding code `shell.schema.unsupported`. Use the already-detected Shell state and existing profile snapshots:

```go
func (p Provider) RestoreCompatibility(saved profile.Shell, current State, apply bool) (model.CompatibilityCategory, error)
```

- [ ] **Step 1: Write failing tests for supported and unsupported schemas**

Assert:

- supported recorded schema + supported current baseline schema + valid baseline documents => Supported + Unchanged;
- unsupported recorded schema => Incompatible + Blocked;
- unsupported current target schema => Incompatible + Blocked;
- Shell has no desired-absence compatibility path.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/providers/shell -run 'Compatibility|Schema' -count=1
```

- [ ] **Step 3: Implement structured assessment using the existing schema parser**

Where the desired/profile document is otherwise parseable, replace the existing user-visible planning-only `migration required` string path with the structured finding returned alongside an otherwise trustworthy blocked fragment. Corrupt/unparseable trusted data remains an ordinary error.

- [ ] **Step 4: Add read-only and Verify alignment tests**

Prove schema assessment only reads/parses Shell state and that supported planning/verification still uses the existing semantic merge intent.

- [ ] **Step 5: Run Shell package tests**

```bash
go test ./internal/providers/shell -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/providers/shell
git commit -m "feat: assess shell restore compatibility"
```

---

### Task 6: Add Themes, Installed plugins, Omarchy default and Hooks assessments

**PR:** C

**Files:**
- Modify: `internal/providers/themes/provider.go`
- Modify: `internal/providers/themes/provider_test.go`
- Modify: `internal/providers/plugins/provider.go`
- Modify: `internal/providers/plugins/provider_test.go`
- Modify: `internal/providers/defaults/provider.go`
- Modify: `internal/providers/defaults/provider_test.go`
- Modify: `internal/providers/hooks/provider.go`
- Modify: `internal/providers/hooks/provider_test.go`
- Create focused `compatibility_test.go` files in any of these packages when existing tests would become unwieldy

**Interfaces:**
- Consumes: Task 1 model/builder.
- Add these package-local assessment entry points so the application state-provider layer can pair each assessment with the exact detected state used by its existing Plan path:

```go
// themes
func (p Provider) RestoreCompatibility(saved, current profile.Themes, applyTargets map[string]bool, exact bool) (model.CompatibilityCategory, error)

// plugins
func RestoreCompatibility(saved, current profile.Plugins, applyTargets map[string]bool, exact bool) (model.CompatibilityCategory, error)

// defaults
func RestoreCompatibility(saved, current profile.Defaults, applyTargets map[string]bool) (model.CompatibilityCategory, error)

// hooks
func RestoreCompatibility(saved, current profile.Hooks, applyTargets map[string]bool, exact bool) (model.CompatibilityCategory, error)
```

- Produces stable finding codes at minimum:
  - `themes.builtin.unavailable`
  - `plugins.first_party.unavailable`
  - `plugins.third_party.unestablished` when target validation cannot be established read-only
  - `defaults.choice.unestablished` for captured portable choices the target semantic surface cannot affirm
  - `hooks.lifecycle.unestablished`

- [ ] **Step 1: Themes — write failing availability/provenance tests**

Built-in Theme absent from current authoritative inventory under effective Apply => Incompatible + Blocked. Portable Git/local Theme with valid captured provenance but no authoritative target contract proof => Unknown, never Supported merely because versions match.

- [ ] **Step 2: Themes — implement and run package tests**

```bash
go test ./internal/providers/themes -count=1
```

- [ ] **Step 3: Installed plugins — write failing first/third-party tests**

First-party required plugin absent from current inventory => Incompatible + Blocked. Third-party provenance remains evidence, but if validation would require mutation, report Unknown rather than probing by install/copy. Shell-owned enablement must not be duplicated.

- [ ] **Step 4: Installed plugins — implement and run package tests**

```bash
go test ./internal/providers/plugins -count=1
```

- [ ] **Step 5: Omarchy default — write failing semantic-choice tests**

Assert raw `.desktop` IDs stay non-portable, empty remains unmanaged, agent remains excluded from automatic set-only Restore, and captured portable values that current read-only semantic evidence cannot affirm are Unknown rather than version-supported.

- [ ] **Step 6: Omarchy default — implement and run package tests**

```bash
go test ./internal/providers/defaults -count=1
```

- [ ] **Step 7: Hooks — write failing lifecycle uncertainty tests**

Assert valid captured path/content/mode + safe Additive file operation with no authoritative lifecycle probe => `Unknown + Unchanged` with `hooks.lifecycle.unestablished`; do not label it Supported. Where Exact/destructive authority depends on unproven lifecycle/ownership, assert Reduced or Blocked and no gained deletion authority.

- [ ] **Step 8: Hooks — implement and run package tests**

```bash
go test ./internal/providers/hooks -count=1
```

- [ ] **Step 9: Add mutation-safety and evidence-sanitization assertions for all four categories**

Their compatibility paths may run only the read-only probes already authoritative for planning. Fail tests on provider-specific mutation commands or `sudo`. Also assert findings/evidence contain stable semantic identifiers only: no raw command stderr, credentials/tokens, environment dumps, credential-bearing URLs, temporary paths, timestamps or nondeterministic list ordering.

- [ ] **Step 10: Commit**

```bash
git add internal/providers/themes internal/providers/plugins internal/providers/defaults internal/providers/hooks
git commit -m "feat: assess remaining restore compatibility"
```

---

### Task 7: Integrate compatibility into the authoritative Restore plan

**PR:** C

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/workflow/providers.go`
- Modify: `internal/workflow/targets.go`
- Modify: `internal/workflow/restore.go`
- Create: `internal/workflow/compatibility_test.go`
- Modify: application state-provider Plan methods and `restoreProviderAdapter` in `internal/app/app.go` to compose `RestoreFragment` from one detected provider state

**Interfaces:**
- Consumes: all category assessment work from Tasks 2-6.
- Add to `model.RestorePlan`:

```go
Compatibility CompatibilityReport `json:"compatibility"`
```

- Introduce provider-local fragment:

```go
type RestoreFragment struct {
    Operations    []model.Operation
    Skipped       []model.Skipped
    Requirements  []model.Requirement
    Compatibility model.CompatibilityCategory
}
```

Place `RestoreFragment` in `workflow/providers.go`. Change the application-level `stateProvider.Plan` implementations in `internal/app` to return this fragment: each state provider performs its existing detection exactly once, builds its provider-package `model.RestorePlan`, calls the package-local `RestoreCompatibility` with that same detected state/scan/plan, and returns both together. `restoreProviderAdapter.Plan` only forwards the fragment. Concrete provider packages stay decoupled from `workflow`; do not make them import workflow or perform a second compatibility-only detection pass.

- Change `RestoreProvider.Plan` to:

```go
Plan(context.Context, profile.Data, omarchy.Info, RestoreContext) (RestoreFragment, error)
```

- Extend `RestoreContext` with:

```go
Compatibility model.CompatibilityCategory
```

The field is empty when passed into `Plan`; after Plan returns, workflow stores the returned category into the context retained for Verify.

- Add the canonical effective-intent helper used by application adapters when calling provider-local compatibility functions:

```go
func (c RestoreContext) ApplyTargets() map[string]bool
func (c RestoreContext) Applies() bool
```

`ApplyTargets` returns every resolved target key with its `Restore` boolean; `Applies` is true when at least one resolved target is Apply. Neither helper changes policy or invents defaults.

- Add typed apply error:

```go
type BlockedCompatibilityError struct {
    Findings []model.CompatibilityFinding
}
```

- [ ] **Step 1: Write failing workflow tests for environment/report assembly**

Cover:

```go
func TestPlanRestoreBuildsCompatibilityEnvironmentContext(t *testing.T)
func TestPlanRestoreUnknownProfileLastCaptureRemainsUsable(t *testing.T)
func TestPlanRestoreRequiresOneCompatibilityCategoryPerSelectedProvider(t *testing.T)
func TestPlanRestoreAllSkippedProviderIsNotApplicable(t *testing.T)
```

Pin `ProfileLastCapture.Known=false` when profile Omarchy metadata is blank and `Target.Known=true` from `omarchy.Detect`.

- [ ] **Step 2: Run focused workflow tests and confirm RED**

```bash
go test ./internal/workflow -run 'TestPlanRestore.*Compatibility|TestPlanRestoreUnknownProfile|TestPlanRestoreAllSkipped' -count=1
```

- [ ] **Step 3: Change the provider contract, add `RestoreContext.ApplyTargets`/`Applies`, and aggregate fragments**

For each selected provider: resolve `RestoreContext`, expose its exact resolved Apply map through the two helpers, call Plan once, append operations/skips/requirements, capture exactly one category, then after all providers are planned build/normalize the report with profile-last-capture and target environment context. Build the target namespace from the same inspected targets used during context resolution and validate all finding target keys. Application state providers pass `restoreCtx.ApplyTargets()` (and Exact where required) into provider-local assessment functions; they do not reconstruct policy independently. Tests must count provider detection/probe calls and prove compatibility does not introduce a logically duplicate second detection pass.

Do not derive any category state from `OmarchyFrom == OmarchyTo`.

- [ ] **Step 4: Write failing applicability tests**

Cover:

```go
func TestCheckRestoreApplicableRejectsBlockedCompatibilityBeforeRequirements(t *testing.T)
func TestCheckRestoreApplicableReturnsStructuredBlockingFindings(t *testing.T)
func TestCheckRestoreApplicableForceCannotBypassCompatibility(t *testing.T)
func TestScopedRestoreIgnoresUnselectedIncompatibleCategory(t *testing.T)
```

The deterministic applicability order is: blocking compatibility, then Requirements, then missing interactive terminal.

- [ ] **Step 5: Implement `BlockedCompatibilityError` and applicability gating**

`CheckRestoreApplicable` must return before journal creation or mutation. It must not discard the plan; callers still render it first.

- [ ] **Step 6: Write approval-stability tests**

Add tests proving both of these return `ErrRestorePlanChanged` with zero mutation:

- compatibility becomes worse after approval;
- compatibility becomes better after approval.

- [ ] **Step 7: Carry planned category into Verify context**

After each provider returns its fragment, assign `restoreCtx.Compatibility = fragment.Compatibility` in the context saved for Verify. Add a fake provider test that asserts Verify receives the exact category returned during planning.

- [ ] **Step 8: Add requirement-link validation test**

A finding with `RequirementID:"packages.metadata"` is valid only when the aggregate plan contains that Requirement. Removal/satisfaction of the Requirement on a replan changes compatibility/report or requirements and therefore invalidates old approval.

- [ ] **Step 9: Run workflow/model/compatibility tests**

```bash
go test ./internal/model ./internal/compatibility ./internal/workflow -count=1
```

- [ ] **Step 10: Run all provider packages**

```bash
go test ./internal/providers/... -count=1
```

- [ ] **Step 11: Commit**

```bash
git add internal/model/model.go internal/workflow/providers.go internal/workflow/targets.go internal/workflow/restore.go internal/workflow/compatibility_test.go internal/app/app.go
git commit -m "feat: enforce restore compatibility preflight"
```

---

### Task 8: Pin backward compatibility and partial-Capture provenance semantics

**PR:** C

**Files:**
- Create: `internal/workflow/compatibility_profile_test.go`
- Test fixture input: reuse an existing older-schema profile fixture from `internal/profile/testdata/` by loading it into a temporary workflow profile; do not modify profile serialization or fixture bytes

**Interfaces:**
- Consumes: Task 7 authoritative compatibility planning.
- Produces no new production API; this task pins profile behavior before client work.

- [ ] **Step 1: Add current-schema profile test with normal Omarchy metadata**

Assert `profile_last_capture` renders the stored version/channel as context and providers still establish category state independently.

- [ ] **Step 2: Add older supported-schema fixture test**

Load an existing older-schema fixture through current load-time migration, plan Restore, and assert compatibility works after the existing migration path without rewriting the fixture/profile directory.

- [ ] **Step 3: Add blank/missing Omarchy metadata test**

Assert planning succeeds when category evidence is otherwise sufficient and reports `profile_last_capture.known=false`.

- [ ] **Step 4: Add the critical partial-Capture provenance regression**

Construct/save desired state that predates the manifest's latest Omarchy version, perform/update only an unrelated category's Capture metadata as the existing Capture path would, then plan Restore. Assert the target category's result is unchanged by the newer global `captured_version`; only `ProfileLastCapture` context changes.

- [ ] **Step 5: Assert compatibility planning is profile-read-only**

Hash/read the profile files before and after `PlanRestore`; assert bytes are identical and no policy/profile write hook was called.

- [ ] **Step 6: Run focused and workflow tests**

```bash
go test ./internal/workflow -run 'Compatibility.*Profile|PartialCapture|LastCapture' -count=1
go test ./internal/workflow -count=1
```

- [ ] **Step 7: Commit**

```bash
git add internal/workflow/compatibility_profile_test.go
git commit -m "test: pin compatibility profile behavior"
```

---

### Task 9: Add CLI JSON and human compatibility presentation

**PR:** D

**Files:**
- Modify: `internal/app/app.go`
- Create: `internal/app/migration_compatibility_test.go`
- Modify: `docs/cli.md`

**Interfaces:**
- Consumes: Task 7 `RestorePlan.Compatibility` and `BlockedCompatibilityError`.
- Produces human renderer:

```go
func renderCompatibility(report model.CompatibilityReport) string
```

Use existing display/category-label conventions. Do not add a command or parse errors to recover findings.

- [ ] **Step 1: Write failing JSON contract test**

Invoke `restore --dry-run --json` through the public CLI test harness and assert:

- `plan.compatibility` exists;
- exact keys `profile_last_capture`, `target`, `categories`;
- Unknown is emitted, not omitted;
- `omarchy_from` and `omarchy_to` remain present;
- arrays are deterministic.

- [ ] **Step 2: Run the focused CLI test and confirm RED**

```bash
go test ./internal/app -run 'TestRestoreCompatibilityJSON' -count=1
```

- [ ] **Step 3: Write failing human-render tests**

Assert compatibility appears before Requirements/Operations and visibly distinguishes:

- Supported;
- Unknown + Reduced;
- Incompatible + Blocked;
- profile **last capture** wording rather than "source runtime" authority wording.

- [ ] **Step 4: Implement `renderCompatibility` and integrate Restore rendering**

Refactor current `renderPlan` / `renderPlanWithOptions` only enough to place the Compatibility section before Requirements and concrete operations/skips. Preserve existing Safe/Force/Exact notices.

- [ ] **Step 5: Update normal apply error flow**

For a blocked plan, render the complete plan first, then call shared `workflow.CheckRestoreApplicable`. Do not prompt for approval. JSON apply should return the structured domain error through existing error handling without a second compatibility calculation.

- [ ] **Step 6: Add Force regression through public CLI**

Plan an incompatible target with `--force`; assert the same compatibility block remains and no operation executes.

- [ ] **Step 7: Update `docs/cli.md`**

Document the three states, three authority effects, profile-last-capture context, `restore --dry-run --json`, selected-scope atomicity, explicit narrowing, Force independence, and external remediation/replanning.

- [ ] **Step 8: Run app tests**

```bash
go test ./internal/app -count=1
```

- [ ] **Step 9: Commit**

```bash
git add internal/app docs/cli.md
git commit -m "feat: show restore compatibility in cli"
```

---

### Task 10: Add TUI parity and shared blocked-Apply behavior

**PR:** D

**Files:**
- Modify: `internal/tui/screens/restore.go`
- Create: `internal/tui/screens/restore_compatibility_test.go`
- Modify: `docs/tui.md`

**Interfaces:**
- Consumes: the exact `model.RestorePlan` and `workflow.CheckRestoreApplicable` semantics from Tasks 7-9.
- Produces no independent compatibility evaluator.

- [ ] **Step 1: Write failing TUI rendering test**

Create a plan with Supported, Unknown+Reduced, Incompatible+Blocked, and `applies:false` categories. Assert Restore review shows profile-last-capture/target context plus the same category states and findings from the plan.

- [ ] **Step 2: Run focused test and confirm RED**

```bash
go test ./internal/tui/screens -run 'TestRestoreCompatibility' -count=1
```

- [ ] **Step 3: Add failing `CanApply`/Enter behavior test**

A plan with operations but blocking compatibility must not open the confirmation modal. A plan with no blocking compatibility and no Requirements retains existing behavior.

- [ ] **Step 4: Implement compatibility sections in full and compact Restore headers**

Make compatibility safety-relevant information visible even in constrained viewports; do not rely on outer clipping. Render category state/authority in the main Restore header and add finding summaries immediately beneath each non-Supported category; keep operation/skip DetailView behavior unchanged.

- [ ] **Step 5: Gate Apply through shared applicability semantics**

Replace the current `CanApply` check that only examines operations/requirements with logic based on the plan plus shared workflow applicability. Do not duplicate `BlockingFindings` ranking in the screen.

Interactive-terminal handoff from ADR 0022 remains unchanged.

- [ ] **Step 6: Add parity regression**

Construct the same `model.RestorePlan` values in the CLI and TUI focused tests and assert both render the model State/Authority strings without computing any version relationship.

- [ ] **Step 7: Update `docs/tui.md`**

Document compatibility in Restore review, blocked Apply, explicit policy/scope changes, and parity with CLI/JSON.

- [ ] **Step 8: Run TUI tests**

```bash
go test ./internal/tui/... -count=1
```

- [ ] **Step 9: Commit**

```bash
git add internal/tui docs/tui.md
git commit -m "feat: show restore compatibility in tui"
```

---

### Task 11: Full verification and architecture guardrails

**PR:** D

**Files:**
- Modify only tests/docs needed to resolve issues found by verification; no new feature scope

**Interfaces:**
- Consumes: all previous tasks.
- Produces: a branch ready for human review; no GitHub mutation is authorized by this task.

- [ ] **Step 1: Run format/static/build checks**

```bash
gofmt -w $(git diff --name-only -- '*.go')
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
git status --short
```

Expected: all commands pass; status contains only intended tracked changes for the current PR.

- [ ] **Step 2: Run targeted safety searches over the diff**

Inspect changed compatibility code for accidental mutating probes:

```bash
git diff --check
git diff -- internal/providers internal/workflow | grep -E 'pacman .*-(S|Sy|Syu)|omarchy update|sudo|profile\.Save|SetPolicy|ClearPolicy' || true
```

Any match must be reviewed manually; legitimate pre-existing mutation planning code is allowed, but compatibility inspection must not introduce a mutating probe.

- [ ] **Step 3: Re-read ADR 0023 and the design spec against the implementation diff**

Explicitly verify:

- no global version equality/range compatibility rule;
- no profile provenance field/schema bump;
- every selected category contributes one assessment;
- all-Skip category => `applies:false`;
- blocking plan is atomic;
- Force cannot bypass;
- Exact gets no authority from Unknown;
- Requirement remediation requires replanning;
- approval equality includes compatibility;
- Verify gets the planned compatibility category/effective intent;
- CLI/TUI share the plan;
- Reconstruction Assurance files are untouched.

- [ ] **Step 4: Record the known baseline-flake rule in the review handoff**

If the known Git temp-directory cleanup flake appears during the full suite, report the exact failing test and compare it to the clean baseline. Do not fix it in this tranche without human scope expansion.

- [ ] **Step 5: Commit only verification-driven fixes, if any**

Use a focused commit message describing the actual correction. Do not create a generic cleanup commit when no changes are necessary.

---

## Worker-Agent Execution Contract

After human approval of this plan, implementation uses worker agents rather than one agent silently implementing the entire tranche.

For each task:

1. dispatch a fresh worker with the approved ADR, design spec, this plan section and current branch state;
2. require TDD and focused verification;
3. have the worker commit only the task's intended files;
4. run a fresh reviewer against the task diff and task acceptance criteria;
5. address review findings before moving on;
6. at each PR boundary, run a whole-PR review against ADR/spec plus `go test ./...`, `go vet ./...`, and build;
7. do not open or mutate a GitHub PR unless the human explicitly authorizes it.

Tasks 2-6 can be assigned to separate workers only when their branches/commits are coordinated through the PR topology and they do not concurrently edit the same shared interface files. The shared `model`, `compatibility`, `workflow`, `app` and TUI integration work remains sequential to avoid interface drift.

## Completion Criteria

The functional tranche represented by PRs A-D is complete when:

1. every current Restore category reports compatibility for effective Apply intent;
2. Supported requires affirmative evidence and Unknown is explicit;
3. Incompatible effective intent blocks the selected plan;
4. Unknown can reduce authority without universally blocking safe work;
5. no compatibility result increases mutation authority;
6. Force remains conflict-only and cannot bypass compatibility;
7. Exact gains no destructive authority from incomplete knowledge;
8. unmet external readiness stays a Requirement and requires external remediation + replanning;
9. compatibility is part of approval equality and any change invalidates approval;
10. Verify uses the same effective intent/planned compatibility category;
11. older profiles and profiles without Omarchy provenance remain usable when category evidence allows it;
12. partial Capture proves global captured version is context only;
13. CLI human output, CLI JSON and TUI show the same shared compatibility result;
14. compatibility inspection remains read-only and non-elevating;
15. `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint` pass from a clean baseline, subject only to an explicitly reported pre-existing known flake;
16. Reconstruction Assurance same-runtime files/claim remain unchanged by the functional implementation.

