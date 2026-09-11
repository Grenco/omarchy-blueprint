# Machine Overlays v1: Resource Path Mappings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add explicit machine overlays and local machine selection so portable Resources can be detected, captured, reconstructed, linked, and verified at machine-specific whole-root paths without changing their portable identity.

**Architecture:** Schema 11 adds portable `machines/<name>.toml` overlay files containing Resource-ID → root-path mappings. A new machine package owns local XDG-state binding and selection precedence; the Resources provider receives one path resolver and routes every live Resource root through it while leaving `Resource.Path` unchanged. Machine policy is visible in CLI/JSON and does not broaden restore overwrite authority.

**Tech Stack:** Go, Cobra, pelletier/go-toml/v2, existing profile/Resources/restore engine, XDG state conventions, standard library filesystem/crypto packages.

**Spec:** `docs/superpowers/specs/2026-09-11-machine-overlay-resource-path-mappings-design.md`

## Global Constraints

- Profile schema becomes **11**; schema-10 binaries must reject schema 11 rather than ignore machine overlays.
- Machine identity is explicit user intent; hostname is a suggestion only.
- Persist no raw machine ID, serial number, product UUID, or hardware fingerprint.
- `Resource.Path` remains the portable/default `~/...` path and must never be rewritten to an effective machine path during capture.
- Machine mappings support whole Resource roots only.
- Mapping paths accept only canonical `~/...` or absolute paths; relative paths, bare `~`, bare `/`, `$HOME`, `${HOME}`, and `~user` are invalid.
- Mapping may target a non-existent destination so policy can be defined before restore.
- Effective Resource roots may not overlap one another, the active profile directory, or Blueprint's own XDG state subtree.
- Effective Resource roots must pass the existing Resources semantic-provider ownership conflict check; mappings are not an ownership bypass.
- `--machine` overrides local binding for one invocation and never mutates the binding.
- A missing explicitly requested/bound machine is an error; never silently fall back to default Resource paths.
- No selected machine is valid and means portable/default paths.
- Machine mapping changes placement only; strategy, captured Git/copy state, links, and overwrite safety remain unchanged.
- Dormant mappings to absent Resource IDs are loadable and informational; CLI `machine map` may only create mappings for currently tracked Resources.
- Existing differing destinations remain conflict/skip by default.
- Do not add machine-specific packages/config/monitors, subtree mappings, hardware matching, mount management, or TUI in this plan.

---

## File structure

New focused files:

```text
internal/machine/binding.go        local XDG profile→machine binding
internal/machine/binding_test.go
internal/machine/selection.go      hostname suggestion + selection precedence
internal/machine/selection_test.go
internal/machine/paths.go          machine path grammar/effective root resolution
internal/machine/paths_test.go
```

Existing files retain their responsibilities:

```text
internal/profile/profile.go                portable machine-overlay types/load/save/schema
internal/providers/resources/path.go       provider-facing live root resolution helpers
internal/providers/resources/provider.go   detect/capture/track/check using effective roots
internal/providers/resources/links.go      mapping-aware link classification/discovery
internal/providers/resources/plan.go       mapping-aware restore destinations
internal/app/app.go                        global flag + machine CLI + human/JSON orchestration
internal/app/providers.go                  selected Resource path resolver injection
```

Avoid creating a separate machine “provider”; machine overlays modify context for Resources rather than owning runtime state themselves.

---

### Task 1: Add schema-11 portable machine overlay serialization

**Files:**
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`
- Create: `docs/adr/0016-machine-overlay-resource-path-mappings.md`
- Create: `docs/superpowers/specs/2026-09-11-machine-overlay-resource-path-mappings-design.md`
- Create: `docs/superpowers/plans/2026-09-11-machine-overlay-resource-path-mappings-implementation-plan.md`

**Interfaces:**
- Produces:

```go
const machineOverlaySchema = 11

type Machines struct {
    Items []Machine `json:"machines" toml:"-"`
}

type Machine struct {
    Name          string                `json:"name" toml:"name"`
    ResourcePaths []MachineResourcePath `json:"resource_paths,omitempty" toml:"resource_path,omitempty"`
}

