# Shell Baseline-Aware Semantic Merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace whole-document Shell conflict refusal with a four-way semantic merge that preserves target-only customization, partially applies safe source intent, and supports a constrained `--force` source-wins conflict policy.

**Architecture:** Reuse the schema-4 captured baseline and desired snapshots; do not change the profile schema. Add one pure object-recursive/array-atomic merge engine under the Shell provider, make Diff/Verify/Plan share its intent analysis, add generated in-memory `FileWrite` sources for merged JSON, and thread a restore-planning force option through generic provider orchestration.

**Tech Stack:** Go 1.25+, Cobra, standard-library `encoding/json`, existing Blueprint Shell `Document`, provider registry, restore planner/executor/journal.

**Spec:** `docs/superpowers/specs/2026-09-04-shell-semantic-merge-design.md`

## Global Constraints

- Do **not** bump `profile.Schema`; it remains `4`.
- Existing schema-4 profiles must work without recapture.
- `A = captured baseline`, `B = captured desired`, `C = current baseline`, `T = current effective state`.
- Source intent is only state where `A != B`.
- Source-untouched target state is preserved and is not Shell drift.
- Recursively merge only nodes where all four values are present JSON objects.
- Arrays, scalars, null, additions/deletions, and type changes are atomic merge units.
- Normal conflicts keep target values and do not block unrelated safe merge changes.
- `--force` resolves only true Shell conflicts in favor of source desired values.
- Force must preserve source-untouched target state.
- Force must not bypass version, integrity, provenance, dependency, backup, or destination-precondition checks.
- A forced-conflict Shell write is `model.RiskHigh`; other Shell writes remain `model.RiskMedium`.
- Generated merged content must not be serialized into JSON dry-run output.
- Destination mutation continues through generic atomic `FileWrite`.
- Target-only third-party plugin refs do not require source-profile provenance.
- Third-party refs newly introduced by proposed Shell result still require captured matching provenance.
- Keep arrays atomic; do not add widget-ID list merging.
- Final verification must run `gofmt`, `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint`.

---

## File map

### Create

```text
internal/providers/shell/merge.go
internal/providers/shell/merge_test.go
docs/superpowers/specs/2026-09-04-shell-semantic-merge-design.md
docs/superpowers/plans/2026-09-04-shell-semantic-merge.md
docs/adr/0009-semantic-shell-merge.md
```

### Modify

```text
internal/model/model.go
internal/restore/executor.go
internal/restore/executor_test.go

internal/providers/shell/document.go
internal/providers/shell/diff.go
internal/providers/shell/diff_test.go
internal/providers/shell/plan.go
internal/providers/shell/plan_test.go
internal/providers/shell/provider.go
internal/providers/shell/provider_test.go

internal/app/providers.go
internal/app/app.go
internal/app/app_test.go

README.md
ROADMAP.md
docs/manual-test-checklist.md
docs/adr/0008-portable-omarchy-shell.md
```

### Responsibility boundaries

- `shell/merge.go`: pure four-way JSON merge only; no filesystem, profile, CLI, or plugin installation logic.
- `shell/document.go`: parsing, deterministic encoding, semantic/raw hashing, references, JSON helpers.
- `shell/diff.go`: transform shared intent analysis into `model.Change` and verification output.
- `shell/plan.go`: exact fast path versus generated merge write, conflict skips, and risk.
- `shell/provider.go`: load/validate captured intent and expose merge-aware plugin requirements.
- `model/model.go`: generic generated `FileWrite` source representation.
- `restore/executor.go`: generic generated-byte source execution; no Shell-specific logic.
- `app/providers.go`: explicit restore-plan option plumbing.
- `app/app.go`: Cobra `--force`, combined-plan finalization, human warnings/progress.

---

### Task 1: Add the pure four-way Shell merge engine

**Files:**
- Create: `internal/providers/shell/merge.go`
- Create: `internal/providers/shell/merge_test.go`
- Modify if helper reuse is needed: `internal/providers/shell/document.go`

**Interfaces:**

