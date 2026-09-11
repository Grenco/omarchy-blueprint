# Dirty Git State v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add policy-aware Git Resources so `git` manages only remote + exact HEAD, `git+diff` also preserves staged/unstaged tracked state plus explicitly selected untracked files, and `copy` treats a Git worktree as filesystem content without copying `.git` administration data.

**Architecture:** Extend the existing Resources provider rather than creating a new provider. Schema 10 carries durable strategy and Git-overlay metadata; Resources captures Git working state into hash-validated artifacts under `resources/git-state/<id>/`; restore reconstructs a missing repository in dependency order with a typed Git-patch operation; status/verification compare only the layers owned by the selected strategy. Resource artifact capture is made transaction-safe so `resources.toml`, copied snapshots, and Git-state artifacts cannot diverge after a failed capture.

**Tech Stack:** Go 1.25+, Cobra, `github.com/pelletier/go-toml/v2`, system Git CLI through `internal/command`, existing restore journal/executor and content hashing helpers.

**Spec:** `docs/superpowers/specs/2026-09-10-dirty-git-state-v2-design.md`

## Global Constraints

- Profile schema is exactly **10** for this milestone.
- Accepted Resource strategies are exactly `copy`, `git`, and `git+diff`.
- Omitting `track --strategy` keeps the safe v1 default: portable Git worktree → `git`; otherwise → `copy`.
- `git` desired state is portable remote + exact HEAD only. Staged, unstaged, and untracked local state is informational and never drift by itself.
- `git+diff` desired state is remote + exact HEAD + deterministic index patch + deterministic worktree patch + explicitly selected untracked regular files.
- Staged and unstaged tracked changes must remain distinct after restore.
- Untracked selection is exact repository-relative regular-file paths only. No globs, recursive directories, ignored files, symlinks, or special files.
- Refuse unresolved conflicts, active merge/rebase/cherry-pick/revert operations, and dirty/changed submodule state for `git+diff` capture.
- Maximum selected untracked file size is **16 MiB**.
- Maximum total persisted Git-state payload per Resource is **128 MiB**.
- High-confidence secret scanning applies to desired local content before persistence.
- Patch files must be regular files and hash-validated immediately before restore use.
- Patch application is exact-base. Never use `git apply --3way`, `--reject`, or a force fallback.
- Existing differing Resource destinations remain untouched; this milestone restores Git state only when the destination Resource is missing or already exactly satisfied.
- Exact HEAD remains authoritative; branch-follow policy is out of scope.
- No profile/resource repository is automatically committed or pushed.
- Existing Resource ownership and `ResourceLink` semantics remain unchanged.
- Explicit `copy` of a Git worktree excludes any path component named exactly `.git`; `.gitignore` and `.gitattributes` remain ordinary content.
- All Git commands are argument-vector execution, never shell strings.

---

## File Structure

### New files

- `internal/sensitive/scan.go` — shared high-confidence content scanner used by Config and Resources.
- `internal/sensitive/scan_test.go` — scanner token/private-key and bounded-read tests.
- `internal/providers/resources/git_state.go` — Git working-state inspection, deterministic patch generation, untracked-path validation, patch hashing, and Git-state comparison helpers.
- `internal/providers/resources/git_state_test.go` — real temporary Git fixtures covering staged/unstaged/untracked/conflict/operation/submodule behavior.
- `internal/providers/resources/transaction.go` — staged Resources artifact transaction and crash/error recovery for `resources.toml`, `files/`, and `git-state/`.
- `internal/providers/resources/transaction_test.go` — rollback/recovery/commit tests.

### Existing files to modify

- `internal/command/runner.go`, `internal/command/runner_test.go` — add bounded binary stdout support without mixing stderr into artifacts.
- `internal/profile/profile.go`, `internal/profile/profile_test.go` — schema 10 and persisted Git-state metadata.
- `internal/providers/config/policy.go`, `internal/providers/config/policy_test.go` — delegate content scanning to `internal/sensitive` without changing Config behavior.
- `internal/providers/resources/git.go`, `internal/providers/resources/git_test.go` — preserve remote sanitization while returning richer working-state metadata.
- `internal/providers/resources/snapshot.go`, `internal/providers/resources/snapshot_test.go` — support excluding `.git` components for explicit copy strategy.
- `internal/providers/resources/provider.go`, `internal/providers/resources/provider_test.go` — strategy-aware Track/Capture, same-path strategy updates, selected untracked policy, and staged artifact handling.
- `internal/providers/resources/diff.go`, `internal/providers/resources/diff_test.go` — policy-aware satisfaction/diff/check behavior.
- `internal/providers/resources/plan.go`, `internal/providers/resources/plan_test.go` — Git+diff restore graph and link dependencies.
- `internal/model/model.go` — typed `GitPatchApply` restore action.
- `internal/restore/executor.go`, `internal/restore/executor_test.go` — validate and execute typed Git patch operations.
- `internal/app/app.go`, `internal/app/providers.go`, `internal/app/app_test.go` — CLI flags, strategy updates, informational Git-state rendering/JSON, and transaction finalization.
- `README.md`, `docs/manual-test-checklist.md`, `docs/adr/0012-portable-resources.md`, `docs/adr/0015-dirty-git-resource-policies.md`, `docs/superpowers/specs/2026-09-08-portable-resources-design.md`, `ROADMAP.md` — delivered behavior and supersession notes.

---

### Task 1: Land the approved design and schema-10 profile model

**Files:**
- Create: `docs/adr/0015-dirty-git-resource-policies.md`
- Create: `docs/superpowers/specs/2026-09-10-dirty-git-state-v2-design.md`
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**
- Produces: `profile.GitUntrackedFile` and schema-10 `profile.Resource` fields consumed by every later task.
- Preserves: schema-9 `copy` and `git` Resources exactly when loaded into schema 10.

- [ ] **Step 1: Commit the approved design documents with final statuses**

Use the approved copies of the ADR/design. Ensure the headers are:

```markdown
## Status

Accepted
```

and:

```markdown
**Status:** Approved design
```

- [ ] **Step 2: Write failing schema/model tests**

Add tests equivalent to:

