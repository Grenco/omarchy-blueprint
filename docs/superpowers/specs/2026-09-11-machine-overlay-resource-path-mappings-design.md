# Machine Overlays v1: Resource Path Mappings Design

**Status:** Approved design
**Date:** 2026-09-11
**Implements:** ADR 0016

## 1. Purpose

Machine Overlays v1 removes the assumption that a portable Resource must live at the same filesystem root on every machine.

The first vertical slice answers one question only:

> Where does `resource:<id>` live on this selected machine?

All other Resource semantics remain portable and unchanged.

This design intentionally establishes the machine-identity and overlay-selection foundation that later TUI, monitor/hardware state, and provider-specific machine policy can reuse without implementing those broader features now.

## 2. Product model

A profile has three layers relevant to this feature:

```text
Portable Resource state
    resource ID + default path + strategy + captured state

Portable machine overlay
    machine name + Resource-ID → root-path overrides

Local selection
    this profile checkout → selected machine name
```

The running system remains the configuration editor for Resource content. Machine placement is explicit policy and changes only through `machine` commands or direct profile editing.

## 3. Current-state constraints

Schema 10 Resources persist a `Path` on each Resource. That path is currently `~/...` and provider code expands it under HOME.

Resources already have stable IDs and semantic links that refer to `TargetResource` and a target relative path. Dirty Git State v2 added strategy-specific local Git state without changing Resource identity.

Machine overlays must therefore be additive:

```text
Resource.Path                    portable/default root
Machine.ResourcePaths[ID].Path   optional selected-machine root
```

The overlay must never replace the portable `Resource.Path` in captured state.

## 4. Scope

### 4.1 Included

- schema 11;
- portable `machines/<name>.toml` files;
- explicit machine names;
- hostname-derived default suggestion for `machine add`;
- local per-profile machine binding under XDG state;
- one-invocation `--machine` override;
- whole-Resource-root mappings;
- HOME-relative and absolute machine paths;
- mapping-aware Resources detection, capture, status, diff, check, planning, restore, verification, link discovery and link restore;
- JSON and human output showing effective machine context;
- overlap/profile-root safety;
- CLI management of machine selection and Resource mappings.

### 4.2 Deferred

- subtree mappings;
- machine overlay inheritance;
- machine-specific package/config/defaults state;
- monitor/hardware providers;
- hardware-driven automatic selection;
- mount management;
- path variables other than HOME;
- TUI.

## 5. Machine identity

### 5.1 Identity is explicit

A machine overlay name is a user-controlled stable ID.

Valid examples:

```text
framework
desktop
work-laptop
lab_vm-1
```

Validation uses:

```text
^[A-Za-z0-9_.-]+$
```

with empty, `.` and `..` rejected.

Names are case-sensitive and must be unique within one profile.

### 5.2 Hostname is only a suggestion

`machine add` takes zero or one positional name:

```bash
omarchy-blueprint machine add framework
omarchy-blueprint machine add
```

When omitted, Blueprint obtains the current static hostname using an injectable `Hostname func() (string, error)` dependency and derives a conservative filename-safe suggestion:

1. trim whitespace;
2. lowercase;
3. replace runs of characters outside `[a-z0-9_.-]` with `-`;
4. trim leading/trailing `-._`;
5. if the result is empty, `localhost`, or invalid, use `machine`;
6. if that name already exists, append `-2`, `-3`, etc.

No vendor/model/chassis probe is required in v1. Later UX may display such information as a hint without changing identity semantics.

### 5.3 Add/use behavior

`machine add [name]`:

- creates a new empty portable overlay;
- binds it locally to the current profile by default;
- `--no-use` creates it without changing local selection;
- never overwrites an existing overlay.

With no name in human mode, it prompts `Machine name [suggestion]:`; blank input accepts the collision-free hostname suggestion and typed input is used directly. JSON mode requires an explicit name and never prompts. EOF before a response makes no change. `machine rename <old> <new>` preserves mappings and updates this installation's matching binding; `machine remove <name>` removes the overlay and matching local binding without touching live Resource bytes.

