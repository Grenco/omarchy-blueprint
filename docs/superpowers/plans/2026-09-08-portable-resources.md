# Portable Resources v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add schema-7 Portable Resources so users can explicitly track files, directories, and clean Git repositories, automatically capture symlinks that point into tracked resources, and safely reconstruct those resources and links on another Omarchy machine.

**Architecture:** Add a new `resources` provider with profile-directed detection and explicit `track`/`untrack` registration. Ordinary files/directories are copied into profile snapshots; clean Git repositories are stored as remote + captured revision; symlinks are modeled as semantic edges from a source location to a resource ID plus target-relative path. A bounded link scanner auto-adopts inbound links into tracked resources, while a small ownership index prevents Resources from taking over paths another provider owns.

**Tech Stack:** Go 1.25+, Cobra, existing `command.Runner`, `github.com/pelletier/go-toml/v2`, current profile/state-provider architecture, current restore journal/executor, native `git` subprocesses, Linux filesystem symlinks.

**Spec:** `docs/superpowers/specs/2026-09-08-portable-resources-design.md`

## Global Constraints

- Profile schema becomes exactly `7`; introduction thresholds remain config `2`, defaults `3`, shell `4`, hooks `5`, Mise packages `6`, Resources `7`.
- Provider/category ID is exactly `resources`; do not add a `directories` provider.
- `track <path>` accepts only real regular files or real directories inside `$HOME`; a symlink itself is not a trackable root in v1.
- Tracked resource roots may not overlap each other, the active profile directory, or non-delegated provider-owned paths.
- Ordinary files/directories use `strategy = "copy"`.
- A directory uses `strategy = "git"` only when it is the clean worktree root with a portable `origin` remote and valid HEAD revision.
- Dirty Git repositories are rejected; do not silently fall back to copied bytes.
- Git restore pins the captured revision; branch-follow policy is outside v1.
- Copy capture never follows symlinks and never stores symlinks in the snapshot tree.
- Symlinks in copied resources are semantic `ResourceLink` records.
- Symlinks inside Git resources remain Git-owned and are not duplicated as restore operations.
- Inbound symlinks into already tracked resources are automatically managed unless ignored or blocked by an ownership claim.
- CLI v1 never automatically tracks a new resource merely because an untracked symlink points to it.
- Default inbound scan scope is direct children of `$HOME`, recursive `$HOME/.config`, and recursive `$HOME/.local/bin`.
- The scanner never follows symlinked directories.
- Restore creates relative symlink text computed from target-machine paths.
- Resource/link restore is additive only; no unknown target file, directory, Git worktree, or symlink is replaced.
- `--force` does not change Resources conflicts in schema 7.
- All new filesystem mutations reject symlinked parent directories at execution time.
- Copy snapshots are hash-validated before mutation.
- Known-sensitive copied paths/private-key content block capture; there is no force-secrets escape hatch in v1.
- Tests must use temp homes/profile dirs and may not inspect the real user's `$HOME`, `.config`, Git config, or dotfiles.
- Final verification remains `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint`.

---

## File Structure

Create focused files instead of growing one very large provider file.

- Modify `internal/profile/profile.go`
  - schema 7;
  - Resources profile types;
  - `resources/resources.toml` persistence.
- Modify `internal/profile/profile_test.go`
  - migration/round-trip/deterministic ordering.

- Create `internal/ownership/ownership.go`
  - provider path claims;
  - root/link conflict queries.
- Create `internal/ownership/ownership_test.go`

- Create `internal/providers/resources/provider.go`
  - public provider type and top-level orchestration.
- Create `internal/providers/resources/path.go`
  - logical HOME paths, IDs, containment, overlap.
- Create `internal/providers/resources/snapshot.go`
  - copy scanning, hashing, staging, sensitive checks.
- Create `internal/providers/resources/git.go`
  - Git provenance detection/equality.
- Create `internal/providers/resources/links.go`
  - symlink scanning/classification/relative-target generation.
- Create `internal/providers/resources/diff.go`
  - semantic Diff/Verify summaries.
- Create `internal/providers/resources/plan.go`
  - conservative restore planning.
- Create focused corresponding `_test.go` files.

- Modify `internal/model/model.go`
  - safe directory/symlink actions;
  - optional Copy source hash/parent guard.
- Modify `internal/restore/executor.go`
  - execute new actions;
  - validate Copy snapshot hash;
  - reuse parent-symlink safety.
- Modify `internal/restore/executor_test.go`

- Modify `internal/app/app.go`
  - Resources dependencies;
  - `track`, `untrack`, `tracked` commands.
- Modify `internal/app/providers.go`
  - Resources state-provider adapter and registry order.
- Modify `internal/app/app_test.go`
  - CLI and vertical integration.

- Modify `internal/providers/hooks/provider_test.go` or app integration tests as needed
  - prove Hooks symlink delegation remains warning/no-drift behavior.

- Modify `README.md`
- Modify `ROADMAP.md`
- Add `docs/adr/0012-portable-resources.md`
- Add the design/plan docs supplied with this milestone.

---

### Task 1: Schema 7 Resources Profile State

**Files:**
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**
- Produces:
  ```go
  type Resources struct
  type Resource struct
  type ResourceLink struct
  ```
- Produces: `profile.Data.Resources`
- Produces: `profile.Manifest.Capture.Resources`
- Persists: `resources/resources.toml`

- [ ] **Step 1: Write a schema-6 migration test**

Add:

```go
func TestSchema6LoadsAsSchema7WithResourcesEmpty(t *testing.T) {
    dir := t.TempDir()
    if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte(
        "schema = 6\n\n"+
            "[profile]\n"+
            "name = 'schema6'\n"+
            "created_at = 2026-09-08T00:00:00Z\n"+
            "updated_at = 2026-09-08T00:00:00Z\n",
    ), 0o644); err != nil {
        t.Fatal(err)
    }

    got, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if got.Manifest.Schema != 7 {
        t.Fatalf("schema=%d want=7", got.Manifest.Schema)
    }
    if got.Manifest.Capture.Resources {
        t.Fatal("schema-6 profile unexpectedly captured resources")
    }
    if len(got.Resources.Items) != 0 || len(got.Resources.Links) != 0 {
        t.Fatalf("resources=%#v want empty", got.Resources)
    }
}
```

- [ ] **Step 2: Write a deterministic Resources round-trip test**

Use both copy and Git metadata:

```go
func TestResourcesRoundTripSchema7(t *testing.T) {
    dir := t.TempDir()
    d := New("main", time.Unix(0, 0))
    d.Manifest.Capture.Resources = true
    d.Resources = Resources{
        Items: []Resource{
            {
                ID: "deploy", Path: "~/.local/bin/deploy",
                Kind: "file", Strategy: "copy",
                Hash: strings.Repeat("a", 64), Mode: "0755",
            },
            {
                ID: "dotfiles", Path: "~/dotfiles",
                Kind: "directory", Strategy: "git",
                Remote: "git@github.com:example/dotfiles.git",
                Branch: "main", Revision: strings.Repeat("b", 40),
            },
        },
        Links: []ResourceLink{
            {
                Source: "~/.config/nvim",
                TargetResource: "dotfiles",
                Target: "nvim",
                Origin: "inbound",
            },
        },
        IgnoredLinks: []string{"~/.config/ignored"},
    }

    if err := Save(dir, d); err != nil {
        t.Fatal(err)
    }
    first, err := os.ReadFile(filepath.Join(dir, "resources", "resources.toml"))
    if err != nil {
        t.Fatal(err)
    }

    loaded, err := Load(dir)
    if err != nil {
        t.Fatal(err)
    }
    if !reflect.DeepEqual(loaded.Resources, d.Resources) {
        t.Fatalf("resources=%#v want=%#v", loaded.Resources, d.Resources)
    }

    if err := Save(dir, loaded); err != nil {
        t.Fatal(err)
    }
    second, err := os.ReadFile(filepath.Join(dir, "resources", "resources.toml"))
    if err != nil {
        t.Fatal(err)
    }
    if !bytes.Equal(first, second) {
        t.Fatalf("resources.toml not deterministic:\n%s\n---\n%s", first, second)
    }
}
```

- [ ] **Step 3: Run focused tests and verify failure**

Run:

```bash
go test ./internal/profile -run 'TestSchema6LoadsAsSchema7WithResourcesEmpty|TestResourcesRoundTripSchema7' -count=1
```

Expected: FAIL because schema/types do not exist.

- [ ] **Step 4: Add schema and profile types**

In `internal/profile/profile.go`:

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