type MachineResourcePath struct {
    Resource string `json:"resource" toml:"resource"`
    Path     string `json:"path" toml:"path"`
}
```

and `Data.Machines Machines`.

- [ ] **Step 1: Write schema migration/load tests**

Add tests proving:

```text
schema 10 profile, no machines directory
→ Load succeeds
→ Manifest.Schema becomes 11 in memory
→ Machines empty

schema 11 + machines/framework.toml
→ Machine loaded
→ filename and `name` must match
→ resource_path entries preserved

schema > 11
→ rejected
```

Use a fixture machine file:

```toml
name = "framework"

[[resource_path]]
resource = "projects"
path = "~/Code"
```

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./internal/profile -run 'Schema|Machine' -count=1
```

Expected: FAIL because schema 11 / machine types are not implemented.

- [ ] **Step 3: Add schema and types**

Set:

```go
const Schema = 11
const machineOverlaySchema = 11
```

Add the machine types above and `Machines Machines` to `profile.Data`.

- [ ] **Step 4: Implement canonical machine load**

For schema ≥ 11:

1. read `machines/` if present;
2. consider regular `*.toml` files only;
3. unmarshal one `Machine` per file;
4. require `strings.TrimSuffix(base, ".toml") == Machine.Name`;
5. reject duplicate machine names;
6. reject duplicate `Resource` keys inside a machine;
7. sort `ResourcePaths` by `Resource` and machines by `Name`.

An absent `machines/` directory is valid.

- [ ] **Step 5: Implement canonical machine save**

During `profile.Save`, ensure `machines/` exists and atomically write each in-memory machine to:

```go
filepath.Join(dir, "machines", machine.Name+".toml")
```

Use `toml.Marshal(machine)` after canonical sorting. Do not implement rename/delete pruning in this task.

- [ ] **Step 6: Add round-trip tests**

Save/load must preserve two machines with multiple path records and deterministic ordering. Assert schema 11 is written.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/profile -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/profile/profile.go internal/profile/profile_test.go \
  docs/adr/0016-machine-overlay-resource-path-mappings.md \
  docs/superpowers/specs/2026-09-11-machine-overlay-resource-path-mappings-design.md \
  docs/superpowers/plans/2026-09-11-machine-overlay-resource-path-mappings-implementation-plan.md
git commit -m "feat: add machine overlay profile state"
```

---

### Task 2: Implement machine IDs, path grammar, and effective root resolution

**Files:**
- Create: `internal/machine/paths.go`
- Create: `internal/machine/paths_test.go`
- Modify: `internal/providers/resources/path.go`
- Modify: `internal/providers/resources/path_test.go`

**Interfaces:**
- Produces:

```go
func ValidateName(name string) error
func NormalizeMappingPath(home, raw string) (string, error)
func ExpandMappingPath(home, stored string) (string, error)

type ResourcePaths struct {
    Home      string
    Overrides map[string]string
}

func (r ResourcePaths) Resolve(item profile.Resource) (string, error)
```

- [ ] **Step 1: Write machine-name validation tests**

Table cases:

```text
framework      valid
desktop-2      valid
work.vm_1      valid
""             invalid
.              invalid
..             invalid
"work laptop" invalid
../../x        invalid
```

- [ ] **Step 2: Write mapping path normalization tests**

Given HOME `/home/test`:

```text
~/Code/site                 → ~/Code/site
/home/test/Code/site        → ~/Code/site
/mnt/fast/site              → /mnt/fast/site
/home/test/../test/Code     → ~/Code
~                           → error
/                           → error
Code/site                   → error
../site                     → error
~other/site                 → error
$HOME/site                  → error
${HOME}/site                → error
```

- [ ] **Step 3: Write ResourcePaths resolution tests**

For:

```go
item := profile.Resource{ID: "projects", Path: "~/Projects"}
```

assert:

```text
no override             → /home/test/Projects
override ~/Code         → /home/test/Code
override /mnt/projects  → /mnt/projects
```

and `item.Path` remains unchanged.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/machine ./internal/providers/resources -run 'Name|MappingPath|ResourcePaths' -count=1
```

Expected: FAIL because package/interfaces are missing.

