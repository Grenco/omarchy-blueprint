# Shell Baseline-Aware Semantic Merge Design

**Status:** Approved design  
**Date:** 2026-09-04  
**Project:** `omarchy-blueprint`  
**Profile schema:** unchanged at 4

## Summary

Replace the Shell provider's whole-document conflict rule with a baseline-aware four-way semantic merge.

Blueprint already has the four documents required:

```text
A = captured Omarchy baseline      shell/baseline.json
B = captured desired Shell state   shell/shell.json
C = current Omarchy baseline       $OMARCHY_PATH/config/omarchy/shell.json
T = current target Shell state     ~/.config/omarchy/shell.json, or C when absent
```

Portable source intent is `A -> B`. Independent target customization is `C -> T`.

Normal restore applies source intent where the target has not independently changed the same semantic unit. Target-only customization is preserved. True conflicts preserve the target value and do not block unrelated safe Shell changes.

`--force` resolves only conflicting Shell merge units in favor of captured source intent. It does not replace unrelated target-only state or bypass version, profile-integrity, plugin-provenance, backup, dependency, or destination-precondition safety.

This fixes the laptop-to-desktop case without requiring the target `shell.json` to be removed first.

## Goals

1. Derive portable Shell intent from captured baseline versus captured desired state.
2. Preserve target-only Shell customization.
3. Apply non-conflicting source intent automatically.
4. Report overlapping source/target edits as semantic conflicts.
5. Apply safe siblings even when another unit conflicts.
6. Add `--force` so captured intent wins only conflicting units.
7. Make Shell status/diff/verification intent-based instead of whole-document based.
8. Carry unknown future version-1 object fields through the generic merge when possible.
9. Keep arrays conservative and atomic in the first merge version.
10. Preserve existing third-party plugin provenance safety for code newly enabled by the result.
11. Continue using atomic `FileWrite`, raw destination preconditions, backups, journaling, and restart dependencies.
12. Do not bump the profile schema.

## Non-goals

This milestone does not:

- merge individual layout entries by widget/plugin ID;
- create machine overlays;
- infer hardware capabilities;
- remove target-only widgets/plugins;
- support unsupported Shell JSON versions;
- add an interactive conflict editor;
- add arbitrary directory capture;
- implement Hooks.

## Why schema 4 is sufficient

Schema 4 already persists:

```text
shell/shell.toml
shell/shell.json
shell/baseline.json
```

with the desired semantic hash and captured baseline hash. No new persistent profile state is required. Existing schema-4 profiles should gain merge behavior without recapture.

## Terminology

For customized captured Shell state:

```text
A = source baseline
B = source desired
C = target baseline
T = target effective state
M = proposed merged state
```

`T` is the current user document when it exists, otherwise `C`.

A unit contains source intent when `A != B`. A unit is independently changed on the target when `C != T`.

A true conflict is:

```text
A != B
C != T
B != T
```

## Merge model

Add a pure merge layer under `internal/providers/shell`:

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
```

The merge engine must never mutate its four input trees.

### Absent versus null

Use an internal node wrapper:

```go
type mergeNode struct {
    Present bool
    Value   any
}
```

Two absent nodes are equal. One absent and one present node differ. Present `null` is therefore different from an absent key.

### Equality

Use canonical JSON semantic equality. Documents are already decoded with `json.Decoder.UseNumber`, and `encoding/json` provides deterministic object-key marshaling for comparison.

## Recursive boundary

Recurse only when all four corresponding nodes are present JSON objects.

All other cases are atomic merge units:

- strings;
- booleans;
- numbers;
- null;
- arrays;
- key additions/deletions where all four sides are not objects;
- type changes.

This naturally makes the first version conservative for:

```text
bar.layout.left
bar.layout.center
bar.layout.right
plugins
disabledPlugins
```

while still merging stable object children such as:

```text
idle.lock
idle.screensaver
bar.position
bar.transparent
futureObject.someField
```

Inline widget settings remain part of their containing layout array in this milestone.

## Atomic decision table

For each atomic unit:

```text
sourceChanged = A != B
targetChanged = C != T
```

Apply these rules in order.

### Source did not change it

```text
A == B
=> result T
=> no drift
=> no conflict
```

This is the central portability rule: target-only Shell customization is allowed.

### Target already satisfies source intent

```text
B == T
=> result T
=> no drift
=> no conflict
```

### Source changed it; target did not

```text
A != B
C == T
B != T
=> result B
=> safe applied change
```

This remains true when the current Omarchy baseline differs from the captured baseline. Explicit source intent wins when the target is still at its current baseline.

### Both changed differently

```text
A != B
C != T
B != T
```

Normal restore:

```text
result T
record conflict
```

Forced restore:

```text
result B
record forced applied change
```

## Examples

### Target-only change

```text
A bar.position = top
B bar.position = top
C bar.position = top
T bar.position = left
```

Result: `left`. No Shell drift.

### Source-only change

```text
A idle.lock = 300
B idle.lock = 600
C idle.lock = 300
T idle.lock = 300
```

Result: `600`.

### Conflict

```text
A idle.lock = 300
B idle.lock = 600
C idle.lock = 300
T idle.lock = 900
```

Normal: keep `900`, report conflict.  
Force: apply `600`.

### Current baseline changed upstream

```text
A futureSetting = 1
B futureSetting = 1
C futureSetting = 2
T futureSetting = 2
```

Result: `2`. The profile never customized that field, so it does not pin the old default.

### Layout conflict

`bar.layout.right` is one atomic array. If laptop and desktop both changed it differently, normal restore keeps the desktop array and reports one conflict; force selects the laptop array. Other independent fields still merge.

## Empty source intent

When `saved.Hash == ""`, the captured machine had no Shell customization to reproduce.

Therefore:

```text
status shell => clean even if target has customization
verify shell => OK
restore shell => no operation
```

The profile is not an instruction to remove target customization.

## Version gate

Four-way merge only runs when captured baseline, captured desired, target baseline, and target effective state all use `SupportedVersion` and captured versions agree with profile metadata.

Unsupported versions remain migration-required. `--force` cannot bypass this.

## Captured baseline integrity

Planning, diff, and verification all depend on `shell/baseline.json`, so the internal intent loader must enforce:

```text
desired.Hash == saved.Hash
desired.Version == saved.Version
baseline.Hash == saved.BaselineHash
baseline.Version == saved.Version
```

The current `Check` behavior already validates the baseline hash. The planner's private snapshot validation must be strengthened to do the same.

## Shared intent analysis

Do not implement slightly different merge semantics in Plan, Diff, Verify, and plugin linking.

Recommended provider helpers:

```go
type ShellIntent struct {
    Baseline Document
    Desired  Document
}

