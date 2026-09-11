# Profile Git Sync v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe core + CLI workflow for initializing, inspecting, diffing, committing, fetching, fast-forward pulling, and pushing the Blueprint profile repository without turning Blueprint into a general Git client.

**Architecture:** A new `internal/profilegit` package owns deterministic Git inspection/actions for the canonical profile root and distinguishes Blueprint-managed profile paths from unrelated repository files. `internal/app/profile_git.go` is a thin Cobra/human/JSON adapter; TUI v1 later calls the same `profilegit.Service` directly through shared workflows.

**Tech Stack:** Go 1.25+, existing `command.Runner`, Git CLI invoked without a shell, Cobra, existing sensitive/content helpers, `github.com/sergi/go-diff/diffmatchpatch` v1.4.0, real temporary Git repositories in tests.

**Spec:** `docs/superpowers/specs/2026-09-11-profile-git-sync-v1-design.md`

## Global Constraints

- No profile schema bump.
- Git operations are rooted exactly at the canonical active profile directory; never discover/use a parent repository as the profile repository.
- Blueprint-managed paths are `profile.toml`, `packages/`, `themes/`, `plugins/`, `config/`, `defaults/`, `shell/`, `hooks/`, `resources/`, and `machines/`.
- Files outside managed paths are reported but never automatically staged/committed.
- Refuse Blueprint commit when any pre-existing staged change exists.
- `status` never fetches implicitly.
- `pull` requires the entire repository worktree/index to be clean and uses fast-forward-only semantics.
- Never stash, merge, rebase, reset working-tree bytes, force-push, or resolve conflicts automatically.
- Never invoke a shell for Git operations.
- Sanitize URL userinfo before human/JSON output.
- Never persist Git credentials/tokens into Blueprint profile data.
- Managed diff never renders sensitive, binary, special, or oversized content as text.
- Network operations occur only through explicit `fetch`, `pull`, or `push`.

---

## File structure

Create focused files:

```text
internal/profile/managed_paths.go              canonical Blueprint-owned profile path registry
internal/profile/managed_paths_test.go

internal/profilegit/service.go                 Service/root validation/common Git execution helpers
internal/profilegit/service_test.go
internal/profilegit/status.go                  porcelain-v2 parsing + repository status
internal/profilegit/status_test.go
internal/inspection/diff.go                    shared bounded/safe structured diff model
internal/inspection/diff_test.go
internal/profilegit/diff.go                    managed profile Git diff adapter
internal/profilegit/diff_test.go
internal/profilegit/commit.go                  managed-only commit transaction
internal/profilegit/commit_test.go
internal/profilegit/sync.go                    init/remote/fetch/pull/push
internal/profilegit/sync_test.go

internal/app/profile_git.go                    `profile git ...` Cobra adapter + rendering
internal/app/profile_git_test.go
```

Modify:

```text
internal/app/app.go                            register `profile` command
README.md                                      profile Git workflow
ROADMAP.md                                     mark simple Git profile integration delivered when complete
docs/manual-test-checklist.md                  disposable-remote acceptance
```

Do not put Git parsing/semantics in Cobra handlers.

---

### Task 1: Add the managed profile-path registry and profile-root service

**Files:**
- Create: `internal/profile/managed_paths.go`
- Create: `internal/profile/managed_paths_test.go`
- Create: `internal/profilegit/service.go`
- Create: `internal/profilegit/service_test.go`

**Interfaces:**

Produces:

```go
package profile

var ManagedTopLevelPaths = []string{
    "profile.toml",
    "packages",
    "themes",
    "plugins",
    "config",
    "defaults",
    "shell",
    "hooks",
    "resources",
    "machines",
}

func IsManagedRepositoryPath(path string) bool
```

and:

```go
package profilegit

type Service struct {
    Runner command.Runner
    Root   string
}

func New(runner command.Runner, profileRoot string) (Service, error)
func (s Service) RootPath() string
```

- [ ] **Step 1: Write managed-path tests**

```go
func TestIsManagedRepositoryPath(t *testing.T) {
    cases := map[string]bool{
        "profile.toml": true,
        "resources/resources.toml": true,
        "machines/desktop.toml": true,
        "README.md": false,
        ".git/config": false,
        "../outside": false,
        "/absolute": false,
    }
    for path, want := range cases {
        if got := IsManagedRepositoryPath(path); got != want {
            t.Errorf("IsManagedRepositoryPath(%q)=%v want %v", path, got, want)
        }
    }
}
```

