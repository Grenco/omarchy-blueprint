# Dirty Git State v2 Design

**Project:** Omarchy Blueprint
**Status:** Approved design
**Date:** 2026-09-10
**Milestone:** Schema 10 — Git Resource Policies / Dirty Git State v2
**Companion:** `docs/adr/0015-dirty-git-resource-policies.md`
**Builds on:** `docs/adr/0012-portable-resources.md` and `docs/superpowers/specs/2026-09-08-portable-resources-design.md`

## 1. Summary

Portable Resources v1 established a provenance-first model:

- ordinary files/directories are copied into the profile;
- Git repositories are reconstructed from a portable origin and exact HEAD;
- symlinks are represented semantically;
- unknown target work is preserved;
- dirty Git working state is deliberately omitted.

Dirty Git State v2 turns that omission into an explicit policy choice rather
than a fixed limitation.

A tracked Resource has one of three reconstruction strategies:

```text
git
    authoritative state = portable remote + exact HEAD
    local working state = unmanaged

git+diff
    authoritative state = portable remote + exact HEAD
    local working state = managed overlay

copy
    authoritative state = captured filesystem content
    Git provenance = unmanaged
```

The feature is intentionally about **resource reconstruction policy**, not a
miniature Git client. Blueprint continues to pin exact revisions, never pushes
or commits repository state, never forces patches through conflicts, and never
mutates a differing existing repository automatically.

The future TUI should be able to render the same three-state strategy choice
without introducing new persistence semantics.

## 2. Why this is the next Resources slice

The ROADMAP already describes dirty repository preservation using tracked
patches plus selected untracked files and shows `resources/git-state/` as the
intended profile storage. ADR 0012 records dirty Git state as an explicit
Resources v1 limitation.

Finishing this before machine path mappings gives path mapping a stable Resource
model. Finishing it before the TUI gives the UI a settled policy/data model to
present rather than making policy choices in the frontend.

Recommended sequence:

```text
Portable Resources v1    complete
Config v2                 complete
        ↓
Dirty Git State v2        this design
        ↓
resource path mappings / machine overlays
        ↓
TUI resource discovery and strategy selection
```

## 3. Goals

Dirty Git State v2 must:

1. Add explicit `git`, `git+diff`, and `copy` Resource strategies.
2. Keep `git` as the automatic default for a portable Git worktree.
3. Make dirty state under `git` informational rather than ordinary drift.
4. Preserve tracked staged changes separately from tracked unstaged changes
   under `git+diff`.
5. Preserve only explicitly selected untracked regular files.
6. Keep ignored files excluded from `git+diff` in this milestone.
7. Preserve exact remote + HEAD provenance for both Git strategies.
8. Restore a missing `git+diff` resource by clone → checkout → index patch →
   worktree patch → selected untracked files → verification.
9. Verify profile patch/untracked artifacts before using them during restore.
10. Refuse unresolved/conflicted Git states Blueprint cannot faithfully replay.
11. Refuse high-confidence secrets in local state before profile persistence.
12. Bound persisted Git-state payloads so `git+diff` cannot accidentally become
    a giant application/data backup.
13. Allow strategy changes for an already tracked exact resource path without
    requiring untrack/retrack.
14. Keep strategy changes atomic and non-mutating to the live repository.
15. Keep existing Resource ownership/link semantics intact.
16. Use schema 10 so older binaries cannot silently discard new strategy state.
17. Expose enough structured current-state metadata for a later TUI to present
    strategy and untracked-selection decisions.

## 4. Non-goals

This milestone does **not** implement:

- branch-follow restore policy;
- automatic pull/fetch/update of existing repositories;
- automatic application of saved local state to an existing differing checkout;
- destructive replacement of an existing Resource destination;
- merge/rebase/cherry-pick/revert continuation;
- preservation of unresolved index conflicts;
- dirty submodule preservation;
- Git bundle generation;
- synthetic commits;
- automatic commit/push of either profile or resource repositories;
- Git LFS-specific object preservation;
- recursive untracked-directory selection;
- untracked glob policy;
- ignored-file capture;
- path mappings or machine overlays;
- broad storage estimation UI;
- TUI implementation.

Existing Git checkout behavior for submodules remains whatever normal Git clone
and checkout already provide; this milestone does not add submodule
reconstruction policy.

