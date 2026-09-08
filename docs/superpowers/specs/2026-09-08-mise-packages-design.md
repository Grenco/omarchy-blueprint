# Mise Packages Design

**Status:** Approved design  
**Date:** 2026-09-08  
**Project:** Omarchy Blueprint  
**Milestone:** Schema 6 — Mise as a third Packages source

## 1. Summary

Omarchy Blueprint will treat globally configured Mise tools as a third package source inside the existing `packages` provider:

```text
Packages
├── official / pacman
├── AUR
└── Mise
```

This is deliberately **not** a new top-level `tools` provider. The existing Packages provider already owns portable installation intent and provenance, while the Defaults provider only selects among available applications. Mise belongs to Packages for the same reason Pacman and AUR do: it describes software the user deliberately configured and expects Blueprint to reconstruct.

The Mise source differs from Pacman/AUR in representation. Official and AUR packages are adequately represented by package names. Mise package declarations can contain version requests, multiple versions, backend-qualified identifiers, install options, dependencies, installation environment, and `postinstall` commands. Blueprint therefore stores Mise state as structured TOML in `packages/mise.toml` instead of a line-oriented text file.

The central rule is:

> **Capture the user's declared global Mise tool intent, not the concrete versions currently resolved in Mise's install/cache directories.**

For example, if the source machine contains:

```toml
[tools]
node = "24"
python = ["3.12", "3.13"]
"npm:@anthropic-ai/claude-code" = "latest"
foo = { version = "2", postinstall = "foo setup" }
```

Blueprint preserves those requests semantically. It does not silently turn `node = "24"` into a resolved `24.x.y` pin, and it does not discard valid tool options such as `postinstall`.

## 2. Why Packages Own Mise

The existing Packages provider already owns these concepts:

- explicit software installation intent;
- package provenance;
- capture, drift, restore, and verification;
- additive restore;
- no automatic removal of target-only packages;
- profile include/exclude policy.

The Defaults provider now follows the complementary rule:

> Defaults selects applications; Packages owns installation.

A separate Tools provider would split one semantic concern across two top-level categories and make cross-provider restore ordering unnecessarily complicated. Keeping Mise under Packages also preserves the existing user-facing commands:

```bash
omarchy-blueprint capture packages
omarchy-blueprint status packages
omarchy-blueprint diff packages
omarchy-blueprint restore packages
```

No new `tools` category is introduced.

## 3. Goals

This milestone must:

1. Capture user-global Mise `[tools]` declarations alongside Pacman/AUR package state.
2. Include tools added by the user, not only Omarchy's preinstalled agent/tool wrappers.
3. Preserve version requests as configured (`latest`, partial versions, exact pins, arrays).
4. Preserve valid structured Mise tool options, including nested values and `postinstall`.
5. Restore missing Mise declarations without overwriting unrelated Mise configuration.
6. Install newly restored declarations through Mise's native installer.
7. Preserve target-only Mise tools.
8. Treat a same-ID, different declaration as user drift and skip it rather than overwrite it.
9. Support package include/exclude policy for `mise:<tool-id>`.
10. Keep project-local Mise configuration outside this provider.
11. Keep global Mise `[env]`, `[settings]`, tasks, and unrelated sections outside Packages.
12. Avoid capturing Mise install caches or resolved versions as desired state.
13. Maintain schema/backward compatibility with existing profiles.
14. Preserve the project's existing secret-exclusion and restore-safety principles.

## 4. Non-goals

This milestone does **not**:

- capture project-local `mise.toml`, `.mise.toml`, `.tool-versions`, or project lockfiles;
- reproduce Mise's cache or `~/.local/share/mise/installs`;
- pin a floating request to the resolved version observed during capture;
- capture global `[env]`, `[settings]`, `[tasks]`, `[tool_alias]`, or other non-`[tools]` sections;
- capture arbitrary generated wrappers in `~/.local/bin`;
- reproduce custom command aliases that exist only because of an external wrapper;
- remove target-only Mise tools during restore;
- overwrite a target Mise declaration that differs from the saved declaration;
- make `--force` apply to Mise conflicts in this milestone;
- become a general-purpose Mise configuration synchronizer.

Omarchy's standard `omarchy-mise-install` wrappers are expected to be present on a normal fresh Omarchy install. A custom wrapper whose alias cannot be reconstructed from `[tools]` alone remains future Portable Resources / wrapper-metadata work.