Also prove Windows-style separators/non-canonical traversal are rejected rather than normalized into managed paths.

- [ ] **Step 2: Write service-root tests**

Create a real profile directory and symlink to it. Assert `New` stores an absolute, clean, symlink-resolved root when resolution succeeds. Assert empty root errors.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/profile ./internal/profilegit -run 'ManagedRepositoryPath|ServiceRoot' -count=1
```

Expected: FAIL because the files/package do not exist.

- [ ] **Step 4: Implement the registry and service constructor**

Use lexical slash-separated repository paths in `IsManagedRepositoryPath`:

```go
func IsManagedRepositoryPath(raw string) bool {
    if raw == "" || filepath.IsAbs(raw) || strings.Contains(raw, "\\") {
        return false
    }
    clean := path.Clean(raw)
    if clean != raw || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
        return false
    }
    for _, root := range ManagedTopLevelPaths {
        if clean == root || strings.HasPrefix(clean, root+"/") {
            return true
        }
    }
    return false
}
```

Canonicalize `Service.Root` using the existing machine/profile-root helper or move that helper to a neutral package if importing `machine` would create an inappropriate dependency. Do not duplicate two subtly different canonicalization implementations.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/profile ./internal/profilegit -run 'ManagedRepositoryPath|ServiceRoot' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/profile/managed_paths.go internal/profile/managed_paths_test.go \
  internal/profilegit/service.go internal/profilegit/service_test.go
git commit -m "feat: define managed profile repository paths"
```

---

### Task 2: Implement deterministic local repository status

**Files:**
- Create: `internal/profilegit/status.go`
- Create: `internal/profilegit/status_test.go`
- Modify: `internal/profilegit/service.go`

**Interfaces:**

Produces:

```go
type Change struct {
    Path            string `json:"path"`
    OriginalPath    string `json:"original_path,omitempty"`
    Index           string `json:"index,omitempty"`
    Worktree        string `json:"worktree,omitempty"`
    Managed         bool   `json:"managed"`
    OriginalManaged bool   `json:"original_managed,omitempty"`
}

type Status struct {
    Repository bool     `json:"repository"`
    Branch     string   `json:"branch,omitempty"`
    Head       string   `json:"head,omitempty"`
    Upstream   string   `json:"upstream,omitempty"`
    Ahead      int      `json:"ahead"`
    Behind     int      `json:"behind"`
    Origin     string   `json:"origin,omitempty"`
    Changes    []Change `json:"changes,omitempty"`
}

func (s Service) Status(ctx context.Context) (Status, error)
```

- [ ] **Step 1: Write real-Git lifecycle status tests**

Using `command.SystemRunner{}` and temp dirs, cover:

```text
non-Git profile                    → Repository=false, no error
initialized repo with no commits   → Repository=true, Head=""
committed main branch              → Branch=main, Head=40-hex
managed modified file              → Managed=true, Worktree=M
unmanaged README modification      → Managed=false
untracked managed file             → Managed=true
staged managed file                → Index is non-dot
file with spaces                   → exact Path preserved
rename                             → destination + OriginalPath represented exactly; both managed flags correct
managed → unmanaged rename           → source remains visible as OriginalManaged=true, destination Managed=false
```

Do not fake porcelain output for the primary parser acceptance test; use real Git so NUL/rename behavior is exercised.

- [ ] **Step 2: Add upstream/ahead-behind fixture**

Create a bare origin plus two clones. Fetch refs and assert local status reports upstream and counts from locally known refs without triggering network activity.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/profilegit -run 'Status|Ahead|Behind|Porcelain' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement repository-root detection**

Use:

```text
git -C <root> rev-parse --show-toplevel
```

A not-a-repository error yields `Repository=false`; if Git reports a top-level directory different from `Service.Root`, treat the profile as **not its own repository** rather than acting on the parent repo.

- [ ] **Step 5: Implement porcelain-v2 parser**

Use a command equivalent to:

```text
git -C <root> status --porcelain=v2 -z --branch --untracked-files=all
```

Parse branch headers and record types without splitting NUL-sensitive names on whitespace. For type-2 rename/copy records, consume the second NUL path correctly.

Sort final `Changes` by `Path` for stable JSON/human output.

- [ ] **Step 6: Read origin without leaking credentials**

Read `origin` via direct Git command when present. Add:

```go
func SanitizeRemote(raw string) string
```

