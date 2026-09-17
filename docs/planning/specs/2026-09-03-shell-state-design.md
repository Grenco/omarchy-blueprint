# Omarchy Shell State Design

**Status:** Approved design
**Date:** 2026-09-03
**Project:** `omarchy-blueprint`

## Summary

Add a first-class `shell` provider that captures, diffs, restores, verifies, and validates Omarchy Shell user state from `~/.config/omarchy/shell.json`.

The provider will understand the JSON semantically for comparison and diagnostics, but the exact captured user document remains the authoritative restore payload. This preserves unknown/future JSON fields rather than rebuilding the file from a rigid Blueprint-owned schema.

The milestone also formalizes provider ownership: once Shell state has been captured, the Shell provider owns plugin enablement, placement, instances, inline settings, active bar selection, first-party disablement, and idle timings. The plugins provider continues to own installation and provenance. Legacy profiles without captured Shell state keep the current plugin enable/disable behavior.

## Current Omarchy contract

The design is based on Omarchy Quattro's current Shell contract:

- User state lives in `~/.config/omarchy/shell.json`.
- The fresh-install baseline lives at `$OMARCHY_PATH/config/omarchy/shell.json` (normally `/usr/share/omarchy/config/omarchy/shell.json`).
- Once a user `shell.json` exists, it is authoritative; Omarchy does not deep-merge later defaults into it.
- `version: 1` is currently required.
- `bar.id`, `bar.layout.*`, and `plugins[]` determine plugin presence/enabled state.
- First-party non-bar plugins are disabled through `disabledPlugins[]`.
- `idle.screensaver` and `idle.lock` are stored in the same file.
- A full Shell restart is preferred after restore rather than relying on live `reloadConfig` for this milestone.

References: Omarchy `quattro` `shell/README.md`, `manual/05-the-top-bar.md`, and `manual/31-dotfiles.md`.

## Scope

### Included

- Profile schema 4.
- Capture metadata for Shell state.
- Exact snapshot of customized `shell.json`.
- Exact snapshot of the Omarchy Shell baseline used at capture time.
- Canonical JSON hashing that ignores whitespace and object-key ordering.
- Semantic diffs for known Shell fields.
- Plugin reference extraction and profile consistency validation.
- Safe atomic Shell restore using the existing `model.FileWrite` executor primitive.
- Cross-provider dependencies so missing referenced third-party plugins are restored before `shell.json` is written.
- Full Omarchy Shell restart after a successful Shell file write.
- Legacy plugin-enable behavior for profiles that have never captured Shell state.

### Deferred

- `~/.config/omarchy/shell.toml` visual/style overrides.
- Schema migration of Shell JSON versions other than version 1.
- Reconstructing `shell.json` through individual `omarchy bar` or `omarchy plugin` commands.
- Automatically removing a user `shell.json` when the captured desired state is the Omarchy default.
- Automatic rollback commands.
- TUI work.

## Ownership model

When `manifest.capture.shell == false`:

- Plugins provider owns plugin source/provenance **and** legacy `Enabled` state.
- Existing schema 1-3 profiles behave exactly as today.

When `manifest.capture.shell == true`:

- Plugins provider owns:
  - installed third-party plugin source,
  - Git URL/revision,
  - local plugin snapshot/hash,
  - validation/rescan required to make plugin source available.
- Shell provider owns:
  - plugin enabled state,
  - bar placement,
  - plugin instances,
  - inline per-instance settings,
  - active full-bar plugin,
  - `disabledPlugins`,
  - idle settings,
  - other persisted fields inside `shell.json`.

`profile.Plugin.Enabled` remains in schema 4 as compatibility data. The plugins provider ignores it for diff/plan/verify whenever Shell state has been captured. Removing the field is a later migration, not part of this milestone.

## Profile schema

Schema 4 introduces:

```go
type CaptureMeta struct {
    Packages bool `toml:"packages"`
    Themes   bool `toml:"themes"`
    Plugins  bool `toml:"plugins"`
    Config   bool `toml:"config"`
    Defaults bool `toml:"defaults"`
    Shell    bool `toml:"shell"`
}

type Shell struct {
    Version      int    `json:"version,omitempty" toml:"version,omitempty"`
    Hash         string `json:"hash,omitempty" toml:"hash,omitempty"`
    BaselineHash string `json:"baseline_hash,omitempty" toml:"baseline_hash,omitempty"`
}
```

Profile layout:

```text
shell/
├── shell.toml
├── shell.json       # only when a customization is captured
└── baseline.json    # only when a customization is captured
```

Meaning:

- `Shell.Hash == ""` means the captured desired state is "no Blueprint-managed Shell customization".
- `Shell.Version` records the detected Omarchy Shell JSON version.
- `Shell.Hash` is the canonical JSON hash of the customized desired document.
- `Shell.BaselineHash` is the canonical JSON hash of the Omarchy baseline at capture time.
- Exact source bytes are stored separately so unknown fields and original representation survive restore.

