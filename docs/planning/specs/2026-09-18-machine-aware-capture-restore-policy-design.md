# Machine-Aware Capture and Restore Policy Design

## Status

Proposed for the v0.4 architecture phase after PR #28.

This design assumes the TUI comprehension, contextual empty-state onboarding, and root-ownership work are complete. It is intended to be implemented as a sequence of independently reviewable PRs rather than one large change.

## Summary

Omarchy Blueprint's current profile model is category-oriented: Packages, Themes, Plugins, Config, Defaults, Shell, Hooks, and Resources can be captured and restored, but most categories are effectively all-or-nothing once selected. Config has richer management policy, Packages has exclusions, and Machines currently provides Resource path overlays, but there is no consistent cross-category model for deciding:

- whether a given machine may update portable desired state during Capture;
- whether a given part of portable desired state should apply to a given machine during Restore;
- how aggressively Restore should resolve conflicts or remove explicitly undesired managed state.

This design introduces a shared policy model while preserving provider-specific ownership and safety semantics.

The core product model is:

> **Desired state** is what Blueprint remembers.
>
> **Capture policy** decides whether this machine may update that desired state.
>
> **Restore policy** decides whether that desired state should apply to this machine.
>
> **Restore intent** decides how conflicts and explicit desired absence are reconciled for this restore run.

The running system remains the configuration editor. Most users should continue to configure Omarchy normally, press Capture, move the profile, and press Restore. New items are included by default unless a provider's safety and ownership rules specifically exclude them.

The feature must not turn Blueprint into a blind snapshot or deletion tool. Safety remains provider-owned and authoritative. Policy can narrow what Blueprint manages; it cannot override safety restrictions.

## Goals

1. Provide consistent Capture and Restore policy across categories where it is meaningful.
2. Support sparse per-machine overrides over portable profile defaults.
3. Preserve the simple default workflow: ordinary users should not need to configure policy before Capture or Restore.
4. Make Capture Disabled mean "preserve the currently saved desired state," not "delete this from the profile."
5. Represent explicit desired absence separately from policy so Blueprint can remember intentional removals without treating every missing item as deletion authority.
6. Introduce Exact convergence without interpreting every local extra as unwanted.
7. Prefer authoritative Omarchy semantic installers/removers where available instead of reducing application intent to raw package-manager operations.
8. Make intentional per-machine differences visible but non-alarming in the TUI.
9. Preserve shared workflow/domain logic so CLI and TUI use the same policy and planning implementation.
10. Keep destructive capability behind inspect → calculate → preview → approve → apply → verify.

## Non-goals

This phase does not add:

- generic systemd service capture/restore;
- credential, authentication-token, or secret capture for semantic installers such as Tailscale;
- blind exact-machine synchronization;
- arbitrary deletion of Resource directories or files;
- granular Shell policy below the whole Shell state;
- TUI horizontal scrolling or a new permanent navigation model;
- a new onboarding wizard;
- independent TUI policy semantics that bypass workflow/domain logic.

Generic service management may be designed later as its own capability.

## Product principles

### The running system is still the configuration editor

Blueprint should learn ordinary user intent from normal Omarchy usage. Installing an application, choosing a theme, adding a plugin, editing supported configuration, or adding a hook should normally be learned by the next Capture without requiring a second opt-in step.

Therefore the built-in policy defaults are generally:

- Capture: Enabled
- Restore: Enabled
- Restore conflict handling: Safe
- Restore convergence: Additive

Providers may exclude targets from management when existing ownership or safety rules require it.

### Policy and safety are independent

Policy answers:

> Does the user want Blueprint to manage this target in this context?

Safety answers:

> Can Blueprint perform the requested operation safely?

An explicit Include/Apply choice never bypasses provider safety, secret filtering, ownership checks, unsupported schema checks, or destructive-operation guards.

For Config, the existing product promise remains exact:

- Auto — `Blueprint decides based on Omarchy defaults, ownership, and safety rules.`
- Included — `You asked Config to manage this path when it is safe to do so.`
- Excluded — `You asked Config to leave this path alone.`

Config detail must continue to state:

> `Included never overrides safety rules.`

### Capture Disabled means preserve, not delete

If a portable desired item already exists and this machine has Capture disabled for that target, local changes on this machine must not update, remove, or replace the saved desired state.

Example:

```text
Portable desired state
  application X = present

Laptop
  Capture: Update
  Restore: Apply

Desktop
  Capture: Ignore local changes
  Restore: Skip
```

If the desktop removes application X and captures, the portable profile still remembers application X as present.

### Desired absence is explicit state, not inference

A previously managed target can become explicitly desired-absent when:

- the target was managed;
- Capture is enabled for the target on the current machine;
- the provider can safely distinguish meaningful absence from missing/unavailable state;
- the user accepts the Capture preview.

This is different from "not managed" and from "missing from the current profile."

Exact convergence uses only explicit desired absence and authoritative Omarchy-managed intent. It must never infer deletion authority from the absence of an unrelated local item from the profile.

### Portable membership is separate from desired absence

A target can also be explicitly **unmanaged**: Blueprint carries no portable desired state for it at all.

This is different from desired-absent.