- [ ] **Step 5: Implement path grammar**

Use lexical filesystem operations only; no shell/environment expansion.

`NormalizeMappingPath` must convert any absolute path strictly below HOME to `~/...`, while retaining external absolute paths. Reject HOME itself and `/`.

- [ ] **Step 6: Implement provider resolver**

Keep existing `ExpandHomePath` unchanged for portable Resource paths. Add `ResourcePaths.Resolve` (or an exact equivalent) so mapping support does not weaken the current `Resource.Path` invariant.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/machine ./internal/providers/resources -run 'Name|MappingPath|ResourcePaths|ExpandHomePath' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/machine/paths.go internal/machine/paths_test.go \
  internal/providers/resources/path.go internal/providers/resources/path_test.go
git commit -m "feat: resolve machine resource paths"
```

---

### Task 3: Add local profile-to-machine binding

**Files:**
- Create: `internal/machine/binding.go`
- Create: `internal/machine/binding_test.go`

**Interfaces:**
- Produces:

```go
type BindingStore struct {
    StateHome string
}

func (s BindingStore) Load(profileDir string) (string, error)
func (s BindingStore) Save(profileDir, machine string) error
func (s BindingStore) Clear(profileDir string) error
```

- [ ] **Step 1: Write binding path and permissions tests**

For profile root `/tmp/profile`, assert binding lives under:

```text
<state>/omarchy-blueprint/profiles/<64-hex-sha256>/machine.toml
```

and contains:

```toml
profile = "/canonical/profile/path"
machine = "framework"
```

Parent directory mode must be `0700`; file mode `0600`.

- [ ] **Step 2: Write canonical-profile-root test**

Create a real profile directory and symlink to it. Save through the symlink and load through the real path; both must address the same binding when `EvalSymlinks` succeeds.

- [ ] **Step 3: Write clear/mismatch tests**

Assert:

```text
Clear removes only this profile binding
missing binding → "", nil
binding file profile field != canonical root → error
invalid machine name in binding → error
```

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/machine -run 'Binding' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement atomic restrictive writes**

Use temp file in the final directory:

```text
create temp
chmod 0600
write TOML
fsync file
close
rename
fsync parent directory
```

Do not reuse profile `atomicWrite` because local binding has stricter permissions and lives outside the profile.

- [ ] **Step 6: Run tests**

```bash
go test ./internal/machine -run 'Binding' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/machine/binding.go internal/machine/binding_test.go
git commit -m "feat: persist local machine selection"
```

---

### Task 4: Implement hostname suggestion and machine selection precedence

**Files:**
- Create: `internal/machine/selection.go`
- Create: `internal/machine/selection_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Produces:

```go
type Selection struct {
    Name    string
    Source  string // explicit | binding | default
    Machine *profile.Machine
}

func SuggestName(hostname string, existing []profile.Machine) string
func Select(explicit, bound string, machines []profile.Machine) (Selection, error)
```

`Dependencies` gains:

```go
Hostname func() (string, error)
```

with default `os.Hostname`.

`options` gains:

```go
machine string
```

- [ ] **Step 1: Write suggestion tests**

Assert:

```text
"Framework"              → framework
"work laptop"            → work-laptop
"HOST.local"             → host.local
"localhost"              → machine
""                       → machine
existing framework        → framework-2
existing framework,-2     → framework-3
```

- [ ] **Step 2: Write selection tests**

Assert precedence:

```text
explicit desktop + binding framework → desktop / explicit
no explicit + binding framework      → framework / binding
neither                              → empty / default
explicit missing                     → error
bound missing                        → error with machine use/clear guidance
```

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/machine -run 'Suggest|Select' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Implement pure selection helpers**

No filesystem reads belong in `Select`; callers load binding and profile machines and pass the values in.

- [ ] **Step 5: Add root flag/dependency**

In `newRoot`:

```go
root.PersistentFlags().StringVar(&opt.machine, "machine", "", "use machine overlay for this invocation")
```

Set `deps.Hostname = os.Hostname` when nil.

Do not yet alter provider behavior in this task.

- [ ] **Step 6: Run tests**

