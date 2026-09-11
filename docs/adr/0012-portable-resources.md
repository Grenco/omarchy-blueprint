# ADR 0012: Portable Resources and Semantic Symlink Ownership

## Status

Accepted

## Context

Omarchy Blueprint can already reproduce semantic Omarchy state such as packages,
themes, plugins, configuration, defaults, Shell state, hooks, and global Mise
packages. Real user setups also rely on external dotfiles/setup repositories and
ordinary user files.

A common pattern is:

```text
~/.config/hypr/overrides.lua
    -> ~/dotfiles/hypr/overrides.lua
```

or:

```text
~/.config/omarchy/hooks/theme-set
    -> ~/omarchy-setup/hooks/omarchy-theme-hook.sh
```

Existing semantic providers intentionally do not dereference arbitrary external
symlinks. Blueprint therefore needs a provider that can own the authoritative
resource and its link relationship without making Config or Hooks absorb the
target bytes.

## Decision

Add a schema-7 `resources` provider.

An explicitly tracked resource is reconstructed with either:

- copied profile content for ordinary files/directories; or
- Git remote + exact captured revision for clean Git repositories.

Symlinks in copied resources are represented as semantic relationships:

```text
source location
→ target resource ID
→ target path relative to that resource
```

They are not copied/dereferenced into resource snapshots.

When a resource is tracked or captured, Blueprint scans a bounded set of user
configuration locations for symlinks that resolve into already tracked
resources. Those inbound links are managed by default unless ignored.

The CLI never automatically tracks a new target resource merely because an
untracked symlink points to it. A future TUI may surface such edges as
user-approved tracking suggestions.

Restore reconstructs resources before dependent links and creates relative
symlinks calculated from the target machine's resource path.

A small provider-ownership claim index prevents Resources from taking over
paths another semantic provider owns. Providers such as Hooks may explicitly
delegate symlink-leaf ownership while retaining semantic ownership of regular
runtime files.

## Consequences

### Positive

- dotfiles/setup repositories become reproducible without copying generated
  symlink targets into unrelated providers;
- inbound `.config`/hook links can be reproduced automatically;
- resource IDs make future path remapping possible;
- Git remains the authoritative source when available;
- the same link scanner can power later TUI discovery;
- unknown target work remains protected.

### Negative

- schema/profile state becomes more complex;
- generic restore gains safe directory/symlink actions;
- copied resources containing external untracked symlinks cannot yet be fully
  captured;
- dirty Git working-tree/index changes were reported as drift but were not captured in Resources v1;
- some provider-owned paths may require explicit ownership handoff before a
  symlink can be Resources-managed.

The v1 dirty-Git limitation is superseded for schema-10 profiles by
[ADR 0015](0015-dirty-git-resource-policies.md). `git` now treats local state
as informational, while `git+diff` preserves supported local state explicitly.

Schema-11 machine path mappings use this Resource-ID link indirection to change
Resource placement without rewriting semantic links; see
[ADR 0016](0016-machine-overlay-resource-path-mappings.md).

## Rejected alternatives

- Explicit-only symlink registration: too manual for dotfiles.
- Recursive automatic tracking of symlink targets: violates selectivity.
- Whole-directory copying for Git repositories: duplicates authoritative state.
- Absolute symlink preservation: prevents portable path remapping.
