# Config v2: Surface-Aware Discovery and Bounded App-State Filtering

**Project:** Omarchy Blueprint
**Status:** Proposed design for user review
**Target schema:** 9
**Date:** 2026-09-10
**Companion to:** `docs/superpowers/specs/2026-09-09-config-overlay-design.md`
**Motivation:** real-machine Config v2 acceptance testing on PR #20

---

## 1. Summary

Config v2 correctly established that Blueprint should capture portable user intent rather than snapshot all bytes under `$HOME`. Real-machine testing exposed a missing layer in the design: `~/.config` is not uniformly configuration.

Many applications use XDG config roots as mixed application-profile stores containing some combination of:

- human-authored configuration;
- generated defaults;
- caches;
- SQLite databases and WAL/SHM companions;
- LevelDB stores;
- browser profiles;
- extension payloads;
- sessions and recovery data;
- history;
- telemetry;
- logs and crash dumps;
- credentials and authentication state;
- downloaded/generated assets;
- document backups.

Treating every regular file below `~/.config` as a Config candidate caused two concrete failures during acceptance testing:

1. routine `status` and `capture` became unexpectedly expensive because Blueprint recursively inspected and hashed large application state trees;
2. status reported thousands of irrelevant file-level differences, dominated by browser and Electron-style profile state.

A real-machine status sample produced 1,846 Config differences. Five application surfaces accounted for roughly 90% of them: Chromium, Zen, Typora, LibreOffice, and 1Password. The dominant browser trees were not portable configuration in the sense Blueprint intends to model.

This design adds a new layer before recursive Config discovery:

> Blueprint classifies each top-level `~/.config` application surface using a bounded metadata-only probe before deciding whether recursive automatic discovery is appropriate.

Surfaces are classified as:

```text
config-lean
state-heavy
mixed
sensitive
```

Only `config-lean` surfaces are recursively auto-discovered.

`state-heavy`, `mixed`, and `sensitive` surfaces are not recursively traversed by default. They are reported once at the surface level. Baseline-backed paths remain authoritative and are still inspected exactly. Users may explicitly include a safe subpath of an automatically skipped surface.

This design also:

- detects browser/profile stores structurally rather than by product-name blacklist;
- introduces generic backup-artifact filtering that catches `.bak` files and backup directories;
- combines sensitive-content inspection and hashing into one regular-file pass;
- makes normal `status` surface-oriented instead of dumping every discovered Config file;
- adds persistent positive Config inclusion policy;
- bumps the profile schema to 9 so older schema-8 binaries cannot silently discard positive inclusion intent.

Where this document conflicts with the Config v2 design, this document is normative for discovery, filtering, output, and Config include-policy behavior.

---

## 2. Goals

This change must:

1. make Config discovery practical on normal desktop installations with large application state stores;
2. automatically avoid browser profile trees without requiring a product-name list for Chrome, Chromium, Brave, Edge, Zen, Firefox, LibreWolf, or similar derivatives;
3. distinguish confidently portable/configuration-oriented surfaces from state-heavy and mixed application surfaces using deterministic structural evidence;
4. prefer false negatives over false positives for automatic capture: uncertain application state should be reported, not silently captured;
5. preserve Omarchy baseline-backed Config semantics even inside an otherwise skipped surface;
6. preserve explicit user control by allowing safe positive inclusion of a subpath inside a mixed/state-heavy surface;
7. continue to refuse high-confidence secrets and sensitive paths even when a user explicitly includes a parent surface;
8. ignore backup artifacts before baseline comparison, tombstone generation, sensitive inspection, and hashing;
9. avoid reading each accepted user file twice during Scan;
10. keep `status` concise and surface-oriented while retaining detailed portable file differences in `diff` and machine-readable JSON;
11. keep automatic classification deterministic and testable without wall-clock timing assertions;
12. preserve provider ownership, symlink, special-file, restore-safety, and three-way-merge behavior from Config v2.

---

## 3. Non-goals

This change will not:

- create a browser-profile backup provider;
- guarantee identification of every browser by product name;
- attempt semantic extraction of individual application settings from opaque databases;
- decide that every SQLite file is volatile or sensitive;
- maintain an exhaustive blacklist of applications;
- use file modification time as evidence of user intent;
- infer user authorship from ownership/UID alone;
- inspect file contents during the surface probe;
- follow symlinks during probing or discovery;
- weaken sensitive-content detection for explicitly included paths;
- automatically capture document recovery, history, cookies, login state, telemetry, or crash data;
- make Config an arbitrary bulk-directory backup mechanism; Resources remains the escape hatch for deliberately authoritative large trees;
- introduce application-specific semantic merge logic;
- make skipped surfaces contribute profile drift merely because they exist.

---

## 4. Design principles