```bash
go test ./internal/machine ./internal/app -run 'Suggest|Select|Root' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/machine/selection.go internal/machine/selection_test.go internal/app/app.go
git commit -m "feat: select machine overlays"
```

---

### Task 5: Add machine management CLI and mapping validation

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/profile/profile.go`
- Modify: `internal/profile/profile_test.go`
- Modify: `internal/machine/paths.go`
- Modify: `internal/machine/paths_test.go`
- Modify: `internal/providers/resources/path.go`
- Modify: `internal/providers/resources/path_test.go`

**Interfaces:**
- Produces commands:

```text
machine add [name] [--no-use]
machine list
machine current
machine use <name>
machine clear
machine map <resource-ref> <path>
machine unmap <resource-ref>
```

and effective-root validation helper:

```go
func ResolveEffectiveRoots(home, profileDir, blueprintStateDir string, resources profile.Resources, machine *profile.Machine) (map[string]string, []string, error)
func ValidateEffectiveOwnership(roots map[string]string, claims ownership.Index) error
```

- [ ] **Step 1: Write command tests for add/list/use/current/clear**

Use injected `Hostname` and `StateHome`.

Required sequence:

```text
machine add            with hostname Framework
→ machines/framework.toml created
→ local binding framework

machine list
→ framework marked selected

machine current
→ framework (local binding)

machine add desktop --no-use
→ desktop created
→ binding still framework

--machine desktop machine current
→ desktop (explicit)
→ binding still framework

machine use desktop
→ binding desktop

machine clear
→ no binding / default
```

Inspect both profile files and local binding, not only human output.

- [ ] **Step 2: Write mapping command tests**

Given tracked Resources `projects` and `dotfiles`:

```text
machine map projects ~/Code
→ stored as resource=projects,path=~/Code

machine map resource:projects /home/test/Work
→ canonical stored ~/Work

machine map projects /mnt/fast/projects
→ stored absolute

machine unmap projects
→ mapping removed
```

No selected machine must error with exact guidance.

Unknown Resource must error.

- [ ] **Step 3: Write effective-root overlap tests**

Cases:

```text
projects → ~/Code
other default ~/Code/other
→ reject overlap

projects → <profileDir>/nested
→ reject profile overlap

projects → <stateHome>/omarchy-blueprint/nested
→ reject Blueprint-state overlap

projects → ~/.config/omarchy with Config recursive ownership claim
→ reject semantic ownership conflict

projects → /mnt/projects
other → /srv/other
→ valid
```

Dormant mapping to absent ID must be ignored by root validation and returned separately as informational/orphan metadata.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/machine ./internal/profile ./internal/app -run 'Machine|EffectiveRoot' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement command group**

Register `machineCommand(deps, opt)` on root.

Every profile-mutating machine command must:

1. `profile.Load`;
2. mutate `d.Machines` only after validation;
3. update `d.Manifest.Profile.UpdatedAt`;
4. `profile.Save`;
5. then emit result.

`machine use`/`clear` mutate only BindingStore and must not update profile `updated_at`.

- [ ] **Step 6: Implement `ResolveEffectiveRoots`**

In `internal/machine/paths.go`, resolve only current Resources. Ignore dormant mapping IDs when calculating active roots and return those dormant IDs separately for informational output. Reject pairwise `PathsOverlap`, profile-directory overlap, and overlap with `<state-home>/omarchy-blueprint`. Return resolved absolute roots keyed by Resource ID plus sorted dormant mapping IDs. Keep this helper independent of provider ownership.

- [ ] **Step 7: Enforce semantic-provider ownership on effective roots**

In `internal/providers/resources/path.go`, add `ValidateEffectiveOwnership(roots, claims)`. Iterate resource IDs deterministically and call `claims.TrackConflict(root)`. Any conflict is a hard error naming the Resource, effective root, provider, and claimed path. Use this validation in `machine map` before profile save so a mapping cannot place Resources into Config/Hooks/Themes/Plugins/Shell/etc. owned paths. Task 6 will also run it when provider context is built so manually edited overlays receive the same protection.

- [ ] **Step 8: Add JSON assertions**

Machine command JSON must expose stable fields such as:

```json
{"name":"framework","source":"binding"}
```

and never expose state-home paths/profile hashes.

- [ ] **Step 9: Run tests**

```bash
go test ./internal/machine ./internal/profile ./internal/app -run 'Machine|EffectiveRoot' -count=1
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go internal/profile/profile.go \
  internal/profile/profile_test.go internal/machine/paths.go internal/machine/paths_test.go \
  internal/providers/resources/path.go internal/providers/resources/path_test.go