- desired present: Blueprint remembers that the target should exist/be configured;
- desired absent: Blueprint remembers an intentional removal where the provider supports it;
- unmanaged: Blueprint intentionally carries no desired state for the target.

Capture Preserve and Restore Skip do not change portable membership.

Where a category needs it, the State view/workflow exposes an explicit **Stop managing** action. Stop managing removes the provider-owned desired state/tombstone and any captured artifact owned solely by that target. It also removes target-specific policy overrides for that target so stale exceptions do not unexpectedly revive if the target is discovered again. Category-level policy remains unchanged.

Provider-specific equivalents remain authoritative where they already exist: Config keeps its established Included/Excluded/Clear Policy controls (Excluded is the leave-alone operation; Clear Policy returns to Auto), Resources uses Untrack, and an empty/unmanaged Defaults slot needs no generic destructive helper.

Once a target is unmanaged, Additive and Exact restore leave unrelated local copies alone because there is no desired state to enforce.

## Architecture overview

The recommended architecture is a hybrid policy core plus provider-defined targets.

The shared policy layer owns:

- target identity envelope: category + provider-defined key;
- Capture/Restore policy inheritance;
- profile and machine override resolution;
- policy source reporting;
- restore conflict and convergence options;
- workflow-level inspection and effective-decision plumbing.

Each provider owns:

- discovery of legitimate policy targets;
- target-key canonicalization and validation;
- hierarchy/parent relationships where applicable;
- provider-specific current and desired state;
- merge/preserve behavior during Capture;
- desired-absence representation;
- safe restore planning;
- safe Exact removal eligibility;
- verification semantics;
- authoritative reconstruction mechanism.

The TUI edits policy through workflow APIs and renders workflow results. It does not parse or mutate profile TOML directly and does not implement provider policy semantics.

## Policy target model

Every manageable unit is represented to the shared layer as a provider-defined policy target.

Conceptually:

```go
type PolicyTarget struct {
    Category string
    Key      string
}
```

Example keys:

```text
packages / official:firefox
packages / aur:obsidian
packages / mise:node

themes   / active
themes   / theme:catppuccin

plugins  / plugin:acme.weather

config   / .config/nvim
config   / .config/nvim/init.lua

defaults / browser
defaults / editor

shell    / state

hooks    / post-update.d/refresh-icons

resources / projects
```

Target keys are stable provider contracts, not UI labels. Providers are responsible for canonicalization and validation.

The shared policy engine must not interpret `official:firefox` or `.config/nvim/init.lua`; it only resolves policy for the target identity and, where declared, its parent chain.

## Target inspection model

Providers expose a read-only target inventory that can represent saved, current, new, missing, blocked, and hierarchical targets without performing capture or restore mutation.

Conceptually:

```go
type TargetState string

const (
    StateUnknown TargetState = "unknown"
    StatePresent TargetState = "present"
    StateAbsent  TargetState = "absent"
)

type PolicyTargetInfo struct {
    Key          string
    Parent       string
    Label        string
    Desired      TargetState
    Current      TargetState
    Capabilities TargetCapabilities
    SafetyReason string
}
```

Inspection is descriptive rather than executable. Actual Capture and Restore must re-inspect before mutation rather than trusting stale UI state.

This inventory also provides the foundation for first-capture candidate preview: a target can be visible before it has ever entered portable desired state.

## Policy axes

### Capture policy

The effective Capture result is binary:

- Update — this machine may update portable desired state for the target.
- Preserve — local state on this machine must not change the currently saved desired state.

Persistent rules may be inherited or explicit, but providers receive only the resolved Update/Preserve decision.

New legitimate targets default to Update unless ignored/blocked by provider safety or ownership rules.

### Restore policy

The effective Restore result is binary:

- Apply — saved desired state may be planned for this machine.
- Skip — saved desired state is intentionally not applied to this machine.

Restore Skip always narrows authority. Neither Force nor Exact can override it.

### Restore conflict handling

Conflict handling is independent of convergence:

- Safe — preserve independently changed/conflicting state when provider rules require it.
- Force — allow provider-supported conflict resolution in favor of captured intent.

Force does not broaden the set of targets in scope and does not imply deletion authority.

### Restore convergence

Convergence is independently selected:

- Additive — recreate desired-present state and apply ordinary provider desired state, while leaving Exact-only desired-absent state alone.
- Exact — allow provider-supported removal of explicitly desired-absent managed state.

Exact does not imply Force.

A restore run therefore carries two axes:

```text
Conflicts:   Safe | Force
Convergence: Additive | Exact
```

Machine files may persist defaults for both axes, and the Restore workflow may temporarily override either for one run without changing the saved machine default.

## Policy inheritance

Policy storage is sparse. Missing records mean inherit/default rather than requiring explicit entries for every discovered target.

Resolution order:

1. nearest machine target override;
2. machine category override;
3. nearest profile target override;
4. profile category override;
5. provider/built-in default.

For hierarchical providers such as Config, "nearest target" means the closest ancestor path.

Example:

```text
Profile
  Config Capture = Update

Desktop
  .config/nvim = Preserve
  .config/nvim/init.lua = Update
```