```go
type MergeOptions struct {
    Force bool
}

type MergeChange struct {
    Path   []string
    Before any
    After  any
    Forced bool
}

type MergeConflict struct {
    Path   []string
    Source any
    Target any
}

type MergeResult struct {
    Value     map[string]any
    Applied   []MergeChange
    Conflicts []MergeConflict
}

func Merge(
    sourceBaseline map[string]any,
    sourceDesired map[string]any,
    targetBaseline map[string]any,
    targetCurrent map[string]any,
    options MergeOptions,
) (MergeResult, error)
```

- [ ] **Step 1: Write failing scalar decision-table tests**

Add `TestMergeAtomicDecisionTable` using one-key object roots and cover:

```text
A=1 B=1 C=1 T=1 -> 1, clean
A=1 B=1 C=1 T=2 -> 2, clean target-only change
A=1 B=2 C=1 T=1 -> 2, one applied
A=1 B=2 C=1 T=2 -> 2, already satisfied
A=1 B=2 C=1 T=3 -> 3, one conflict
A=1 B=2 C=1 T=3 + force -> 2, one forced applied
A=1 B=2 C=9 T=9 -> 2, source applies across baseline evolution
```

- [ ] **Step 2: Run focused test and verify failure**

```bash
go test ./internal/providers/shell -run '^TestMergeAtomicDecisionTable$' -count=1
```

Expected: FAIL because `Merge` does not exist.

- [ ] **Step 3: Implement absent-aware nodes**

```go
type mergeNode struct {
    Present bool
    Value   any
}
```

Implement `nodeEqual(a,b)`:

```text
both absent -> true
one absent -> false
both present -> canonicalValue values equal
```

Do not equate absent and JSON null.

- [ ] **Step 4: Implement recursive object merge**

Add `mergeNodeValue(path, a, b, c, t, options, result)`.

Recurse only when all four nodes are present `map[string]any` values. Merge the sorted union of child keys. Returned absent child nodes are omitted from output.

Never mutate any input map/slice.

- [ ] **Step 5: Implement atomic rules exactly**

```go
sourceChanged := !nodeEqual(a, b)
if !sourceChanged {
    return cloneNode(t)
}
if nodeEqual(b, t) {
    return cloneNode(t)
}
targetChanged := !nodeEqual(c, t)
if !targetChanged {
    appendApplied(path, t, b, false)
    return cloneNode(b)
}
if options.Force {
    appendApplied(path, t, b, true)
    return cloneNode(b)
}
appendConflict(path, b, t)
return cloneNode(t)
```

- [ ] **Step 6: Add structural tests**

Add:

```go
TestMergeSourceAddsKey
TestMergeSourceDeletesKey
TestMergeTargetOnlyAddedKeyIsPreserved
TestMergeAddAddConflict
TestMergeDeleteChangeConflict
TestMergeIndependentObjectChildren
TestMergeArraysAreAtomic
TestMergeArrayConflictCanBeForced
TestMergeTypeChangeIsAtomic
TestMergeInputsAreNotMutated
TestMergePathsAreDeterministic
```

The independent-child test must prove source `idle.lock` and target `idle.screensaver` changes both survive with no conflict.

- [ ] **Step 7: Run merge tests**

```bash
go test ./internal/providers/shell -run '^TestMerge' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/shell/merge.go internal/providers/shell/merge_test.go internal/providers/shell/document.go
git commit -m "feat: add four-way shell merge engine"
```

---

### Task 2: Add shared Shell intent analysis and intent-based Diff/Verify

**Files:**
- Modify: `internal/providers/shell/provider.go`
- Modify: `internal/providers/shell/diff.go`
- Modify: `internal/providers/shell/diff_test.go`
- Modify: `internal/providers/shell/provider_test.go`

**Interfaces:**

```go
type ShellIntent struct {
    Baseline Document
    Desired  Document
}

func (p Provider) loadIntent(saved profile.Shell) (ShellIntent, error)
func effectiveTarget(current State) Document
func (p Provider) Analyze(saved profile.Shell, current State, options MergeOptions) (MergeResult, error)
```

