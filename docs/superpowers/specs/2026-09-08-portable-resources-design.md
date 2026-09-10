# Portable Resources v1 Design

**Status:** Approved design  
**Date:** 2026-09-08  
**Project:** Omarchy Blueprint  
**Milestone:** Schema 7 — Portable Resources

> **Supersession note (schema 10+):** This document preserves the historical
> Resources v1 design. Its dirty-Git omission, clean-worktree detection, and
> dirty-repository failure statements are superseded by [Dirty Git State v2](2026-09-10-dirty-git-state-v2-design.md)
> and [ADR 0015](../../adr/0015-dirty-git-resource-policies.md). Other v1
> Resources decisions remain historical context unless separately superseded.

## 1. Summary

Omarchy Blueprint will add a first-class **Portable Resources** provider for user-selected files, directories, Git repositories, and the symlink relationships that connect those resources to the rest of the user's home directory.

The provider ID and CLI category are:

```text
resources
```

This supersedes the roadmap's earlier generic `directories` naming. Files, Git repositories, and symlinks are all first-class state, so `resources` is the clearer long-term boundary.

The primary workflow is:

```bash
omarchy-blueprint track ~/dotfiles
```

or:

```bash
omarchy-blueprint track ~/omarchy-setup
```

Blueprint inspects the selected path, chooses the safe v1 reconstruction strategy, captures its portable state, and then discovers symlinks that point **into** the newly tracked resource.

For example:

```text
~/omarchy-setup/
├── config/hyprland-overrides.lua
└── hooks/omarchy-theme-hook.sh

~/.config/hypr/overrides.lua
    -> ~/omarchy-setup/config/hyprland-overrides.lua

~/.config/omarchy/hooks/theme-set
    -> ~/omarchy-setup/hooks/omarchy-theme-hook.sh
```

Tracking `~/omarchy-setup` automatically causes both inbound links to become Portable Resources state.

The restore graph is:

```text
reconstruct resource
        ↓
verify desired resource state
        ↓
create resource-relative symlinks
        ↓
other providers consume those paths without owning the linked bytes
```

The central rule is:

> **Explicitly tracked resources may automatically own symlinks that point into them, but Blueprint never automatically expands an untracked symlink target into a new tracked resource in CLI v1.**

The later TUI can reuse the same discovery engine to offer the inverse workflow:

```text
Found:
~/.config/nvim -> ~/dotfiles/nvim

Target is inside untracked Git repository:
~/dotfiles

[Track dotfiles] [Ignore]
```

That future interactive suggestion does not change v1's deterministic ownership semantics.

## 2. Classification

This is an architectural change.

It introduces:

- a new provider and schema state;
- new CLI lifecycle commands;
- copy and Git reconstruction strategies;
- semantic symlink relationships;
- reverse symlink discovery;
- cross-resource dependency ordering;
- resource ownership claims shared with existing providers;
- new safe filesystem restore operations.

It should be implemented as one milestone but can be reviewed in multiple commits or PR slices.

## 3. Approaches Considered

### 3.1 Recommended: tracked resources own inbound links

The user explicitly tracks a resource root. Blueprint then scans a bounded user-configuration namespace for symlinks whose resolved targets are inside tracked resources.

Advantages:

- matches the desired dotfiles workflow;
- no repeated manual link registration;
- does not silently add unrelated repositories/directories;
- preserves the authoritative resource as the primary owner;
- naturally supports future path remapping because links target a resource ID plus relative path;
- provides the discovery data the TUI will later need.

This is the selected design.

### 3.2 Explicit symlink registration only

Every symlink would require a command such as:

```bash
omarchy-blueprint track-link ~/.config/hypr/overrides.lua
```

This is simple and safe but too manual for normal dotfiles usage. It also fails to make resource ownership feel semantic.

Rejected.

### 3.3 Recursive graph expansion

Tracking one resource would recursively follow symlink targets and automatically track any directories or repositories found on the other side.

Example:

```text
track ~/.config/nvim
        ↓
link points to ~/dotfiles/nvim
        ↓
automatically track ~/dotfiles
        ↓
dotfiles contains link to ~/Projects/foo
        ↓
automatically track ~/Projects/foo
```

This is powerful but violates selectivity and makes one CLI command capable of unexpectedly expanding profile scope.

Rejected for CLI v1.

The TUI may later offer graph-expansion **suggestions**, always requiring user choice.

## 4. Goals

Portable Resources v1 must:

1. Add a `resources` provider and schema-7 profile state.
2. Support explicit tracking of regular files.
3. Support explicit tracking of ordinary directories.
4. Support explicit tracking of clean Git repositories with portable remote provenance.
5. Preserve configured filesystem modes for copied files/directories.
6. Never follow symlinks while copying resource bytes.
7. Model portable symlink relationships semantically.
8. Automatically discover inbound symlinks pointing into tracked resources.
9. Automatically model symlinks inside copied resources when their targets belong to the same or another tracked resource.
10. Preserve Git-owned symlinks through Git rather than duplicating their ownership.
11. Restore resources before links that depend on them.
12. Create portable relative symlinks rather than source-machine absolute links.
13. Preserve unknown target work; no destructive replacement.
14. Detect path/resource collisions before mutation.
15. Prevent restore through symlinked parent directories.
16. Avoid capturing known-sensitive paths by default.
17. Provide `track`, `untrack`, and `tracked` workflows.
18. Participate in `capture`, `status`, `diff`, `restore`, and `check`.
19. Keep resource definitions human-readable and Git-friendly.
20. Prepare for later machine path mappings without implementing them yet.
21. Expose untracked symlink-target candidates to future UI code without auto-tracking them in CLI v1.

## 5. Non-goals

Portable Resources v1 does **not** implement:

- recursive `~/Projects` mixed-resource discovery;
- dirty Git patch preservation;
- untracked Git file preservation;
- Git LFS special handling;
- submodule reconstruction beyond whatever normal Git checkout already provides;
- branch-follow restore policy;
- automatic pushing or committing;
- machine path mappings;
- machine overlays;
- arbitrary path templating;
- profile storage estimation UI;
- automatic large-directory recommendations;
- project-local Mise handling;
- broad TUI resource discovery;
- automatic tracking of the target side of an untracked symlink;
- destructive resource replacement;
- merging into an existing differing copied directory;
- updating an existing differing Git checkout;
- hard-link preservation;
- xattrs, ACLs, capabilities, sockets, FIFOs, or device nodes.