## 5. Upstream Mise Semantics We Rely On

The design deliberately follows Mise's public configuration model:

- Global configuration normally lives at `~/.config/mise/config.toml`.
- `MISE_GLOBAL_CONFIG_FILE` can override the global file.
- `MISE_CONFIG_DIR`, then `XDG_CONFIG_HOME`, affect the default global config directory.
- `mise use --global` installs a tool and records its version request in global configuration.
- `mise install` installs configured tools without changing the declaration.
- A request such as `"24"` remains a floating request within that series; it should not be replaced by a resolved concrete version unless the user explicitly pinned it.
- `[tools]` entries may be strings, arrays, or tables.
- Tool tables may contain `postinstall`, `install_env`, `depends`, `os`, and backend-specific options.

Current upstream references:

- https://mise.jdx.dev/configuration.html
- https://mise.jdx.dev/cli/use.html
- https://mise.jdx.dev/cli/install.html
- https://mise.jdx.dev/dev-tools/
- https://mise.jdx.dev/directories.html

## 6. Scope of "Global" in Schema 6

Schema 6 captures the `[tools]` subtree from the **primary user-global Mise config file**, i.e. the file targeted by `mise use --global`.

The path resolves in this order:

1. `MISE_GLOBAL_CONFIG_FILE`, if non-empty.
2. `${MISE_CONFIG_DIR}/config.toml`, if `MISE_CONFIG_DIR` is non-empty.
3. `${XDG_CONFIG_HOME}/mise/config.toml`, if `XDG_CONFIG_HOME` is non-empty.
4. `$HOME/.config/mise/config.toml`.

This deliberately does not merge `conf.d`, environment-specific global fragments, or `config.local.toml` in the first implementation. Those files participate in Mise's broader global configuration hierarchy, but emulating their precedence correctly would substantially expand the milestone and make source provenance ambiguous.

This limitation is explicit: Schema 6 captures the primary global `[tools]` file that normal `mise use -g` and Omarchy's `omarchy-mise-install` use. Effective multi-file global Mise state can be added later as a separate design if real-world profiles require it.

## 7. Schema and Profile Storage

### 7.1 Schema bump

Profile schema increases from 5 to 6:

```go
const Schema = 6

const (
    configSchema       = 2
    defaultsSchema     = 3
    shellSchema        = 4
    hooksSchema        = 5
    misePackagesSchema = 6
)
```

Schema 5 and older profiles load with empty Mise state.

### 7.2 Profile model

Add structured Mise state to `profile.Packages`:

```go
type MiseTool map[string]any
type MiseTools map[string]MiseTool

type Packages struct {
    Official        []string  `json:"official"`
    AUR             []string  `json:"aur"`
    Mise            MiseTools `json:"mise,omitempty" toml:"-"`
    MachineSpecific []string  `json:"machine_specific,omitempty"`
    Excluded        []string  `json:"excluded,omitempty"`
    Installed       []string  `json:"-" toml:"-"`
}
```

`Installed` continues to mean Pacman-installed package names used to satisfy official/AUR intent. Mise IDs are **not** added to that equivalence set.

### 7.3 `packages/mise.toml`

Mise state is stored separately:

```text
packages/
├── official.txt
├── aur.txt
├── mise.toml
├── machine-specific.txt
└── excluded.txt
```

Profile representation canonicalizes every tool to table form:

```toml
[tools.node]
version = "24"

[tools.python]
version = ["3.12", "3.13"]

[tools."npm:@anthropic-ai/claude-code"]
version = "latest"

[tools.foo]
version = "2"
postinstall = "foo setup"
```

Nested tool options remain nested:

```toml
[tools."http:example/tool"]
version = "1.0.0"

[tools."http:example/tool".platforms.linux-x64]
url = "https://example.invalid/tool.tar.gz"
```

This representation is semantic, not byte-for-byte source capture. Source forms that Mise treats equivalently are normalized so they compare cleanly.

## 8. Tool Declaration Normalization

Add a focused Mise helper in the Packages provider, for example:

```text
internal/providers/packages/mise.go
internal/providers/packages/mise_test.go
```

Core interfaces:

```go
func ResolveMiseGlobalConfigPath() (string, error)

func ReadMiseTools(path string) (profile.MiseTools, error)

func NormalizeMiseTools(raw map[string]any) (profile.MiseTools, error)

func NormalizeMiseTool(id string, raw any) (profile.MiseTool, error)

func EqualMiseTool(left, right profile.MiseTool) bool

func EncodeMiseTools(tools profile.MiseTools) ([]byte, error)

func SummarizeMiseTool(tool profile.MiseTool) string
```

