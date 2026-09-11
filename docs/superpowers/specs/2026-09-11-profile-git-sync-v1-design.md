# Profile Git Sync v1 Design

**Status:** Approved design prerequisite for TUI v1

**Date:** 2026-09-11

**ADR:** `docs/adr/0017-tui-interactive-client-architecture.md`

## 1. Purpose

Profile Git Sync v1 gives Omarchy Blueprint a small, safe, first-class workflow for version-controlling and synchronizing the **Blueprint profile itself**.

It is intentionally not a general Git client.

The motivating workflow is:

```text
change running Omarchy
        ↓
inspect/capture with Blueprint
        ↓
review profile repository changes
        ↓
commit
        ↓
push
```

The capability is implemented before the TUI Sync screen so the same operations and state are available through core Go APIs and CLI/JSON.

## 2. Product boundary

Blueprint owns simple profile-repository orchestration:

```text
status
initialize
origin remote
fetch
managed profile diff
commit managed profile changes
fast-forward-only pull
push
```

Blueprint does **not** own:

```text
interactive staging
merge conflict resolution
rebase
cherry-pick
branch surgery
history rewriting
submodule management
credential storage
SSH key management
Git hosting account creation
```

When repository state exceeds the safe v1 workflow, commands return an explanation and the TUI offers a LazyGit handoff.

## 3. Repository identity and scope

The repository, when present, is the canonical active profile directory resolved by the same canonical-profile-root logic used elsewhere in Blueprint.

Profile Git Sync never searches parent directories for a repository and never acts on a repository outside the active profile root.

A profile may remain entirely local and non-Git. Git is optional.

No profile schema bump is required. Git repository state lives in `.git/`; no current branch, remote, credentials, ahead/behind count, or commit hash is written into portable Blueprint TOML.

## 4. Blueprint-managed Git paths

Blueprint distinguishes profile paths it owns from unrelated files a user may keep in the same repository.

The v1 managed path registry is:

```text
profile.toml
packages/
themes/
plugins/
config/
defaults/
shell/
hooks/
resources/
machines/
```

The registry lives in profile/core code rather than being duplicated in CLI and TUI.

Future Blueprint-owned top-level paths must be added to this registry when their writer is introduced.

Files outside this registry are **unmanaged repository paths**. Blueprint reports them but does not stage or commit them automatically.

`.git/` is always excluded.

## 5. Core package

Add a presentation-independent package, conceptually:

```text
internal/profilegit/
```

It uses the existing `command.Runner` abstraction or an equivalent direct-exec abstraction. It never invokes a shell.

Core shape:

```go
type Service struct {
    Runner command.Runner
    Root   string
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

type Change struct {
    Path            string `json:"path"`
    OriginalPath    string `json:"original_path,omitempty"`
    Index           string `json:"index,omitempty"`
    Worktree        string `json:"worktree,omitempty"`
    Managed         bool   `json:"managed"`
    OriginalManaged bool   `json:"original_managed,omitempty"`
}
```

Exact internal field names may vary, but CLI JSON must retain equivalent information.

`Status` is local-only by default. It does not fetch.

## 6. Git status inspection

Use deterministic, machine-readable Git commands such as porcelain v2 with NUL termination. Do not parse human `git status` output.

Status must report:

- whether the profile root is a Git repository;
- current branch or detached HEAD;
- HEAD revision when present;
- origin URL sanitized for display;
- configured upstream when present;
- ahead/behind counts against locally known upstream refs;
- staged and unstaged status for every changed path;
- rename/copy source path when porcelain supplies one;
- whether the destination and original path are Blueprint-managed.

An uninitialized repository with no commits is valid and reports an empty HEAD.

Remote URL output must remove embedded username/password credentials. SSH/scp-like remote syntax may be shown because it does not embed a password by default, but userinfo in URL-form remotes must be stripped before output/logging.

## 7. CLI

Add:

```bash
omarchy-blueprint profile git status
omarchy-blueprint profile git init
omarchy-blueprint profile git remote
omarchy-blueprint profile git remote set <url>
omarchy-blueprint profile git remote remove
omarchy-blueprint profile git fetch
omarchy-blueprint profile git diff [path]
omarchy-blueprint profile git commit [-m <message>]
omarchy-blueprint profile git pull
omarchy-blueprint profile git push
```

All commands support the root global `--profile` flag.

State-returning commands support existing global `--json` output.

`profile git` commands do not require the current machine overlay because they operate on the profile repository, not live Resource placement.

## 8. `profile git init`

Preconditions:

- active profile must load successfully;
- profile root must not already be inside a different parent Git repository acting as the profile repository;
- `.git` must not already contain an incompatible/non-directory object.

Behavior:

```text
git init -b main <profile-root>
```

or an equivalent direct invocation.

If the profile is already a Git repository rooted at the profile directory, the operation is an idempotent no-op with structured status.

The command does not create a remote, does not make an automatic commit, and does not modify user Git global configuration.

## 9. Origin remote

v1 manages one conventional remote: `origin`.