Loader thresholds remain tied to introduction versions:

```go
const (
    configSchema   = 2
    defaultsSchema = 3
    shellSchema    = 4
)
```

A schema-3 profile upgrades in memory to schema 4 with `Capture.Shell == false`, preserving legacy plugin enablement semantics.

## Shell document parsing

Create a Shell-specific JSON document parser. It must:

1. Open only regular, non-symlink files using the existing `content.OpenRegularFile` safety primitive.
2. Decode JSON with `json.Decoder.UseNumber()`.
3. Require a top-level JSON object.
4. Require an integer `version` value.
5. Preserve the exact raw bytes.
6. Compute a raw SHA-256 hash for executor preconditions.
7. Compute a canonical JSON hash by marshaling the decoded value with Go's deterministic `encoding/json` object-key ordering.
8. Extract referenced plugin IDs from:
   - `bar.id`,
   - `bar.layout.left[]`,
   - `bar.layout.center[]`,
   - `bar.layout.right[]`,
   - `plugins[]`.
9. Return unique, sorted third-party references by excluding IDs beginning with `omarchy.` and the built-in `omarchy.bar`.

Canonical hashing intentionally guarantees equality across whitespace and object-key ordering. It does not attempt to define a new numeric equivalence language; `UseNumber` prevents precision loss for unknown fields.

## Detection state

The Shell provider uses:

```go
type Status string

const (
    StatusDefault     Status = "default"
    StatusCustomized  Status = "customized"
    StatusUnsupported Status = "unsupported"
)

type State struct {
    Status          Status
    Version         int
    Hash            string
    BaselineHash    string
    UserExists      bool
    References      []string
    Current         Document
    Baseline        Document
}
```

Detection rules:

- Baseline missing/symlink/special/invalid JSON: error.
- Baseline JSON version other than the supported version: `StatusUnsupported`.
- User file missing: logical `StatusDefault`.
- User file present but invalid/symlink/special: error.
- User JSON version other than supported: `StatusUnsupported`.
- User canonical hash equal to baseline canonical hash: `StatusDefault` even if formatting differs.
- Otherwise: `StatusCustomized`.

The provider retains raw hashes/documents internally through `Document`, allowing capture and restore planning without reinterpreting the file.

## Capture

Aggregate provider order remains:

```text
packages → themes → plugins → config → defaults → shell
```

That order is deliberate: when Shell references third-party plugins, aggregate capture has already refreshed plugin provenance in `profile.Data`.

For explicit `capture shell`:

- Detect the current Shell state.
- If customized state references third-party plugin IDs not present in the captured plugin profile, fail before mutating profile snapshots with an actionable message to capture plugins first.

For customized Shell state:

- Store exact detected user bytes as `shell/shell.json`.
- Store exact detected Omarchy baseline bytes as `shell/baseline.json`.
- Record version + canonical hashes in `shell/shell.toml` through `profile.Save`.

For default Shell state:

- Mark `Capture.Shell = true`.
- Store `Version` and `BaselineHash` metadata.
- Set `Hash = ""`.
- Remove stale `shell/shell.json` and `shell/baseline.json` snapshots.

An empty/default capture is still intentional state so later Shell customization appears as drift.

## Diff

Diff is semantic, not raw-text based.

When the saved profile has no custom Shell hash:

- current default: no change,
- current customization: additive drift (`+ shell customization present`),
- unsupported current version: warning drift.

When the saved profile has a custom Shell hash:

- same canonical hash: no change,
- current default: customization removed,
- unsupported version: migration warning,
- different customized document: compare known semantic fields.

Known summaries:

- `bar.id`
- `bar.position`
- `bar.transparent`
- `bar.centerAnchor`
- `idle.screensaver`
- `idle.lock`
- `bar.layout.left`
- `bar.layout.center`
- `bar.layout.right`
- `plugins`
- `disabledPlugins`

If canonical hashes differ but none of the known summaries detect a difference, emit a generic `~ shell configuration differs` change so unknown/future fields still create drift.

## Profile consistency and plugin references

A customized captured Shell profile must be self-consistent.

`ValidatePluginReferences` checks every third-party reference against `profile.Plugins.Items` and requires matching captured provenance. Missing provenance is an error for `capture shell` and `check`.

This does not mean targeted restore requires the plugins provider to be selected. Runtime restore rules are separate.

## Restore planning

### Desired default / no custom snapshot

If `Shell.Hash == ""`:

- Do not delete an existing machine `shell.json`.
- If the machine has an additional customization, add a skipped entry explaining that removal is disabled.
- Verification remains successful because Blueprint uses additive restore semantics.

### Desired customized Shell

If current canonical hash already equals the desired hash:

- no operation.

If current Shell JSON version is not the captured supported version:

- skip with `Shell schema changed; migration required`.

If the machine has a different user customization:

- skip with `existing user Shell configuration differs; overwrite disabled`.

If the machine is using the Omarchy default:

- safely write the exact captured `shell/shell.json`.
- If a user file exists but is semantically equal to the current default, use its raw hash as `ExpectedHash` and create a backup.
- If no user file exists, use `ExpectedMissing` and no backup.

Unlike Hyprland overrides, a changed baseline canonical hash alone does **not** block restore. Omarchy's own contract says a user `shell.json` is authoritative and is not deep-merged with future defaults. Only an incompatible Shell JSON version requires migration.

After a Shell write, append:

```text
shell.write → shell.restart
```

where restart executes:

```bash
omarchy-restart-shell
```

Do not use `reloadConfig` for this milestone.

## Cross-provider restore dependencies

After all provider plans are combined, Blueprint performs a Shell-aware plan-linking pass.

For each third-party plugin reference in the desired `shell.json`:

1. If the plugin is already discovered on the machine, no dependency is required.
2. If it is missing and the plugins provider is not part of this restore, remove Shell write/restart operations and append a skip explaining that plugins must be restored first or aggregate restore should be used.
3. If it is missing and plugins are selected, find the final planned operation for `plugin:<id>`.
4. If no such operation exists, remove Shell write/restart and append a skip because the referenced plugin cannot be reconstructed automatically.
5. Otherwise add that final plugin operation ID to `shell.write.DependsOn`.

This makes targeted `restore shell` conservative while allowing aggregate restore to reconstruct plugin code first.

## Restore-plan validation

Add a generic `restore.ValidatePlan` guard before dry-run display and execution.

It rejects:

- blank operation IDs,
- duplicate operation IDs,
- blank dependency IDs,
- unknown dependencies,
- forward dependencies.

Because every dependency must point to an earlier operation, cycles are impossible by construction.

`restore.Execute` also validates defensively at entry.

## Plugin provider compatibility behavior

Add explicit plugin semantics:

```go
type Semantics struct {
    ManageEnabled bool
}
```

When `ManageEnabled` is true, current behavior remains unchanged.

When false:

- `Diff` ignores `Enabled` differences,
- `Plan` emits no enable/disable operations,
- `Verify` ignores `Enabled`,
- source/provenance/install/pin/validate/rescan behavior is unchanged.

The app adapter uses:

```go
Semantics{ManageEnabled: !d.Manifest.Capture.Shell}
```

for plugin diff/plan/verify and capture-change summaries.

## Verification

If no custom Shell hash is captured, verification is OK regardless of extra machine Shell customization.

If a custom Shell hash is captured, verification requires:

- supported current Shell version,
- current user Shell customization exists,
- current canonical hash equals the desired canonical hash.

Plugin source/provenance verification remains the plugins provider's responsibility.

## `check`

For captured customized Shell state, `check` verifies:

- supported recorded version,
- regular non-symlink desired snapshot,
- regular non-symlink baseline snapshot,
- canonical desired hash matches metadata,
- canonical baseline hash matches metadata,
- desired and baseline document versions match recorded version,
- every third-party desired Shell reference has captured plugin provenance.

For captured default Shell state (`Hash == ""`):

- version must be supported,
- stale desired/baseline snapshots must not exist.

## CLI and UX

Add `shell` to:

```text
capture [packages|themes|plugins|config|defaults|shell]
status  [packages|themes|plugins|config|defaults|shell]
diff    [packages|themes|plugins|config|defaults|shell]
restore [packages|themes|plugins|config|defaults|shell]
```

Human plan warnings distinguish:

- replacing an existing Shell file with backup,
- creating a missing Shell file,
- Shell restart.

Progress messages distinguish `shell.write` from `shell.restart`.

## Acceptance criteria

1. Schema 1-3 profiles load as schema 4 with Shell uncaptured and legacy plugin enablement preserved.
2. A clean/default Shell capture marks Shell captured without storing a user snapshot.
3. Later Shell customization produces drift.
4. Customized Shell capture stores exact desired and baseline bytes plus semantic hashes.
5. Whitespace/key-order-only edits do not create drift.
6. Known Shell changes produce semantic summaries.
7. Unknown-field changes still produce generic drift.
8. Shell capture/check rejects missing third-party plugin provenance.
9. Aggregate restore can install a missing referenced plugin, then write Shell config, then restart Shell.
10. Targeted Shell restore refuses to write if a required third-party plugin is missing.
11. A different target-machine Shell customization is never overwritten.
12. A changed Omarchy baseline with the same supported Shell JSON version does not block restoration.
13. Shell JSON version mismatch produces a migration-required skip.
14. Profiles with captured Shell state ignore legacy plugin `Enabled` differences; profiles without captured Shell state preserve current behavior.
15. Restore-plan validation rejects invalid dependency graphs before display/execution.