### 4.1 Surfaces before files

The first meaningful unit below `~/.config` is usually the application surface:

```text
~/.config/ghostty/
~/.config/nvim/
~/.config/chromium/
~/.config/1Password/
~/.config/libreoffice/
```

Blueprint should decide whether a surface is appropriate for automatic recursive discovery before opening every file inside it.

### 4.2 Baseline provenance outranks heuristic suspicion

If Omarchy ships a known baseline file at a path, Blueprint has explicit provenance that the path participates in configuration management.

Therefore:

> Surface classification controls user-added recursive discovery, not exact baseline-backed paths.

A baseline-backed file inside a state-heavy surface is still inspected exactly and may be captured as modified/deleted Config state.

### 4.3 High precision beats maximum recall

Automatic discovery should capture state that is very likely portable user configuration.

If Blueprint cannot confidently distinguish configuration from runtime state, it should report the surface as `mixed` rather than recursively capture it.

A missed preference can be explicitly included later. Accidentally committing cookies, recovery files, credential databases, or thousands of generated extension assets is materially worse.

### 4.4 Product-independent structure beats app-name blacklists

Browser detection should recognize Chromium-, Gecko-, and WebKit/profile-style structures rather than enumerate browser brands.

Application-specific names may remain in the high-confidence sensitive-path deny list where the security value justifies them, but they are not the primary discovery mechanism.

### 4.5 Explicit intent can narrow automatic policy, not bypass hard safety

A user may explicitly include a safe subpath under a `mixed` or `state-heavy` surface.

Explicit inclusion does not override:

- stronger provider ownership;
- hard sensitive-path policy;
- high-confidence sensitive-content detection;
- symlink refusal/delegation;
- unsupported special-file refusal;
- backup-artifact filtering;
- per-file Config size limits.

If the user truly wants an opaque or sensitive bulk tree, Resources is the more appropriate explicit mechanism.

### 4.6 Status is a summary; diff is the forensic view

Normal `status` should answer whether the portable configuration profile is in sync.

It should not print one line for every object in an application state store.

---

## 5. Terminology

### 5.1 Config surface

For recursive XDG discovery, a surface is one immediate child of `$HOME/.config`.

Directory examples:

```text
.config/ghostty
.config/nvim
.config/chromium
.config/Typora
```

A regular file directly under `.config` is a leaf surface and does not require directory probing:

```text
.config/mimeapps.list
.config/brave-flags.conf
```

Curated exact HOME paths such as `.bashrc` and `.tmux.conf` are not surface-classified. They retain exact-path Config v2 behavior.

### 5.2 Surface classification

```go
type SurfaceClassification string

const (
    SurfaceConfigLean  SurfaceClassification = "config-lean"
    SurfaceStateHeavy  SurfaceClassification = "state-heavy"
    SurfaceMixed       SurfaceClassification = "mixed"
    SurfaceSensitive   SurfaceClassification = "sensitive"
)
```

### 5.3 Surface summary

Surface classification is runtime discovery metadata, not desired restore state.

Recommended shape:

```go
type SurfaceSummary struct {
    Path           string                `json:"path"`
    Classification SurfaceClassification `json:"classification"`
    Reasons        []string              `json:"reasons,omitempty"`
    SampledEntries int                   `json:"sampled_entries,omitempty"`
    Explicit       bool                  `json:"explicit,omitempty"`
}
```

Reasons are stable machine-readable identifiers, for example:

```text
browser-profile-chromium
browser-profile-gecko
state-database-family
state-leveldb
state-runtime-directories
state-high-fanout
config-like-files
mixed-config-and-runtime
hard-sensitive-structure
probe-limit-reached
```

Human output may render friendly descriptions instead.

### 5.4 Scan summary

Extend Config scan output:

```go
type ScanSummary struct {
    Candidates []Candidate      `json:"candidates"`
    Surfaces   []SurfaceSummary `json:"surfaces,omitempty"`
}
```

Candidates continue to represent actual file/tombstone Config semantics. Surface summaries explain discovery decisions.

---

## 6. Schema 9: persistent positive Config inclusion

Config v2 schema 8 currently persists exclusions but has no way to remember:

> this path should be discovered even though its parent surface is automatically classified mixed/state-heavy.

Add:

```go
type Configs struct {
    Files    []ConfigFile   `json:"files" toml:"file"`
    Deletes  []ConfigDelete `json:"deletes,omitempty" toml:"delete,omitempty"`
    Included []string       `json:"included,omitempty" toml:"included,omitempty"`
    Excluded []string       `json:"excluded,omitempty" toml:"excluded,omitempty"`
}
```

### 6.1 Why schema 9 is required

This is not written as an unversioned schema-8 extension.