Show:

```bash
omarchy-blueprint profile git remote
```

Set/add:

```bash
omarchy-blueprint profile git remote set <url>
```

- if `origin` does not exist, add it;
- if `origin` exists, replace only its URL;
- do not alter other remotes.

Remove:

```bash
omarchy-blueprint profile git remote remove
```

- remove only `origin`;
- missing `origin` is an idempotent no-op.

Never copy remote credentials into profile files or logs.

## 10. Explicit fetch and cached remote status

The following commands may access the network:

```text
fetch
pull
push
```

`status` never fetches implicitly.

`profile git fetch` requires `origin` and performs a normal fetch without pruning arbitrary user refs unless Git's normal configured behavior does so. Errors from authentication/network are returned as Git errors with command context but secrets are not echoed by Blueprint.

After fetch, a caller can request `Status` again to obtain current ahead/behind counts.

The TUI maps this to an explicit Refresh action.

## 11. Managed profile diff

`profile git diff` is a read-only inspection surface for the profile repository.

Default behavior includes only Blueprint-managed changed paths. A positional path, when supplied, must be a canonical path inside the profile root and must be Blueprint-managed.

Profile Git Sync v1 introduces the presentation-independent shared diff model under `internal/inspection`, because both CLI Git diff and the later TUI need the same bounded/safe representation.

The shared representation distinguishes:

```text
text
binary
sensitive
too-large
metadata-only
unavailable
```

with structured hunks/line numbers for text and metadata facts for non-text cases. `profilegit.DiffFile` carries one `inspection.DiffDocument` plus profile-path metadata such as new/deleted state.

For tracked content, compare HEAD to the current working-tree file. For an unborn HEAD, all managed working-tree files are new. For untracked managed text files, synthesize an empty → current diff.

Diff preview rules:

- do not render content classified sensitive by Blueprint's existing sensitive scanner;
- do not render special files or symlinks as text;
- cap each file preview at 2 MiB and 20,000 logical lines, with a 16 KiB per-line representation cap;
- sanitize terminal control characters before they enter structured diff lines;
- JSON exposes metadata and bounded structured hunks, never sensitive bytes.

The TUI later renders these same `inspection.DiffDocument` values rather than reparsing human/unified Git output.

## 12. Commit semantics

`profile git commit` commits **Blueprint-managed profile changes only**.

### 12.1 Refuse pre-existing staged state

Before changing the index, inspect repository status.

If any path is already staged, Blueprint refuses the simple commit workflow with guidance to finish or reset that staging in Git/LazyGit first.

Rationale: Blueprint must not silently combine its managed profile commit with user-created staged work or destroy a staging selection.

### 12.2 Leave unmanaged working-tree changes untouched

Unstaged/untracked files outside the managed path registry may remain present. Blueprint reports them but does not stage them and does not include them in the commit.

### 12.3 Stage managed paths only

Stage deletions/additions/modifications under the managed path registry using explicit pathspecs and `git add -A -- ...` or equivalent direct invocations.

After staging, verify that every staged path is managed before committing. If an unmanaged path appears in the index, abort and unstage Blueprint's attempted staging without modifying working-tree bytes.

### 12.4 Commit message

If `-m` is provided and non-empty, use it exactly as the commit message.

Otherwise derive a deterministic suggestion from changed managed top-level areas:

```text
Update Blueprint profile: config, resources
```

When only one area changed:

```text
Update Blueprint profile: machines
```

If area classification is unavailable:

```text
Update Blueprint profile
```

The TUI may present this as an editable one-line commit message before invoking the same core commit operation.

### 12.5 No changes

If no managed changes exist, return a successful no-op result rather than creating an empty commit.

### 12.6 Failure cleanup

If staging succeeds but commit fails, unstage the paths Blueprint staged while leaving working-tree bytes unchanged. Because pre-existing staged state is refused, cleanup does not need to reconstruct a prior index selection.

## 13. Pull semantics

`profile git pull` is deliberately strict.

Require:

- repository exists;
- HEAD exists;
- upstream exists;
- the entire repository index and working tree are clean, including unmanaged paths.

Then perform a fast-forward-only update:

```text
git pull --ff-only
```

No automatic stash, merge commit, conflict resolution, or rebase is permitted.

If fast-forward is impossible, return guidance to use Git/LazyGit.

After pull, callers reload the Blueprint profile from disk before any further operation.

## 14. Push semantics

Require:

- repository exists;
- HEAD exists;
- origin exists.

If the current branch has an upstream, perform a normal push.

If the current branch has no upstream and is not detached, establish it with:

```text
git push --set-upstream origin <branch>
```

Detached HEAD push is refused in v1.

Uncommitted working-tree changes do not prevent pushing already committed history, but status/output must make them visible so the TUI cannot imply that all profile changes were backed up.

## 15. Sync state terminology

Use precise state rather than a generic “synced” boolean.

Examples:

```text
not a repository
repository, no origin
clean, no upstream
3 managed working-tree changes
2 unmanaged working-tree changes
1 commit ahead
2 commits behind (last fetched state)
diverged: 2 ahead / 3 behind
detached HEAD
```

The TUI may summarize these, but JSON preserves the components.

## 16. TUI composition rules

The TUI Sync screen may compose operations:

```text
Commit & push
→ Commit
→ if commit/no-op succeeds, Push
→ refresh Status
```

It must not create a hidden `git add`, `git pull`, merge, or force-push path that is unavailable through the core/CLI semantics above.

After capture, the TUI may navigate directly to Sync and highlight new managed changes. Capture itself does not automatically commit or push in v1.

## 17. Remote browser handoff

The TUI may offer “Open remote” when `origin` can be safely converted to an HTTP(S) repository URL.

Supported conversions include conventional forms such as:

```text
https://host/owner/repo.git → https://host/owner/repo
git@host:owner/repo.git    → https://host/owner/repo
ssh://git@host/owner/repo  → https://host/owner/repo
```

Unknown/custom schemes are displayed/copyable but not guessed into a browser URL.

The shared Profile Git package exposes this pure helper so URL conversion is tested once:

```go
func BrowserURL(rawRemote string) (string, bool)
```

It strips a trailing `.git`, accepts only recognized HTTPS or SSH/scp-style Git repository forms, and returns only an `https://` URL. It never performs network I/O.

This is a TUI convenience, not a separate Git operation.

## 18. Security and privacy

- never use a shell for Git commands;
- never log command environments containing credentials;
- sanitize URL userinfo before display/JSON;
- do not persist tokens or credentials in Blueprint profile data;
- respect existing Git credential helpers/SSH agents without managing them;
- never auto-push or auto-fetch on capture/TUI startup;
- never use force push in v1;
- never run arbitrary Git hooks supplied by Blueprint itself; normal Git behavior of the user's repository remains outside Blueprint's control;
- do not read or render sensitive profile file contents in diff previews.

## 19. JSON expectations

Representative status envelope data:

```json
{
  "repository": true,
  "branch": "main",
  "head": "0123456789abcdef",
  "origin": "git@github.com:user/profile.git",
  "upstream": "origin/main",
  "ahead": 1,
  "behind": 0,
  "changes": [
    {
      "path": "resources/resources.toml",
      "index": ".",
      "worktree": "M",
      "managed": true
    }
  ]
}
```

Command results for init/remote/fetch/commit/pull/push include the refreshed `Status` plus a small operation-specific result such as created commit SHA where available.

## 20. Error guidance

Errors must distinguish recoverable simple-workflow conditions from execution failure.

Examples:

```text
profile is not a Git repository; run `omarchy-blueprint profile git init`
origin is not configured; run `... profile git remote set <url>`
repository has staged changes; finish them in Git/LazyGit before Blueprint commit
profile has local changes; pull requires a completely clean working tree
pull is not fast-forwardable; resolve repository history in Git/LazyGit
cannot push detached HEAD in Blueprint v1
```

Human output is actionable; JSON includes a stable error code where new workflow-specific errors are introduced.

## 21. Testing and acceptance

Automated tests use real temporary Git repositories for lifecycle behavior rather than mocking Git semantics that matter.

Required acceptance coverage:

```text
non-Git profile status
init new repository
init idempotence
set/show/remove origin
remote credential sanitization
status managed vs unmanaged changes
status staged/unstaged parsing including spaces/renames
unborn HEAD
managed diff including untracked/deleted/binary/oversized/sensitive
commit managed paths only
commit refuses pre-existing staging
commit leaves unmanaged worktree changes untouched
commit failure cleanup
fetch refreshes ahead/behind
pull requires clean entire repo
pull fast-forwards
pull rejects divergence
push establishes upstream
push existing upstream
push refuses detached HEAD
network/auth failure leaves profile bytes intact
JSON contains no secret URL userinfo
```

Manual acceptance on a disposable remote should confirm the natural flow:

```bash
omarchy-blueprint --profile "$PROFILE" profile git init
omarchy-blueprint --profile "$PROFILE" profile git remote set <test-remote>
omarchy-blueprint --profile "$PROFILE" profile git status
omarchy-blueprint --profile "$PROFILE" profile git diff
omarchy-blueprint --profile "$PROFILE" profile git commit -m "Initial Blueprint profile"
omarchy-blueprint --profile "$PROFILE" profile git push
```

Then modify/capture profile state, commit, push, fetch/pull from a second checkout, and confirm Blueprint profile loading remains correct.

## 22. Deferred

Not part of Profile Git Sync v1:

- Git hosting provider APIs;
- repository creation on GitHub/GitLab;
- automatic scheduled backup;
- automatic commit on capture;
- agent-generated commit messages;
- multiple managed remotes;
- branch switching/creation UI;
- merge conflict resolution;
- rebase/cherry-pick;
- force push;
- remote snapshot backends such as Google Drive.

A later non-Git backup backend should be designed from its actual semantics rather than forcing Profile Git v1 behind a premature generic “backup provider” abstraction.