For URL-form remotes, strip `url.User`; preserve normal scp-like `git@host:path` text.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/profilegit -run 'Status|Ahead|Behind|Porcelain|SanitizeRemote' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profilegit/status.go internal/profilegit/status_test.go internal/profilegit/service.go
git commit -m "feat: inspect profile git status"
```

---

### Task 3: Add init and origin management

**Files:**
- Create: `internal/profilegit/sync.go`
- Create: `internal/profilegit/sync_test.go`

**Interfaces:**

Produces:

```go
type Result struct {
    Changed bool   `json:"changed"`
    Commit  string `json:"commit,omitempty"`
    Status  Status `json:"status"`
}

func (s Service) Init(ctx context.Context) (Result, error)
func (s Service) Remote(ctx context.Context) (string, error)
func (s Service) SetRemote(ctx context.Context, rawURL string) (Result, error)
func (s Service) RemoveRemote(ctx context.Context) (Result, error)
func BrowserURL(rawRemote string) (string, bool)
```

- [ ] **Step 1: Write init tests**

Prove:

```text
profile loads + no repo → initializes branch main
second Init            → successful Changed=false
profile nested in unrelated parent repo → creates/uses only profile/.git when Init requested
invalid existing .git filesystem object → error, no profile file mutation
```

- [ ] **Step 2: Write origin tests**

With real Git:

```text
Remote with no origin        → "", nil
SetRemote first time         → origin added
SetRemote second URL         → origin URL replaced only
other remote "backup"       → preserved
RemoveRemote                 → origin removed
RemoveRemote again           → idempotent Changed=false
URL with user:password@host  → stored by Git as requested but returned Status/Remote is sanitized
```

Use a clearly fake credential URL in a temp repository; never print it in test failure messages.

Also table-test `BrowserURL`:

```text
https://github.com/acme/profile.git → https://github.com/acme/profile
git@github.com:acme/profile.git    → https://github.com/acme/profile
ssh://git@gitlab.com/acme/profile  → https://gitlab.com/acme/profile
file:///tmp/repo                   → unsupported
custom::repo                       → unsupported
https://user:secret@host/repo.git  → https://host/repo
```

No test may log the credential-bearing input on failure.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/profilegit -run 'Init|Remote' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement direct Git actions**

`Init`:

```go
_, err := s.Runner.Run(ctx, "git", "init", "-b", "main", s.Root)
```

Before invoking, check whether `Status.Repository` is already true.

Origin management uses:

```text
git -C root remote add origin <url>
git -C root remote set-url origin <url>
git -C root remote remove origin
```

No shell.

- [ ] **Step 5: Return refreshed sanitized status**

Every mutating action ends with `Status(ctx)` and returns it.

- [ ] **Step 6: Run tests**

```bash
go test ./internal/profilegit -run 'Init|Remote' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/profilegit/sync.go internal/profilegit/sync_test.go
git commit -m "feat: initialize profile git repositories"
```

---

### Task 4: Add the shared bounded diff model and managed profile diff

**Files:**
- Create: `internal/inspection/diff.go`
- Create: `internal/inspection/diff_test.go`
- Create: `internal/profilegit/diff.go`
- Create: `internal/profilegit/diff_test.go`
- Modify: `internal/profilegit/service.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**

Produces the presentation-independent shared representation:

```go
package inspection

type DiffKind string

const (
    DiffText        DiffKind = "text"
    DiffBinary      DiffKind = "binary"
    DiffSensitive   DiffKind = "sensitive"
    DiffTooLarge    DiffKind = "too-large"
    DiffMetadata    DiffKind = "metadata"
    DiffUnavailable DiffKind = "unavailable"
)

type DiffFact struct {
    Key   string `json:"key"`
    Value string `json:"value"`
}

type DiffLine struct {
    Kind    string `json:"kind"` // context | add | remove
    OldLine int    `json:"old_line,omitempty"`
    NewLine int    `json:"new_line,omitempty"`
    Text    string `json:"text"`
}

type DiffHunk struct {
    OldStart int        `json:"old_start"`
    OldCount int        `json:"old_count"`
    NewStart int        `json:"new_start"`
    NewCount int        `json:"new_count"`
    Lines    []DiffLine `json:"lines"`
}

type DiffDocument struct {
    Kind     DiffKind   `json:"kind"`
    Title    string     `json:"title,omitempty"`
    OldLabel string     `json:"old_label,omitempty"`
    NewLabel string     `json:"new_label,omitempty"`
    Hunks    []DiffHunk `json:"hunks,omitempty"`
    Metadata []DiffFact `json:"metadata,omitempty"`
}

func BuildTextDiff(oldLabel string, old []byte, newLabel string, new []byte) DiffDocument
```

