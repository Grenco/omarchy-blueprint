# Documentation Comprehension & Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the monolithic implementation-first README with a concise landing page and a small user-guide layer that explains Blueprint’s mental model, TUI, CLI, Resources, and safety using the product terminology shipped by PR #25.

**Architecture:** Keep Markdown-only documentation with no new tooling or dependencies. `README.md` becomes the product landing page, while `docs/guide.md`, `docs/tui.md`, `docs/cli.md`, and `docs/resources.md` each own one user concern; `docs/README.md` routes readers between user and maintainer material. Existing ROADMAP, ADRs, manual test material, and internal planning history remain technical reference rather than onboarding content.

**Tech Stack:** Markdown, existing Go repository, shell/Python one-off validation commands only.

**Spec:** `docs/planning/specs/2026-09-14-documentation-comprehension-design.md`

## Global Constraints

- Documentation-only PR: do not change Go code, schemas, migrations, CLI behavior, capture/restore semantics, or TUI behavior.
- User-facing terminology: `category` instead of `provider`; `Omarchy default` instead of `baseline`; `differences` instead of `drift`; `installed theme` / `installed plugin` instead of `Git theme` / `Git plugin`.
- Do not rename internal identifiers, serialized fields, literal CLI syntax, or historical ADR terminology.
- The running-system mental model is: users configure Omarchy normally; Blueprint captures meaningful intent into a portable, readable, Git-friendly profile.
- Reconstruction preference is: native Omarchy → native package manager → authoritative remote source → captured profile content → manual action.
- Safety story is: inspect → calculate → preview → approve → apply → verify.
- Config policy copy must remain exact:
  - Auto — `Blueprint decides based on Omarchy defaults, ownership, and safety rules.`
  - Included — `You asked Config to manage this path when it is safe to do so.`
  - Excluded — `You asked Config to leave this path alone.`
  - Included never overrides safety rules.
- Leave `ROADMAP.md`, existing `docs/adr/`, `docs/manual-test-checklist.md`, and existing `docs/planning/` history intact except for adding this work’s normal spec/plan files if desired.
- Do not introduce a docs generator, website, linter dependency, or generated CLI reference.

---

### Task 1: Establish the user documentation layer

**Files:**
- Create: `docs/README.md`
- Create: `docs/guide.md`

**Interfaces:**
- Consumes: Current product principles from `ROADMAP.md`; current product presentation rules from PR #25.
- Produces: The user-facing conceptual vocabulary and links that README, TUI, CLI, and Resources docs will reuse.

- [ ] **Step 1: Create `docs/README.md` as an audience router**

Write a short index with exactly two top-level audience sections after the title/introduction:

```markdown
# Omarchy Blueprint documentation

Use the README for the quickest introduction. These guides explain normal Blueprint use without requiring the architecture or implementation history.

## Start here

- [Guide](guide.md) — how Blueprint thinks about capture, profiles, restore, portability, and safety.
- [TUI](tui.md) — interactive navigation and what each screen is for.
- [CLI](cli.md) — common command-line workflows and automation conventions.
- [Resources](resources.md) — explicitly tracked files, folders, Git projects, and per-machine paths.

## Project and contributor reference

- [Roadmap](../ROADMAP.md) — working product specification and longer-term direction.
- [Architecture decisions](adr/) — technical decisions and historical context.
- [Manual test checklist](manual-test-checklist.md) — maintainer acceptance journeys.
- [Contributing](../CONTRIBUTING.md) — repository contribution and verification basics.
```

Do not link `docs/planning/` as user documentation.

- [ ] **Step 2: Write the conceptual skeleton of `docs/guide.md`**

Use these headings in this order:

```markdown
# Guide
## The running system is the configuration editor
## From system to profile to another machine
## Reconstruct first, copy only when needed
## What Blueprint remembers
## Portable and machine-specific state
## State, policy, and restore intent
## Profiles are readable and Git-friendly
## Safety model
## Current boundaries
```