git commit -m "feat: manage machine resource mappings"
```

---

### Task 6: Route Resources detection and capture through effective paths

**Files:**
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/providers/resources/provider_test.go`
- Modify: `internal/providers/resources/path.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Consumes: `machine.ValidateEffectiveRoots` / `ResourcePaths`.
- Provider gains a single effective-root context, for example:

```go
type Provider struct {
    // existing fields
    ResourcePaths ResourcePaths
}

func (p Provider) resourceRoot(item profile.Resource) (string, error)
```

- [ ] **Step 1: Inventory direct portable-path expansion**

Before editing, search Resources code for all live-path uses of:

```text
ExpandHomePath(... item.Path ...)
ExpandHomePath(... resource.Path ...)
```

List each call in the task notes and route every live Resource root through `resourceRoot`. Do not alter pure profile/snapshot artifact paths.

- [ ] **Step 2: Write detection tests for all strategies**

For saved default `~/Projects/repo`, override to `~/Code/repo`:

```text
copy current bytes only at ~/Code/repo → detected/satisfied
git repo only at ~/Code/repo          → detected/satisfied
git+diff only at ~/Code/repo          → detected/satisfied
bytes/repo at default path only        → mapped machine reports missing/drift
```

- [ ] **Step 3: Write capture preservation tests**

For each strategy capture at mapped root and assert returned/persisted Resource still has:

```go
Path == "~/Projects/repo"
```

while hashes/revision/patches reflect mapped live state.

- [ ] **Step 4: Write existing-track update test**

Saved Resource default path `~/Projects/repo`, active effective root `~/Code/repo`.

Calling `PrepareTrack(..., ~/Code/repo, strategy update)` must match the existing Resource ID and must not rewrite portable `Path`.

- [ ] **Step 5: Run tests to verify failure**

```bash
go test ./internal/providers/resources ./internal/app -run 'Mapped|EffectivePath|Machine.*Resource' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Implement provider root helper and replace live path lookups**

`resourceRoot` uses `Provider.ResourcePaths.Resolve(item)`. If no overrides are configured, behavior must be byte-for-byte compatible with current `ExpandHomePath` semantics.

Do not create a cloned Resources object with mutated `Path` and feed it back into capture; that risks persisting machine paths.

- [ ] **Step 7: Inject selected mapping in app adapter**

`resourcesStateProvider.provider()` resolves current selection using:

```text
opt.machine
BindingStore
profile machines
```

and passes effective overrides to Resources provider. Before returning the provider, run `ValidateEffectiveOwnership` against the provider's ownership index so manually edited machine files cannot bypass semantic ownership.

Missing explicit/bound overlay or an ownership-conflicting effective root must fail before detection/capture.

- [ ] **Step 8: Run tests**

```bash
go test ./internal/providers/resources ./internal/app -run 'Mapped|EffectivePath|Machine.*Resource' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/providers/resources/path.go internal/providers/resources/provider.go \
  internal/providers/resources/provider_test.go internal/app/providers.go internal/app/app_test.go
git commit -m "feat: capture resources at machine paths"
```

---

### Task 7: Make ResourceLinks mapping-aware

**Files:**
- Modify: `internal/providers/resources/links.go`
- Modify: `internal/providers/resources/links_test.go`
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/providers/resources/provider_test.go`

**Interfaces:**
- Consumes `Provider.resourceRoot` / resolved Resource root map.
- Produces mapping-aware classification/discovery while leaving persisted `ResourceLink` unchanged.

- [ ] **Step 1: Write inbound-link discovery test**

Create:

```text
HOME/.config/tool -> /mnt/dotfiles/tool
```

with portable Resource `dotfiles.Path=~/dotfiles` and effective root `/mnt/dotfiles`.

Discovery must classify the link as managed inbound targeting:

```text
TargetResource = dotfiles
Target         = tool
```

- [ ] **Step 2: Write internal-link classification test**

Map both source and target Resources outside their default paths and create a symlink inside mapped source pointing inside mapped target. Classification must preserve source/target Resource IDs and relative path.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/providers/resources -run 'Link.*Mapped|Mapped.*Link' -count=1
```