These are future milestones.

## 6. Provider Naming and CLI

The roadmap previously used `directories`. This milestone uses:

```text
resources
```

because the provider owns:

```text
regular files
regular directories
Git repositories
portable symlink relationships
```

Category commands:

```bash
omarchy-blueprint capture resources
omarchy-blueprint status resources
omarchy-blueprint diff resources
omarchy-blueprint restore resources
omarchy-blueprint check
```

Resource registry commands:

```bash
omarchy-blueprint track ~/dotfiles
omarchy-blueprint track ~/.local/bin/my-script

omarchy-blueprint tracked

omarchy-blueprint untrack dotfiles
omarchy-blueprint untrack resource:dotfiles
```

Links are normally automatic, but users need a persistent opt-out:

```bash
omarchy-blueprint untrack 'link:~/.config/example'
```

This does **not** delete the live symlink. It records an ignored-link rule so later captures do not immediately re-adopt it.

Re-enable:

```bash
omarchy-blueprint track 'link:~/.config/example'
```

This only succeeds when the link currently points into an already tracked resource.

No CLI command automatically tracks a new target resource from a symlink.

## 7. `track` Semantics

`track <path>` is an atomic register-and-initial-capture operation.

Flow:

```text
resolve logical user path
        ↓
validate tracking boundary
        ↓
inspect file kind
        ↓
detect clean portable Git provenance
        │
        ├─ clean portable Git repo → git strategy
        └─ ordinary file/dir       → copy strategy
        ↓
preflight sensitive/unsupported content
        ↓
stage snapshot or Git metadata
        ↓
discover portable link relationships
        ↓
validate ownership/collisions
        ↓
commit profile metadata + staged snapshot
```

If any preflight/capture step fails, the resource is not added to the saved profile.

### 7.1 Tracking a symlink directly

A symlink is not a valid resource root in v1.

Example:

```bash
omarchy-blueprint track ~/.config/nvim
```

when `~/.config/nvim` is a symlink returns a message equivalent to:

```text
~/.config/nvim is a symlink to ~/dotfiles/nvim.
Track the owning resource (for example ~/dotfiles) instead.
```

Blueprint does not automatically adopt the target from this CLI operation.

The future TUI may offer that target as a one-click suggestion.

## 8. Logical Paths

Portable resource roots are stored as logical home-relative paths wherever possible:

```text
~/dotfiles
~/.local/bin/deploy
~/Projects/example
```

Internally, filesystem operations use absolute normalized paths.

Schema 7 requires tracked resource roots to be inside the current user's home directory.

This keeps v1 portable and avoids pretending that arbitrary `/srv`, `/etc`, removable-media, or mount-specific paths can be reconstructed safely before machine mappings exist.

The profile directory itself may not be tracked, nor may a tracked resource contain or be contained by the active profile directory.

## 9. Resource IDs

Every resource has a stable ID.

Default:

```text
~/dotfiles              → dotfiles
~/omarchy-setup         → omarchy-setup
~/.local/bin/deploy     → deploy
```

IDs:

- must be non-empty;
- may contain ASCII letters, digits, `_`, `-`, and `.`;
- may not be `.` or `..`;
- must be unique.

If the derived ID collides, `track` fails and asks for:

```bash
omarchy-blueprint track <path> --id <id>
```

IDs, not absolute paths, are used by symlink targets and future machine path mappings.

## 10. Resource Root Overlap

Tracked resource roots may not overlap.

These are rejected:

```text
resource: dotfiles → ~/dotfiles
resource: nvim     → ~/dotfiles/nvim
```

and:

```text
resource: home-config → ~/.config
resource: nvim        → ~/.config/nvim
```

The first explicitly tracked root owns its entire root boundary for this provider.

This avoids double capture, ambiguous symlink ownership, and future path-mapping ambiguity.

## 11. Profile Schema

Schema increases from 6 to 7:

```go
const Schema = 7

const (
    configSchema       = 2
    defaultsSchema     = 3
    shellSchema        = 4
    hooksSchema        = 5
    misePackagesSchema = 6
    resourcesSchema    = 7
)
```

Add:

```go
type CaptureMeta struct {
    Packages  bool `toml:"packages"`
    Themes    bool `toml:"themes"`
    Plugins   bool `toml:"plugins"`
    Config    bool `toml:"config"`
    Defaults  bool `toml:"defaults"`
    Shell     bool `toml:"shell"`
    Hooks     bool `toml:"hooks"`
    Resources bool `toml:"resources"`
}
```

and:

```go
type Data struct {
    Manifest  Manifest
    Packages  Packages
    Themes    Themes
    Plugins   Plugins
    Resources Resources
    Config    Configs
    Defaults  Defaults
    Shell     Shell
    Hooks     Hooks
}
```

Schema 6 and older profiles load with empty uncaptured Resources state.

## 12. Profile Model

Recommended profile types:

```go
type Resources struct {
    Items        []Resource     `json:"resources" toml:"resource"`
    Links        []ResourceLink `json:"links" toml:"link"`
    IgnoredLinks []string       `json:"ignored_links,omitempty" toml:"ignored_links,omitempty"`
}

type Resource struct {
    ID       string `json:"id" toml:"id"`
    Path     string `json:"path" toml:"path"`

    Kind     string `json:"kind" toml:"kind"`         // file | directory
    Strategy string `json:"strategy" toml:"strategy"` // copy | git

    Hash     string `json:"hash,omitempty" toml:"hash,omitempty"`
    Mode     string `json:"mode,omitempty" toml:"mode,omitempty"`

    Remote   string `json:"remote,omitempty" toml:"remote,omitempty"`
    Branch   string `json:"branch,omitempty" toml:"branch,omitempty"`
    Revision string `json:"revision,omitempty" toml:"revision,omitempty"`
}

type ResourceLink struct {
    SourceResource string `json:"source_resource,omitempty" toml:"source_resource,omitempty"`
    Source         string `json:"source" toml:"source"`

    TargetResource string `json:"target_resource" toml:"target_resource"`
    Target         string `json:"target" toml:"target"`

    Origin         string `json:"origin" toml:"origin"` // inbound | resource
}
```