## 5. Strategy semantics

### 5.1 `git`

`git` means:

> Reconstruct this Resource from its portable Git origin at this exact commit.
> Do not manage index, working-tree, or untracked state.

Persisted desired fields:

```text
ID
Path
Kind=directory
Strategy=git
Remote
Branch (informational)
Revision (authoritative)
```

`Dirty` remains runtime discovery metadata and is not profile intent.

Satisfaction:

```text
same portable remote
AND same HEAD revision
→ satisfied
```

This remains satisfied even when the current repository has staged, unstaged,
or untracked work.

Local work may be shown as informational output:

```text
dotfiles
  HEAD: abc123
  3 tracked local changes
  2 untracked files
  Capture policy: git
  Local working state is not managed.
```

Informational dirtiness does not create a `model.Change`, does not make
verification fail, and does not cause status/diff exit code 2 by itself.

Remote or HEAD mismatch remains managed Resource drift.

### 5.2 `git+diff`

`git+diff` means:

> Reconstruct this Resource from its portable Git origin at this exact commit,
> then replay the explicitly captured local Git state.

Persisted desired fields additionally describe:

```text
index patch hash
worktree patch hash
selected untracked files: path + hash + mode
```

Either patch may be absent when that layer is clean.

For `git+diff`, staged and unstaged tracked state are desired state. A current
tracked difference not represented by the saved overlay is Resource drift.

Only selected untracked paths are managed. Additional unselected untracked
files remain informational and do not make the Resource unsatisfied.

### 5.3 `copy`

`copy` retains Portable Resources v1 filesystem semantics. When the explicitly
selected path is a Git worktree, Git administrative entries named exactly
`.git` are excluded from the copied snapshot at any depth. `.gitignore`,
`.gitattributes`, and ordinary worktree files remain content.

This strategy means Git provenance is not authoritative. Restore reconstructs
filesystem content, not a repository checkout.

## 6. Automatic strategy selection

`track <path>` without `--strategy` preserves v1 behavior:

```text
supported directory
  ↓
valid worktree root + portable origin + valid HEAD
  → git

otherwise supported file/directory
  → copy
```

A dirty portable Git worktree still selects `git` by default. The command must
make omission visible:

```text
Tracked resource dotfiles using git.
Working tree has local changes; git policy does not capture them.
Use --strategy git+diff to preserve supported local state,
or --strategy copy to treat the worktree as filesystem content.
```

Blueprint never silently upgrades a dirty repository to `git+diff` or `copy`.

## 7. CLI

### 7.1 Track with strategy

```bash
omarchy-blueprint track ~/dotfiles --strategy git
omarchy-blueprint track ~/dev/hacky-repo --strategy git+diff
omarchy-blueprint track ~/some-local-tree --strategy copy
```

Accepted values are exactly:

```text
git
git+diff
copy
```

A regular-file Resource may only use `copy`.

`git` and `git+diff` require the selected directory itself to be a usable Git
worktree root with a portable origin and valid HEAD.

### 7.2 Updating an existing Resource policy

Passing the exact path of an already tracked Resource updates that Resource
rather than failing the overlap check:

```bash
omarchy-blueprint track ~/dotfiles --strategy git+diff
```

Requirements:

- the logical/absolute root must equal one existing Resource root exactly;
- the existing ID is retained;
- `--id` must be omitted or equal the existing ID;
- another overlapping-but-not-equal Resource root still fails;
- capture of the new strategy is staged completely before saved metadata or
  old snapshots are replaced.

This is intentionally an idempotent policy-setting use of `track`; it avoids a
separate pre-TUI `resource set-strategy` command.

### 7.3 Selecting untracked files

Only `git+diff` accepts untracked-selection flags:

```bash
omarchy-blueprint track ~/dev/hacky-repo \
  --strategy git+diff \
  --include-untracked notes.md \
  --include-untracked scripts/new-tool
```

For an already tracked `git+diff` resource:

```bash
omarchy-blueprint track ~/dev/hacky-repo \
  --include-untracked another-file \
  --exclude-untracked old-file
```

Both flags are repeatable. Values are exact repository-relative file paths, not
globs.

Normalization requirements:

- slash-separated repository-relative identity is persisted;
- path must be non-empty and clean;
- absolute paths are rejected;
- `.` / `..` traversal is rejected;
- any path containing a `.git` component is rejected;
- invalid UTF-8 paths are outside profile support;
- symlinks, directories, and special files are rejected;
- ignored files are rejected;
- the path must be untracked at the time it is newly included.

`--include-untracked` and `--exclude-untracked` without an effective
`git+diff` strategy are errors.

### 7.4 Selection across later captures

Saved selected untracked entries seed the next `capture resources` operation.

For a previously selected path:

```text
still untracked + eligible
→ recapture current bytes/mode

now tracked by Git
→ remove from selected-untracked metadata; Git layers own it

now absent
→ remove from selected-untracked metadata

now ignored
→ fail capture with a clear message; do not silently discard explicit intent

now symlink/directory/special/sensitive/oversized
→ fail capture; preserve previous profile state
```

A newly appearing untracked file is not selected automatically.

## 8. Schema 10

Profile schema increases:

```go
const Schema = 10
```

Add a Resource-provider introduction threshold for Git-state-v2 compatibility
if needed by loader validation, but existing schema introduction thresholds
must continue to refer to the schema where each provider first appeared.

Recommended model:

```go
type Resource struct {
    ID       string `json:"id" toml:"id"`
    Path     string `json:"path" toml:"path"`
    Kind     string `json:"kind" toml:"kind"`
    Strategy string `json:"strategy" toml:"strategy"`

    Hash string `json:"hash,omitempty" toml:"hash,omitempty"`
    Mode string `json:"mode,omitempty" toml:"mode,omitempty"`

    Remote   string `json:"remote,omitempty" toml:"remote,omitempty"`
    Branch   string `json:"branch,omitempty" toml:"branch,omitempty"`
    Revision string `json:"revision,omitempty" toml:"revision,omitempty"`

    IndexPatchHash    string             `json:"index_patch_hash,omitempty" toml:"index_patch_hash,omitempty"`
    WorktreePatchHash string             `json:"worktree_patch_hash,omitempty" toml:"worktree_patch_hash,omitempty"`
    Untracked         []GitUntrackedFile `json:"untracked,omitempty" toml:"untracked,omitempty"`

    // runtime-only detection metadata
    Dirty bool `json:"dirty,omitempty" toml:"-"`
}

type GitUntrackedFile struct {
    Path string `json:"path" toml:"path"`
    Hash string `json:"hash" toml:"hash"`
    Mode string `json:"mode" toml:"mode"`
}
```

Implementation may use a separate runtime `GitWorkingState` rather than adding
additional transient counts to `profile.Resource`; persisted semantics above
are normative, exact transient struct placement is not.

### 8.1 Strategy metadata invariants

`copy`:

```text
Hash + Mode required
Remote/Revision/patch hashes/Untracked empty
```

`git`:

```text
Remote + valid Revision required
Hash/Mode/patch hashes/Untracked empty
```

`git+diff`:

```text
Remote + valid Revision required
Hash/Mode empty
patch hashes optional independently
Untracked zero or more validated entries
```

Branch is optional/informational for both Git strategies.

Unknown strategies are validation errors.

### 8.2 Migration

Schema 9 → 10 is in-memory and non-destructive:

- existing `copy` Resources remain `copy`;
- existing `git` Resources remain `git`;
- no Git-state payload is invented;
- existing Resources snapshots/links remain unchanged.

A schema-9 binary must reject schema 10 rather than load/save and erase new
fields.

## 9. Profile storage

```text
resources/
├── resources.toml
├── files/
│   └── <copy-resource snapshots>
└── git-state/
    └── <resource-id>/
        ├── index.patch
        ├── worktree.patch
        └── untracked/
            └── <selected relative files>
```

Only `git+diff` Resources own entries below `resources/git-state/`.

Only non-empty patch artifacts are written. The corresponding hash field is
empty when a layer has no patch.

Untracked snapshot paths are:

```text
resources/git-state/<id>/untracked/<repo-relative-path>
```

Snapshots contain regular files only and use the exact content hash/mode stored
in `Resource.Untracked`.

A capture uses a staging directory and swaps `files/` and `git-state/`
transactionally so a failed capture cannot leave metadata referring to a
partially written overlay. Existing Resources staging/recovery conventions
should be strengthened if necessary rather than creating an unrelated second
transaction model.

