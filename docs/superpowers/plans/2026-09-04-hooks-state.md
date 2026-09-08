# Omarchy Hooks State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a schema-5 Hooks provider that safely captures and restores Omarchy automation hooks with exact source bytes, portable permission modes, high-risk approval UX, and conservative additive semantics.

**Architecture:** Add a focused `internal/providers/hooks` package that models only runtime-relevant flat and `.d/` hook files. Extend the existing atomic `FileWrite` primitive with optional desired/expected modes, then integrate Hooks through the existing provider registry without adding provider-specific orchestration.

**Tech Stack:** Go 1.25+, Cobra, `pelletier/go-toml/v2`, standard-library filesystem/crypto packages, existing Blueprint provider/restore abstractions.

**Spec:** `docs/superpowers/specs/2026-09-04-hooks-state-design.md`

## Global Constraints

- Profile schema becomes exactly `5`.
- Provider introduction versions remain `configSchema=2`, `defaultsSchema=3`, `shellSchema=4`, `hooksSchema=5`.
- Hooks root defaults to `$HOME/.config/omarchy/hooks`.
- Valid persisted hook paths are exactly `<event>` or `<event>.d/<filename>`.
- Unknown event names are allowed; no hard-coded Omarchy event allowlist.
- `.d` children beginning with `.` or ending with `.sample` are not captured.
- Nested `.d` directories are not traversed.
- Runtime-relevant entry symlinks are never followed or captured; they are surfaced as unmanaged warnings and left untouched. Hooks-root symlinks are rejected.
- Hook source is captured verbatim and snapshot files are stored mode `0644`.
- Desired runtime mode is stored as exactly four octal digits in `0000..0777`.
- Every Hook file mutation is `model.RiskHigh`.
- Differing target hook contents are never overwritten.
- Extra target hooks are never removed.
- Blueprint never executes captured hook source.
- Existing Config and Shell `FileWrite` behavior must remain unchanged when mode fields are nil.
- Implement with TDD: each task starts with failing tests and ends with passing focused/full tests.
- Run `gofmt`, `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint` before the final documentation commit.

---

## File map

### Create

```text
internal/providers/hooks/types.go
internal/providers/hooks/path.go
internal/providers/hooks/provider.go
internal/providers/hooks/diff.go
internal/providers/hooks/plan.go
internal/providers/hooks/path_test.go
internal/providers/hooks/provider_test.go
internal/providers/hooks/diff_test.go
internal/providers/hooks/plan_test.go
docs/adr/0010-portable-omarchy-hooks.md
docs/superpowers/specs/2026-09-04-hooks-state-design.md
docs/superpowers/plans/2026-09-04-hooks-state.md
```

### Modify

```text
internal/model/model.go
internal/profile/profile.go
internal/profile/profile_test.go
internal/restore/executor.go
internal/restore/executor_test.go
internal/app/app.go
internal/app/app_test.go
internal/app/providers.go
README.md
ROADMAP.md
docs/manual-test-checklist.md
```

### Responsibility boundaries

- `hooks/types.go`: provider state types only.
- `hooks/path.go`: path grammar, mode parsing/formatting, metadata validation.
- `hooks/provider.go`: live discovery, exact-byte capture, snapshot-tree integrity checking.
- `hooks/diff.go`: capture change summaries, live drift, verification.
- `hooks/plan.go`: conservative restore planning only.
- `profile/profile.go`: schema-5 persistence; no hook discovery logic.
- `restore/executor.go`: generic optional desired/expected mode support; no Hooks-specific branches.
- `app/providers.go`: thin Hooks adapter into `stateProvider`.
- `app/app.go`: dependency injection, category help text, human plan/progress UX.

---

### Task 1: Add schema-5 Hooks profile state

**Files:**

- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**

- Consumes: existing schema loading and `profile.Save`.
- Produces:
  - `const hooksSchema = 5`
  - `type Hooks struct { Items []Hook }`
  - `type Hook struct { Path, Hash, Mode string }`
  - `CaptureMeta.Hooks bool`
  - `Data.Hooks Hooks`

- [ ] **Step 1: Write failing migration and round-trip tests**

Add focused tests equivalent to:

```go
func TestSchema4LoadsAsSchema5WithHooksUncaptured(t *testing.T) {
    dir := profileFixture(t, 4)
    d, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if d.Manifest.Schema != 5 {
        t.Fatalf("schema = %d, want 5", d.Manifest.Schema)
    }
    if d.Manifest.Capture.Hooks {
        t.Fatal("schema-4 profile must not invent captured Hooks state")
    }
    if len(d.Hooks.Items) != 0 {
        t.Fatalf("hooks = %#v", d.Hooks.Items)
    }
}

func TestHooksRoundTripSchema5(t *testing.T) {
    dir := t.TempDir()
    d := New("test", time.Unix(0, 0))
    d.Manifest.Capture.Hooks = true
    d.Hooks.Items = []Hook{{
        Path: "post-update.d/update-rust",
        Hash: strings.Repeat("a", 64),
        Mode: "0755",
    }}
    if err := Save(dir, d); err != nil {
        t.Fatal(err)
    }
    loaded, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(loaded.Hooks, d.Hooks) {
        t.Fatalf("hooks = %#v, want %#v", loaded.Hooks, d.Hooks)
    }
}

func TestCapturedHooksRequiresHooksToml(t *testing.T) {
    dir := schema5FixtureWithCaptureHooks(t)
    if _, err := Load(dir); err == nil ||
        !strings.Contains(err.Error(), "hooks state marked captured") {
        t.Fatalf("err = %v", err)
    }
}

func TestProviderSchemaIntroductionVersionsStayPinned(t *testing.T) {
    if configSchema != 2 || defaultsSchema != 3 ||
        shellSchema != 4 || hooksSchema != 5 {
        t.Fatalf("provider schema thresholds changed")
    }
    if hooksSchema > Schema {
        t.Fatalf("hooksSchema=%d exceeds Schema=%d", hooksSchema, Schema)
    }
}
```

