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

Services are discovered as candidates, not silently adopted. Preview them with
`capture services --dry-run` or `--json capture services --review`, then use
`capture services --review` in a terminal to select specific user services and
optionally decline recommended custom dependencies. A plain `capture services`
updates already-managed Services intent but does not adopt new units or record
an unreviewed removal. External base definitions remain outside the profile.
`restore services --dry-run` previews persistent reconstruction of selected
definitions, unit-specific drop-ins, enablement, and user masks. Authored file
changes are guarded, then systemd refreshes definitions before persistent
state changes. Services defaults to persistent state only; previously active
evidence never grants activation authority by itself. Services never stops,
restarts, or reloads application processes.

Use `--activation persistent` (the default), `--activation working`, or
`--activation review` for this run. Working mode is headless-safe only when
explicitly requested and proposes starts only for captured-active targets whose
saved `activation_preference` is `restore-working-state`. A saved `review`
preference remains unstarted and visibly requires interactive activation review.
Review mode requires an interactive terminal for Apply and asks about eligible
starts individually; `--yes` does not answer those questions. JSON/dry-run
previews list review candidates without granting start authority.

New captures default to `activation_preference = "persistent-only"`. In the
human-readable Services profile, select `restore-working-state` or `review` for
targets you want considered for activation. Persistent-only preferences never
start under either run mode. Every start also requires captured-active evidence,
safe effective topology, parser validation, and compatibility/readiness. Timers,
sockets and paths activate their entry point, not the triggered service directly.
Already-running processes are never restarted. Approved activation is verified
using this run's successful job receipt plus manager state; completed successful
oneshots need not remain active.

Safe keeps conflicting user files; Force can replace a supported user-controlled
conflict with a backup, but cannot acquire an external base, cross linked paths,
or bypass compatibility. Exact removes only explicit managed absence with
matching prior content (and drop-in mode) provenance. Unknown dependencies or
missing provenance withhold removal and remain non-converged if the target is
still present. External bases are validated as dependencies, not copied into
the profile or target. A missing user manager or verifier appears as a
Requirement; satisfy it externally and calculate a new plan.

Existing uninstantiated templates with unresolved topology and broader/alias
drop-in scopes remain Unknown/Reduced. A persistent mask hiding the underlying
base must be resolved externally before changing that base. Masking a managed
definition in the same user configuration path would replace its authored file,
so that effect is withheld. Exact mask/instance removals without prior managed
identity evidence are also withheld. A new owned template can be installed once
with separately configured instance state; template/instance Capture adoption
still needs authoritative source resolution before Services v1 is complete.

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

Each Restore plan shows **Compatibility** before requirements and operations.
For each selected category, **Supported** means provider evidence supports its
selected intent, **Unknown** means a material compatibility fact cannot be
established, and **Incompatible** means the intent is not safely meaningful on
this target. The separate authority effect is **Unchanged** (existing safety
rules still apply), **Reduced** (some work, especially uncertain Exact removal,
is withheld), or **Blocked** (the selected plan cannot apply). A category with
no effective Apply intent is shown as not selected, not Supported. Findings
identify the affected targets and explain the missing or conflicting evidence.

The listed Omarchy environment from the profile is its **last Capture**
context; a partial Capture may leave older target intent untouched, so this is
not a source-version verdict. `restore --dry-run --json` exposes the same
report under `plan.compatibility`, with `profile_last_capture`, `target`, and
deterministically ordered `categories`. Unknown remains explicit in JSON.

Any Blocked finding refuses the entire selected Restore scope before approval
or mutation, even under `--force` (Force only changes conflict handling). You
can explicitly narrow the scope with `restore <category>` or a Restore Skip
policy for a target you intend to leave alone. Otherwise address the finding
outside Blueprint and plan again; changing the environment or a requirement
remediation never silently approves the old plan.

Installing or removing system packages goes through Omarchy, which asks for
administrator authentication with `sudo`. These steps are marked interactive
in the plan and run in your terminal, where `sudo` shows its own prompt;
Blueprint never sees or stores the password.

A freshly installed Omarchy machine has no package database yet. Blueprint can
still check it and show what it would restore, but a plan that installs
packages lists `Requires before applying: … Run: omarchy update` and refuses
to apply until you have run Omarchy's own update. Blueprint never updates the
system for you. `restore <category>` still applies categories that need no
packages.

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
omarchy-blueprint --profile ~/omarchy-profile policy set capture packages preserve official:firefox --scope machine:desktop
omarchy-blueprint --profile ~/omarchy-profile policy set restore packages skip official:firefox --scope machine:desktop
omarchy-blueprint --profile ~/omarchy-profile policy clear capture packages official:firefox --scope machine:desktop
```

`policy set capture <category> <update|preserve> [target]` and
`policy set restore <category> <apply|skip> [target]` set explicit overrides,
at category level when the target is omitted. Preserve keeps previously saved
desired state; Skip leaves it unapplied on the selected machine. Neither
forgets it.
`policy clear <capture|restore> <category> [target]` removes that override so
the setting inherits again. `policy show [category [target]]` reports explicit
rules and effective settings, including their inheritance sources and target
state; `--json` returns semantic `value` fields and source/explicit metadata
for each axis. `--scope profile` selects portable defaults, while
`--scope machine:<name>` selects a named machine without changing its local
binding, including one named `profile` (`--scope machine:profile`). Without
`--scope`, policy uses the selected machine (or
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
a JSON restore never waits for a prompt. Plans with interactive steps (such as
package changes that need `sudo`) are refused before anything is applied when
there is no terminal or `--json` is set; inspect them with `--dry-run --json`,
whose plan carries `interactive` operations and any `requirements`.

## Exit codes

```text
0 success / state matches
1 command, validation, or environment error
2 differences detected by status or diff
```

## Related guides

- [Resources](resources.md) for `track`, `tracked`, `untrack`, and machine path mappings.
- [Guide](guide.md) for the concepts behind capture, restore, and safety.
