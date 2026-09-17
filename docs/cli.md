# CLI guide

`omarchy-blueprint --help` and subcommand help are the canonical reference
for complete options and flags. This guide focuses on common workflows.

## Create or select a profile

```sh
mkdir -p ~/omarchy-profile
omarchy-blueprint init ~/omarchy-profile --name main
```

Most other commands take `--profile <path>` to select which profile to use;
it defaults to the current directory.

## Capture

```sh
omarchy-blueprint --profile ~/omarchy-profile capture
```

Capture reads the current system and updates the profile. A category-less
`capture` captures every supported category; only content that differs from
the Omarchy default is written, so clean defaults stay out of the profile.

## Check, status, and diff

```sh
omarchy-blueprint --profile ~/omarchy-profile check
omarchy-blueprint --profile ~/omarchy-profile status
omarchy-blueprint --profile ~/omarchy-profile diff
```

`check` validates the profile and environment. `status` reports whether this
machine matches the profile; `diff` shows the semantic differences behind
that status. Category-less `status` and `diff` operate on every captured
category.

## Preview and apply restore

```sh
omarchy-blueprint --profile ~/omarchy-profile restore --dry-run
omarchy-blueprint --profile ~/omarchy-profile restore
```

`restore --dry-run` calculates and prints the plan without changing
anything. Plain `restore` shows the same plan and asks for interactive
approval before applying it. Add `--force` to resolve supported conflicts in
favor of the profile instead of leaving them for manual review.

## Target one category

Most lifecycle commands accept an optional category argument to operate on
just that part of the profile instead of everything:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture themes
omarchy-blueprint --profile ~/omarchy-profile status config
omarchy-blueprint --profile ~/omarchy-profile restore shell --dry-run
```

The same pattern works for `capture`, `status`, `diff`, and `restore` across
every category — `packages`, `themes`, `plugins`, `resources`, `config`,
`defaults`, `shell`, and `hooks`.

## Include and exclude policy

```sh
omarchy-blueprint --profile ~/omarchy-profile exclude package:dislocker-git
omarchy-blueprint --profile ~/omarchy-profile include aur:dislocker-git
```

`exclude` keeps a package out of capture, differences, and restore; the
exclusion persists across future captures. `include` puts a previously
excluded package back under management. Package references can be given as
`package:<name>`, or explicitly as `official:<name>` / `aur:<name>`.

Config paths have an equivalent policy, set from the TUI's Config screen or
with `omarchy-blueprint include config:<path>` / `omarchy-blueprint auto
config:<path>`; see the [TUI guide](tui.md#config-states-and-policies) for
the exact policy wording.

## Profile Git workflow

Blueprint can manage the simple capture-to-commit-to-push workflow for its
own profile files:

```sh
omarchy-blueprint --profile ~/omarchy-profile profile git init
omarchy-blueprint --profile ~/omarchy-profile profile git remote set <remote>
omarchy-blueprint --profile ~/omarchy-profile profile git status
omarchy-blueprint --profile ~/omarchy-profile profile git diff
omarchy-blueprint --profile ~/omarchy-profile profile git commit
omarchy-blueprint --profile ~/omarchy-profile profile git push
```

Only Blueprint-managed profile paths are staged and committed; unrelated
files in the same repository are left alone. Status never fetches
implicitly, `profile git pull` is fast-forward-only and requires a clean
repository, and existing staged work blocks a Blueprint commit. Use Git or
LazyGit directly for conflicts, rebases, history editing, interactive
staging, or other advanced repository work.

## Non-interactive and JSON use

```sh
omarchy-blueprint --profile ~/omarchy-profile restore --yes
omarchy-blueprint --profile ~/omarchy-profile status --json
omarchy-blueprint --profile ~/omarchy-profile restore --dry-run --json
```

`--yes` approves a restore without an interactive prompt. `--json` emits
machine-readable output and can be combined with `--dry-run` or `--yes`;
a JSON restore never waits for a prompt.

## Exit codes

```text
0 success / state matches
1 command, validation, or environment error
2 differences detected by status or diff
```

## Related guides

- [Resources](resources.md) for `track`, `tracked`, `untrack`, and machine path mappings.
- [Guide](guide.md) for the concepts behind capture, restore, and safety.