```go
func TestSchema10ResourceGitDiffRoundTrip(t *testing.T) {
    dir := t.TempDir()
    d := New("test", time.Unix(0, 0))
    d.Resources.Items = []Resource{{
        ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff",
        Remote: "github.com/example/dotfiles",
        Branch: "main",
        Revision: strings.Repeat("a", 40),
        IndexPatchHash: strings.Repeat("b", 64),
        WorktreePatchHash: strings.Repeat("c", 64),
        Untracked: []GitUntrackedFile{{Path: "notes.md", Hash: strings.Repeat("d", 64), Mode: "0644"}},
    }}
    d.Manifest.Capture.Resources = true
    if err := Save(dir, d); err != nil { t.Fatal(err) }
    got, err := Load(dir)
    if err != nil { t.Fatal(err) }
    if got.Manifest.Schema != 10 || !reflect.DeepEqual(got.Resources, d.Resources) {
        t.Fatalf("round trip=%#v", got.Resources)
    }
}

func TestSchema9GitResourceLoadsWithoutInventedGitState(t *testing.T) {
    // Load an existing schema-9 resource fixture containing Strategy="git".
    // Assert Strategy remains git and IndexPatchHash, WorktreePatchHash and Untracked are empty.
}
```

- [ ] **Step 3: Run the focused tests and confirm failure**

Run:

```bash
go test ./internal/profile -run 'TestSchema10ResourceGitDiffRoundTrip|TestSchema9GitResourceLoadsWithoutInventedGitState' -count=1
```

Expected: fail because schema 10 fields/types do not exist yet.

- [ ] **Step 4: Add schema-10 model fields**

Use this persisted shape:

```go
const Schema = 10

const (
    configSchema        = 2
    defaultsSchema      = 3
    shellSchema         = 4
    hooksSchema         = 5
    misePackagesSchema  = 6
    resourcesSchema     = 7
    configOverlaySchema = 8
    gitStateSchema      = 10
)

type GitUntrackedFile struct {
    Path string `json:"path" toml:"path"`
    Hash string `json:"hash" toml:"hash"`
    Mode string `json:"mode" toml:"mode"`
}

type Resource struct {
    ID       string `json:"id" toml:"id"`
    Path     string `json:"path" toml:"path"`
    Kind     string `json:"kind" toml:"kind"`
    Strategy string `json:"strategy" toml:"strategy"`
    Hash     string `json:"hash,omitempty" toml:"hash,omitempty"`
    Mode     string `json:"mode,omitempty" toml:"mode,omitempty"`
    Remote   string `json:"remote,omitempty" toml:"remote,omitempty"`
    Branch   string `json:"branch,omitempty" toml:"branch,omitempty"`
    Revision string `json:"revision,omitempty" toml:"revision,omitempty"`

    IndexPatchHash    string             `json:"index_patch_hash,omitempty" toml:"index_patch_hash,omitempty"`
    WorktreePatchHash string             `json:"worktree_patch_hash,omitempty" toml:"worktree_patch_hash,omitempty"`
    Untracked         []GitUntrackedFile `json:"untracked,omitempty" toml:"untracked,omitempty"`

    Dirty bool `json:"dirty,omitempty" toml:"-"`
}
```

Update `sortResources`/normalization so `Resource.Untracked` is sorted by slash-normalized `Path` and duplicate paths are rejected rather than silently collapsed.

- [ ] **Step 5: Make schema-9 migration non-destructive**

Do not manufacture any Git-state fields when `loadedSchema < gitStateSchema`. Existing `copy` and `git` data must load untouched except for the normal in-memory schema bump.

- [ ] **Step 6: Run profile tests**

```bash
go test ./internal/profile -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add docs/adr/0015-dirty-git-resource-policies.md \
  docs/superpowers/specs/2026-09-10-dirty-git-state-v2-design.md \
  internal/profile/profile.go internal/profile/profile_test.go
git commit -m "feat: add dirty git resource profile state"
```

---

### Task 2: Add bounded binary command output for Git artifacts

**Files:**
- Modify: `internal/command/runner.go`
- Modify: `internal/command/runner_test.go`

**Interfaces:**
- Produces: `command.RunOutput(ctx, runner, limit, name, args...) ([]byte, error)`.
- Requirement: production stdout bytes are separate from stderr and are rejected once they exceed the caller-supplied bound.

- [ ] **Step 1: Write failing runner tests**

Add tests for raw bytes, stderr separation, and size overflow. Use a temporary shell helper only inside `internal/command` tests:

```go
func TestRunOutputSeparatesStderrAndPreservesBytes(t *testing.T) {
    out, err := RunOutput(context.Background(), SystemRunner{}, 1024,
        "sh", "-c", "printf '\\001\\000\\377'; printf 'warning' >&2")
    if err != nil { t.Fatal(err) }
    if !bytes.Equal(out, []byte{1, 0, 255}) { t.Fatalf("out=%v", out) }
}

func TestRunOutputRejectsLimit(t *testing.T) {
    _, err := RunOutput(context.Background(), SystemRunner{}, 3,
        "sh", "-c", "printf 'abcd'")
    if err == nil || !strings.Contains(err.Error(), "output exceeds 3 bytes") {
        t.Fatalf("err=%v", err)
    }
}
```

- [ ] **Step 2: Run tests and verify failure**

```bash
go test ./internal/command -run 'TestRunOutput' -count=1
```

Expected: fail because `RunOutput` does not exist.

- [ ] **Step 3: Add an optional binary-output runner capability**

Keep the existing `Runner` interface intact so existing fakes do not all need rewriting:

```go
type OutputRunner interface {
    RunOutput(ctx context.Context, limit int64, name string, args ...string) ([]byte, error)
}

func RunOutput(ctx context.Context, runner Runner, limit int64, name string, args ...string) ([]byte, error) {
    if r, ok := runner.(OutputRunner); ok {
        return r.RunOutput(ctx, limit, name, args...)
    }
    out, err := runner.Run(ctx, name, args...)
    if err != nil { return nil, err }
    if int64(len(out)) > limit {
        return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
    }
    return []byte(out), nil
}
```

Implement `SystemRunner.RunOutput` with `exec.CommandContext`, a bounded stdout writer, and a separate stderr buffer. The stdout writer must return an error immediately after `limit+1` bytes so huge Git diffs are not retained in memory. Wrap process errors in `RunError` with stderr text; never append stderr to successful stdout bytes.