Expected: FAIL.

- [ ] **Step 4: Refactor link helpers to accept resolved roots**

Do not mutate portable `Resource.Path`. Pass an `id -> absolute root` map or resolver into link classification functions.

The default/no-overlay call path must remain simple and preserve existing tests.

- [ ] **Step 5: Run full link tests**

```bash
go test ./internal/providers/resources -run 'Link' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/resources/links.go internal/providers/resources/links_test.go \
  internal/providers/resources/provider.go internal/providers/resources/provider_test.go
git commit -m "feat: resolve resource links through machine paths"
```

---

### Task 8: Make restore planning use effective Resource roots

**Files:**
- Modify: `internal/providers/resources/plan.go`
- Modify: `internal/providers/resources/plan_test.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- All Resource plan destinations/repositories/symlink roots come from `Provider.resourceRoot`.

- [ ] **Step 1: Write mapped copy restore-plan tests**

For missing copy Resource:

```text
portable path = ~/Scripts
effective path = /mnt/tools/Scripts
```

assert FileWrite/Copy destination is mapped path and no operation references portable path.

- [ ] **Step 2: Write mapped Git and git+diff graph tests**

Assert:

```text
git clone destination                  = mapped root
git checkout -C path                  = mapped root
GitPatchApply.Repository               = mapped root
selected-untracked FileWrite destination = mapped root/subpath
```

Dependency ordering must remain unchanged.

- [ ] **Step 3: Write mapped-link restore test**

Inbound link destination remains its semantic source location, while target is calculated relative to mapped target Resource root.

For internal links, both mapped source and target roots must be used.

- [ ] **Step 4: Write existing-conflict test**

Put differing unknown state at mapped destination and keep default path absent. Plan must conflict/skip at mapped destination and must not fall back to portable default path.

- [ ] **Step 5: Run tests to verify failure**

```bash
go test ./internal/providers/resources ./internal/app -run 'Plan.*Mapped|Mapped.*Restore|Machine.*Restore' -count=1
```

Expected: FAIL.

- [ ] **Step 6: Replace plan path expansion with `resourceRoot`**

Cover:

```text
copy destinations
clone destination
checkout repository
Git patch repository
selected-untracked destination
resourcePath(...) used by links
```

No restore executor change should be necessary; it receives already-resolved absolute operations.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/providers/resources ./internal/app -run 'Plan|Mapped|Restore' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/providers/resources/plan.go internal/providers/resources/plan_test.go \
  internal/app/providers.go internal/app/app_test.go
git commit -m "feat: restore resources to machine paths"
```

---

### Task 9: Expose machine context in status, diff, tracked, check, and JSON

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`
- Modify: `internal/providers/resources/provider.go`
- Modify: `internal/providers/resources/provider_test.go`

**Interfaces:**
- Produces stable output context:

```go
type machineOutput struct {
    Name   string `json:"name"`
    Source string `json:"source"`
}
```

Resource runtime output gains default/effective path detail without changing persisted `profile.Resource`.

- [ ] **Step 1: Write human-output tests**

With active mapping:

```text
Machine: desktop
```

and `tracked` includes:

```text
projects  ...  ~/Projects
  effective: /mnt/fast/projects (desktop)