Use existing profile fixture helpers rather than creating duplicate fixture infrastructure.

- [ ] **Step 2: Run focused tests and confirm failure**

```bash
go test ./internal/profile -run 'Schema4|Hooks|IntroductionVersions' -count=1
```

Expected: FAIL because schema 5 and Hooks fields do not exist.

- [ ] **Step 3: Implement schema-5 persistence**

In `internal/profile/profile.go`:

```go
const Schema = 5

const (
    configSchema   = 2
    defaultsSchema = 3
    shellSchema    = 4
    hooksSchema    = 5
)

type Hooks struct {
    Items []Hook `json:"hooks" toml:"hook"`
}

type Hook struct {
    Path string `json:"path" toml:"path"`
    Hash string `json:"hash" toml:"hash"`
    Mode string `json:"mode" toml:"mode"`
}
```

Add:

```go
Hooks bool `toml:"hooks"`
```

to `CaptureMeta`, and:

```go
Hooks Hooks `json:"hooks"`
```

to `Data`.

In `Load`, read `hooks/hooks.toml` only when:

```go
loadedSchema >= hooksSchema
```

If the file is missing while `d.Manifest.Capture.Hooks` is true, return:

```text
hooks state marked captured but hooks/hooks.toml is missing
```

In `Save`:

- create `hooks/`;
- sort Hooks metadata lexically by `Path`;
- marshal `d.Hooks`;
- atomically write `hooks/hooks.toml`.

Add a deterministic `sortHooks` helper. Do not tie Hooks loading to the latest `Schema` constant.

- [ ] **Step 4: Run profile tests**

```bash
go test ./internal/profile -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go
git commit -m "feat: add hooks profile schema"
```

---

### Task 2: Extend atomic file writes with desired and expected modes

**Files:**

- Modify: `internal/model/model.go`
- Modify: `internal/restore/executor.go`
- Modify: `internal/restore/executor_test.go`

**Interfaces:**

- Consumes: existing `model.FileWrite`, `writeFileAtomic`, `validateDestination`.
- Produces:

```go
Mode         *uint32 `json:"mode,omitempty"`
ExpectedMode *uint32 `json:"expected_mode,omitempty"`
```

Existing callers that leave both nil behave exactly as before.

- [ ] **Step 1: Write failing executor tests**

Add:

```go
func modePtr(v uint32) *uint32 { return &v }
```

Then add tests covering the generic primitive:

```go
func TestFileWriteExplicitModeAppliesToMissingDestination(t *testing.T) {
    source := writeFixtureFile(t, "source", "hook\n", 0o644)
    destination := filepath.Join(t.TempDir(), "hook")
    action := model.FileWrite{
        Source:          source,
        Destination:     destination,
        SourceHash:      mustHash(t, source),
        ExpectedMissing: true,
        Mode:            modePtr(0o755),
    }
    executeFileWriteFixture(t, action)
    info, err := os.Stat(destination)
    if err != nil {
        t.Fatal(err)
    }
    if got := info.Mode().Perm(); got != 0o755 {
        t.Fatalf("mode = %04o, want 0755", got)
    }
}

func TestFileWriteExplicitModeOverridesExistingDestinationMode(t *testing.T) {
    source := writeFixtureFile(t, "source", "same\n", 0o644)
    destination := writeFixtureFile(t, "destination", "same\n", 0o600)
    action := model.FileWrite{
        Source:       source,
        Destination:  destination,
        SourceHash:   mustHash(t, source),
        ExpectedHash: mustHash(t, destination),
        ExpectedMode: modePtr(0o600),
        Backup:       true,
        Mode:         modePtr(0o755),
    }
    executeFileWriteFixture(t, action)
    info, _ := os.Stat(destination)
    if info.Mode().Perm() != 0o755 {
        t.Fatalf("mode = %04o", info.Mode().Perm())
    }
}

func TestFileWriteExpectedModeMismatchBlocksMutation(t *testing.T) {
    source := writeFixtureFile(t, "source", "desired\n", 0o644)
    destination := writeFixtureFile(t, "destination", "current\n", 0o644)
    action := model.FileWrite{
        Source:       source,
        Destination:  destination,
        SourceHash:   mustHash(t, source),
        ExpectedHash: mustHash(t, destination),
        ExpectedMode: modePtr(0o600),
        Mode:         modePtr(0o755),
    }
    err := runFileWriteFixture(t, action)
    if err == nil || !strings.Contains(err.Error(), "mode mismatch") {
        t.Fatalf("err = %v", err)
    }
    got, _ := os.ReadFile(destination)
    if string(got) != "current\n" {
        t.Fatal("destination mutated despite failed mode precondition")
    }
}

func TestFileWriteRejectsInvalidDesiredMode(t *testing.T) {
    action := validFileWriteFixture(t)
    action.Mode = modePtr(0o1000)
    if err := runFileWriteFixture(t, action); err == nil {
        t.Fatal("mode > 0777 must fail")
    }
}

func TestFileWriteExpectedModeCannotAccompanyExpectedMissing(t *testing.T) {
    action := validMissingFileWriteFixture(t)
    action.ExpectedMode = modePtr(0o644)
    if err := runFileWriteFixture(t, action); err == nil {
        t.Fatal("ExpectedMode with ExpectedMissing must fail")
    }
}
```