- [ ] **Step 1: Write failing intent tests**

Add:

```go
TestDiffEmptySourceIntentIgnoresTargetCustomization
TestDiffIgnoresTargetOnlyFieldChange
TestDiffReportsSourceOnlyIntent
TestDiffReportsIndependentConflict
TestVerifyAllowsTargetOnlyCustomization
TestVerifyFailsUnsatisfiedSourceIntent
TestVerifyFailsUnresolvedConflict
TestDiffKeepsChangedCurrentBaselineForUntouchedSourceField
```

Example target-only assertion:

```text
A position=top, B position=top, C position=top, T position=left
Diff == []
Verify.OK == true
```

- [ ] **Step 2: Run focused tests and verify failure**

```bash
go test ./internal/providers/shell -run 'Diff|Verify|Intent' -count=1
```

Expected: FAIL under whole-document semantics.

- [ ] **Step 3: Strengthen captured intent loading**

`loadIntent` must read both snapshots and enforce:

```text
desired.Hash == saved.Hash
desired.Version == saved.Version
baseline.Hash == saved.BaselineHash
baseline.Version == saved.Version
```

A tampered baseline is a hard error for Diff/Verify/Plan, not only for `check`.

- [ ] **Step 4: Implement effective target selection**

```go
func effectiveTarget(current State) Document {
    if current.UserExists {
        return current.Current
    }
    return current.Baseline
}
```

- [ ] **Step 5: Implement shared `Analyze`**

```text
saved.Hash == "" -> no portable source intent, clean result
unsupported/current version mismatch -> typed/version error
customized source -> Merge(A.Value, B.Value, C.Value, T.Value, options)
```

- [ ] **Step 6: Rewrite `Diff`**

Call `Analyze(..., MergeOptions{Force:false})`.

For `Applied`:

- scalar value: `~ path: current -> desired`;
- array/object value: `~ path differs`.

For conflicts:

```text
! path changed independently; current target value preserved
```

Use `model.ChangeWarn` for conflicts. Omit target-only differences. Sort by path.

- [ ] **Step 7: Rewrite `Verify`**

```go
OK := len(result.Applied) == 0 && len(result.Conflicts) == 0
```

Missing labels:

```text
shell:<dot-path>
```

Deduplicate and sort.

- [ ] **Step 8: Run Shell tests**

```bash
go test ./internal/providers/shell -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/shell/provider.go internal/providers/shell/diff.go internal/providers/shell/diff_test.go internal/providers/shell/provider_test.go
git commit -m "feat: make shell status intent-aware"
```

---

### Task 3: Add generated in-memory sources to generic FileWrite

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/restore/executor.go`
- Modify: `internal/restore/executor_test.go`

**Interfaces:**

```go
type FileWrite struct {
    Source          string `json:"source,omitempty"`
    Generated       bool   `json:"generated,omitempty"`
    Content         []byte `json:"-"`
    SourceHash      string `json:"source_hash"`
    ExpectedHash    string `json:"expected_hash,omitempty"`
    ExpectedMissing bool   `json:"expected_missing,omitempty"`
    Backup          bool   `json:"backup"`
}
```

- [ ] **Step 1: Write failing generated-source tests**

Add:

```go
TestFileWriteGeneratedContentCreatesMissingFile
TestFileWriteGeneratedContentPreservesExistingMode
TestFileWriteGeneratedContentRejectsHashMismatch
TestFileWriteRejectsGeneratedAndPathSourceTogether
TestFileWriteRejectsGeneratedWithoutContent
TestFileWriteGeneratedContentIsOmittedFromJSON
```

The create test must assert mode `0644`.

The JSON test must prove content bytes do not appear but `"generated":true` does.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
go test ./internal/restore -run 'FileWriteGenerated|GeneratedContent' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Validate exactly one source form**

Valid path source:

```text
Generated=false
Source non-empty
Content nil
```

Valid generated source:

```text
Generated=true
Source empty
Content non-nil
```

A non-nil zero-length slice is valid generated content. `SourceHash` remains required.

- [ ] **Step 4: Refactor source opening behind one helper**

Recommended:

```go
type fileWriteSource struct {
    Reader io.ReadSeeker
    Mode   os.FileMode
    Close  func() error
}