Profile Git result:

```go
type DiffFile struct {
    Path     string                  `json:"path"`
    Document inspection.DiffDocument `json:"document"`
    Deleted  bool                    `json:"deleted,omitempty"`
    New      bool                    `json:"new,omitempty"`
}

type Diff struct {
    Files []DiffFile `json:"files"`
}

func (s Service) Diff(ctx context.Context, onlyPath string) (Diff, error)
```

- [ ] **Step 1: Write shared diff safety/shape tests**

Cases:

```text
normal text modification → structured hunks with add/remove/context + line numbers
new file                 → old side empty
binary NUL               → DiffBinary, no Hunks
sensitive fixture        → DiffSensitive, no Hunks
>2 MiB or >20k lines     → DiffTooLarge
mode-only metadata       → DiffMetadata facts
ESC/OSC/control bytes    → sanitized; DiffLine.Text contains no ESC byte
very long line           → capped at 16 KiB with explicit truncation metadata
```

- [ ] **Step 2: Write Profile Git fixture tests**

Real repo cases:

```text
tracked managed text modified     → DiffText document
managed new untracked text        → DiffText, New=true
managed deleted file              → DiffText/metadata, Deleted=true
unmanaged README modified         → omitted from default Diff
onlyPath=README.md                 → rejected as unmanaged
binary managed file with NUL      → DiffBinary, no text hunks
>2 MiB managed text               → DiffTooLarge
sensitive token-like managed file → DiffSensitive, no text hunks
mode-only managed change          → DiffMetadata, not silently empty
unborn HEAD                       → managed files treated as new
```

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/inspection ./internal/profilegit -run 'Diff' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Pin the pure-Go diff dependency and implement bounds first**

```bash
go get github.com/sergi/go-diff@v1.4.0
```

Define:

```go
const MaxPreviewBytes = 2 << 20
const MaxPreviewLines = 20_000
const MaxPreviewLineBytes = 16 << 10
```

Use existing regular-file/content helpers and sensitive scanner. Reject symlink/special content from text rendering. Sanitize C0/C1 terminal controls before storing `DiffLine.Text`; preserve normalized newline/tab semantics only.

Use `diffmatchpatch` in line mode to build deterministic hunks after all safety/bounds checks. The UI must never need to parse a unified patch string.

- [ ] **Step 5: Read HEAD content without checkout mutation**

For tracked paths use direct Git blob access:

```text
git -C root show HEAD:<slash-path>
```

through bounded `command.RunOutput`. For unborn HEAD/missing path, treat the old side as empty. Do not use `git checkout` or temp working-tree mutation.

- [ ] **Step 6: Build Profile Git `DiffFile` values**

Default diff enumerates changed managed paths from `Status`. `onlyPath` must pass canonical managed-path validation. For each safe path, build `inspection.DiffDocument` with explicit labels `HEAD/<path>` and `worktree/<path>`. Mode-only/deleted/special cases use metadata facts rather than fake text.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/inspection ./internal/profilegit -run 'Diff' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/inspection internal/profilegit/diff.go internal/profilegit/diff_test.go \
  internal/profilegit/service.go go.mod go.sum
git commit -m "feat: inspect managed profile diffs safely"
```

---

### Task 5: Implement managed-only commit with index safety

**Files:**
- Create: `internal/profilegit/commit.go`
- Create: `internal/profilegit/commit_test.go`

**Interfaces:**

Produces:

```go
var ErrStagedChanges = errors.New("profile repository already has staged changes")

func SuggestedCommitMessage(status Status) string
func (s Service) Commit(ctx context.Context, message string) (Result, error)
```

- [ ] **Step 1: Write commit acceptance tests**

Real repository cases:

```text
managed config + resources changes
→ one commit containing only those paths

unmanaged README unstaged change
→ remains uncommitted and modified after Blueprint commit

pre-existing staged managed change
→ ErrStagedChanges; index/worktree unchanged

pre-existing staged unmanaged change
→ ErrStagedChanges; index/worktree unchanged

no managed changes
→ success Changed=false, no new commit