type Resources struct {
    Items        []Resource     `json:"resources" toml:"resource"`
    Links        []ResourceLink `json:"links" toml:"link"`
    IgnoredLinks []string       `json:"ignored_links,omitempty" toml:"ignored_links,omitempty"`
}

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
}

type ResourceLink struct {
    SourceResource string `json:"source_resource,omitempty" toml:"source_resource,omitempty"`
    Source         string `json:"source" toml:"source"`

    TargetResource string `json:"target_resource" toml:"target_resource"`
    Target         string `json:"target" toml:"target"`
    Origin         string `json:"origin" toml:"origin"`
}
```

Add `Resources bool` to `CaptureMeta` and `Resources Resources` to `Data`.

- [ ] **Step 5: Add load/save persistence**

Load only when the source schema introduced Resources:

```go
if loadedSchema >= resourcesSchema {
    b, err := os.ReadFile(filepath.Join(dir, "resources", "resources.toml"))
    if errors.Is(err, os.ErrNotExist) && d.Manifest.Capture.Resources {
        return d, errors.New(
            "resources state marked captured but resources/resources.toml is missing",
        )
    }
    if err == nil {
        if err := toml.Unmarshal(b, &d.Resources); err != nil {
            return d, fmt.Errorf("parse resources/resources.toml: %w", err)
        }
    } else if !errors.Is(err, os.ErrNotExist) {
        return d, err
    }
}
```

In `Save`, create `resources/`, sort Resources deterministically, marshal it, and include `resources/resources.toml` in the atomic writes.

Sorting rules:

```go
sort.Slice(d.Resources.Items, func(i, j int) bool {
    return d.Resources.Items[i].ID < d.Resources.Items[j].ID
})
sort.Slice(d.Resources.Links, func(i, j int) bool {
    if d.Resources.Links[i].SourceResource == d.Resources.Links[j].SourceResource {
        return d.Resources.Links[i].Source < d.Resources.Links[j].Source
    }
    return d.Resources.Links[i].SourceResource < d.Resources.Links[j].SourceResource
})
d.Resources.IgnoredLinks = normalize(d.Resources.IgnoredLinks)
```

- [ ] **Step 6: Update schema-threshold assertions**

Require:

```go
if configSchema != 2 ||
    defaultsSchema != 3 ||
    shellSchema != 4 ||
    hooksSchema != 5 ||
    misePackagesSchema != 6 ||
    resourcesSchema != 7 {
    t.Fatalf("unexpected introduction versions")
}
```

Update current-schema expectations from 6 to 7 without changing historical fixture schemas.

- [ ] **Step 7: Run profile tests**

```bash
go test ./internal/profile -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go
git commit -m "feat: add schema 7 resource state"
```

---

### Task 2: Logical Resource Paths and Ownership Claims

**Files:**
- Create: `internal/providers/resources/path.go`
- Create: `internal/providers/resources/path_test.go`
- Create: `internal/ownership/ownership.go`
- Create: `internal/ownership/ownership_test.go`

**Interfaces:**
- Produces:
  ```go
  func ExpandHomePath(home, logical string) (string, error)
  func LogicalHomePath(home, absolute string) (string, error)
  func ValidateResourceID(id string) error
  func ResourceIDForPath(path string) string
  func PathsOverlap(a, b string) bool
  func SafeRelativeResourcePath(path string) bool
  ```
- Produces:
  ```go
  type ownership.Claim struct
  type ownership.Index struct
  func (Index) TrackConflict(path string) []Claim
  func (Index) LinkConflict(path string) []Claim
  ```

- [ ] **Step 1: Write HOME path round-trip tests**

```go
func TestHomePathRoundTrip(t *testing.T) {
    home := filepath.Join(t.TempDir(), "home")
    absolute := filepath.Join(home, ".config", "nvim")

    logical, err := LogicalHomePath(home, absolute)
    if err != nil {
        t.Fatal(err)
    }
    if logical != "~/.config/nvim" {
        t.Fatalf("logical=%q", logical)
    }

    expanded, err := ExpandHomePath(home, logical)
    if err != nil {
        t.Fatal(err)
    }
    if expanded != absolute {
        t.Fatalf("expanded=%q want=%q", expanded, absolute)
    }
}
```

Reject:

```text
/etc/example
~/../outside
relative/no-tilde
~
```

`~` itself is rejected as too broad for v1.

- [ ] **Step 2: Write overlap and target-relative safety tests**

```go
func TestPathsOverlap(t *testing.T) {
    root := filepath.Join(t.TempDir(), "home")
    a := filepath.Join(root, "dotfiles")
    b := filepath.Join(a, "nvim")
    c := filepath.Join(root, "Scripts")

    if !PathsOverlap(a, b) {
        t.Fatal("ancestor/descendant must overlap")
    }
    if PathsOverlap(a, c) {
        t.Fatal("siblings must not overlap")
    }
}

func TestSafeRelativeResourcePath(t *testing.T) {
    for _, good := range []string{".", "nvim", "config/hypr.lua"} {
        if !SafeRelativeResourcePath(good) {
            t.Fatalf("%q should be safe", good)
        }
    }
    for _, bad := range []string{"", "../outside", "/abs", "a/../../outside"} {
        if SafeRelativeResourcePath(bad) {
            t.Fatalf("%q should be unsafe", bad)
        }
    }
}
```

- [ ] **Step 3: Write ownership tests**

```go
func TestOwnershipDelegatesSymlinkLeavesOnly(t *testing.T) {
    root := t.TempDir()
    hooks := filepath.Join(root, ".config", "omarchy", "hooks")
    index := Index{Claims: []Claim{
        {
            Provider: "hooks",
            Path: hooks,
            Recursive: true,
            DelegateSymlinks: true,
        },
    }}

    if got := index.TrackConflict(hooks); len(got) != 1 {
        t.Fatalf("track conflict=%#v", got)
    }
    if got := index.LinkConflict(filepath.Join(hooks, "theme-set")); len(got) != 0 {
        t.Fatalf("delegated link conflict=%#v", got)
    }
}
```

Non-delegated exact Config claim must block the link leaf:

```go
func TestOwnershipBlocksNonDelegatedConfigLink(t *testing.T) {
    path := filepath.Join(t.TempDir(), "bindings.lua")
    index := Index{Claims: []Claim{
        {Provider: "config", Path: path},
    }}
    if len(index.LinkConflict(path)) != 1 {
        t.Fatal("config-owned link source should conflict")
    }
}
```

- [ ] **Step 4: Run tests and verify failure**

```bash
go test ./internal/providers/resources ./internal/ownership -count=1
```

Expected: FAIL because packages/functions do not exist.

- [ ] **Step 5: Implement path normalization**

Rules:

```go
func ExpandHomePath(home, logical string) (string, error) {
    if logical == "~" || !strings.HasPrefix(logical, "~/") {
        return "", fmt.Errorf("resource path must use ~/...")
    }
    relative := filepath.FromSlash(strings.TrimPrefix(logical, "~/"))
    absolute := filepath.Clean(filepath.Join(home, relative))
    if !within(home, absolute) || absolute == filepath.Clean(home) {
        return "", fmt.Errorf("resource path escapes home: %s", logical)
    }
    return absolute, nil
}
```

`LogicalHomePath` must require actual containment beneath HOME and return slash-normalized `~/...`.

- [ ] **Step 6: Implement resource ID validation**

```go
var validResourceID = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func ValidateResourceID(id string) error {
    if id == "" || id == "." || id == ".." || !validResourceID.MatchString(id) {
        return fmt.Errorf("invalid resource id %q", id)
    }
    return nil
}
```

`ResourceIDForPath` derives from `filepath.Base`.

- [ ] **Step 7: Implement ownership index**

`TrackConflict` returns claims when:

```text
candidate == claim.Path
candidate contains claim.Path
claim recursively contains candidate
```

A broad tracked root that contains an exact owned file is therefore rejected.

`LinkConflict` returns an intersecting claim unless `DelegateSymlinks` is true.

Sort returned claims by provider then path for deterministic errors.

- [ ] **Step 8: Run focused tests**

```bash
go test ./internal/providers/resources ./internal/ownership -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/resources/path.go \
  internal/providers/resources/path_test.go \
  internal/ownership
