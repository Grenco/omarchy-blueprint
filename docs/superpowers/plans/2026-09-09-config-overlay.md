# Config v2 Baseline-Aware Home Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand the Config provider into a baseline-aware home-configuration overlay covering recursive `~/.config/**` plus a curated exact `$HOME`-relative config registry, while capturing meaningful user changes, new config, deletion intent, provider delegation, Omarchy/update-extension backup noise, safe cross-version merges, and unknown-target preservation.

**Architecture:** Replace Config v1's fixed-spec detector with a path-identified recursive scanner and sparse profile overlay. Separate scanning/policy, capture, diff/verify/check, three-way merge, and restore planning into focused files; reuse the hardened restore executor for guarded writes and add one explicit guarded delete action. Config receives a stronger-provider ownership index from the app layer so Resources/Themes/Plugins/Hooks/Shell remain authoritative.

**Tech Stack:** Go 1.25+/1.26, `github.com/pelletier/go-toml/v2`, existing Omarchy Blueprint provider/model/restore architecture, SHA-256 content helpers, pure-Go text merge implementation/library behind a local interface, Linux filesystem primitives.

**Spec:** `docs/superpowers/specs/2026-09-09-config-overlay-design.md`

## Global Constraints

- Profile schema becomes exactly **8**; schema-7 Config profiles must load and restore without an eager destructive migration.
- Config surfaces are recursive `~/.config/**` plus curated exact `$HOME`-relative config paths; there is no recursive `$HOME` scan.
- Profile Config identity is always `$HOME`-relative (`.config/...`, `.bashrc`, `.tmux.conf`, etc.).
- The primary `.config/**` baseline root remains `${OMARCHY_PATH:-/usr/share/omarchy}/config`; curated home paths use only explicit trustworthy package-template baselines.
- User-added regular config files are captured by default.
- Unchanged Omarchy baseline files are not persisted.
- Baseline files absent from the user tree become file tombstones.
- Omarchy update backups whose basename matches `xxx.bak.xxx` and extension/setup backups matching `*.backup-*` are ignored before capture/tombstone classification.
- The `*.bak.*` and `*.backup-*` rules are built-in policy, not persistent user exclusions.
- Never follow or dereference arbitrary symlinks.
- Resources, Themes, Plugins, Hooks, and Shell outrank generic Config ownership.
- Resources stays before Config in provider order.
- High-confidence secrets are never captured; `--force` does not authorize secret capture.
- High-confidence volatile/runtime state is ignored and reported.
- Automatic capture limit is **16 MiB per regular file**.
- Three-way text merge limit is **4 MiB**; mergeable text must be valid UTF-8 and contain no NUL byte.
- Cross-version merge must be pure Go at runtime; do not require `git`, `diff3`, or another external merge command.
- Unknown target-machine work is preserved by default.
- Config `--force` is bounded: validate target preconditions, back up displaced target state, then replace/delete; never blindly `RemoveAll` unknown state.
- Package exclusion and Config exclusion remain separate desired-state choices. Package-to-config association is advisory only.
- Empty directories, ACLs, xattrs, capabilities, hard-link identity, application-specific restarts, actual account login-shell mutation (`chsh`/`/etc/passwd`), and dirty-Git preservation are outside this feature.
- Existing Hyprland reload behavior remains, but reload only depends on successful relevant Config mutations.
- Every new filesystem traversal and mutation must be deterministic, parent-symlink-safe, and TOCTOU-aware.

---

## File Structure

The implementation should converge on these focused boundaries.

```text
internal/providers/config/
├── provider.go          orchestration and Provider construction
├── scan.go              recursive user/baseline discovery + classification
├── policy.go            exclusions, backup-noise, volatile, sensitive, size policy
├── surfaces.go          recursive .config surface + curated exact home path registry/baselines
├── capture.go           staged sparse desired/baseline snapshots
├── diff.go              profile/live diff, Verify, Check
├── merge.go             mergeable-text detection + 3-way interface
├── plan.go              restore candidate calculation and operations
├── association.go       package→config advisory matching
├── scan_test.go
├── policy_test.go
├── capture_test.go
├── diff_test.go
├── merge_test.go
├── plan_test.go
└── association_test.go

internal/profile/
├── profile.go           schema-8 Config metadata/load/save
└── profile_test.go

internal/model/
├── model.go             explicit FileDelete action
└── model_test.go

internal/restore/
├── executor.go          guarded delete + reusable backup/precondition support
├── executor_test.go
├── plan_validation.go   include FileDelete in action exclusivity/validation
└── plan_validation_test.go

internal/app/
├── providers.go         Config ownership context + force plumbing
├── app.go               config include/exclude CLI + package advisory output
└── app_test.go

internal/ownership/
└── reuse current claim/index APIs unless a test proves one small extension is necessary

README.md
ROADMAP.md
docs/manual-test-checklist.md
docs/adr/0013-baseline-aware-config-overlay.md
```

Keep `internal/providers/config/provider.go` small after the split. Do not keep appending Config v2 behavior to the current monolithic v1 file.

---

# Review Slice A — Schema, policy, scanner, capture

### Task 1: Schema 8 Config state and schema-7 compatibility

**Files:**
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: existing `profile.Load`, `profile.Save`, schema-threshold pattern.
- Produces:
  - `const Schema = 8`
  - `const configOverlaySchema = 8`
  - `type Configs struct { Files []ConfigFile; Deletes []ConfigDelete; Excluded []string }`
  - path-identified `ConfigFile`
  - `ConfigDelete`
  - normalized schema-8 save/load behavior used by every later task.

- [ ] **Step 1: Write schema-8 round-trip tests**

Add tests that construct:

```go
want := profile.Configs{
    Files: []profile.ConfigFile{
        {
            Path:         ".config/hypr/bindings.lua",
            Hash:         strings.Repeat("a", 64),
            Mode:         "0644",
            BaselineHash: strings.Repeat("b", 64),
            BaselineMode: "0644",
        },
        {
            Path: ".config/ghostty/config",
            Hash: strings.Repeat("c", 64),
            Mode: "0600",
        },
    },
    Deletes: []profile.ConfigDelete{
        {
            Path:         ".config/example/default.conf",
            BaselineHash: strings.Repeat("d", 64),
            BaselineMode: "0644",
        },
    },
    Excluded: []string{".config/discord", ".config/google-chrome"},
}
```

Save and reload a profile. Assert:

```go
if got.Manifest.Schema != 8 {
    t.Fatalf("schema=%d", got.Manifest.Schema)
}
if !reflect.DeepEqual(got.Config, want) {
    t.Fatalf("config=%#v want=%#v", got.Config, want)
}
```

Also assert `config/config.toml` is lexically sorted by path.

- [ ] **Step 2: Run the focused profile tests and confirm failure**

Run:

```bash
go test ./internal/profile -run 'Test.*Config.*Schema8|Test.*Schema8.*Config' -count=1
```

Expected: FAIL because schema 8 fields/types are absent.

- [ ] **Step 3: Add schema-8 types**

Use this shape:

