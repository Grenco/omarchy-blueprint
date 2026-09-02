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
```

Machine-specific entries retain provenance, for example
`official:nvidia-open` or `aur:nvidia-580xx-dkms`.

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
- Known GPU-driver and CPU-microcode packages are recorded as machine-specific
  and skipped during portable restore.
- Missing native packages and missing AUR packages are each installed in a
  single transaction.
- Package installs are journaled and are not automatically rolled back.