## 10. Git inspection model

`DetectGitResource` should evolve from a single `Dirty bool` into enough
structured runtime state to support policy-aware status/capture.

Conceptually:

```go
type GitWorkingState struct {
    Remote   string
    Branch   string
    Revision string

    StagedTracked   int
    UnstagedTracked int
    Untracked       []string

    Conflicted bool
    Operation  string // "", merge, rebase, cherry-pick, revert
    DirtySubmodule bool
}
```

Exact struct naming is implementation detail. The behavior is not:

- distinguish staged tracked from unstaged tracked state;
- enumerate non-ignored untracked files deterministically;
- identify unresolved conflicts;
- identify active operations;
- identify dirty/changed submodule state;
- preserve existing portable-remote sanitization.

Use NUL-delimited Git output where paths are parsed.

## 11. Deterministic patch generation

For `git+diff`, generate two layers from the exact captured HEAD.

Index patch (HEAD → index):

```bash
git -C <repo> diff \
  --cached HEAD \
  --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --
```

Worktree patch (index → working tree):

```bash
git -C <repo> diff \
  --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --
```

The implementation should pass arguments directly, never via a shell.

`--no-renames` deliberately represents renames as delete/add so behavior does
not depend on user/system rename-detection configuration. Binary-capable full
index patches preserve ordinary binary tracked changes and executable-bit
changes supported by Git patches.

Environment/config that can alter patch semantics must be neutralized through
explicit flags or scoped `git -c` settings.

## 12. Unsupported Git states

`git+diff` capture must refuse:

### 12.1 Unmerged index

Any unresolved/unmerged path is unsupported.

Detection should use Git plumbing/status output, not parse human terminal text.

Error:

```text
resource <id> has unresolved Git conflicts; finish or abort the Git operation before capturing git+diff state
```

### 12.2 Active operations

Refuse active:

```text
merge
rebase
cherry-pick
revert
```

Use `git rev-parse --git-path ...` or equivalent so linked worktrees and
non-directory `.git` administrative layouts are handled correctly.

### 12.3 Submodules

A repository may contain clean submodules, subject to existing normal Git
checkout behavior. `git+diff` refuses:

- staged gitlink revision changes;
- modified/dirty submodule worktrees;
- untracked state inside submodules.

The user may capture such a repository with `git` (omitting local submodule
state) or choose `copy` when filesystem bytes are the desired authority.

## 13. Sensitive local-state scanning

Dirty Git v2 persists bytes that may never have been committed to the portable
remote, so it must use the same high-confidence safety posture as Config.

Extract the reusable secret-content logic behind a small shared package/service
rather than importing Config provider internals into Resources.

Required scanner capabilities:

```go
ScanReader(io.Reader) (sensitive bool, reason string, err error)
ScanRegularFile(path string, maxBytes int64) (...)
```

Exact API may differ, but Config and Resources must share the underlying
high-confidence signatures rather than maintaining divergent token/private-key
regexes.

### 13.1 What is scanned

Before persisting patches:

- enumerate staged changed paths and scan each desired stage-0 regular blob from
  the index;
- enumerate unstaged changed paths and scan each resulting regular working-tree
  file;
- apply sensitive-path checks to locally introduced/modified desired paths;
- scan each selected untracked regular file.

Deleted files require no desired-content scan.

Do **not** simply scan raw patch text for secrets. Patch context/deleted lines may
contain content already present in the authoritative remote and can create false
positives unrelated to the local desired state.

If a changed binary/opaque file cannot be safely classified by the shared
scanner and violates size limits, capture fails rather than storing it blindly.

## 14. Size and storage limits

`git+diff` is for portable working state, not large-data backup.

Hard initial limits:

```text
maximum selected untracked regular file: 16 MiB
maximum total persisted git-state payload per Resource: 128 MiB
```

The aggregate includes:

```text
index.patch bytes
+ worktree.patch bytes
+ selected untracked snapshot bytes
```

Calculate the total before committing staged profile state.

On overflow:

```text
resource <id> local Git state would add <size> to the profile (limit 128 MiB); commit/split the work, select fewer untracked files, or use copy strategy intentionally
```

Future storage-estimation/TUI work may make these limits configurable or more
interactive. This milestone uses fixed conservative values.