### 12.1 Resource fields

For copied regular file:

```toml
[[resource]]
id = "deploy"
path = "~/.local/bin/deploy"
kind = "file"
strategy = "copy"
hash = "sha256..."
mode = "0755"
```

For copied directory:

```toml
[[resource]]
id = "scripts"
path = "~/Scripts"
kind = "directory"
strategy = "copy"
hash = "sha256..."
mode = "0755"
```

For Git:

```toml
[[resource]]
id = "dotfiles"
path = "~/dotfiles"
kind = "directory"
strategy = "git"
remote = "git@github.com:Grenco/dotfiles.git"
branch = "main"
revision = "0123456789abcdef..."
```

`branch` is informational in v1.

`revision` is the restore intent.

## 13. Profile Storage

```text
resources/
├── resources.toml
└── files/
    ├── deploy/
    │   └── content
    └── scripts/
        └── ...
```

Git resources do not copy repository content into the profile.

Copied file snapshot:

```text
resources/files/<id>/content
```

Copied directory snapshot:

```text
resources/files/<id>/
    <tree contents>
```

Symlinks are **not stored inside copied snapshots**. They are represented semantically in `resources.toml`.

Snapshot directories must therefore contain only:

- regular directories;
- regular files.

This keeps hashing/restoration deterministic and makes link dependencies explicit.

## 14. Copy Resource Hashing

Copied resources use a canonical SHA-256 hash.

For regular file:

```text
type
mode
file bytes
```

For directory:

```text
sorted relative path
entry type
mode
regular-file bytes
```

Directory symlink entries are excluded from the copy-tree hash because their state is represented by `ResourceLink`.

Unsupported non-regular entries fail capture.

The root mode is stored separately in `Resource.Mode`.

Hashing never follows symlinks.

## 15. Git Resource Detection

A directory is reconstructed as Git only when all of the following are true:

1. the selected path is the worktree root;
2. `.git` identifies a usable Git worktree;
3. `git status --porcelain --untracked-files=all` is empty;
4. `git rev-parse HEAD` returns a valid commit;
5. a usable `origin` remote exists;
6. the remote is portable.

Capture records:

```text
remote
branch, when attached
HEAD revision
```

Git credentials embedded in URL userinfo are never persisted.

An HTTPS remote containing userinfo is sanitized. The user is expected to use normal Git credential mechanisms during restore.

Local filesystem-only remotes are rejected in v1 because they are not portable reconstruction sources.

### 15.1 Dirty repository

This section describes historical Resources v1 behavior. Schema-10 Dirty Git
State v2 supersedes it with `git`, `git+diff`, and `copy` policies.

A dirty Git repository is not silently copied. `track` and `capture resources`
record its portable remote and current HEAD revision, but omit working-tree and
index changes. They report:

```text
resource dotfiles has uncommitted or untracked Git state;
local Git changes are not captured
```

Schema-10 Dirty Git State v2 adds separate tracked patch layers and explicitly
selected untracked files.

## 16. Git Restore Policy

Portable Resources v1 pins the captured revision.

Restore for a missing Git resource:

```text
ensure destination parent
        ↓
git clone --no-checkout <remote> <destination>
        ↓
git -C <destination> checkout --detach <revision>
```

This reproduces the captured state rather than whatever the branch happens to contain later.

A future milestone can add:

```text
restore_policy = "branch"
```

without changing the resource/provider boundary.

## 17. Existing Git Destination

If the target resource path already exists:

### Clean, same remote, same revision

No-op.

### Same repository, different revision

Conflict; leave untouched.

### Dirty repository

Conflict; leave untouched.

### Different remote

Conflict; leave untouched.

### Non-Git destination

Conflict; leave untouched.

Portable Resources v1 never automatically fetches, resets, cleans, or replaces an existing Git worktree.

## 18. Copy Restore Semantics

### 18.1 Regular file

Rules:

1. destination missing → create from snapshot;
2. destination regular with same hash and mode → no-op;
3. destination differs → conflict/skip;
4. destination is symlink/special → conflict/skip.

No overwrite.

### 18.2 Directory

Rules:

1. destination missing → restore entire captured tree;
2. destination directory with same canonical tree state and modeled links → no-op;
3. destination exists and differs → conflict/skip;
4. destination is non-directory/symlink → conflict/skip.

v1 does not merge missing files into an existing different directory.

This intentionally favors target safety over convenience.

## 19. Symlink Model

Portable Resources does not preserve the raw source-machine symlink text as desired state.

It preserves the **semantic relationship**:

```text
source location
    → target resource ID
    → target path relative to resource root
```

Example:

```toml
[[link]]
source = "~/.config/hypr/overrides.lua"
target_resource = "omarchy-setup"
target = "config/hyprland-overrides.lua"
origin = "inbound"
```

The original source machine might contain:

```text
/home/alice/.config/hypr/overrides.lua
    -> /home/alice/omarchy-setup/config/hyprland-overrides.lua
```

Restore on another machine computes a relative link from the destination path to the reconstructed resource.

For example:

```text
~/.config/hypr/overrides.lua
    -> ../../omarchy-setup/config/hyprland-overrides.lua
```

Status/verify compare the resolved semantic target, not literal symlink text.

An absolute source link and an equivalent relative target link therefore compare equal.

## 20. Link Source Kinds

### 20.1 Inbound link

The link source is outside all tracked resource roots and points into one.

Example:

```text
~/.config/hypr/overrides.lua
    -> ~/dotfiles/hypr/overrides.lua
```

Stored with:

```toml
source = "~/.config/hypr/overrides.lua"
target_resource = "dotfiles"
target = "hypr/overrides.lua"
origin = "inbound"
```

### 20.2 Resource-owned link

A symlink exists inside a copied resource and targets the same or another tracked resource.

Example:

```text
~/Scripts/current-tool
    -> ~/dotfiles/bin/tool
```