- [ ] **Step 3: Fill the first three sections with the product mental model**

Include this workflow, in equivalent wording:

```text
Use Omarchy normally
        ↓
Capture
        ↓
Portable Blueprint profile
        ↓
Git / backup
        ↓
Another Omarchy machine
        ↓
Preview and approve restore
        ↓
Equivalent personalised environment
```

Explain that Blueprint captures intent rather than requiring a hand-authored declarative system configuration.

Document the reconstruction preference as an ordered list:

1. Native Omarchy operation
2. Native package manager
3. Authoritative remote source
4. Captured profile content
5. Manual action when safe automation is unavailable

- [ ] **Step 4: Add the category overview**

Describe these user-facing categories with one or two sentences each:

- Packages
- Themes
- Plugins
- Defaults
- Config
- Shell
- Hooks
- Resources

Explain separately that Machines provide per-machine Resource placement and Sync manages the Git repository containing the Blueprint profile.

Do not use `provider` as the umbrella noun.

- [ ] **Step 5: Explain state, policy, profile, and safety**

State explicitly that:

- State is what Blueprint observed.
- Policy is whether Blueprint should manage an item/path.
- Restore intent is what the selected restore plan would do.

Explain that profiles are readable and suitable for Git, but generated by Blueprint rather than primarily authored by hand.

Use the safety sequence:

```text
inspect → calculate → preview → approve → apply → verify
```

Include the product-level boundaries: no silent deletion, no blind whole-machine snapshot, no intentional secret capture, and conflicts are not silently overwritten merely because the profile differs.

- [ ] **Step 6: Add a short representative profile tree**

Keep this illustrative rather than exhaustive, for example:

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
```

Do not reproduce the full current README tree unless a path is necessary to explain a user workflow.

- [ ] **Step 7: Verify Task 1 terminology and links**

Run:

```bash
git grep -nE '\b(provider|baseline|drift)\b' -- docs/README.md docs/guide.md || true
```

Expected: no user-facing matches. If a literal/internal reference is genuinely needed, rewrite the sentence to introduce the user concept first or justify the exception in the PR description.

Run:

```bash
git diff --check -- docs/README.md docs/guide.md
```

Expected: no output.

- [ ] **Step 8: Commit Task 1**

```bash
git add docs/README.md docs/guide.md
git commit -m "docs: add user guide foundation"
```

---

### Task 2: Document the TUI using shipped product copy

**Files:**
- Create: `docs/tui.md`
- Reference only: `internal/tui/catalog.go`
- Reference only: `internal/tui/screens/config.go`

**Interfaces:**
- Consumes: Screen labels/short/long descriptions from `internal/tui/catalog.go`; Config group and policy presentation from `internal/tui/screens/config.go`.
- Produces: The canonical long-form user explanation of interactive Blueprint use.

- [ ] **Step 1: Create the TUI guide structure**

Use these headings:

```markdown
# TUI guide
## Launching the TUI
## Navigation
## Command palette and help
## Workflow
### Overview
### Capture
### Restore
## Software
### Packages
### Themes
### Plugins
### Defaults
## Customisation
### Config
### Shell
### Hooks
### Resources
## Profile
### Machines
### Sync
## Config states and policies
## Small terminals and NO_COLOR
```

- [ ] **Step 2: Write launch and navigation guidance**

Document:

```sh
omarchy-blueprint
omarchy-blueprint tui
omarchy-blueprint --profile ~/omarchy-profile
```

Explain that bare interactive invocation opens the TUI, while non-interactive invocation prints normal CLI help.

Document stable navigation concepts without building an exhaustive key table:

- `j` / `k` move through normal lists/navigation;
- `Tab` moves focus where relevant;
- `:` opens command search;
- `?` opens searchable contextual help;
- `[` / `]` jump between sidebar sections or screen groups where supported;
- `Esc` closes overlays/transient views.

Tell users that `?` is the canonical source for screen-specific actions.

- [ ] **Step 3: Describe every screen from the current catalogue**

Read `internal/tui/catalog.go` before writing. Preserve its meaning and presentation vocabulary.

Do not invent alternate screen purposes. In particular:

- Overview is the starting decision inbox.
- Capture updates the profile from the current system.
- Restore previews and applies what the profile would recreate.
- Machines changes per-machine Resource locations, not Resource content.
- Sync manages profile Git, not application or Resource repositories.

Keep each screen explanation concise: purpose first, then one paragraph of what a user does there.

- [ ] **Step 4: Document Config groups and policies exactly**

List the groups:

```text
Needs review
Changes
No action needed
Not managed by Config
```

Then document:

```markdown
- **Auto** — Blueprint decides based on Omarchy defaults, ownership, and safety rules.
- **Included** — You asked Config to manage this path when it is safe to do so.
- **Excluded** — You asked Config to leave this path alone.
```

Add this sentence verbatim:

```text
Included never overrides safety rules.
```

Explain “Omarchy default” rather than “baseline” in user prose.

- [ ] **Step 5: Document terminal presentation behavior**

State that compact layouts preserve the same semantics with less simultaneous detail and that `NO_COLOR=1` disables colour without making warning/diff/selection meaning depend on colour alone.

- [ ] **Step 6: Verify the TUI guide against source copy**

Run:

```bash
git grep -nE '\b(provider|baseline)\b' -- docs/tui.md || true
```

Expected: no user-facing matches.

Run:

```bash
grep -F "Blueprint decides based on Omarchy defaults, ownership, and safety rules." docs/tui.md
grep -F "You asked Config to manage this path when it is safe to do so." docs/tui.md
grep -F "You asked Config to leave this path alone." docs/tui.md
grep -F "Included never overrides safety rules." docs/tui.md
```

Expected: all four commands find exactly the intended documentation text.

- [ ] **Step 7: Commit Task 2**

```bash
git add docs/tui.md
git commit -m "docs: explain the interactive tui"
```

---

### Task 3: Document common CLI workflows without duplicating `--help`

**Files:**
- Create: `docs/cli.md`
- Reference: current `README.md` command examples
- Reference: current CLI help/output as needed

**Interfaces:**
- Consumes: Existing public CLI surface only.
- Produces: Workflow-level CLI guidance; complete option syntax remains owned by CLI help.

- [ ] **Step 1: Create the CLI guide structure**

Use:

```markdown
# CLI guide

