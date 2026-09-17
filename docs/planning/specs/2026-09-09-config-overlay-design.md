# Config v2: Baseline-Aware Home Configuration Overlay

**Project:** Omarchy Blueprint  
**Status:** Proposed design for user review  
**Target schema:** 8  
**Date:** 2026-09-09  
**Supersedes:** Config v1's fixed four-file Hyprland capture model
**Configuration surfaces:** recursive `~/.config/**` plus curated exact `$HOME`-relative config paths  
**Depends on:** Portable Resources v1 / schema 7

---

## 1. Summary

Config v2 expands Omarchy Blueprint's `config` provider from a small fixed list of Hyprland files into a first-class, baseline-aware model of the user's configuration surfaces: recursive `~/.config/**` plus a curated registry of exact configuration files elsewhere under `$HOME`.

The provider must make this workflow practical:

```text
User configures Omarchy and applications normally
        ↓
~/.config accumulates defaults + personal changes
        ↓
omarchy-blueprint capture config
        ↓
Blueprint captures only meaningful user intent
        ↓
fresh Omarchy install
        ↓
omarchy-blueprint restore config
        ↓
personal configuration restored without copying unchanged Omarchy defaults
```

The provider does **not** snapshot `~/.config` wholesale.

Instead it classifies each path relative to Omarchy's shipped config baseline and Blueprint's provider ownership model:

```text
unchanged Omarchy baseline
→ ignore

modified Omarchy baseline
→ capture desired file + captured baseline

deleted Omarchy baseline file
→ capture tombstone

new user file
→ capture by default

path owned by Themes / Plugins / Hooks / Shell / Resources
→ delegate

symlink not represented by Resources
→ do not dereference; report

high-confidence volatile/runtime state
→ ignore and report in summary

high-confidence sensitive material
→ refuse capture and report

unsupported special filesystem object
→ skip/refuse safely
```

Restore preserves Blueprint's core safety rule:

> Unknown target-machine work wins unless the user explicitly chooses a bounded `--force` operation that first creates a recoverable backup.

Where the captured file modifies an Omarchy baseline file and the current Omarchy baseline has changed since capture, Config v2 performs a text three-way merge when possible.

If the merge is clean, Blueprint restores the rebased customization.

If the merge conflicts, the default restore preserves the target and reports the conflict.

With `--force`, Blueprint backs up the target and writes the captured desired state.

---

## 2. Why this feature comes next

Portable Resources v1 now gives dotfiles users a strong reconstruction path:

```text
Git repo
→ clone exact revision
→ recreate semantic links
```

That solves the user who already maintains authoritative dotfiles.

The larger remaining portability gap is the user who simply configures their system normally and expects Blueprint to reconstruct it.

For that user, `~/.config` is the primary configuration surface, but important portable state also lives directly under `$HOME` or in legacy tool-specific locations such as `.bashrc`, `.zshrc`, `.tmux.conf`, `.XCompose`, and `.gitconfig`.

Config v1 captures only:

```text
hypr/hyprland.lua
hypr/bindings.lua
hypr/looknfeel.lua
hypr/autostart.lua
```

Config v2 should answer:

> What in this user's `~/.config` represents portable user intent rather than unchanged Omarchy defaults, provider-owned semantic state, runtime noise, or secrets?

This feature should therefore precede Git Resource Policies / dirty Git preservation.

---

## 3. Goals

Config v2 must:

1. recursively inspect the user's `~/.config` and inspect a curated registry of exact `$HOME`-relative configuration paths;
2. avoid storing unchanged Omarchy defaults;
3. capture modified Omarchy defaults;
4. capture new user-created config files by default;
5. capture deliberate removal of shipped baseline files as tombstones;
6. preserve existing Blueprint provider ownership boundaries;
7. delegate tracked-resource symlinks to Resources rather than dereference them;
8. let users persistently opt config paths out;
9. detect and avoid high-confidence volatile/runtime state;
10. refuse high-confidence sensitive material;
11. support regular text and binary config files;
12. perform three-way text merges across Omarchy baseline changes where possible;
13. preserve target-only and target-modified config by default;
14. let `--force` resolve supported config conflicts only after creating a recoverable backup;
15. keep capture, diff, status, restore, verify, check, JSON, and dry-run behavior deterministic;
16. keep profile data Git-friendly and human-readable;
17. retain schema-7 Config profiles without requiring a destructive migration;
18. avoid requiring a dotfiles repository.

---

## 4. Non-goals

Config v2 will not:

- recursively back up arbitrary state under `$HOME`;
- become a browser profile backup system;
- copy caches, logs, sessions, sockets, lockfiles, or known runtime artifacts;
- capture secrets by default;
- follow or dereference arbitrary symlinks;
- automatically turn an untracked symlink target into a Resource;
- automatically restart arbitrary applications after restore;
- perform semantic merges for arbitrary application-specific formats;
- merge unknown target-machine user edits with saved user edits;
- provide a user-authored glob language in the first version;
- infer package-to-config ownership strongly enough to delete or exclude config automatically;
- capture empty directories solely for filesystem fidelity;
- preserve ACLs, xattrs, capabilities, hard-link identity, or filesystem birth times;
- remove target-only config files that are absent from the profile;
- implement dirty Git patch preservation.

---

## 5. Design principles

### 5.1 The running system remains the editor

Users configure applications normally.

Blueprint observes and captures the meaningful delta.

The primary workflow remains:

```bash
omarchy-blueprint capture config
```

rather than requiring users to enumerate every application directory manually.

### 5.2 Overlay, not snapshot

The Config profile represents:

```text
user intent relative to known baseline
```

not:

```text
all bytes currently under ~/.config
```

This is what prevents a fresh profile from containing a huge unchanged copy of Omarchy's defaults.

### 5.3 Specific providers outrank generic Config

Config is the generic owner of otherwise-unclaimed configuration files.

It must yield to semantic providers.

### 5.4 Symlinks are relationships, not file contents

Config never follows a symlink.

A symlink already represented by Resources is delegated.

An unrepresented symlink is reported, not captured as target bytes.

### 5.5 Unknown target work is preserved

A target file that differs from every baseline/provenance state Blueprint understands is a conflict.

Default restore skips it.

### 5.6 Force is bounded and recoverable

`--force` means:

> Resolve this known Config conflict in favor of the profile, but first preserve the displaced target object.

It does not mean:

> Recursively delete anything in the way.

---

## 6. Current architecture and required evolution

Config v1 has:

```go
type Configs struct {
    Files []ConfigFile
}

type ConfigFile struct {
    ID           string
    Path         string
    Hash         string
    BaselineHash string
}
```

and a fixed `DefaultSpecs` list.

Config v2 changes the identity model.

Arbitrary config files cannot depend on a curated ID list.

The relative path under `~/.config` becomes the stable semantic identity.

The existing schema-7 snapshot layout is useful and should be retained:

```text
config/
├── config.toml
├── files/
└── baseline/
```

but both trees become recursive and sparse.

Only captured intent is stored.

---

## 7. Schema 8 profile model

Recommended model:

```go
type Configs struct {
    Files       []ConfigFile      `json:"files" toml:"file"`
    Directories []ConfigDirectory `json:"directories,omitempty" toml:"directory,omitempty"`
    Deletes     []ConfigDelete    `json:"deletes,omitempty" toml:"delete,omitempty"`
    Excluded    []string          `json:"excluded,omitempty" toml:"excluded,omitempty"`
}

type ConfigDirectory struct {
    Path string `json:"path" toml:"path"`
    Mode string `json:"mode" toml:"mode"`
}

type ConfigFile struct {
    Path         string `json:"path" toml:"path"`
    Hash         string `json:"hash" toml:"hash"`
    Mode         string `json:"mode,omitempty" toml:"mode,omitempty"`

    BaselineHash string `json:"baseline_hash,omitempty" toml:"baseline_hash,omitempty"`
    BaselineMode string `json:"baseline_mode,omitempty" toml:"baseline_mode,omitempty"`
}

type ConfigDelete struct {
    Path         string `json:"path" toml:"path"`
    BaselineHash string `json:"baseline_hash" toml:"baseline_hash"`
    BaselineMode string `json:"baseline_mode,omitempty" toml:"baseline_mode,omitempty"`
}
```