The file override wins over its directory ancestor.

The resolver is shared. Providers must not independently implement inheritance rules.

## Persistence

### Portable policy

Portable policy is stored separately from provider desired state:

```text
policy/policy.toml
```

Only overrides need to be written. Records identify:

- category;
- target, when target-specific;
- Capture setting when overridden;
- Restore setting when overridden.

Category-level rules omit the target.

Policy files are canonicalized and sorted on save so Git diffs are stable and human-readable.

### Machine policy

Machine-specific policy lives with the existing portable machine overlay:

```text
machines/<name>.toml
```

A machine may contain:

- existing Resource path mappings;
- default conflict mode;
- default convergence mode;
- sparse Capture overrides;
- sparse Restore overrides.

Missing restore-mode fields resolve to Safe + Additive.

### Desired state stays provider-owned

Desired-present/desired-absent state must not be serialized into generic policy records.

Providers retain typed state and gain the minimum domain-specific absence representation they need.

Examples:

- Packages: present package/tool declarations plus absent canonical refs.
- Themes: available themes plus absent previously-managed user themes.
- Plugins: available plugins plus absent previously-managed third-party plugins.
- Config: existing files and `ConfigDelete` tombstones.
- Hooks: present hooks plus absent tombstones carrying prior provenance.
- Defaults: empty slot means unmanaged; no absence tombstone.
- Shell: empty Hash means no Blueprint-managed customization; no absence tombstone.
- Resources: no desired-absence representation.

## Provider contract and workflow ownership

The workflow layer resolves policy before invoking provider mutation/planning.

Providers receive immutable effective decisions rather than raw policy databases.

Conceptually:

```go
type CaptureContext struct {
    Machine string
    Targets map[string]CaptureDecision
}

type RestoreContext struct {
    Machine     string
    Conflicts   ConflictMode
    Convergence ConvergenceMode
    Targets     map[string]RestoreDecision
}
```

Providers do not need to know whether a decision came from a machine target override, machine category override, profile rule, ancestor path, or built-in default.

### Capture flow

```text
provider inspection
  → shared policy resolution
  → capture inspection/preview
  → optional policy edit
  → recalculate
  → approve
  → re-inspect
  → provider merges Update targets with preserved prior desired state
  → atomic profile transaction
```

The existing cross-provider capture transaction must be preserved: staged provider artifacts, one combined profile save, then provider commit/finalize with rollback on failure.

### Capture merge invariant

For a provider that supports desired absence:

| Previous | Current | Capture | Result |
| --- | --- | --- | --- |
| unknown | present | Update | desired present |
| present | present, changed | Update | update desired |
| present | absent | Update | desired absent |
| absent | present | Update | desired present |
| present | absent | Preserve | preserve desired present |
| absent | present | Preserve | preserve desired absent |
| unknown | present | Preserve | remain unmanaged |

Providers without meaningful desired absence define a safe domain-specific result. Resources, for example, preserve previous desired state when a tracked resource is locally missing.

### Restore flow

```text
load desired state
  → inspect current targets
  → resolve Restore policy
  → resolve machine defaults + one-run overrides
  → provider plans reconciliation
  → preview
  → approve
  → replan
  → apply
  → verify against the same effective intent
```

A target with Restore Skip must remain a visible skip/reason in the plan rather than silently disappearing where practical.

### Verification invariant

Verification must receive the same effective Restore context used by planning.

Examples:

- desired-present + Restore Skip: verification ignores the target;
- desired-absent + Additive: verification does not require absence when removal is Exact-only;
- desired-absent + Exact + supported safe removal: verification requires absence after successful apply;
- unsupported/unavailable automatic action: verification must not fail solely because the planner intentionally skipped it.

Plan intent and verification intent must remain aligned.

## Differences versus actionable candidates

The domain model must distinguish:

- Difference — machine and portable desired state differ.
- Capture candidate — the difference would update desired state under current Capture policy.
- Restore candidate — the difference would produce restore work under current Restore policy and restore intent.
- Intentional difference — there is a difference, but current policy deliberately makes it non-actionable.

Intentional machine differences are not warnings by default.

This distinction is required for Overview and avoids describing a machine-specific preference as an unhealthy difference that requires attention.

## Provider capability model

Providers/targets expose descriptive capability metadata such as:

```go
type TargetCapabilities struct {
    SupportsCapture        bool
    SupportsRestore        bool
    SupportsDesiredAbsence bool
    SupportsExactRemoval   bool
    Hierarchical           bool
}
```

Capabilities describe possible behavior; they do not grant permission. Provider safety checks remain authoritative at planning time.

## Packages

### Targets

Only user-meaningful explicit declarations are ordinary policy targets:

- `official:<name>`
- `aur:<name>`
- `mise:<id>`

Dependencies are not exposed merely because they are installed.

Known CPU microcode, fingerprint, NVIDIA, and similar hardware packages remain protected machine state rather than portable desired targets.

### Defaults

- Capture: Update
- Restore: Apply

### Desired absence

A previously managed generic package/tool that disappears while Capture is Update becomes explicitly desired-absent.

Generic package absence is Exact-only for removal. Additive restore leaves an existing copy installed.