`omarchy-blueprint --help` and subcommand help are the canonical reference for complete options and flags. This guide focuses on common workflows.

## Create or select a profile
## Capture
## Check, status, and diff
## Preview and apply restore
## Target one category
## Include and exclude policy
## Profile Git workflow
## Non-interactive and JSON use
## Exit codes
```

- [ ] **Step 2: Add one primary lifecycle example**

Use a concise workflow such as:

```sh
omarchy-blueprint init ~/omarchy-profile --name main
omarchy-blueprint --profile ~/omarchy-profile capture
omarchy-blueprint --profile ~/omarchy-profile status
omarchy-blueprint --profile ~/omarchy-profile restore --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore
```

Explain what each phase is for rather than listing every category variant.

- [ ] **Step 3: Explain category targeting as a pattern**

Show only representative examples:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture themes
omarchy-blueprint --profile ~/omarchy-profile status config
omarchy-blueprint --profile ~/omarchy-profile restore shell --dry-run
```

State that category-less lifecycle commands operate on the applicable captured categories.

Do not recreate separate four-command blocks for every category.

- [ ] **Step 4: Move include/exclude, Git sync, automation, and exit-code guidance into this guide**

Include representative policy commands such as:

```sh
omarchy-blueprint --profile ~/omarchy-profile exclude package:dislocker-git
omarchy-blueprint --profile ~/omarchy-profile include aur:dislocker-git
```

Include the safe profile Git workflow currently documented in README, but keep advanced Git work delegated to Git/LazyGit.

Document the non-interactive restore form and JSON behavior using the currently supported syntax.

Document exit codes:

```text
0 success / state matches
1 command, validation, or environment error
2 differences detected by status or diff
```

Use “differences” in prose rather than “drift”.

- [ ] **Step 5: Verify CLI examples are public syntax, not inferred syntax**

Compare every command block against current CLI help or existing tested README examples. Remove any example whose current support cannot be verified.

Run:

```bash
git diff --check -- docs/cli.md
```

Expected: no output.

- [ ] **Step 6: Commit Task 3**

```bash
git add docs/cli.md
git commit -m "docs: document common cli workflows"
```

---

### Task 4: Move Resources and machine-path detail into a focused guide

**Files:**
- Create: `docs/resources.md`
- Reference: current `README.md` Resources sections

**Interfaces:**
- Consumes: Existing Resources and Machines public behavior.
- Produces: The canonical user guide for explicitly tracked files/folders/Git worktrees and machine path mappings.

- [ ] **Step 1: Create the Resources guide structure**

Use:

```markdown
# Resources
## When to use Resources
## Track and untrack
## Git strategy
## Git plus local changes
## Copy strategy
## Symlink adoption
## Conflicts and restore behavior
## Per-machine Resource paths
## Safety boundaries
```

- [ ] **Step 2: Explain when Resources are appropriate**

Explain that Resources are for files, folders, and Git projects users explicitly want Blueprint to carry when they do not belong to a normal Blueprint category.

Use the product distinction: Config is selective configuration discovery; Resources are explicit authoritative trees/files/projects.

- [ ] **Step 3: Preserve the existing strategy semantics**

Document representative commands:

```sh
omarchy-blueprint --profile ~/omarchy-profile track ~/dotfiles --strategy git
omarchy-blueprint --profile ~/omarchy-profile track ~/dev/hacky-repo --strategy git+diff --include-untracked notes.md
omarchy-blueprint --profile ~/omarchy-profile track ~/some-local-tree --strategy copy
omarchy-blueprint --profile ~/omarchy-profile tracked
omarchy-blueprint --profile ~/omarchy-profile untrack dotfiles
```

Explain:

- `git`: remote + exact revision, not arbitrary local dirty state;
- `git+diff`: tracked local changes plus only explicitly selected untracked files;
- `copy`: filesystem content, excluding Git administration data for worktrees.

Preserve the additive/conflict-safe restore behavior.

- [ ] **Step 4: Move machine path mapping guidance here**

Include representative commands from the current README and explain that mappings alter placement for a selected machine without changing the portable Resource definition or saved bytes.

Do not duplicate the same explanation in `docs/guide.md`; link here from the guide instead.

- [ ] **Step 5: Verify terminology and safety language**

Run:

```bash
git grep -nE '\b(provider|baseline|drift)\b' -- docs/resources.md || true
git diff --check -- docs/resources.md
```

Expected: no user-facing terminology regressions and no whitespace errors.

- [ ] **Step 6: Commit Task 4**

```bash
git add docs/resources.md
git commit -m "docs: move resource workflows into a focused guide"
```

---

### Task 5: Rewrite README as the product landing page

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: Completed user guides from Tasks 1–4.
- Produces: The primary repository landing page and entry point into all user documentation.

- [ ] **Step 1: Replace the implementation-first opening**

Keep the title and a concise tagline built around this meaning:

```text
Use Omarchy normally. Blueprint remembers the meaningful choices needed to rebuild your setup.
```

Within the first few paragraphs explain:

- the running system is the configuration editor;
- Blueprint captures meaningful intent into a readable, portable profile;
- the profile can be version-controlled and restored elsewhere;
- Blueprint prefers native reconstruction over blind snapshots.

Do not lead with individual implementation details for Packages, Themes, Plugins, Config, Defaults, Shell, or widgets.

- [ ] **Step 2: Put Quick start near the top**

Retain the existing supported create/capture/check/status/restore flow, but keep it compact.

Include a short TUI path:

```sh
omarchy-blueprint
```