Also add a regression test proving nil `Mode` preserves the existing destination mode on replacement.

- [ ] **Step 2: Run focused executor tests and verify failure**

```bash
go test ./internal/restore -run 'FileWrite.*Mode' -count=1
```

Expected: FAIL because mode fields/behavior do not exist.

- [ ] **Step 3: Add fields to `model.FileWrite`**

```go
type FileWrite struct {
    Source          string  `json:"source"`
    Destination     string  `json:"destination"`
    SourceHash      string  `json:"source_hash"`
    ExpectedHash    string  `json:"expected_hash,omitempty"`
    ExpectedMissing bool    `json:"expected_missing,omitempty"`
    Backup          bool    `json:"backup"`
    Mode            *uint32 `json:"mode,omitempty"`
    ExpectedMode    *uint32 `json:"expected_mode,omitempty"`
}
```

- [ ] **Step 4: Validate mode preconditions in the executor**

At the start of `writeFileAtomic`:

```go
if action.Mode != nil && *action.Mode > 0o777 {
    return fmt.Errorf("file write mode is invalid: %04o", *action.Mode)
}
if action.ExpectedMode != nil && *action.ExpectedMode > 0o777 {
    return fmt.Errorf("file write expected mode is invalid: %04o", *action.ExpectedMode)
}
if action.ExpectedMissing && action.ExpectedMode != nil {
    return fmt.Errorf(
        "file write expected mode requires an existing destination: %s",
        operation,
    )
}
```

In `validateDestination`, after content hash validation:

```go
if action.ExpectedMode != nil &&
    uint32(info.Mode().Perm()) != *action.ExpectedMode {
    return nil, fmt.Errorf(
        "file write destination mode mismatch: %s",
        action.Destination,
    )
}
```

Because `validateDestination` is already called twice, this protects the final pre-rename boundary too.

- [ ] **Step 5: Apply explicit desired mode**

Replace the current temp mode selection with:

```go
mode := sourceInfo.Mode().Perm()
if destinationInfo != nil {
    mode = destinationInfo.Mode().Perm()
}
if action.Mode != nil {
    mode = os.FileMode(*action.Mode)
}
```

Keep backup, sync, rename, and cancellation behavior unchanged.

- [ ] **Step 6: Run focused and full tests**

```bash
go test ./internal/restore -count=1
go test ./... -count=1
```

Expected: PASS, including existing Config and Shell restore tests.

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/restore/executor.go internal/restore/executor_test.go
git commit -m "feat: support explicit file restore modes"
```

---

### Task 3: Implement Hook path grammar and mode parsing

**Files:**

- Create: `internal/providers/hooks/types.go`
- Create: `internal/providers/hooks/path.go`
- Create: `internal/providers/hooks/path_test.go`

**Interfaces:**

- Produces:

```go
type DetectedHook struct {
    Path string
    Hash string
    Mode string
    Raw  []byte
}

type State struct {
    Items []DetectedHook
}

type Provider struct {
    UserDir    string
    ProfileDir string
}

func ValidatePath(path string) error
func ParseMode(mode string) (uint32, error)
func FormatMode(mode os.FileMode) (string, error)
func ValidateMetadata(items []profile.Hook) error
```

- [ ] **Step 1: Write failing path/mode tests**

```go
func TestValidatePathAcceptsFlatAndDirectoryHooks(t *testing.T) {
    for _, path := range []string{
        "post-boot",
        "future-event",
        "foo.d",
        "post-update.d/update-rust",
        ".private-event",
    } {
        if err := ValidatePath(path); err != nil {
            t.Fatalf("%q rejected: %v", path, err)
        }
    }
}

func TestValidatePathRejectsNonRuntimeShapes(t *testing.T) {
    for _, path := range []string{
        "",
        ".",
        "..",
        "/absolute",
        "../outside",
        "event.d/../outside",
        `event\name`,
        "event.d/",
        ".d/hook",
        "event.d/.hidden",
        "event.d/example.sample",
        "event.d/nested/hook",
    } {
        if err := ValidatePath(path); err == nil {
            t.Fatalf("%q must be rejected", path)
        }
    }
}

func TestParseModeRequiresFourOctalDigits(t *testing.T) {
    valid := map[string]uint32{
        "0000": 0,
        "0644": 0o644,
        "0755": 0o755,
        "0777": 0o777,
    }
    for raw, want := range valid {
        got, err := ParseMode(raw)
        if err != nil || got != want {
            t.Fatalf("%q => %o, %v", raw, got, err)
        }
    }
    for _, raw := range []string{"755", "07550", "0888", "-755", "0x1ff"} {
        if _, err := ParseMode(raw); err == nil {
            t.Fatalf("%q must be rejected", raw)
        }
    }
}

