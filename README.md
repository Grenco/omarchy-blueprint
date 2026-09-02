# Omarchy State

Use your system normally. Omarchy State remembers how to rebuild it.

This repository currently contains the first packages-only vertical slice of
the broader design in [ROADMAP.md](ROADMAP.md). It captures all explicitly
installed native and foreign packages, shows semantic drift, creates a safe
restore plan, installs only missing packages through Omarchy, and verifies the
result. It never removes additional packages.

## Requirements

- Omarchy 4 or newer
- Go 1.25 or newer when building from source
- `pacman` and the public `omarchy` CLI on `PATH`

## Build and test

```sh
go test ./...
go vet ./...
go build ./cmd/omarchy-state
```

## First workflow

Create and capture a profile:

```sh
mkdir -p ~/omarchy-profile
omarchy-state init ~/omarchy-profile --name main
omarchy-state --profile ~/omarchy-profile capture
```

After cloning that profile on another Omarchy machine:

```sh
omarchy-state check
omarchy-state status
omarchy-state restore --dry-run
omarchy-state restore
```

The non-interactive form is `omarchy-state restore --yes`. Combine `--json`
with `--dry-run` or `--yes`; JSON restores never wait for a prompt.

Package profiles are human-readable:

```text
profile.toml
packages/official.txt
packages/aur.txt
```

Restore journals are written to
`$XDG_STATE_HOME/omarchy-state/restores/`, falling back to
`~/.local/state/omarchy-state/restores/`.

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
- Package installs are journaled and are not automatically rolled back.