when both `Scripts` and `dotfiles` are tracked.

Stored:

```toml
source_resource = "scripts"
source = "current-tool"
target_resource = "dotfiles"
target = "bin/tool"
origin = "resource"
```

The symlink is omitted from the copied `scripts` snapshot and recreated after required resources exist.

### 20.3 Git-owned link

A symlink inside a Git-strategy resource remains Git-owned.

Git already records the symlink entry as repository content. Blueprint does not create a duplicate `ResourceLink` operation for it.

The link scanner may classify it for diagnostics and future TUI graph visualization, but it does not take ownership.

## 21. External and Broken Links

A symlink inside a copied resource whose resolved target is outside all tracked resources is not silently omitted.

Capture fails that resource with a clear dependency error:

```text
resource scripts contains symlink current-tool
pointing to untracked target ~/other-tools/current-tool;
track the owning resource or remove/ignore the dependency
```

A broken symlink inside a copied resource likewise fails capture.

This prevents a supposedly portable copied resource from silently losing part of its structure.

For a discovered inbound symlink outside resource roots:

- target inside tracked resource → manage automatically;
- target inside untracked home path → discovery candidate only;
- target outside home → external warning/candidate only;
- broken → warning/candidate only.

CLI v1 does not persist or restore the latter three classes.

## 22. Reverse Symlink Discovery

When resources are tracked or captured, Blueprint scans a bounded namespace for symlink edges.

Default v1 search scope:

```text
$HOME                     direct children only
$HOME/.config             recursive
$HOME/.local/bin          recursive
```

This finds common patterns such as:

```text
~/.zshrc
~/.gitconfig
~/.config/nvim
~/.config/hypr/overrides.lua
~/.config/omarchy/hooks/theme-set
~/.local/bin/tool
```

without recursively walking the user's entire home directory.

The search roots are internal provider dependencies so tests can inject isolated roots and future settings can extend them.

## 23. Scanner Safety

The link scanner:

- uses `Lstat`/`WalkDir`;
- never follows a symlinked directory;
- reads link text with `Readlink`;
- may resolve the single link target to classify ownership;
- never reads target file contents for discovery;
- skips the active profile directory;
- skips tracked resource roots during inbound scanning because internal links are handled separately;
- de-duplicates source paths;
- sorts results deterministically.

A symlink target is considered inside a resource only after resolving enough filesystem state to prove containment.

Lexical prefix matching alone is insufficient.

## 24. Link Discovery Data Model

The low-level scanner should return all useful edges, not only the subset CLI v1 manages.

Conceptually:

```go
type LinkCandidate struct {
    Source       string
    RawTarget    string
    Resolved     string

    Classification LinkClassification

    SourceResource string
    TargetResource string
    TargetRelative string
}
```

Classifications:

```text
managed-inbound
managed-resource
git-owned
untracked-home-target
external-target
broken
ownership-conflict
```

CLI v1:

```text
managed-* → save/use
git-owned → ignore as duplicate ownership
other     → warning/candidate only
```

Future TUI:

```text
untracked-home-target
    ↓
inspect target root
    ↓
offer "Track this resource?"
```

This is how the TUI feature can be added without redesigning discovery.

## 25. Automatic Adoption Rule

A discovered inbound link becomes managed by default when:

1. its source is inside the v1 link-search namespace;
2. its source is outside all tracked resource roots;
3. its resolved target is inside an already tracked resource;
4. the target path exists;
5. the source path is not in `IgnoredLinks`;
6. adopting the symlink does not violate another provider's ownership claim.

No prompt is required.

That is the desired:

> "As soon as I start tracking dotfiles, links into dotfiles are tracked by default."

behavior.

## 26. Ignored Links

Because auto-adoption is the default, users need a persistent opt-out.

Profile:

```toml
ignored_links = [
  "~/.config/example"
]
```

Only inbound source paths are stored in `IgnoredLinks` in v1.

`capture resources` excludes those source paths from automatic link state.

`untrack link:<path>` adds the normalized path to `IgnoredLinks` and removes the saved inbound `ResourceLink`.

`track link:<path>` removes the ignore entry and re-adopts the link if it currently resolves inside a tracked resource.

Ignoring a link never changes the live filesystem.

## 27. Resource Ownership Claims

Portable Resources introduces a small reusable ownership index.

Purpose:

> A generic resource should not silently take ownership of state another provider manages semantically.

Conceptual API:

```go
type Claim struct {
    Provider         string
    Path             string
    Recursive        bool
    DelegateSymlinks bool
}

type Index struct {
    Claims []Claim
}

func (i Index) TrackConflict(path string) []Claim
func (i Index) LinkConflict(path string) []Claim
```

### 27.1 Track-root rule

Tracking a file/directory conflicts with any claim whose owned content would be inside the tracked root.

Examples:

```text
track ~/.config
→ reject: overlaps Omarchy/Blueprint-owned config, shell, hooks, plugin state

track ~/.config/special-app
→ allowed if no specific provider claims it
```

### 27.2 Link-leaf rule

A symlink source may be Portable Resources-owned when:

- no provider claims that path; or
- the claiming provider explicitly delegates symlink ownership.

Hooks is the important first example.

The Hooks provider already treats runtime symlinks as externally managed, so its ownership claim should allow:

```text
DelegateSymlinks = true
```

This lets Resources own:

```text
~/.config/omarchy/hooks/theme-set
    -> resource:omarchy-setup/hooks/...
```

without making Hooks capture the target bytes.

Existing fixed Config paths may remain non-delegated in v1 unless their provider is explicitly updated to understand external Resource ownership.

A conflict is reported rather than stolen.

## 28. Initial Ownership Claims

The app layer should build claims from actual provider paths.

At minimum:

```text
Config fixed managed files        exact, non-delegated
Shell user shell.json             exact, non-delegated
Hooks runtime root                recursive, symlink-delegated
Plugin user root                  recursive, non-delegated
Theme user root                   recursive, non-delegated
Blueprint profile directory       recursive, non-delegated
Blueprint state/journal directory recursive, non-delegated
```

The ownership index is not a new user-facing provider.

It is a safety service used by Resources and available for later provider coordination.

