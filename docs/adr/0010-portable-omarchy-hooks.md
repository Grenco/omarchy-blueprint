# ADR 0010: Portable Omarchy Hooks

**Status:** Accepted  
**Date:** 2026-09-04

## Context

Omarchy supports user automation hooks under:

```text
~/.config/omarchy/hooks/<event>
~/.config/omarchy/hooks/<event>.d/<hook>
```

Current `omarchy-hook` runs the flat file first, then immediate regular files in the `.d/` directory. It invokes hook source through `bash`, skips `*.sample` entries in `.d/`, does not recurse, and does not require the executable bit at runtime.

Hooks are arbitrary code that may run automatically during boot, updates, theme changes, font changes, and other system events. Treating the Hooks directory as ordinary configuration would hide that risk and would not preserve permission semantics reliably through Git.

## Decision

Blueprint will add a first-class `hooks` provider in profile schema 5.

### Runtime state captured

The provider captures only runtime-relevant regular files:

```text
<event>
<event>.d/<hook>
```

It ignores nested directories, hidden `.d` children, and `.d/*.sample` files.

Unknown event names are allowed. Blueprint does not maintain an Omarchy event allowlist.

Runtime-relevant symlinks are rejected rather than followed.

### Profile representation

```text
hooks/
├── hooks.toml
└── files/<runtime-relative-path>
```

Each metadata item records:

```text
path
SHA-256 content hash
four-digit octal permission mode
```

Snapshot files are stored as inert mode `0644`. `hooks.toml` is authoritative for desired runtime mode because Git does not preserve arbitrary Unix permission bits.

### Restore behavior

Restore is additive and conservative.

- Missing desired hook: create atomically.
- Desired bytes and mode already present: no operation.
- Desired bytes present with wrong mode: safely repair using the file-write primitive.
- Different target bytes: skip; overwrite disabled.
- Extra target hook: leave installed; removal disabled.
- Symlink or special target: never follow or replace.

Every hook mutation is `RiskHigh`.

Blueprint never executes a hook during capture, restore, `check`, or verification.

### File restore primitive

`model.FileWrite` gains optional:

```go
Mode         *uint32
ExpectedMode *uint32
```

Existing providers leave both nil and retain their current behavior.

Hooks use `Mode` as the captured desired permission and `ExpectedMode` as a destination precondition for existing files. Content and mode are both rechecked at the final mutation boundary.

### Backups

Existing targets are backed up before replacement. The restore journal preserves previous bytes and permission bits.

### Secrets

Blueprint stores hook source verbatim. It does not claim to detect or redact secrets in arbitrary shell source. Users should reference external credential stores or environment variables rather than committing embedded secrets.

## Consequences

### Positive

- Hook behavior becomes portable and version-controlled.
- Unknown future Omarchy hook events remain capturable.
- Executable-code risk is visible in restore plans.
- Hook modes survive Git clones reliably.
- The mode-aware file primitive is reusable by future file-backed providers.
- Unknown target automation is never overwritten or removed.

### Negative

- A captured hook may represent temporary Omarchy-generated behavior because provenance is not inferred.
- Hook behavior is not functionally tested by Blueprint.
- Extra hooks remain drift after restore by design.
- Profiles containing hooks must be treated as executable source and reviewed accordingly.

## Deferred

- hook execution/linting;
- hook include/exclude policies;
- provenance classification;
- generic secret scanning;
- recursive arbitrary directories;
- automatic removal;
- rollback commands;
- TUI integration.