```go
const Schema = 8

const (
    configSchema        = 2
    defaultsSchema      = 3
    shellSchema         = 4
    hooksSchema         = 5
    misePackagesSchema  = 6
    resourcesSchema     = 7
    configOverlaySchema = 8
)

type Configs struct {
    Files    []ConfigFile   `json:"files" toml:"file"`
    Deletes  []ConfigDelete `json:"deletes,omitempty" toml:"delete,omitempty"`
    Excluded []string       `json:"excluded,omitempty" toml:"excluded,omitempty"`
}

type ConfigFile struct {
    ID           string `json:"id,omitempty" toml:"id,omitempty"` // schema-7 decoder compatibility only
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

Add normalization helpers that:

```text
sort Files by Path
sort Deletes by Path
normalize/sort/dedupe Excluded
```

Do not use `ID` for equality or ordering.

- [ ] **Step 4: Add schema-7 fixture compatibility test**

Write a schema-7 profile with:

```toml
[[file]]
id = "hypr.bindings"
path = ".config/hypr/bindings.lua"
hash = "<64 hex>"
baseline_hash = "<64 hex>"
```

and matching snapshot files.

Load it with the schema-8 code and assert:

```go
if got.Config.Files[0].Path != ".config/hypr/bindings.lua" {
    t.Fatal(...)
}
```

Then save it and assert the emitted schema is 8 and Config identity remains path-based. Clear/omit the legacy `ID` during schema-8 normalization so new saves do not depend on it.

- [ ] **Step 5: Add normalization/validation regression tests**

Cover:

```text
duplicate ConfigFile path
duplicate ConfigDelete path
same path in Files and Deletes
absolute path
"."
".."
"a/../b"
empty path
duplicate exclusions
exclusion "ghostty/../nvim"
```

Use a helper contract:

```go
func ValidateConfigPath(path string) error
func NormalizeConfigPath(path string) (string, error)
```

Expected valid examples:

```text
hypr/bindings.lua
ghostty/config
nvim/lua/foo.lua
```

- [ ] **Step 6: Implement minimal path normalization**

Rules:

```go
func NormalizeConfigPath(path string) (string, error) {
    path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
    if path == "" || path == "." || filepath.IsAbs(path) {
        return "", fmt.Errorf("invalid config path %q", path)
    }
    if path == ".." || strings.HasPrefix(path, "../") {
        return "", fmt.Errorf("config path escapes root: %q", path)
    }
    return path, nil
}
```

Use equivalent cross-platform-safe code, but profile output must always use `/`.

- [ ] **Step 7: Run profile tests**

Run:

```bash
go test ./internal/profile -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go
git commit -m "feat: add schema 8 config overlay state"
```

---

### Task 2: Split Config v1 into focused provider files without behavior change

**Files:**
- Modify: `internal/providers/config/provider.go`
- Create: `internal/providers/config/diff.go`
- Create: `internal/providers/config/plan.go`
- Move/update tests from: `internal/providers/config/provider_test.go`
- Create: `internal/providers/config/diff_test.go`
- Create: `internal/providers/config/plan_test.go`

**Interfaces:**
- Consumes: Task 1 profile types.
- Produces: small `Provider` orchestration shell and clear file boundaries that later tasks replace incrementally.

- [ ] **Step 1: Characterize current Config v1 behavior before moving code**

Ensure tests explicitly cover:

```text
customized fixed Hypr file capture
baseline-equal file omitted
target missing restore
target baseline restore
target drift skip
baseline version change currently skipped
hyprctl reload dependency
```

Run:

```bash
go test ./internal/providers/config -count=1
```

Expected: PASS before refactor.

- [ ] **Step 2: Move diff/verify/check logic into `diff.go`**

Keep public signatures working while code moves:

```go
func DiffConfigs(previous, next profile.Configs) []model.Change
func Diff(saved profile.Configs, current State) []model.Change
func Verify(saved profile.Configs, current State) model.VerificationResult
func (p Provider) Check(saved profile.Configs) error
```

- [ ] **Step 3: Move planning logic into `plan.go`**

Keep current public signature temporarily:

```go
func (p Provider) Plan(
    saved profile.Configs,
    current State,
    schema int,
    from string,
    to string,
) (model.RestorePlan, error)
```

This task is structural only. Do not add recursive behavior yet.

- [ ] **Step 4: Run provider tests after each move**

Run:

```bash
go test ./internal/providers/config -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/config
git commit -m "refactor: split config provider responsibilities"
```

---

### Task 3: Config policy engine, including Omarchy `xxx.bak.xxx` backups

**Files:**
- Create: `internal/providers/config/policy.go`
- Create: `internal/providers/config/policy_test.go`
- Reuse: existing sensitive-content helpers under `internal/content` / Resources where appropriate.

**Interfaces:**
- Consumes: normalized relative Config paths.
- Produces:

```go
const MaxAutomaticConfigFileSize int64 = 16 << 20
const MaxMergeableTextSize int64 = 4 << 20

type PolicyReason string

const (
    PolicyAllowed             PolicyReason = "allowed"
    PolicyExcluded            PolicyReason = "excluded"
    PolicyOmarchyUpdateBackup PolicyReason = "omarchy-update-backup"
    PolicyVolatile            PolicyReason = "volatile"
    PolicySensitive           PolicyReason = "sensitive"
    PolicyOversized           PolicyReason = "oversized"
)

func IsOmarchyUpdateBackupName(name string) bool
func IsOmarchySetupBackupName(name string) bool
func IsExcludedConfigPath(path string, excluded []string) bool
func ClassifyConfigPolicy(path string, info os.FileInfo, excluded []string) PolicyDecision
```

- [ ] **Step 1: Write exact Omarchy backup matcher tests**

Use:

```go
cases := map[string]bool{
    "bindings.lua.bak.20260909": true,
    "config.bak.before-update":  true,
    "x.bak.y":                   true,

    ".bak.y":        false,
    "x.bak.":        false,
    "x.bak":         false,
    "backup.conf":   false,
    "foo.bakx.bar":  false,
    ".bashrc.backup-20260909-080102": true,
    ".zshrc.backup-now": true,
    ".backup-now": false,
    ".bashrc.backup-": false,
}
```

The implementation contract is:

```go
func IsOmarchyUpdateBackupName(name string) bool
func IsOmarchySetupBackupName(name string) bool {
    marker := ".bak."
    index := strings.Index(name, marker)
    return index > 0 && index+len(marker) < len(name)
}
```

A basename match is sufficient; do not scan full parent paths for `.bak.`.

- [ ] **Step 2: Test exclusions as exact path + subtree**

Examples:

```text
excluded [".config/google-chrome"]

google-chrome            → excluded
google-chrome/Default    → excluded
google-chromium          → allowed
foo/google-chrome        → allowed
```

Use path-aware containment, not string prefix.

- [ ] **Step 3: Test high-confidence volatile rules**

Start with the spec's conservative set and test concrete examples:

```text
chromium/Default/Cache/index
chromium/Default/Code Cache/js
foo/GPUCache/data
foo/Session Storage/LOG
foo/Service Worker/Database
foo/SingletonLock
foo/app.pid
foo/app.lock
```

Also assert these remain allowed:

```text
myapp/state.toml
myapp/data/config.json
locksmith/config.toml
```

- [ ] **Step 4: Test file-size classification**

Create a regular file stat/test seam where:

```text
size == 16 MiB       → allowed
size == 16 MiB + 1   → oversized
```

- [ ] **Step 5: Implement policy in strict order**

For a candidate:

```text
1. normalize path
2. built-in Blueprint/Omarchy backup noise
3. user exclusion
4. stronger provider ownership (scanner supplies this separately)
5. symlink/special object
6. sensitive classification
7. volatile classification
8. size
9. baseline comparison
```

Do not read file contents just to detect `*.bak.*`, exclusions, or directory-level volatile policy.

- [ ] **Step 6: Integrate existing sensitive checks**

Prefer reusing the Resources/content sensitive detector. If it is resource-specific, extract only the generic pure helper needed by both providers and keep provider-specific policy around it.

Regression:

```text
credentials.json with high-confidence token/private-key content
→ sensitive
→ capture policy refuses persistence
```

Never include the secret text in an error or JSON reason.

- [ ] **Step 7: Run policy tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Policy|Test.*Backup|Test.*Excluded|Test.*Volatile|Test.*Sensitive' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/config/policy.go internal/providers/config/policy_test.go internal/content
git commit -m "feat: add config capture policy"
```

---

### Task 4: Configuration surfaces and curated home-path registry

**Files:**
- Create: `internal/providers/config/surfaces.go`
- Create: `internal/providers/config/surfaces_test.go`
- Modify: `internal/providers/config/provider.go`
- Modify: `internal/app/app.go` only if a dependency seam is required to resolve installed baseline templates.

**Interfaces:**
- Consumes:
  - `$HOME`
  - primary `.config` baseline root
  - installed filesystem/package templates
- Produces:

```go
type SurfaceKind string

const (
    SurfaceRecursiveConfig SurfaceKind = "recursive-config"
    SurfaceExactHome       SurfaceKind = "exact-home"
)

type HomeConfigSpec struct {
    Path string // normalized $HOME-relative path

    // Optional trustworthy template resolver. `ok=false` means the file is
    // ordinary user-added Config state rather than baseline-backed state.
    ResolveBaseline func() (path string, ok bool, err error)
}

func DefaultHomeConfigSpecs() []HomeConfigSpec
func (p Provider) ResolveBaselineFor(path string) (string, bool, error)
```

The implementation may use value-only tables plus provider methods instead of closures if that is easier to test. The semantic contract must remain explicit.

- [ ] **Step 1: Write registry coverage tests**

Assert `DefaultHomeConfigSpecs()` includes the approved safe exact registry:

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

.tmux.conf
.screenrc
.wezterm.lua
.alacritty.yml
.alacritty.toml

.vimrc
.gvimrc
.exrc
.nanorc
.ideavimrc
.emacs
.emacs.el
.spacemacs

.gitconfig
.gitignore
.gitignore_global
.gitmessage
.gitmessage.txt
.gitattributes_global
.tigrc

.editorconfig
.dircolors
.ctags
.ackrc
.agignore
.ripgreprc
.fdignore
.ignore
.lesskey

.XCompose
.Xresources
.Xdefaults
.xprofile
.xinitrc
.pam_environment
.gtkrc-2.0

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

.cargo/config
.cargo/config.toml
.julia/config/startup.jl
```

Also assert the registry has no duplicate normalized path.

- [ ] **Step 2: Test dangerous/state paths are absent**

Assert the default registry does **not** include:

```text
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
.bash_history
.zsh_history
.python_history
.lesshst
.viminfo
.wget-hsts
.ssh/config
.aws/config
.kube/config
.docker/config.json
```

These remain built-in-sensitive/state policy, explicit Resources territory, or future secret-provider state.

- [ ] **Step 3: Test recursive surface is only `.config`**

The scanner roots produced by the surface registry must be:

```text
recursive: $HOME/.config
exact: each registered home path
```

Assert there is no:

```go
filepath.WalkDir(home, ...)
```

or equivalent whole-HOME recursive root in the surfaced API.

- [ ] **Step 4: Test profile identity is `$HOME`-relative**

Normalize:

```text
$HOME/.config/ghostty/config → .config/ghostty/config
$HOME/.bashrc               → .bashrc
$HOME/.cargo/config.toml    → .cargo/config.toml
```

Reject paths outside `$HOME`.

Provide:

```go
func LogicalHomeConfigPath(home, absolute string) (string, error)
func ExpandHomeConfigPath(home, logical string) (string, error)
```

These are Config-specific helpers; reuse Resources home-path helpers only if doing so does not couple profile syntaxes accidentally.

- [ ] **Step 5: Test Omarchy Zsh baselines**

With temporary files standing in for installed package templates, assert:

```text
.bashrc  → zsh templates/bashrc
.zshrc   → zsh templates/zshrc
.inputrc → zsh shell/inputrc
```

only when the relevant resolver reports those sources available.

The production resolver should use:

```text
/usr/share/omarchy-zsh/templates/bashrc
/usr/share/omarchy-zsh/templates/zshrc
/usr/share/omarchy-zsh/shell/inputrc
```

Do not infer the package baseline if those authoritative files do not exist.

- [ ] **Step 6: Test Omarchy Fish `.bashrc` baseline**

Production mapping:

```text
.bashrc → /usr/share/omarchy-fish/templates/bashrc
```

when present.

Fish user configuration under `.config/fish/**` remains ordinary recursive Config state.

- [ ] **Step 7: Test ambiguous `.bashrc` baseline**

If both Zsh and Fish package template resolvers claim `.bashrc`:

```text
→ ResolveBaselineFor(".bashrc") returns an ambiguity error
```

Do not pick one based on lexical order or package ordering.

- [ ] **Step 8: Test ordinary curated path with no baseline**

For:

```text
.tmux.conf
.XCompose
.gitconfig
```

when no explicit baseline resolver exists:

```text
→ baseline absent
→ scanner later classifies live file as user-added
```

- [ ] **Step 9: Implement the registry and resolver**

Keep the registry deterministic and code-owned.

Do not add globbing.

Do not recursively inspect parent directories such as `.cargo/**`; only `Lstat` the exact registered nested file.

- [ ] **Step 10: Run surface tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Surface|Test.*HomeConfig|Test.*BaselineResolver' -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/providers/config/surfaces.go internal/providers/config/surfaces_test.go internal/providers/config/provider.go
git commit -m "feat: add curated home config surfaces"
```

---

### Task 5: Recursive baseline-aware scanner and provider delegation

**Files:**
- Create: `internal/providers/config/scan.go`
- Create: `internal/providers/config/scan_test.go`
- Modify: `internal/providers/config/provider.go`
- Modify later in same task if needed: `internal/app/providers.go` test seam only; full app wiring is Task 12.

**Interfaces:**
- Consumes:
  - Task 3 policy.
  - Task 4 configuration surfaces/baseline resolver.
  - `ownership.Index`.
  - `Provider.HomeDir`, `Provider.BaselineRoot`.
- Produces:

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
    ConfigUnmanagedSymlink  Classification = "unmanaged-symlink"
    ConfigUnsupported       Classification = "unsupported"
    ConfigOversized         Classification = "oversized"
)

type Candidate struct {
    Path           string         `json:"path"`
    Classification Classification `json:"classification"`
    UserHash       string         `json:"hash,omitempty"`
    UserMode       string         `json:"mode,omitempty"`
    BaselineHash   string         `json:"baseline_hash,omitempty"`
    BaselineMode   string         `json:"baseline_mode,omitempty"`
    Reason         string         `json:"reason,omitempty"`
}

type ScanSummary struct {
    Candidates []Candidate `json:"candidates"`
}

func (p Provider) Scan(saved profile.Configs) (ScanSummary, error)
```

Extend `Provider`:

```go
type Provider struct {
    UserRoot     string
    BaselineRoot string
    ProfileDir   string
    Ownership    ownership.Index
}
```

- [ ] **Step 1: Test the four core baseline classes**

Fixture:

```text
baseline/
  same.conf      = "same"
  changed.conf   = "base"
  deleted.conf   = "base"

user/
  same.conf      = "same"
  changed.conf   = "user"
  added.conf     = "new"
```

Expect:

```text
same.conf    unchanged-baseline
changed.conf modified-baseline
deleted.conf deleted-baseline
added.conf   added
```

Hashes and modes must be populated only where meaningful.

- [ ] **Step 2: Test that Omarchy `.bak.` files never become Added or Deleted**

Fixture:

```text
user/hypr/bindings.lua.bak.20260909
```

Expect:

```text
classification = volatile
reason = "omarchy-update-backup"
```

Also put a matching backup path in the baseline-only side and assert it does **not** produce a tombstone.

For a matching directory:

```text
user/foo.bak.20260909/inside.conf
```

assert scanner reports/skips the root and never enumerates/captures `inside.conf`.

- [ ] **Step 3: Test stronger provider ownership**

Create claims:

```go
ownership.Index{Claims: []ownership.Claim{
    {Provider: "themes", Path: filepath.Join(userRoot, "omarchy", "themes"), Recursive: true},
    {Provider: "plugins", Path: filepath.Join(userRoot, "omarchy", "plugins"), Recursive: true},
    {Provider: "hooks", Path: filepath.Join(userRoot, "omarchy", "hooks"), Recursive: true},
    {Provider: "shell", Path: filepath.Join(userRoot, "omarchy", "shell.json")},
    {Provider: "resources", Path: filepath.Join(userRoot, "nvim"), Recursive: true},
}}
```

Assert candidates below those roots are `delegated` and not hashed/copied.

- [ ] **Step 4: Test symlink behavior**

Cases:

```text
user/foo -> ordinary target
→ unmanaged-symlink

user/nvim -> tracked Resource target, with exact Resources ownership claim
→ delegated

broken link
→ unmanaged-symlink

symlink directory
→ never followed
```

Assert no target bytes are read.

- [ ] **Step 5: Test exclusions and volatile directories are pruned**

Place a sentinel filesystem object under an excluded/volatile directory that would fail if traversed. Assert scan succeeds because the subtree is `SkipDir`.

- [ ] **Step 6: Implement union traversal**

Build a deterministic union from recursive `$HOME/.config` + primary baseline tree + exact curated home paths without following symlinks:

```go
type treeEntry struct {
    Abs  string
    Info os.FileInfo
}
```

Then sort the union of relative paths lexically and classify.

For recursive directory claims/policy:

```text
SkipDir during walk where possible
```

Do not rely only on post-walk filtering.

- [ ] **Step 7: Add source-stability/hash tests**

If a regular source changes while being inspected, the scanner/capture path must not silently persist mismatched metadata.

Use existing safe open/hash helpers. The final authoritative hash will be revalidated in Task 5 staging.

- [ ] **Step 8: Run scanner tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Scan' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/config/scan.go internal/providers/config/scan_test.go internal/providers/config/provider.go
git commit -m "feat: scan baseline aware config tree"
```

---

### Task 6: Sparse transactional Config capture

**Files:**
- Create: `internal/providers/config/capture.go`
- Create: `internal/providers/config/capture_test.go`
- Modify: `internal/providers/config/provider.go`
- Modify: `internal/providers/config/provider_test.go`

**Interfaces:**
- Consumes: `Provider.Scan(saved profile.Configs)`.
- Produces:

```go
type CaptureResult struct {
    State   profile.Configs
    Scan    ScanSummary
    Changes []model.Change
}

func (p Provider) Capture(saved profile.Configs) (CaptureResult, error)
```

The app adapter may temporarily wrap the new return shape in Task 12.

- [ ] **Step 1: Test sparse profile layout**

Given the Task 4 fixture, capture should produce:

```text
config/files/changed.conf
config/files/added.conf
config/baseline/changed.conf
config/baseline/deleted.conf
```

It must **not** produce:

```text
config/files/same.conf
config/baseline/same.conf
config/files/*.bak.*
config/baseline/*.bak.*
```

Metadata:

```go
Files = []{
    {Path:"added.conf", BaselineHash:""},
    {Path:"changed.conf", BaselineHash:"..."},
}
Deletes = []{{Path:"deleted.conf", BaselineHash:"..."}}
```

- [ ] **Step 2: Test persistent exclusions survive absent live paths**

Start with:

```go
saved.Excluded = []string{".config/discord"}
```

with no live `discord` path.

After capture:

```go
result.State.Excluded == []string{".config/discord"}
```

- [ ] **Step 3: Test delegated state removes stale Config bytes**

Saved Config contains:

```text
nvim/init.lua
```

Live system now has `~/.config/nvim` owned by Resources.

After capture:

```text
nvim/init.lua absent from State.Files
old config/files/nvim/init.lua absent after swap
```

The change summary should identify delegation rather than corruption:

```text
- config nvim/init.lua delegated to resources
```

If the existing `model.Change` cannot carry a reason separately, make the summary explicit.

- [ ] **Step 4: Implement staging**

Use:

```text
config/.capture-*/
  files/
  baseline/