git commit -m "feat: define resource path ownership"
```

---

### Task 3: Copy Snapshot Capture, Hashing, and Sensitive Guards

**Files:**
- Create: `internal/providers/resources/snapshot.go`
- Create: `internal/providers/resources/snapshot_test.go`

**Interfaces:**
- Consumes: path helpers from Task 2.
- Produces:
  ```go
  type SnapshotScan struct {
      Hash      string
      Mode      os.FileMode
      FileCount int
      Links     []RawLink
  }

  type RawLink struct {
      SourceAbsolute string
      RawTarget      string
  }

  func ScanCopyResource(root string) (SnapshotScan, error)
  func StageCopyResource(source, destination string) (SnapshotScan, error)
  func HashSnapshotTree(root string) (string, error)
  func ValidateSnapshotTree(root, expectedHash string) error
  ```

- [ ] **Step 1: Write regular-file snapshot test**

```go
func TestStageCopyFilePreservesModeAndHash(t *testing.T) {
    source := filepath.Join(t.TempDir(), "deploy")
    if err := os.WriteFile(source, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
        t.Fatal(err)
    }

    destination := filepath.Join(t.TempDir(), "deploy", "content")
    scan, err := StageCopyResource(source, destination)
    if err != nil {
        t.Fatal(err)
    }
    if scan.Mode.Perm() != 0o755 || scan.Hash == "" {
        t.Fatalf("scan=%#v", scan)
    }
    got, err := os.ReadFile(destination)
    if err != nil || string(got) != "#!/bin/sh\necho ok\n" {
        t.Fatalf("content=%q err=%v", got, err)
    }
}
```

- [ ] **Step 2: Write directory-symlink extraction test**

Create:

```text
source/
  regular.txt
  internal -> regular.txt
```

Assert:

```go
scan, err := StageCopyResource(source, destination)
if err != nil {
    t.Fatal(err)
}
if len(scan.Links) != 1 {
    t.Fatalf("links=%#v", scan.Links)
}
if _, err := os.Lstat(filepath.Join(destination, "internal")); !os.IsNotExist(err) {
    t.Fatal("snapshot must omit symlink")
}
```

- [ ] **Step 3: Write special-file rejection test**

On Linux create FIFO:

```go
if err := syscall.Mkfifo(filepath.Join(source, "pipe"), 0o600); err != nil {
    t.Fatal(err)
}
```

Assert capture error contains `unsupported file`.

- [ ] **Step 4: Write sensitive path/content tests**

Path examples:

```text
.env
.env.production
id_ed25519 under .ssh
credentials
private_key.pem
```

Private-key content:

```text
-----BEGIN OPENSSH PRIVATE KEY-----
```

Assert capture fails with the exact relative path.

Also prove an ordinary file called `credential-helper.go` is not rejected solely by an over-broad substring rule. Use high-confidence names, not `strings.Contains("credential")` everywhere.

- [ ] **Step 5: Run focused tests and verify failure**

```bash
go test ./internal/providers/resources -run 'TestStageCopy|TestSensitive|TestCopy.*Special' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Implement non-following scanner**

Use `os.Lstat` for the root and `filepath.WalkDir` for directories.

When `entry.Type()&os.ModeSymlink != 0`:

```go
raw, err := os.Readlink(path)
if err != nil {
    return err
}
scan.Links = append(scan.Links, RawLink{
    SourceAbsolute: path,
    RawTarget: raw,
})
return nil
```

Do not call `entry.Info()` before testing symlink type.

Regular files contribute to the canonical hash and are copied.

Directories contribute relative path + mode and are created.

- [ ] **Step 7: Implement canonical hash**

Use SHA-256 with stable path/type/mode delimiters:

```go
fmt.Fprintf(h, "dir\x00%s\x00%04o\x00", slashRel, mode)
fmt.Fprintf(h, "file\x00%s\x00%04o\x00", slashRel, mode)
io.Copy(h, file)
h.Write([]byte{0})
```

For a single file root include a stable root marker so file and one-entry directory cannot collide.

- [ ] **Step 8: Implement high-confidence sensitive checks**

Use exact/base patterns such as:

```go
func sensitiveResourcePath(relative string) bool {
    base := strings.ToLower(filepath.Base(relative))
    switch base {
    case ".env", "credentials", "credentials.json",
        "id_rsa", "id_ed25519", "id_ecdsa", "id_dsa":
        return true
    }
    if strings.HasPrefix(base, ".env.") {
        return true
    }
    if strings.HasSuffix(base, ".key") &&
        strings.Contains(strings.ToLower(relative), ".ssh") {
        return true
    }
    return false
}
```

For small regular text files, scan an initial bounded chunk for private-key PEM markers. Do not load multi-gigabyte files into memory.

- [ ] **Step 9: Implement snapshot validation**

`ValidateSnapshotTree` rejects every symlink/special entry and compares the canonical hash to `expectedHash`.

- [ ] **Step 10: Run package tests**

```bash
go test ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/providers/resources/snapshot.go \
  internal/providers/resources/snapshot_test.go
git commit -m "feat: capture portable resource snapshots"
```

---

### Task 4: Clean Git Resource Provenance

**Files:**
- Create: `internal/providers/resources/git.go`
- Create: `internal/providers/resources/git_test.go`

**Interfaces:**
- Produces:
  ```go
  type GitState struct {
      Remote   string
      Branch   string
      Revision string
      Dirty    bool
  }

  func DetectGitResource(
      ctx context.Context,
      runner command.Runner,
      root string,
  ) (GitState, bool, error)

  func PortableGitRemote(raw string) (string, error)
  func EqualGitResource(saved profile.Resource, current GitState) bool
  ```

- [ ] **Step 1: Write a map-runner fixture**

```go
type gitRunner struct {
    output map[string]string
    err    map[string]error
}

func (r gitRunner) Run(_ context.Context, name string, args ...string) (string, error) {
    key := name + " " + strings.Join(args, " ")
    return r.output[key], r.err[key]
}
```

- [ ] **Step 2: Write clean Git detection test**

Expected command set:

```text
git -C <root> rev-parse --show-toplevel
git -C <root> status --porcelain --untracked-files=all
git -C <root> remote get-url origin
git -C <root> rev-parse HEAD
git -C <root> symbolic-ref --quiet --short HEAD
```

Assert:

```go
state, isGit, err := DetectGitResource(ctx, runner, root)
if err != nil || !isGit {
    t.Fatalf("state=%#v isGit=%t err=%v", state, isGit, err)
}
if state.Remote != "git@github.com:example/dotfiles.git" ||
    state.Branch != "main" ||
    state.Revision != strings.Repeat("a", 40) ||
    state.Dirty {
    t.Fatalf("state=%#v", state)
}
```

- [ ] **Step 3: Write dirty Git refusal test**

A non-empty porcelain output must return `Dirty=true` and later Track/Capture must reject it.

- [ ] **Step 4: Write portable remote validation tests**

Allow:

```text
https://github.com/example/dotfiles.git
ssh://git@github.com/example/dotfiles.git
git@github.com:example/dotfiles.git
```

Sanitize:

```text
https://token@example.com/example/dotfiles.git
→ https://example.com/example/dotfiles.git
```

Reject:

```text
/path/to/local/repo
../relative/repo
file:///home/user/repo
```

- [ ] **Step 5: Run tests and verify failure**

```bash
go test ./internal/providers/resources -run 'Test.*Git' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Implement detection**

A path counts as Git strategy only if `rev-parse --show-toplevel` resolves exactly to the selected root after cleaning.

A nested directory inside a worktree remains an ordinary copy resource candidate; do not silently promote its repository root.

- [ ] **Step 7: Implement revision validation**

Accept the repository's full hex object ID:

```go
func validGitRevision(value string) bool {
    if len(value) != 40 && len(value) != 64 {
        return false
    }
    for _, r := range value {
        if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
            return false
        }
    }
    return true
}
```

- [ ] **Step 8: Run resource tests**

```bash
go test ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/resources/git.go \
  internal/providers/resources/git_test.go
