# ADR 0006: portable Hyprland configuration

Blueprint captures customized Omarchy Hyprland configuration files as content
alongside the baselines they were derived from, and restores them only when it
can prove the target file is not carrying unknown user work.

## Scope

This increment manages exactly four files:

```text
hypr/hyprland.lua
hypr/bindings.lua
hypr/looknfeel.lua
hypr/autostart.lua
```

Monitors, input, shell state, terminal configuration, and other user config
are deferred to later increments.

## Profile layout (schema 2)

```text
profile.toml
config/config.toml
config/files/<path>
config/baseline/<path>
```

`config/config.toml` records one entry per captured file with its ID, relative
path, desired content hash, and the hash of the Omarchy baseline it was
captured against. `config/files/` stores the user's customized content;
`config/baseline/` stores the Omarchy-provided content it was captured against.
Entries are sorted by ID for deterministic diffs. Profiles loaded with schema 1
upgrade in memory to schema 2 with no config state.

## Baseline safety model

Detection compares the user's file in `~/.config` against the Omarchy baseline
in `${OMARCHY_PATH:-/usr/share/omarchy}/config`:

- identical to baseline → default (never captured);
- different from baseline → customized (captured);
- absent → missing;
- baseline no longer shipped by this Omarchy version → unsupported.

Symlinks and non-regular files are rejected for both baseline and user paths.
Capture only stores customized files and removes stale snapshots when a file
returns to its Omarchy default.

Restore may replace a target file only under one of these conditions:

1. the target already matches the desired hash (no operation);
2. the target is missing and the current baseline still matches the captured
   baseline;
3. the target matches the current baseline and that baseline equals the
   captured baseline.

Otherwise the operation is skipped: baseline drift is reported as
`Omarchy baseline changed; migration required` (a later migration engine can
three-way merge captured baseline + desired content + current baseline), user
drift is reported as
`existing user configuration differs; overwrite disabled`, and unsupported
files are skipped with an explicit reason. Blueprint never overwrites user
work.

## Writes, backups, and journal

Config writes are medium-risk atomic `FileWrite` operations: the source hash is
verified immediately before mutation, the destination is revalidated against
its expected hash or expected-missing precondition, a backup of the replaced
file is created beside the restore journal (`<journal>.backup/`), the write is
staged in a temporary file, fsynced, and atomically renamed. Every write is
journaled with a `BACKUP_CREATED` event so it can be reversed manually until a
dedicated rollback command exists.

When at least one file is written, a final `hyprctl reload` operation is
appended and depends on every write, so a failed write blocks the reload while
independent operations continue.

## Verification

Verification is intentionally asymmetric, like packages: every saved
customization must exist and match the desired hash, but extra customization on
the target machine is reported as status/diff drift and never fails restore
verification.

## Deferred

Monitor/input sections, shell state, terminal configuration, cross-version
migrations, and a dedicated rollback command are postponed.