func (p Provider) loadIntent(saved profile.Shell) (ShellIntent, error)
func effectiveTarget(current State) Document
func (p Provider) Analyze(saved profile.Shell, current State, options MergeOptions) (MergeResult, error)
```

All Shell behavior should consume this analysis.

## Intent-based diff and status

For customized source state:

1. load and validate A/B;
2. detect C/T;
3. run non-forced analysis;
4. report `Applied` units as normal drift;
5. report `Conflicts` as conflict warnings;
6. omit target-only differences.

Examples:

```text
~ idle.lock: 300 -> 600
~ bar.position: "top" -> "bottom"
~ bar.layout.center differs
! bar.layout.right changed independently; current target value preserved
```

Any unsatisfied source intent (`Applied` or `Conflicts`) yields status exit 2. Target-only customization does not.

## Intent-based verification

Verification succeeds when non-forced analysis returns:

```text
Applied == empty
Conflicts == empty
```

Failure labels identify semantic units:

```text
shell:idle.lock
shell:bar.position
shell:bar.layout.right
```

A partial normal restore may succeed operationally while verification remains incomplete because conflicts were intentionally preserved.

## Exact fast path

Keep exact captured-byte restore when it is semantically correct:

```text
captured baseline A == current baseline C
AND target is semantically default
AND exact B is semantically equal to proposed M
```

This preserves captured formatting and unknown fields on a fresh machine with the same Omarchy baseline.

When current baseline changed or target is customized, use generated merge output.

## Generated merged output

A merge produces a new `M` not present in the profile. Do not write temporary generated files into the Git profile.

Extend generic `model.FileWrite`:

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

Exactly one source form is valid:

```text
file source:
  Generated=false, Source non-empty, Content nil

generated source:
  Generated=true, Source empty, Content non-nil