An older schema-8 binary does not understand `Included`. If it loaded a schema-8 profile containing the new field and later saved it, it could discard the positive-inclusion policy.

Schema 9 makes the compatibility behavior safe:

```text
schema-8 binary + schema-9 profile
→ reject unsupported schema
→ never silently erase Included policy
```

### 6.2 Migration

Schema 8 → 9 is in-memory and non-destructive:

```text
Included = empty
existing Files / Deletes / Excluded unchanged
snapshot layout unchanged
```

No eager filesystem migration occurs on load.

Existing saved Config files remain desired state for status/restore until the user performs a new capture. A subsequent schema-9 capture may prune previously auto-captured files from newly skipped surfaces; that changes profile state only, never live files, and must be reported in capture changes.

### 6.3 Canonical policy paths

`Included` and `Excluded` paths use the same canonical HOME-relative identity as Config files:

```text
.config/Typora/themes
.config/libreoffice/4/user/basic
.config/nvim.backup
```

No alternate `.config`-relative representation is persisted.

---

## 7. Explicit include/exclude semantics

### 7.1 Exclude

```bash
omarchy-blueprint exclude config:.config/Typora
```

means:

- persist the exclusion;
- remove saved Config Files/Deletes below the excluded path;
- remove positive Included entries at or below that path;
- SkipDir the excluded path during future discovery;
- never generate tombstones for paths removed by exclusion.

Exclusion outranks positive inclusion.

### 7.2 Include

```bash
omarchy-blueprint include config:.config/Typora/themes
```

means:

- persist the path in `Configs.Included`;
- use it as an explicit discovery root even if `.config/Typora` is `mixed`, `state-heavy`, or structurally sensitive;
- inspect only that requested subtree, not skipped siblings;
- continue to apply hard per-path/per-file safety rules.

If an excluded ancestor still covers the requested include path, return an error explaining that the ancestor must first be included/unexcluded. Do not implement longest-prefix include/exclude precedence in this version.

### 7.3 Include does not mean force-secrets

Explicit inclusion does not make these capturable:

```text
.config/gh/hosts.yml
.config/rclone/rclone.conf
.config/sops/age/keys.txt
credential/private-key content
arbitrary symlink targets
backup artifacts
special files
oversized Config files
```

The existing no-force-secrets rule remains.

### 7.4 Resources remains the bulk-tree escape hatch

If a user wants to preserve an entire state-heavy tree as authoritative bytes, the CLI should direct them toward explicit Resources tracking rather than weakening Config discovery.

---

## 8. Surface probe

Before recursively walking a directory surface, run a bounded metadata-only probe.

### 8.1 Probe constraints

Initial limits:

```text
maximum sampled entries: 256
maximum relative depth:   3
```

The probe:

- uses `os.ReadDir`/`Lstat` semantics;
- sorts entries lexically for determinism;
- never follows symlinks;
- never opens regular-file contents;
- records only names, type, size, extension, depth, and parent-relative structure;
- may terminate early after a decisive browser/sensitive/state-heavy signature.

The probe is deliberately cheap enough to run for every `.config` directory surface.

### 8.2 Probe output

Recommended internal representation:

```go
type SurfaceProbe struct {
    Entries          int
    RegularFiles     int
    Directories      int
    MaxDepth         int
    ProbeLimitHit    bool
    ConfigSignals    map[string]bool
    StateSignals     map[string]bool
    SensitiveSignals map[string]bool
    Names            map[string]bool
    RelativeNames    map[string]bool
}
```

Do not persist raw probe contents in the profile.

---

## 9. Browser/profile detection

Browser detection is structural and engine-family oriented.

### 9.1 Chromium-family signature

Classify a surface as `state-heavy` with reason `browser-profile-chromium` when the bounded probe finds:

```text
Local State
AND
(Default/ OR Profile */)
AND
at least two profile-state markers under one such profile directory:
  Preferences
  Secure Preferences
  Cookies
  History
  Web Data
  Extensions/
  Network/
  DIPS
```

This recognizes Chromium, Chrome, Brave, Edge, Vivaldi, Opera, Chromium-derived browsers, and similar layouts without product-name matching.

A surface containing only `NativeMessagingHosts/` or standalone flags files does not satisfy this signature and may remain config-lean.

### 9.2 Gecko-family signature

Classify as `state-heavy` with reason `browser-profile-gecko` when either:

```text
profiles.ini exists
AND a sampled profile contains prefs.js
AND at least one of:
  places.sqlite
  cookies.sqlite
  extensions.json
  sessionstore.jsonlz4
```

or the surface itself is a profile directory containing:

```text
prefs.js
AND at least two of:
  places.sqlite
  cookies.sqlite
  extensions.json
  storage/
  sessionstore-backups/
```