```

With no selection and existing overlays:

```text
Machine: none (portable default resource paths)
```

- [ ] **Step 2: Write JSON tests**

Status/diff Resources output must include:

```json
"machine": {"name":"desktop","source":"binding"}
```

and Resource path context:

```json
"default_path":"~/Projects"
"effective_path":"/mnt/fast/projects"
```

Do not expose binding file path, profile-root hash, or local-state directory.

- [ ] **Step 3: Write `check` tests**

Assert hard errors for selected-machine Resource-root overlap, profile/state-root overlap, and semantic-provider ownership conflicts, but dormant mapping is surfaced informationally and does not fail artifact integrity.

If the current `check` result model cannot represent informational findings, include dormant mappings in human/JSON machine detail rather than inventing a generic warning/error semantic.

- [ ] **Step 4: Run tests to verify failure**

```bash
go test ./internal/app ./internal/providers/resources -run 'Machine.*Status|Machine.*Diff|Tracked.*Machine|Check.*Machine' -count=1
```

Expected: FAIL.

- [ ] **Step 5: Implement one selection-resolution helper in app**

Avoid each command independently reading binding/overlay state. Add a helper that returns the resolved `machine.Selection` plus effective roots and reuse it across Resource-sensitive commands/provider construction.

- [ ] **Step 6: Render machine context**

Keep machine context supplemental; generic `model.Change` drift semantics remain about managed state, not the existence of a mapping.

- [ ] **Step 7: Run tests**

```bash
go test ./internal/app ./internal/providers/resources -run 'Machine|Tracked|Check' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go \
  internal/providers/resources/provider.go internal/providers/resources/provider_test.go
git commit -m "feat: report active machine resource paths"
```

---

### Task 10: Cover aggregate capture, explicit override, and mapping idempotence

**Files:**
- Modify: `internal/app/app_test.go`
- Modify: `internal/providers/resources/provider_test.go`
- Modify: `internal/profile/profile_test.go`

**Interfaces:**
- Validates orchestration only; no new public API expected.

- [ ] **Step 1: Add aggregate capture regression**

With local binding active, run full `capture` (not just `capture resources`). Ensure Resources reads mapped root while Config/packages/etc. behavior is unchanged.

- [ ] **Step 2: Add explicit override regression**

Create `laptop` local binding and `desktop` overlay. Run:

```text
--machine desktop status resources
--machine desktop capture resources
```

Assert desktop root is used and BindingStore still contains `laptop` afterward.

- [ ] **Step 3: Add idempotence regression**

Repeated:

```text
machine map projects /mnt/projects
capture resources
capture resources
```

must leave:

- machine TOML byte-stable after first mapping;
- portable Resource.Path unchanged;
- second capture with unchanged state has no Resources changes.

- [ ] **Step 4: Add default/no-overlay compatibility regression**

Create a schema-10-style Resource layout, load/migrate, run status/capture/restore planning with no machines and prove paths/changes match pre-feature expectations.

- [ ] **Step 5: Run tests**

```bash
go test ./internal/app ./internal/profile ./internal/providers/resources -run 'Machine|Capture|Idempotent|Schema10' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/app_test.go internal/profile/profile_test.go \
  internal/providers/resources/provider_test.go