Exact may propose removal only when Packages can prove safe ownership and the package is not protected, required hardware/system state, an unmanaged dependency, or otherwise unsafe to remove.

### Reconstruction

Packages prefers the highest-level authoritative mechanism available:

1. Omarchy-native semantic installer/restorer;
2. Omarchy/native package operation;
3. authoritative remote/source;
4. captured profile content where applicable;
5. manual/fallback.

Raw package installation must not be presented as equivalent when Blueprint knows Omarchy has richer semantic setup logic.

## Omarchy preinstalls

Omarchy preinstalls are a semantic Packages subdomain, not a new top-level category.

Blueprint should understand:

- Omarchy's authoritative preinstall catalogue;
- the `~/.local/state/omarchy/preinstalls-removed` intent marker;
- individual current presence where useful;
- Omarchy-native install/remove behavior.

The global opt-out marker is explicit Omarchy-supported user intent and should be portable.

Known optional preinstall absence may be applied in ordinary Additive restore because Omarchy explicitly models the choice. It is not treated like arbitrary package absence.

Blueprint should derive the catalogue from the installed/supported Omarchy version through a provider adapter rather than permanently hard-code the list.

## Omarchy semantic application recipes

Where Omarchy ships dedicated install/remove flows, Blueprint should restore application intent through those flows when safe.

Tailscale is the motivating example: Omarchy's installer performs service enablement and other integration beyond package installation. Blueprint should restore the semantic setup rather than only installing the package.

This design does not capture authentication tokens, tailnet identity, or other secrets to automate interactive setup.

Generic systemd capture remains out of scope.

## Themes

Themes have two independent policy domains:

- `active` — selected active theme;
- `theme:<id>` — availability of a user-installed/local theme.

Built-in theme availability is owned by Omarchy and does not need portable availability policy, though a built-in theme can be desired as active.

New user themes default to Capture Update / Restore Apply.

A previously managed user theme removed while Capture is Update becomes desired-absent. Removal is Exact-only and must use safe ownership checks, preferably Omarchy's native theme removal command.

Built-in themes are never Exact-removed.

A plan must not remove the currently active theme unless it can establish a valid desired active theme first.

## Plugins

Third-party installed/local plugins are per-plugin policy targets.

New legitimate plugins default to Capture Update / Restore Apply.

A previously managed third-party plugin removed while Capture is Update becomes desired-absent. Exact may remove it through Omarchy's native plugin removal mechanism when Blueprint can prove ownership.

First-party plugins are never Exact-removed.

Plugin code/source ownership remains separate from Shell layout/enablement ownership. When Shell state is captured, Shell continues to own plugin enablement/layout semantics; Plugins owns how plugin code/source is reconstructed.

## Config

Config retains three distinct dimensions:

### Management

Existing Config policy remains:

- Auto
- Included
- Excluded

This answers whether Config may discover/manage the path.

### Capture

- Update
- Preserve

This answers whether local changes on the selected machine may update already-manageable desired state.

Capture Preserve must never be implemented by translating it into Config Excluded. Existing Config Excluded semantics prune saved state; Capture Preserve freezes it.

### Restore

- Apply
- Skip

Config is hierarchical. The TUI should show top-level directories by default with expandable directory/file detail, while the resolver uses nearest-ancestor inheritance.

Existing `ConfigDelete` is already provider-specific desired absence and remains valid ordinary restore state where current safety/precondition rules allow it.

Exact does not sweep arbitrary `.config` files just because they are absent from the profile.

## Defaults

Defaults expose four targets:

- terminal
- browser
- editor
- agent

Each defaults to Capture Update / Restore Apply when portable and safely restorable.

Defaults do not use desired-absence tombstones. A previously managed slot becoming empty under Capture Update means stop managing that slot.

The provider continues to delegate reconstruction to Omarchy's semantic default setter.

If a default cannot be restored safely, policy cannot override the restriction. The current Agent behavior remains remembered/visible but automatically unavailable while Omarchy's setter would launch the selected agent.

Exact adds no special Defaults cleanup behavior.

## Shell

Shell exposes one target:

- `state`

Policy is whole-category only in this phase:

- Capture Update / Preserve
- Restore Apply / Skip

Shell retains its semantic merge/conflict analysis against Omarchy's baseline.

Force applies only to supported Shell conflicts. Exact adds no Shell cleanup behavior.

Shell does not support desired absence. An empty captured Shell hash means Blueprint holds no managed Shell customization, not "reset every machine to Omarchy defaults."

## Hooks

Hooks expose one target per supported hook path.

New legitimate regular hooks default to Capture Update / Restore Apply. Existing unmanaged symlinks and unsupported shapes remain outside normal capture management.

A previously managed hook deleted under Capture Update becomes desired-absent with enough previous provenance to make future deletion safe, including prior content hash and mode where needed.

Exact removal is allowed only if the existing target still matches the previously managed provenance. An independently changed replacement must be skipped rather than deleted.

Additive restore does not remove desired-absent Hooks.

## Resources

Resources expose one target per tracked Resource ID.

Capture Preserve freezes the last captured Resource state for that machine.