This recognizes Firefox, Zen, LibreWolf, Floorp, Waterfox, and other Gecko-derived profile layouts without enumerating brand names.

### 9.3 WebKit / generic web-profile signature

Classify as `state-heavy` when the probe finds a strong web-profile combination such as at least three of:

```text
WebsiteData/
WebKitWebsiteData/
LocalStorage/
IndexedDB/
Cookies/
NetworkCache/
Service Worker/
Session Storage/
```

The generic state classifier below remains the fallback for browser engines/layouts not covered by these signatures.

### 9.4 Browser guarantee

The design goal is:

> Skip browser/profile roots independent of product name for the major browser engine/profile families, with generic state-heavy fallback for unfamiliar layouts.

It is not a promise that an arbitrary future browser with a completely novel storage layout can be identified as a browser by name-free heuristics.

---

## 10. Generic structural evidence

Classification uses evidence families rather than individual filenames. Repeated files from the same database/runtime mechanism count as one family so a directory with 100 WAL files does not artificially dominate the decision.

### 10.1 State-heavy evidence families

#### Database family

Evidence when the probe observes database + companion patterns such as:

```text
foo.sqlite + foo.sqlite-wal
foo.sqlite + foo.sqlite-shm
foo.db + foo.db-journal
```

A lone `.sqlite` or `.db` file is not enough by itself.

#### LevelDB/RocksDB-style family

Evidence when one directory contains combinations such as:

```text
CURRENT
MANIFEST-*
LOCK
*.ldb
*.log
```

#### Runtime directory family

Case-insensitive exact path-component matches such as:

```text
cache
code cache
gpucache
dawncache
dawngraphitecache
dawnwebgpucache
indexeddb
local storage
session storage
service worker
webstorage
blob_storage
file system
crashpad
crashes
logs
telemetry
sessionstore-backups
bookmarkbackups
draftsrecover
```

These are structural evidence during the surface probe and remain normal volatile/SkipDir policy during full discovery.

#### Browser/application-state file family

Stable generated-state names such as:

```text
Cookies
History
DIPS
SharedStorage
Trust Tokens
Network Persistent State
BrowserMetrics*
LOCK
CURRENT
MANIFEST-*
```

A single generic `LOCK` is not decisive by itself; family combinations matter.

#### High-fanout/generated family

Evidence when the probe reaches the 256-entry cap and the sampled tree is dominated by generated/versioned/binary/runtime-looking entries rather than configuration/source files.

`probe-limit-reached` alone must not classify a surface state-heavy; it contributes only with other state evidence.

### 10.2 Sensitive structural evidence

High-confidence structural markers may upgrade a state-heavy surface to `sensitive`, for example combinations involving:

```text
Login Data
logins.json + key4.db
credential stores
password/account databases
Cookies + authentication/profile state
```

This surface label is primarily diagnostic. Hard sensitive path/content rules remain the actual capture boundary.

### 10.3 Config-oriented evidence

Config evidence includes:

- root/shallow files named `config`, `settings`, `keybindings`, `preferences` with conventional config extensions;
- extensions such as `.conf`, `.ini`, `.toml`, `.yaml`, `.yml`, `.json`, `.jsonc`, `.lua`, `.vim`, `.fish`, `.nu`, `.css`, `.scss`, `.py`, `.sh`;
- user-facing configuration directories such as `themes`, `snippets`, `keybindings`, `macros`, `templates`;
- a high proportion of configuration/source-like files with no runtime-state families.

Names are evidence, not hard capture permission.

---

## 11. Deterministic surface decision rules

Apply rules in this order.

### Rule 1: policy/ownership/backup root

Before probing:

```text
explicitly excluded surface
→ excluded + SkipDir

stronger provider recursively owns surface
→ delegated + SkipDir

surface basename is a backup artifact
→ backup + SkipDir
```

These are existing path policy outcomes rather than one of the four heuristic classifications.

### Rule 2: browser profile

Any decisive Chromium/Gecko/WebKit profile signature:

```text
→ state-heavy
→ SkipDir for automatic discovery
```

### Rule 3: sensitive application-profile structure

High-confidence sensitive structure combined with state evidence:

```text
→ sensitive
→ SkipDir for automatic discovery
```

### Rule 4: mixed

If the probe contains at least one meaningful config-oriented evidence family and at least one meaningful state-heavy evidence family:

```text
→ mixed
→ SkipDir for automatic discovery
```

This is the expected classification for applications like Typora or LibreOffice that contain portable-looking settings/themes/macros alongside recovery, generated extension, database, or web-runtime state.

### Rule 5: state-heavy

If the probe contains two or more independent state-heavy evidence families and no meaningful config-oriented family:

```text
→ state-heavy
→ SkipDir
```

