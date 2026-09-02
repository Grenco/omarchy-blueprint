# Omarchy Blueprint

Use your system normally. Omarchy Blueprint remembers how to rebuild it.

This repository currently contains the first packages-only vertical slice of
the broader design in [ROADMAP.md](ROADMAP.md). It captures explicitly
installed native and foreign packages, separates known machine-specific
hardware packages, shows semantic drift, creates a safe restore plan, installs
only missing portable packages through Omarchy, and verifies the result. It
never removes additional packages.

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

The non-interactive form is `omarchy-blueprint restore --yes`. Combine `--json`
with `--dry-run` or `--yes`; JSON restores never wait for a prompt.

Package profiles are human-readable:

```text
profile.toml
packages/official.txt
packages/aur.txt
packages/machine-specific.txt
packages/excluded.txt
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
explicit packages. It also intentionally postpones the TUI, themes, plugins,
shell configuration, directories, Git automation, migrations, and AI.

## Exit codes

- `0`: success or state matches
- `1`: command, validation, or environment error
- `2`: drift detected by `status` or `diff`

## Safety

- Restore previews are non-mutating.
- Apply requires confirmation or `--yes`.
- Commands are executed without a shell.
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
- Package installs are journaled and are not automatically rolled back.