Restore Skip prevents reconstructing that Resource on the selected machine. Dependent links must become visible skips rather than partially wiring unavailable state.

Resources do not support desired absence. A tracked Resource missing locally during Capture preserves previous desired state and reports the missing condition.

Exact never deletes Resource files or directories.

Existing machine-specific Resource path mappings remain independent of Capture/Restore policy.

## Exact convergence safety boundary

Exact means:

> Converge Blueprint-managed state toward explicit desired state wherever Blueprint can prove the destructive operation is safe.

It does not mean:

> Delete every local thing that does not exist in this profile.

Provider matrix:

| Category | Desired absence | Additive removal | Exact removal |
| --- | --- | --- | --- |
| Packages | Yes | No | safe explicit package/tool only |
| Omarchy preinstalls | Yes | supported Omarchy intent may apply | Yes |
| Themes | user themes | No | managed user themes only |
| Plugins | third-party plugins | No | managed third-party plugins only |
| Config | existing explicit tombstones | existing safe tombstones | no sweeping extras |
| Defaults | No | No | No |
| Shell | No | No | No |
| Hooks | Yes | No | provenance-matched managed hook |
| Resources | No | No | Never delete Resource data |

Restore Skip always removes the target from convergence scope.

Provider safety always wins over Exact.

## TUI information architecture

### Category tabs

Where relevant, category screens use:

```text
State | Capture | Restore
```

State remains the default tab.

State answers what Blueprint remembers and how this machine differs.

Capture answers what the selected policy scope may contribute to portable desired state.

Restore answers which portable desired state should apply to the selected policy scope.

Config's existing Auto/Included/Excluded management controls remain distinct from these tabs.

### Policy scope

Policy editing must always show whether the user is editing:

- Profile defaults; or
- a named machine.

The scope must be explicit in the main screen and not merely implied by navigation history.

When no machine is selected/available, Profile defaults is the only scope.

### Effective versus explicit policy

Inherited/effective settings should be visually quieter than explicit overrides.

Example:

```text
Firefox      ↳ Apply
Obsidian       Skip
```

The TUI provides a separate reset action to remove the current-scope override and return to inheritance rather than cycling inheritance as though it were an ordinary setting value.

### Right sidebar

The right sidebar should carry detailed explanation such as:

- desired/current state;
- effective Capture/Restore setting;
- source of inherited policy;
- safety/capability reason;
- Exact behavior when relevant;
- reconstruction mechanism.

Essential safety information must still be understandable without the sidebar, especially around 80 columns.

### Visual semantics

Color is supplementary, not the only indicator.

Intentional Skip/Preserve must not use the same visual language as blocked/unsafe state.

Suggested concepts:

- normal/positive styling: explicit managed/apply;
- muted + `↳`: inherited;
- muted/paused styling: intentional Preserve/Skip;
- warning/error styling + `!`: unavailable/blocked;
- `−`: desired absent;
- `~`: differs;
- `+`: new candidate.

## Packages TUI

Packages should group meaningful domains, including a visible Omarchy preinstalls subsection.

State shows saved/current package/tool state.

Capture shows Include/Ignore decisions.

Restore shows Apply/Skip decisions.

Desired-absent targets remain visible, with Details explaining Additive versus Exact behavior.

## Themes TUI

Themes separates:

- Active theme;
- Available themes.

Users can independently decide whether the current machine contributes/restores active-theme choice and which user themes should be carried/available.

## Plugins TUI

Plugins policy primarily controls plugin availability/source reconstruction.

When Shell owns plugin enablement/layout, Details should say so rather than expose conflicting duplicate controls.

## Config TUI

Config Capture/Restore policy is hierarchical.

Top-level directories are shown by default, with expandable child paths/files.

A parent row should indicate when descendant overrides exist.

The existing State groups remain:

- Needs review
- Changes
- No action needed
- Not managed by Config

## Defaults TUI

Defaults can show one row per slot, including unavailable restore capability and its reason when needed.

## Shell TUI

Shell gets the same tab structure for consistency but only one whole-state policy target.

## Hooks TUI

Hooks policy is per supported path. Desired-absent hooks remain visible with Details explaining provenance-based Exact removal.

## Resources TUI

Resources policy is per tracked Resource ID.

Details should make the guarantee explicit:

> Exact restore never deletes Resource data.

A missing Resource during Capture reports that previous captured state is being preserved rather than creating desired absence.

## Machines TUI

Machines remains the machine summary/control center rather than duplicating every category policy editor.

It directly owns:

- Resource path mappings;
- default conflict mode;
- default convergence mode.

It summarizes policy override counts by category and provides navigation to the relevant category policy view.

## Capture workflow

Capture remains simple for ordinary users.

### Category selection

The existing category selection controls remain a one-run choice of which categories to update. They are not persistent policy.

Category rows may summarize inspection results, for example:

```text
Packages   3 updates · 1 preserved
Config     4 updates · 2 ignored
Hooks      1 removal
```

### Review phase

Before mutation, Capture shows proposed portable-profile changes grouped as:

- Changes;
- Preserved by policy;
- Blocked;
- No action needed.

Fresh profiles can therefore show new candidates before first capture.