and a short CLI path. Link to `docs/tui.md` and `docs/cli.md` for detail.

- [ ] **Step 3: Add a concise “What Blueprint can remember” section**

Use a compact table or short bullets covering:

- Packages
- Themes
- Plugins
- Defaults
- Config
- Shell
- Hooks
- Resources

Add one brief note that Machines handles Resource placement and Sync handles profile Git.

Do not describe provider internals or reproduce implementation-specific file formats here.

- [ ] **Step 4: Replace the long safety list with the product safety model**

Explain:

```text
inspect → calculate → preview → approve → apply → verify
```

Keep a short set of high-value guarantees/constraints:

- restore previews are non-mutating;
- apply requires approval or explicit non-interactive confirmation;
- additional packages are not silently removed;
- conflicts/user work are not silently overwritten;
- sensitive state is not something Blueprint intentionally snapshots into the profile.

Link to `docs/guide.md` and specialized guides for detail.

- [ ] **Step 5: Remove/migrate detail now owned elsewhere**

Delete from README after confirming the destination guide contains the useful information:

- long TUI navigation/detail paragraph;
- full profile tree;
- full Profile Git command sequence;
- Portable Resources and Machine Resource Paths sections;
- package exclusion deep dive;
- repeated Theme/Config/Defaults/Shell/Hooks lifecycle blocks;
- exit-code reference;
- long subsystem-specific Safety checklist.

Do not simply copy all removed prose into one new giant guide. Rewrite each destination around its user question.

- [ ] **Step 6: Add a Documentation section**

Link to:

```markdown
- [Guide](docs/guide.md)
- [TUI](docs/tui.md)
- [CLI](docs/cli.md)
- [Resources](docs/resources.md)
- [All documentation](docs/README.md)
```

Keep ROADMAP available as project direction/reference, not as the next step for a normal user.

- [ ] **Step 7: Keep requirements and source verification concise**

Preserve current supported requirements and:

```sh
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Avoid presenting build/test instructions before the product and first workflow.

- [ ] **Step 8: Check README size and vocabulary**

Run:

```bash
wc -c README.md
```

Target: `<= 10240` bytes. If slightly above, prefer clarity over gaming the number, but any large overage requires reviewer justification.

Run:

```bash
git grep -nE '\b(provider|baseline|drift)\b' -- README.md || true
```

Expected: no user-facing matches. Literal/internal references require deliberate justification.

Run:

```bash
git diff --check -- README.md
```

Expected: no output.

- [ ] **Step 9: Commit Task 5**

```bash
git add README.md
git commit -m "docs: simplify the project readme"
```

---

### Task 6: Run the consistency sweep and final verification

**Files:**
- Modify: `CONTRIBUTING.md`
- Review: `README.md`
- Review: `docs/README.md`
- Review: `docs/guide.md`
- Review: `docs/tui.md`
- Review: `docs/cli.md`
- Review: `docs/resources.md`

**Interfaces:**
- Consumes: All preceding documentation tasks.
- Produces: A coherent docs-only PR ready for normal review.

- [ ] **Step 1: Fix the branch-name inconsistency**

In `CONTRIBUTING.md`, change:

```text
Create a focused branch from `master`
```

 to:

```text
Create a focused branch from `main`
```

Do not expand this into a contributor-guide redesign.

- [ ] **Step 2: Run a user-facing terminology sweep**

Run:

```bash
git grep -nE '\b(provider|baseline|drift)\b' -- README.md docs/README.md docs/guide.md docs/tui.md docs/cli.md docs/resources.md || true
```

Review every match manually. The expected outcome is zero user-facing uses. Do not alter historical ADRs, ROADMAP, manual tests, internal planning docs, source identifiers, or literal program output merely to make this grep globally clean.

Also search presentation terms:

```bash
git grep -nE 'Git theme|Git plugin' -- README.md docs/README.md docs/guide.md docs/tui.md docs/cli.md docs/resources.md || true
```

Expected: no user-facing matches; use `installed theme` / `installed plugin` where applicable.

- [ ] **Step 3: Validate local Markdown links**

Run from the repository root:

```bash
python - <<'PY'
from pathlib import Path
import re