- [ ] **Step 4: Run command tests**

```bash
go test ./internal/command -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/command/runner.go internal/command/runner_test.go
git commit -m "feat: add bounded binary command output"
```

---

### Task 3: Inspect Git working state deterministically

**Files:**
- Create: `internal/providers/resources/git_state.go`
- Create: `internal/providers/resources/git_state_test.go`
- Modify: `internal/providers/resources/git.go`
- Modify: `internal/providers/resources/git_test.go`

**Interfaces:**
- Produces:

```go
type GitWorkingState struct {
    Remote           string
    Branch           string
    Revision         string
    StagedTracked    int
    UnstagedTracked  int
    Untracked        []string
    Conflicted       bool
    Operation        string
    DirtySubmodule   bool
}

func InspectGitWorkingState(ctx context.Context, runner command.Runner, root string) (GitWorkingState, bool, error)
```

- `DetectGitResource` remains available as a compatibility wrapper while callers migrate.

- [ ] **Step 1: Add a real temporary Git fixture helper**

In `git_state_test.go`, create helpers that initialize a repository, configure local identity, create a bare origin, push one commit, and set `origin` to a portable test URL after initial setup:

```go
func gitFixture(t *testing.T) string {
    t.Helper()
    root := t.TempDir()
    runGit(t, root, "init")
    runGit(t, root, "config", "user.email", "test@example.invalid")
    runGit(t, root, "config", "user.name", "Blueprint Test")
    os.WriteFile(filepath.Join(root, "file.txt"), []byte("A\n"), 0o644)
    runGit(t, root, "add", "file.txt")
    runGit(t, root, "commit", "-m", "initial")
    runGit(t, root, "remote", "add", "origin", "https://github.com/example/fixture.git")
    return root
}
```

Do not access the network.

- [ ] **Step 2: Write failing structured-state tests**

Cover at minimum:

```text
clean HEAD                     → 0 staged, 0 unstaged, no untracked
staged change                  → staged=1
same file staged then modified → staged=1, unstaged=1
untracked regular file         → sorted path in Untracked
unresolved merge               → Conflicted=true
active cherry-pick/rebase      → Operation set
staged gitlink or dirty submodule → DirtySubmodule=true
```

Use NUL-delimited porcelain v2 or equivalent plumbing; assertions must use filenames containing spaces to prove path parsing is safe.

- [ ] **Step 3: Run the tests and confirm failure**

```bash
go test ./internal/providers/resources -run 'TestInspectGitWorkingState' -count=1
```

Expected: fail because the structured inspector does not exist.

- [ ] **Step 4: Implement structured inspection**

Use direct argument vectors and NUL-delimited Git output. Preserve existing `PortableGitRemote` and revision validation.

Required checks:

```go
// provenance
git -C root rev-parse --show-toplevel
git -C root remote get-url origin
git -C root rev-parse HEAD
git -C root symbolic-ref --quiet --short HEAD

// working state
git -C root status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none
```

Detect active operations through `git rev-parse --git-path MERGE_HEAD`, `rebase-merge`, `rebase-apply`, `CHERRY_PICK_HEAD`, and `REVERT_HEAD`; test filesystem existence at the returned administrative path rather than assuming `.git` is a directory.

- [ ] **Step 5: Keep `DetectGitResource` as a wrapper**

The wrapper should map the structured state back to the old shape until later tasks remove legacy assumptions:

```go
func DetectGitResource(ctx context.Context, runner command.Runner, root string) (GitState, bool, error) {
    state, ok, err := InspectGitWorkingState(ctx, runner, root)
    if err != nil || !ok { return GitState{}, ok, err }
    dirty := state.StagedTracked > 0 || state.UnstagedTracked > 0 || len(state.Untracked) > 0 || state.Conflicted || state.DirtySubmodule
    return GitState{Remote: state.Remote, Branch: state.Branch, Revision: state.Revision, Dirty: dirty}, true, nil
}
```

- [ ] **Step 6: Run Resources Git tests**

```bash
go test ./internal/providers/resources -run 'Git' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/resources/git.go internal/providers/resources/git_test.go \
  internal/providers/resources/git_state.go internal/providers/resources/git_state_test.go
git commit -m "feat: inspect structured git working state"
```

---

### Task 4: Extract the shared high-confidence secret scanner

**Files:**
- Create: `internal/sensitive/scan.go`
- Create: `internal/sensitive/scan_test.go`
- Modify: `internal/providers/config/policy.go`
- Modify: `internal/providers/config/policy_test.go`

**Interfaces:**
- Produces:

```go
type Result struct {
    Sensitive bool
    Reason    string
}

func ScanReader(r io.Reader, maxBytes int64) (Result, error)
func ScanRegularFile(path string, maxBytes int64) (Result, error)
```

- [ ] **Step 1: Move the existing Config signatures into focused tests**

Add shared scanner tests for:

```text
OpenSSH/PKCS private-key PEM → sensitive
api_token/access_token/auth_token/refresh_token/client_secret with >=16 token chars → sensitive
large ordinary text → not sensitive
signature split across chunk boundary → sensitive
reader larger than maxBytes → error
```

- [ ] **Step 2: Run the new tests and confirm failure**

```bash
go test ./internal/sensitive -count=1
```

Expected: fail because package does not exist.

- [ ] **Step 3: Implement `internal/sensitive` using Config's current bounded scanner**

Move the existing high-confidence regexes/chunk overlap logic without broadening detection. `ScanRegularFile` must use `content.OpenRegularFile` so symlinks/special files are rejected.

Use stable reasons:

```go
const (
    ReasonPrivateKey = "private-key"
    ReasonToken      = "credential-token"
)
```

- [ ] **Step 4: Replace Config's implementation with a thin wrapper**

Keep `hasSensitiveContent(path)` in Config so the provider API and tests remain stable:

```go
func hasSensitiveContent(path string) (bool, error) {
    if sensitiveContentInspection != nil { sensitiveContentInspection() }
    result, err := sensitive.ScanRegularFile(path, MaxAutomaticConfigFileSize)
    return result.Sensitive, err
}
```