func openFileWriteSource(action model.FileWrite) (fileWriteSource, error)
```

Path source uses `content.OpenRegularFile`. Generated source uses `bytes.NewReader(action.Content)` and source mode `0644`.

Keep existing hash-then-seek behavior identical for both.

- [ ] **Step 5: Preserve destination mode behavior**

```text
missing target -> source mode (0644 for generated)
existing target -> existing target mode
```

Do not add explicit desired mode support in this milestone.

- [ ] **Step 6: Run restore/full regression suite**

```bash
go test ./internal/restore -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/restore/executor.go internal/restore/executor_test.go
git commit -m "feat: support generated file write sources"
```

---

### Task 4: Rewrite Shell planning around semantic merge and force

**Files:**
- Modify: `internal/providers/shell/document.go`
- Modify: `internal/providers/shell/plan.go`
- Modify: `internal/providers/shell/plan_test.go`

**Interfaces:**

```go
func EncodeDocument(value map[string]any) (Document, error)

func (p Provider) Plan(
    saved profile.Shell,
    current State,
    schema int,
    from, to string,
    options MergeOptions,
) (model.RestorePlan, error)
```

- [ ] **Step 1: Write failing fast-path/merge-path tests**

Add:

```go
TestPlanExactFastPathWhenBaselinesMatchAndTargetDefault
TestPlanUsesGeneratedMergeWhenCurrentBaselineChanged
TestPlanUsesGeneratedMergeForCustomizedTarget
TestPlanPreservesTargetOnlyUnknownField
TestPlanNoOpWhenCurrentBaselineAlreadySatisfiesSourceIntent
```

Exact fast path:

```go
op.File.Generated == false
op.File.Source == p.shellSnapshotPath("shell.json")
```

Merge path:

```go
op.File.Generated == true
op.File.Source == ""
len(op.File.Content) > 0
```

- [ ] **Step 2: Write failing partial-conflict/force tests**

Add:

```go
TestPlanPartialConflictStillWritesSafeChanges
TestPlanAllConflictsEmitsNoWrite
TestPlanForceAppliesConflictingSourceValue
TestPlanForcePreservesSourceUntouchedTargetValue
TestPlanForcedWriteIsHighRisk
TestPlanNonForcedMergedWriteIsMediumRisk
TestPlanRejectsTamperedBaselineSnapshot
```

Partial example:

```text
source intent: lock 300->600, position top->bottom
target: lock 900 conflict, position top unchanged
```

Normal output must contain `lock=900`, `position=bottom`, plus conflict skip.

- [ ] **Step 3: Run plan tests and verify failure**

```bash
go test ./internal/providers/shell -run '^TestPlan' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement deterministic encoding**

```go
func EncodeDocument(value map[string]any) (Document, error) {
    raw, err := json.MarshalIndent(value, "", "  ")
    if err != nil { ... }
    raw = append(raw, '\n')
    return ParseDocument(raw)
}
```

- [ ] **Step 5: Implement plan decision tree**

Pseudo-code:

```text
saved.Hash empty -> no plan
validate/load A/B
unsupported/version mismatch -> migration-required skip
analysis := Analyze(saved,current,options)
normal conflicts -> append semantic shell:<path> skips
no Applied -> no write/restart
proposed M := analysis.Value
if captured baseline == current baseline
   and target is default
   and exact B semantically equals M:
      use existing profile snapshot source
else:
      EncodeDocument(M)
      use generated FileWrite source
append shell.write and shell.restart
```

Do not use force to bypass the version or snapshot gates.

- [ ] **Step 6: Apply risk rule**

```text
any analysis.Applied.Forced -> shell.write RiskHigh
otherwise -> RiskMedium
```

`shell.restart` remains RiskMedium.

- [ ] **Step 7: Keep raw destination preconditions**

User file exists:

```go
ExpectedHash = current.Current.RawHash
Backup = true
```

