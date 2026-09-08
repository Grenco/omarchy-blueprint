# ADR 0011: Treat Global Mise Tools As Packages

**Status:** Accepted
**Date:** 2026-09-08

## Decision

Global Mise `[tools]` declarations are a third source in the existing Packages
provider alongside official Pacman and AUR packages. They are stored as
structured, normalized declarations in `packages/mise.toml` under schema 6.

Blueprint captures declared requests and tool options from the primary global
Mise config, not resolved install-cache versions. It excludes project-local
Mise files and global sections outside `[tools]`.

Restore is additive and conservative. Missing declarations are appended to the
existing global config only after validating an exact candidate; differing and
target-only declarations are preserved. A generated atomic write precedes
`mise -C / install` for added IDs. Mutations through config or parent symlinks
are refused.

Literal values under obvious secret keys are rejected during capture and check.
Declarations with `postinstall` remain portable intent but make the install
operation high risk.

## Consequences

- No new top-level Tools category is introduced.
- `mise:<id>` participates in package include/exclude policy.
- Mise IDs do not satisfy Pacman/AUR packages of the same name.
- Blueprint never rewrites unrelated global Mise configuration or removes
  target-only tools.