```

Copy selected regular files with non-following source helpers.

After copy:

```text
hash staged file
chmod exact source permission bits
validate staged object is regular/non-symlink
```

Metadata hashes must describe staged bytes, not an earlier scan hash.

- [ ] **Step 5: Make both sparse trees one capture boundary**

Avoid the Config v1 pattern where `files/` and `baseline/` swaps can succeed independently.

Stage a complete config capture directory and replace the managed capture trees as one transaction boundary where practical.

If profile layout requires preserving `config.toml`, use a sibling staged payload plus rename choreography with rollback. Tests must prove an injected second-rename failure restores the old capture trees.

- [ ] **Step 6: Test restrictive umask**

Use:

```go
old := syscall.Umask(0o077)
defer syscall.Umask(old)
```

Capture source files at:

```text
0644
0600
0755
```

Assert staged snapshots retain exact intended file modes.

- [ ] **Step 7: Test backup/runtime/sensitive skips do not fail ordinary capture**

Capture succeeds and `CaptureResult.Scan` contains the skipped classifications/reasons.

Sensitive bytes must be absent from profile storage.

- [ ] **Step 8: Run capture tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Capture' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/config/capture.go internal/providers/config/capture_test.go internal/providers/config/provider.go internal/providers/config/provider_test.go
git commit -m "feat: capture sparse config overlay"
```

---

### Task 7: Diff, Verify, and Check for files, tombstones, exclusions, and dual ownership

**Files:**
- Modify: `internal/providers/config/diff.go`
- Modify: `internal/providers/config/diff_test.go`
- Modify: `internal/providers/config/provider.go` only for small shared helpers if required.

**Interfaces:**
- Consumes: path-identified scan + schema-8 state.
- Produces:

```go
func DiffConfigs(previous, next profile.Configs) []model.Change
func Diff(saved profile.Configs, current ScanSummary) []model.Change
func Verify(saved profile.Configs, current ScanSummary, effective EffectiveResolver) model.VerificationResult
func (p Provider) Check(saved profile.Configs) error
```

Do not introduce `EffectiveResolver` if a smaller private helper can compute merge candidates; Task 9 may move the final Verify signature. Keep signatures consistent once Task 9 lands.

- [ ] **Step 1: Test path-based Config diff**

Cover:

```text
new added file
modified saved file
saved file returned to baseline → removal of captured customization
new tombstone
removed tombstone
exclusion change
```

Exclusion changes should be semantic policy changes in capture diff, but need not be machine drift.

- [ ] **Step 2: Test target-only Config**

Saved:

```text
ghostty/config
```

Target additionally has:

```text
newapp/config
```

Verify should still pass if all saved intent is satisfied.

Diff/status may show `+ config newapp/config` as additive drift.

- [ ] **Step 3: Test tombstone Verify**

Saved tombstone:

```text
example/default.conf
```

Cases:

```text
target absent → satisfied
target present → missing/drift
```

- [ ] **Step 4: Test Check snapshot integrity**

Check must reject:

```text
missing desired snapshot
desired hash mismatch
desired snapshot is symlink
missing required baseline snapshot
baseline hash mismatch
baseline snapshot is symlink
added file unexpectedly requiring nonexistent baseline
file/delete duplicate path
invalid exclusion path
persisted high-confidence sensitive desired bytes
```

- [ ] **Step 5: Add dual-ownership Check input**

Add `Provider.Ownership`.

For each saved file/tombstone absolute destination:

```go
claims := p.Ownership.TrackConflict(abs)
```

If stronger saved ownership exists, return:

```text
config path nvim/init.lua conflicts with resources ownership
```

Do not make `~/.config` itself a blanket Config claim.

- [ ] **Step 6: Run diff/check tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Diff|Test.*Verify|Test.*Check' -count=1
```

Expected: PASS.

- [ ] **Step 7: Slice A review checkpoint**

Run:

```bash
go test ./internal/profile ./internal/providers/config -count=1
go test ./... -count=1
git diff --check
```

Review specifically for:

```text
schema-7 load compatibility
no accidental *.bak.* capture/tombstones
no symlink following
no semantic provider duplication
capture transaction integrity
secret persistence
```

- [ ] **Step 8: Commit checkpoint fixes if any**

```bash
git add internal/profile internal/providers/config
git commit -m "test: harden config overlay capture"
```

Only create this commit if review required changes; otherwise continue without an empty commit.

---

# Review Slice B — Merge and safe restore

### Task 8: Pure-Go mergeable-text classifier and three-way merge

**Files:**
- Create: `internal/providers/config/merge.go`
- Create: `internal/providers/config/merge_test.go`
- Modify: `go.mod`, `go.sum` only if choosing a small maintained pure-Go diff library.

**Interfaces:**
- Produces:

```go
type MergeResult struct {
    Content   []byte
    Conflicts bool
}