git commit -m "feat: detect clean git resources"
```

---

### Task 5: Symlink Discovery and Classification

**Files:**
- Create: `internal/providers/resources/links.go`
- Create: `internal/providers/resources/links_test.go`

**Interfaces:**
- Consumes: ownership index, path helpers.
- Produces:
  ```go
  type LinkClassification string

  const (
      LinkManagedInbound      LinkClassification = "managed-inbound"
      LinkManagedResource     LinkClassification = "managed-resource"
      LinkGitOwned            LinkClassification = "git-owned"
      LinkUntrackedHomeTarget LinkClassification = "untracked-home-target"
      LinkExternalTarget      LinkClassification = "external-target"
      LinkBroken              LinkClassification = "broken"
      LinkOwnershipConflict   LinkClassification = "ownership-conflict"
  )

  type LinkSearchRoot struct {
      Path      string
      Recursive bool
  }

  type LinkCandidate struct {
      Source           string
      RawTarget        string
      Resolved         string
      Classification   LinkClassification
      SourceResource   string
      TargetResource   string
      TargetRelative   string
  }

  func DefaultLinkSearchRoots(home string) []LinkSearchRoot
  func DiscoverLinks(
      home string,
      roots []LinkSearchRoot,
      resources []profile.Resource,
      ownership ownership.Index,
      ignored []string,
  ) ([]LinkCandidate, error)

  func ClassifyResourceLinks(
      home string,
      resource profile.Resource,
      raw []RawLink,
      all []profile.Resource,
  ) ([]LinkCandidate, error)

  func RelativeSymlinkTarget(sourcePath, targetPath string) (string, error)
  ```

- [ ] **Step 1: Write default-root test**

```go
func TestDefaultLinkSearchRoots(t *testing.T) {
    home := filepath.Join(t.TempDir(), "home")
    got := DefaultLinkSearchRoots(home)
    want := []LinkSearchRoot{
        {Path: home, Recursive: false},
        {Path: filepath.Join(home, ".config"), Recursive: true},
        {Path: filepath.Join(home, ".local", "bin"), Recursive: true},
    }
    if !reflect.DeepEqual(got, want) {
        t.Fatalf("got=%#v want=%#v", got, want)
    }
}
```

- [ ] **Step 2: Write inbound-link discovery test**

Create:

```text
home/dotfiles/hypr/overrides.lua
home/.config/hypr/overrides.lua -> ../../dotfiles/hypr/overrides.lua
```

Tracked resource:

```go
profile.Resource{
    ID: "dotfiles",
    Path: "~/dotfiles",
    Kind: "directory",
    Strategy: "copy",
}
```

Assert one `LinkManagedInbound` candidate with:

```text
Source           ~/.config/hypr/overrides.lua
TargetResource   dotfiles
TargetRelative   hypr/overrides.lua
```

- [ ] **Step 3: Write direct-HOME and `.local/bin` tests**

Prove:

```text
~/.zshrc -> ~/dotfiles/zshrc
~/.local/bin/tool -> ~/dotfiles/bin/tool
```

are discovered.

- [ ] **Step 4: Write no-symlink-directory-follow test**

Create:

```text
~/.config/external -> ~/huge-external-dir
~/huge-external-dir/nested -> ~/dotfiles/target
```

The nested link must not be returned because `.config/external` is itself a symlinked directory and scanner must not walk through it.

- [ ] **Step 5: Write untracked-target candidate test**

```text
~/.config/nvim -> ~/dotfiles/nvim
```

with no tracked `dotfiles`.

Expected classification:

```text
untracked-home-target
```

No resource is added.

- [ ] **Step 6: Write ownership-delegation tests**

Hooks claim:

```go
ownership.Claim{
    Provider: "hooks",
    Path: hooksRoot,
    Recursive: true,
    DelegateSymlinks: true,
}
```

must allow `theme-set`.

A Config exact non-delegated claim must produce `ownership-conflict`.

- [ ] **Step 7: Write internal copied-resource link test**

Two resources:

```text
scripts → ~/Scripts
dotfiles → ~/dotfiles
```

Raw link inside Scripts:

```text
~/Scripts/current -> ~/dotfiles/bin/current
```

Expected:

```text
managed-resource
SourceResource = scripts
Source = current
TargetResource = dotfiles
TargetRelative = bin/current
```

- [ ] **Step 8: Write external/broken copied-link failure classification**

Classify:

```text
~/Scripts/external -> ~/untracked/tool
~/Scripts/broken -> ~/missing
```

as untracked/broken so later copy capture can reject them.

- [ ] **Step 9: Write relative-target equivalence test**

```go
source := filepath.Join(home, ".config", "hypr", "overrides.lua")
target := filepath.Join(home, "dotfiles", "hypr", "overrides.lua")

got, err := RelativeSymlinkTarget(source, target)
if err != nil {
    t.Fatal(err)
}
if filepath.IsAbs(got) {
    t.Fatalf("target=%q must be relative", got)
}
```

- [ ] **Step 10: Run tests and verify failure**

```bash
go test ./internal/providers/resources -run 'Test.*Link|TestDefaultLinkSearchRoots' -count=1
```

Expected: FAIL.

- [ ] **Step 11: Implement scan without following symlink directories**

For recursive roots use `WalkDir`.

For a symlink entry:

```go
raw, err := os.Readlink(path)
...
candidate, err := classifyLink(...)
...
```

Return without descending.

For direct-HOME root, use `os.ReadDir(home)` and inspect only entries whose `Type()&os.ModeSymlink != 0`.

- [ ] **Step 12: Implement resolved containment**

Resolve one symlink target relative to its source parent.

Use `filepath.EvalSymlinks` on the target only for classification.

Then compare the resolved target with resolved tracked resource roots using a path-component-aware containment helper.

Do not classify by string prefix.

- [ ] **Step 13: Run resource tests**

```bash
go test ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add internal/providers/resources/links.go \
  internal/providers/resources/links_test.go