Policy may be edited from review; the workflow recalculates inspection before approval.

### Apply phase

Actual Capture re-inspects immediately before mutation and uses the existing transactional multi-provider save/commit/rollback model.

## Restore workflow

Restore always plans the currently selected run settings rather than rendering four parallel mode columns.

The screen visibly shows:

- machine;
- current conflict mode;
- current convergence mode;
- whether either is a temporary one-run override.

Changing either control triggers replan.

### Exact visibility

Exact must be conspicuous whenever selected and explain that Blueprint may remove explicitly undesired managed state while never deleting Resource data as Exact cleanup.

Destructive operations are individually visible in the plan and summarized again at approval.

If Force is also enabled, approval should separately summarize conflict-overriding operations.

## Overview

Overview must distinguish actionable differences from intentional machine differences.

Recommended presentation groups:

- Needs attention — blocked/unsafe/unresolved conflicts;
- Changes available — actionable Capture/Restore candidates;
- Intentional differences — policy explains why machine/profile differ;
- No action needed.

Intentional differences may remain compact/collapsed and are not warnings by default.

## Responsive behavior

Wide layouts can use navigation + main content + Details.

Narrow/compact layouts must preserve policy state and destructive-operation clarity in the main content. The right sidebar is explanatory enhancement, not required for understanding safety-critical intent.

This phase does not add horizontal scrolling or redesign compact navigation. Those remain subject to real-Omarchy acceptance after implementation.

At approximately 80 columns the user must still be able to tell:

- Apply versus Skip;
- Update versus Preserve;
- whether Exact is selected;
- whether Force is selected;
- whether a destructive operation is planned;
- whether a setting is a machine-specific exception.

## Workflow APIs

Policy mutations live in workflow, not screens.

Expected workflow responsibilities include operations conceptually equivalent to:

```go
SetProfilePolicy(...)
ClearProfilePolicy(...)
SetMachinePolicy(...)
ClearMachinePolicy(...)
SetMachineRestoreDefaults(...)
InspectCapture(...)
```

Providers validate/canonicalize target identities through shared provider registration/catalogue boundaries.

CLI and TUI call the same workflow/domain methods.

## CLI restore semantics

The internal restore API replaces the current Normal/Forced enum with explicit options:

```text
Conflicts:   Safe | Force
Convergence: Additive | Exact
```

Useful CLI compatibility may remain:

- `--force` as shorthand for conflict handling = Force;
- `--exact` as shorthand for convergence = Exact.

Explicit controls should also support overriding a persisted machine default in either direction for one run.

The old internal Normal-versus-Forced comparison API does not need a long-lived compatibility wrapper in this pre-release phase.

## Schema evolution

The current schema before this work is 11.

This phase should be treated as one intentional v0.4 schema cutover rather than many ceremonial intermediate schemas.

The exact integer may be 12 or later depending on landing order, but all persistence changes in this architecture should target one coherent current format.

### Load/migrate/save lifecycle

```text
load legacy profile
  → parse legacy fields
  → normalize into current in-memory model
  → run current validation
  → use normally
  → first explicit profile mutation
  → save current schema
  → prune obsolete legacy files
```

Loading an old profile must never mutate the running machine.

Loading should not rewrite the profile on disk simply because Blueprint started.

Git remains the review/rollback mechanism for profile-file conversion.

### Manifest capture flags remain

`Manifest.Capture` continues to distinguish an uncaptured category from a legitimately captured-but-empty category.

Policy does not replace category capture metadata.

### Legacy package exclusions

Legacy Packages `Excluded` migrates to portable unmanaged policy semantics:

```text
Capture: Preserve/disabled
Restore: Skip/disabled
Desired state: unmanaged
```

The obsolete package exclusion file/field is removed on the next current-schema save.

For legacy excluded Mise declarations, the migration does not need to preserve a permanent hidden declaration solely to recreate the old re-enable convenience. Re-enabling Capture later may rediscover the tool.

### Legacy MachineSpecific packages

Persisted `MachineSpecific` package desired state is removed in the current model.

Known hardware/machine classification becomes inspection/safety metadata discovered locally rather than portable desired state.

This also prevents confusion between legacy automatic hardware classification and the new genuine per-machine policy overlay.

### No inferred tombstones during migration

Schema migration never consults the current machine to infer desired absence.

A legacy desired-present item remains desired-present until a later policy-aware Capture inspection proposes and the user accepts a change.

### Config migration

Config `Included`, `Excluded`, and existing deletion tombstones keep their current semantics.

They are not translated into generic Capture/Restore policy.

### Machine migration

Existing machine Resource mappings survive unchanged.

Missing new machine fields resolve to:

- conflict mode = Safe;
- convergence = Additive;
- no Capture/Restore overrides.

### Policy validation

Missing policy files mean no overrides and are valid.

Hand-edited policy validates at two levels:

Shared layer:

- known category;
- valid setting;
- no duplicate/conflicting records.

Provider:

- syntactically valid canonical target;
- valid hierarchy/key structure.

Target presence on the current machine is not required because policy may refer to desired-absent or temporarily unavailable targets.

## Compatibility scope