User file absent:

```go
ExpectedMissing = true
Backup = false
```

- [ ] **Step 8: Run Shell tests**

```bash
go test ./internal/providers/shell -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/shell/document.go internal/providers/shell/plan.go internal/providers/shell/plan_test.go
git commit -m "feat: merge shell state during restore"
```

---

### Task 5: Make plugin provenance requirements merge-aware

**Files:**
- Modify: `internal/providers/shell/provider.go`
- Modify: `internal/providers/shell/provider_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**

```go
func (p Provider) RequiredThirdPartyPlugins(
    saved profile.Shell,
    current State,
    options MergeOptions,
) ([]string, error)
```

Return only third-party IDs newly introduced by proposed result M relative to target T.

- [ ] **Step 1: Write failing required-plugin tests**

Add:

```go
TestRequiredPluginsIgnoresTargetLocalReference
TestRequiredPluginsIncludesSourceIntroducedReference
TestRequiredPluginsIncludesForcedConflictReference
TestRequiredPluginsUsesExactFastPathDifference
```

Target-local example:

```text
T references local.desktop-widget
M preserves it
profile has no provenance for it
-> not required
```

Source introduction:

```text
T lacks grenco.weather
M references grenco.weather
-> required
```

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/providers/shell -run 'RequiredPlugins' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement introduced-reference set difference**

```go
func introducedReferences(before, after Document) []string
```

Return sorted `after.References - before.References`.

Factor a shared proposed-result helper if needed so Plan and RequiredThirdPartyPlugins cannot diverge.

- [ ] **Step 4: Update app combined-plan finalizer**

When Shell write participates:

1. detect current Shell state;
2. pass same `Force` option;
3. get introduced refs only;
4. retain existing plugin provenance-equivalence logic;
5. add plugin operation dependencies for missing captured plugins;
6. block/remove `shell.write` and `shell.restart` if an introduced plugin cannot be safely satisfied.

Do not require provenance for preserved target-local refs.

- [ ] **Step 5: Add finalizer regression tests**

Add:

```go
TestFinalizeShellAllowsUncapturedTargetLocalPlugin
TestFinalizeShellStillBlocksDifferingIntroducedPlugin
TestFinalizeShellAddsDependencyForMissingIntroducedPlugin
TestFinalizeForcedShellPluginIntroductionStillChecksProvenance
```

- [ ] **Step 6: Run Shell/app tests**

```bash
go test ./internal/providers/shell ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/shell/provider.go internal/providers/shell/provider_test.go internal/app/app.go internal/app/app_test.go
git commit -m "fix: scope shell plugin provenance to introduced refs"
```

---

### Task 6: Plumb `--force` through generic restore planning

**Files:**
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**

```go
type restorePlanOptions struct {
    Force bool
}
```

Change `stateProvider.Plan` to:

```go
Plan(context.Context, profile.Data, omarchy.Info, restorePlanOptions) (model.RestorePlan, error)
```

- [ ] **Step 1: Write failing CLI tests**

Add:

```go
TestRestoreAcceptsForceForShell
TestAggregateRestoreAcceptsForce
TestForceWithoutYesStillPrompts
TestForceAndYesRunsNonInteractively
```

`--force` is conflict policy, not approval.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
go test ./internal/app -run 'Force|force' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Add Cobra flag**

```go
var dryRun, yes, force bool
```

```go
cmd.Flags().BoolVar(
    &force,
    "force",
    false,
    "resolve supported restore conflicts in favor of the profile (currently Shell)",
)
```

Pass `restorePlanOptions{Force:force}` into targeted and aggregate planning.

- [ ] **Step 4: Update all app provider adapters**

Packages/themes/plugins/config/defaults ignore the new options argument explicitly. Shell passes:

```go
shellprovider.MergeOptions{Force: options.Force}
```

Do not store Force in global `options` and do not use context values.

- [ ] **Step 5: Thread same options into plan finalization**

The combined-plan plugin linker must receive the same `restorePlanOptions` used for provider planning.

- [ ] **Step 6: Run app/full tests**

```bash
go test ./internal/app -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/providers.go internal/app/app.go internal/app/app_test.go
git commit -m "feat: add force restore planning option"
```

---

### Task 7: Add human partial-conflict and force UX

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:** Human rendering only.

- [ ] **Step 1: Write failing render tests**

Add:

```go
TestRenderShellPartialConflictShowsPreservedTarget
TestRenderForceWarnsSourceWinsConflicts
TestRenderForceDoesNotSayWholeFileWillBeReplaced
TestRenderGeneratedShellWriteStillShowsBackupWarning
```

Required force meaning:

```text
Force enabled: conflicting Shell values will be replaced by captured profile intent; unrelated target-only Shell customization is preserved.
```

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/app -run 'Render.*Shell|Render.*Force' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Render semantic Shell conflict skips**

Use resources like:

```text
shell:idle.lock
shell:bar.layout.right
```

Human form:

```text
! bar.layout.right changed independently on this machine; keeping the current value
```

Plan reason should mention `--force`.

- [ ] **Step 4: Render force warning once**

Print one prominent warning before approval when force is active and Shell participates. Do not imply whole-file replacement.

- [ ] **Step 5: Keep backup/restart messages accurate**

Generated source does not change backup/restart behavior.

- [ ] **Step 6: Run app tests**

```bash
go test ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "feat: explain shell merge conflicts"
```

---

### Task 8: Add cross-machine vertical slices

**Files:**
- Modify: `internal/app/app_test.go`
- Modify focused Shell fixtures only when integration exposes a defect

**Interfaces:** Public `Execute` flows and real profile/current-baseline files.

- [ ] **Step 1: Add laptop-to-desktop non-conflicting slice**

Build:

```text
A source baseline:
  idle.lock=300
  bar.position=top
  right layout=default

