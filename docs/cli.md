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
`capture` updates every supported category, with each category applying its
own capture rules. Config saves meaningful customisations relative to
Omarchy defaults rather than copying unchanged defaults.

Preview candidates (including targets that have never been captured), policy
preserves, and safety blocks without writing the profile:

```sh
omarchy-blueprint --profile ~/omarchy-profile capture --dry-run
omarchy-blueprint --profile ~/omarchy-profile capture packages --review
```

`--review` asks for approval in a terminal, then re-inspects before and after
staging. If the approved outcome or captured value has changed, Capture stops
without saving and asks you to review again. `--json capture --review` emits
the preview only; it never prompts or applies. Plain `capture` remains useful
for scripts that intentionally capture immediately.

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
approval before applying it. `--conflicts safe|force` and
`--convergence additive|exact` override the selected machine's defaults for
one run, independently. `--force` and `--exact` are shorthands for Force and
Exact. Exact only removes explicitly undesired state when its owning provider
can do so safely; it never deletes Resource data.

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

These are *management* actions: package exclusion forgets its saved desired
state. To preserve saved state while preventing one machine from updating it,
or to skip restoring it on that machine, use Capture/Restore policy instead:

```sh
omarchy-blueprint --profile ~/omarchy-profile policy show packages official:firefox --scope profile
omarchy-blueprint --profile ~/omarchy-profile policy set capture packages disabled official:firefox --scope desktop
omarchy-blueprint --profile ~/omarchy-profile policy set restore packages disabled official:firefox --scope desktop
omarchy-blueprint --profile ~/omarchy-profile policy clear capture packages official:firefox --scope desktop
```

`policy set <capture|restore> <category> <enabled|disabled> [target]` sets
an explicit override, at category level when the target is omitted.
`policy clear <capture|restore> <category> [target]` removes that override so
the setting inherits again. `policy show [category [target]]` reports explicit
rules and effective settings, including their inheritance sources and target
state; `--json` returns structured data. `--scope profile` selects portable
defaults, while `--scope <machine>` selects a named machine without changing
its local binding. Without `--scope`, policy uses the selected machine (or
profile defaults when none is selected). Target keys are shown by `policy show`
and in the Capture preview: for example `themes active`, `themes theme:<id>`,
`defaults browser`, `shell state`, and `resources resource:<id>`.

To forget a managed target and its target-specific overrides rather than
preserving it or remembering its absence, use
`policy stop-managing <category> <target>`. Providers that do not support this action reject it with an
explanation; Config management and Resource untracking have their own verbs.

Config paths have an equivalent policy, set from the TUI's Config screen or
with `omarchy-blueprint include config:<path>` / `omarchy-blueprint auto
config:<path>`; see the [TUI guide](tui.md#config-states-and-policies) for
the exact policy wording.

## Machine Restore defaults

```sh
omarchy-blueprint --profile ~/omarchy-profile machine restore-defaults desktop
omarchy-blueprint --profile ~/omarchy-profile machine restore-defaults set desktop --conflicts force
omarchy-blueprint --profile ~/omarchy-profile machine restore-defaults set desktop --convergence exact
```

These persisted defaults affect Restore planning for that machine. A partial
edit retains the other axis. With no name, the read command uses the active
machine; setting defaults always requires a name. One-run `restore` flags do
not change these saved defaults.

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