## 15. Capture semantics

### 15.1 `git`

Capture refreshes:

```text
portable remote
branch metadata
exact HEAD
```

It does not write `resources/git-state/<id>`.

If local working state exists, capture may print an informational omission
summary but must not report that omission as a captured profile change.

Recapturing a `git` Resource that previously used `git+diff` removes its
persisted Git-state overlay only as part of a successful strategy transition.

### 15.2 `git+diff`

Capture flow:

```text
resolve exact Resource root
→ validate portable Git provenance
→ reject unsupported Git operation/conflict/submodule state
→ resolve saved + requested untracked selection
→ validate selected paths
→ scan desired local content for high-confidence secrets
→ generate deterministic index/worktree patches
→ stage selected untracked regular files
→ enforce per-file + aggregate size limits
→ hash staged patch/untracked artifacts
→ stage updated resources.toml state
→ atomically replace Resources snapshots/state
```

The captured `Revision` is the current HEAD even when local patches exist.

### 15.3 `copy`

When the Resource root is a Git worktree and strategy is explicitly `copy`,
snapshot traversal omits entries whose path component is exactly `.git`.
Symlink and sensitive-resource rules from Resources v1 continue to apply.

Switching to `copy` clears Remote/Branch/Revision and Git-state metadata only
after the new copy snapshot has staged successfully.

## 16. Strategy transitions

### 16.1 Same-strategy recapture

`capture resources` preserves each Resource's saved strategy. Automatic
re-detection never changes strategy after registration.

This matters when:

- a `copy` Resource later becomes a Git repository;
- a `git` Resource becomes dirty;
- a `git+diff` Resource becomes clean.

Strategy changes require explicit `track <same-path> --strategy ...`.

### 16.2 `git` → `git+diff`

Capture current supported dirty state and selected untracked files.

If capture fails safety validation, retain the previous `git` definition.

### 16.3 `git+diff` → `git`

Capture current remote + HEAD and intentionally drop Git-state overlay and
untracked metadata. The command output should say local-state capture was
disabled and profile overlay bytes removed.

Live repository state is untouched.

### 16.4 Git strategy → `copy`

Stage a safe copy snapshot excluding `.git` administrative entries, then clear
Git provenance/overlay metadata.

### 16.5 `copy` → Git strategy

Require the live Resource root to be a usable portable Git worktree. Stage the
new Git metadata/overlay first, then remove the copied snapshot.

## 17. Diff and status semantics

Resource drift is policy-aware.

### 17.1 `git`

Managed comparison:

```text
saved Remote vs current Remote
saved Revision vs current HEAD
```

Local working-state differences are informational only.

Example:

```text
Resources
  dotfiles       matches pinned Git revision

Git working state not managed
  dotfiles       2 staged, 1 unstaged, 3 untracked
  Capture policy: git
```

No exit-code-2 drift solely from that section.

### 17.2 `git+diff`

Managed comparison includes:

```text
portable remote
HEAD revision
index patch state
worktree patch state
selected untracked file bytes/modes
```

Examples of drift:

```text
~ resource dotfiles staged Git state differs
~ resource dotfiles unstaged Git state differs
~ resource dotfiles untracked:notes.md differs
- resource dotfiles untracked:scripts/new-tool missing
```

Additional unselected untracked files are informational:

```text
Git working state not managed
  dotfiles       2 additional untracked files
```

### 17.3 Human `diff`

`diff resources` should show enough detail to explain policy decisions without
dumping patch bodies by default.

Suggested form:

```text
dotfiles
  strategy: git+diff
  HEAD:     abc123

  staged tracked changes:   3
  unstaged tracked changes: 1
  selected untracked:       2
  additional untracked:     4 (not managed)
```

Raw patch display is not required in this milestone.

### 17.4 JSON

Machine-readable Resources status should expose current Git working-state
summary sufficient for the future TUI:

```json
{
  "id": "dotfiles",
  "strategy": "git+diff",
  "remote": "github.com/Grenco/dotfiles",
  "revision": "abc123...",
  "git_working_state": {
    "staged_tracked": 3,
    "unstaged_tracked": 1,
    "untracked": ["notes.md", "tmp.txt"],
    "selected_untracked": ["notes.md"]
  }
}
```