B source desired:
  idle.lock=600
  bar.position=bottom
  right layout unchanged

C target baseline:
  same defaults

T target:
  right layout customized only
```

Assert:

```text
status shell -> drift for lock + position only
restore shell --dry-run -> write planned, no right-layout conflict
restore shell --yes -> lock 600, position bottom, desktop right layout preserved
status shell -> clean
```

- [ ] **Step 2: Run slice and fix only integration defects**

```bash
go test ./internal/app -run 'LaptopToDesktopShellMerge' -count=1
```

Expected: PASS after implementation.

- [ ] **Step 3: Add partial conflict slice**

Target additionally sets `idle.lock=900`.

Normal restore must:

```text
position -> bottom
lock stays 900
right layout stays desktop-specific
status remains exit 2 for lock conflict
```

- [ ] **Step 4: Add forced conflict slice**

```bash
omarchy-blueprint restore shell --force --yes
```

Assert:

```text
lock -> 600
position -> bottom
right layout still desktop-specific
status -> clean
```

- [ ] **Step 5: Add current-baseline-evolution slice**

Source leaves one future field untouched while Omarchy changes its default. Restore must retain the new current default. Include a different source-intended field to prove explicit intent still applies.

- [ ] **Step 6: Add target-local plugin slice**

Target has an uncaptured local plugin ref before restore. Source changes unrelated scalar state. Restore must preserve target plugin without requiring profile provenance and still apply scalar intent.

- [ ] **Step 7: Run full suite**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app_test.go internal/providers/shell
git commit -m "test: cover cross-machine shell merging"
```

---

### Task 9: Update architecture and user docs

**Files:**
- Create: `docs/adr/0009-semantic-shell-merge.md`
- Ensure present: design spec and this plan
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Modify: `docs/manual-test-checklist.md`
- Modify: `docs/adr/0008-portable-omarchy-shell.md`

- [ ] **Step 1: Add ADR 0009 and supersession note**

ADR 0009 documents A/B/C/T merge, recursive objects/atomic arrays, partial conflicts, intent-based status, force, generated FileWrite, plugin provenance, and no schema bump.

In ADR 0008, add a short note that ADR 0009 supersedes only the whole-document restore conflict rule. Keep ADR 0008 historical capture/ownership decisions intact.