### 7.1 Path is identity

All Config paths are:

- relative to `$HOME`;
- `.config/...` for XDG config entries;
- slash-separated in the profile;
- clean;
- non-empty;
- not absolute;
- free of `..`;
- never equal to `.`.

Examples:

```text
.config/hypr/bindings.lua
.config/ghostty/config
.config/lazygit/config.yml
.bashrc
.zshrc
.tmux.conf
.XCompose
```

### 7.2 Baseline hash semantics

A `ConfigFile` with:

```text
BaselineHash != ""
```

means:

```text
this file existed in the Omarchy baseline at capture time
and the user version differed
```

A file with:

```text
BaselineHash == ""
```

means:

```text
user-added file with no corresponding Omarchy baseline
```

No separate `Origin` field is needed.

### 7.3 Tombstone semantics

A `ConfigDelete` means:

```text
the file existed in the Omarchy baseline
but the user's desired state was "file absent"
```

The captured baseline bytes are stored under `config/baseline/<path>`.

### 7.4 Legacy Config IDs

Schema 8 removes known-spec IDs as the primary identity.

Schema-7 TOML may still contain:

```toml
id = "hypr.bindings"
```

The schema-8 loader ignores this legacy field.

The file path and existing snapshots already contain enough information to preserve the state.

Schema-7 snapshots remain layout-compatible.

### 7.5 Modes

Desired regular-file mode is persisted.

Captured baseline mode is persisted for diagnostics and preconditions.

Legacy entries with no mode remain valid:

```text
mode omitted
→ infer source mode from saved snapshot during restore/check
```

This avoids requiring an eager migration just to populate mode metadata.

---

## 8. Profile layout

Example:

```text
config/
├── config.toml
├── files/
│   ├── hypr/
│   │   └── bindings.lua
│   ├── ghostty/
│   │   └── config
│   └── lazygit/
│       └── config.yml
└── baseline/
    ├── hypr/
    │   └── bindings.lua
    └── some-app/
        └── default.conf
```

Rules:

- `config/files/<path>` exists for every saved `ConfigFile`;
- `config/baseline/<path>` exists for every saved file with `BaselineHash`;
- `config/baseline/<path>` also exists for every tombstone;
- added files have no baseline snapshot;
- unchanged baseline files are not stored;
- excluded/delegated/volatile/sensitive paths are not stored.

---

## 9. Provider ownership and delegation

Config v2 scans broadly but owns narrowly.

Before classifying content, it must know paths owned by more specific providers.

### 9.1 Strong semantic ownership

These paths are not Config-owned:

```text
Themes user root
→ recursive Themes ownership

Plugins user root
→ recursive Plugins ownership

Hooks root
→ recursive Hooks ownership

Shell config
→ exact Shell ownership

Resources tracked roots inside ~/.config
→ recursive/exact Resources ownership

Resources managed inbound link sources
→ exact Resources ownership
```

The active Blueprint profile and state roots are also excluded if they ever overlap the scan root.

### 9.2 Hooks and symlink delegation

Hooks may delegate a symlink leaf to Resources.

Config sees neither as its own state.

Example:

```text
~/.config/omarchy/hooks/theme-set
    → ~/omarchy-setup/hooks/theme-set
```

Ownership is:

```text
Hooks owns hook semantics
Resources owns the symlink relationship
Config owns nothing at that path
```

### 9.3 Generic Config ownership is intentionally defeasible

Config v2 should **not** register all of `~/.config` as a strong recursive ownership claim against Resources.

That would prevent explicit Resources use.

Instead:

- specific semantic Config baseline files are Config-managed through the Config provider;
- generic user-added Config entries are captured only when no stronger provider claims them;
- explicitly tracked Resources and managed ResourceLinks take precedence during future Config capture.

This preserves the user's ability to evolve from:

```text
Blueprint-captured ~/.config/nvim files
```

to:

```text
~/dotfiles/nvim
+ ResourceLink ~/.config/nvim
```

without defining the entire `.config` tree as permanently Config-owned.

### 9.4 Dual ownership must fail Check

A persisted profile must not intentionally restore the same destination from two providers.

`check` should report dual ownership if a saved Config entry still overlaps a saved Resources destination/link or another provider's exact/recursive claim.

Aggregate capture order should normally clean this up automatically because Resources runs before Config.

---

## 10. Scanner model

Introduce a recursive Config scanner with deterministic classification.

Conceptual types:

```go
type Classification string

const (
    ConfigUnchangedBaseline Classification = "unchanged-baseline"
    ConfigModifiedBaseline  Classification = "modified-baseline"
    ConfigDeletedBaseline   Classification = "deleted-baseline"
    ConfigAdded             Classification = "added"
    ConfigDelegated         Classification = "delegated"
    ConfigExcluded          Classification = "excluded"
    ConfigVolatile          Classification = "volatile"
    ConfigSensitive         Classification = "sensitive"
    ConfigSymlink           Classification = "unmanaged-symlink"
    ConfigUnsupported       Classification = "unsupported"
)

type Candidate struct {
    Path           string
    Classification Classification

    UserHash       string
    UserMode       string
    BaselineHash   string
    BaselineMode   string

    Reason         string
}
```

The scanner should produce a complete classified view.

Capture decides which classifications become profile state.

Status can summarize the rest.

---

## 11. Tree traversal

The scanner considers the union of paths in:

