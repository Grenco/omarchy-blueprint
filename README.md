# Omarchy Blueprint

Use your system normally. Omarchy Blueprint remembers how to rebuild it.

This repository currently contains package reconstruction plus the first theme
vertical slice from [ROADMAP.md](ROADMAP.md). It captures explicitly
installed native and foreign packages, separates known machine-specific
hardware packages, shows semantic drift, creates a safe restore plan, installs
only missing portable packages through Omarchy, and verifies the result. It
never removes additional packages.

Theme state can also be captured, compared, restored, and verified. Built-ins
are referenced by name, clean Git themes retain their URL and revision, and
local themes or built-in overlays are copied into the profile.

First-party Omarchy plugin enablement is captured semantically. Clean Git
plugins retain their public URL and revision; modified, cloned, and local
plugins are snapshotted under `plugins/local/`. Restore uses Omarchy validation
and lifecycle commands, and marks executable third-party code as high risk.

## Requirements

- Omarchy 4 or newer
- Go 1.25 or newer when building from source
- `pacman` and the public `omarchy` CLI on `PATH`

## Build and test

```sh
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

## First workflow

Create and capture a profile:

```sh
mkdir -p ~/omarchy-profile
omarchy-blueprint init ~/omarchy-profile --name main
omarchy-blueprint --profile ~/omarchy-profile capture
```

After cloning that profile on another Omarchy machine:

```sh
omarchy-blueprint check
omarchy-blueprint status
omarchy-blueprint restore --dry-run
omarchy-blueprint restore
```

Category-less `status`, `diff`, and `restore` operate on every captured
provider. Use `status packages`, `status themes`, `restore packages`, or
`restore themes` when you want to target one category.

Category-less `capture` captures every supported provider. Use
`capture packages` or `capture themes` for a targeted refresh.

The non-interactive form is `omarchy-blueprint restore --yes`. Combine `--json`
with `--dry-run` or `--yes`; JSON restores never wait for a prompt.

Package profiles are human-readable:

```text
profile.toml
packages/official.txt
packages/aur.txt
packages/machine-specific.txt
packages/excluded.txt
themes/themes.toml
themes/local/<theme>/
plugins/plugins.toml
plugins/local/<plugin-id>/
```

Machine-specific entries retain provenance, for example
`official:nvidia-open` or `aur:nvidia-580xx-dkms`.

## Package exclusions

Exclude a package that should remain outside portable restore:

```sh
omarchy-blueprint --profile ~/omarchy-profile exclude package:dislocker-git
```

`package:<name>` resolves against the captured profile. `official:<name>` and
`aur:<name>` can be used explicitly. Exclusions persist across capture, do not
produce drift, and are shown as skipped by `check` and restore dry-runs.

Put an excluded package back into the managed set with:

```sh
omarchy-blueprint --profile ~/omarchy-profile include aur:dislocker-git
```

Restore journals are written to
`$XDG_STATE_HOME/omarchy-blueprint/restores/`, falling back to
`~/.local/state/omarchy-blueprint/restores/`.

## Current boundaries

This milestone does not yet distinguish packages added by the user from other
explicit packages. Theme restore installs only missing themes. An existing
theme with different provenance or content is reported as a conflict and is
never overwritten automatically. Internal symlinks and special files are
rejected during local-theme capture. The TUI, plugins, shell configuration,
directories, Git automation, migrations, and AI remain postponed.

Capture and inspect theme state explicitly with:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture themes
omarchy-blueprint --profile ~/omarchy-profile status themes
omarchy-blueprint --profile ~/omarchy-profile restore themes --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore themes
```

## Exit codes

- `0`: success or state matches
- `1`: command, validation, or environment error
- `2`: drift detected by `status` or `diff`

## Safety

- Restore previews are non-mutating.
- Apply requires confirmation or `--yes`.
- Commands are executed without a shell.
- Local theme snapshots exclude Git internals and reject internal symlinks.
- Theme restore never overwrites an existing user theme directory.
- Additional packages are never removed.
- Known GPU-driver, CPU-microcode, and fingerprint-stack packages are recorded
  as machine-specific and skipped during portable restore.
- Missing native packages and missing AUR packages are each installed in a
  single transaction.
- Interactive restores print an immediate operation message and an elapsed-time
  heartbeat every five seconds while package tools are running.
- AUR packages restore as separate operations. Failures are journaled, later
  packages continue, and the final summary lists successes and failures.
- Installed package names satisfy the profile regardless of whether a machine
  currently classifies them as official or foreign/AUR; captured provenance is
  used only when an installation is actually required.
- Packages already present as dependencies also satisfy the profile. Capture
  still records only explicitly installed packages, avoiding dependency noise.
- Additional explicit packages remain visible as status drift, but restore
  reports them as intentionally left installed because removal is disabled.
- Package installs are journaled and are not automatically rolled back.