message=""
→ deterministic SuggestedCommitMessage used
```

Inspect commits with `git diff-tree --name-only` rather than only status output.

- [ ] **Step 2: Add commit-failure cleanup test**

Configure a failing commit hook or use a deterministic test runner around the final commit command after real staging. Assert all Blueprint-staged paths are unstaged afterward and working-tree bytes are unchanged.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/profilegit -run 'Commit|SuggestedCommitMessage' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement staged-state gate**

From `Status.Changes`, reject if any `Index` represents a staged change before Blueprint modifies the index.

- [ ] **Step 5: Stage only managed roots**

Build explicit arguments:

```go
args := []string{"-C", s.Root, "add", "-A", "--"}
args = append(args, profile.ManagedTopLevelPaths...)
```

After staging, inspect status again and verify every staged path satisfies `profile.IsManagedRepositoryPath`.

If not, run:

```text
git -C root reset -- <managed-pathspecs...>
```

and return an error.

Because pre-existing staging was refused, this cleanup cannot destroy a user staging selection.

- [ ] **Step 6: Commit and clean up failure**

Use:

```text
git -C root commit -m <message>
```

On failure, unstage the same managed pathspecs. Never reset/checkout worktree bytes.

On success, read HEAD SHA and refreshed status into `Result`.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/profilegit -run 'Commit|SuggestedCommitMessage' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profilegit/commit.go internal/profilegit/commit_test.go
git commit -m "feat: commit managed profile changes"
```

---

### Task 6: Implement explicit fetch, safe pull, and push

**Files:**
- Modify: `internal/profilegit/sync.go`
- Modify: `internal/profilegit/sync_test.go`

**Interfaces:**

Produces:

```go
func (s Service) Fetch(ctx context.Context) (Result, error)
func (s Service) Pull(ctx context.Context) (Result, error)
func (s Service) Push(ctx context.Context) (Result, error)
```

- [ ] **Step 1: Write fetch/ahead-behind integration test**

Use a bare origin and two working repositories:

```text
clone A pushes commit
clone B Status before Fetch uses old local refs
clone B Fetch
clone B Status reports Behind=1
```

Also prove `Status` alone does not contact/update the origin by comparing refs before/after.

- [ ] **Step 2: Write pull tests**

Cases:

```text
clean repo behind one commit       → Pull fast-forwards
managed unstaged change            → Pull refused, bytes unchanged
unmanaged unstaged change          → Pull refused
staged change                       → Pull refused
diverged histories                 → Pull returns ff-only guidance, no merge commit
missing upstream                   → actionable error
```

- [ ] **Step 3: Write push tests**

Cases:

```text
branch has no upstream + origin    → push --set-upstream origin <branch>
existing upstream                  → normal push
detached HEAD                      → refused
missing origin                     → actionable error
uncommitted worktree changes       → push committed history allowed; refreshed Status still reports changes
```

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/profilegit -run 'Fetch|Pull|Push' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement explicit network operations**

Use direct commands only:

```text
git -C root fetch origin
git -C root pull --ff-only
git -C root push
git -C root push --set-upstream origin <branch>
```

`Pull` first requires `Status` and rejects any `Changes`, managed or unmanaged.

`Push` refuses detached HEAD and missing origin; it does not require a clean working tree.

- [ ] **Step 6: Run tests**

```bash
go test ./internal/profilegit -run 'Fetch|Pull|Push' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/profilegit/sync.go internal/profilegit/sync_test.go
git commit -m "feat: sync profile git repositories"
```

---

### Task 7: Add the `profile git` CLI/JSON surface

**Files:**
- Create: `internal/app/profile_git.go`
- Create: `internal/app/profile_git_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**

Produces commands:

```text
profile git status
profile git init
profile git remote
profile git remote set <url>
profile git remote remove
profile git fetch
profile git diff [path]
profile git commit [-m <message>]
profile git pull
profile git push
```

- [ ] **Step 1: Write command-registration and non-Git status tests**

Use `Execute` with temp profile and injected output. Assert human and JSON shapes contain `repository`, `branch`, `ahead`, `behind`, and change managed flags without exposing internal command strings.

- [ ] **Step 2: Write lifecycle CLI integration test**

Use a temp profile + bare local origin:

```go
run("profile", "git", "init")
run("profile", "git", "remote", "set", origin)
run("profile", "git", "commit", "-m", "Initial profile")
run("profile", "git", "push")
```

Assert Git state with real Git commands and verify `--json profile git status` parses into the expected stable fields.

- [ ] **Step 3: Write refusal/output tests**

Cover staged-change commit refusal, dirty pull refusal, detached push refusal, and sanitized URL JSON.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/app -run 'ProfileGit' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Register a focused command tree**

In `app.go` add `profileCommand(deps, opt)` once. Implement Git subcommands in `profile_git.go`.

Construct service through one helper:

```go
func profileGitService(deps Dependencies, opt *options) (profilegit.Service, error) {
    return profilegit.New(deps.Runner, opt.profileDir)
}
```

Do not duplicate Git policy in handlers.

- [ ] **Step 6: Render stable human/JSON output**

Human status example:

```text
Profile Git
  repository: yes
  branch: main
  origin: git@github.com:user/profile.git
  upstream: origin/main
  ahead/behind: 1/0