func TestValidateMetadataRejectsDuplicates(t *testing.T) {
    items := []profile.Hook{
        {Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0644"},
        {Path: "post-boot", Hash: strings.Repeat("b", 64), Mode: "0755"},
    }
    if err := ValidateMetadata(items); err == nil {
        t.Fatal("duplicate hook paths must fail")
    }
}
```

Also test that hashes must be lowercase 64-character hex.

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/providers/hooks -run 'ValidatePath|Mode|Metadata' -count=1
```

Expected: FAIL because the package/functions do not exist.

- [ ] **Step 3: Implement types**

`types.go`:

```go
package hooks

type DetectedHook struct {
    Path string
    Hash string
    Mode string
    Raw  []byte
}

type State struct {
    Items []DetectedHook
}

type Provider struct {
    UserDir    string
    ProfileDir string
}
```

- [ ] **Step 4: Implement canonical path validation**

`path.go` core logic:

```go
func ValidatePath(path string) error {
    if path == "" || strings.Contains(path, `\`) || filepath.IsAbs(path) {
        return fmt.Errorf("invalid hook path %q", path)
    }
    parts := strings.Split(path, "/")
    switch len(parts) {
    case 1:
        return validateComponent(parts[0], "event")
    case 2:
        dir, name := parts[0], parts[1]
        if err := validateComponent(dir, "event directory"); err != nil {
            return err
        }
        if !strings.HasSuffix(dir, ".d") ||
            strings.TrimSuffix(dir, ".d") == "" {
            return fmt.Errorf("invalid hook event directory %q", dir)
        }
        if err := validateComponent(name, "hook filename"); err != nil {
            return err
        }
        if strings.HasPrefix(name, ".") {
            return fmt.Errorf("hidden .d hook is not runtime-relevant: %q", path)
        }
        if strings.HasSuffix(name, ".sample") {
            return fmt.Errorf("sample hook is not runtime-relevant: %q", path)
        }
        return nil
    default:
        return fmt.Errorf("hook path must be flat or one .d child: %q", path)
    }
}
```

`validateComponent` rejects empty, `.`, `..`, and `/`.

- [ ] **Step 5: Implement mode/hash metadata validation**

```go
func ParseMode(raw string) (uint32, error) {
    if len(raw) != 4 {
        return 0, fmt.Errorf("hook mode must contain four octal digits")
    }
    for _, r := range raw {
        if r < '0' || r > '7' {
            return 0, fmt.Errorf("invalid hook mode %q", raw)
        }
    }
    value, err := strconv.ParseUint(raw, 8, 32)
    if err != nil || value > 0o777 {
        return 0, fmt.Errorf("invalid hook mode %q", raw)
    }
    return uint32(value), nil
}
```

`FormatMode` rejects special bits and returns:

```go
fmt.Sprintf("%04o", mode.Perm())
```

`ValidateMetadata` validates path/hash/mode and duplicate paths.

- [ ] **Step 6: Run package tests**

```bash
go test ./internal/providers/hooks -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/hooks/types.go internal/providers/hooks/path.go internal/providers/hooks/path_test.go
git commit -m "feat: define portable hook metadata"
```

---

### Task 4: Implement runtime Hook discovery

**Files:**

- Create: `internal/providers/hooks/provider.go`
- Create: `internal/providers/hooks/provider_test.go`
- Reuse: `internal/content/file.go`

**Interfaces:**

- Consumes: `Provider.UserDir`, `DetectedHook`, `ValidatePath`, `FormatMode`.
- Produces:

```go
func (p Provider) Detect() (State, error)
```

- [ ] **Step 1: Write failing discovery tests**

Add:

```go
func TestDetectMissingRootIsEmpty(t *testing.T)
func TestDetectCapturesFlatHook(t *testing.T)
func TestDetectCapturesUnknownEventDirectory(t *testing.T)
func TestDetectSortsDirectoryHooks(t *testing.T)
func TestDetectIgnoresSamplesHiddenFilesAndNestedDirectories(t *testing.T)
func TestDetectIgnoresNonDotDDirectory(t *testing.T)
func TestDetectRejectsRootSymlink(t *testing.T)
func TestDetectLeavesRuntimeRelevantSymlinkUnmanaged(t *testing.T)
func TestDetectRetainsExactBytesAndMode(t *testing.T)
```

Use an ignored fixture tree:

```text
hooks/
└── post-update.d/
    ├── .hidden
    ├── example.sample
    ├── nested/
    │   └── ignored
    └── active
```

Expected state contains only `post-update.d/active`.

- [ ] **Step 2: Run discovery tests and verify failure**

```bash
go test ./internal/providers/hooks -run '^TestDetect' -count=1
```

Expected: FAIL because `Detect` is missing.

- [ ] **Step 3: Implement Hooks-root scanning**

Use `os.Lstat`:

```go
info, err := os.Lstat(p.UserDir)
if errors.Is(err, os.ErrNotExist) {
    return State{}, nil
}
if err != nil {
    return State{}, err
}
if info.Mode()&os.ModeSymlink != 0 {
    return State{}, fmt.Errorf("hooks root is a symlink: %s", p.UserDir)
}
if !info.IsDir() {
    return State{}, fmt.Errorf("hooks root is not a directory: %s", p.UserDir)
}
```

Read root entries with `os.ReadDir`.

For each root entry:

- symlink: record as unmanaged and continue;
- regular file: capture as flat Hook;
- directory ending `.d`: scan immediate children;
- other directory/special file: ignore.

- [ ] **Step 4: Implement `.d` scanning**

The runtime-order checks are:

```go
name := entry.Name()
if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".sample") {
    continue
}
if entry.Type()&os.ModeSymlink != 0 {
    recordUnmanagedSymlink(rel)
    continue
}
if entry.IsDir() {
    continue
}
info, err := entry.Info()
if err != nil {
    return err
}
if !info.Mode().IsRegular() {
    continue
}
```

Never recurse.

- [ ] **Step 5: Read exact bytes once**

Use `content.OpenRegularFile`, then:

```go
raw, err := io.ReadAll(f)
if err != nil {
    return DetectedHook{}, err
}
sum := sha256.Sum256(raw)
mode, err := FormatMode(info.Mode())
if err != nil {
    return DetectedHook{}, err
}
return DetectedHook{
    Path: rel,
    Hash: hex.EncodeToString(sum[:]),
    Mode: mode,
    Raw:  raw,
}, nil
```

Sort `State.Items` lexically by `Path`.

- [ ] **Step 6: Run discovery tests**

```bash
go test ./internal/providers/hooks -run '^TestDetect' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/hooks/provider.go internal/providers/hooks/provider_test.go
git commit -m "feat: detect Omarchy hooks"
```

---

### Task 5: Capture exact Hook snapshots and validate profile integrity

**Files:**

- Modify: `internal/providers/hooks/provider.go`
- Modify: `internal/providers/hooks/provider_test.go`

**Interfaces:**

- Produces:

```go
func (p Provider) Capture(state State) (profile.Hooks, error)
func (p Provider) Check(saved profile.Hooks) error
```

- [ ] **Step 1: Write failing capture/check tests**

Add:

```go
func TestCaptureStoresExactBytesAsInertFiles(t *testing.T)
func TestCaptureRecordsDesiredMode(t *testing.T)
func TestCaptureEmptyStateRemovesStaleSnapshots(t *testing.T)
func TestCaptureRecaptureRemovesDeletedHook(t *testing.T)
func TestCheckRejectsDuplicateMetadata(t *testing.T)
func TestCheckRejectsMissingOrTamperedSnapshot(t *testing.T)
func TestCheckRejectsOrphanSnapshot(t *testing.T)
func TestCheckRejectsSnapshotSymlink(t *testing.T)
```

In the inert-file test, source mode is `0755`; captured snapshot mode must be `0644` while metadata remains `"0755"`.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
go test ./internal/providers/hooks -run 'Capture|Check' -count=1
```

Expected: FAIL because Capture/Check are missing.

- [ ] **Step 3: Implement staged capture**

Create staging beneath:

```text
<profile>/hooks/.capture-*
```

For each detected Hook:

1. validate path/hash/mode;
2. destination = `staging/files/<Path>`;
3. create parent directories `0755`;
4. write exact `Raw` bytes to a new regular file mode `0644`;
5. verify the bytes hash to `DetectedHook.Hash`.

Build metadata explicitly:

```go
captured := profile.Hooks{Items: make([]profile.Hook, 0, len(state.Items))}
for _, item := range state.Items {
    captured.Items = append(captured.Items, profile.Hook{
        Path: item.Path,
        Hash: item.Hash,
        Mode: item.Mode,
    })
}
sort.Slice(captured.Items, func(i, j int) bool {
    return captured.Items[i].Path < captured.Items[j].Path
})
```

Replace only the `hooks/files` tree using the same staged-directory swap pattern already used by Config. Empty state replaces the old tree with an empty tree.

- [ ] **Step 4: Implement `Check` metadata-to-snapshot validation**

Start with:

```go
if err := ValidateMetadata(saved.Items); err != nil {
    return err
}
```

For each metadata entry:

```go
snapshot := filepath.Join(
    p.ProfileDir,
    "hooks",
    "files",
    filepath.FromSlash(item.Path),
)
hash, err := content.HashRegularFile(snapshot)
if err != nil {
    return fmt.Errorf("hook snapshot %q: %w", item.Path, err)
}
if hash != item.Hash {
    return fmt.Errorf(
        "hook snapshot hash mismatch for %q (%s != %s)",
        item.Path,
        hash,
        item.Hash,
    )
}
```

Then walk `hooks/files` without following symlinks:

- directories are only parents;
- symlink/special file => hard error;
- each regular file must map to saved metadata;
- orphan regular file => hard error;
- invalid relative path => hard error.

If `hooks/files` does not exist and `saved.Items` is empty, accept it.

- [ ] **Step 5: Run Hooks tests**

```bash
go test ./internal/providers/hooks -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/hooks/provider.go internal/providers/hooks/provider_test.go
git commit -m "feat: capture and validate hook snapshots"
```

---

### Task 6: Add Hook capture changes, drift, and verification

**Files:**

- Create: `internal/providers/hooks/diff.go`
- Create: `internal/providers/hooks/diff_test.go`

**Interfaces:**

- Produces:

```go
func DiffCaptures(previous, next profile.Hooks) []model.Change
func Diff(saved profile.Hooks, current State) []model.Change
func Verify(saved profile.Hooks, current State) model.VerificationResult
```

- [ ] **Step 1: Write failing semantic diff tests**

Add:

```go
func TestDiffNoChange(t *testing.T)
func TestDiffReportsAddedHook(t *testing.T)
func TestDiffReportsRemovedHook(t *testing.T)
func TestDiffReportsContentChange(t *testing.T)
func TestDiffReportsModeOnlyChange(t *testing.T)
func TestDiffReportsCombinedContentAndModeChange(t *testing.T)
func TestVerifyIgnoresExtraTargetHooks(t *testing.T)
func TestVerifyFailsMissingOrMismatchedDesiredHook(t *testing.T)
```

Expected summaries:

```text
+ hook post-update.d/update-rust
- hook post-boot
~ hook post-boot changed
~ hook post-boot mode 0644 → 0755
~ hook post-boot content and mode changed (0644 → 0755)
```

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/providers/hooks -run 'Diff|Verify' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement map-based semantic comparison**

Use path as identity:

```go
func savedMap(items []profile.Hook) map[string]profile.Hook
func currentMap(items []DetectedHook) map[string]DetectedHook
```

Emit one change per path and sort by `Name`.

For verification, only saved desired Hooks are requirements. Use missing labels:

```text
hook:post-update.d/update-rust
```

Extra current Hooks do not enter `Missing`.

- [ ] **Step 4: Run Hooks tests**

```bash
go test ./internal/providers/hooks -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/providers/hooks/diff.go internal/providers/hooks/diff_test.go
git commit -m "feat: diff and verify hook state"
```

---

### Task 7: Plan high-risk additive Hook restore

**Files:**

- Create: `internal/providers/hooks/plan.go`
- Create: `internal/providers/hooks/plan_test.go`

**Interfaces:**

- Consumes: `ParseMode`, `Provider.ProfileDir`, current `State`, schema/from/to metadata.
- Produces:

```go
func (p Provider) Plan(
    saved profile.Hooks,
    current State,
    schema int,
    from string,
    to string,
) (model.RestorePlan, error)
```

Operation IDs must be deterministic and path-safe. Use:

```go
func operationID(path string) string {
    sum := sha256.Sum256([]byte(path))
    return "hooks.write." + hex.EncodeToString(sum[:8])
}
```

Keep the human-readable path in `Resource` and `Items`.

- [ ] **Step 1: Write failing plan tests**

Add:

```go
func TestPlanCreatesMissingHookHighRisk(t *testing.T)
func TestPlanNoOpWhenHashAndModeMatch(t *testing.T)
func TestPlanRepairsModeOnlyDrift(t *testing.T)
func TestPlanSkipsDifferentTargetContent(t *testing.T)
func TestPlanLeavesExtraTargetHookInstalled(t *testing.T)
func TestPlanNeverEmitsCommandOperation(t *testing.T)
```

For missing Hook assert:

```go
op.Provider == "hooks"
op.Action == "write"
op.Resource == "hook:post-update.d/update-rust"
op.Risk == model.RiskHigh
op.File.ExpectedMissing
op.File.Mode != nil && *op.File.Mode == 0o755
len(op.Command) == 0
```

For mode-only drift assert:

```go
op.File.ExpectedHash == saved.Hash
op.File.ExpectedMode != nil
op.File.Backup
op.Reversible
```

- [ ] **Step 2: Run plan tests and verify failure**

```bash
go test ./internal/providers/hooks -run '^TestPlan' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement plan validation and indexing**

Start Plan with:

```go
if err := ValidateMetadata(saved.Items); err != nil {
    return model.RestorePlan{}, err
}
```

Build current map once.

For each saved Hook:

- resolve snapshot source only after path validation;
- parse desired mode;
- missing => create;
- same hash+mode => no-op;
- same hash, wrong mode => `FileWrite` with `ExpectedHash`, `ExpectedMode`, `Backup`, `Mode`;
- different hash => `Skipped` with overwrite disabled.

For extra current Hooks, append removal-disabled skips.

Sort saved/current paths lexically so the plan is deterministic.

- [ ] **Step 4: Ensure source integrity metadata is used**

Every FileWrite must contain:

```go
SourceHash: savedHook.Hash
```

The executor rechecks the profile snapshot before mutation.

- [ ] **Step 5: Run Hooks and restore tests**

```bash
go test ./internal/providers/hooks ./internal/restore -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/hooks/plan.go internal/providers/hooks/plan_test.go
git commit -m "feat: plan safe hook restoration"
```

---

### Task 8: Integrate Hooks through the generic provider registry

**Files:**

- Modify: `internal/app/app.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**

- Adds to `Dependencies`:

```go
HooksDir func() (string, error)
```

- Adds `hooksStateProvider` implementing all `stateProvider` methods.
- Provider ID: `"hooks"`.

- [ ] **Step 1: Write failing provider/CLI integration tests**

Add:

```go
func TestHooksCategoryIsRegistered(t *testing.T)
func TestHooksRequiresCaptureBeforeTargetedStatus(t *testing.T)
func TestAggregateCaptureMarksHooksCapturedWhenEmpty(t *testing.T)
func TestCaptureHooksRecaptureReportsChanges(t *testing.T)
func TestCheckRunsHooksIntegrityValidation(t *testing.T)
```

Also assert command argument validation accepts `hooks`.

- [ ] **Step 2: Run app tests and verify failure**

```bash
go test ./internal/app -run 'Hooks|hooks' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Add dependency injection**

In `Dependencies`:

```go
HooksDir func() (string, error)
```

Default:

```go
func defaultHooksDir() (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(home, ".config", "omarchy", "hooks"), nil
}
```

Initialize it in `Execute` when nil.

- [ ] **Step 4: Register provider after Shell**

Import:

```go
hooksprovider "github.com/Grenco/omarchy-blueprint/internal/providers/hooks"
```

Append:

```go
hooksStateProvider{deps: deps, opt: opt},
```

after `shellStateProvider`.

Update `captureRequiredError`, `providerStateLabel`, and `providerCheckLabel` with Hooks wording.

- [ ] **Step 5: Implement the thin adapter**

```go
type hooksStateProvider struct {
    deps Dependencies
    opt  *options
}

func (hooksStateProvider) ID() string { return "hooks" }
func (hooksStateProvider) CategoryEnabled() bool { return true }
func (hooksStateProvider) Captured(d profile.Data) bool {
    return d.Manifest.Capture.Hooks
}
```

Provider constructor:

```go
func (p hooksStateProvider) provider() (hooksprovider.Provider, error) {
    dir, err := p.deps.HooksDir()
    return hooksprovider.Provider{
        UserDir: dir,
        ProfileDir: p.opt.profileDir,
    }, err
}
```

Capture must Detect once, Capture that state, diff previous/new desired state, set `d.Hooks`, and set `d.Manifest.Capture.Hooks = true` even when the state is empty.

Implement Diff/Plan/Verify/Check by detecting current state once per method and delegating.

Implement `stateEmptyer` so empty Hooks output can be omitted while capture metadata remains true.

- [ ] **Step 6: Update Cobra usage strings**

Update category text to:

```text
capture [packages|themes|plugins|config|defaults|shell|hooks]
status  [packages|themes|plugins|config|defaults|shell|hooks]
diff    [packages|themes|plugins|config|defaults|shell|hooks]
restore [packages|themes|plugins|config|defaults|shell|hooks]
```

Do not add a separate Hooks orchestration path.

- [ ] **Step 7: Run app and full tests**

```bash
go test ./internal/app -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/providers.go internal/app/app_test.go
git commit -m "feat: integrate hooks provider"
```

---

### Task 9: Add high-risk Hook restore UX

**Files:**

- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**

- Consumes: existing `renderPlan` and `renderProgress`.
- Produces Hooks-specific human output only; JSON plan shape is unchanged.

- [ ] **Step 1: Write failing render tests**

```go
func TestRenderPlanWarnsAboutExecutableHooksOnce(t *testing.T) {
    plan := model.RestorePlan{Operations: []model.Operation{
        {ID: "hooks.write.a", Provider: "hooks", Action: "write",
            Resource: "hook:post-boot", Risk: model.RiskHigh},
        {ID: "hooks.write.b", Provider: "hooks", Action: "write",
            Resource: "hook:post-update.d/x", Risk: model.RiskHigh},
    }}
    text := renderPlan(plan, true)
    warning := "Omarchy hooks are arbitrary user code"
    if strings.Count(text, warning) != 1 {
        t.Fatalf("warning count in %q", text)
    }
}

func TestRenderHookProgressUsesHookPath(t *testing.T) {
    var out bytes.Buffer
    op := model.Operation{
        Provider: "hooks",
        Action: "write",
        Resource: "hook:post-update.d/update-rust",
    }
    renderProgress(&out, restore.Progress{
        Type: restore.ProgressStarted,
        Operation: op,
    })
    if !strings.Contains(
        out.String(),
        "Restoring hook post-update.d/update-rust",
    ) {
        t.Fatalf("progress = %q", out.String())
    }
}
```

- [ ] **Step 2: Run focused render tests and verify failure**

```bash
go test ./internal/app -run 'Render.*Hook' -count=1
```

Expected: FAIL.

- [ ] **Step 3: Add one plan warning**

Detect any Hooks mutation and print exactly once:

```text
! Omarchy hooks are arbitrary user code that runs automatically on system events. Review captured hook source before approving this restore.
```

Do not reuse the plugin warning; Hooks are a separate execution mechanism.

- [ ] **Step 4: Add Hooks progress branch before package fallback**

For provider `"hooks"`:

```go
path := strings.TrimPrefix(event.Operation.Resource, "hook:")
```

Render started/completed/heartbeat/failed messages around `Restoring hook <path>` and return before package fallback.

- [ ] **Step 5: Run app tests**

```bash
go test ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "feat: add hooks restore warnings"
```

---

### Task 10: Add end-to-end Hooks vertical slices

**Files:**

- Modify: `internal/app/app_test.go`
- Modify only existing test helpers if fixture support is necessary.

**Interfaces:**

- Exercises public `Execute` flow and real profile files.
- No new production interfaces.

- [ ] **Step 1: Write failing missing-hook vertical slice**

The test must perform:

```text
init
→ create source hook 0755
→ capture hooks
→ inspect profile snapshot mode 0644 + metadata mode 0755
→ remove live hook
→ status hooks returns drift exit 2
→ restore hooks --dry-run contains high-risk warning
→ restore hooks --yes
→ restored bytes exactly match
→ restored mode is 0755
→ status hooks returns 0
```

Use a fake Omarchy runner only for existing Omarchy version detection. Hooks restore itself must not require a runner command.

- [ ] **Step 2: Run the vertical slice and verify failure**

```bash
go test ./internal/app -run 'HooksVerticalSlice' -count=1
```

Expected: FAIL until all integration details are correct.

- [ ] **Step 3: Fix only integration defects exposed by the slice**

Do not add new feature semantics here. Limit fixes to path wiring, labels, fixture setup, or provider capture flag handling.

- [ ] **Step 4: Add differing-target conflict slice**

Flow:

```text
capture hook with bytes A
change live target to bytes B
restore hooks --dry-run
assert hook source B is described as skipped
restore hooks --yes
assert bytes B remain untouched
assert verification reports desired hook still missing
```

- [ ] **Step 5: Add aggregate compatibility slice**

Capture at least Packages + Hooks, then aggregate restore and assert both plans coexist without new dependency linking or duplicate operation IDs.

- [ ] **Step 6: Run full suite**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/app_test.go
git commit -m "test: cover hooks restore workflow"
```

---

### Task 11: Document Hooks behavior and manual validation

**Files:**

- Create: `docs/adr/0010-portable-omarchy-hooks.md`
- Ensure present: `docs/superpowers/specs/2026-09-04-hooks-state-design.md`
- Ensure present: `docs/superpowers/plans/2026-09-04-hooks-state.md`
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Modify: `docs/manual-test-checklist.md`

**Interfaces:**

- No production interfaces.
- Documents exact schema-5 behavior implemented by Tasks 1-10.

- [ ] **Step 1: Add ADR 0010**

The ADR must state:

- flat + `.d` runtime forms;
- unknown events allowed;
- `.d` samples/dotfiles/nested content ignored;
- snapshot mode `0644`;
- metadata owns desired mode;
- high-risk additive restore;
- differing targets never overwritten;
- Hooks never execute during Blueprint operations;
- secrets are stored verbatim.

- [ ] **Step 2: Update README**

Add Hooks to supported providers, CLI examples, profile layout, safety behavior, and current boundaries.

Include:

> Hook source is stored verbatim in the profile. Hooks intended for version control should read credentials from environment variables, a password manager, or another external secret source rather than embedding secret values directly.

- [ ] **Step 3: Update roadmap**

Mark first-class Hooks complete while keeping machine overlays, input/monitors, user-selected directories, migration engine, and TUI as separate future work.

Do not claim generic directory support is implemented.

- [ ] **Step 4: Add manual Hooks checklist**

Include:

```text
flat post-boot hook
.d hook
ignored .sample
ignored nested file
unknown event
0644 and 0755 capture
empty capture then later drift
missing restore
mode-only repair
differing-content refusal
extra-hook additive behavior
backup permission verification
Git clone of profile followed by restore preserving metadata mode
confirmation that no Hook runs merely because Blueprint restored it
```

For the final point, use a hook that writes a marker file when executed and confirm the marker never appears during Blueprint capture/restore/check.

- [ ] **Step 5: Run documentation consistency searches**

```bash
rg 'schema 4|Schema = 4|shell state.*postponed' README.md ROADMAP.md docs internal || true
rg 'hooks' README.md ROADMAP.md docs/adr docs/manual-test-checklist.md
```

Inspect each hit and correct stale statements without rewriting historical ADR context.

- [ ] **Step 6: Run formatting and verification**

```bash
gofmt -w internal
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: all commands succeed.

- [ ] **Step 7: Commit**

```bash
git add README.md ROADMAP.md docs internal
git commit -m "docs: document portable Omarchy hooks"
```

---

## Final review checklist

Before opening the PR, verify these invariants manually:

- [ ] `hooksSchema` is pinned to `5`, not `Schema`.
- [ ] Schema-4 profiles load with `Capture.Hooks == false`.
- [ ] Empty Hooks capture still sets `Capture.Hooks == true`.
- [ ] No production code contains a hard-coded known-event allowlist.
- [ ] `.d/*.sample` and `.d/.hidden` are ignored.
- [ ] Detection never recurses into `.d` children.
- [ ] Runtime-relevant entry symlinks are never followed, captured, or overwritten.
- [ ] Snapshot files are written `0644`.
- [ ] Profile metadata stores desired runtime mode.
- [ ] Existing Config/Shell FileWrite operations leave `Mode` and `ExpectedMode` nil.
- [ ] `ExpectedMode` is checked through the shared destination validator at both validation boundaries.
- [ ] Missing Hook creates use `ExpectedMissing`.
- [ ] Existing Hook repairs use `ExpectedHash` and `ExpectedMode`.
- [ ] Differing target contents only produce skips.
- [ ] Extra target Hooks only produce additive drift/skips.
- [ ] Every Hook mutation is `RiskHigh`.
- [ ] No Hooks plan operation contains `Command`.
- [ ] No capture/check/verify code invokes `bash`, `sh`, or `omarchy hook`.
- [ ] Human restore output includes exactly one executable-code warning.
- [ ] Vertical slice proves desired mode survives an inert `0644` profile snapshot.
- [ ] Full CI commands pass.

## Suggested PR structure

Prefer the task commits above rather than one giant commit. A clean history should read approximately:

```text
feat: add hooks profile schema
feat: support explicit file restore modes
feat: define portable hook metadata
feat: detect Omarchy hooks
feat: capture and validate hook snapshots
feat: diff and verify hook state
feat: plan safe hook restoration
feat: integrate hooks provider
feat: add hooks restore warnings
test: cover hooks restore workflow
docs: document portable Omarchy hooks
```

The PR description should call out the security model explicitly:

```text
- Hook source is arbitrary code and every mutation is high-risk.
- Blueprint never executes restored hooks.
- Existing differing hook content is never overwritten.
- Desired modes are metadata-backed so Git checkout behavior cannot change restoration.
```