```text
configuration surfaces:
- recursive `$HOME/.config/**`
- curated exact `$HOME`-relative paths

primary Omarchy baseline root for `.config/**`:
`/usr/share/omarchy/config`
or active `OMARCHY_PATH/config`

optional curated-path baselines:
resolved only from a trustworthy installed package/template mapping
```

Both roots are walked with `Lstat`/`WalkDir`.

Rules:

- never follow symlink directories;
- user paths are always stored relative to `$HOME`;
- `.config/**` baselines map by stripping the `.config/` prefix; curated home paths use an explicit baseline resolver and otherwise have no baseline;
- deterministic lexical order;
- claimed recursive provider roots are `SkipDir`;
- excluded recursive paths are `SkipDir`;
- known volatile directories are `SkipDir`;
- Blueprint backup paths are ignored;
- special files are not traversed as normal files.

The scanner must not read arbitrary targets through symlinks.

---

## 12. Baseline classification

For each regular-file relative path:

### 12.1 Both user and baseline exist

Compute hashes.

```text
user hash == baseline hash
→ unchanged baseline
→ not captured
```

```text
user hash != baseline hash
→ modified baseline
→ capture desired + baseline
```

### 12.2 User exists, baseline missing

```text
regular user file
→ added
→ capture by default
```

### 12.3 Baseline exists, user missing

```text
baseline file absent from user's tree
→ deleted baseline
→ capture tombstone
```

This is valid because the baseline tree represents shipped user config material.

### 12.4 Both missing

No state.

### 12.5 Type mismatch

Examples:

```text
baseline regular file
user directory

baseline regular file
user symlink
```

Do not silently reinterpret.

Classify as conflict/unsupported for capture unless another provider owns the user-side object.

Report clearly.

---

## 13. Tombstones

Tombstones preserve meaningful absence.

Example:

```text
baseline:
some-app/default.conf

user:
missing
```

Profile:

```toml
[[delete]]
path = "some-app/default.conf"
baseline_hash = "..."
baseline_mode = "0644"
```

Stored baseline:

```text
config/baseline/some-app/default.conf
```

Tombstones must never apply to:

- user-added files that simply disappeared;
- provider-owned paths;
- excluded paths;
- baseline directories;
- arbitrary target-only files.

Only a captured baseline file can produce a tombstone.

---

## 14. Exclusions

Users may opt config paths out persistently.

Example:

```toml
excluded = [
  "google-chrome",
  "discord",
  "some-app/generated.conf",
]
```

### 14.1 Exclusion semantics

An exclusion is a normalized relative path.

It excludes:

```text
the exact path
and all descendants
```

No glob syntax is needed in Config v2.

This keeps validation and UI predictable.

### 14.2 CLI

Extend the existing include/exclude commands with typed refs while preserving bare package behavior:

```bash
omarchy-blueprint exclude config:google-chrome
omarchy-blueprint include config:google-chrome

omarchy-blueprint exclude config:some-app/generated.conf
```

Existing:

```bash
omarchy-blueprint exclude firefox
```

continues to mean package exclusion.

Accepted config input may also normalize:

```text
config:~/.config/ghostty
config:ghostty
```

to:

```text
ghostty
```

### 14.3 Exclusions survive capture

Capture never silently removes an exclusion just because the path is currently absent.

`include config:<path>` is the explicit way to remove it.

---

## 15. Built-in volatile/runtime policy

Config v2 should not assume that every byte below `~/.config` is durable configuration.

A small built-in high-confidence filter should reject obvious runtime/cache state.

Initial examples include path components or objects such as:

```text
Cache
Code Cache
GPUCache
DawnCache
ShaderCache
Crash Reports
blob_storage
Session Storage
Service Worker
logs
tmp
temp
SingletonLock
SingletonSocket
*.lock
*.pid
```

Sockets, FIFOs, devices, and other special filesystem objects are unsupported regardless of name.

### 15.1 Conservative matching

The built-in filter should prefer false negatives over false positives.

Do not use broad rules such as:

```text
anything named "state"
anything named "data"
```

because those names often contain real configuration.

### 15.2 Reporting

Skipped volatile content is summarized:

```text
Ignored volatile/runtime config
  23 paths
```

Verbose/JSON output may include reasons per path.

### 15.3 User control

Built-in volatile paths are not converted into persistent user exclusions automatically.

They are policy classifications.

Future versions may expose override/include policy for advanced cases.

---

## 16. Sensitive material

Config capture runs sensitive-path/content checks before persisting bytes.

High-confidence examples include:

```text
private keys
credential files
token stores
cookie/login databases
obvious auth secrets
```

SQLite files whose role is clearly browser/login/session storage should not be treated as ordinary portable config.

### 16.1 Default behavior

High-confidence sensitive content is **not captured**.

Capture reports it.

Example:

```text
Sensitive config skipped
  ~/.config/example/credentials.json
```

### 16.2 No force-secrets in Config v2

`--force` affects restore conflicts.

It does not authorize secret capture.

A future explicit secrets policy can be designed separately.

---

## 17. Size and resource limits

Config is not a general backup provider.

Use a per-file safety limit for automatic capture.

Recommended initial limit:

```text
16 MiB per regular file
```

Files larger than the limit are skipped and reported.

Rationale:

- ordinary config files are far smaller;
- large databases/profile artifacts are usually state rather than declarative configuration;
- this prevents accidental profile blow-up.

No total-tree byte quota is required in the first version.

The per-file threshold should be a named constant with tests, not scattered magic numbers.

---

## 18. Text vs opaque binary config

Config v2 captures regular binary files when they pass sensitive, volatile, size, and ownership checks.

User-added SQLite databases are skipped by default as opaque application state unless they correspond to an Omarchy baseline file. This avoids treating browser/session/token databases as ordinary configuration. Baseline-backed opaque files may still be captured exactly.

Text classification exists only to determine merge eligibility.

A file is mergeable text when:

- valid UTF-8;
- contains no NUL byte;
- size is within the merge engine's practical limit.

Recommended merge limit:

```text
4 MiB
```

Anything else is opaque bytes.

Opaque files can still be restored safely when provenance proves the destination is unchanged.

They simply cannot participate in three-way text merge.

---

## 19. Capture algorithm

Capture should:

1. load persistent exclusions;
2. build semantic ownership/delegation index;
3. scan user and baseline roots;
4. classify all paths;
5. refuse/flag high-confidence sensitive material;
6. select:
   - modified baseline files;
   - user-added files;
   - baseline tombstones;
7. stage profile snapshots under `config/.capture-*`;
8. copy desired files without following symlinks;
9. copy baseline files required by modified entries and tombstones;
10. compute and validate hashes/modes from the staged copy;
11. sort metadata by path;
12. atomically swap staged `files/` and `baseline/` trees;
13. return the new Config state;
14. persist profile metadata through the existing profile save boundary.

Capture must never partially adopt an unmanaged symlink.

---

## 20. Capture status UX

The user should not be drowned in unchanged defaults.

Default human output should emphasize meaningful state.

Example:

```text
Config

Modified
  ~/.config/hypr/bindings.lua
  ~/.config/ghostty/config

Added
  ~/.config/lazygit/config.yml
  ~/.config/btop/btop.conf

Deleted
  ~/.config/example/default.conf

Delegated
  ~/.config/nvim → resource:dotfiles/nvim

Ignored
  143 unchanged Omarchy files
  18 volatile/runtime paths
  2 provider-owned trees

Skipped
  1 sensitive path
  1 oversized file
```

Normal output need not print every ignored path.

`--json` includes structured counts and per-path reasons.

---

## 21. Diff semantics

Diff remains:

```text
saved profile
vs
current machine
```

Changes include:

```text
+ config ghostty/config
~ config hypr/bindings.lua
- config lazygit/config.yml
~ config delete some-app/default.conf no longer satisfied
```

Unchanged baseline files do not appear.

Provider-owned delegated paths do not appear as Config drift.

A target-only added config file appears as additive drift but does not fail Verify.

---

## 22. Restore categories

Restore logic distinguishes three saved intents:

```text
1. user-added file
2. modified baseline file
3. tombstone
```

Each has different provenance rules.

---

## 23. Restoring user-added files

Saved state:

```text
BaselineHash == ""
```

### Target missing

```text
→ create saved file
→ low risk
```

### Target equals saved hash

```text
→ no-op
```

### Target exists and differs

```text
→ conflict
→ preserve target
```

With `--force`:

```text
→ verify target precondition
→ create recoverable backup
→ replace with saved file
→ high risk
```

### Target is symlink/directory

Default:

```text
→ conflict
→ preserve
```

Force replaces only through the existing bounded backup/precondition machinery.

Never `RemoveAll` an unknown target.

---

## 24. Restoring modified baseline files

Saved state:

```text
captured baseline A
captured desired B
```

Current Omarchy provides:

```text
current baseline C
```

Live target is:

```text
T
```

### 24.1 Baseline unchanged

If:

```text
hash(A) == hash(C)
```

the desired candidate is simply:

```text
B
```

Safe target cases:

```text
T missing
T == A
T == B
```

Result:

```text
missing / old baseline
→ write B

already B
→ no-op
```

Unknown target:

```text
T differs from A and B
→ conflict
```

### 24.2 Baseline changed and text merge is available

If:

```text
A != C
```

and A, B, C are mergeable text:

```text
M = merge3(base=A, desired=B, newBaseline=C)
```

If merge is clean:

```text
M becomes the desired restore candidate
```

Safe target cases:

```text
T missing
T == C
T == A
T == M
```

Result:

```text
T missing / current baseline / captured baseline
→ write M

T == M
→ no-op
```

If T differs from all known safe states:

```text
→ preserve target
→ report target drift conflict
```

### 24.3 Baseline changed but merge conflicts

Default:

```text
→ no write
→ report three-way merge conflict
→ preserve target
```

Do not emit conflict markers into the live file.

### 24.4 Baseline changed but file is opaque

Default:

```text
→ no automatic rebase
→ preserve target
→ report non-mergeable baseline change
```

### 24.5 Current baseline missing

If the new Omarchy version no longer ships the baseline path:

```text
target missing
→ restore captured desired B as a user-owned file
→ medium risk + warning that upstream baseline disappeared
```

If target exists and differs from B:

```text
→ conflict
```

---

## 25. Three-way merge engine

Introduce a focused interface:

```go
type MergeResult struct {
    Content   []byte
    Conflicts bool
}

func MergeText3(base, desired, currentBaseline []byte) (MergeResult, error)
```

Requirements:

- deterministic;
- line-oriented;
- pure Go at runtime;
- no dependency on an external `git` or `diff3` command;
- no conflict markers written to the target;
- tests for insert/delete/edit interactions;
- tests for CRLF/LF handling;
- identical inputs fast-path;
- bounded by the mergeable-text size limit.

Implementation may use a small maintained pure-Go library behind this interface, but the provider must depend on the interface rather than library-specific behavior.

### 25.1 Merge direction

The semantic operation is:

```text
take the user's delta:
A → B

apply it to:
C
```

The merge engine must not accidentally privilege the new baseline over explicit captured user edits.

---

## 26. Unknown target edits are not four-way merged

Suppose:

```text
captured baseline A
captured user desired B
new baseline C
target user's own changes T
```

Config v2 does **not** attempt:

```text
A/B/C/T four-way reconciliation
```

If T is not one of the known safe states:

```text
→ conflict
→ preserve T
```

This prevents Blueprint from combining unrelated target-machine user work without explicit intent.

A future semantic conflict assistant may help here.

---

## 27. `--force` for Config

Config becomes the next provider with explicit bounded force semantics.

### 27.1 Force never skips backup

For a conflicting destination, force must:

1. validate the approved target precondition;
2. preserve the existing target object;
3. perform the desired write/delete;
4. journal the backup location;
5. mark the operation reversible;
6. rollback safely if the mutation fails;
7. never overwrite work that appeared after approval.

### 27.2 Force candidate selection

If a clean cross-version merge exists:

```text
force writes merged candidate M
```

If the merge itself conflicts:

```text
force writes captured desired B
```

This is explicit:

> The user's captured configuration wins, even if that discards new upstream baseline changes.

The dry-run must say so.

### 27.3 Risk

```text
create missing added file
→ low

write modified baseline over a proven baseline state
→ medium

three-way merged baseline write
→ medium

delete a baseline file from a proven baseline state
→ high, reversible

force-replace unknown target
→ high, reversible

force-delete unknown target
→ high, reversible
```

---

## 28. Tombstone restore

Saved:

```text
delete path P
captured baseline A
```

Current baseline:

```text
C, maybe absent
```

Target:

```text
T
```

### Target missing

```text
→ satisfied
```

### Target equals captured baseline A

```text
→ backup T
→ delete
```

### Target equals current baseline C

Even if C changed since capture:

```text
→ Blueprint knows T is still upstream baseline, not unknown user work
→ backup T
→ delete
```

This honors the persisted "file absent" intent.

### Target differs from both known baselines

Default:

```text
→ conflict
→ preserve
```

Force:

```text
→ backup target
→ delete
```

### Current baseline no longer exists

If target missing:

```text
→ satisfied
```

If target exists:

```text
→ unknown target work
→ preserve unless force
```

---

## 29. Delete executor model

Introduce an explicit safe delete operation rather than encoding deletion as an empty FileWrite.

Conceptual model:

```go
type FileDelete struct {
    Destination string `json:"destination"`

    ExpectedHash string `json:"expected_hash,omitempty"`
    ExpectedMode uint32 `json:"expected_mode,omitempty"`

    Backup               bool `json:"backup,omitempty"`
    RejectSymlinkParents bool `json:"reject_symlink_parents,omitempty"`
}
```

The final implementation may use a more general filesystem precondition shared with Resources.

Requirements:

- `Lstat`, never follow destination symlink;
- parent-symlink protection;
- precondition immediately before mutation;
- backup before delete;
- no-recursive destructive delete;
- if target is a directory/symlink under force, use bounded object-preserving backup semantics rather than dereference/delete;
- rollback if the final mutation fails;
- preserve a concurrently recreated destination.

---

## 30. Restore operation ordering

Config operations are independent by path except:

```text
parent directory creation
→ file write
```

and:

```text
Hyprland config mutation(s)
→ hyprctl reload
```

Do not add application-specific restart operations for arbitrary config directories.

Resources restore remains earlier in provider order.

Therefore a Config scan/plan can correctly regard Resource-owned link paths as delegated and avoid replacing them.

---

## 31. Hyprland reload behavior

Config v1 reloads Hyprland after managed Hyprland config changes.

Retain that behavior.

Config v2 should add:

```text
hyprctl reload
```

only if an applied Config mutation affects a managed Hyprland configuration path.

At minimum:

```text
hypr/*.lua
```

or the exact known Omarchy Hyprland user-config set.

Do not run `hyprctl reload` because `ghostty/config` or `lazygit/config.yml` changed.

Reload remains dependency-gated on successful writes.

---

## 32. Package-to-config association hints

Package exclusion and Config capture remain separate desired-state decisions.

Blueprint provides **advisory** association when confidence is high.

Example:

```text
Package excluded:
  ghostty

Related config still captured:
  ~/.config/ghostty

To exclude it too:
  omarchy-blueprint exclude config:ghostty
```

### 32.1 Association confidence

Initial association sources may include:

1. exact top-level config directory == package name;
2. a small curated mapping for obvious high-value mismatches;
3. package aliases already known by Blueprint.

Examples:

```text
neovim → nvim
google-chrome → google-chrome
ghostty → ghostty
```

### 32.2 No automatic exclusion

Even at high confidence:

```text
exclude package
≠ automatically exclude config
```

Reasons:

- user may want config ready before reinstalling later;
- package names and config roots are not a universal identity system;
- a config directory may be shared;
- preserving state is safer than silently dropping it.

The TUI can later present paired checkboxes.

### 32.3 Non-interactive behavior

Package exclusion never prompts in scripted mode.

The config stays captured until the user explicitly excludes it.

---

## 33. Status grouping for package associations

Status groups a high-confidence advisory:

```text
Excluded package
  ghostty

Captured related config
  ~/.config/ghostty
```

This is informational, not drift and not a check failure.

---

## 34. Check

`omarchy-blueprint check` must validate:

### Metadata

- unique normalized Config file paths;
- unique normalized Config directory paths;
- no duplicate files/tombstones;
- file and tombstone sets disjoint;
- every saved directory is required by at least one saved file;
- exclusions normalized and unique;
- no path escape;
- no path equals `.`;
- valid mode strings;
- no Config entry overlaps a stronger saved provider ownership claim.

### Desired snapshots

For every ConfigFile:

```text
config/files/<path>
→ regular non-symlink file
→ hash matches
→ mode compatible with metadata
```

### Baseline snapshots

For every ConfigFile with `BaselineHash` and every tombstone:

```text
config/baseline/<path>
→ regular non-symlink file
→ hash matches
```

Added files must not require a baseline snapshot.

### Sensitive persistence

`check` should run the high-confidence sensitive scanner against persisted Config snapshots.

A profile must not pass Check if Config contains material the capture policy would have refused.

---

## 35. Verify

Verify asks:

> Is every saved Config intent satisfied?

### Saved file

Satisfied when live target contains the exact desired **effective** state.

For unchanged-capture-version restore:

```text
live hash == saved desired hash
```

For a clean cross-version merge:

```text
live hash == merge candidate hash
```

The provider may recompute the merge candidate from saved baseline/desired plus current Omarchy baseline.

### Tombstone

Satisfied when:

```text
target missing
```

### Extra target-only config

Does not make Verify fail.

It may appear as additive drift in Status.

---

## 36. Capture idempotence

Repeated capture without live changes must produce:

```text
no semantic Config changes
same normalized config.toml
same file hashes
same baseline hashes
```

Ordering is lexical by path.

Capture must not fluctuate based on filesystem enumeration order.

---

## 37. Baseline changes during capture

Capture compares the live user file against the current Omarchy baseline.

If Omarchy updated since the previous capture:

- a user file equal to the new baseline is no longer custom and drops out;
- a user file differing from new baseline is captured against the new baseline;
- a previously captured customization may therefore be rebased at capture time simply because the running system is now based on the new Omarchy version.

Capture does not try to preserve the old baseline once the user has intentionally captured again on the upgraded machine.

The new capture becomes authoritative.

---

## 38. Baseline disappearance during capture

If a previously baseline-backed Config file no longer exists in the current Omarchy baseline but still exists in the user's `~/.config`:

```text
it becomes a user-added Config file
```

The next capture stores:

```text
desired file only
BaselineHash = ""
```

This is a natural transition.

If both old baseline and user file disappear:

```text
no saved Config file remains
```

unless the current Omarchy baseline still contains the path and therefore produces a tombstone.

---

## 39. Config directories

Config v2 persists directory mode metadata for directories required by captured files.

Rules:

- record every user-side directory that is an ancestor of at least one captured Config file, stopping at `~/.config`;
- store normalized relative path + permission mode;
- do not capture an otherwise-empty directory;
- do not create directory tombstones;
- restore missing directories before child files;
- newly created directories use the captured mode;
- existing target directories are never chmodded merely to match the profile;
- an existing non-directory at a required directory path is a conflict;
- `--force` may resolve that conflict only through the same backup/precondition rules as other Config replacements.

This preserves private directory modes such as `0700` without turning Config into a recursive directory-state provider.

---

## 40. Directory metadata validation

`ConfigDirectory` entries are part of the schema-8 contract.

They must be:

- unique by normalized relative path;
- ancestors of at least one saved Config file;
- disjoint from stronger provider-owned roots;
- free of path escape;
- valid permission modes.

Capture omits `~/.config` itself from this table.

Check rejects orphaned directory metadata that no saved file needs.

---

## 41. Sensitive and volatile classification ordering

Classification order matters.

Recommended order:

```text
1. normalize path
2. Blueprint backup/internal path?
3. user exclusion?
4. stronger provider ownership?
5. symlink?
6. special object?
7. high-confidence sensitive?
8. high-confidence volatile?
9. size limit?
10. baseline/user comparison
11. text-vs-opaque merge capability
```

Reason:

- never dereference a symlink merely to scan content;
- never read a provider-owned tree unnecessarily;
- never persist a secret just because it is also "added config";
- avoid expensive hashing/reads for content already excluded.

---

## 42. Unmanaged symlink UX

Example:

```text
~/.config/foo
    → ~/some-untracked-directory/foo
```

Config output:

```text
Skipped unmanaged symlink
  ~/.config/foo
  target is not represented by Portable Resources
```

If target is inside HOME, future TUI may suggest:

```text
Track ~/some-untracked-directory?
```

CLI Config v2 does not auto-track it.

Broken and external symlinks are similarly reported, never dereferenced.

---

## 43. ResourceLink handoff

If Config currently has captured bytes at:

```text
nvim/init.lua
```

and the user later changes the live system to:

```text
~/.config/nvim
→ ~/dotfiles/nvim
```

with `dotfiles` tracked by Resources:

next aggregate capture should result in:

```text
Resources:
  owns ~/.config/nvim link

Config:
  removes saved nvim/* entries from desired Config state
```

Diff should make this handoff visible without calling it data loss:

```text
Config
  - nvim/init.lua delegated to Resources

Resources
  + link ~/.config/nvim → resource:dotfiles/nvim
```

No provider should retain duplicate restore intent.

---

## 44. Capture exclusions and previously saved state

If the user runs:

```bash
omarchy-blueprint exclude config:ghostty
```

the exclusion should immediately remove existing saved Config file/tombstone entries under:

```text
ghostty
```

and remove their profile snapshots on next save/capture transaction.

The live filesystem is never modified.

Likewise:

```bash
omarchy-blueprint include config:ghostty
```

removes the exclusion but does not immediately capture bytes.

The next:

```bash
omarchy-blueprint capture config
```

adopts the live config.

This mirrors package include/exclude policy.

---

## 45. Capture warnings vs failures

Not every skipped object should abort the entire capture.

### Hard failure

Examples:

- profile staging cannot be written;
- path escape/invariant failure;
- duplicate ownership bug;
- hash changes while copying a supposedly stable source;
- sensitive bytes would otherwise be persisted due to an internal policy violation.

### Warning / classified skip

Examples:

- volatile runtime path;
- unmanaged symlink;
- special socket/FIFO;
- oversized file;
- high-confidence sensitive candidate;
- provider-owned subtree.

User-visible capture succeeds but reports skipped counts.

This is necessary for scanning a heterogeneous `.config` tree.

---

## 46. TOCTOU protection during capture

For each selected regular file:

1. `Lstat`;
2. open without following symlink;
3. copy to staging;
4. hash staged copy;
5. re-stat/re-hash source where needed;
6. fail that capture if source identity/content changed unexpectedly.

Config capture must not turn a file into a followed symlink if it changes during scanning.

Use existing hardened content helpers where possible.

---

## 47. TOCTOU protection during restore

Every mutation retains the existing restore safety model:

- reject symlinked parents;
- validate destination precondition immediately before mutation;
- hash-guard profile source bytes;
- write through same-directory temp + atomic rename;
- backup before forced destructive replacement;
- no-replace rollback;
- preserve concurrently-created work.

Config v2 should reuse Resources' hardened filesystem primitives rather than invent parallel unsafe variants.

This feature is a good reason to extract any duplicated generic backup/precondition helpers into `internal/restore` if the resulting API is clearer.

Do not refactor unrelated restore behavior.

---

## 48. Restore dry-run examples

### Clean cross-version merge

```text
MEDIUM config
  merge ~/.config/hypr/bindings.lua
  baseline changed since capture
  three-way merge clean
```

### Merge conflict

```text
Skipped
  config:hypr/bindings.lua
  three-way merge conflict; target preserved
```

### Force merge conflict

```text
HIGH config
  replace ~/.config/hypr/bindings.lua
  three-way merge conflicted
  captured user version will win
  existing target will be backed up
```

### Tombstone

```text
HIGH config
  delete ~/.config/example/default.conf
  captured user intent is file absent
  current file matches Omarchy baseline
  existing file will be backed up
```

### Added config conflict

```text
Skipped
  config:ghostty/config
  target differs from captured user config
```

---

## 49. JSON model

Machine-readable scan/diff output should expose classifications rather than only human strings.

Example conceptual shape:

```json
{
  "path": "hypr/bindings.lua",
  "classification": "modified-baseline",
  "hash": "...",
  "baseline_hash": "...",
  "mergeable_text": true
}
```

Skipped entries include:

```json
{
  "path": "example/credentials.json",
  "classification": "sensitive",
  "reason": "high-confidence credential path"
}
```

Do not emit secret contents.

---

## 50. CLI surface

Existing commands remain:

```bash
omarchy-blueprint capture config
omarchy-blueprint status config
omarchy-blueprint diff config
omarchy-blueprint restore config
omarchy-blueprint restore config --force
omarchy-blueprint check
```

Extend policy commands:

```bash
omarchy-blueprint exclude config:ghostty
omarchy-blueprint include config:ghostty
```

Optional future convenience:

```bash
omarchy-blueprint config excluded
```

is not required for Config v2 if `--json` / profile TOML already exposes exclusions.

Avoid adding multiple redundant policy surfaces in one release.

---

## 51. Schema migration

Schema increments:

```text
7 → 8
```

### 51.1 Loading schema 7

A schema-7 Config profile contains fixed entries with:

```text
id
path
hash
baseline_hash
```

and already stores snapshots at:

```text
config/files/<path>
config/baseline/<path>
```

Schema 8 can reinterpret those entries by path.

The old `id` is not needed for restore identity.

### 51.2 No destructive rewrite on load

Load upgrades schema in memory as today.

Do not rewrite the user's profile simply because it was loaded.

The next normal Save/Capture may serialize schema-8 Config metadata.

### 51.3 Legacy restore before recapture

A loaded schema-7 Config entry must remain restorable immediately.

Missing `Mode` fields use snapshot file mode.

### 51.4 Existing fixed files

The previous four Hyprland files become ordinary baseline-backed Config entries.

Their behavior should remain compatible, except baseline changes may now receive a clean three-way merge instead of always reporting migration-required.

---

## 52. Existing Config behavior intentionally changed

Config v1 behavior:

```text
captured baseline changed
→ migration required
→ no merge
```

Config v2:

```text
captured baseline changed
→ attempt three-way merge if text
→ clean merge: plan merged result
→ conflict/non-mergeable: preserve target
```

This is a deliberate capability expansion.

All conservative target-drift behavior remains.

---

## 53. Application reloads

Only existing known native reload behavior is retained.

Config v2 does not attempt to infer:

```text
ghostty reload
nvim reload
browser restart
lazygit reload
```

Restored config is durable state and becomes active according to the application lifecycle.

Future semantic providers may add app-native operations where useful.

---

## 54. Package/config advisory UX

When a package is explicitly excluded and Blueprint can identify a likely config root, show a non-blocking hint.

Example:

```text
Excluded package: neovim

Related Config state remains included:
  ~/.config/nvim

Run:
  omarchy-blueprint exclude config:nvim
```

Do not show low-confidence guesses.

No warning is needed if the Config path is already excluded or delegated to Resources.

---

## 55. Testing architecture

Tests should be split around independent units.

### 55.1 Scanner tests

Cover:

- baseline same;
- baseline modified;
- baseline missing/user added;
- baseline exists/user missing tombstone;
- exact exclusion;
- excluded subtree;
- provider-owned subtree;
- tracked ResourceLink delegation;
- unmanaged symlink;
- broken symlink;
- special file;
- volatile path;
- sensitive path;
- oversized file;
- deterministic ordering.

### 55.2 Profile tests

Cover:

- schema 8 read/write;
- schema 7 Config metadata load;
- legacy `id` ignored/migrated;
- path normalization;
- duplicate path rejection;
- file/tombstone overlap rejection;
- exclusion normalization;
- snapshot layout.

### 55.3 Merge tests

Cover:

- baseline unchanged;
- independent edits merge;
- user edit + upstream insertion;
- user deletion + upstream edit;
- overlapping conflict;
- binary/non-text rejection;
- size limit;
- line endings;
- deterministic output;
- no live conflict markers.

### 55.4 Plan tests

Cover:

- added target missing;
- added target same;
- added target conflict;
- modified baseline same;
- modified baseline cross-version clean merge;
- modified baseline merge conflict;
- opaque baseline changed;
- target unknown drift;
- force replacement;
- tombstone target captured baseline;
- tombstone target current baseline;
- tombstone unknown drift;
- force tombstone.

### 55.5 Executor tests

Cover:

- source hash guard;
- parent symlink guard;
- file backup;
- symlink backup;
- directory-at-file-path force backup;
- delete backup;
- rollback;
- rollback blocked by concurrent destination;
- no overwrite of backup collision;
- exact modes despite restrictive umask.

### 55.6 Provider handoff tests

Cover:

- Resources-owned link excluded from Config;
- Hooks-owned root excluded;
- Themes-owned root excluded;
- Plugins-owned root excluded;
- Shell exact file excluded;
- stale Config bytes disappear after resource delegation;
- dual persisted ownership fails Check.

### 55.7 App/CLI tests

Cover:

- aggregate capture output grouped;
- unchanged baseline count not verbose;
- config exclusion commands;
- package exclusion advisory;
- restore `--force` plumbing;
- JSON classification;
- Hypr reload only for relevant config changes.

---

## 56. Manual acceptance scenarios

### Scenario A — ordinary non-dotfiles user

Machine has:

```text
~/.config/hypr/bindings.lua       modified from Omarchy baseline
~/.config/ghostty/config          user-added
~/.config/lazygit/config.yml      user-added
```

Capture:

```bash
omarchy-blueprint capture config
```

Profile contains exactly those meaningful files plus the captured baseline for `hypr/bindings.lua`.

Unchanged Omarchy baseline files are absent from the profile.

### Scenario B — fresh restore

Fresh machine has current Omarchy defaults.

Restore:

```bash
omarchy-blueprint restore config
```

Expected:

- user-added configs created;
- baseline-backed config restored;
- target-only config preserved;
- no overwrite of unknown target edits.

### Scenario C — Omarchy upgraded cleanly

Captured:

```text
A = old hypr/bindings.lua baseline
B = user's edited bindings
```

Target Omarchy ships:

```text
C = new baseline
```

If user delta and upstream delta do not overlap:

```text
restore produces clean M
```

### Scenario D — cross-version merge conflict

A/B/C overlap.

Normal restore:

```text
skip
preserve target
report merge conflict
```

Force:

```text
backup target
write captured B
```

### Scenario E — deleted baseline config

Captured user deleted a shipped file.

Fresh target has current untouched baseline.

Restore:

```text
backup target file
delete it
```

Verify passes when absent.

### Scenario F — dotfiles handoff

Tracked resource:

```text
~/dotfiles
```

Link:

```text
~/.config/nvim → ~/dotfiles/nvim
```

Config:

```text
does not capture/dereference nvim
```

Resources owns the relationship.

### Scenario G — package excluded

Package:

```text
neovim
```

is excluded.

Config:

```text
~/.config/nvim
```

remains captured.

Blueprint emits an advisory, not an automatic exclusion.

### Scenario H — noisy browser/runtime state

Large/runtime/sensitive browser profile artifacts are not blindly dumped into the profile.

Capture succeeds with a concise skipped summary.

---

## 57. Acceptance criteria

Config v2 is complete when all of the following hold.

1. `capture config` recursively discovers `~/.config/**` and inspects the curated exact home-config registry.
2. Unchanged Omarchy baseline files are not persisted.
3. Modified baseline files persist desired + captured baseline bytes.
4. User-added regular config files are captured by default.
5. Baseline files missing from the user's tree become explicit tombstones.
6. Excluded paths are persistent and suppress their subtree.
7. Provider-owned paths are delegated rather than duplicated.
8. Managed ResourceLinks are not captured as Config bytes.
9. Unmanaged symlinks are never dereferenced.
10. Sensitive paths are not persisted.
11. Volatile/runtime paths are not persisted.
12. Oversized files are skipped with a visible reason.
13. Binary files may be captured but are never text-merged.
14. Cross-version text baseline changes attempt deterministic three-way merge.
15. Clean merge results can restore automatically when the target has no unknown user work.
16. Merge conflicts preserve the target by default.
17. Unknown target edits preserve the target by default.
18. `--force` backs up before replacing a conflicting target.
19. `--force` on a merge conflict writes the captured user version, not conflict markers.
20. Tombstones delete only provenance-safe targets by default.
21. Forced tombstone deletion backs up the target first.
22. Target-only config is never removed merely because it is not saved.
23. Schema-7 Config profiles still load and restore.
24. The four existing Hyprland config files remain supported.
25. Hyprland reload remains dependency-gated and only runs after relevant successful mutations.
26. Package/config association is advisory only.
27. Check detects profile corruption and dual ownership.
28. Verify ignores extra target-only config but requires all saved Config intent.
29. Missing parent directories are recreated with captured modes; existing directory modes are preserved.
30. Capture output groups unchanged/delegated/skipped state instead of printing thousands of default files.
31. `go test ./...`, `go vet ./...`, CLI build, and `git diff --check` pass.

---

## 58. Anti-regressions

The implementation must not introduce any of these behaviors:

```text
copy all of ~/.config blindly
follow config symlinks
capture symlink targets as bytes
claim themes/plugins/hooks/shell twice
capture tracked dotfiles bytes through Config
delete target-only config
overwrite target drift without force
write merge conflict markers live
force without a recoverable backup
RemoveAll unknown target directories
silently capture credentials/tokens
persist caches/sockets/session runtime state
make package exclusion silently drop config
require Git just to merge Config
make all of ~/.config or all of $HOME a strong Resources ownership conflict
```

---

## 59. Likely implementation boundaries

The implementation plan should prefer focused units:

```text
internal/providers/config/
├── provider.go          provider orchestration
├── scan.go              recursive classification
├── policy.go            exclusions / volatile / sensitive decisions
├── merge.go             text 3-way merge interface
├── plan.go              restore candidate + conflict planning
├── diff.go              diff / verify
└── *_test.go

internal/restore/
├── existing file-write primitives
└── safe delete / generic backup helper if required

internal/profile/
└── schema-8 Config metadata and validation

internal/ownership/
└── reuse existing claims; only extend if required for soft/delegated ownership
```

Do not leave the expanded feature as one oversized `provider.go`.

---

## 60. Roadmap impact

After Config v2:

```text
Portable Resources v1
✓

Config v2 — baseline-aware ~/.config
→ next

Git Resources v2
  git
  git+diff
  copy

Machine overlays / path mappings

TUI discovery and policy management
```

Config v2 deliberately improves the non-dotfiles path before adding richer dirty-Git preservation.

---

## 61. Future extensions

Explicitly deferred:

### 61.1 Rich Config include policy

Allow a user to opt a normally skipped binary/volatile subtree back in.

### 61.2 Curated application semantics

Some config roots may eventually become first-class providers.

### 61.3 Package/config TUI grouping

Paired application/package/config controls.

### 61.4 AI-assisted merge explanation

Explain difficult merge conflicts without automatically applying them.

### 61.5 Target user-work merge

A future conflict workflow may attempt to combine saved user intent with target-only user edits.

### 61.6 Secret provider

Encrypted/explicitly-authorized secret state must be a separate design.

---

## 62. Decision summary

Config v2 is a baseline-aware home-configuration overlay provider for recursive `~/.config/**` plus curated exact `$HOME`-relative config paths.

It captures:

```text
modified shipped config
new user config
explicit deletion of shipped config
```

It does not capture:

```text
unchanged shipped defaults
semantic-provider-owned state
tracked-resource symlink targets
known volatile/runtime noise
high-confidence secrets
unsupported special objects
```

Across Omarchy versions:

```text
old baseline A
captured user desired B
new baseline C
        ↓
three-way text merge
        ↓
clean → restore merged M
conflict → preserve target
--force → backup + captured B wins
```

This gives users who do not maintain dotfiles a practical path to portable system state while preserving the same safety model established by the rest of Omarchy Blueprint.

---

## 63. Approved addendum: Omarchy update backup files

Omarchy updates may leave backup copies of user configuration with names in the form:

```text
xxx.bak.xxx
```

Examples:

```text
hyprland.lua.bak.20260909
bindings.lua.bak.before-update
config.bak.1757390000
```

Config v2 treats these as built-in ignored update artifacts.

Matching rule:

```text
basename contains ".bak."
with non-empty text before and after the marker
```

This policy applies before baseline comparison, capture selection, and tombstone generation.

Therefore a matching path is:

```text
→ classification: volatile / ignored-update-backup
→ never captured
→ never stored as a baseline snapshot
→ never interpreted as a deletion tombstone
→ never restored
```

If a matching entry is a directory, the scanner skips that subtree.

The default human summary groups these with ignored runtime/update noise. JSON may expose the more specific reason:

```json
{
  "path": "hypr/bindings.lua.bak.20260909",
  "classification": "volatile",
  "reason": "omarchy-update-backup"
}
```

This is a built-in Config policy, not a persistent user exclusion. The user does not need to maintain exclusions for backups created by future Omarchy upgrades.
---

## 64. Approved addendum: home configuration surfaces

Config v2 is broader than `~/.config`.

Its discovery boundary is:

```text
$HOME
├── .config/**                    recursive Config surface
└── curated exact paths           exact files only; no recursive HOME scan
```

This avoids an unsafe recursive scan of `$HOME` while still covering the important configuration files that predate or do not follow XDG conventions.

Profile path identity is therefore `$HOME`-relative:

```text
.config/hypr/bindings.lua
.config/ghostty/config
.bashrc
.zshrc
.tmux.conf
.XCompose
```

The profile layout mirrors that identity:

```text
config/
├── files/
│   ├── .config/
│   │   ├── ghostty/config
│   │   └── hypr/bindings.lua
│   ├── .bashrc
│   ├── .zshrc
│   ├── .tmux.conf
│   └── .XCompose
└── baseline/
    ├── .config/
    │   └── hypr/bindings.lua
    └── <curated path only when a trustworthy baseline exists>
```

There is **no** recursive `$HOME` traversal.

Curated files that do not have a trustworthy installed baseline are treated as user-added Config files.

---

## 65. Curated exact home-config registry

The default registry should be intentionally broad for portable configuration but limited to paths whose purpose is configuration rather than state, credentials, caches, histories, or package data.

The registry is a set of exact `$HOME`-relative paths. Nested exact paths are allowed; registering `.cargo/config.toml` does not authorize scanning `.cargo/**`.

### 65.1 Shell, login, and interactive environment

Capture these when present:

```text
.bashrc
.bash_profile
.bash_login
.bash_logout
.bash_aliases
.profile

.zshrc
.zprofile
.zlogin
.zlogout
.zshenv
.p10k.zsh
.zsh_plugins.txt

.inputrc
.hushlogin

.aliases
.functions
.exports
```

Notes:

- `.bashrc` is an explicit Omarchy customization surface.
- `.zshrc` is the Omarchy Zsh user customization surface.
- Fish user configuration already lives under `.config/fish/**` and is covered recursively.
- `.hushlogin` may be an empty marker file; an empty regular file is still meaningful configuration.
- generic `.aliases`, `.functions`, and `.exports` are included only as exact files when they exist; Blueprint does not invent them.

### 65.2 Terminal and multiplexers

```text
.tmux.conf
.screenrc
.wezterm.lua
.alacritty.yml
.alacritty.toml
```

Modern XDG versions under `.config/**` are already covered.

### 65.3 Editors and interactive development tools

```text
.vimrc
.gvimrc
.exrc
.nanorc
.ideavimrc

.emacs
.emacs.el
.spacemacs
```

Do not recursively capture:

```text
.vim/**
.emacs.d/**
```

in this feature. Those trees may contain packages, caches, generated state, and secrets. Users can move them to XDG config, explicitly track them with Resources, or a future registry can add safe subpaths.

### 65.4 Git and version-control preferences

```text
.gitconfig
.gitignore
.gitignore_global
.gitmessage
.gitmessage.txt
.gitattributes_global
.tigrc
```

The sensitive-content scanner still applies. In particular, a `.gitconfig` containing embedded credential-bearing URLs must be refused rather than blindly persisted.

### 65.5 CLI search, formatting, and terminal preferences

```text
.editorconfig
.dircolors
.ctags
.ackrc
.agignore
.ripgreprc
.fdignore
.ignore
.lesskey
```

Histories such as `.lesshst` are state, not config, and are not included.

### 65.6 X11 / desktop-session compatibility

```text
.XCompose
.Xresources
.Xdefaults
.xprofile
.xinitrc
.pam_environment
.gtkrc-2.0
```

`.XCompose` is particularly important on Omarchy: it is a documented user customization surface.

These are exact files only. Blueprint does not recursively capture legacy X11 directories.

### 65.7 REPL and language-tool preferences

Safe exact files worth including when present:

```text
.pythonrc
.pythonrc.py
.irbrc
.pryrc
.Rprofile
.ghci
.haskeline
.iex.exs
.ocamlinit
.sbclrc
.luacheckrc
.stylua.toml
.rustfmt.toml
.clippy.toml
.sqliterc
.psqlrc
.mongorc.js
.taskrc
.condarc
.gemrc
```

Nested exact tool config:

```text
.cargo/config
.cargo/config.toml
.julia/config/startup.jl
```

The registry does not imply that these tools are installed. Package restore remains independent.

### 65.8 Deliberately not auto-captured

These are secret-heavy, credential-heavy, runtime-heavy, or account/machine state and should be excluded by built-in policy unless a future explicit feature says otherwise:

```text
.ssh/**
.gnupg/**
.aws/**
.azure/**
.kube/**
.docker/**
.password-store/**
.local/share/keyrings/**
.netrc
.authinfo
.authinfo.gpg
.pgpass
.my.cnf
.pypirc
.npmrc
.yarnrc
.yarnrc.yml
.bundle/config
.config/gh/hosts.yml
.config/gcloud/**
.config/rclone/rclone.conf
.config/sops/age/keys.txt
.config/containers/auth.json
```

Also never auto-capture histories/state such as:

```text
.bash_history
.zsh_history
.python_history
.node_repl_history
.sqlite_history
.lesshst
.viminfo
.wget-hsts
```

A future secret provider may handle some of these explicitly and encrypted.

### 65.9 Registry evolution

The curated registry is code-owned, deterministic, and reviewable.

It may grow over time, but additions require answering:

1. Is this durable configuration?
2. Is it safe enough to inspect/capture by default?
3. Is it an exact path rather than a broad state directory?
4. Does a more semantic Blueprint provider already own it?
5. Is there a trustworthy baseline source?
6. Does its package or application commonly store credentials in the same file?

---

## 66. Baselines for curated home config

`~/.config/**` continues to use Omarchy's config baseline tree.

Curated home paths use one of two strategies:

```text
known trustworthy installed template
→ baseline-aware capture / tombstone / three-way merge

no trustworthy baseline
→ ordinary user-added Config file
```

Never guess a baseline by filename alone.

A baseline mapping is explicit:

```go
type HomeConfigSpec struct {
    Path string

    // Optional. Returns an installed source file whose bytes are the
    // authoritative template that created this user file.
    ResolveBaseline func() (string, bool)
}
```

The implementation may use a simpler data representation, but the semantic rule is explicit provenance.

### 66.1 Omarchy Zsh extension baselines

When the current Omarchy Zsh package is installed and these package files exist:

```text
.bashrc
  baseline → /usr/share/omarchy-zsh/templates/bashrc

.zshrc
  baseline → /usr/share/omarchy-zsh/templates/zshrc

.inputrc
  baseline → /usr/share/omarchy-zsh/shell/inputrc
```

This allows unchanged extension-generated files to remain out of the profile while still capturing user edits relative to the extension template.

### 66.2 Omarchy Fish extension baseline

When the current Omarchy Fish package is installed and the template exists:

```text
.bashrc
  baseline → /usr/share/omarchy-fish/templates/bashrc
```

Fish customization under:

```text
.config/fish/**
```

is handled by the normal recursive `.config` scanner.

### 66.3 Baseline selection conflict

If more than one installed provider claims a baseline for the same curated path:

```text
→ capture must not guess
→ report ambiguous baseline
→ treat as unsupported/conflict until package state is coherent
```

For example, both `omarchy-zsh` and `omarchy-fish` should not simultaneously define the authoritative `.bashrc` baseline.

---

## 67. Omarchy Zsh and Fish setup semantics

As verified against the current upstream extension scripts on 2026-09-09:

### Zsh

`omarchy-setup-zsh`:

```text
backs up ~/.zshrc → ~/.zshrc.backup-<timestamp>
backs up ~/.bashrc → ~/.bashrc.backup-<timestamp>
copies the package zshrc template to ~/.zshrc
copies the package bashrc template to ~/.bashrc
copies the package inputrc to ~/.inputrc
```

The installed `.bashrc` auto-launches Zsh for the interactive top-level Bash session.

It does **not** change the account shell in `/etc/passwd`.

### Fish

`omarchy-setup-fish`:

```text
backs up ~/.bashrc → ~/.bashrc.backup-<timestamp>
copies the package bashrc template to ~/.bashrc
```

The installed `.bashrc` auto-launches Fish.

Fish user customization remains under:

```text
~/.config/fish/config.fish
~/.config/fish/functions/**
```

It does **not** change the account shell in `/etc/passwd`.

### Config v2 consequence

The ordinary Omarchy extension behavior can be reconstructed by the combination of:

```text
Packages
  → omarchy-zsh or omarchy-fish package installed

Config
  → .bashrc / .zshrc / .inputrc / .config/fish custom state restored
```

Blueprint does not need to execute `omarchy-setup-zsh` or `omarchy-setup-fish` during restore if it already has the intended resulting Config files.

This avoids a setup script overwriting target state after Blueprint has planned it.

---

## 68. Login shell selection is separate semantic state

The actual account login shell is not Config state.

Examples:

```text
/etc/passwd user shell = /bin/bash
/etc/passwd user shell = /usr/bin/zsh
/etc/passwd user shell = /usr/bin/fish
```

Changing this is account state, may require authentication, and is not represented by files under `$HOME`.

Config v2 therefore does **not** call:

```text
chsh
usermod
```

and never edits `/etc/passwd`.

A future small feature should capture semantic login-shell intent:

```go
type LoginShell struct {
    Name string // bash | zsh | fish | ...
}
```

with rules such as:

```text
detect current account login shell
require package/provenance for non-baseline shells
plan native `chsh -s <resolved executable>`
high-risk / explicit approval
never edit /etc/passwd directly
preserve target choice by default when profile has no intent
```

This could live as a future `account`/`user` provider or a carefully scoped extension of Defaults, but should be designed separately.

It is not required to reproduce the normal Omarchy Zsh/Fish auto-launch mechanism because those extensions intentionally implement that mechanism through `.bashrc`.

---

## 69. Expanded built-in backup-noise policy

The existing Omarchy update rule:

```text
xxx.bak.xxx
```

remains.

Also ignore extension/setup backups matching:

```text
*.backup-*
```

with non-empty text before and after `backup-`.

Examples:

```text
.bashrc.backup-20260909-080102
.zshrc.backup-20260909-080102
```

Rules:

```text
→ ignored update/setup noise
→ never captured
→ never stored as baseline
→ never turned into tombstone
→ never restored
```

The rule applies across every Config surface.

Because curated home paths are exact, backup siblings are not eligible Config state anyway; this policy additionally ensures any discovery/status pass classifies them consistently instead of presenting them as unexplained target-only config.