A browser signature also reaches this outcome earlier.

### Rule 6: config-lean

Classify `config-lean` when no state-heavy evidence family exists and either:

```text
(a) the probe is small/shallow:
    <= 64 sampled entries
    AND max depth <= 3

OR

(b) at least 70% of sampled regular files have config/source-like names/extensions
```

This allows conventional editor/terminal/window-manager config repositories to remain auto-discoverable even when they contain more than a handful of files.

### Rule 7: conservative fallback

Anything not confidently config-lean and not already classified state-heavy/sensitive:

```text
→ mixed
→ SkipDir
```

The fallback is intentionally conservative.

---

## 12. Baseline-backed paths inside skipped surfaces

Surface SkipDir must not hide Omarchy-provenance Config files.

Reorder Scan conceptually:

```text
1. enumerate baseline Config paths
2. classify/probe user .config surfaces
3. recursively walk only config-lean automatic surfaces
4. recursively walk explicit Included roots
5. add exact user counterparts for every baseline path not already seen
6. add exact saved paths needed for desired-state/status/restore evaluation
7. classify resulting candidates
```

Therefore, if Omarchy ships:

```text
/usr/share/omarchy/config/some-browser/NativeMessagingHosts/com.omarchy.foo.json
```

and the user surface is otherwise state-heavy, Blueprint still examines that exact corresponding user path and can capture modification/deletion relative to baseline.

It does not recursively discover unrelated browser state.

---

## 13. Saved paths inside newly skipped surfaces

A saved `ConfigFile` or `ConfigDelete` remains desired profile state until a new capture replaces it.

`status`, `diff`, `restore`, `verify`, and `check` must inspect saved exact paths even if the containing surface is now state-heavy/mixed.

This preserves profile semantics across policy upgrades.

During a subsequent `capture config`:

- skipped-surface files that are not baseline-backed and not explicitly Included are omitted from the newly captured state;
- removal from profile state is reported as capture policy pruning;
- live files are never deleted by capture;
- the scanner does not need to recursively reopen the skipped surface to prune old profile entries.

This lets a schema-8 profile that accidentally captured large browser trees shrink on its next schema-9 capture without destructive live-system behavior.

---

## 14. Generic backup-artifact policy

The current `.bak.` matcher is too narrow. Real Omarchy upgrade artifacts commonly end in `.bak`, and entire backup directories may use suffixes such as `.backup` or names such as `backups`.

Backup policy runs before baseline comparison, tombstone generation, sensitive inspection, hashing, and surface probing of descendants.

### 14.1 File backup patterns

Case-insensitive basename rules:

```text
*.bak
*.bak.*
*.backup
*.backup-*
*.orig
*~
```

Retain the exact Blueprint sibling-backup rule:

```text
.<name>.omarchy-blueprint-backup-<digits>
```

Do not add a blanket `*.old` rule in this version.

### 14.2 Directory backup patterns

Case-insensitive directory basename rules:

```text
backup
backups
*.bak
*.backup
*-backup
*-backups
*_backup
*_backups
```

Examples that must be pruned:

```text
.config/nvim.backup/
.config/Typora/backups/
.config/libreoffice/4/user/backup/
.config/foo.omarchy-upgrade-to-quattro.<timestamp>.bak/
```

A matched directory returns `filepath.SkipDir`.

### 14.3 No tombstones for backup artifacts

If a backup-looking file exists in the Omarchy baseline or user tree, it is outside Config desired-state semantics and never generates a tombstone.

---

## 15. Full discovery of config-lean / explicitly included paths

Once a surface/subtree is eligible for recursion, retain the existing Config path policy with these ordering rules:

```text
ownership delegation
→ backup artifact
→ explicit exclusion
→ hard sensitive path
→ symlink/special-file policy
→ volatile/runtime path
→ per-file size limit
→ regular-file inspection/hash
→ baseline classification
```

Directories rejected by ownership, backup, exclusion, sensitive, or volatile policy are pruned with `SkipDir` where applicable.

Explicit inclusion bypasses only the surface-level heuristic classification. It does not bypass the hard rules above.

---

## 16. Single-pass regular-file inspection and hashing

The current Scan opens/reads an accepted user file once for sensitive-content inspection and again for SHA-256 hashing.

Replace that with one hardened read pass.

Recommended API:

```go
type FileInspection struct {
    Hash      string
    Sensitive bool
    BytesRead int64
}

func InspectRegularFile(path string, maxBytes int64) (FileInspection, error)
```

Requirements:

1. open with the existing hardened `content.OpenRegularFile` semantics;
2. feed every chunk into SHA-256 while simultaneously evaluating sensitive-content signatures;
3. preserve a bounded overlap between chunks so signatures spanning a read boundary are detectable;
4. stop and classify oversized if bytes exceed `MaxAutomaticConfigFileSize` even if the file grew after Lstat;
5. return no usable Config hash for a sensitive file;
6. never reopen the file solely to hash it.

