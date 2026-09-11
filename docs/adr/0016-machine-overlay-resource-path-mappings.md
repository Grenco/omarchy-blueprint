# ADR 0016: Machine Overlay Identity and Resource Path Mappings

## Status

Accepted

## Context

Omarchy Blueprint profiles are intended to reconstruct the same portable user intent across different machines. Portable Resources already have stable IDs, portable state, semantic ResourceLinks, and three reconstruction strategies (`git`, `git+diff`, and `copy`). The remaining path assumption is that every Resource is reconstructed at its captured `Resource.Path`.

That assumption is too strong for multi-machine profiles. A Resource captured as:

```text
resource:projects
path = ~/Projects
```

may intentionally live at:

```text
~/Code
```

on a laptop and:

```text
/mnt/fast/projects
```

on a desktop.

ADR 0012 deliberately made ResourceLinks refer to a Resource ID plus a path relative to that Resource so later path remapping could change the Resource root without rewriting semantic links.

The roadmap also anticipates machine overlays and path mappings as the first machine-specific layer over a portable base profile.

Machine identity itself needs to be stable user intent. Hostnames are convenient suggestions but may change. Hardware model/vendor data describes a class of machine, not a unique stable identity. Installation identifiers such as `/etc/machine-id` are local implementation details and must not become portable profile identity.

## Decision

Add schema-11 Machine Overlays v1 with **whole-Resource-root path mappings only**.

Machine overlays have explicit, user-controlled names such as:

```text
framework
desktop
work-laptop
```

Blueprint may suggest a machine name from the current hostname, but the suggestion is never authoritative identity. Hardware/vendor/model metadata is out of scope for v1 identity and may later be shown as a non-authoritative UX hint.

Portable machine overlays are stored under:

```text
machines/<name>.toml
```

A v1 overlay contains only Resource path mappings:

```toml
name = "desktop"

[[resource_path]]
resource = "projects"
path = "/mnt/fast/projects"

[[resource_path]]
resource = "dotfiles"
path = "~/dotfiles"
```

`Resource.Path` remains the portable/default path and retains its existing `~/...` contract. A machine mapping overrides only the physical root used on the selected machine. It does not duplicate or override Resource strategy, revision, hashes, Git overlay state, or ResourceLinks.

Path resolution is:

```text
explicit --machine NAME
        ↓
locally bound machine for this profile
        ↓
no machine overlay
```

Then for each Resource:

```text
selected overlay has mapping for Resource.ID
        ? mapping path
        : Resource.Path
```

Machine mapping paths may use either:

```text
~/...
/absolute/path
```

Relative paths, bare `~`, bare `/`, environment-variable expansion, and `~user` syntax are rejected. CLI writes canonicalize absolute paths inside the current HOME back to `~/...`; external absolute paths remain absolute.

Only whole Resource roots may be mapped in v1. Subtree mappings, mount composition, conditional expressions, per-provider config overlays, and path templating are deferred.

## Local machine binding

The selected machine for a profile is local state, not portable profile state.

Blueprint stores a per-profile binding below XDG state, keyed by the canonical absolute profile directory. Conceptually:

```text
$XDG_STATE_HOME/omarchy-blueprint/profiles/<sha256(profile-root)>/machine.toml
```

The binding records the selected machine name and is mode `0600`. No hardware serial, product UUID, raw machine ID, or other fingerprint is stored.

Moving the profile directory may therefore lose the convenience binding; it does not change the portable profile or machine overlays.

An explicit `--machine NAME` overrides the local binding for one invocation and never changes the stored binding.

No machine selection is also valid and means “use portable/default Resource paths.” Commands that plan or report Resource state must make this explicit when machine overlays exist.

## CLI consequences

Machine management begins with:

```bash
omarchy-blueprint machine add [name]
omarchy-blueprint machine list
omarchy-blueprint machine current
omarchy-blueprint machine use <name>
omarchy-blueprint machine clear
omarchy-blueprint machine map resource:<id> <path>
omarchy-blueprint machine unmap resource:<id>
```

`machine add` accepts an explicit name. When omitted, Blueprint derives a conservative suggestion from the hostname and uses it as the new name. The command binds the newly created overlay to the current profile unless `--no-use` is supplied.

`machine map` and `machine unmap` operate on the effective machine selected by `--machine` or local binding and fail when there is no selected machine.

The global form:

```bash
omarchy-blueprint --machine desktop status
omarchy-blueprint --machine desktop diff resources
omarchy-blueprint --machine desktop capture resources
omarchy-blueprint --machine desktop restore resources
```