## 29. `omarchy-setup` Acceptance Case

Source:

```text
~/omarchy-setup                 clean Git repo
├── hooks/omarchy-theme-hook.sh
└── config/hyprland-overrides.lua
```

Live links:

```text
~/.config/omarchy/hooks/theme-set
    -> ~/omarchy-setup/hooks/omarchy-theme-hook.sh

~/.config/hypr/overrides.lua
    -> ~/omarchy-setup/config/hyprland-overrides.lua
```

Command:

```bash
omarchy-blueprint track ~/omarchy-setup
```

Expected profile:

```toml
[[resource]]
id = "omarchy-setup"
path = "~/omarchy-setup"
kind = "directory"
strategy = "git"
remote = "..."
revision = "..."

[[link]]
source = "~/.config/hypr/overrides.lua"
target_resource = "omarchy-setup"
target = "config/hyprland-overrides.lua"
origin = "inbound"

[[link]]
source = "~/.config/omarchy/hooks/theme-set"
target_resource = "omarchy-setup"
target = "hooks/omarchy-theme-hook.sh"
origin = "inbound"
```

Fresh-machine restore:

```text
ensure ~/ parent
        ↓
clone ~/omarchy-setup
        ↓
checkout captured revision
        ↓
create ~/.config/hypr parent if needed
        ↓
create relative overrides.lua link
        ↓
create ~/.config/omarchy/hooks parent if needed
        ↓
create relative theme-set link
        ↓
verify both links resolve inside resource
```

Hooks continues to regard `theme-set` as externally managed. Resources is responsible for reproducing its symlink relationship.

## 30. Copied Resource Symlink Example

Tracked:

```text
~/Scripts
~/dotfiles
```

Live:

```text
~/Scripts/current
    -> ~/dotfiles/bin/current
```

`Scripts` is copy strategy.

Capture:

```text
resources/files/scripts/
    regular files only
```

and:

```toml
[[link]]
source_resource = "scripts"
source = "current"
target_resource = "dotfiles"
target = "bin/current"
origin = "resource"
```

Restore order:

```text
restore scripts regular tree
restore dotfiles
create scripts/current symlink
```

The link operation depends on both resource satisfaction states.

## 31. Restore Operation Extensions

Portable Resources needs safe local filesystem actions that should live in the generic restore executor rather than shelling out to `ln` or `mkdir`.

Add:

```go
type DirectoryCreate struct {
    Path                 string `json:"path"`
    Mode                 uint32 `json:"mode"`
    RejectSymlinkParents bool   `json:"reject_symlink_parents,omitempty"`
}

type SymlinkWrite struct {
    Destination          string `json:"destination"`
    Target               string `json:"target"`
    ExpectedMissing      bool   `json:"expected_missing"`
    RejectSymlinkParents bool   `json:"reject_symlink_parents,omitempty"`
}
```

Extend `Operation`:

```go
Directory *DirectoryCreate `json:"directory,omitempty"`
Symlink   *SymlinkWrite     `json:"symlink,omitempty"`
```

Exactly one action remains required per operation.

### 31.1 Directory action

The directory action is idempotent for an existing real directory.

It rejects:

- existing symlink;
- existing non-directory;
- symlinked parent when guard enabled.

Used primarily to create Git destination parents safely.

### 31.2 Symlink action

The symlink action only creates a missing destination.

It never overwrites:

- regular file;
- directory;
- existing different symlink.

The provider handles an already-equivalent symlink as a plan no-op.

Execution rechecks:

```text
destination still missing
parent path contains no symlink
target string is the approved generated target
```

before mutation.

## 32. Copy Executor Hardening

Extend `model.Copy` for Resources:

```go
type Copy struct {
    Source               string `json:"source"`
    Destination          string `json:"destination"`
    SourceHash           string `json:"source_hash,omitempty"`
    RejectSymlinkParents bool   `json:"reject_symlink_parents,omitempty"`
}
```

When `SourceHash` is present:

1. validate the snapshot tree contains only regular dirs/files;
2. hash it before mutation;
3. require exact expected hash;
4. recheck parent-symlink safety before temp creation/rename.

Existing callers that do not set `SourceHash` retain current behavior until migrated.

Resource snapshots always set it.

## 33. Operation IDs

Examples:

```text
resources.mkdir.omarchy-setup
resources.git.clone.omarchy-setup
resources.git.checkout.omarchy-setup

resources.copy.scripts
resources.file.deploy

resources.link.home-.config-hypr-overrides.lua
resources.link.scripts-current
```

IDs must be deterministic and sanitized.

## 34. Restore Dependency Graph

Conceptually:

```text
resource root operation
        ↓
resource ready operation
        ↓
link operations targeting that resource
```

For Git:

```text
mkdir parent
    ↓
clone
    ↓
checkout revision
    ↓
inbound links
```

For copied directory:

```text
copy tree
    ↓
resource-internal links
```

Cross-resource link:

```text
source resource ready ─┐
                       ├─→ create link
target resource ready ─┘
```

If a resource is already satisfied, it has no mutation operation and is treated as ready.

If a required resource is in conflict, dependent links are skipped rather than pointed into conflicting state.

## 35. Restore Risk

Portable Resources v1 performs only additive, non-overwriting restore.

Recommended risk:

```text
create parent directory      low
clone Git repo               low
checkout captured revision   low
create copied file           low
copy missing directory       low
create missing symlink       low
```

Existing target conflicts produce skips.

No `--force` behavior is added for Resources in schema 7.

## 36. Detect

Resources detection is profile-directed.

Unlike package discovery, Blueprint does not crawl the home directory looking for arbitrary resources.

For every saved resource definition:

```text
expand logical path
        ↓
Lstat
        ↓
missing?
        ↓
detect copy/Git state according to saved strategy
```

Separately:

```text
scan link-search roots
        ↓
classify links against tracked resource roots
        ↓
produce current managed inbound link set
```

For copied directories, scan internal symlinks and classify them against the tracked resource index.

## 37. Capture

`capture resources` refreshes only already tracked resources.

It does not discover and add arbitrary directories/repositories.

For copy resources:

```text
stage new snapshot
        ↓
hash
        ↓
collect modeled symlinks
```

For Git:

```text
require clean
        ↓
refresh remote/branch/revision metadata
```

Then:

```text
discover inbound links
        ↓
apply ignored links
        ↓
validate ownership
        ↓
atomically replace resource metadata/snapshots
```

If one resource fails capture, no partially staged resource profile is committed.

## 38. Diff Semantics

### Resources

Saved copy resource vs current:

```text
missing       → removed/missing
same          → clean
different     → modified
wrong kind    → modified/conflict
```

Saved Git resource vs current:

```text
missing                → missing
same remote + revision
and clean              → clean
different revision     → modified
different remote       → modified
dirty                  → modified
wrong kind             → modified
```

### Links

```text
saved link missing             → removed
same semantic target           → clean
same source, different target  → modified
new managed inbound link       → added / uncaptured
```

New inbound links pointing into tracked resources therefore appear in status/diff until the next capture, at which point they become saved state automatically.

## 39. Status

Example:

```text
Portable Resources

~ git resource dotfiles
  revision: 1234abcd → 5678ef01

+ link ~/.config/wezterm
  → resource:dotfiles/wezterm

- link ~/.config/hypr/overrides.lua
  → resource:dotfiles/hypr/overrides.lua

! copied resource scripts contains untracked external symlink
```

Any managed Resources difference contributes to normal drift exit code 2.

Warnings that represent an unsupported/external dependency should be surfaced clearly. Whether a warning increments drift follows the same provider policy as Hooks: stable known unmanaged dependencies should not create perpetual false drift after capture.

## 40. Verify

Verification requires every saved managed resource to be satisfied.

Copy:

```text
path exists
kind correct
hash correct
mode correct
```

Git:

```text
worktree exists
remote equivalent
HEAD == captured revision
working tree clean
```

Link:

```text
source is symlink
resolved semantic target == target resource/path
```

Target-only/new inbound links do not fail verification of saved desired state.

They may still appear as uncaptured status drift.

## 41. Check

Resources `check` validates:

1. schema/resource metadata;
2. unique resource IDs;
3. no overlapping tracked roots;
4. logical paths resolve inside HOME;
5. profile directory does not overlap tracked roots;
6. copy snapshot exists when required;
7. copy snapshot contains no symlinks/special files;
8. copy snapshot hash/mode matches metadata;
9. Git metadata has a portable remote and valid revision;
10. link source keys are unique;
11. every managed link references an existing resource;
12. every target-relative path is safe and cannot escape the resource root;
13. ignored-link paths are normalized and unique;
14. no managed link conflicts with provider ownership rules;
15. required executables (`git`) are available when Git resources exist;
16. no known-sensitive copied path was persisted.

`check` does not require Git when the profile has no Git resources.

## 42. Sensitive Path Guard

Portable Resources is the first generic byte-capture feature, so it must be conservative about obvious secrets.

v1 refuses to copy resources whose root or descendant path matches high-confidence sensitive patterns such as:

```text
~/.ssh/id_*
~/.gnupg
~/.aws/credentials
.env
.env.*
*credentials*
*private_key*
```

Exact policy belongs in a reusable helper rather than scattered provider code.

The scanner must avoid overly broad substring rules that make ordinary source trees unusable.

High-confidence private-key content markers should also be detected in small text files:

```text
-----BEGIN ... PRIVATE KEY-----
```

A detected sensitive file blocks capture with the exact path.

There is no `--force-secrets-into-profile` escape hatch in v1.

Users should keep secrets in an external secret manager and symlink/reference them separately rather than committing them to a Blueprint profile.

## 43. Unsupported Files

Copy capture fails on:

```text
socket
FIFO
device node
unknown special file
```

Hard links are copied as independent regular files in v1; hard-link identity is not preserved.

Symlinks are handled semantically as described above.

## 44. Provider Ownership and Existing Providers

### Hooks

Hooks already treats runtime symlinks as externally managed.

When a link points into a tracked Resource:

```text
Hooks
→ does not follow/capture target

Resources
→ owns and restores the link
```

This is the desired division.

### Config

Fixed Config files remain semantically Config-owned.

If an inbound link source exactly collides with a Config-owned fixed path and Config does not yet delegate symlink ownership, Resources reports an ownership conflict rather than auto-adopting it.

The existing `hypr/overrides.lua` acceptance case is not one of the Config provider's fixed managed files and is therefore Resources-owned.

### Plugins/themes/shell

Resources does not track roots that overlap these providers' managed state.

This prevents generic copying from duplicating semantic providers.

## 45. Track-root Ownership Preflight

Before adding a resource, compare its absolute root against ownership claims.

Reject examples:

```text
track ~/.config/omarchy/hooks
track <plugin user root>
track <theme user root>
track <active Blueprint profile>
```

Allow examples:

```text
track ~/dotfiles
track ~/Scripts
track ~/.config/special-app
```

when they do not contain another provider's managed paths.

The error lists the conflicting provider/path instead of merely saying "not allowed."

## 46. Untracking

`untrack resource:<id>`:

- removes resource metadata;
- removes its captured copy snapshot;
- removes saved links whose target is that resource;
- removes saved resource-owned links sourced inside that resource;
- does not touch the live resource;
- does not delete live symlinks;
- does not remove other resources.

If another remaining copied resource currently contains a link to the untracked resource, the next capture reports it as an untracked external dependency.

This makes untracking profile-only and non-destructive.

## 47. `tracked` Output

Example:

```text
Portable resources

dotfiles
  ~/dotfiles
  git @ 5678ef01
  3 inbound links

scripts
  ~/Scripts
  copy
  12 files
  1 resource link

Ignored links
  ~/.config/example
```

JSON output should use the same profile structs.

## 48. Capture Selection

Resources is a normal category:

```bash
omarchy-blueprint capture resources
```

Aggregate `capture` includes Resources after the profile has at least one tracked resource or `Capture.Resources` is true.

Tracking the first resource sets:

```toml
[capture]
resources = true
```

An empty tracked set may remain captured after the last untrack, matching Config's "captured but empty" semantics.

## 49. Provider Registry Order