Adjust/remove Config-only regex instrumentation tests if they test implementation mechanics rather than behavior; retain the large ordinary-file regression and all token/PEM behavior.

- [ ] **Step 5: Run both packages**

```bash
go test ./internal/sensitive ./internal/providers/config -count=1
```

Expected: PASS with no Config semantic changes.

- [ ] **Step 6: Commit**

```bash
git add internal/sensitive internal/providers/config/policy.go internal/providers/config/policy_test.go
git commit -m "refactor: share sensitive content scanning"
```

---

### Task 5: Generate and validate Git-state artifacts

**Files:**
- Modify: `internal/providers/resources/git_state.go`
- Modify: `internal/providers/resources/git_state_test.go`

**Interfaces:**
- Produces:

```go
const MaxGitUntrackedFileSize int64 = 16 << 20
const MaxGitStateSize int64 = 128 << 20

type GitStateCapture struct {
    IndexPatch       []byte
    WorktreePatch    []byte
    IndexPatchHash   string
    WorktreePatchHash string
    Untracked        []profile.GitUntrackedFile
    TotalBytes       int64
}

func CaptureGitWorkingState(ctx context.Context, runner command.Runner, root string, selected []string) (GitStateCapture, error)
```

- [ ] **Step 1: Write failing patch-layer tests**

Construct a fixture where:

```text
HEAD file.txt = A
index file.txt = B
worktree file.txt = C
```

Assert:

- index patch is non-empty and deterministic across repeated capture;
- worktree patch is non-empty and deterministic;
- applying index patch with `git apply --index` to a fresh checkout produces index/worktree B;
- applying worktree patch next produces worktree C while index remains B.

Also test staged deletion, staged new file, binary modification, and executable-bit change.

- [ ] **Step 2: Write failing selected-untracked tests**

Required cases:

```text
exact untracked file → accepted, path/hash/mode returned
absolute / ../ / .git component → rejected
tracked path → rejected when newly selected
ignored path → rejected
symlink/directory/FIFO → rejected
>16 MiB → rejected
aggregate >128 MiB → rejected before persistence
```

- [ ] **Step 3: Run tests and verify failure**

```bash
go test ./internal/providers/resources -run 'TestCaptureGitWorkingState|TestValidateUntracked' -count=1
```

- [ ] **Step 4: Generate deterministic patch layers**

Use bounded binary output from Task 2:

```go
index, err := command.RunOutput(ctx, runner, MaxGitStateSize, "git", "-C", root,
    "diff", "--cached", "HEAD", "--binary", "--full-index", "--no-color",
    "--no-ext-diff", "--no-textconv", "--no-renames", "--")

worktree, err := command.RunOutput(ctx, runner, MaxGitStateSize-int64(len(index)), "git", "-C", root,
    "diff", "--binary", "--full-index", "--no-color",
    "--no-ext-diff", "--no-textconv", "--no-renames", "--")
```

Hash non-empty patch bytes with SHA-256; empty patch → empty hash.

- [ ] **Step 5: Refuse unsupported Git states before generation**

Return clear capture errors when `Conflicted`, `Operation != ""`, or `DirtySubmodule` is true. Do not generate partial artifacts first.

- [ ] **Step 6: Validate selected untracked paths and content**

Normalize to slash-separated paths with `path.Clean`. Reject absolute/empty/traversal/`.git` components. Use Git plumbing (`git ls-files --error-unmatch`, `git check-ignore -q`, and current untracked enumeration) to prove eligibility.

For each selected file:

- `content.OpenRegularFile`;
- size ≤ 16 MiB;
- Resource sensitive-path policy;
- `sensitive.ScanRegularFile`;
- SHA-256 hash;
- mode string `%04o`.

Sort metadata by path.

- [ ] **Step 7: Scan desired staged/unstaged content rather than patch text**

Enumerate locally introduced/modified desired paths. For index state, read stage-0 blobs using `git show :<path>` through bounded binary output and scan the resulting desired bytes. For worktree state, scan the resulting regular working-tree file. Deleted desired files require no content scan.

Do not reject merely because a removed/context patch line contains an upstream token.

- [ ] **Step 8: Run focused Resources tests**

```bash
go test ./internal/providers/resources -run 'GitWorkingState|GitState|Untracked' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/resources/git_state.go internal/providers/resources/git_state_test.go
git commit -m "feat: capture deterministic git working overlays"
```

---

### Task 6: Make Resources snapshots and metadata a recoverable capture transaction

**Files:**
- Create: `internal/providers/resources/transaction.go`
- Create: `internal/providers/resources/transaction_test.go`
- Modify: `internal/profile/profile.go`
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Produces:

```go
type PreparedCapture struct {
    State   profile.Resources
    Changes []model.Change
}

func (p Provider) PrepareCapture(ctx context.Context, saved profile.Resources, options CaptureOptions) (*PreparedCapture, error)
func (c *PreparedCapture) Install() error
func (c *PreparedCapture) Rollback() error
func (c *PreparedCapture) Finalize() error
```

`Install` makes new Resources artifacts/metadata visible while retaining recoverable previous state; `Rollback` restores the previous generation; `Finalize` removes backup/transaction debris after the outer profile save succeeds.

- [ ] **Step 1: Add a canonical Resources TOML marshal helper**

Extract the exact Resources serialization used by `profile.Save`:

```go
func MarshalResources(resources Resources) ([]byte, error) {
    copy := resources
    sortResources(&copy)
    return toml.Marshal(copy)
}
```

`profile.Save` must call this helper too, so transaction-staged `resources.toml` bytes exactly match normal profile serialization.

- [ ] **Step 2: Write failing transaction tests**

Cover:

```text
old files/resources.toml installed + prepared new generation + Install + Rollback → exact old generation
old generation + Install + Finalize → exact new generation; no previous/marker/stage debris
failure between moving first and last artifact → rollback restores all old artifacts
process-style stale marker with phase=rollback → next prepare recovers old generation
phase=committed → next prepare keeps new generation and removes previous debris
stale .capture-stage-* without marker → removed under lock
```

- [ ] **Step 3: Add a Resources capture lock**

Use the same kernel advisory-lock pattern already accepted for Config. Keep lock filename distinct from stage cleanup patterns:

```text
resources/.capture.lock
resources/.capture-stage-<random>/
```