git commit -m "test: cover machine overlay orchestration"
```

---

### Task 11: Add end-to-end machine-mapped reconstruction

**Files:**
- Modify: `internal/providers/resources/plan_test.go`
- Modify: `internal/app/app_test.go`

**Interfaces:**
- Validates the whole vertical slice through real Git/filesystem operations.

- [ ] **Step 1: Build real Git fixture**

Create local bare origin and source repo with:

```text
HEAD A
index B
worktree C
selected untracked notes.md
```

Track/capture portable Resource with default path under HOME using `git+diff`.

- [ ] **Step 2: Create machine mapping to a different missing root**

Use overlay `desktop`:

```text
resource:dotfiles → <temp external-or-different-home-root>/dotfiles
```

Add an inbound semantic link under HOME pointing into the Resource.

- [ ] **Step 3: Remove live source/default Resource and restore with desktop selected**

Generate/execute real restore plan and assert:

```text
mapped destination exists
default Resource path remains absent
HEAD == captured revision
index == B
worktree == C
selected untracked bytes/mode restored
inbound symlink resolves into mapped destination
```

- [ ] **Step 4: Verify mapped state clean**

Run provider verification/status using desktop overlay and assert no Resource drift.

Clear selection and assert default-path interpretation now reports Resource missing rather than accidentally following desktop mapping.

- [ ] **Step 5: Run focused integration tests**

```bash
go test ./internal/providers/resources ./internal/app -run 'Machine.*Reconstruct|Mapped.*GitDiff' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/providers/resources/plan_test.go internal/app/app_test.go
git commit -m "test: reconstruct resources through machine mappings"
```

---

### Task 12: Documentation, roadmap state, and final verification

**Files:**
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Modify: `docs/manual-test-checklist.md`
- Modify: `docs/adr/0012-portable-resources.md`
- Modify: `docs/adr/0016-machine-overlay-resource-path-mappings.md` only if implementation-required clarifications are discovered

**Interfaces:**
- Documentation only; no new runtime interface.

- [ ] **Step 1: Update README**

Document:

```bash
omarchy-blueprint machine add framework
omarchy-blueprint machine map resource:projects ~/Code
omarchy-blueprint machine use framework
omarchy-blueprint --machine desktop restore resources
```

Explain that default Resource path stays portable and the selected machine changes placement only.

- [ ] **Step 2: Update ROADMAP**

Mark Resource path mapping / Machine Overlays v1 as delivered in schema 11 and point to ADR 0016. Keep broader hardware-specific state and machine overlays pending.

Do not mark all Phase 6 complete.

- [ ] **Step 3: Update ADR 0012 consequence note**

Add a short note that its Resource-ID link indirection now powers schema-11 machine path mappings through ADR 0016.

- [ ] **Step 4: Expand manual checklist**

Include real-machine checks:

```text
create/bind machine overlay
map HOME-local Resource path
capture/status at mapped root
map Resource to external absolute root
missing-destination restore to mapped root
link follows mapped target
--machine override does not alter binding
machine clear reverts to portable default path
overlap/profile-root mapping rejected
```

- [ ] **Step 5: Run full verification**

```bash
go test ./... -count=1
go vet ./...
go build ./cmd/omarchy-blueprint
git diff --check
```

Expected: all PASS.

- [ ] **Step 6: Run manual acceptance**

On a real machine/profile copy, verify:

```bash
./omarchy-blueprint --profile "$PROFILE" machine add mapping-test
./omarchy-blueprint --profile "$PROFILE" machine map resource:<safe-test-resource> <alternate-root>
./omarchy-blueprint --profile "$PROFILE" machine current
./omarchy-blueprint --profile "$PROFILE" tracked
./omarchy-blueprint --profile "$PROFILE" status resources
./omarchy-blueprint --profile "$PROFILE" capture resources
./omarchy-blueprint --profile "$PROFILE" restore resources --dry-run
./omarchy-blueprint --profile "$PROFILE" --json status resources
```

Confirm the Resource's `path` in `resources/resources.toml` never changes to the mapped path.

- [ ] **Step 7: Commit**

```bash
git add README.md ROADMAP.md docs/manual-test-checklist.md \
  docs/adr/0012-portable-resources.md docs/adr/0016-machine-overlay-resource-path-mappings.md
git commit -m "docs: document machine resource mappings"
```

---

## Final review checklist

Before opening the PR, verify every item explicitly:

```text
[ ] schema 11 rejects downgrade/old-binary silent ignore
[ ] machine name is explicit; hostname is suggestion only
[ ] local binding is outside portable profile and mode 0600
[ ] --machine override does not persist
[ ] no-selection default behavior remains compatible
[ ] mapping paths support ~/ and external absolute roots only
[ ] Resource.Path never becomes the mapped path
[ ] copy, git, git+diff detect/capture at effective root
[ ] restore plans target effective root
[ ] differing mapped destination remains protected
[ ] inbound and internal ResourceLinks follow mapped roots
[ ] effective root overlaps rejected
[ ] profile-directory and Blueprint-state-directory overlap rejected
[ ] semantic-provider ownership conflicts rejected at effective mapped roots
[ ] dormant mapping is informational
[ ] machine context present in human/JSON output
[ ] aggregate capture respects selected machine
[ ] real git+diff reconstruction passes at alternate root
[ ] README/ROADMAP/manual checklist updated
[ ] go test ./... -count=1
[ ] go vet ./...
[ ] go build ./cmd/omarchy-blueprint
[ ] git diff --check
```