files = [
    Path('README.md'),
    Path('docs/README.md'),
    Path('docs/guide.md'),
    Path('docs/tui.md'),
    Path('docs/cli.md'),
    Path('docs/resources.md'),
    Path('CONTRIBUTING.md'),
]
pattern = re.compile(r'\[[^\]]+\]\(([^)]+)\)')
errors = []
for file in files:
    text = file.read_text()
    for target in pattern.findall(text):
        if target.startswith(('http://', 'https://', '#', 'mailto:')):
            continue
        path = target.split('#', 1)[0]
        if not path:
            continue
        resolved = (file.parent / path).resolve()
        if not resolved.exists():
            errors.append(f'{file}: missing {target}')
if errors:
    raise SystemExit('\n'.join(errors))
print('local markdown links: ok')
PY
```

Expected:

```text
local markdown links: ok
```

- [ ] **Step 4: Perform a comprehension review, not only a spellcheck**

Without opening ROADMAP or ADRs, answer from README + user docs:

1. What is Blueprint?
2. What should a user do after intentionally changing their Omarchy setup?
3. What is reconstructed rather than copied?
4. What happens before restore mutates anything?
5. Where does a user learn TUI actions, CLI automation, or Resources strategies?

If any answer requires architecture knowledge or implementation inference, revise the user docs.

- [ ] **Step 5: Check the PR remains docs-only**

Run:

```bash
git diff --name-only origin/main...HEAD
```

Expected implementation scope:

```text
README.md
CONTRIBUTING.md
docs/README.md
docs/guide.md
docs/tui.md
docs/cli.md
docs/resources.md
```

The normal spec/plan files under `docs/planning/` may also appear if the project commits planning artifacts. No Go/source/schema file should appear.

- [ ] **Step 6: Run repository verification**

Run:

```bash
git diff --check
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: all commands exit successfully.

- [ ] **Step 7: Review the final diff for duplication**

Read the full documentation diff and specifically reject:

- the same explanation repeated in README and a guide;
- README regrowing subsystem implementation detail;
- exhaustive CLI flag tables that duplicate `--help`;
- exhaustive TUI key tables that duplicate contextual `?` help;
- Resources detail leaking back into README;
- historical/internal documents rewritten only for presentation vocabulary.

- [ ] **Step 8: Commit the consistency pass**

```bash
git add README.md CONTRIBUTING.md docs/README.md docs/guide.md docs/tui.md docs/cli.md docs/resources.md
git commit -m "docs: align terminology and navigation"
```

## Final review checklist

Before opening the PR, confirm:

- [ ] README is a landing page, not a feature log.
- [ ] Quick start is near the top.
- [ ] The running-system-as-editor mental model is explicit.
- [ ] Reconstruction preference is explained without provider internals.
- [ ] Restore safety is explained as preview/approval workflow.
- [ ] TUI guide follows `internal/tui/catalog.go` presentation language.
- [ ] Config policies use exact approved wording and state that Included never overrides safety.
- [ ] CLI guide points to `--help` for exhaustive syntax.
- [ ] Resources and machine paths remain documented after leaving README.
- [ ] No behavior, schema, or internal identifier changes are mixed into the PR.
- [ ] `CONTRIBUTING.md` says `main`.
- [ ] Links resolve and repository verification passes.

## Suggested PR title

```text
docs: simplify README and add user guides
```

## Suggested PR summary

```markdown
## Summary
- make README a concise product landing page with the core Blueprint mental model and quick start
- add focused guides for concepts, TUI, CLI, and Resources while keeping architecture/maintainer docs separate
- align user-facing documentation with the terminology and Config safety language shipped by the TUI comprehension work

## Verification
- local Markdown links resolve
- git diff --check
- go test ./... -count=1
- go vet ./...
- go build ./cmd/omarchy-blueprint
```