Only clean stale stages/transaction remnants while holding the exclusive lock.

- [ ] **Step 4: Implement the transaction marker**

Use:

```text
resources/.capture-transaction
```

with exact phases:

```text
rollback
committed
```

Previous names:

```text
.resources.toml-previous
.files-previous
.git-state-previous
```

Recovery rule:

```text
marker=rollback  → previous generation is authoritative; restore it
marker=committed → current generation is authoritative; delete previous remnants
no marker        → if current exists, stale previous is removable; if current is missing and previous exists, restore previous
```

- [ ] **Step 5: Stage all three Resources components**

Every prepared capture stages:

```text
.capture-stage-X/resources.toml
.capture-stage-X/files/
.capture-stage-X/git-state/
```

Empty directories are still staged so strategy transitions can remove obsolete old artifacts atomically.

- [ ] **Step 6: Wire outer profile save to rollback/finalize**

For Resources capture/update, the orchestration sequence is:

```go
prepared, err := provider.PrepareCapture(...)
if err != nil { return err }
if err := prepared.Install(); err != nil { return err }
d.Resources = prepared.State
if err := profile.Save(opt.profileDir, d); err != nil {
    _ = prepared.Rollback()
    return fmt.Errorf("save profile: %w", err)
}
if err := prepared.Finalize(); err != nil { return err }
```

Do this in both `capture resources`/aggregate capture and `track` strategy/update paths. Do not leave one path using the old eager artifact swap.

- [ ] **Step 7: Run transaction, profile, and app tests**

```bash
go test ./internal/profile ./internal/providers/resources ./internal/app -run 'Resource|Capture' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profile/profile.go internal/providers/resources/transaction.go \
  internal/providers/resources/transaction_test.go internal/providers/resources/provider.go \
  internal/app/providers.go internal/app/app.go
git commit -m "fix: make resource capture artifacts transactional"
```

---

### Task 7: Implement strategy-aware Track/Capture and `.git`-safe copy

**Files:**
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/providers/resources/provider_test.go`
- Modify: `internal/providers/resources/snapshot.go`
- Modify: `internal/providers/resources/snapshot_test.go`

**Interfaces:**
- Produces:

```go
type TrackOptions struct {
    ID               string
    Strategy         string
    IncludeUntracked []string
    ExcludeUntracked []string
}

type CaptureOptions struct{}

func (p Provider) PrepareTrack(ctx context.Context, saved profile.Resources, resourcePath string, options TrackOptions) (*PreparedCapture, error)
```

- [ ] **Step 1: Write failing strategy-registration tests**

Cover:

```text
no strategy + portable Git root → git
no strategy + ordinary dir → copy
explicit git+diff + Git root → git+diff
explicit git/git+diff + regular file → error
explicit git/git+diff + non-Git dir → error
invalid strategy → error
same exact tracked path + different strategy → update existing ID
same path + mismatched --id → error
overlapping but non-identical root → error
```

- [ ] **Step 2: Write failing strategy-transition tests**

Cover all transitions:

```text
git → git+diff
git+diff → git
git → copy
git+diff → copy
copy → git
copy → git+diff
```

Assert the live repository bytes/index are never mutated by the strategy update itself.

- [ ] **Step 3: Implement same-path update matching before overlap rejection**

Resolve exact root identity first. If an existing item's expanded root equals the requested absolute path, update that item. Only then apply overlap rejection against other resources.

- [ ] **Step 4: Persist untracked selection policy**

For an effective `git+diff` strategy:

```go
selected := map[string]bool{}
for _, file := range existing.Untracked { selected[file.Path] = true }
for _, path := range options.IncludeUntracked { selected[normalized(path)] = true }
for _, path := range options.ExcludeUntracked { delete(selected, normalized(path)) }
```

New includes must be currently eligible. Existing selected entries follow recapture semantics from the spec: now tracked/absent → drop; now ignored/unsafe → fail and preserve previous profile.

- [ ] **Step 5: Add copy traversal option for Git administrative entries**

Introduce:

```go
type SnapshotOptions struct {
    ExcludeGitAdmin bool
}

func ScanCopyResourceWithOptions(root string, options SnapshotOptions) (SnapshotScan, error)
func StageCopyResourceWithOptions(source, destination string, options SnapshotOptions) (SnapshotScan, error)
```

Keep existing wrappers passing zero options.

When `ExcludeGitAdmin` is true, `filepath.WalkDir` skips any entry whose relative path component equals exactly `.git`. Do not skip `.gitignore` or `.gitattributes`.

- [ ] **Step 6: Use `.git` exclusion only for copy of a Git worktree**

When preparing a `copy` strategy, detect whether the tracked root itself is a Git worktree root. If so, stage with `ExcludeGitAdmin: true`; otherwise preserve existing v1 copy behavior.

- [ ] **Step 7: Run Resources tests**

```bash
go test ./internal/providers/resources -run 'Track|Strategy|Transition|Copy|Snapshot' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/resources/provider.go internal/providers/resources/provider_test.go \
  internal/providers/resources/snapshot.go internal/providers/resources/snapshot_test.go
git commit -m "feat: add git resource strategy policies"
```

---

### Task 8: Add CLI strategy and untracked-selection controls

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Consumes: `resources.TrackOptions` and `Provider.PrepareTrack` from Task 7.
- Produces: stable CLI flags used by later TUI/API surfaces.

- [ ] **Step 1: Write failing command tests**

Required command-level cases:

```text
track PATH --strategy git
track PATH --strategy git+diff --include-untracked notes.md
track same PATH --strategy copy
track same PATH --include-untracked another.txt
track same PATH --exclude-untracked notes.md
--include-untracked under effective git/copy → error
--exclude-untracked under effective git/copy → error
--strategy invalid → Cobra/provider error
--id mismatch on existing exact path → error
```

Inspect the saved `resources/resources.toml` after each successful command, not only the human message.

- [ ] **Step 2: Add Cobra flags**

In `trackCommand`:

```go
var id, strategy string
var includeUntracked, excludeUntracked []string