uses that overlay only for the current invocation.

## Resource semantics

A machine path mapping changes **placement only**.

For every Resource strategy:

```text
git      → remote + exact HEAD restored at effective machine path
git+diff → remote + HEAD + local overlay restored at effective machine path
copy     → captured filesystem content restored at effective machine path
```

Capture and detection read the effective machine path, but persisted `Resource.Path` remains the portable/default path.

Changing a machine mapping does not mutate live Resource bytes. Restore remains conservative: a differing existing destination at the newly mapped path is a conflict and is not overwritten by default.

The old/default Resource path is unmanaged while another machine path is active. Its presence or absence does not create drift for that selected machine.

## ResourceLink semantics

ResourceLinks continue to resolve through Resource IDs.

If:

```text
link source: ~/.config/foo
link target: resource:dotfiles / foo
```

and machine `desktop` maps:

```text
resource:dotfiles → /mnt/data/dotfiles
```

then restore computes the link target from `/mnt/data/dotfiles/foo`.

Internal links whose source itself belongs to a mapped Resource likewise use the mapped source root.

The mapping is not copied into the ResourceLink and the ResourceLink remains portable.

## Validation and safety

Machine names use the same conservative identifier character set as Resource IDs: letters, digits, `.`, `_`, and `-`; empty, `.` and `..` are invalid.

A mapping created through the CLI must reference an existing Resource ID.

For an effective machine selection, all Resource roots are resolved before provider work begins. The resolved roots must not overlap one another. A mapping that would make two Resources equal, ancestor/descendant, or otherwise overlap is rejected.

Mapped roots must not overlap the active profile directory or Blueprint's own XDG state subtree for the current user. Existing provider ownership claims continue to apply to effective roots: a mapping that would place a generic Resource inside a path owned by Config, Hooks, Themes, Plugins, Shell, Defaults, or another semantic provider is rejected using the same Resources tracking-conflict rules. Machine mappings do not grant permission to overwrite unknown target state or bypass semantic-provider ownership.

A manually authored overlay entry that references a missing Resource is retained as dormant policy but has no effect until that Resource ID exists again. `check` reports it as an informational orphan rather than making the whole profile unloadable. `machine unmap` can remove it. This avoids coupling Resource untrack transactions to unrelated machine-overlay file rewrites.

## Schema and compatibility

The profile schema increases from 10 to 11.

Older binaries must reject schema 11 rather than silently ignore `machines/*.toml` and restore Resources to their default paths.

Schema 10 → 11 migration creates no machine overlays and no local binding. Existing Resource paths and behavior remain unchanged until the user creates/selects an overlay.

## Non-goals for v1

This ADR does not add:

- machine-specific packages;
- monitor or hardware configuration;
- provider-specific machine overlays;
- conditional rules based on vendor/model/chassis;
- automatic machine fingerprint matching;
- subtree Resource path mappings;
- automatic mount creation;
- machine overlay inheritance;
- machine rename/delete workflows;
- path variables beyond `~/...` expansion;
- TUI screens.

Those can build on the same explicit machine identity later.

## Consequences

### Positive

- one portable profile can reconstruct the same Resource identity at different machine-specific roots;
- Resource state remains single-source and portable;
- existing ResourceLink indirection pays off without schema duplication;
- no hardware fingerprint becomes profile identity;
- local machine selection avoids Git churn between machines;
- `--machine` makes automation and fresh-machine restore explicit;
- the model is ready for a later TUI and broader machine-specific state.

### Negative

- Resource path resolution becomes contextual and every Resource read/plan path must use the same resolver;
- local profile bindings add XDG state outside the portable profile;
- absolute machine paths may depend on target mount availability;
- users must explicitly choose/create machine overlays; v1 does not infer identity automatically.

## Rejected alternatives

- **Hostname as authoritative identity:** too easy to rename and not reliably stable.
- **Hardware fingerprint as identity:** creates privacy, replacement, VM-cloning, and ambiguity problems.
- **`/etc/machine-id` as portable identity:** installation-local and unsuitable for profile identity.
- **Store current machine in `profile.toml`:** causes multiple machines using the same Git profile to fight over one mutable selection.
- **Put machine mappings directly on each Resource:** scatters machine policy through portable Resource state and makes future machine-specific concerns harder to group and review.
- **Subtree mappings in v1:** turns simple placement into filesystem composition and substantially complicates conflict/ownership semantics.