func IsMergeableText(content []byte) bool
func MergeText3(base, desired, currentBaseline []byte) (MergeResult, error)
```

- [ ] **Step 1: Write mergeable-text tests**

Assert:

```text
valid UTF-8 < 4 MiB, no NUL → true
contains NUL                 → false
invalid UTF-8                → false
> 4 MiB                      → false
```

Use exact boundary tests at 4 MiB.

- [ ] **Step 2: Write clean merge tests**

Use small exact fixtures.

Case 1:

```text
A:
alpha
beta

B:
alpha
beta-user

C:
upstream
alpha
beta
```

Expected merged:

```text
upstream
alpha
beta-user
```

Case 2 tests user deletion plus unrelated upstream insertion.

Case 3 tests independent additions at separate line ranges.

- [ ] **Step 3: Write overlap conflict test**

Example:

```text
A: value=old
B: value=user
C: value=upstream
```

Assert:

```go
result.Conflicts == true
```

Do not assert live conflict-marker content because callers must never write conflict markers.

- [ ] **Step 4: Write fast-path tests**

Requirements:

```text
A == B → result C (user made no delta)
A == C → result B
B == C → result B
```

These reduce library edge cases.

- [ ] **Step 5: Implement merge behind the local interface**

If using a dependency, isolate it in `merge.go`.

Do not expose dependency types.

Normalize internal line handling deterministically and preserve a trailing newline when the merge semantics require it.

- [ ] **Step 6: Test CRLF/LF behavior**

Use a captured CRLF file and an LF new baseline. Assert deterministic output and no accidental binary classification.

Choose and document one normalization rule in the code comment. Prefer preserving the desired/user newline convention when unambiguous.

- [ ] **Step 7: Run merge tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Merge' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/config/merge.go internal/providers/config/merge_test.go go.mod go.sum
git commit -m "feat: add config three way merge"
```

---

### Task 9: Restore model — explicit guarded `FileDelete`

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/model/model_test.go`
- Modify: `internal/restore/plan_validation.go`
- Modify: `internal/restore/plan_validation_test.go`

**Interfaces:**
- Produces:

```go
type FileDelete struct {
    Destination          string                  `json:"destination"`
    ExpectedExisting     *FilesystemPrecondition `json:"expected_existing,omitempty"`
    ExpectedMissing      bool                    `json:"expected_missing,omitempty"`
    Backup               bool                    `json:"backup,omitempty"`
    RejectSymlinkParents bool                    `json:"reject_symlink_parents,omitempty"`
}

type Operation struct {
    ...
    Delete *FileDelete `json:"delete,omitempty"`
}
```

Use the existing `FilesystemPrecondition` shape rather than inventing a second precondition type.

- [ ] **Step 1: Add action-exclusivity tests**

Valid:

```go
model.Operation{Delete: &model.FileDelete{...}}
```

Invalid:

```go
model.Operation{
    File:   &model.FileWrite{},
    Delete: &model.FileDelete{},
}
```

Plan validation must require exactly one executable action.

- [ ] **Step 2: Add delete validation tests**

Reject:

```text
empty destination
ExpectedMissing == true with ExpectedExisting != nil
neither ExpectedMissing nor ExpectedExisting
invalid precondition type
Backup=true while ExpectedMissing=true
```

For actual planned tombstone deletes, `ExpectedExisting` is required.

- [ ] **Step 3: Implement model + validation**

Update any error text listing action kinds from:

```text
command, copy, file, directory, or symlink
```

to include:

```text
delete
```

- [ ] **Step 4: Run model/restore validation tests**

Run:

```bash
go test ./internal/model ./internal/restore -run 'Test.*Delete|Test.*Action' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model internal/restore/plan_validation.go internal/restore/plan_validation_test.go
git commit -m "feat: model guarded file deletion"
```

---

### Task 10: Restore executor — safe delete and reusable object backup

**Files:**
- Modify: `internal/restore/executor.go`
- Modify: `internal/restore/executor_test.go`
- Modify: `internal/restore/journal.go` only if a generic renamed-object backup event helper materially simplifies the code.

**Interfaces:**
- Consumes:
  - `model.FileDelete`
  - existing `model.FilesystemPrecondition`
  - existing no-replace rename and ResourceLink backup safety.
- Produces:

```go
func executeFileDeleteWithJournal(
    operation string,
    action model.FileDelete,
    journal *Journal,
    now func() time.Time,
) error
```

Extract generic helpers only where both SymlinkWrite and FileDelete need the same semantics:

```go
func validateFilesystemPrecondition(path string, expected model.FilesystemPrecondition) error
func reserveSiblingBackupPath(destination string) (string, error)
func renameNoReplace(old, new string) error
```

- [ ] **Step 1: Test proven regular-file delete**

Create a file, hash it into an expected precondition, execute a delete with `Backup:true`.

Assert:

```text
destination missing after success
sibling backup exists with exact bytes/mode
BACKUP_CREATED journal event contains backup path
completed operation is reversible
```

- [ ] **Step 2: Test changed destination is preserved**

Plan against file A, replace with file B before execution.

Assert:

```text
delete fails precondition
file B remains
no backup created
```

- [ ] **Step 3: Test symlink/directory force backup**

For force conflict cases, use a `FilesystemPrecondition` generated from the exact live object.

Assert a symlink or directory can be moved aside intact without following/copying its contents.

The delete operation then leaves the intended path absent.

- [ ] **Step 4: Test parent symlink TOCTOU**

Change an ancestor to a symlink after plan creation.

Assert:

```text
delete fails
external target untouched
approved object not moved
```

- [ ] **Step 5: Test rollback/concurrent destination safety**

Inject failure after moving destination to backup but before final completion bookkeeping.

If another process recreates the destination, rollback must use no-replace semantics:

```text
new user destination remains
backup remains recoverable
error reports rollback blocked
```

Never `Remove` the concurrent destination.

- [ ] **Step 6: Wire delete action into executor**

In `executeOperation` count/delete dispatch:

```go
if op.Delete != nil {
    actions++
}
...
if op.Delete != nil {
    return executeFileDeleteWithJournal(op.ID, *op.Delete, journal, now)
}
```

Mark successful `Delete.Backup` operations reversible in `Execute`.

- [ ] **Step 7: Run executor tests**

Run:

```bash
go test ./internal/restore -run 'Test.*Delete|Test.*Rollback|Test.*Precondition' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/restore internal/model
git commit -m "feat: execute recoverable config deletes"
```

---

### Task 11: Config restore candidate engine and normal planning

**Files:**
- Rewrite/modify: `internal/providers/config/plan.go`
- Modify: `internal/providers/config/plan_test.go`
- Modify: `internal/providers/config/provider.go`

**Interfaces:**
- Consumes Tasks 7–9.
- Produces:

```go
type PlanOptions struct {
    Force bool
}

type DesiredCandidate struct {
    Content        []byte
    Hash           string
    Mode           uint32
    Merged         bool
    MergeConflict  bool
    BaselineGone   bool
}