## 6. Portable storage

### 6.1 Schema

Profile schema becomes 11.

Schema 10 profiles migrate in memory with zero machines. Saving under the new binary writes schema 11.

A schema-10 binary must reject schema 11 before it can ignore machine path policy.

### 6.2 Machine file

Each overlay is one human-readable TOML file:

```text
machines/<name>.toml
```

Canonical example:

```toml
name = "desktop"

[[resource_path]]
resource = "dotfiles"
path = "~/dotfiles"

[[resource_path]]
resource = "projects"
path = "/mnt/fast/projects"
```

Use explicit records rather than a TOML map because Resource IDs may contain punctuation such as `.` and because records leave room for future per-mapping metadata without changing key semantics.

### 6.3 Profile types

Conceptually:

```go
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

`profile.Data` gains `Machines Machines`.

`profile.Load` reads `machines/*.toml` for schema 11, verifies each filename matches `Machine.Name`, validates every machine name, mapping Resource ID, and stored mapping path, normalizes ordering, and rejects duplicates.

`profile.Save` atomically writes all desired machine TOMLs, removes stale regular `*.toml` files not represented by `Data.Machines`, preserves unrelated non-TOML files, then fsyncs the machines directory.

### 6.4 Dormant mappings

A mapping may reference a Resource ID that is not currently present. It is dormant and has no effect.

Why:

- Resource untrack should not require a second cross-file transaction solely to erase placement policy;
- stable Resource IDs may later be reintroduced;
- manually edited profiles can remain loadable while `check` explains the orphan.

CLI `machine map` still requires the Resource to exist when creating/updating a mapping.

`check` reports dormant mappings informationally rather than failing the profile.

## 7. Machine path grammar

Portable `Resource.Path` remains unchanged and still uses `~/...` only.

Machine mapping paths accept:

```text
~/Code/project
/mnt/fast/project
```

Reject:

```text
~
/
Code/project
../project
~other/project
$HOME/project
${HOME}/project
```

No shell expansion occurs.

### 7.1 Canonical CLI storage

When `machine map` receives a path:

- convert it to an absolute path for validation;
- if the absolute result is strictly below current HOME, store it canonically as `~/...`;
- otherwise store the cleaned absolute Linux path.

This keeps HOME-local mappings portable across username/home-root changes while still supporting machine-specific mounted storage.

### 7.2 Effective root resolution

Add a single resolver boundary rather than sprinkling string substitution throughout providers.

Conceptually:

```go
type ResourcePaths struct {
    Home      string
    Overrides map[string]string // resource ID -> machine path
}

func (r ResourcePaths) Resolve(item profile.Resource) (string, error)
```

Resolution:

```text
path = item.Path
if override exists for item.ID:
    path = override

if path begins ~/:
    expand below HOME
else if path is absolute:
    filepath.Clean(path)
else:
    reject
```

Provider code must use this resolver for every live Resource root. It must never mutate `item.Path` to the effective machine path.

## 8. Local profile binding

### 8.1 Location

Use existing XDG state resolution supplied by `Dependencies.StateHome`.

For canonical absolute profile root `P`:

```text
key = hex(sha256(P))
```

Store:

```text
$STATE_HOME/omarchy-blueprint/profiles/<key>/machine.toml
```

with parent mode `0700` and file mode `0600`.

Canonical file:

```toml
profile = "/home/user/omarchy-profile"
machine = "framework"
```

The absolute profile field is local-only and verifies hash/path correspondence; it is never copied into the portable profile.

Resolve the profile root with `filepath.Abs`, `filepath.Clean`, and `filepath.EvalSymlinks` when possible so invoking Blueprint through a symlink does not create duplicate bindings.

### 8.2 Binding API

A focused package should own this state, conceptually:

```go
type BindingStore struct {
    StateHome string
}

func (s BindingStore) Load(profileDir string) (string, error)
func (s BindingStore) Save(profileDir, machine string) error
func (s BindingStore) Clear(profileDir string) error
```

Writes are atomic and synced before rename.

### 8.3 Selection precedence

For ordinary commands:

```text
--machine NAME present
→ load that portable overlay
→ source = explicit

else local binding present
→ load bound overlay
→ source = binding

else
→ no overlay
→ source = default
```

An explicit or bound name that does not exist is an error. Do not silently fall back to default paths because that could restore a Resource to the wrong root.

No overlay is valid when there is no binding/flag, even if overlays exist. Output should say that portable/default paths are active.

## 9. CLI

Add root persistent flag:

```bash
--machine <name>
```

It applies to commands that inspect or act on machine-sensitive state and never changes local binding.

Add command group:

```bash
omarchy-blueprint machine add [name] [--no-use]
omarchy-blueprint machine list
omarchy-blueprint machine current
omarchy-blueprint machine use <name>
omarchy-blueprint machine clear
omarchy-blueprint machine map <resource-ref> <path>
omarchy-blueprint machine unmap <resource-ref>
```

### 9.1 `machine list`

Human example:

```text
Machines
* framework
  desktop

* locally selected for this profile
```

JSON includes machine names plus `selected` and selection source.

### 9.2 `machine current`

Examples:

```text
Machine: framework (local binding)
```

or:

```text
Machine: desktop (explicit --machine)
```

or:

```text
Machine: none (portable default resource paths)
```

### 9.3 `machine use`

Validates the named overlay exists and then persists local binding. It does not modify `profile.toml`, `machines/*.toml`, or `updated_at`.

### 9.4 `machine clear`

Removes the local binding. Subsequent commands use portable default paths unless `--machine` is supplied.

### 9.5 `machine map`

Effective machine is selected using normal precedence. No selected machine is an error:

```text
no machine selected; use `machine use <name>` or pass `--machine <name>`
```

Resource reference accepts `resource:<id>` and bare `<id>`.

Creating/updating a mapping:

- requires Resource ID currently exists;
- canonicalizes path;
- validates all effective Resource roots for overlap/profile-root conflicts;
- changes portable machine file only;
- updates profile `updated_at`;
- does not touch live Resource bytes.

### 9.6 `machine unmap`

Removes the selected overlay's mapping if present. The Resource falls back to `Resource.Path` immediately for later commands. No live bytes are moved or deleted.

## 10. Effective-root safety

Before a selected overlay is supplied to the Resources provider, resolve all current Resource roots.

Reject when any two effective roots overlap:

```text
resource A = /mnt/data
resource B = /mnt/data/project
```

Reject mappings whose effective root overlaps the active profile directory in either direction. Reject mappings that overlap Blueprint's own local state subtree (`<state-home>/omarchy-blueprint`) in either direction.

Run the existing Resources ownership index against every effective root. If `Ownership.TrackConflict(root)` returns semantic-provider claims, reject the mapping exactly as tracking that root would be rejected. A machine overlay is path context, not an ownership escape hatch.

Do not require mapped destination to exist. Mapping must be configurable before a restore.

Do not create mounts or parent directories during mapping. Normal restore planning decides which parent directories may be created.

Existing restore rules remain authoritative:

- differing existing Resource destinations are conflict/skip by default;
- force semantics remain bounded and do not gain new deletion authority;
- symlink-parent protections still apply;
- mapping cannot bypass provider ownership rules.

## 11. Resources provider integration

### 11.1 Provider configuration

Extend Resources provider with a path resolver or explicit overrides. Preferred shape:

```go
type Provider struct {
    // existing fields...
    ResourcePaths ResourcePaths
}
```

or an equivalent narrow resolver interface.

The provider exposes one internal helper:

```go
func (p Provider) resourceRoot(item profile.Resource) (string, error)
```

Every live-path operation must route through it.

### 11.2 Detection/status/diff

For selected machine `desktop`:

```text
saved Resource.Path = ~/Projects/site
mapping             = /mnt/fast/site
```

Detection inspects only `/mnt/fast/site`.

The absence/presence of `~/Projects/site` is irrelevant to drift while `desktop` is active.

Runtime/JSON Resource detail should include:

```json
{
  "id": "site",
  "default_path": "~/Projects/site",
  "effective_path": "/mnt/fast/site",
  "machine": "desktop"
}
```

Do not persist `effective_path` into `resources.toml`.

### 11.3 Capture

Capture reads state from effective roots for `copy`, `git`, and `git+diff`.

It updates hashes/revisions/patches/untracked metadata as today, while preserving:

```text
Resource.ID
Resource.Path
machine overlay mappings
```

Capture never learns a new mapping merely because bytes are found elsewhere.

### 11.4 Track

Tracking a new Resource retains existing v1 behavior: its portable path must be under HOME and becomes `Resource.Path`.

When an existing Resource is mapped on the selected machine, `track <effective-root> ...` may identify/update that same Resource by its effective root. It must not replace the portable `Resource.Path` with the mapping.

Tracking a brand-new external absolute path is not added in this slice; external placement is a mapping of an already portable Resource identity.

### 11.5 Check and verification

`check` validates:

- machine file syntax;
- duplicate names/mappings;
- mapping path grammar;
- effective-root overlaps for the selected machine;
- profile-root and Blueprint-state-root overlap;
- semantic-provider ownership conflicts on effective roots;
- normal Resource artifacts/integrity.

Dormant mappings are emitted as informational findings, not hard failure.

Verification uses effective roots exactly like detection.

## 12. ResourceLink integration

Resource links must be evaluated against effective Resource roots.

### 12.1 Inbound link

```text
~/.config/nvim -> resource:dotfiles/nvim
```

If `dotfiles` maps to `/mnt/config/dotfiles`, planning computes the target there and still writes a relative symlink from `~/.config/nvim`.

### 12.2 Internal link

If source Resource `scripts` and target Resource `dotfiles` are both mapped, source and target roots both resolve through the selected overlay before relative-link calculation.

### 12.3 Discovery

Link discovery/classification must use effective roots so an existing HOME symlink resolving into an externally mapped Resource is recognized as targeting that Resource.

The persisted link identity remains Resource IDs + relative paths only.

## 13. Restore planning

For missing mapped Resource:

```text
portable Resource.Path = ~/Projects/site
machine path           = /mnt/fast/site
```

plan operations target `/mnt/fast/site`.

This applies to:

- copy file/directory writes;
- Git clone destination;
- Git checkout;
- Git patch application repository;
- selected-untracked destination;
- dependent ResourceLink targets/sources.

Existing differing destination safety is unchanged. If `/mnt/fast/site` exists but is not exactly satisfied, plan conflict/skip rather than touching `~/Projects/site` or moving data automatically.

No operation is created to move content from default path to mapped path. A mapping changes desired placement for future detection/restore; it is not a filesystem move command.

## 14. Human and JSON output

Whenever machine selection affects Resource interpretation, output should make it visible.

Human header examples:

```text
Machine: desktop
```

or:

```text
Machine: none (portable default resource paths)
```

`tracked` should distinguish default and effective paths when they differ:

```text
projects  git  ~/Projects
  effective: /mnt/fast/projects (desktop)
```

JSON for status/diff/restore planning should include a stable machine context:

```json
{
  "machine": {
    "name": "desktop",
    "source": "binding"
  }
}
```

When no overlay is active:

```json
{
  "machine": {
    "name": "",
    "source": "default"
  }
}
```

Do not include local binding file paths or profile-root hashes in normal output.

## 15. Error behavior

Errors should be explicit and actionable:

```text
machine "desktop" does not exist
no machine selected; use `machine use <name>` or pass `--machine <name>`
resource "projects" is not tracked
machine "desktop" maps resource "projects" to invalid path "..."
machine "desktop" creates overlapping resource roots: projects and dotfiles
machine "desktop" maps resource "projects" into profile directory
machine "desktop" maps resource "projects" into Blueprint state directory
machine "desktop" maps resource "dotfiles" into path owned by config
bound machine "old-laptop" no longer exists; run `machine use <name>` or `machine clear`
```

Machine-selection errors occur before provider detection/restore so Blueprint never silently operates against default Resource paths when an explicitly requested/bound overlay is invalid.

## 16. Concurrency and persistence

Portable machine files use the same atomic-file-write posture as other small profile metadata.

Local binding writes use a separate atomic temp-file + fsync + rename under XDG state with restrictive permissions.

No machine command mutates Resource snapshot artifacts, so it does not participate in the Resources capture transaction.

`machine map` changes placement policy only. Concurrent profile writers are outside v1's broader locking model; do not add a second global profile lock solely for this slice.

## 17. Acceptance scenarios

### 17.1 Default behavior unchanged

Schema-10 profile migrated to 11 with no machines:

```text
status/capture/restore
→ same Resource paths and semantics as before
```

### 17.2 HOME-relative mapping

```text
resource:projects default ~/Projects
framework maps projects → ~/Code
framework selected
```

`status`, `capture`, `restore`, and verify use `~/Code`. `resources.toml` still says `~/Projects`.

### 17.3 External absolute mapping

```text
desktop maps projects → /mnt/fast/projects
```

A missing Resource restores there. Blueprint does not require `/mnt/fast/projects` to exist when mapping is defined.

### 17.4 Explicit override

Local binding = `framework`.

```bash
omarchy-blueprint --machine desktop status resources
```

uses `desktop` for this invocation and local binding remains `framework`.

### 17.5 Mapping-aware capture

Mapped Git Resource changes at effective path. Capture updates revision/overlay state but not default path or machine mapping.

### 17.6 Mapping-aware links

Target Resource maps outside HOME. Restore creates the correct relative inbound symlink to mapped target.

### 17.7 Conflict safety

Mapped destination already contains different unknown state. Restore reports conflict/skip; default path is not used as a fallback.

### 17.8 Overlap and ownership rejection

Two Resources map to ancestor/descendant roots. `machine map` fails before saving the overlay. Mapping into the profile directory, Blueprint XDG state subtree, or an effective root claimed by another semantic provider is likewise rejected before save.

### 17.9 Broken local binding

Binding references deleted/renamed machine file. Resource-sensitive command errors before detection and gives `machine use` / `machine clear` guidance.

### 17.10 Dormant mapping

Overlay contains a mapping for an untracked Resource ID. Load succeeds; `check` reports it informationally; other Resource mappings still work.

## 18. Manual acceptance workflow

Using a disposable profile and two temporary roots:

```bash
omarchy-blueprint --profile "$PROFILE" machine add laptop
omarchy-blueprint --profile "$PROFILE" machine map resource:dotfiles ~/Code/dotfiles
omarchy-blueprint --profile "$PROFILE" machine current
omarchy-blueprint --profile "$PROFILE" tracked
omarchy-blueprint --profile "$PROFILE" status resources
omarchy-blueprint --profile "$PROFILE" capture resources
omarchy-blueprint --profile "$PROFILE" restore resources --dry-run
```

Then create another overlay:

```bash
omarchy-blueprint --profile "$PROFILE" machine add desktop --no-use
omarchy-blueprint --profile "$PROFILE" --machine desktop machine map resource:dotfiles /tmp/desktop-dotfiles
omarchy-blueprint --profile "$PROFILE" --machine desktop restore resources --yes
```

Verify:

- default `Resource.Path` remains unchanged in `resources/resources.toml`;
- local binding remains `laptop` after explicit `--machine desktop` commands;
- desktop reconstruction occurs at mapped path;
- links target mapped Resource root;
- status/verify are clean under the correct overlay;
- clearing local machine reverts interpretation to portable default path;
- no machine ID/serial/hardware UUID appears in profile or local binding.

## 19. Future extension points

The explicit machine identity can later own additional sections without changing selection semantics:

```toml
name = "desktop"

[[resource_path]]
...

# future, not v1
[packages]
...

[monitors]
...
```

The TUI should consume the same machine-selection and path-mapping APIs rather than inventing a separate state model.