Baseline files may use the existing hardened hash-only path because the installed baseline is trusted distribution content rather than user secret material.

---

## 17. Sensitive-content prefilter performance

Remove the sliding `containsASCIIFold` implementation that calls `bytes.EqualFold` at every byte offset for every token key.

For each bounded chunk window:

```text
lowercase ASCII/window once
→ bytes.Contains for each small literal key
→ only if a key is present, run structured validation on a bounded region around the hit
```

For PEM detection:

```text
bytes.Contains("-----BEGIN ")
→ inspect only the candidate line/nearby bounded region
```

Do not run a whole-window regexp merely because one token key occurred somewhere in a large chunk.

Tests should assert call shape/counters, not timing:

```text
large ordinary file
→ one file read pass
→ zero structured regex checks

large token-bearing file
→ one pass
→ bounded structured check
→ sensitive
```

---

## 18. Discovery size guard

The surface probe itself is strictly bounded at 256 entries/depth 3.

For automatic full recursion of a `config-lean` surface, add a per-surface guard so an unexpectedly enormous source-looking tree cannot make status/capture unbounded:

```text
maximum automatically inspected regular files per surface: 5,000
maximum automatically inspected bytes per surface:         128 MiB
```

These limits are separate from the existing 16 MiB per-file limit.

Implementation rule:

- collect automatic-discovery candidates for one surface in a temporary buffer;
- if either surface limit is exceeded, discard that surface's newly discovered automatic candidates;
- report the surface as `mixed` with reason `automatic-scan-budget-exceeded`;
- still retain exact baseline-backed and previously saved-path inspection outside that automatic buffer;
- do not save a partial automatic capture of the surface.

An explicitly Included subtree uses the same 5,000-file / 128 MiB Config guard. If it exceeds the guard, return a clear policy error suggesting Resources for bulk state rather than silently truncating.

This makes the cost of any one Config surface operationally bounded without introducing a partial-profile global scan budget.

---

## 19. Capture semantics

`capture config` produces new desired Config state only from:

```text
baseline-backed modified/deleted paths
+ user-added candidates under config-lean surfaces
+ user-added candidates under explicit Included roots
+ curated exact HOME paths
```

It never captures recursively discovered files from state-heavy/mixed/sensitive surfaces unless a narrower path is explicitly Included and passes hard safety policy.

Capture output should summarize at the surface level.

Example:

```text
Captured Config state
  modified  4 files across 3 surfaces
  added     9 files across 4 surfaces
  deleted   1 baseline file

Config discovery
  skipped   chromium       application/profile state
  skipped   zen            application/profile state
  skipped   1Password      state-heavy/sensitive
  mixed     Typora
  mixed     libreoffice
  mixed     obsidian
```

Do not emit hundreds of individual ignored state files in human output.

---

## 20. Status semantics and human output

Normal `status` becomes surface-oriented for Config.

### 20.1 Drift

Actual portable Config drift is aggregated by surface:

```text
Config
  modified  ghostty          1
  added     nvim             4
  deleted   environment.d    1
```

A surface line may contain multiple change types if necessary, but deterministic one-line-per-type output is simpler.

New config-lean user files remain real drift because they would be captured automatically.

### 20.2 Discovery information

Skipped surfaces are informational and do not make status exit with drift solely because they exist:

```text
Config discovery
  skipped   chromium       application/profile state
  skipped   zen            application/profile state
  mixed     Typora
  mixed     libreoffice
```

Do not enumerate skipped descendants.

### 20.3 Saved state under skipped surfaces

If a saved Config file inside a now-skipped surface actually drifts, it is still desired state and therefore appears in Config drift aggregation.

The surface's heuristic classification does not suppress known saved-state drift.

---

## 21. Diff semantics

`diff config` remains the detailed forensic view for portable Config state.

It may emit file-level changes for:

- saved Config paths;
- baseline-backed paths;
- config-lean auto-discovered paths;
- explicitly Included paths.

For skipped surfaces it emits only one summary entry per surface, never every descendant.

Example:

```text
~ .config/ghostty/config
+ .config/nvim/lua/plugins/foo.lua

Skipped discovery surfaces:
  chromium      state-heavy
  Typora        mixed
```

---

## 22. JSON output

Machine-readable output should preserve more detail than human output without forcing traversal of skipped trees.

Config JSON should include:

```json
{
  "config": {
    "files": [],
    "deletes": [],
    "included": [],
    "excluded": [],
    "scan": {
      "surfaces": [],
      "candidates": []
    }
  }
}
```