cmd.Flags().StringVar(&id, "id", "", "stable resource ID")
cmd.Flags().StringVar(&strategy, "strategy", "", "resource strategy: git, git+diff, or copy")
cmd.Flags().StringSliceVar(&includeUntracked, "include-untracked", nil, "include exact untracked file in git+diff state (repeatable)")
cmd.Flags().StringSliceVar(&excludeUntracked, "exclude-untracked", nil, "remove exact untracked file from git+diff state (repeatable)")
```

If Cobra's `StringSlice` comma semantics would make filenames containing commas ambiguous, use a custom repeatable string value that treats each occurrence literally. The persisted path identity must not split a filename on commas.

- [ ] **Step 3: Route all track changes through the prepared transaction**

Build:

```go
resourcesprovider.TrackOptions{
    ID: id,
    Strategy: strategy,
    IncludeUntracked: includeUntracked,
    ExcludeUntracked: excludeUntracked,
}
```

Then use `PrepareTrack → Install → profile.Save → Finalize`, rolling back on save failure.

- [ ] **Step 4: Render strategy choice and omitted dirty state**

For auto/explicit `git` on a dirty repo, print an informational message containing:

```text
strategy: git
Local Git state is not captured by strategy git
Use --strategy git+diff ... or --strategy copy ...
```

Do not add a `model.Change` for the omitted dirty state.

- [ ] **Step 5: Run app tests**

```bash
go test ./internal/app -run 'Track.*Resource|Git.*Strategy|Untracked' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "feat: expose resource strategy controls"
```

---

### Task 9: Make Resource detection, diff, status, verification, and check policy-aware

**Files:**
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/providers/resources/diff.go`
- Modify: `internal/providers/resources/diff_test.go`
- Modify: `internal/providers/resources/provider_test.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Produces runtime details without persisting them:

```go
type GitWorkingSummary struct {
    StagedTracked      int      `json:"staged_tracked"`
    UnstagedTracked    int      `json:"unstaged_tracked"`
    Untracked          []string `json:"untracked"`
    SelectedUntracked  []string `json:"selected_untracked,omitempty"`
}

type Detection struct {
    Resources profile.Resources
    Links     []LinkCandidate
    Git       map[string]GitWorkingSummary
}

func (p Provider) DetectDetailed(ctx context.Context, saved profile.Resources) (Detection, error)
```

Keep the existing `Detect` wrapper returning `(profile.Resources, []LinkCandidate, error)` for callers/tests that do not need runtime details.

- [ ] **Step 1: Write failing `git` satisfaction tests**

Assert:

```text
same remote + HEAD + dirty worktree under saved strategy=git
→ resourceSatisfied=true
→ Diff has no Resource change
→ Verify OK
```

Remote/HEAD mismatch must still produce drift.

- [ ] **Step 2: Write failing `git+diff` comparison tests**

Detection must calculate current deterministic patch hashes and current metadata for saved selected-untracked paths without persisting bytes.

Assert:

```text
same index/worktree patches + same selected untracked hashes/modes → satisfied
changed staged layer → drift
changed unstaged layer → drift
selected untracked differs/missing → drift
extra unselected untracked → no drift, informational only
```

- [ ] **Step 3: Update `resourceSatisfied`**

Use strategy ownership:

```go
case "git":
    return saved.Remote == current.Remote && saved.Revision == current.Revision
case "git+diff":
    return saved.Remote == current.Remote &&
        saved.Revision == current.Revision &&
        saved.IndexPatchHash == current.IndexPatchHash &&
        saved.WorktreePatchHash == current.WorktreePatchHash &&
        equalUntracked(saved.Untracked, current.Untracked)
```

Never include current `Dirty` in `git` satisfaction.

- [ ] **Step 4: Improve Resource change summaries**

For `git+diff`, emit specific managed drift summaries where possible:

```text
~ resource dotfiles staged Git state differs
~ resource dotfiles unstaged Git state differs
~ resource dotfiles untracked:notes.md differs
- resource dotfiles untracked:scripts/new-tool missing
```

Avoid duplicate generic + specific changes for the same cause.

- [ ] **Step 5: Extend profile `check` for Git-state artifacts**

For each `git+diff` item:

- non-empty patch hash ↔ regular patch file exists and hashes exactly;
- empty patch hash ↔ patch file must not exist;
- every selected untracked snapshot exists as regular file and hashes exactly;
- path is safe beneath `git-state/<id>/untracked`;
- mode parses to ≤ `0777`;
- metadata invariants match the strategy.

Continue existing Git/gh availability checks.

- [ ] **Step 6: Expose informational runtime state in status/diff JSON and human output**

Add a Resources-specific status detail path in app orchestration, analogous to Config's extra scan metadata rather than changing generic `model.Change` semantics.

Human output for a satisfied dirty `git` Resource should include:

```text
Git working state not managed
  dotfiles       2 staged, 1 unstaged, 3 untracked
  Capture policy: git
```

For `git+diff`, additional unselected untracked files should be informational only.

JSON should include `git_working_state` keyed or attached by resource ID with staged/unstaged counts and untracked/selected lists.

- [ ] **Step 7: Run provider/app tests**

```bash
go test ./internal/providers/resources ./internal/app -run 'Resource|GitWorking|Status|Diff|Check' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/resources/provider.go internal/providers/resources/provider_test.go \
  internal/providers/resources/diff.go internal/providers/resources/diff_test.go \
  internal/app/providers.go internal/app/app.go internal/app/app_test.go
git commit -m "feat: make git resource drift policy aware"
```

---

### Task 10: Add the hash-validated typed Git patch restore action

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/restore/executor.go`
- Modify: `internal/restore/executor_test.go`
- Modify: `internal/restore/plan.go`
- Modify: `internal/restore/plan_test.go`

**Interfaces:**
- Produces:

```go
type GitPatchApply struct {
    Repository string `json:"repository"`
    Source     string `json:"source"`
    SourceHash string `json:"source_hash"`
    ToIndex    bool   `json:"to_index"`
}
```

and `Operation.GitPatch *GitPatchApply`.

- [ ] **Step 1: Write failing plan validation tests**

Reject operations where:

```text
GitPatch coexists with Command/File/etc.
Repository empty
Source empty
SourceHash empty/invalid
Source is not a regular file at execution time
```

- [ ] **Step 2: Add `GitPatch` to the one-action model invariant**

Update `model.Operation`:

```go
GitPatch *GitPatchApply `json:"git_patch,omitempty"`
```

Update restore `ValidatePlan` and `executeOperation` action counting so exactly one action is still required.

- [ ] **Step 3: Write failing executor integrity tests**

Create a real temp repository and patch artifact. Assert:

```text
correct hash + ToIndex=true → index/worktree updated
correct hash + ToIndex=false → worktree updated only
tampered patch after plan creation → refused before git apply
patch path symlink → refused
repository path through symlinked parent → refused
patch does not apply → operation failure
```

- [ ] **Step 4: Implement executor validation immediately before Git**

Use:

```go
func executeGitPatch(ctx context.Context, runner command.Runner, action model.GitPatchApply) error {
    if err := validateSymlinkParents(action.Repository); err != nil { return err }
    got, err := content.HashRegularFile(action.Source)
    if err != nil { return err }
    if got != action.SourceHash {
        return fmt.Errorf("git patch source hash mismatch: got %s want %s", got, action.SourceHash)
    }
    args := []string{"-C", action.Repository, "apply", "--whitespace=nowarn"}
    if action.ToIndex { args = append(args, "--index") }
    args = append(args, action.Source)
    _, err = runner.Run(ctx, "git", args...)
    return err
}
```

Do not add `--3way`, `--reject`, or shell invocation.

- [ ] **Step 5: Run model/restore tests**

```bash
go test ./internal/model ./internal/restore -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/restore/executor.go internal/restore/executor_test.go \
  internal/restore/plan.go internal/restore/plan_test.go
git commit -m "feat: add validated git patch restore action"
```

---

### Task 11: Plan and verify full missing `git+diff` reconstruction

**Files:**
- Modify: `internal/providers/resources/plan.go`
- Modify: `internal/providers/resources/plan_test.go`
- Modify: `internal/providers/resources/diff.go`
- Modify: `internal/providers/resources/provider_test.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Consumes: typed `GitPatchApply` from Task 10 and normal `FileWrite` for selected untracked files.
- Produces: final ready-operation dependency for ResourceLink creation.

- [ ] **Step 1: Write failing restore-plan graph test**

For a missing `git+diff` Resource with both patches and one selected untracked file, assert exact ordering/dependencies:

```text
resources.git.clone.dotfiles
  ↓
resources.git.checkout.dotfiles
  ↓
resources.git.apply-index.dotfiles
  ↓
resources.git.apply-worktree.dotfiles
  ↓
resources.git.untracked.dotfiles.notes-md
  ↓
resource link operations
```

When either patch is absent, dependencies skip that node without breaking ordering.

- [ ] **Step 2: Add `git+diff` planning branch**

Clone/checkout are the existing v1 operations. Patch sources are:

```text
<profile>/resources/git-state/<id>/index.patch
<profile>/resources/git-state/<id>/worktree.patch
```

Selected-untracked sources are:

```text
<profile>/resources/git-state/<id>/untracked/<relative-path>
```

Use recorded hashes/modes in the operations.

- [ ] **Step 3: Keep existing differing destinations untouched**

`planResource` behavior must remain:

```text
exact complete desired state → no operation
missing destination → reconstruct
anything existing but not exactly satisfied → conflict/skip; no mutation
```

Specifically test same remote + HEAD but missing saved overlay: conflict/skip, not patch-in-place.

- [ ] **Step 4: Make ResourceLink readiness depend on the final reconstruction operation**

`resourcePlanState.ReadyOpID` for a missing `git+diff` Resource is the final patch/untracked operation, or checkout when no overlay artifacts exist. Existing inbound/internal link planning must depend on that final ID.

- [ ] **Step 5: Add end-to-end staged-vs-unstaged reconstruction test**

Use a real temporary Git source fixture with:

```text
HEAD A
index B
worktree C
selected untracked notes.md
```

Capture `git+diff`, remove the destination, run the planned restore through the real restore executor, and assert with Git plumbing:

```bash
git -C DEST rev-parse HEAD
git -C DEST show :file.txt
git -C DEST diff --cached
git -C DEST diff
```

Expected: HEAD = captured revision, index file = B, worktree file = C, selected untracked bytes/mode restored.

If the fixture uses a local bare origin for offline clone, keep the profile-facing remote validation isolated from network usage; do not make CI contact GitHub.

- [ ] **Step 6: Run Resources/restore/app tests**

```bash
go test ./internal/providers/resources ./internal/restore ./internal/app -run 'GitDiff|GitPatch|Resource.*Restore' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/resources/plan.go internal/providers/resources/plan_test.go \
  internal/providers/resources/diff.go internal/providers/resources/provider_test.go internal/app/app_test.go
git commit -m "feat: restore dirty git resource state"
```

---

### Task 12: Cover recapture policy, safety failures, and idempotence

**Files:**
- Modify: `internal/providers/resources/provider_test.go`
- Modify: `internal/providers/resources/git_state_test.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Validates all state-machine behavior from the approved spec; no new public interface should be introduced in this task unless a test exposes a real missing seam.

- [ ] **Step 1: Add recapture tests for selected untracked policy**

Required cases:

```text
selected still untracked → recapture current bytes/mode
selected becomes tracked → remove from Resource.Untracked and snapshot
selected disappears → remove from Resource.Untracked and snapshot
selected becomes ignored → capture fails; previous profile/artifacts unchanged
selected becomes symlink/directory/special/sensitive/oversized → capture fails; previous profile unchanged
new unselected untracked file appears → never auto-selected
```

- [ ] **Step 2: Add unsupported-operation preservation tests**

For conflict, active merge/rebase/cherry-pick/revert, and dirty submodule, assert:

```text
capture returns error
resources.toml unchanged
resources/files unchanged
resources/git-state unchanged
live repository unchanged
```

- [ ] **Step 3: Add size-limit atomicity tests**

Generate an oversized selected file and an aggregate payload >128 MiB using sparse/test-generated bytes where practical. Assert no staged/partial profile state becomes current.

- [ ] **Step 4: Add idempotence tests**

Capture the same `git+diff` state twice. Assert:

```text
second Diff(saved,current) empty
resources.toml byte-identical
index.patch byte-identical
worktree.patch byte-identical
selected untracked snapshot hashes identical
```