Exact envelope placement should follow existing app JSON conventions. Do not
persist runtime-only counts to `resources.toml`.

## 18. Verification semantics

### 18.1 `git`

Verification succeeds when current portable remote + HEAD equal the saved
values, regardless of local working state.

### 18.2 `git+diff`

Verification succeeds when:

- portable remote matches;
- HEAD matches saved Revision;
- deterministic current index patch hash equals saved IndexPatchHash (both
  empty is equal);
- deterministic current worktree patch hash equals saved WorktreePatchHash
  (both empty is equal);
- every selected untracked file exists as a regular file with saved hash/mode;
- a selected untracked path has not become tracked/ignored/unsupported.

Additional unselected untracked files do not fail verification.

### 18.3 Profile `check`

`check` validates profile artifacts without depending on live Resource state:

- patch files are regular files;
- patch hash equals metadata;
- absent patch file corresponds to empty hash;
- selected untracked snapshots are regular files;
- snapshot hash/mode metadata is complete;
- no snapshot path escapes `git-state/<id>/untracked`;
- strategy metadata invariants hold;
- Git executable is available when any Git strategy is captured;
- existing GitHub CLI requirement for GitHub remotes remains unchanged unless
  Resources clone behavior is separately changed.

## 19. Restore planning

Existing destination policy remains conservative.

### 19.1 Missing `git`

Unchanged from Resources v1:

```text
create parent if required
→ clone --no-checkout
→ checkout --detach exact revision
```

### 19.2 Missing `git+diff`

Plan operations in this dependency order:

```text
resources.git.clone.<id>
        ↓
resources.git.checkout.<id>
        ↓
resources.git.apply-index.<id>       (if index patch exists)
        ↓
resources.git.apply-worktree.<id>    (if worktree patch exists)
        ↓
resources.git.untracked.<id>.<path>  (selected files)
        ↓
link operations depending on the resource
```

When index patch is absent, worktree apply depends directly on checkout. When
both patches are absent, selected untracked writes depend on checkout.

A Resource is only `Satisfied: true` for dependent links after the complete
Resource reconstruction chain succeeds.

### 19.3 Existing destination

If current state already exactly satisfies the saved Resource, no operation is
needed.

Otherwise, preserve Resources v1 safety:

```text
existing differing resource
→ skip/conflict
→ no automatic mutation
```

Even an existing clean checkout at the correct remote + HEAD but missing a
saved `git+diff` overlay is considered differing and is not patched in place in
this milestone.

This intentionally leaves the roadmap's future choice:

```text
Apply saved local state only
```

for a later explicit compatible-repository workflow/TUI design.

## 20. Hash-validated Git patch operation

Do not model patch application as an unchecked generic shell/command string.

Add a typed restore operation (exact names may follow existing model style),
conceptually:

```go
type GitPatchApply struct {
    Repository string `json:"repository"`
    Source     string `json:"source"`
    SourceHash string `json:"source_hash"`
    ToIndex    bool   `json:"to_index"`
}
```

Executor requirements:

1. validate Source is a regular file under the active profile;
2. hash Source and compare to SourceHash immediately before use;
3. reject symlinked source/unsafe repository parents using existing content
   safety helpers where applicable;
4. invoke Git directly without a shell;
5. for index patches, use `git apply --index`;
6. for worktree patches, use ordinary `git apply`;
7. normalize whitespace behavior explicitly so user Git config does not change
   pass/fail semantics;
8. propagate non-zero Git apply as operation failure;
9. never use `--3way`, `--reject`, or force-style fallback.

The exact pinned HEAD plus dependency ordering is the base precondition. The
index patch is applied before the worktree patch so staged-vs-unstaged intent is
reconstructed.

## 21. Untracked restore

Selected untracked files use existing hash-validated filesystem write
operations where possible.

Requirements:

- source snapshot hash is validated;
- destination is repository path + safe relative path;
- symlinked destination parents are rejected;
- destination is expected missing after clone/checkout;
- recorded mode is restored;
- no directory is recursively restored as one untracked selection.

Because restore into existing differing repositories is not performed in this
milestone, selected-untracked collisions should only indicate profile/repository
inconsistency and must fail safely.

## 22. Links and ownership

Resource identity and path remain unchanged by strategy selection. Existing
`ResourceLink` relationships therefore continue to target Resource ID +
relative path.