func (p Provider) Plan(
    saved profile.Configs,
    current ScanSummary,
    schema int,
    from string,
    to string,
    options PlanOptions,
) (model.RestorePlan, error)
```

Use private helpers:

```go
func (p Provider) desiredCandidate(file profile.ConfigFile) (DesiredCandidate, error)
func (p Provider) planFile(...)
func (p Provider) planDelete(...)
```

- [ ] **Step 1: Test user-added file cases**

Saved added file B.

Cases:

```text
T missing → low-risk FileWrite ExpectedMissing
T == B    → no-op
T differs → skipped conflict
T symlink → skipped conflict
T dir     → skipped conflict
```

No force in this step.

- [ ] **Step 2: Test baseline unchanged cases**

Saved baseline A + desired B; current baseline C == A.

Cases:

```text
T missing → write B
T == A    → write B, medium
T == B    → no-op
T unknown → skip
```

The operation must hash-guard the saved desired snapshot and destination baseline precondition.

- [ ] **Step 3: Test cross-version clean merge**

A/B/C from Task 7 produce clean M.

Cases:

```text
T == C → write generated M
T == A → write generated M
T missing → write generated M
T == M → no-op
T unknown → skip
```

Use:

```go
model.FileWrite{
    Generated:   true,
    Content:     merged,
    SourceHash:  hash(merged),
    ...
}
```

Do not write the generated merge into the profile during planning.

- [ ] **Step 4: Test merge conflict and opaque baseline change**

Normal mode:

```text
merge conflict → skipped, target preserved
opaque A/B/C with A != C → skipped, target preserved
```

Reasons should distinguish:

```text
three-way merge conflict
baseline changed; config is not mergeable text
```

- [ ] **Step 5: Test current baseline disappeared**

Saved had baseline A/B, current baseline absent.

Cases:

```text
T missing → plan B as user-owned file, medium + warning in action/resource text
T == B    → no-op
T unknown → skip
```

Do not treat disappearance as permission to overwrite target state.

- [ ] **Step 6: Test tombstone normal planning**

Saved tombstone A.

Cases:

```text
T missing       → satisfied
T == A          → high-risk FileDelete + backup
T == C baseline → high-risk FileDelete + backup
T unknown       → skip
current baseline absent + T exists → skip
```

A safe tombstone delete is still high risk/reversible because it removes a live path.

- [ ] **Step 7: Preserve Hyprland reload dependency**

If at least one planned mutation touches a relevant Hyprland path, append the existing reload command and make it depend on all Config mutation operation IDs.

Do not reload for only:

```text
ghostty/config
lazygit/config.yml
```

- [ ] **Step 8: Run normal-plan tests**

Run:

```bash
go test ./internal/providers/config -run 'Test.*Plan' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/config/plan.go internal/providers/config/plan_test.go internal/providers/config/provider.go
git commit -m "feat: plan safe config overlay restore"
```

---

### Task 12: Config `--force` conflict resolution

**Files:**
- Modify: `internal/providers/config/plan.go`
- Modify: `internal/providers/config/plan_test.go`
- Modify if required by object replacement: `internal/model/model.go`, `internal/restore/executor.go`, tests.

**Interfaces:**
- Consumes `PlanOptions{Force:true}`.
- Produces bounded, high-risk, reversible operations for supported Config conflicts.

- [ ] **Step 1: Test force on unknown regular-file conflict**

For saved added or modified file where T is unknown:

```text
Force=false → skipped
Force=true  → HIGH replacement operation, backup required
```

The operation must carry an exact `FilesystemPrecondition` for T.

If existing `FileWrite` can only precondition regular files, either extend it with:

```go
ExpectedExisting *FilesystemPrecondition
ReplaceExisting  bool
```

or use a small generic replace operation shared with Resources.

Do not approximate directory/symlink force replacement with `os.RemoveAll`.

- [ ] **Step 2: Test force on directory/symlink at file destination**

Desired file:

```text
~/.config/foo/config
```

Target is:

```text
symlink or directory
```

Force should:

```text
move object aside intact
write desired regular file
```

with precondition + rollback.

This must reuse the safe object-backup/no-replace semantics proven in Resources.

- [ ] **Step 3: Test force on three-way merge conflict**

A/B/C conflicts.

Force candidate is exactly **B**, the captured desired user version.

Assert plan text/action data distinguishes:

```text
merge conflicted; captured user version will win
```

Do not write conflict-marker output.

- [ ] **Step 4: Test force tombstone on unknown target**

Force:

```text
backup exact target object
delete desired path
HIGH risk
reversible
```

- [ ] **Step 5: Test target changed after approval**

Plan force against T1; mutate to T2 before executor.

Expected:

```text
execution fails precondition
T2 untouched
no overwrite
```

- [ ] **Step 6: Run force tests**

Run:

```bash
go test ./internal/providers/config ./internal/restore -run 'Test.*Force|Test.*Replace|Test.*Rollback' -count=1
```

Expected: PASS.

- [ ] **Step 7: Slice B review checkpoint**

Run:

```bash
go test ./internal/providers/config ./internal/model ./internal/restore -count=1
go test ./... -count=1
go vet ./...
git diff --check
```

Review specifically for:

```text
unknown target preservation
merge direction A→B applied to C
no live conflict markers
force always backs up
no RemoveAll on user conflict
rollback no-replace safety
parent symlink checks
exact source/precondition hashes
```

- [ ] **Step 8: Commit**

```bash
git add internal/providers/config internal/model internal/restore
git commit -m "feat: force recoverable config conflicts"
```

---

# Review Slice C — Ownership, policy CLI, app integration, acceptance

### Task 13: Build Config ownership context from saved semantic providers

**Files:**
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/providers/config/scan_test.go` only if an integration fixture is clearer there.

**Interfaces:**
- Consumes: saved `profile.Data`.
- Produces:

```go
func (p configStateProvider) provider(d profile.Data) (configprovider.Provider, error)
```

with a populated `ownership.Index`.

- [ ] **Step 1: Write app-level ownership fixture**

Create roots:

```text
~/.config/omarchy/themes
~/.config/omarchy/plugins
~/.config/omarchy/hooks
~/.config/omarchy/shell.json
~/.config/nvim             Resource directory
~/.config/wezterm          generic Config
```

Populate profile Resources with:

```go
profile.Resource{
    ID: "dotfiles",
    Path: "~/.config/nvim",
    Kind: "directory",
    Strategy: "copy",
}
```

or an inbound `ResourceLink` source inside `.config`.

- [ ] **Step 2: Build claims from app dependencies/profile state**

Claims:

```text
theme user root     recursive
plugin user root    recursive
hooks root          recursive
shell user path     exact
saved Resource dir  recursive
saved Resource file exact
saved inbound ResourceLink source exact
active profile/state roots where relevant
```

Do **not** claim all of `~/.config` recursively.

Do **not** turn every previously captured generic Config file into a hard claim that blocks explicit Resource adoption.

- [ ] **Step 3: Test aggregate capture handoff**

Start with saved Config `nvim/init.lua`.

Add a saved Resources link/root owning `~/.config/nvim`.

Run Config capture.

Assert:

```text
Config state drops nvim/init.lua
Resources state remains authoritative
generic ~/.config/wezterm remains Config-captured
```

- [ ] **Step 4: Keep Resources' narrow semantic Config claims**

Resources should continue protecting the established semantic Config paths that must not be stolen accidentally.

Do not regress the recently fixed behavior that allows generic:

```bash
track ~/.config/special-app
```

while blocking explicitly semantic Config paths.

If Config v2 removes `DefaultSpecs`, move that narrow claim list to a clearly named exported helper rather than recreating a blanket `~/.config` claim:

```go
func SemanticOwnedPaths() []string
```

Initially it returns the existing Hyprland semantic paths.

- [ ] **Step 5: Run ownership/app tests**

Run:

```bash
go test ./internal/app ./internal/providers/config ./internal/providers/resources -run 'Test.*Ownership|Test.*Delegate|Test.*Track|Test.*Config' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/providers.go internal/app/app_test.go internal/providers/config internal/providers/resources
git commit -m "feat: delegate config ownership to semantic providers"
```

---

### Task 14: App adapter, recursive capture output, JSON, and `--force` plumbing

**Files:**
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Consumes:
  - `configprovider.CaptureResult`
  - `configprovider.PlanOptions`
  - scan classifications.
- Produces human/JSON config lifecycle output.

- [ ] **Step 1: Update Config provider adapter signatures**

Change:

```go
func (p configStateProvider) provider() ...
```

to:

```go
func (p configStateProvider) provider(d profile.Data) ...
```

Capture:

```go
result, err := provider.Capture(d.Config)
changes := configprovider.DiffConfigs(d.Config, result.State)
d.Config = result.State
```

Plan:

```go
provider.Plan(
    d.Config,
    current,
    d.Manifest.Schema,
    d.Manifest.Omarchy.CapturedVersion,
    info.Version,
    configprovider.PlanOptions{Force: options.Force},
)
```

- [ ] **Step 2: Update `Empty` semantics**

Config is empty only when all are empty:

```go
len(Files) == 0 &&
len(Deletes) == 0 &&
len(Excluded) == 0
```

Persistent exclusions are meaningful Config state.

- [ ] **Step 3: Add grouped human capture summary**

A capture with many unchanged files must not print them one by one.

Test output contains groups/counts such as:

```text
Config

Modified
  ~/.config/hypr/bindings.lua

Added
  ~/.config/ghostty/config

Deleted
  ~/.config/example/default.conf

Ignored
  143 unchanged Omarchy files
  4 Omarchy update backups
  18 volatile/runtime paths

Skipped
  1 sensitive path
```

Exact wording can follow existing `renderChanges`, but the test must assert backup files are counted as ignored and their filenames are not promoted to captured additions.

- [ ] **Step 4: Add structured JSON scan summary**

Machine-readable capture data should include:

```json
{
  "config": {
    "files": [],
    "deletes": [],
    "excluded": []
  },
  "scan": {
    "counts": {
      "unchanged-baseline": 143,
      "volatile": 22
    },
    "candidates": [
      {
        "path": "hypr/bindings.lua.bak.20260909",
        "classification": "volatile",
        "reason": "omarchy-update-backup"
      }
    ]
  }
}
```

Never include skipped sensitive content bytes.

- [ ] **Step 5: Test Config restore receives `--force`**

CLI:

```bash
omarchy-blueprint restore config --force --dry-run
```

must produce the force replacement operation where normal restore skips.

- [ ] **Step 6: Run app tests**

Run:

```bash
go test ./internal/app -run 'Test.*Config|Test.*Force|Test.*JSON' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/providers.go internal/app/app.go internal/app/app_test.go
git commit -m "feat: integrate recursive config overlay"
```

---

### Task 15: Config include/exclude CLI policy

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Create/modify: `internal/providers/config/policy.go`
- Modify: `internal/providers/config/policy_test.go`
- Modify: `internal/profile/profile.go` save normalization if needed.

**Interfaces:**
- Produces:

```bash
omarchy-blueprint exclude config:ghostty
omarchy-blueprint include config:ghostty
```

while preserving:

```bash
omarchy-blueprint exclude firefox
omarchy-blueprint include firefox
```

as package policy.

- [ ] **Step 1: Refactor policy command parsing tests first**

Accepted:

```text
exclude firefox            → package "firefox"
exclude config:ghostty     → config path "ghostty"
exclude config:~/.config/nvim → config path "nvim"
include config:ghostty
```

Rejected:

```text
config:
config:../ssh
config:/etc
config:~/.ssh
```

- [ ] **Step 2: Add pure Config exclusion helpers**

Use:

```go
func AddExclusion(saved profile.Configs, path string) (profile.Configs, []string, error)
func RemoveExclusion(saved profile.Configs, path string) (profile.Configs, bool, error)
```

`AddExclusion` must immediately remove saved Config files/tombstones under that subtree from metadata.

Profile snapshot cleanup should happen through the next Config capture transaction or an explicit safe profile-state pruning helper; never touch live `~/.config`.

- [ ] **Step 3: Test exclusion persistence**

Sequence:

```bash
exclude config:ghostty
capture config
capture config
```

Assert `ghostty` remains excluded and never returns to captured state.

Then:

```bash
include config:ghostty
capture config
```

assert the live file is adopted.

- [ ] **Step 4: Preserve JSON envelope compatibility**

Policy command JSON should identify:

```json
{
  "kind": "config",
  "path": "ghostty",
  "excluded": true
}
```

Do not change existing package-policy JSON keys unnecessarily.

- [ ] **Step 5: Run policy CLI tests**

Run:

```bash
go test ./internal/app ./internal/providers/config -run 'Test.*Exclude|Test.*Include|Test.*Policy' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app internal/providers/config internal/profile
git commit -m "feat: add config include exclude policy"
```

---

### Task 16: Advisory package-to-config associations

**Files:**
- Create: `internal/providers/config/association.go`
- Create: `internal/providers/config/association_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces:

```go
type Association struct {
    Package    string `json:"package"`
    ConfigPath string `json:"config_path"`
    Confidence string `json:"confidence"`
}

func RelatedConfig(
    excludedPackages []string,
    config profile.Configs,
) []Association
```

Initial confidence is only `"high"`.

- [ ] **Step 1: Test exact top-level association**

Saved Config contains:

```text
ghostty/config
```

Excluded package:

```text
ghostty
```

Expected:

```go
Association{Package:"ghostty", ConfigPath:"ghostty", Confidence:"high"}
```

- [ ] **Step 2: Test curated mismatch**

Hard-code a small map:

```go
var curatedPackageConfig = map[string]string{
    "neovim": "nvim",
}
```

Do not add speculative dozens of aliases.

Test:

```text
neovim excluded + nvim/init.lua captured → high-confidence hint
```

- [ ] **Step 3: Test no automatic exclusion**

After:

```bash
omarchy-blueprint exclude neovim
```

assert:

```text
package excluded
config nvim remains in profile
```

Human output may add:

```text
Related Config state remains included:
  ~/.config/nvim
Run:
  omarchy-blueprint exclude config:nvim