- [ ] **Step 2: Update README**

Use wording equivalent to:

```text
Blueprint restores Shell customizations captured relative to Omarchy's baseline. Independent target Shell customization is preserved; overlapping changes are reported as conflicts. --force lets captured Shell intent win those conflicts without discarding unrelated target state.
```

Document `--force`.

- [ ] **Step 3: Update ROADMAP**

Put semantic Shell merge ahead of Hooks. Keep later:

```text
Hooks
machine overlays
input.lua / monitors.lua
user-selected directories
migration engine
TUI
```

- [ ] **Step 4: Expand manual test checklist**

Include:

```text
fresh desktop no shell.json
desktop independent scalar customization
desktop independent bar layout
same scalar changed differently
--force conflict
changed Omarchy baseline
target-local third-party plugin
newly introduced captured third-party plugin
destination edit between planning/execution
backup verification
```

- [ ] **Step 5: Search for stale whole-file wording**

```bash
rg 'overwrite disabled|existing user Shell configuration differs|exact captured shell.json|whole.*Shell|--force' README.md ROADMAP.md docs internal/providers/shell internal/app
```

Historical ADR language may remain only when clearly marked superseded.

- [ ] **Step 6: Final verification**

```bash
gofmt -w internal
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: all commands succeed.

- [ ] **Step 7: Commit**

```bash
git add README.md ROADMAP.md docs internal
git commit -m "docs: describe semantic shell restoration"
```

---

## Final review checklist

- [ ] `profile.Schema` is still `4`.
- [ ] Existing schema-4 profiles work without recapture.
- [ ] `saved.Hash == ""` means no portable Shell intent.
- [ ] Target-only Shell customization does not create drift.
- [ ] Merge uses both source and current baselines.
- [ ] Object recursion occurs only when all four nodes are objects.
- [ ] Arrays are atomic.
- [ ] Missing key and JSON null are distinct.
- [ ] Merge inputs are not mutated.
- [ ] Normal conflicts keep target.
- [ ] Safe siblings still merge during a conflict.
- [ ] Force conflicts pick source.
- [ ] Force does not change source-untouched target nodes.
- [ ] Force cannot bypass unsupported Shell version or snapshot integrity.
- [ ] Force cannot bypass plugin provenance for introduced refs.
- [ ] Forced Shell writes are `RiskHigh`.
- [ ] `--force` still prompts without `--yes`.
- [ ] New upstream defaults survive for source-untouched fields.
- [ ] Exact fast path is used only when equivalent to proposed semantic result.
- [ ] Generated `FileWrite.Content` is excluded from JSON serialization.
- [ ] Generated source hash is validated before destination mutation.
- [ ] Existing destination mode is preserved; missing generated file uses `0644`.
- [ ] Existing destination raw hash remains the mutation precondition.
- [ ] Existing target Shell file is backed up.
- [ ] Shell restart depends on successful `shell.write`.
- [ ] Intent-based verification ignores target-only state.
- [ ] Unresolved conflicts fail verification/status.
- [ ] Target-local third-party refs do not require source provenance.
- [ ] Newly introduced refs do require captured matching provenance.
- [ ] Human output explains partial conflicts and force accurately.
- [ ] Full tests/vet/build pass.

## Suggested PR commit sequence

```text
feat: add four-way shell merge engine
feat: make shell status intent-aware
feat: support generated file write sources
feat: merge shell state during restore
fix: scope shell plugin provenance to introduced refs
feat: add force restore planning option
feat: explain shell merge conflicts
test: cover cross-machine shell merging
docs: describe semantic shell restoration
```

## PR description points

```text
- Shell restore now replays captured intent instead of requiring whole-file equality.
- Independent target Shell changes are preserved and no longer count as drift.
- Overlapping changes become partial conflicts rather than blocking the whole Shell restore.
- --force resolves those conflicts in favor of captured intent without discarding unrelated target state.
- Existing schema-4 profiles work without recapture.
- Newly enabled third-party plugins retain provenance/dependency safety.
```