```

`Content` is intentionally omitted from JSON dry-run output so full Shell/plugin settings are not dumped into machine-readable plans.

For generated content, executor source mode is `0644`. Existing-target replacements continue preserving the destination mode.

Encode merged JSON with deterministic indentation and one trailing newline, then parse it through `ParseDocument` before planning the write.

## Destination safety

When user `shell.json` exists:

```text
ExpectedHash = current.Current.RawHash
Backup = true
```

When absent:

```text
ExpectedMissing = true
Backup = false
```

Generated-source hashing, destination validation, backup, final-boundary revalidation, atomic rename, directory sync, journaling, and cancellation behavior stay generic in the restore executor.

## Partial conflicts

Conflicts do not cancel unrelated safe changes.

If three units are safe and one conflicts, proposed M contains the three source values plus the target value for the conflicted unit. Plan emits one `shell.write`, one dependent `shell.restart`, and conflict skip/warning entries.

If every unsatisfied source unit conflicts and no safe change remains, emit conflict entries only; do not write or restart Shell.

## Force semantics

Add:

```bash
omarchy-blueprint restore shell --force
omarchy-blueprint restore --force
```

Force means:

> resolve supported Shell semantic conflicts in favor of captured profile intent.

It does not mean verbatim replacement of the target Shell file.

Source-untouched target values remain preserved under force.

A `shell.write` containing at least one forced conflict is `RiskHigh`. Other Shell writes remain `RiskMedium`.

Force does not bypass:

- Shell version checks;
- captured snapshot/hash validation;
- plugin provenance for newly introduced refs;
- dependency failures;
- destination raw-hash checks;
- symlink/regular-file protections;
- backups;
- journaling;
- atomic writes.

Force still prompts unless `--yes` is also supplied.

## Restore option plumbing

Use explicit planning options rather than hidden mutable state:

```go
type restorePlanOptions struct {
    Force bool
}
```

Change the app-internal provider interface to:

```go
Plan(context.Context, profile.Data, omarchy.Info, restorePlanOptions) (model.RestorePlan, error)
```

Non-Shell providers ignore Force. `shellStateProvider` passes it to `shell.MergeOptions`.

CLI help:

```text
--force  resolve supported restore conflicts in favor of the profile (currently Shell)
```

## Plugin provenance after merge

Semantic merge may preserve target-local third-party plugin references that never existed in the source profile. Those must not suddenly require source provenance.

After proposing M, calculate:

```text
introducedRefs = ThirdPartyReferences(M) - ThirdPartyReferences(T)
```

Only those newly introduced third-party IDs require captured plugin provenance and plugin restore dependency linking.

Target-local refs already present in T are preserved without being claimed by the source profile.

The source profile's `Check` still requires provenance for all third-party references in captured desired B, because that is profile self-consistency.

Recommended API:

```go
func (p Provider) RequiredThirdPartyPlugins(saved profile.Shell, current State, options MergeOptions) ([]string, error)
```

It must reuse the same proposed-result logic as Plan.

## Unknown fields

Unknown version-1 object fields participate in the generic merge:

```text
source untouched => target wins
source-only change => source applies
both changed => conflict
```

Unknown arrays remain atomic. Never reconstruct Shell state from a rigid Go struct.

## Human UX

Normal conflict:

```text
Shell

~ idle.lock: 300 -> 600
~ bar.position: top -> bottom

! bar.layout.right changed independently on this machine;
  keeping the current value
```

Force:

```text
! Force enabled: conflicting Shell values will be replaced by captured
  profile intent; unrelated target-only Shell customization is preserved.
```

Do not dump full arrays in human output.

## Testing strategy

### Merge engine

Test source-only, target-only, satisfied intent, conflict, force, additions, deletions, add/add conflicts, delete/change conflicts, recursive independent object-child edits, atomic arrays, type changes, unknown fields, deterministic paths, and input immutability.

### Diff/verify

Test empty source intent, target-only changes, source-only drift, conflict drift, baseline evolution, target-only verification, unresolved conflict verification, and fully merged verification.

### Plan

Test exact fast path, changed baseline merge, customized target merge, partial conflict, all-conflict no-write, force, risk levels, generated unknown-field preservation, raw destination preconditions, and tampered captured baseline rejection.

### Generated FileWrite

Test missing create at `0644`, existing-mode preservation, source hash mismatch, mutually exclusive source forms, JSON content omission, backup creation, and final destination recheck.

### Plugin provenance

Test target-local unprofiled plugin preservation, source-introduced plugin requirements, matching/mismatching installed provenance, missing plugin dependencies, and forced plugin introduction.

### Vertical slices

Use laptop-to-desktop cases with independent bar layout, scalar conflicts, `--force`, current-baseline evolution, and target-local plugins.

## Acceptance criteria

1. `profile.Schema` remains 4.
2. Existing schema-4 profiles require no recapture.
3. Target-only Shell customization is not drift.
4. Source intent comes only from A -> B.
5. Target customization comes from C -> T.
6. Objects merge recursively; arrays/scalars/type changes are atomic.
7. Safe source changes apply automatically.
8. Target-only changes are preserved.
9. Normal true conflicts preserve target values.
10. Safe siblings still apply during conflicts.
11. `--force` selects source only for true conflicts.
12. Force preserves source-untouched target state.
13. Force does not bypass hard safety gates.
14. Forced writes are high risk.
15. New upstream defaults survive for source-untouched fields.
16. Unknown version-1 object fields survive generated merge.
17. Generated output uses generic atomic `FileWrite`.
18. Generated content is not serialized in JSON plans.
19. Verification checks intent, not whole-document hash equality.
20. Target-local plugin refs do not require source provenance.
21. Newly introduced refs retain provenance/dependency safety.
22. Full tests, vet, and build pass.

## Future work

Later milestones may add widget-aware array merge, machine overlays, interactive TUI conflict selection, and reusable baseline-aware structured merge for other providers.

Hooks remain postponed until this cross-machine Shell behavior is proven in real use.