- [ ] **Step 5: Add `git` informational-only command acceptance**

Through `Execute`:

```text
track dirty portable repo with default strategy
→ strategy git saved
status resources
→ exit 0 if remote/HEAD match
→ human output states local Git state is not managed
→ JSON exposes working-state summary
```

- [ ] **Step 6: Run full Resources/app packages**

```bash
go test ./internal/providers/resources ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/providers/resources/provider_test.go internal/providers/resources/git_state_test.go internal/app/app_test.go
git commit -m "test: cover dirty git resource safety"
```

---

### Task 13: Update product documentation and roadmap delivery state

**Files:**
- Modify: `README.md`
- Modify: `docs/manual-test-checklist.md`
- Modify: `docs/adr/0012-portable-resources.md`
- Modify: `docs/superpowers/specs/2026-09-08-portable-resources-design.md`
- Modify: `ROADMAP.md`

**Interfaces:**
- No code interfaces. Documentation must describe actual implemented behavior only.

- [ ] **Step 1: Update README Resource examples**

Document:

```bash
omarchy-blueprint track ~/dotfiles --strategy git
omarchy-blueprint track ~/dev/hacky-repo --strategy git+diff --include-untracked notes.md
omarchy-blueprint track ~/some-local-tree --strategy copy
```

State clearly that `git` ignores local working state by policy and that `git+diff` does not auto-select untracked files.

- [ ] **Step 2: Add manual Dirty Git v2 acceptance**

Copy the approved spec's 11-step manual acceptance into `docs/manual-test-checklist.md`, adjusted only to match final CLI wording.

- [ ] **Step 3: Mark ADR 0012's dirty-Git limitation as superseded**

Do not rewrite the historical v1 decision. Add a short consequence note pointing to ADR 0015.

- [ ] **Step 4: Add a supersession note to Portable Resources v1 design**

State that sections describing dirty repositories as omitted are v1 historical behavior and are superseded by the Dirty Git State v2 design for schema 10+.

- [ ] **Step 5: Update ROADMAP delivery state without reordering unrelated phases**

Mark dirty repository preservation in Resources coverage as delivered and reference ADR 0015/design. Do not pull path mappings/TUI/Git workflow helpers into this milestone.

- [ ] **Step 6: Validate docs**

```bash
git diff --check
rg -n 'dirty Git|dirty repository|local changes are not captured|git\+diff|strategy' README.md ROADMAP.md docs
```

Review every hit for stale v1 claims that lack an explicit historical/superseded qualifier.

- [ ] **Step 7: Commit**

```bash
git add README.md ROADMAP.md docs/manual-test-checklist.md docs/adr/0012-portable-resources.md \
  docs/superpowers/specs/2026-09-08-portable-resources-design.md
git commit -m "docs: document dirty git resource policies"
```

---

### Task 14: Final verification and real-machine acceptance

**Files:**
- No new files expected; fix only defects revealed by verification.

**Interfaces:**
- This task is the merge gate.

- [ ] **Step 1: Run the complete CI-equivalent verification**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 2: Inspect profile schema compatibility**

Run focused migration/round-trip tests explicitly:

```bash
go test ./internal/profile -run 'Schema|Resource' -count=1
```

Expected: schema 9 loads; schema 10 round-trips; malformed Git-state metadata is rejected by normal validation/check paths.

- [ ] **Step 3: Perform disposable real-repository acceptance**

Use a disposable repository with a real portable origin but do not push Blueprint-created work. Exercise:

```bash
omarchy-blueprint --profile ~/omarchy-profile track ~/path/to/disposable-repo
omarchy-blueprint --profile ~/omarchy-profile status resources
omarchy-blueprint --profile ~/omarchy-profile track ~/path/to/disposable-repo --strategy git+diff --include-untracked notes.md
omarchy-blueprint --profile ~/omarchy-profile capture resources
omarchy-blueprint --profile ~/omarchy-profile status resources
omarchy-blueprint --profile ~/omarchy-profile --json status resources
omarchy-blueprint --profile ~/omarchy-profile check
```

Validate manually:

```text
safe default remains git
git dirtiness alone is informational / exit 0
staged + unstaged state captured separately
only selected untracked files stored
second capture is idempotent
extra unselected untracked file is informational
managed tracked edit becomes drift
missing-destination restore recreates HEAD/index/worktree/untracked state
existing differing destination is skipped
switching back to git removes git-state artifacts without touching live local work
copy strategy excludes .git data
```

- [ ] **Step 4: Inspect profile footprint**

```bash
du -sh ~/omarchy-profile/resources
find ~/omarchy-profile/resources/git-state -maxdepth 4 -type f -printf '%P %s bytes\n' 2>/dev/null | sort
```

Confirm no unexpected repository object database, ignored files, or unselected untracked files entered the profile.

- [ ] **Step 5: Review final branch delta against the approved spec**

```bash
git status --short
git log --oneline --decorate main..HEAD
git diff --stat main...HEAD
git diff --check main...HEAD
```

The branch should contain only Dirty Git v2, its transaction/sensitive-scanner prerequisites, and documentation updates.

- [ ] **Step 6: Final commit only if verification exposed a necessary fix**

If verification required a defect correction, commit that correction with a precise message and rerun Step 1. If no correction was necessary, do not create an empty “verification” commit.

---

## Plan Self-Review Checklist

Before implementation handoff, verify:

- Every acceptance criterion in `2026-09-10-dirty-git-state-v2-design.md` maps to at least one task above.
- `git` never uses `Dirty` as a satisfaction condition after Task 9.
- `git+diff` captures index and worktree as separate deterministic layers.
- Selected untracked files remain explicit policy and never become recursive/glob capture.
- Secret scanning examines desired local content rather than raw patch context.
- Binary patch stdout never contains stderr and is bounded before in-memory/profile persistence.
- Strategy transitions stage new state before old artifacts are discarded.
- Resource metadata and artifact rollback/recovery are covered before CLI strategy updates depend on them.
- Typed patch restore validates source bytes immediately before invoking Git.
- Missing-resource restore is complete before dependent links become ready.
- Existing differing repositories remain non-mutating conflicts.
- No task implements branch-follow, in-place compatible-repo patching, path mappings, TUI, profile auto-commit, or push/pull helpers.