Changes
  M resources/resources.toml  managed
  ? README.md                 unmanaged
```

JSON uses existing `emit` envelope and serializes `profilegit.Status`/`Diff` directly or through a stable DTO.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/app -run 'ProfileGit' -count=1
go test ./internal/profilegit -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/profile_git.go internal/app/profile_git_test.go
git commit -m "feat: expose profile git workflow"
```

---

### Task 8: Verify no regression in profile save/capture and document the workflow

**Files:**
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Modify: `docs/manual-test-checklist.md`
- Modify: `docs/adr/0017-tui-interactive-client-architecture.md` only for implementation-required clarification

**Interfaces:**
- Documentation/acceptance only.

- [ ] **Step 1: Add a capture-to-commit regression test**

In the most appropriate app integration test file, create an initialized profile Git repo, run a real Blueprint profile mutation/capture already supported by tests, then assert:

```text
profile git status → managed changes
profile git commit → commits only Blueprint-managed files
profile still loads after commit
```

- [ ] **Step 2: Update README**

Document the simple flow:

```bash
omarchy-blueprint --profile ~/omarchy-profile profile git init
omarchy-blueprint --profile ~/omarchy-profile profile git remote set <remote>
omarchy-blueprint --profile ~/omarchy-profile profile git status
omarchy-blueprint --profile ~/omarchy-profile profile git diff
omarchy-blueprint --profile ~/omarchy-profile profile git commit
omarchy-blueprint --profile ~/omarchy-profile profile git push
```

State explicitly that complex Git history/conflicts belong in Git/LazyGit.

- [ ] **Step 3: Update roadmap accurately**

Mark **simple Profile Git Sync v1** delivered when implementation is complete. Do not claim generic cloud backup, automatic commits, history UI, or full Git workflow is complete.

- [ ] **Step 4: Expand manual checklist**

Add disposable-remote checks for init/remote/commit/push/fetch/ff-only pull plus staged/dirty refusal and unmanaged-file preservation.

- [ ] **Step 5: Run full verification**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Expected: all PASS.

- [ ] **Step 6: Run manual disposable-remote acceptance**

Use two temporary profile checkouts against a temporary or disposable remote and confirm:

```text
capture/profile mutation → managed status → commit → push
second checkout fetch/pull → profile loads
unmanaged file stays uncommitted
pre-staged state blocks Blueprint commit
local dirty state blocks Blueprint pull
divergence blocks ff-only pull
```

- [ ] **Step 7: Commit docs/tests**

```bash
git add README.md ROADMAP.md docs/manual-test-checklist.md \
  docs/adr/0017-tui-interactive-client-architecture.md internal/app/*_test.go
git commit -m "docs: document profile git sync"
```

---

## Final review checklist

Before raising the Profile Git Sync v1 PR:

```text
[ ] profile root is exact/canonical; parent repo is never silently used
[ ] managed path registry is centralized
[ ] status uses porcelain-v2/NUL-safe parsing
[ ] status performs no implicit fetch
[ ] origin userinfo is sanitized in all output
[ ] init does not auto-commit or auto-add remote
[ ] remote operations touch only origin
[ ] diff excludes unmanaged paths by default
[ ] diff hides sensitive/binary/oversized content
[ ] commit refuses all pre-existing staged state
[ ] commit stages/commits managed paths only
[ ] commit leaves unmanaged working-tree changes untouched
[ ] failed commit restores a clean index without touching worktree bytes
[ ] pull requires completely clean repo and is ff-only
[ ] push never force-pushes and handles first upstream safely
[ ] detached HEAD push is refused
[ ] no credentials are persisted in profile data
[ ] CLI + JSON expose all Sync-screen semantics
[ ] real Git integration tests cover lifecycle
[ ] go test ./... -count=1
[ ] go vet ./...
[ ] go build ./cmd/omarchy-blueprint
[ ] git diff --check
```
