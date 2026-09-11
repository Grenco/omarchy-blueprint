# ADR 0015: Git Resource Policies and Dirty Working-State Preservation

## Status

Accepted

## Delivery

Delivered in schema 10. The implementation provides the three strategies,
transactional Git-state artifacts, strategy-aware status/check behavior, and
missing-resource reconstruction described below. Path mappings, a TUI workflow,
and in-place overlay application to an existing differing checkout remain out
of scope.

## Context

ADR 0012 introduced Portable Resources with two reconstruction strategies:

- `copy` for ordinary files and directories; and
- `git` for Git worktrees reconstructed from a portable remote plus an exact
  captured revision.

Resources v1 deliberately leaves dirty Git working-tree and index state outside
the profile. A dirty repository may still be tracked as Git, but staged,
unstaged, and untracked work is omitted. That is safe, but it leaves an
important reconstruction gap for user-owned dotfiles/setup repositories and
other intentionally portable working repositories.

The roadmap already anticipates dirty repository preservation as part of
Resources coverage: tracked changes may be stored as patches, selected
untracked files may be copied separately, and restore should reconstruct the
repository before replaying local state.

A single Git reconstruction policy is not sufficient. Different repositories
have different desired semantics:

- some should be reconstructed strictly from their authoritative remote and
  pinned commit;
- some should additionally preserve intentionally unfinished local work; and
- some should be treated as filesystem content even though they happen to be
  Git worktrees.

The future TUI also needs a stable core policy model to present rather than
inventing UI-only behavior.

## Decision

Portable Resources gains three explicit strategies:

```text
git
    remote + exact HEAD
    working-tree/index state intentionally unmanaged

git+diff
    remote + exact HEAD
    staged tracked changes preserved
    unstaged tracked changes preserved
    explicitly selected untracked regular files preserved

copy
    filesystem content is authoritative
    Git provenance is not authoritative for restore
```

`git` remains the automatic and safest Git default. A dirty worktree under
`git` is informational only when remote and HEAD match the profile; it is not
ordinary resource drift merely because local changes exist.

`git+diff` stores local tracked state as two deterministic binary-capable patch
layers:

```text
HEAD
  ↓
index.patch       HEAD → index
  ↓
worktree.patch    index → working tree
```

Selected untracked files are stored separately with their path, content hash,
and mode. Untracked files are never captured automatically and ignored files
are not selectable in this milestone.

Restore for a missing `git+diff` resource is:

```text
clone portable remote
→ checkout exact revision
→ apply index patch to index + worktree
→ apply worktree patch to worktree only
→ restore selected untracked files
→ verify
```

Patch application is exact-base, not three-way. If a patch cannot be applied to
the pinned revision, restore fails safely rather than guessing or forcing.

The profile schema increases from 9 to 10 so older binaries cannot silently
discard `git+diff` strategy state or untracked-file intent.

Explicit `copy` on a Git worktree captures worktree content while excluding Git
administrative `.git` entries. `.gitignore`, `.gitattributes`, and ordinary
worktree files remain content.

Existing-destination safety remains conservative in this milestone. A missing
Git resource may be fully reconstructed. An existing repository that already
matches the complete desired `git+diff` state is satisfied. An existing
repository that differs is not mutated automatically, even if it has the same
remote and HEAD; compatible in-place overlay application remains a later
explicit workflow/TUI decision.

## Safety constraints

`git+diff` capture refuses states Blueprint cannot faithfully and safely replay:

- unresolved index conflicts;
- active merge, rebase, cherry-pick, or revert operations;
- dirty or changed submodule state;
- non-portable/missing origin remotes;
- invalid or missing HEAD revisions;
- selected untracked symlinks, directories, special files, ignored files, or
  paths outside the repository;
- high-confidence sensitive paths/content in local state;
- an individual selected untracked file larger than 16 MiB; or
- total persisted Git-state payload for one resource larger than 128 MiB.

Sensitive-content inspection applies to the desired local content represented
by staged/unstaged changes and selected untracked files. Blueprint does not
reject a patch merely because unchanged or removed context from the
already-authoritative remote happens to contain a token-like string.

Patch files stored in the profile are hash-validated before restore. Applying a
profile patch must use a typed restore operation or equivalent executor path
that validates the local patch artifact before invoking Git; raw unchecked
command execution is insufficient.