Normalization rules:

### 8.1 String declaration

Source:

```toml
[tools]
node = "24"
```

Normalized:

```toml
[tools.node]
version = "24"
```

### 8.2 Array declaration

Source:

```toml
[tools]
python = ["3.12", "3.13"]
```

Normalized:

```toml
[tools.python]
version = ["3.12", "3.13"]
```

### 8.3 Table declaration

Source:

```toml
[tools]
foo = { version = "2", postinstall = "foo setup" }
```

Normalized recursively while retaining all fields:

```toml
[tools.foo]
version = "2"
postinstall = "foo setup"
```

### 8.4 Omitted `version`

If a valid tool table omits `version`, normalize it to:

```toml
version = "latest"
```

This matches Mise's default version-request behavior and prevents a syntactic omission from creating false drift against an explicit `"latest"` declaration.

### 8.5 Values

The normalizer accepts the TOML value shapes used by current Mise tool declarations:

- strings;
- booleans;
- signed integers;
- floats;
- arrays;
- nested maps/tables.

It recursively deep-copies maps and arrays so source parse objects are never retained by reference.

Unsupported or non-TOML values fail capture/check rather than being silently dropped.

### 8.6 IDs

Mise tool IDs may legitimately contain punctuation that Pacman package names do not, for example:

```text
npm:@anthropic-ai/claude-code
github:can1357/oh-my-pi
aqua:modem-dev/hunk
```

A Mise tool ID must:

- be non-empty after trimming;
- contain no ASCII control characters;
- contain no whitespace.

Colons, slashes, `@`, dots, hyphens, and other printable backend punctuation are allowed.

## 9. Capture

`packages.Provider.Detect` continues to detect Pacman state and additionally reads the resolved primary global Mise config file.

Pseudo-flow:

```text
pacman -Qqen
pacman -Qqem
pacman -Qq

resolve global Mise config path
        ↓
missing file → empty Mise set
        ↓
read regular file
        ↓
parse TOML
        ↓
extract only [tools]
        ↓
normalize declarations
        ↓
apply package exclusions
        ↓
return profile.Packages
```

Capture never reads Mise's installed-tool directory to derive desired versions.

A missing global config or missing `[tools]` table is valid and produces an empty Mise set.

Malformed TOML is an error. Blueprint must not guess what the user's tool state means.

### 9.1 Symlinked source config

Capture may follow a symlinked global Mise config **for semantic read-only detection**. This is different from raw file ownership: Blueprint is not copying the target file or later claiming ownership of its path. It extracts only the `[tools]` declarations.

Restore remains stricter: it must not mutate through a symlinked global config or symlinked parent directory.

## 10. Sensitive Values

The profile is intended for Git, so a Mise declaration must not become a new route for credentials into the repository.

The primary risk inside `[tools]` is a literal secret embedded in fields such as `install_env`.

Schema 6 adds a narrow Mise safety check. Recursively inspect map keys case-insensitively for obvious sensitive names, including:

```text
token
secret
password
passwd
credential
credentials
private_key
private-key
api_key
api-key
apikey
```

If such a key contains a non-empty literal scalar value, capture/check fails with a path-specific error, for example:

```text
mise tool github:example/tool contains a literal sensitive value at install_env.GITHUB_TOKEN;
reference an environment variable or external secret source instead
```

Reference/template expressions such as:

```toml
GITHUB_TOKEN = "{{ env.GITHUB_TOKEN }}"
```

are allowed because the secret value itself is not stored in the profile.

Blueprint does **not** redact a literal value and continue, because redaction would silently change installation semantics.

This is intentionally a narrow guard, not a general secret-detection subsystem.

## 11. Package Identity and Cross-source Semantics

Pacman official/AUR retain their existing cross-repository equivalence: if a package moves between official and AUR, an installed package of the same name can satisfy the profile.

Mise does **not** participate in that name-only equivalence.

These are distinct portable intents:

```text
official:node
mise:node
```

Even if both expose a `node` command, their provenance and update semantics differ.

Therefore:

```go
packageNames(packages)
```

continues to unify only Pacman/AUR/installed Pacman names.

Mise comparisons use exact Mise tool IDs.