There are currently no external users, so compatibility engineering should be intentionally limited.

Required:

- current schema-11 profile loads safely;
- existing portable desired state survives;
- package exclusions preserve effective unmanaged behavior;
- Config policy semantics remain unchanged;
- Resource machine mappings survive;
- old machines default to Safe + Additive;
- no tombstones are inferred during migration;
- current-schema save prunes obsolete legacy files;
- older Blueprint rejects a newer unsupported profile rather than misinterpreting it.

Not required:

- downgrade support;
- dual-writing old and new policy formats;
- migration backups outside Git;
- elaborate compatibility for every historical development schema;
- preservation of every excluded-Mise re-enable edge case;
- long-lived old internal restore APIs.

## Error handling and safety

### Policy errors

Invalid policy files fail validation with actionable category/target context. Unknown machine names or invalid target syntax are not silently ignored.

### Inspection errors

A provider inspection failure must not silently convert state to absent or create tombstones. Capture preview should surface the provider error and block the affected category/target as appropriate.

### Capture errors

Capture remains transactional across selected categories. A provider or profile-save failure rolls back staged provider state according to the existing transaction model.

### Restore errors

Restore remains plan-first and preconditioned. Approved plans are recalculated before apply.

A failed safety precondition converts the operation into a failure/skip according to existing execution semantics rather than retrying destructively.

### Destructive operations

All Exact removals must be explicit plan operations with:

- provider ownership;
- target identity;
- reason/provenance;
- appropriate risk classification;
- preconditions where the provider can validate existing state;
- visible preview before approval.

No generic workflow layer may manufacture a removal simply because a target is absent from desired state. Only the owning provider can plan safe removal.

## Testing strategy

### Policy resolver tests

Cover precedence and source reporting:

- machine target over machine category;
- machine category over profile target;
- profile target over profile category;
- provider default fallback;
- nearest Config ancestor;
- reset-to-inherit behavior.

### Provider target tests

Each provider must test:

- stable/canonical target identities;
- eligibility filtering;
- desired/current classification;
- safety-blocked targets;
- hierarchy where applicable.

### Capture scenario tests

Architectural regression scenarios include:

1. Laptop contributes application X; desktop Capture Preserve does not erase it.
2. New target + Capture Update enters desired state.
3. New target + Capture Preserve stays unmanaged.
4. Managed present + local absent + Capture Update becomes provider-specific desired absence where supported.
5. Resources missing locally preserve previous desired state.
6. Config Capture Preserve does not behave like Config Excluded.
7. Stop managing removes desired state without creating desired absence and clears target-specific stale overrides where the provider uses the shared action.
8. Safety blocking wins over explicit Include/Update.

### Restore scenario tests

1. Restore Skip suppresses target work under Safe/Additive.
2. Restore Skip still suppresses target work under Force + Exact.
3. Force resolves provider-supported conflict without broadening convergence scope.
4. Additive leaves Exact-only desired-absent package/plugin/theme/hook state alone.
5. Exact plans only provider-approved desired-absence removals.
6. Exact never deletes Resource data.
7. Shell ignores Exact and still obeys Safe/Force merge behavior.
8. Verification matches the approved effective restore intent.

### Migration tests

Cover the actual supported path:

- current schema 11 → current v0.4 in-memory model;
- no machine mutation on load;
- package exclusions → unmanaged policy;
- machine mappings survive;
- no inferred desired absence;
- save writes current format and prunes obsolete package files;
- newer unsupported schema is rejected by older/current loader rules as appropriate.

### TUI tests

Cover:

- State/Capture/Restore tab navigation;
- explicit policy scope visibility;
- inherited versus explicit indicators;
- reset-to-inherit;
- Config hierarchy/override count;
- machine restore defaults;
- one-run restore overrides;
- Capture review grouping;
- Exact/removal visibility;
- intentional differences not rendered as warnings;
- constrained width with no safety-critical information depending on the Details pane.

## Implementation sequence

Implementation should land in eight independently reviewable PRs.

### PR 1 — `refactor: introduce target policy domain`

Add shared policy types, restore axes, inheritance resolver, source reporting, hierarchy support, and resolver tests.

No persistence, user-facing behavior, schema change, or provider semantic change.

### PR 2 — `refactor: make provider workflows target-aware`

Add provider target inventories, Capture/Restore contexts, target-aware Plan/Verify plumbing, and read-only capture inspection while feeding existing effective defaults so behavior remains unchanged.

Establish plan/verification intent alignment before new policy is activated.

### PR 3 — `feat: add portable and machine capture policy`

Perform the v0.4 profile cutover, add policy persistence and machine overrides, migrate package exclusions/hardware state, activate Capture Update/Preserve semantics, provider-specific desired absence, explicit stop-managing semantics where applicable, and policy-aware capture merge behavior.

Because Restore policy/Exact activation lands in PR 4+, every new Exact-only tombstone introduced by PR 3 must already be loadable and conservatively non-destructive under the existing effective Safe + Additive restore path. PR 3 must not create a profile state that the immediately-following Restore workflow misinterprets as deletion authority. Existing Config deletion semantics remain unchanged.

### PR 4 — `feat: add machine-aware restore policy`