Recommended state-provider order:

```text
packages
themes
plugins
resources
config
defaults
shell
hooks
```

Resources comes before raw/consumer configuration providers so restore planning and ownership context are available early.

Cross-provider correctness must not rely only on list order; explicit operation dependencies are still used where one Resource operation depends on another.

## 50. App Dependencies

Add injectable path/discovery dependencies so tests never inspect the developer's actual home.

Conceptually:

```go
type Dependencies struct {
    ...
    HomeDir            func() (string, error)
    ResourceLinkRoots  func(home string) []resources.LinkSearchRoot
    ResourceOwnership  func(...) ownership.Index
}
```

Tests use isolated temp homes.

No Resources test may read the real `$HOME`, `.config`, Git config, or dotfiles.

## 51. Git Command Safety

All Git operations use argv arrays.

No shell command strings.

Examples:

```go
[]string{
    "git", "clone", "--no-checkout",
    remote,
    destination,
}

[]string{
    "git", "-C", destination,
    "checkout", "--detach", revision,
}
```

Remote and revision values are validated before entering an operation.

## 52. Relative Symlink Generation

Given:

```text
source:
~/.config/hypr/overrides.lua

target:
resource dotfiles at ~/dotfiles
target relative path hypr/overrides.lua
```

Planner:

```text
absolute source parent
absolute resolved target
        ↓
filepath.Rel(sourceParent, target)
        ↓
portable relative link text
```

Before operation creation:

- target must remain inside the mapped resource root;
- resulting relation must be non-empty;
- source destination must be inside HOME;
- source parent path must not traverse a symlink during execution.

The raw original link text is not required for restore.

## 53. Link Collision Rules

At plan time:

### Missing source

Safe create candidate.

### Existing symlink with same semantic target

No-op.

### Existing symlink with different target

Conflict/skip.

### Existing regular file

Conflict/skip.

### Existing directory

Conflict/skip.

### Existing special file

Conflict/skip.

No automatic replacement.

`--force` does not change these rules in v1.

## 54. Resource Collision Rules

### Saved resource missing

Restore candidate.

### Existing resource semantically equal

No-op.

### Existing resource differs

Conflict/skip.

### Existing path has wrong type

Conflict/skip.

Unknown target work always wins.

## 55. Atomicity and Journaling

Copy file writes use existing journal-backed `FileWrite`.

Copy directory restore uses the existing temp-directory + rename pattern, extended with source hash and parent-symlink checks.

Git clone is not reversible through Blueprint's generic rollback in v1.

Creating a new symlink is reversible in principle, but the executor should record the operation rather than automatically remove it after unrelated later failures.

The restore journal captures every Resources operation using existing event types.

## 56. Capture Transaction

Capture should stage under:

```text
resources/.capture-<random>/
```

for copied snapshots and candidate metadata.

Only after all tracked resources and links validate:

```text
swap files snapshot root
write resources.toml atomically
```

If capture fails before commit, the previous saved Resources state remains intact.

This is especially important when a broad copied directory contains a newly introduced unsupported symlink or sensitive file.

## 57. Profile Editing

`resources/resources.toml` remains intentionally readable and editable.

However:

- IDs and paths are validated on load/check;
- link target paths may not escape a resource root;
- editing Git revisions/remote URLs changes desired state and is allowed when valid;
- editing hashes without matching snapshots causes `check` failure.

The running system remains the normal editor, but direct profile edits are not prohibited.

## 58. Future TUI Discovery

The TUI feature builds on the same scanner.

Example current graph:

```text
~/.config/nvim
    -> ~/dotfiles/nvim

~/dotfiles is not tracked
```

Scanner result:

```go
LinkCandidate{
    Classification: UntrackedHomeTarget,
    Source:          "~/.config/nvim",
    Resolved:        "~/dotfiles/nvim",
}
```

TUI can walk upward to an obvious candidate root:

```text
~/dotfiles/.git
```

and present:

```text
Symlink target is inside Git repository ~/dotfiles.

Track ~/dotfiles?
● Yes
○ Ignore this link
○ Not now
```

Only after user approval would it call the same resource tracking API.

The CLI v1 does not perform that upward auto-tracking step.

## 59. Future Path Mappings

The link model is intentionally target-resource-relative.

Today:

```toml
[[resource]]
id = "dotfiles"
path = "~/dotfiles"

[[link]]
source = "~/.config/nvim"
target_resource = "dotfiles"
target = "nvim"
```

Future machine overlay:

```toml
[paths]
dotfiles = "~/src/dotfiles"
```

The saved link relationship requires no schema change.

Restore simply recalculates:

```text
~/.config/nvim
    -> ../../src/dotfiles/nvim
```

This is a major reason not to preserve absolute raw symlink text.

## 60. Future Mixed Directories

A later `mixed` strategy can recursively turn:

```text
~/Projects
├── repo-a/
├── repo-b/
└── notes/
```

into child resources.

Portable Resources v1 intentionally avoids that automatic expansion.

The schema can extend `Resource.Strategy` from:

```text
copy
git
```

to:

```text
copy
git
mixed
reference
ignore
```

without changing provider ownership.

## 61. Future Dirty Git State

**Superseded by schema-10 Dirty Git State v2.** This section records the v1
extension sketch only; see [Dirty Git State v2](2026-09-10-dirty-git-state-v2-design.md)
and [ADR 0015](../../adr/0015-dirty-git-resource-policies.md) for delivered
behavior.

Later Git-state support can extend a Git resource with:

```text
tracked.patch
selected untracked files
restore patch operation
```

The v1 resource already records:

```text
ID
logical path
remote
branch
revision
```

so the future milestone is additive.

## 62. Security Properties

Portable Resources v1 guarantees:

- no automatic recursive resource expansion;
- no symlink dereference during copy capture;
- no symlink dereference during snapshot restore;
- no mutation through symlinked parent directories;
- no overwrite of unknown target work;
- no profile capture of known-sensitive paths;
- no embedded Git URL credentials;
- no shell interpolation for Git/link operations;
- no raw absolute symlink dependency on source username/home path;
- no tracking overlap with provider-owned roots.

## 63. Failure Handling

### Track failures