`surfaces` contains one record per top-level directory/leaf surface considered.

`candidates` contains only paths that were actually inspected as Config candidates. It does not contain fictional descendant records for a pruned surface.

This keeps JSON truthful and bounded.

---

## 23. Restore, verify, and check implications

Surface classification is primarily a discovery concern.

Restore semantics remain driven by saved `ConfigFile` / `ConfigDelete` state and the Slice B effective A/B/C/M resolver.

Therefore:

```text
restore
→ never refuses a saved file merely because its surface now classifies state-heavy
```

`verify` inspects exact saved desired paths and does not require recursive discovery of skipped siblings.

`check` additionally validates:

- Included/Excluded policy paths;
- no impossible include under an excluded ancestor;
- no saved Config path overlapping stronger provider ownership;
- no snapshot corruption;
- schema-9 compatibility.

---

## 24. Ownership interaction

Surface classification never overrides stronger provider ownership.

If Resources owns `.config/foo` recursively:

```text
surface foo
→ delegated
→ SkipDir
```

If Resources owns only `.config/foo/bar`, the surface probe may still classify `.config/foo`, but full recursion must prune the owned subtree before inspecting its contents.

The active Blueprint profile/state roots remain mandatory recursive ownership/exclusion claims if they overlap Config discovery.

---

## 25. Security properties

This design improves security because state-heavy and mixed surfaces are usually rejected before content is opened.

Required invariants:

1. surface probing never follows symlinks;
2. surface probing never opens file contents;
3. hard sensitive paths are checked before content capture;
4. sensitive-content inspection uses hardened regular-file open semantics;
5. explicit inclusion does not disable secret detection;
6. browser/profile trees are not auto-captured;
7. backup/document-recovery trees are not auto-captured;
8. unknown special filesystem objects are not captured;
9. no skipped surface generates broad deletion/tombstone semantics.

---

## 26. Performance acceptance criteria

Performance tests should verify algorithmic work, not fragile wall-clock thresholds.

Required test seams/counters:

```text
surface probe entries visited
regular files opened
bytes inspected
sensitive structured checks
hash passes
```

Acceptance properties:

```text
Chromium-like tree with thousands of descendants
→ probe <= 256 entries
→ surface classified state-heavy
→ descendants not recursively walked
→ regular content opens ~= 0 except exact baseline/saved paths

Gecko-like tree
→ same

mixed Electron-style tree
→ bounded probe
→ mixed
→ no recursive content inspection

ordinary config-lean file
→ exactly one user content read pass for hash + sensitive inspection

backup directory
→ SkipDir before child enumeration

explicit included mixed subpath
→ only included subtree recursively inspected
```

A manual acceptance run on a normal desktop should make:

```bash
time omarchy-blueprint --profile <profile> status config
```

comfortably interactive and produce surface-scale output rather than thousands of browser/state filenames.

Do not encode a hard seconds-based CI assertion because hardware/filesystem variability makes it flaky.

---

## 27. Test matrix

### 27.1 Surface classifier unit tests

Cover:

```text
small Ghostty-like surface                        → config-lean
large Lua-only nvim-like surface                  → config-lean
Chromium structural profile                       → state-heavy
Chromium derivative with arbitrary root name      → state-heavy
Gecko profile with arbitrary root name            → state-heavy
WebKit/profile state structure                    → state-heavy
SQLite + WAL/SHM + runtime dirs, no config         → state-heavy
Typora-like config + Chromium runtime              → mixed
LibreOffice-like config + backups/generated state → mixed
unknown high-fanout generated tree                → mixed
```

No product-name dependency should be necessary for the browser derivative tests.

### 27.2 Backup tests

Cover files:

```text
foo.bak
foo.bak.123
foo.omarchy-upgrade-to-quattro.20260819110957.bak
foo.backup
foo.backup-123
foo.orig
foo~
```

Cover directories:

```text
backup/
backups/
nvim.backup/
foo-backup/
foo_backups/
foo.<timestamp>.bak/
```

Assert directories are `SkipDir`-pruned and their children never reach content inspection.

### 27.3 Include/exclude tests

Cover:

```text
mixed Typora surface
+ Included=.config/Typora/themes
→ only themes recursively inspected

state-heavy browser surface
+ Included exact NativeMessagingHosts/foo.json
→ exact path inspected
→ browser siblings not walked

hard-sensitive file below Included parent
→ still refused

Excluded ancestor + Included child
→ validation/command error

exclude surface containing Included descendants
→ descendants removed from Included and saved state pruned
```

### 27.4 Baseline tests

Cover:

```text
baseline-backed path inside state-heavy browser surface
→ exact user path compared/captured
→ no recursive browser discovery

baseline file absent from user
→ tombstone still captured
```