git commit -m "feat: discover resource symlink relationships"
```

---

### Task 6: Resource Registry, Transactional Capture, Track and Untrack

**Files:**
- Create: `internal/providers/resources/provider.go`
- Create: `internal/providers/resources/provider_test.go`

**Interfaces:**
- Produces:
  ```go
  type Provider struct {
      Runner      command.Runner
      HomeDir     string
      ProfileDir  string
      LinkRoots   []LinkSearchRoot
      Ownership   ownership.Index
  }

  func (p Provider) Track(
      ctx context.Context,
      saved profile.Resources,
      path string,
      requestedID string,
  ) (profile.Resources, []model.Change, error)

  func (p Provider) Capture(
      ctx context.Context,
      saved profile.Resources,
  ) (profile.Resources, []model.Change, error)

  func (p Provider) Untrack(
      saved profile.Resources,
      ref string,
  ) (profile.Resources, []string, error)

  func (p Provider) Detect(
      ctx context.Context,
      saved profile.Resources,
  ) (profile.Resources, []LinkCandidate, error)
  ```

- [ ] **Step 1: Write `Track` file test**

```go
func TestTrackRegularFileCapturesImmediately(t *testing.T) {
    home := t.TempDir()
    profileDir := t.TempDir()
    source := filepath.Join(home, ".local", "bin", "deploy")
    if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(source, []byte("echo deploy\n"), 0o755); err != nil {
        t.Fatal(err)
    }

    p := Provider{
        HomeDir: home, ProfileDir: profileDir,
        LinkRoots: DefaultLinkSearchRoots(home),
    }
    got, _, err := p.Track(context.Background(), profile.Resources{}, source, "")
    if err != nil {
        t.Fatal(err)
    }
    if len(got.Items) != 1 ||
        got.Items[0].ID != "deploy" ||
        got.Items[0].Kind != "file" ||
        got.Items[0].Strategy != "copy" {
        t.Fatalf("resources=%#v", got)
    }
    if _, err := os.Stat(filepath.Join(
        profileDir, "resources", "files", "deploy", "content",
    )); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Write track-root rejection tests**

Reject:

```text
source path is symlink
outside HOME
overlaps existing resource
contains active profile dir
conflicts with ownership claim
invalid/colliding resource ID
```

Assert profile snapshot root remains unchanged after failure.

- [ ] **Step 3: Write clean Git Track test**

Use a mock Runner and real temp worktree marker path only as needed.

Assert:

```text
Strategy = git
Remote/Revision populated
resources/files/<id> does not exist
```

Dirty Git detection must cause Track error.

- [ ] **Step 4: Write transactional capture failure test**

Start with saved copy resource and valid old snapshot.

Modify live directory by adding a FIFO or untracked external symlink.

Call `Capture`.

Assert:

```text
Capture returns error
old resources/files/<id> remains unchanged
old metadata returned/saved by caller is untouched
temporary .capture-* directory removed
```

- [ ] **Step 5: Write inbound-link auto-adoption Track test**

Source:

```text
~/dotfiles/value
~/.config/example -> ~/dotfiles/value
```

After `Track("~/dotfiles")`, assert saved `Links` contains the inbound link automatically.

- [ ] **Step 6: Write ignored-link behavior test**

Saved:

```go
IgnoredLinks: []string{"~/.config/example"}
```

Tracking/capturing target resource must not add the link.

- [ ] **Step 7: Write Untrack test**

```go
updated, changed, err := p.Untrack(saved, "resource:dotfiles")
```

Assert:

- dotfiles item removed;
- links targeting dotfiles removed;
- links sourced from dotfiles removed;
- snapshot directory removed;
- live `~/dotfiles` and live inbound symlinks remain untouched.

- [ ] **Step 8: Write link opt-out/re-enable tests**

`Untrack(saved, "link:~/.config/example")`:

- removes saved inbound link;
- appends normalized path to `IgnoredLinks`;
- does not delete filesystem link.

`Track` for a `link:` reference is better handled by the app/registry helper, so add:

```go
func (p Provider) EnableLink(
    saved profile.Resources,
    source string,
) (profile.Resources, error)
```

It removes the ignore only after proving the live source resolves into an existing tracked resource.

- [ ] **Step 9: Run focused tests and verify failure**

```bash
go test ./internal/providers/resources -run 'TestTrack|TestCapture.*Transaction|TestUntrack|Test.*Ignored' -count=1
```

Expected: FAIL.

- [ ] **Step 10: Implement staged capture root**

Use:

```text
<profile>/resources/.capture-<random>/
```

Build all copy snapshots under the staging root.

Only after every item/link validates:

1. rename current `resources/files` to a temporary previous location;
2. rename staged `files` into place;
3. return new metadata to the app for profile.Save;
4. remove previous snapshot only after the new tree is installed.

If metadata save later fails, app-level rollback should restore previous files; expose a small capture transaction object if necessary:

```go
type CaptureTransaction struct {
    State profile.Resources
    Commit func() error
    Rollback func() error
}
```

Prefer this explicit transaction if simple "capture mutates snapshots before Save" cannot preserve atomicity across metadata + files.

- [ ] **Step 11: Implement Track strategy selection**

Pseudo-code:

```go
info, err := os.Lstat(abs)
if info.Mode()&os.ModeSymlink != 0 {
    return ..., fmt.Errorf("%s is a symlink; track its owning resource instead", logical)
}

if info.IsDir() {
    git, isGit, err := DetectGitResource(...)
    if err != nil {
        return ..., err
    }
    if isGit {
        if git.Dirty {
            return ..., fmt.Errorf("resource %s has uncommitted or untracked Git state", id)
        }
        // store Git metadata, no snapshot
    } else {
        // copy strategy
    }
} else if info.Mode().IsRegular() {
    // copy file
} else {
    return ..., fmt.Errorf("unsupported resource type")
}
```

- [ ] **Step 12: Implement internal-link validation**

For every symlink omitted from a copy snapshot:

- classify against the complete candidate resource set;
- save `managed-resource` links;
- fail capture on broken/untracked/external links.

Git-owned internal links are not processed as snapshot links.

- [ ] **Step 13: Run provider tests**

```bash
go test ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add internal/providers/resources/provider.go \
  internal/providers/resources/provider_test.go
git commit -m "feat: track and capture portable resources"
```

---

### Task 7: Resources Diff, Verify, and Check

**Files:**
- Create: `internal/providers/resources/diff.go`
- Create: `internal/providers/resources/diff_test.go`
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/providers/resources/provider_test.go`

**Interfaces:**
- Produces:
  ```go
  func Diff(saved, current profile.Resources) []model.Change
  func Verify(saved, current profile.Resources) model.VerificationResult
  func (p Provider) Check(ctx context.Context, saved profile.Resources) error
  ```

- [ ] **Step 1: Write copy-resource drift test**

```go
func TestDiffCopyResourceHashChange(t *testing.T) {
    saved := profile.Resources{Items: []profile.Resource{
        {
            ID: "scripts", Path: "~/Scripts",
            Kind: "directory", Strategy: "copy",
            Hash: "old", Mode: "0755",
        },
    }}
    current := profile.Resources{Items: []profile.Resource{
        {
            ID: "scripts", Path: "~/Scripts",
            Kind: "directory", Strategy: "copy",
            Hash: "new", Mode: "0755",
        },
    }}

    changes := Diff(saved, current)
    if len(changes) != 1 ||
        changes[0].Kind != "resource" ||
        changes[0].Name != "scripts" {
        t.Fatalf("changes=%#v", changes)
    }
}
```

- [ ] **Step 2: Write Git drift tests**

Cover:

```text
same remote + revision + clean → clean
different revision            → modify
different remote              → modify
dirty current                 → modify
missing                       → remove/missing
```

`Detect` should encode dirty state for current detection without putting a permanent `Dirty` field into saved profile metadata if not desired. If necessary use an internal detected-state struct rather than overloading `profile.Resource`.

- [ ] **Step 3: Write link semantic-equivalence test**

Saved semantic link should compare clean when live source is absolute or relative but resolves to same resource target.

The detected current `ResourceLink` should be normalized, so Diff compares source key + target resource/relative path only.

- [ ] **Step 4: Write new inbound-link drift test**

Saved has no link; current discovery finds:

```text
~/.config/wezterm → resource:dotfiles/wezterm
```

Assert one `ChangeAdd`.

- [ ] **Step 5: Write Verify target-only-link test**

Extra current inbound links do not fail verification:

```go
if got := Verify(saved, currentWithExtraLink); !got.OK {
    t.Fatalf("verification=%#v", got)
}
```

Missing/changed saved links do fail.

- [ ] **Step 6: Write Check corruption tests**

Check must fail for:

```text
duplicate resource IDs
overlapping roots
missing copy snapshot
corrupt snapshot hash
symlink inside snapshot
unsafe target "../outside"
unknown target_resource
duplicate link source
invalid Git revision
local-only Git remote
```

- [ ] **Step 7: Write conditional Git executable test**

When no saved Git resources exist, `Check` must not call:

```text
git --version
```

When at least one exists, failure of `git --version` must fail Check.

- [ ] **Step 8: Run tests and verify failure**

```bash
go test ./internal/providers/resources -run 'TestDiff|TestVerify|TestCheck' -count=1
```

Expected: FAIL.

- [ ] **Step 9: Implement stable keyed maps**

Helpers:

```go
func resourceMap(items []profile.Resource) map[string]profile.Resource
func linkKey(link profile.ResourceLink) string
func linkMap(links []profile.ResourceLink) map[string]profile.ResourceLink
```

Recommended link key:

```text
home:<logical-source>
resource:<source-resource>:<relative-source>
```

- [ ] **Step 10: Implement Diff**

Change shapes:

```go
model.Change{
    Type: model.ChangeModify,
    Provider: "resources",
    Kind: "resource",
    Name: id,
    Summary: "~ resource " + id + " differs",
}
```

Links use `Kind: "link"` and semantic summaries.

Sort by Kind then Name.

- [ ] **Step 11: Implement Verify**

Only saved resources/links are requirements.

Extra current resources are impossible because detection is saved-definition-directed; extra inbound links are ignored by Verify.

- [ ] **Step 12: Implement Check**

Validate metadata first, then profile snapshots, then required executable availability.

Do not touch the live target paths except where needed to validate the configured HOME/profile relationship.

- [ ] **Step 13: Run resource tests**

```bash
go test ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add internal/providers/resources/diff.go \
  internal/providers/resources/diff_test.go \
  internal/providers/resources/provider.go \
  internal/providers/resources/provider_test.go
git commit -m "feat: diff and validate portable resources"
```

---

### Task 8: Restore Executor Directory, Symlink, and Copy Integrity Actions

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/restore/executor.go`
- Modify: `internal/restore/executor_test.go`

**Interfaces:**
- Produces:
  ```go
  type DirectoryCreate struct
  type SymlinkWrite struct
  ```
- Extends:
  ```go
  type model.Operation
  type model.Copy
  ```

- [ ] **Step 1: Write operation-validation tests**

Ensure an operation with more than one action fails.

Add cases for:

```text
Directory only → valid
Symlink only   → valid
File + Symlink → invalid
Copy + Directory → invalid
```

- [ ] **Step 2: Write directory-create tests**

```go
func TestDirectoryCreateRejectsSymlinkParent(t *testing.T) {
    external := t.TempDir()
    root := t.TempDir()
    if err := os.Symlink(external, filepath.Join(root, "linked")); err != nil {
        t.Fatal(err)
    }

    action := model.DirectoryCreate{
        Path: filepath.Join(root, "linked", "Projects"),
        Mode: 0o755,
        RejectSymlinkParents: true,
    }
    err := executeDirectoryCreate(action)
    if err == nil || !strings.Contains(err.Error(), "parent is a symlink") {
        t.Fatalf("err=%v", err)
    }
}
```

Also prove existing real directory is idempotent.

- [ ] **Step 3: Write symlink-create tests**

Missing destination:

```go
action := model.SymlinkWrite{
    Destination: filepath.Join(root, ".config", "nvim"),
    Target: "../../dotfiles/nvim",
    ExpectedMissing: true,
    RejectSymlinkParents: true,
}
```

Assert `os.Readlink` returns exact approved target.

Existing regular file and existing different symlink must fail without mutation.

- [ ] **Step 4: Write symlink parent TOCTOU guard test**

Plan destination under a real parent, then replace a parent component with a symlink before execution.

Execution must fail and external target must remain untouched.

- [ ] **Step 5: Write Copy source-hash test**

Extend `model.Copy`:

```go
type Copy struct {
    Source               string `json:"source"`
    Destination          string `json:"destination"`
    SourceHash           string `json:"source_hash,omitempty"`
    RejectSymlinkParents bool   `json:"reject_symlink_parents,omitempty"`
}
```

Test a valid snapshot hash succeeds.

Then mutate the snapshot after plan creation and assert execution fails before destination creation.

- [ ] **Step 6: Write Copy parent-symlink guard test**

Same pattern as FileWrite/Mise: a destination parent changed to symlink between plan and execution must prevent copy into external storage.

- [ ] **Step 7: Run focused tests and verify failure**

```bash
go test ./internal/restore -run 'Test.*Directory|Test.*Symlink|Test.*Copy.*Hash|Test.*Copy.*Parent' -count=1
```

Expected: FAIL.

- [ ] **Step 8: Add model fields**

```go
type Operation struct {
    ...
    Directory *DirectoryCreate `json:"directory,omitempty"`
    Symlink   *SymlinkWrite     `json:"symlink,omitempty"`
}

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

- [ ] **Step 9: Generalize action counting**

In `executeOperation`, count:

```text
Command
Copy
File
Directory
Symlink
```

and require exactly one.

- [ ] **Step 10: Reuse one parent-symlink validator**

The existing `validateSymlinkParents(destination)` from Mise FileWrite should be reused by:

```text
FileWrite
Copy
DirectoryCreate
SymlinkWrite
```

Do not create divergent implementations.

- [ ] **Step 11: Implement directory action**

Algorithm:

```text
validate mode <= 0777
validate ancestors
Lstat destination
  missing → MkdirAll + Chmod final dir
  real dir → success
  symlink/non-dir → error
validate ancestors again around mutation boundary
```

- [ ] **Step 12: Implement symlink action**

Algorithm:

```text
require non-empty Target
require ExpectedMissing
validate parents
Lstat Destination
  exists → error
  missing → continue
MkdirAll(parent)
validate parents
recheck Destination still missing
os.Symlink(Target, Destination)
```

- [ ] **Step 13: Implement Copy integrity**

If `SourceHash != ""`, hash/validate source tree immediately before mutation.

Use a generic restore-side hash routine with the same canonical format as Resources. To avoid importing a provider into restore, move canonical regular-tree hashing to a neutral package if necessary:

```text
internal/content/tree.go
```

Preferred interface:

```go
func content.HashRegularTree(root string) (string, error)
```

It rejects symlinks/special files.

Resources snapshot code should use the same helper after this refactor.

- [ ] **Step 14: Run restore tests**

```bash
go test ./internal/restore ./internal/content -count=1
```

Expected: PASS.

- [ ] **Step 15: Run all tests for regressions**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 16: Commit**

```bash
git add internal/model/model.go \
  internal/restore/executor.go \
  internal/restore/executor_test.go \
  internal/content
git commit -m "feat: add safe resource filesystem operations"
```

---

### Task 9: Conservative Resources Restore Planner

**Files:**
- Create: `internal/providers/resources/plan.go`
- Create: `internal/providers/resources/plan_test.go`

**Interfaces:**
- Consumes model actions from Task 8.
- Produces:
  ```go
  func (p Provider) Plan(
      ctx context.Context,
      saved profile.Resources,
      current profile.Resources,
      schema int,
      from, to string,
  ) (model.RestorePlan, error)
  ```

- [ ] **Step 1: Write missing copied-file plan test**

Expected operation uses snapshot source:

```go
op.File.Source = filepath.Join(
    p.ProfileDir, "resources", "files", "deploy", "content",
)
op.File.Destination = filepath.Join(p.HomeDir, ".local", "bin", "deploy")
op.File.ExpectedMissing = true
op.File.SourceHash = savedResource.Hash
op.File.Mode = ptr(0o755)
op.File.RejectSymlinkParents = true
```

Risk Low.

- [ ] **Step 2: Write differing copied-file conflict test**

If target exists with different hash:

```text
0 operations
1 skip
reason contains "existing resource differs; overwrite disabled"
```

- [ ] **Step 3: Write missing copied-directory plan test**

Expected:

```go
model.Operation{
    ID: "resources.copy.scripts",
    Provider: "resources",
    Action: "copy",
    Resource: "resource:scripts",
    Copy: &model.Copy{
        Source: <profile snapshot>,
        Destination: <home/Scripts>,
        SourceHash: saved.Hash,
        RejectSymlinkParents: true,
    },
    Risk: model.RiskLow,
}
```

- [ ] **Step 4: Write missing Git plan test**

Expected ordered operations:

```text
resources.mkdir.dotfiles
resources.git.clone.dotfiles
resources.git.checkout.dotfiles
```

Commands:

```go
[]string{
    "git", "clone", "--no-checkout",
    saved.Remote,
    destination,
}
```

and:

```go
[]string{
    "git", "-C", destination,
    "checkout", "--detach",
    saved.Revision,
}
```

Dependencies:

```text
clone depends mkdir
checkout depends clone
```

- [ ] **Step 5: Write existing differing Git conflict test**

Dirty/different revision/different remote produce no Git mutation and one skip.

- [ ] **Step 6: Write inbound-link create test**

Saved:

```text
dotfiles Git resource missing
~/.config/nvim -> resource:dotfiles/nvim
```

Expected symlink operation:

```go
op.Symlink.Destination == <home>/.config/nvim
filepath.IsAbs(op.Symlink.Target) == false
op.DependsOn == []string{"resources.git.checkout.dotfiles"}
```

- [ ] **Step 7: Write existing equivalent-link no-op test**

Create an absolute live link and save semantic target.

Planner must emit no symlink op because resolved target is equivalent.

Repeat with an already-relative link.

- [ ] **Step 8: Write existing conflicting-link skip test**

Different symlink, regular file, and directory at source all produce skips and no mutation.

- [ ] **Step 9: Write cross-resource dependency test**

Copied `scripts/current` points to Git `dotfiles/bin/current`.

If both missing:

```text
link depends on resources.copy.scripts
link depends on resources.git.checkout.dotfiles
```

Sort dependencies deterministically.

- [ ] **Step 10: Write unsatisfied-target suppression test**

If target `dotfiles` exists in conflict and cannot be restored, dependent inbound link must be skipped:

```text
reason contains "target resource dotfiles is not satisfied"
```

Do not create a link into conflicting target state even if the target path happens to exist.

- [ ] **Step 11: Run focused tests and verify failure**

```bash
go test ./internal/providers/resources -run 'TestPlan' -count=1
```

Expected: FAIL.

- [ ] **Step 12: Implement per-resource satisfaction state**

Use:

```go
type resourcePlanState struct {
    Satisfied bool
    ReadyOpID string
    Conflict  string
}
```

For every saved resource, plan it first and record its final state.

Then plan links in a second pass.

- [ ] **Step 13: Implement file mode parser**

Saved modes are strings such as `"0755"`.

Use:

```go
func parseResourceMode(value string) (uint32, error) {
    parsed, err := strconv.ParseUint(value, 8, 32)
    if err != nil || parsed > 0o777 {
        return 0, fmt.Errorf("invalid resource mode %q", value)
    }
    return uint32(parsed), nil
}
```

Invalid saved metadata is an error, not a skip.

- [ ] **Step 14: Implement Git parent operation only when needed**

If destination parent exists as a real directory, clone can have no mkdir dependency.

If parent is missing, emit one `DirectoryCreate`.

If parent path is symlink/non-dir, skip the resource before clone.

- [ ] **Step 15: Implement link semantic check**

Resolve live link source without following it as a destination object:

```text
Lstat source
Readlink source
resolve relative to source parent
EvalSymlinks target when it exists
compare to expected resource target
```

If expected target does not exist yet because resource is missing, semantic comparison is only needed for an existing live source; a missing source becomes create candidate after resource restore.

- [ ] **Step 16: Run resource tests**

```bash
go test ./internal/providers/resources -count=1
```

Expected: PASS.

- [ ] **Step 17: Commit**

```bash
git add internal/providers/resources/plan.go \
  internal/providers/resources/plan_test.go
git commit -m "feat: plan safe portable resource restore"
```

---

### Task 10: App Provider Integration and Ownership Claims

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Adds:
  ```go
  Dependencies.HomeDir
  Dependencies.ResourceLinkRoots
  ```
- Produces: `resourcesStateProvider`
- Provider registry order becomes:
  ```text
  packages, themes, plugins, resources, config, defaults, shell, hooks
  ```

- [ ] **Step 1: Write provider-registry order test**

Update:

```go
want := []string{
    "packages",
    "themes",
    "plugins",
    "resources",
    "config",
    "defaults",
    "shell",
    "hooks",
}
```

- [ ] **Step 2: Write isolated app-home fixture**

Add a helper used by all Resources tests:

```go
func resourceHome(t *testing.T) string {
    t.Helper()
    home := filepath.Join(t.TempDir(), "home")
    if err := os.MkdirAll(home, 0o755); err != nil {
        t.Fatal(err)
    }
    return home
}
```

Inject:

```go
deps.HomeDir = func() (string, error) { return home, nil }
deps.ResourceLinkRoots = func(home string) []resourcesprovider.LinkSearchRoot {
    return resourcesprovider.DefaultLinkSearchRoots(home)
}
```

Never rely on process HOME in Resources app tests.

- [ ] **Step 3: Build ownership claims from current app paths**

Add helper:

```go
func resourceOwnership(
    deps Dependencies,
    opt *options,
) (ownership.Index, error)
```

Claims must include:

- profile dir recursive, non-delegated;
- state/journal root recursive, non-delegated;
- Config default exact user paths, non-delegated;
- Shell user state exact, non-delegated;
- Hooks root recursive, `DelegateSymlinks: true`;
- plugin user root recursive, non-delegated;
- theme user root recursive, non-delegated.

Use actual injected dependency paths, not hardcoded usernames.

- [ ] **Step 4: Add Resources adapter**

```go
type resourcesStateProvider struct {
    deps Dependencies
    opt  *options
}
```

Methods:

```text
ID()              "resources"
CategoryEnabled() true
Captured(d)       d.Manifest.Capture.Resources
Capture           provider.Capture
Diff              provider.Detect + resources.Diff
Plan              provider.Detect + provider.Plan
Verify            provider.Detect + resources.Verify
Check             provider.Check
```

`Capture` updates `d.Resources` and sets `Capture.Resources=true`.

- [ ] **Step 5: Add Resources capture-required message**

```text
resources state has not been captured; track a resource or run capture resources first
```

If no resource has ever been tracked, explicit `status resources` should explain how to start.

- [ ] **Step 6: Add labels**

Provider state label:

```text
portable resources
```

Check label:

```text
portable resource state valid
```

- [ ] **Step 7: Run app tests**

```bash
go test ./internal/app -count=1
```

Expected: PASS after implementation.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go \
  internal/app/providers.go \
  internal/app/app_test.go
git commit -m "feat: integrate portable resources provider"
```

---

### Task 11: `track`, `untrack`, and `tracked` CLI

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces CLI:
  ```text
  omarchy-blueprint track <path|link:...> [--id ID]
  omarchy-blueprint untrack <resource-ref|link-ref>
  omarchy-blueprint tracked
  ```

- [ ] **Step 1: Write `track` command vertical test**

Fixture:

```text
HOME/dotfiles/value
HOME/.config/example -> HOME/dotfiles/value
```

Run:

```go
code := Execute(ctx, []string{
    "--profile", profileDir,
    "track", filepath.Join(home, "dotfiles"),
}, deps)
```

Assert:

- exit 0;
- profile has resource `dotfiles`;
- profile has inbound link `~/.config/example`;
- `Capture.Resources` true;
- output mentions both tracked resource and discovered link.

- [ ] **Step 2: Write symlink-root CLI refusal test**

`track ~/.config/example` when it is symlink must exit 1 and suggest tracking the owning target resource.

It must not add the target automatically.

- [ ] **Step 3: Write `--id` collision test**

Track two paths with same basename.

Second without `--id` fails.

Second with:

```text
--id second-tool
```

succeeds.

- [ ] **Step 4: Write `tracked` output test**

Text output should contain:

```text
dotfiles
~/dotfiles
git or copy
inbound link count
ignored links when present
```

JSON output must use the normal API envelope and Resources profile types.

- [ ] **Step 5: Write `untrack resource` non-destructive test**

After command:

```text
profile metadata removed
snapshot removed
live resource still exists
live inbound link still exists
```

- [ ] **Step 6: Write link ignore/re-enable CLI test**

Run:

```bash
omarchy-blueprint untrack 'link:~/.config/example'
```

Assert link is moved to `IgnoredLinks`.

Run capture; it stays ignored.

Then:

```bash
omarchy-blueprint track 'link:~/.config/example'
```

Assert ignore removed and link saved again.

- [ ] **Step 7: Run focused app tests and verify failure**

```bash
go test ./internal/app -run 'TestTrack|TestTracked|TestUntrack' -count=1
```

Expected: FAIL.

- [ ] **Step 8: Add Cobra commands**

`track`:

```go
var resourceID string
cmd := &cobra.Command{
    Use:   "track <path|link:...>",
    Short: "Track a portable user resource",
    Args:  cobra.ExactArgs(1),
    RunE: ...
}
cmd.Flags().StringVar(&resourceID, "id", "", "stable resource ID")
```

If arg starts `link:` route to re-enable link behavior.

Otherwise resolve/track resource.

- [ ] **Step 9: Save profile only after provider succeeds**

Flow:

```text
Load profile
construct Resources provider
Track/EnableLink
update d.Resources
set Capture.Resources
update UpdatedAt
profile.Save
```

If `profile.Save` fails after a staged snapshot transaction, invoke provider rollback.

- [ ] **Step 10: Implement `untrack`**

Accept:

```text
dotfiles
resource:dotfiles
link:~/.config/example
```

Bare value is a resource ID.

No filesystem mutation outside profile snapshot storage.

- [ ] **Step 11: Implement `tracked`**

No machine detection required for the basic listing; it is a profile registry view.

- [ ] **Step 12: Run app tests**

```bash
go test ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 13: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "feat: add portable resource tracking commands"
```

---

### Task 12: `omarchy-setup` End-to-End Acceptance and Hooks Handoff

**Files:**
- Modify: `internal/app/app_test.go`
- Modify: `internal/providers/hooks/provider_test.go` only if a direct regression is clearer there.

**Interfaces:**
- Exercises:
  ```text
  track
  capture resources
  status resources
  restore resources
  aggregate restore
  Hooks symlink unmanaged policy
  ```

- [ ] **Step 1: Build source Git fixture**

Create a real local bare remote plus source clone so native `git` behavior is exercised without network.

Example test setup commands through `exec.Command` in test setup:

```text
git init --bare <remote>
git init <source>
git -C <source> config user.email test@example.com
git -C <source> config user.name Test
write hooks/omarchy-theme-hook.sh
write config/hyprland-overrides.lua
git -C <source> add .
git -C <source> commit -m fixture
git -C <source> remote add origin <portable-test-remote>
```

Because v1 rejects local filesystem remotes as nonportable in production, the provider's Git runner fixture may need to report a stable fake SSH remote while actual restore Git commands use a test command shim. Prefer dependency injection over weakening production remote validation.

- [ ] **Step 2: Create source links**

```text
$HOME/.config/hypr/overrides.lua
→ $HOME/omarchy-setup/config/hyprland-overrides.lua

$HOME/.config/omarchy/hooks/theme-set
→ $HOME/omarchy-setup/hooks/omarchy-theme-hook.sh
```

Use absolute links on the source side to prove restore normalization does not preserve source-machine absolute link text.

- [ ] **Step 3: Track the Git resource**

Run:

```bash
omarchy-blueprint --profile <profile> track <home>/omarchy-setup
```

Assert `resources/resources.toml` contains:

```text
one git resource
two inbound links
no copied resource bytes for omarchy-setup
```

- [ ] **Step 4: Prove Hooks capture still succeeds**

Run aggregate or Hooks capture with the live `theme-set` symlink.

Expected:

```text
capture succeeds
Hooks does not store theme-set bytes
warning behavior remains non-drift
Resources owns the link
```

- [ ] **Step 5: Switch to a fresh target HOME**

Use a different temp home and same profile.

The target has neither repo nor links.

Run:

```bash
omarchy-blueprint --profile <profile> restore resources --yes
```

or aggregate restore.

- [ ] **Step 6: Assert operation order**

Recorded plan/execution must put:

```text
Git checkout
before
hypr overrides symlink
and
theme-set symlink
```

- [ ] **Step 7: Assert exact revision restored**

```bash
git -C <target>/omarchy-setup rev-parse HEAD
```

must equal captured revision.

- [ ] **Step 8: Assert symlink text is relative**

```go
raw, err := os.Readlink(filepath.Join(targetHome, ".config", "hypr", "overrides.lua"))
if err != nil {
    t.Fatal(err)
}
if filepath.IsAbs(raw) {
    t.Fatalf("restored link is absolute: %q", raw)
}
```

Resolve it and prove it lands on the target machine's `omarchy-setup` file.

Repeat for Hooks `theme-set`.

- [ ] **Step 9: Assert post-restore verify**

Resources Verify must be OK.

Hooks must still treat `theme-set` as unmanaged rather than trying to replace/follow it.

- [ ] **Step 10: Add new-inbound-link capture regression**

After initial capture, add:

```text
~/.config/new-tool -> ~/omarchy-setup/config/new-tool
```

and corresponding target file.

Before capture:

```text
status resources → exit 2, + link
```

After:

```text
capture resources
status resources → clean for that link
```

- [ ] **Step 11: Add future-TUI candidate regression**

Create:

```text
~/.config/untracked -> ~/another-dotfiles/config
```

without tracking `another-dotfiles`.

Assert scanner reports `LinkUntrackedHomeTarget`, but profile `Resources.Items` is unchanged.

- [ ] **Step 12: Run focused vertical tests**

```bash
go test ./internal/app ./internal/providers/hooks -run 'Test.*Resource|Test.*OmarchySetup|Test.*Symlink' -count=1
```

Expected: PASS.

- [ ] **Step 13: Run full suite**

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 14: Commit**

```bash
git add internal/app/app_test.go internal/providers/hooks/provider_test.go
git commit -m "test: restore dotfiles resources and inbound links"
```

---

### Task 13: Documentation, Roadmap, and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Add: `docs/adr/0012-portable-resources.md`
- Add: `docs/superpowers/specs/2026-09-08-portable-resources-design.md`
- Add: `docs/superpowers/plans/2026-09-08-portable-resources.md`

**Interfaces:**
- Documents completed behavior.

- [ ] **Step 1: Update README profile layout**

Show:

```text
resources/
├── resources.toml
└── files/
```

Document:

```bash
omarchy-blueprint track ~/dotfiles
omarchy-blueprint tracked
omarchy-blueprint untrack dotfiles
omarchy-blueprint capture resources
omarchy-blueprint restore resources
```

- [ ] **Step 2: Document automatic inbound links**

Use the concrete example:

```text
~/.config/hypr/overrides.lua
→ ~/dotfiles/hypr/overrides.lua
```

State clearly:

```text
Tracking ~/dotfiles automatically manages the inbound link.
Tracking is not recursively expanded to unrelated symlink targets.
```

- [ ] **Step 3: Update ROADMAP terminology**

Replace the feature-level `directories` provider language with Portable Resources where it refers to the provider/category.

Keep future strategy names:

```text
copy
git
mixed
reference
ignore
```

Mark v1 complete scope as:

```text
explicit file/dir tracking
clean Git provenance
semantic symlinks
automatic inbound-link adoption
```

Keep future items:

```text
mixed directory discovery
dirty Git state
path mappings
TUI reverse discovery
```

- [ ] **Step 4: Add ADR 0012**

Use the supplied ADR content and make sure it matches the final implementation rather than the initial plan if any implementation detail changed.

- [ ] **Step 5: Search for contradictory terminology**

Run:

```bash
grep -RniE \
  'directories provider|capture directories|restore directories|track ~/Projects|resources provider|Portable Resources' \
  README.md ROADMAP.md docs
```

Update only genuinely contradictory current-product statements. Historical design docs may retain their original terminology if clearly historical.

- [ ] **Step 6: Run gofmt**

```bash
gofmt -w \
  internal/profile/profile.go \
  internal/profile/profile_test.go \
  internal/ownership/*.go \
  internal/providers/resources/*.go \
  internal/model/model.go \
  internal/restore/executor.go \
  internal/restore/executor_test.go \
  internal/app/app.go \
  internal/app/providers.go \
  internal/app/app_test.go
```

- [ ] **Step 7: Run exact full verification**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: all exit 0.

- [ ] **Step 8: Inspect repository diff**

```bash
git status --short
git diff --check
git diff --stat
```

Expected:

- no whitespace errors;
- no test temp homes/profile snapshots;
- no real dotfiles;
- no private keys/secrets;
- no built binary committed.

- [ ] **Step 9: Commit docs**

```bash
git add README.md ROADMAP.md \
  docs/adr/0012-portable-resources.md \
  docs/superpowers/specs/2026-09-08-portable-resources-design.md \
  docs/superpowers/plans/2026-09-08-portable-resources.md
git commit -m "docs: describe portable resources"
```

---

## Recommended Review Slices

The implementation can remain one feature branch while reviewers gate these coherent slices:

### Slice A — State and safe primitives

Tasks 1–3 and 8:

```text
schema 7
path/ownership model
copy snapshot safety
directory/symlink/copy executor primitives
```

No end-user Resources workflow needs to merge before these are safe.

### Slice B — Semantic provider

Tasks 4–7 and 9:

```text
Git provenance
link discovery
track/capture provider
diff/check/verify
restore plan
```

### Slice C — Product integration

Tasks 10–13:

```text
app provider registry
track/untrack/tracked CLI
omarchy-setup vertical test
docs/roadmap
```

Do not merge a slice that leaves the default branch with a user-visible partially wired `resources` category.

## Final Regression Matrix

Run:

```bash
go test ./internal/profile -count=1
go test ./internal/ownership -count=1
go test ./internal/providers/resources -count=1
go test ./internal/restore -count=1
go test ./internal/app -count=1

go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Manually inspect a test-generated profile:

```text
resources/
├── resources.toml
└── files/
```

Confirm:

- Git resources have no copied repo bytes;
- copied snapshots have no symlink entries;
- links target resource IDs + relative paths;
- ignored links are deterministic logical HOME paths.

## Real-machine Acceptance Pass

On the source machine:

```bash
omarchy-blueprint --profile ~/omarchy-profile track ~/omarchy-setup
omarchy-blueprint --profile ~/omarchy-profile tracked
cat ~/omarchy-profile/resources/resources.toml
```

Confirm links such as:

```text
~/.config/hypr/overrides.lua
~/.config/omarchy/hooks/theme-set
```

were adopted when they resolve into `~/omarchy-setup`.

Then:

```bash
omarchy-blueprint --profile ~/omarchy-profile status resources
omarchy-blueprint --profile ~/omarchy-profile check
```

On a fresh target machine:

```bash
omarchy-blueprint --profile ~/omarchy-profile restore resources --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore resources
```

Confirm the plan shows:

```text
clone resource
checkout captured revision
create inbound links
```

and never proposes replacement of existing conflicting paths.

After restore:

```bash
readlink ~/.config/hypr/overrides.lua
readlink ~/.config/omarchy/hooks/theme-set
git -C ~/omarchy-setup rev-parse HEAD
omarchy-blueprint --profile ~/omarchy-profile status resources
```

The symlink text should be relative and the semantic targets should resolve inside the reconstructed resource.

## PR Review Gate

Before merge, explicitly review for these blocker classes:

1. Any copy code that follows a symlink.
2. Any snapshot containing symlink entries despite semantic link metadata.
3. Any automatic addition of an untracked resource from a symlink target.
4. Any reverse scan that recursively walks all of `$HOME`.
5. Any scan that follows a symlinked directory.
6. Any resource-root overlap accepted without an ownership decision.
7. Any generic Resources capture of Config/Plugin/Theme/Shell state already owned elsewhere.
8. Any Hooks symlink target bytes absorbed into Hooks.
9. Any Git dirty state silently discarded.
10. Any local/credential-bearing Git remote persisted as portable provenance.
11. Any restore that resets/updates/replaces an existing different Git checkout.
12. Any copied file/directory overwrite.
13. Any symlink overwrite.
14. Any parent-symlink TOCTOU path that can redirect a write.
15. Any Copy operation that can execute after its snapshot hash changed.
16. Any absolute source-machine symlink target written during restore.
17. Any link operation that can run when its required target resource is unsatisfied.
18. Any sensitive private-key/credential bytes added to the profile.
19. Any app test that reads the developer's real HOME.
20. Any provider registry/category naming that reintroduces `directories` as a separate category.

Only merge after exact-head CI passes the supported Go matrix.