Do not modify profile when:

- path missing;
- path is symlink;
- path outside HOME;
- path overlaps another tracked resource;
- path conflicts with provider ownership;
- Git repo is dirty (v1 only; superseded by schema-10 Dirty Git State v2);
- Git remote is not portable;
- copied resource contains unsupported special file;
- copied resource contains broken/untracked external symlink;
- copied resource contains known-sensitive content.

### Capture failures

Preserve previous saved state and staged snapshots are discarded.

### Restore skips

Safe independent resources continue when:

- one resource destination conflicts;
- a link destination conflicts;
- a required target resource is unsatisfied;
- a symlink parent becomes unsafe.

### Execution failures

Use the existing restore result/journal model.

## 64. Acceptance Criteria

Portable Resources v1 is complete when all of the following hold:

1. Schema 6 loads as schema 7 with Resources uncaptured/empty.
2. `track` of a regular file captures its bytes/hash/mode.
3. `track` of a regular directory captures only regular files/dirs.
4. Copy capture never follows symlinks.
5. A same-resource internal symlink is stored semantically and omitted from snapshot bytes.
6. A cross-resource symlink is stored with source/target resource IDs.
7. A copied resource with a symlink to an untracked target fails capture.
8. A broken symlink inside a copied resource fails capture.
9. A clean Git repo is stored as remote + branch + exact revision without repository bytes.
10. In v1, a dirty Git repo captures portable HEAD provenance and reports uncaptured local drift; schema-10 behavior is superseded by Dirty Git State v2.
11. A local-only Git remote is refused.
12. URL credentials are not persisted.
13. `track ~/omarchy-setup` auto-discovers `~/.config/hypr/overrides.lua` pointing into it.
14. The same track auto-discovers `~/.config/omarchy/hooks/theme-set`.
15. Hooks continues not to capture/dereference that target.
16. A new inbound symlink into a tracked resource appears as uncaptured status drift.
17. `capture resources` adopts that new inbound link unless ignored.
18. `untrack link:<path>` persists an ignored-link rule and does not delete the live link.
19. `track link:<path>` re-enables an ignored link only when it targets a tracked resource.
20. The scanner does not recursively walk all of `$HOME`.
21. The scanner never follows symlink directories.
22. Direct home symlinks such as `~/.zshrc` can be auto-discovered.
23. `.config` and `.local/bin` inbound links can be auto-discovered.
24. An inbound link target outside tracked resources is never auto-tracked.
25. The scanner exposes an untracked-target candidate suitable for future TUI use.
26. Resource roots cannot overlap each other.
27. Resource roots cannot overlap the profile directory.
28. Resource roots cannot capture non-delegated provider-owned state.
29. Hooks-owned namespace may delegate symlink leaf ownership to Resources.
30. Restore of a missing copied file is atomic and preserves mode.
31. Restore of a missing copied directory validates the snapshot hash first.
32. Restore of a differing copied file/directory skips without overwrite.
33. Restore of a missing Git repo clones and checks out the captured revision.
34. Existing clean same-revision Git repo is a no-op.
35. Existing different/dirty Git repo is preserved as a conflict.
36. Git destination parents can be safely created.
37. Symlink restore uses a generated relative target.
38. Existing semantically equivalent absolute/relative symlink is a no-op.
39. Existing different symlink is preserved.
40. Existing regular file at link source is preserved.
41. Link creation rejects symlinked parent directories at execution time.
42. Cross-resource link operation depends on both resources being satisfied.
43. A conflicting target resource prevents dependent link creation.
44. `verify` checks resource semantics and link resolved targets.
45. Target-only/new inbound links do not make saved desired-state verification fail.
46. `check` catches corrupted copy snapshots.
47. `check` catches unsafe link target traversal such as `../../outside`.
48. `check` requires Git only when Git resources exist.
49. Known private-key paths/content block copied capture.
50. Unsupported sockets/FIFOs/devices block copied capture.
51. `untrack resource:<id>` removes profile state/snapshot but no live filesystem state.
52. `tracked` shows resources, strategies, and inbound link counts.
53. Aggregate capture/restore includes Resources once enabled.
54. Exact existing provider tests remain green.
55. A fresh-machine vertical test reconstructs `omarchy-setup` and both example links.
56. Final exact-head CI passes Go 1.25.x and 1.26.x tests/vet/build.

## 65. Primary Vertical Test

Fixture source home:

```text
home/
├── omarchy-setup/                   Git worktree
│   ├── .git/
│   ├── hooks/omarchy-theme-hook.sh
│   └── config/hyprland-overrides.lua
└── .config/
    ├── hypr/
    │   └── overrides.lua
    │       -> ../../omarchy-setup/config/hyprland-overrides.lua
    └── omarchy/hooks/
        └── theme-set
            -> ../../../omarchy-setup/hooks/omarchy-theme-hook.sh
```

Capture expected:

```text
resources/resources.toml
→ one Git resource
→ two inbound links
→ no Git repository bytes copied
```

Target starts:

```text
fresh empty home configuration
```

Restore expected:

```text
clone exact repo revision
create both relative links
Hooks does not overwrite/follow theme-set
status resources clean
verify resources OK
```

This is the milestone's primary real-world acceptance case.

## 66. ADR Decision Summary

The accompanying ADR records these durable decisions:

1. new provider is named Resources, not Directories;
2. explicit roots are the unit of user intent;
3. Git provenance is preferred over copied bytes;
4. v1 pins Git revisions;
5. copied symlinks are semantic graph edges rather than snapshot bytes;
6. inbound links into tracked resources are auto-managed;
7. untracked symlink targets are suggestions only, never auto-tracked in CLI v1;
8. restore creates relative links;
9. unknown target work is never replaced;
10. resource ownership claims prevent generic provider takeover.

## 67. Future Sequence

After Portable Resources v1:

```text
Portable Resources v1
        ↓
Dirty Git state / mixed resource discovery
        ↓
Machine path mappings + overlays
        ↓
TUI resource discovery and graph suggestions
        ↓
Migration/agent-assisted conflict workflows
```

The TUI feature can therefore focus on interaction rather than inventing a second filesystem-discovery engine.