Activate Restore Apply/Skip, persisted machine restore defaults, one-run restore overrides, Safe/Force + Additive/Exact options, policy-filtered plans, explicit policy skips, and policy-aware verification.

Do not yet introduce broad new destructive Exact removal behavior.

### PR 5 — `feat: add safe exact convergence`

Enable provider-owned Exact removal for Themes, Plugins, Hooks, and any required integration of existing Config deletion behavior.

Add strong negative safety tests and explicit no-delete guarantees for Resources and Shell.

### PR 6 — `feat: restore Omarchy application intent`

Add Omarchy preinstall semantics, native semantic application recipe selection, the Tailscale-style reconstruction path, and safe Exact removal for Packages where ownership/protection rules permit it.

No generic systemd capture.

### PR 7 — `feat: expose machine policy controls in TUI`

Add category State/Capture/Restore tabs, policy scope, inherited/explicit presentation, Config hierarchy, Theme split, preinstall grouping, machine restore defaults, restore one-run controls, and Details explanations.

### PR 8 — `feat: add policy-aware capture review`

Use capture inspection to implement first-capture candidate review, policy-adjust-and-recalculate flow, intentional-difference Overview semantics, final help/catalogue updates, and full UX integration.

## Real-Omarchy acceptance checkpoints

### After PR 6

Run behavior acceptance on a real Omarchy machine for:

- package discovery;
- removed/reinstalled preinstalls;
- at least one semantic application installer such as Tailscale where practical;
- theme/plugin install/remove;
- Hook deletion provenance;
- Safe versus Force planning;
- Additive versus Exact planning;
- Resources no-delete guarantees.

This checkpoint validates Omarchy assumptions before the full TUI is built around them.

### After PR 8

Run full UX acceptance at approximately:

- 140 columns;
- 100 columns;
- 80 columns;
- too-small conditions.

Exercise:

- fresh profile;
- first Capture preview;
- machine-specific Capture policy;
- machine-specific Restore policy;
- Exact Restore preview;
- Force override;
- one-run restore overrides;
- Config hierarchy;
- Theme active/availability split;
- intentional machine differences.

Only after this real-machine pass should compact-navigation ergonomics be reconsidered.

## Acceptance criteria

The architecture is complete when all of the following are true:

- Most users can still configure Omarchy normally and use Capture/Restore without preconfiguring policy.
- Newly discovered legitimate targets are captured by default.
- Capture Preserve cannot delete or overwrite previously saved desired state.
- Explicit Stop managing is distinct from Capture Preserve and from desired absence; unmanaged state produces no Exact cleanup authority.
- Profile policy and machine overrides resolve predictably through one shared resolver.
- Restore Skip cannot be bypassed by Force or Exact.
- Force and Exact are independent restore axes.
- Exact removal is limited to explicit desired absence and authoritative Omarchy-managed intent.
- Providers, not shared workflow code, own destructive safety decisions.
- Resources are never deleted as Exact cleanup.
- Shell receives no granular or Exact-cleanup semantics in this phase.
- Config management policy remains distinct from Capture/Restore policy.
- Theme active selection and available-theme reconstruction can be configured independently.
- Omarchy preinstall opt-out/removal intent can be captured and replayed.
- Known Omarchy semantic installers are preferred over raw package-only reconstruction.
- Plan and Verify use the same effective restore intent.
- Intentional machine differences remain inspectable without being reported as failures/warnings.
- Capture previews first-time candidates before they enter the profile.
- Actual Capture and Restore re-inspect/replan before mutation.
- All destructive restore work remains inspect → calculate → preview → approve → apply → verify.
- Existing schema-11 profiles migrate safely without inferring deletions from current machine state.
- Current-schema save removes obsolete legacy package-policy files rather than dual-writing them.
- TUI policy controls are projections of workflow/domain state and do not create parallel semantics.

## Deferred work

The following remain explicitly outside this design:

- generic user/system systemd service capture and restoration;
- credentials or identity-state transfer for services such as Tailscale;
- arbitrary exact filesystem synchronization;
- Resource deletion semantics;
- granular Shell-policy trees;
- horizontal scrolling/permanent partial sidebar redesign;
- compatibility engineering for a mature external user base;
- generalized provider plugin APIs beyond what current built-in providers require.

## Decision summary

This design adopts:

- a shared policy resolver with provider-defined targets;
- sparse portable defaults plus sparse machine overrides;
- Capture Update/Preserve independent from Restore Apply/Skip;
- Safe/Force conflict handling independent from Additive/Exact convergence;
- provider-specific desired absence;
- provider-owned Exact deletion safety;
- Omarchy-native reconstruction as the preferred semantic path;
- category State/Capture/Restore TUI tabs with explicit policy scope;
- capture candidate preview as a workflow capability, not a TUI-only feature;
- one intentional pre-release schema cutover with narrow migration scope;
- an eight-PR implementation sequence with two real-Omarchy acceptance checkpoints.

The resulting product model is:

> Blueprint remembers portable intended Omarchy state, understands which machines may contribute to that state, understands which parts should apply to each machine, and safely reconstructs that intent without treating local differences as accidental by default.