## 12. Diff and Status Semantics

For each normalized Mise tool ID:

| Saved profile | Current target | Result |
|---|---|---|
| absent | absent | clean |
| present | absent | missing / restore candidate |
| present | same declaration | clean |
| present | different declaration | modified / conflict |
| absent | present | target-only extra |

Examples:

```text
- mise package node (24)
+ mise package bun (latest)
~ mise package python: [3.12, 3.13] → 3.13
```

For structured declarations, summaries use deterministic compact TOML-like output.

Any Mise difference contributes to Packages drift and therefore normal `status` exit code 2, matching existing package behavior.

## 13. Exclude / Include Policy

Explicit references add a third source:

```text
official:<name>
aur:<name>
mise:<tool-id>
package:<name-or-tool-id>
```

Examples:

```bash
omarchy-blueprint exclude mise:npm:@anthropic-ai/claude-code
omarchy-blueprint include mise:npm:@anthropic-ai/claude-code
```

Reference parsing splits only on the **first** colon, so backend-qualified tool IDs remain intact.

### 13.1 Generic `package:` references

`package:<name>` searches all three managed sources.

If exactly one source matches, it resolves to that source.

If multiple sources match, it is ambiguous:

```text
package node is ambiguous; use official:node or mise:node
```

### 13.2 Excluded Mise declarations remain stored

Pacman exclusions can reconstruct an included entry from the package name alone. Mise cannot: the declaration may contain rich options.

Therefore excluding a Mise package does **not** delete `Packages.Mise[id]`.

Instead:

- `Packages.Mise[id]` retains the last captured declaration;
- `Excluded` gains `mise:<id>`;
- `ApplyExclusions` removes the key only from the logical managed view used by Diff/Plan/Verify;
- `Include` removes the exclusion and the stored declaration becomes managed again.

While a Mise key is excluded, subsequent capture does not refresh its stored declaration. Exclusion therefore means "leave this resource outside capture/drift/restore" rather than "track its live changes invisibly."

`ValidateExclusions` requires an excluded Mise ID to have a stored declaration.

## 14. Restore Decision Rules

For each saved, non-excluded Mise tool:

```text
S = saved normalized declaration
T = current target normalized declaration
```

Rules:

1. Tool absent in target → safe addition.
2. Tool present and `S == T` → no-op.
3. Tool present and `S != T` → conflict; preserve target and skip.
4. Tool exists only on target → leave installed/configured; removal disabled.

`--force` does not override rule 3 in Schema 6.

This mirrors the existing "unknown target work wins" package philosophy while acknowledging that a Mise declaration carries more user intent than a simple package name.

A restore may partially succeed: safe missing Mise tools are added even if other Mise IDs conflict.

## 15. Safe Mutation of the Target Global Mise Config

Blueprint must not deserialize and re-marshal the user's whole global config. Doing so would reorder/reformat unrelated `[env]`, `[settings]`, tasks, comments, and custom formatting.

Blueprint also must not create a permanent high-precedence Blueprint-owned Mise overlay, because that would violate:

> The running system is the configuration editor.

For example, if the user later ran `mise unuse -g node`, an always-loaded Blueprint overlay would silently keep Node active.

### 15.1 Append-only candidate

For safe missing tools:

1. Read the exact existing global config bytes.
2. Encode only the missing saved tool declarations as canonical child tables.
3. Append those child tables to the existing bytes.
4. Parse the entire candidate document with `go-toml/v2`.
5. Re-extract and normalize `[tools]`.
6. Assert every intended addition is present and semantically equal.
7. Assert every pre-existing normalized tool remains semantically equal.
8. Only then emit a generated `FileWrite`.

Example appended block:

```toml
[tools.node]
version = "24"

[tools."npm:@anthropic-ai/claude-code"]
version = "latest"
```

If the existing document uses a TOML construct that cannot legally be extended this way—for example an inline root `tools = { ... }` that cannot later be reopened as child tables—Blueprint skips the missing Mise additions with a clear reason rather than reserializing the user's file.

### 15.2 Missing config

If the primary global config file does not exist, generate a new config containing only the saved missing tool declarations. Desired mode is `0644`.

### 15.3 Existing regular config

Emit one generated `FileWrite`:

```go
model.FileWrite{
    Generated:    true,
    Content:      candidate,
    Destination:  p.MiseGlobalConfig,
    SourceHash:   sha256(candidate),
    ExpectedHash: sha256(currentBytes),
    Backup:       true,
}
```