## CLI consequences

Tracking accepts an explicit strategy:

```bash
omarchy-blueprint track ~/dotfiles --strategy git
omarchy-blueprint track ~/dev/hacky-repo --strategy git+diff
omarchy-blueprint track ~/some-local-tree --strategy copy
```

Omitting `--strategy` preserves Resources v1 automatic behavior: a portable Git
worktree uses `git`; other supported resources use `copy`.

For `git+diff`, exact repository-relative untracked files may be selected:

```bash
omarchy-blueprint track ~/dev/hacky-repo \
  --strategy git+diff \
  --include-untracked notes.md \
  --include-untracked scripts/new-tool
```

An already tracked exact resource path may be passed to `track` again to update
its strategy and/or untracked selection atomically. A different overlapping
resource path remains an error. `--id` when updating an existing path must be
empty or equal to the existing resource ID.

Untracked selection can be changed with repeated exact-path flags:

```bash
omarchy-blueprint track ~/dev/hacky-repo \
  --include-untracked another-file \
  --exclude-untracked old-file
```

These flags are valid only for an effective `git+diff` strategy. Globs and
recursive untracked-directory selection are not introduced here.

## Status and drift consequences

For `git`, desired state is only portable remote + pinned revision. A matching
repository with local staged/unstaged/untracked work remains satisfied. Human
status may report omitted work as informational metadata, and JSON should expose
that summary, but it does not produce exit-code-2 drift by itself.

For `git+diff`, tracked index/worktree state and selected untracked files are
desired state. Differences in those managed layers are ordinary Resource drift.
Additional unselected untracked files are informational only.

Branch attachment remains informational for both Git strategies; exact HEAD is
the restore intent in this milestone.

## Profile storage

Git-state artifacts live separately from copied resources:

```text
resources/
├── resources.toml
├── files/
│   └── ...
└── git-state/
    └── <resource-id>/
        ├── index.patch
        ├── worktree.patch
        └── untracked/
            └── <repository-relative paths>
```

Only non-empty patch files are persisted. `resources.toml` stores hashes for
persisted patches and path/hash/mode metadata for selected untracked files.

## Strategy transitions

Changing strategy is a profile capture operation and never mutates the live
resource.

```text
git → git+diff
    capture current safe local state

git+diff → git
    discard persisted git-state overlay and untracked selection

copy → git / git+diff
    require a valid portable Git worktree at the tracked root
    discard copied snapshot after successful staged capture

git / git+diff → copy
    snapshot worktree content excluding .git administrative entries
    clear Git provenance/overlay metadata after successful staged capture
```

All transitions are atomic with respect to profile state: failure leaves the
previous resource definition and profile snapshots intact.

## Consequences

### Positive

- dirty dotfiles/setup repositories become genuinely reconstructible;
- users choose explicit provenance semantics instead of Blueprint guessing;
- staged and unstaged intent survives cross-machine restore;
- selected untracked work can be preserved without turning every repository
  into a filesystem backup;
- `git` stops producing unavoidable drift for state it explicitly does not own;
- strategy policy maps directly to future TUI controls;
- Git remains the authoritative source whenever the user chooses a Git strategy.

### Negative

- schema/profile state becomes more complex;
- Resources needs deterministic Git patch generation and verification;
- restore gains a hash-validated Git patch operation;
- high-confidence secret scanning must be shared with Resources local-state
  capture;
- in-place restoration into an existing compatible repository remains limited
  until a later explicit conflict/update workflow is designed.

## Rejected alternatives

- **Always copy dirty repositories:** duplicates authoritative Git content and
  destroys provenance-first semantics.
- **Create synthetic commits:** rewrites/reinterprets user history and does not
  naturally preserve staged-vs-unstaged state or untracked files.
- **Store one flattened patch:** loses the index/worktree distinction.
- **Capture all untracked files:** violates selectivity and risks capturing
  credentials, build products, caches, and large generated data.
- **Treat dirtiness as drift under `git`:** contradicts the explicit policy that
  working-tree state is unmanaged.
- **Apply overlays automatically to any existing matching-remote checkout:**
  broadens destructive/conflict behavior beyond the existing Resources safety
  model and is deferred to a later workflow/TUI milestone.