```

- [ ] **Step 4: Suppress irrelevant hints**

No hint if:

```text
config path already excluded
config path delegated/not in saved Config state
only low-confidence name similarity exists
```

- [ ] **Step 5: Run association tests**

Run:

```bash
go test ./internal/providers/config ./internal/app -run 'Test.*Association|Test.*RelatedConfig|Test.*Package.*Config' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/config/association.go internal/providers/config/association_test.go internal/app
git commit -m "feat: suggest related config exclusions"
```

---

### Task 17: Schema-7 legacy acceptance and cross-provider end-to-end tests

**Files:**
- Modify: `internal/app/app_test.go`
- Modify: `internal/profile/profile_test.go`
- Modify: `docs/manual-test-checklist.md`

**Interfaces:**
- Consumes the complete feature.
- Produces regression coverage for real upgrade/restore flows.

- [ ] **Step 1: Add schema-7 profile → schema-8 restore fixture**

Construct a schema-7 profile containing the four legacy Hyprland entries and snapshot layout.

With current code:

```text
Load
→ schema upgrades in memory
→ status works
→ restore config works before recapture
→ next capture writes path-based schema-8 state
```

Assert no snapshot is lost.

- [ ] **Step 2: Add ordinary non-dotfiles user acceptance test**

Live:

```text
modified hypr/bindings.lua
new ghostty/config
new lazygit/config.yml
unchanged baseline files
one *.bak.* update backup
```

Capture.

Assert profile stores only:

```text
modified + added desired bytes
baseline for modified file
```

and **not** unchanged/backup noise.

Reset user config to clean target, restore, Verify.

- [ ] **Step 3: Add clean cross-version merge acceptance test**

Capture A/B, change baseline to C, reset target to C, restore.

Assert target becomes clean merged M and Verify passes.

- [ ] **Step 4: Add cross-version conflict acceptance test**

Normal restore:

```text
skip
target preserved
```

Force restore:

```text
backup target
captured B installed
```

- [ ] **Step 5: Add tombstone acceptance test**

Capture baseline file absent from user.

Fresh target contains baseline.

Restore:

```text
backs up baseline
deletes destination
Verify passes
```

- [ ] **Step 6: Add Resource handoff acceptance test**

Start with Config-owned `nvim/init.lua`.

Then track an appropriate Resource and model `~/.config/nvim` as Resources-owned.

Capture all providers in registry order.

Assert:

```text
Resources owns link/root
Config drops nvim bytes
no duplicate restore destination
check passes
```

- [ ] **Step 7: Add manual checklist section**

Document these commands:

```bash
omarchy-blueprint capture config
omarchy-blueprint status config
omarchy-blueprint restore config --dry-run
omarchy-blueprint restore config
omarchy-blueprint restore config --force
omarchy-blueprint exclude config:<path>
omarchy-blueprint include config:<path>
```

Include manual inspection that `*.bak.*` files do not appear under profile `config/files/` or `config/baseline/`.

- [ ] **Step 8: Run end-to-end tests**

Run:

```bash
go test ./internal/profile ./internal/app ./internal/providers/config ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/profile internal/app internal/providers/config internal/providers/resources docs/manual-test-checklist.md
git commit -m "test: cover config overlay upgrade and restore"
```

---

### Task 18: Documentation, ADR, roadmap, and final verification

**Files:**
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Create: `docs/adr/0013-baseline-aware-config-overlay.md`
- Add/commit:
  - `docs/superpowers/specs/2026-09-09-config-overlay-design.md`
  - `docs/superpowers/plans/2026-09-09-config-overlay.md`

**Interfaces:**
- Produces user/developer documentation matching the implemented behavior.

- [ ] **Step 1: Write ADR 0013**

Decision summary must explicitly say:

```text
Config owns a sparse baseline-relative overlay, not ~/.config wholesale.
Paths are profile identity.
Added files capture by default.
Baseline removals are tombstones.
*.bak.* Omarchy update backups are ignored built-in noise.
Semantic providers outrank generic Config.
Cross-version text changes use 3-way merge.
Unknown target work is preserved.
--force requires recoverable backup.
```

Consequences should include larger Config scope and more complex classification/merge logic.

- [ ] **Step 2: Update README Config section**

Show a short example:

```text
Modified  ~/.config/hypr/bindings.lua
Added     ~/.config/ghostty/config
Deleted   ~/.config/example/default.conf
Ignored   unchanged Omarchy defaults and *.bak.* update backups
```

Document:

```bash
omarchy-blueprint exclude config:google-chrome
```

and the cross-version merge/force behavior.

- [ ] **Step 3: Update ROADMAP**

Mark baseline-aware Config expansion complete when merged.

Keep next major feature:

```text
Git Resource Policies / Dirty Git State v2
```

with:

```text
git
git+diff
copy
```

Do not pull dirty-Git implementation into this PR.

### Deferred follow-up explicitly documented: account login shell

Do **not** implement login-shell mutation in this Config PR.

The Omarchy Zsh/Fish setup scripts currently reconstruct their interactive-shell preference through `.bashrc` auto-launch plus package-owned templates; Config + Packages covers that mechanism.

A user who independently ran:

```bash
chsh -s /usr/bin/zsh
```

has account-level state outside `$HOME`.

Add a roadmap/manual note for a future semantic feature with these requirements:

```text
detect account login shell
store semantic shell name/path
require corresponding package/provenance
plan `chsh -s <resolved executable>` or another native account operation
high risk + explicit approval
never edit /etc/passwd directly
```

Do not silently infer actual login-shell intent merely because `.zshrc` or Fish config exists.

- [ ] **Step 4: Run formatting**

Run:

```bash
gofmt -w \
  internal/profile/*.go \
  internal/model/*.go \
  internal/restore/*.go \
  internal/providers/config/*.go \
  internal/app/*.go
```

Review `git status` afterward so unrelated generated/local files are not committed.

- [ ] **Step 5: Run focused verification**

Run:

```bash
go test ./internal/profile -count=1
go test ./internal/providers/config -count=1
go test ./internal/model -count=1
go test ./internal/restore -count=1
go test ./internal/app -count=1
```

Expected: all PASS.

- [ ] **Step 6: Run full verification**

Run exactly:

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Expected: all succeed with exit code 0.

- [ ] **Step 7: Run anti-regression source checks**

Inspect changed code and confirm there is no Config path that:

```text
uses os.Stat to classify a symlink target
follows symlink directories
captures *.bak.* files
creates tombstones for *.bak.* files
uses os.RemoveAll on unknown live target conflict
writes merge conflict markers live
silently excludes config because a package is excluded
claims all ~/.config against Resources
requires external git/diff3 for Config merge
persists sensitive candidate bytes
```

- [ ] **Step 8: Final real-machine acceptance**

On an Omarchy machine containing update-created backup files:

```bash
omarchy-blueprint capture config
omarchy-blueprint status config
```

Inspect:

```bash
find <profile>/config -type f
```

Confirm no user `xxx.bak.xxx` update backup is persisted.

Then test a fresh/current Omarchy baseline restore with:

```bash
omarchy-blueprint restore config --dry-run
omarchy-blueprint restore config
omarchy-blueprint check
omarchy-blueprint status config
```

If testing a known conflict:

```bash
omarchy-blueprint restore config --force --dry-run
```

Confirm the plan states that target data will be backed up before replacement/deletion.

- [ ] **Step 9: Commit documentation/final cleanup**

```bash
git add README.md ROADMAP.md docs/adr/0013-baseline-aware-config-overlay.md docs/manual-test-checklist.md docs/superpowers/specs/2026-09-09-config-overlay-design.md docs/superpowers/plans/2026-09-09-config-overlay.md
git commit -m "docs: document baseline aware config overlay"
```

---

# Required review checkpoints

Use a fresh reviewer after each slice.

## Slice A — Tasks 1–7

Reviewer focus:

```text
schema 8 / schema 7 compatibility
path normalization
backup-file ignore rule
volatile/sensitive policy
recursive scan safety
provider delegation
capture transaction
snapshot integrity
```

Do not continue with restore implementation until blocker/important findings are resolved.

## Slice B — Tasks 8–12

Reviewer focus:

```text
merge correctness
candidate selection
tombstone safety
FileDelete executor
force backup semantics
precondition/TOCTOU correctness
rollback concurrency
no conflict-marker writes
```

This is the highest-risk slice.

## Slice C — Tasks 13–18

Reviewer focus:

```text
ownership wiring
Resources handoff
CLI compatibility
JSON/human output
package advisory only
legacy acceptance
documentation and full verification
```

---

# Agent completion report

The implementation agent should report all of the following, with exact commands/results rather than a generic "tests pass":

1. Schema migration behavior from 7 → 8.
2. Config metadata/storage layout implemented.
3. Exact `xxx.bak.xxx` matcher and tests proving backup files never capture/tombstone.
4. Built-in volatile/sensitive/size policies.
5. Provider ownership/delegation behavior.
6. Three-way merge implementation and dependency choice, if any.
7. Normal conflict-preserve behavior.
8. Config `--force` backup/replace/delete behavior.
9. Tombstone restore safety.
10. Package→config advisory behavior.
11. Tests added by category.
12. Manual acceptance performed.
13. Output of:

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

14. Any deliberate deviations from the spec, with rationale.

---

# Implementation anti-pattern checklist

Before opening the PR, the agent must be able to answer **no** to every question below:

- Did Config snapshot all of `~/.config` instead of the sparse overlay?
- Did Config recursively scan all of `$HOME`?
- Did any scanner follow a symlink?
- Did any Omarchy `xxx.bak.xxx` or `*.backup-*` file reach `config/files/`, `config/baseline/`, or `ConfigDelete`?
- Did Config duplicate a Resources/Themes/Plugins/Hooks/Shell-owned path?
- Did package exclusion silently remove Config?
- Did a merge conflict write conflict markers to the live file?
- Did default restore overwrite unknown target work?
- Did `--force` replace/delete unknown work without backup?
- Did rollback delete a concurrently recreated destination?
- Did a tombstone delete a target-only file without provenance or force?
- Did Config require an external merge command?
- Did Check accept sensitive persisted bytes that capture would refuse?
- Did Config make `~/.config` or `$HOME` a blanket hard ownership claim against Resources?
- Did Config call `chsh`, `usermod`, or edit `/etc/passwd`?

If any answer is yes, the feature is not ready to merge.

---

# Approved implementation addendum — home dotfiles and Omarchy shell extensions

This plan now treats Config as a home-configuration overlay, not a `.config`-only provider.

The executor must read the updated design sections 64–69 before starting Task 1.

Key implications:

```text
profile Config path identity
→ relative to $HOME

recursive scan
→ only $HOME/.config

outside .config
→ exact curated registry only

Omarchy Zsh
→ package baselines for .bashrc/.zshrc/.inputrc when installed

Omarchy Fish
→ package baseline for .bashrc when installed

actual account login shell
→ separate future semantic feature
```

Do not run `omarchy-setup-zsh` or `omarchy-setup-fish` during restore. They overwrite files as a setup side effect; Blueprint already has the intended resulting files and must restore them through its guarded Config operations.

The packages provider remains responsible for installing `omarchy-zsh` / `omarchy-fish` when they are captured official packages.

The combination:

```text
package installed
+ Config template/customizations restored
```

is sufficient to reproduce the normal Omarchy auto-launch shell behavior.