No explicit `Mode` is supplied, so the executor preserves the existing destination mode.

### 15.4 Mutation boundary and symlinks

Restore refuses to mutate when:

- the global config destination is a symlink;
- the destination is not a regular file;
- any existing parent component on the path to the config file is a symlink.

This provider-level parent check is required because the generic `FileWrite` executor currently rejects a symlink at the destination itself but does not reject every symlinked ancestor.

A missing parent directory is allowed if all existing ancestors are real directories.

## 16. Restore Operations and Ordering

The Packages plan keeps stable source ordering:

```text
1. official package install
2. AUR package installs
3. Mise config write, if needed
4. Mise install, if needed
```

The Mise config operation ID is:

```text
packages.mise.configure
```

The install operation ID is:

```text
packages.mise.install
```

The install operation depends on `packages.mise.configure`.

After the declaration write succeeds, Blueprint invokes:

```bash
mise -C / install <tool-id>...
```

with missing IDs passed as separate argv entries in sorted order.

Why `-C /`:

- it prevents a project-local `mise.toml` in Blueprint's current working directory from affecting the restore;
- global and system configuration still apply;
- bare tool IDs use the configured version request when one exists.

Blueprint does not construct shell command strings.

The install command is only emitted for declarations Blueprint safely added. Conflicting target declarations are not installed on behalf of the profile.

## 17. Risk Classification

Mise configuration and installation are user-approved package restoration, but tool declarations can optionally execute arbitrary `postinstall` commands.

Risk rules:

### Config write

```text
RiskMedium
```

Reason: it mutates a shared user-global configuration file, although only after exact preconditions and a backup.

### Install without `postinstall`

```text
RiskLow
```

This matches official/AUR package installation.

### Install containing any captured `postinstall`

```text
RiskHigh
```

The operation is still allowed and is not skipped. High risk simply makes the executable captured behavior visible in the plan, consistent with Blueprint's treatment of arbitrary hook source.

`postinstall` detection is recursive enough to cover the supported normalized tool table shape.

## 18. Verification

Verification checks **declaration intent**, not Mise's resolved cache.

For every saved, non-excluded Mise tool:

- missing target declaration → verification failure;
- different target declaration → verification failure;
- semantically equal declaration → satisfied.

Target-only Mise declarations do not fail verification.

Blueprint does not require that a particular concrete resolved version remains installed after restore because the saved profile did not capture that concrete version as desired state.

A failed `mise install` already appears as an execution failure during restore. A later `verify` focuses on reproducible declaration state.

## 19. `check`

`check` validates:

1. package exclusion metadata;
2. profile Mise declaration shapes;
3. sensitive-value policy;
4. target global Mise config TOML, if it exists;
5. that `mise --version` succeeds when the profile manages one or more Mise packages.

A profile with no Mise packages does not require Mise merely to pass `check`, preserving compatibility with older Omarchy/profile states.

## 20. Profile Migration

Schema 5 → 6 migration is additive.

Loading a Schema 5 profile:

```text
Official/AUR/etc. → retained
Mise               → empty
Schema              → upgraded to 6 in memory
```

No `packages/mise.toml` file is required for schema 5 or older.

Once saved under schema 6, `packages/mise.toml` is written, including an empty `[tools]` table for deterministic repository shape.

A schema-6 profile whose `packages/mise.toml` is malformed fails to load.

## 21. App Integration

The Packages provider remains the first state provider and remains category ID:

```text
packages
```

Add a dependency hook so tests can isolate the global config path:

```go
type Dependencies struct {
    ...
    MiseGlobalConfig func() (string, error)
}
```

Default implementation calls `packages.ResolveMiseGlobalConfigPath()`.

The package adapter constructs:

```go
packages.Provider{
    Runner:           deps.Runner,
    MiseGlobalConfig: path,
}
```

No new category, capture flag, or restore selector is introduced.

## 22. Interaction With Defaults and Agents

This milestone improves the existing Defaults boundary without changing it.

Example profile:

```text
Packages
  mise: claude = latest

Defaults
  agent = claude
```

Packages can now reconstruct the tool declaration. The existing Defaults provider still skips automatic agent selection because Omarchy's current agent setter launches the selected agent.

A future safe set-only agent setter can depend on Packages availability without redesigning Mise state.

## 23. Failure Handling

### Capture failures

Capture fails rather than guessing when:

- global config cannot be read for a reason other than absence;
- TOML is malformed;
- a `[tools]` declaration cannot be normalized;
- an obvious literal secret would be written into the profile.

### Restore skips

Restore skips affected Mise additions when:

- a saved tool conflicts with an existing target declaration;
- destination config is a symlink/special file;
- an existing parent directory is a symlink;
- target config is malformed;
- append-only candidate construction is not valid TOML;
- candidate validation would alter pre-existing tool semantics.

Safe independent additions may still proceed.

### Execution failures

If the config write succeeds but `mise install` fails, the declaration remains configured and the operation failure is journaled/reported. Blueprint does not roll back a valid user-global declaration merely because a network/backend install failed; re-running restore or `mise install` can retry it.

## 24. Alternatives Considered

### A. Separate `tools` provider

Rejected.

It splits installation semantics from the existing Packages provider and makes Defaults/package ordering harder to reason about.

### B. Capture only Omarchy-generated wrappers

Rejected.

The user wants Blueprint to reconstruct the state they actually left the machine in, including Mise tools they configured themselves.

### C. Capture installed concrete Mise versions

Rejected.

Mise distinguishes declared requests from resolved versions. Pinning a broad request during capture would change the user's update policy.

### D. Copy `~/.config/mise/config.toml` wholesale

Rejected.

Packages owns `[tools]`, not unrelated Mise environment/settings/task configuration. Whole-file capture would also create unsafe cross-machine overwrite behavior.

### E. Re-marshal the whole target global config

Rejected.

It would destroy comments/formatting and rewrite state outside Packages ownership.

### F. Persistent Blueprint `conf.d` overlay

Rejected.

It would remain active after the user edits/removes their global tools and violate the "running system is the configuration editor" principle.

### G. Skip declarations containing `postinstall`

Rejected.

`postinstall` is legitimate Mise package-install intent the user previously chose. Blueprint restores it and exposes the operation as high risk rather than mutating or refusing the declaration.

## 25. Acceptance Criteria

The milestone is complete when all of the following hold:

1. A schema-5 profile loads as schema 6 with no Mise state.
2. Capture writes deterministic `packages/mise.toml`.
3. `node = "24"` remains request `"24"`, not a resolved `24.x.y`.
4. `latest` remains `latest`.
5. arrays such as `["3.12", "3.13"]` round-trip.
6. backend-qualified IDs containing `:`, `/`, and `@` round-trip.
7. structured options round-trip, including nested maps.
8. `postinstall` round-trips and makes the Mise install operation high risk.
9. `install_env` round-trips unless it contains a detected literal secret.
10. missing global config can be safely created.
11. a missing Mise tool can be added to an existing config without changing existing bytes before the appended block.
12. existing comments, `[env]`, `[settings]`, and tasks remain byte-for-byte untouched.
13. an equal target declaration is a no-op.
14. a conflicting declaration is reported and not overwritten.
15. target-only Mise tools are preserved and reported as additional package drift.
16. target config symlinks are never followed on restore.
17. symlinked parent directories are never written through.
18. malformed target TOML produces no mutation.
19. an append-incompatible existing TOML shape produces a skip, not a whole-file rewrite.
20. `mise:<id>` exclude/include works with backend-qualified IDs.
21. excluding a Mise tool retains its saved declaration for later include.
22. `package:<id>` becomes ambiguous when the same ID is managed by Pacman/AUR and Mise.
23. official/AUR cross-source equivalence remains unchanged.
24. one restore plan can contain Pacman, AUR, and Mise operations in stable order.
25. Mise install runs only after its configuration write succeeds.
26. the install command is argv-safe: `mise -C / install <id>...`.
27. verification fails for missing/changed saved Mise declarations and ignores target-only declarations.
28. `check` requires a working Mise executable only when the profile manages Mise packages.
29. app-level source→fresh-target capture/restore/status finishes clean with all three package sources represented.
30. no new top-level CLI category is introduced.

## 26. Future Extensions

Possible later work, outside Schema 6:

- effective global Mise state across `conf.d` / environment-specific global fragments;
- global Mise lockfile intent;
- custom `[tool_alias]` provenance;
- Omarchy wrapper alias/bin metadata;
- user-selectable remove/exact convergence semantics;
- machine-specific Mise tool declarations;
- project-local Mise state as part of Portable Resources;
- dependency linking from Defaults agent selection to a managed Mise tool.