Dependency rule:

```text
all reconstruction operations for target Resource
→ complete
→ inbound/internal Resource links may be created
```

A tracked path cannot change strategy to escape stronger provider ownership.
The existing ownership collision rules still run when initially registering a
Resource.

Git-owned symlinks within tracked Git content continue to be owned by Git; they
are not duplicated as ResourceLink entries. A tracked symlink change represented
by a Git patch remains part of the Git overlay.

## 23. Error handling and atomicity

### 23.1 Capture

All strategy capture/update operations are transactional with respect to the
profile:

```text
inspect + validate
→ build complete staging tree
→ validate hashes/limits
→ swap staged snapshots/state
→ save metadata
```

A failure leaves previous profile metadata and previous valid snapshots usable.
If current Resources capture sequencing cannot guarantee metadata + artifact
atomicity together, fix the Resources transaction boundary as part of this
milestone rather than accepting cross-file corruption.

### 23.2 Restore

Restore failures follow the normal operation journal.

If clone/checkout succeeds but later patch application fails, the newly created
resource directory may remain as partial restore output and the journal records
the failed operation. Blueprint must not attempt an unreviewed rollback that
could itself destroy data.

Verification must not report success after any failed Git-state operation.

## 24. Human workflow examples

### 24.1 Safe default

```bash
omarchy-blueprint track ~/dotfiles
```

Output when dirty:

```text
Tracked portable resource dotfiles
  ~/dotfiles
  strategy: git
  remote: github.com/user/dotfiles
  revision: abc123

Local Git state is not captured by strategy git:
  2 staged tracked files
  1 unstaged tracked file
  3 untracked files

Use --strategy git+diff to preserve supported local state,
or --strategy copy to capture worktree files as filesystem content.
```

### 24.2 Preserve local work

```bash
omarchy-blueprint track ~/dotfiles \
  --strategy git+diff \
  --include-untracked notes.md
```

Result:

```text
Updated portable resource dotfiles
  strategy: git → git+diff
  captured staged tracked changes: 2
  captured unstaged tracked changes: 1
  captured selected untracked files: 1
  untracked files left unmanaged: 2
```

### 24.3 Drop local-state ownership later

```bash
omarchy-blueprint track ~/dotfiles --strategy git
```

Result:

```text
Updated portable resource dotfiles
  strategy: git+diff → git
  removed captured Git working-state overlay
  live repository was not modified
```

## 25. Future TUI mapping

The TUI should consume this model rather than add UI-specific policy.

Conceptual Resource panel:

```text
dotfiles
~/dotfiles

Reconstruction strategy
  ● Git remote + commit
  ○ Git + local changes
  ○ Copy filesystem

Current local state
  staged tracked       2
  unstaged tracked     1
  untracked             3

Untracked files
  [x] notes.md
  [ ] tmp/output.log
  [ ] scratch.txt
```

Changing the radio choice maps to `Resource.Strategy`. Checkboxes map to the
same selected-untracked metadata manipulated by CLI flags.

The TUI may later add confirmation flows for ignored files, large overlays,
existing compatible repositories, or path mappings. Those flows must not change
this milestone's persisted strategy meanings.

## 26. Testing strategy

### 26.1 Git fixture helper

Use real temporary Git repositories in provider tests where practical. Configure
local author identity per fixture and create a bare local origin or injectable
runner fixture appropriate to the test layer. Do not depend on network access.

Test repositories should cover:

```text
clean HEAD
staged text modification
unstaged text modification
same file staged then further modified
staged new file
staged deletion
binary tracked modification
mode change
untracked regular file
ignored file
unresolved conflict
active operation
submodule dirty state
```

### 26.2 Strategy behavior

Required tests:

```text
dirty repo + strategy git + same remote/HEAD
→ satisfied
→ informational dirtiness only

dirty repo + strategy git+diff
→ deterministic patch hashes + selected untracked snapshots
→ recapture unchanged is idempotent

extra unselected untracked file under git+diff
→ informational only
→ not drift

managed tracked/untracked difference under git+diff
→ drift
```

### 26.3 Layer reconstruction

Build source fixture:

```text
HEAD file = A
index file = B
working tree file = C
```

Capture `git+diff`, restore into a missing destination, then assert:

```text
HEAD == captured Revision
index version == B
working-tree version == C
```

Use Git plumbing (`git show :path`, `git diff --cached`, `git diff`) rather than
only reading the worktree file.

### 26.4 Untracked selection

Test:

```text
selected regular untracked file
→ persisted + restored

unselected untracked file
→ not persisted
→ informational only

selected becomes tracked
→ removed from untracked metadata on recapture

selected disappears
→ removed from untracked metadata on recapture

selected becomes ignored
→ recapture fails, previous profile preserved

selected symlink/directory/FIFO
→ rejected

selected sensitive file/content
→ rejected
```

### 26.5 Strategy transitions

Test every supported transition:

```text
git ↔ git+diff
git → copy
git+diff → copy
copy → git
copy → git+diff
```

Verify obsolete profile artifacts are removed only after new capture succeeds
and live worktree bytes are untouched.

### 26.6 Restore safety

Test:

```text
missing git+diff destination
→ clone + checkout + apply + untracked writes

existing exact desired git+diff destination
→ no operations

existing clean same-HEAD repo missing overlay
→ conflict/skip, no mutation

existing dirty/different repo
→ conflict/skip, no mutation

tampered index.patch/worktree.patch
→ check/restore refuses before git apply

patch apply failure
→ later dependent operations do not run
→ verification fails
```

### 26.7 Schema

Test schema 9 → 10 loading and schema-10 round-trip with:

- `git`;
- `git+diff` with both patches and selected untracked metadata;
- `copy`.

Older-schema fixtures must continue to load without fabricated Git-state data.

## 27. Manual acceptance

On a disposable real Git repository with a portable remote:

1. Track dirty repo without strategy and confirm `git` is selected and local
   state is explicitly reported as omitted.
2. Confirm `status resources` is clean when remote/HEAD match despite dirtiness.
3. Switch to `git+diff`; select one untracked regular file and capture.
4. Confirm `resources/git-state/<id>/` contains deterministic patch files as
   applicable and only the selected untracked file.
5. Re-run capture without edits and confirm no profile diff.
6. Make an additional unselected untracked file and confirm it is informational,
   not drift.
7. Modify a managed tracked file and confirm Resources drift.
8. Restore onto a machine/path where the Resource is missing; confirm exact HEAD,
   staged state, unstaged state, selected untracked bytes/modes, and dependent
   links are reconstructed.
9. Confirm an existing differing checkout is skipped rather than mutated.
10. Switch the source resource back to `git`; capture and confirm `git-state/<id>`
    is removed from the profile while live local changes remain untouched.
11. Switch to `copy`; confirm `.git` administrative state is not copied.

## 28. Documentation updates at implementation time

Implementation should update:

- `README.md` Resource workflow and strategy examples;
- `docs/manual-test-checklist.md` with Dirty Git v2 acceptance;
- ADR 0012 consequences/non-goals with a pointer to ADR 0015;
- Portable Resources v1 design with a short supersession note for dirty-Git
  behavior rather than rewriting its historical v1 decisions;
- ROADMAP only where necessary to mark dirty repository preservation delivered,
  without moving unrelated phases.

## 29. Acceptance criteria

Dirty Git State v2 is complete when all of the following are true:

1. `track --strategy git|git+diff|copy` is stable and profile-persistent.
2. Existing exact tracked paths can change strategy atomically.
3. `git` ignores local working-state drift by design while still reporting it
   informationally.
4. `git+diff` preserves staged and unstaged tracked state as separate patch
   layers.
5. Only explicitly selected, safe, non-ignored untracked regular files are
   captured.
6. Unsupported Git operation/conflict/submodule states fail capture without
   changing the previous profile.
7. High-confidence secrets and oversized local state are rejected before
   persistence.
8. Profile patch/untracked artifacts are hash-validated.
9. Missing-resource restore recreates remote + exact HEAD + index + worktree +
   selected untracked files in dependency order.
10. Existing differing repositories remain untouched.
11. Resource verification is strategy-aware and policy-consistent.
12. Schema 9 profiles migrate safely to schema 10; schema-10 state cannot be
    silently discarded by older binaries.
13. Existing Resources links/ownership/copy/Git v1 behavior remains green.
14. Full project tests, vet, build, and diff checks pass.