### 27.5 Saved-state compatibility tests

Cover schema 8 → 9:

```text
existing saved browser ConfigFile
→ loads unchanged
→ status/restore still inspect exact saved path
→ next capture omits it unless baseline-backed/Included
→ capture reports profile pruning
```

### 27.6 Inspection tests

Cover:

```text
ordinary 10 MiB config
→ one OpenRegularFile
→ one stream pass
→ hash produced
→ no structured secret regex

secret near chunk boundary
→ detected

hash/content source changes during open
→ existing hardened semantics preserved

file grows past 16 MiB during scan
→ oversized, not captured
```

### 27.7 Status/output tests

Given 1,000 skipped browser descendants:

```text
status human output
→ one browser surface line
→ no descendant filenames
→ skipped surface alone does not produce drift exit status

diff human output
→ one skipped surface summary

--json
→ SurfaceSummary present
→ descendants absent unless actually inspected
```

---

## 28. Suggested implementation boundaries

Keep surface policy isolated from file classification.

Recommended files/components:

```text
internal/providers/config/surface.go
  SurfaceProbe
  SurfaceClassification
  ProbeSurface
  ClassifySurface
  browser/profile signatures
  structural evidence extraction

internal/providers/config/policy.go
  hard per-path policy
  generic backup-artifact helpers
  sensitive-content streaming matcher

internal/providers/config/inspect.go
  single-pass regular-file sensitive inspection + hash

internal/providers/config/scan.go
  baseline-first orchestration
  surface probing
  config-lean recursion
  selective Included-root traversal
  exact saved/baseline path injection

internal/profile/profile.go
  schema 9
  Configs.Included
  validation/migration

internal/app/app.go / providers.go
  include/exclude persistence
  surface-oriented capture/status/diff rendering
```

Avoid putting browser signatures directly in `scan.go`; they should be independently unit-testable.

---

## 29. Implementation order

Implement in TDD slices:

1. schema 9 + `Configs.Included` + migration/validation;
2. generic backup-artifact rules and directory pruning;
3. surface probe model with deterministic bounded traversal;
4. Chromium/Gecko/WebKit structural signature tests;
5. generic config/state evidence and classification rules;
6. baseline-first / surface-aware Scan orchestration;
7. explicit Included selective traversal;
8. single-pass user inspection+hash and removal of `containsASCIIFold`;
9. per-surface automatic scan guard;
10. capture pruning behavior for legacy saved paths;
11. surface-oriented status/capture/diff output;
12. JSON and end-to-end lifecycle coverage;
13. real-machine manual acceptance and final docs update.

The highest-risk implementation areas are Scan orchestration and positive inclusion boundaries. Keep those changes reviewable and avoid combining them with unrelated restore changes.

---

## 30. Manual acceptance checklist

On at least one normal Omarchy desktop with browsers and Electron-style apps installed:

```bash
time omarchy-blueprint --profile <profile> status config
```

Confirm:

- no long CPU stall in regex/EqualFold scanning;
- browser surfaces appear once, not as hundreds/thousands of files;
- 1Password-like state does not auto-capture;
- mixed surfaces such as Typora/LibreOffice/Obsidian are reported but not auto-captured recursively;
- conventional config-lean surfaces still produce expected file drift;
- `.bak` upgrade files are absent;
- backup directories such as `nvim.backup` are absent;
- exact Omarchy baseline-backed files still participate in drift even under skipped surfaces.

Then test an explicit mixed-surface include:

```bash
omarchy-blueprint include config:.config/Typora/themes
omarchy-blueprint capture config
omarchy-blueprint status config
```

Confirm only the requested subtree becomes Config desired state and sibling recovery/runtime data remains skipped.

Finally run:

```bash
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

---

## 31. Decision summary

The implementation should follow these final decisions:

```text
Unit of automatic recursive discovery
→ top-level .config surface

Automatic capture posture
→ high precision; uncertain/mixed state is skipped and reported

Browser handling
→ structural engine/profile detection, not product blacklist

Mixed app handling
→ no recursive auto-capture; explicit safe subpath include supported

Baseline-backed paths
→ always exact-inspected regardless of surface classification

Saved desired paths
→ remain authoritative regardless of later surface classification

Backup handling
→ generic high-confidence file + directory patterns, applied before all Config semantics

User file processing
→ one hardened read pass for sensitive inspection + SHA-256

Status
→ surface-level human summary

Diff
→ detailed portable file differences + one-line skipped surfaces

Positive include persistence
→ schema 9 `Configs.Included`

Bulk opaque trees
→ Resources, not weakened Config policy
```

This preserves Config v2's core promise—capture meaningful portable user intent from the running system—while preventing `$HOME/.config` from being treated as an indiscriminate application-state backup root.
