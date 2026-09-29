# ADR 0024: Semantic User Service Reconstruction

## Status

Proposed

## Context

Omarchy Blueprint's core product promise is:

> I want this machine to behave like that machine.

Blueprint already reconstructs packages, themes, installed plugins, resources, configuration, shell state, hooks and Omarchy defaults through category-specific semantic mechanisms rather than blind filesystem snapshots.

Custom services are another important part of machine behavior.

Users, applications and agents can create persistent `systemd --user` units that:

- run background processes;
- schedule work;
- watch files and directories;
- listen for socket activation;
- group related user services;
- apply resource controls;
- customize externally owned services through drop-ins or masks.

A machine can therefore contain important user-controlled behavior that Blueprint does not yet preserve.

However, a systemd user manager also knows about units that Blueprint must not automatically treat as user-owned:

- package-provided units;
- Omarchy-provided units;
- application-installed user units;
- generated and transient units;
- runtime scopes;
- hardware-related units;
- units whose definitions live outside the portable machine state Blueprint currently manages.

The fact that a unit runs in `systemd --user` does not establish that Blueprint owns it.

Likewise, the fact that a unit is currently active does not mean that activity is persistent desired state. Runtime state and persistent service intent are different concepts.

Services v1 therefore needs to preserve meaningful user-controlled service behavior without turning Blueprint into:

- a snapshot of the user systemd namespace;
- a second package manager;
- a process checkpointing system;
- a system/root service manager;
- an implicit service launcher with hidden side effects.

The existing Blueprint safety lifecycle remains authoritative:

```text
inspect
→ calculate
→ preview
→ approve
→ re-inspect/recalculate
→ require stable authority
→ apply
→ verify
```

Services must participate in that lifecycle and in the existing Capture/Restore policy, Safe/Force conflict authority, Additive/Exact convergence, migration compatibility and CLI/TUI parity contracts.

## Decision

### 1. Services v1 manages persistent custom `systemd --user` intent

Services v1 introduces a first-class `Services` category for persistent user-service state.

Its purpose is to reconstruct user-controlled systemd behavior, not raw runtime observations.

Services v1 may manage:

- persistent unit definitions;
- persistent user-owned drop-ins;
- persistent enablement intent;
- persistent masks;
- configured template instances;
- explicit desired absence for previously Blueprint-managed service artifacts;
- an explicit post-Restore activation preference;
- captured runtime activity only as evidence used by that activation preference.

Services v1 does not make transient runtime state desired state.

Blueprint does not attempt to preserve:

- PIDs;
- uptime;
- current invocation IDs;
- current resource usage;
- exit codes;
- failed state;
- current timer countdowns;
- arbitrary process state.

### 2. Discovery never implies ownership

A unit becoming visible to Blueprint does not make it Blueprint-managed.

The ownership lifecycle is:

```text
discover
→ classify
→ present Capture candidate
→ user reviews and includes
→ Blueprint manages that selected intent
```

New top-level candidates are not silently adopted by normal Capture.

The first adoption of a service definition, overlay or mask requires reviewed user selection.

After adoption, future Capture follows normal machine-aware Capture policy:

- Capture Update may update managed desired state;
- Capture Preserve freezes existing desired state;
- Restore Apply may reconstruct managed intent;
- Restore Skip leaves it alone.

Plain non-reviewed Capture updates already managed Services targets but does not silently acquire newly discovered units.

### 3. Services has recommended and advanced persistent unit types

Services v1 recommends the common portable types:

- `.service`;
- `.timer`.

Services v1 also allows explicit advanced selection of:

- `.socket`;
- `.path`;
- `.target`;
- `.slice`.

Advanced units are visible only through an intentional expanded view and are not automatically acquired.

Blueprint does not manage these as Services v1 portable desired state:

- `.scope`;
- `.device`;
- `.mount`;
- `.automount`;
- `.swap`;
- generated units;
- transient units;
- runtime-only units;
- unknown unit kinds for which Blueprint has no deliberate semantic contract.

System/root units are outside Services v1.

### 4. Templates and instances are first-class

Systemd template units such as:

```text
backup@.service
```

are supported.

The template definition is captured once.

Configured instances such as:

```text
backup@photos.service
backup@projects.service
```

retain their own stable instance identity and persistent enablement intent without duplicating the template definition.

Instance-specific drop-ins remain separately managed artifacts under the relevant instance.

### 5. Blueprint distinguishes three ownership modes

Services v1 recognizes three important forms of service state.

#### Blueprint-managed definition

Blueprint manages:

- the selected persistent unit definition;
- selected persistent drop-ins;
- selected enablement/mask intent;
- selected template-instance intent;
- selected activation preference.

#### Blueprint-managed overlay on an external unit

Blueprint may manage user-owned customization of an externally owned base unit, including:

- user drop-ins;
- persistent user masks.

The external base definition remains unmanaged.

Blueprint must not copy, replace or delete the external base merely because it manages an overlay on that unit.

#### External unit

An external unit may appear as:

- dependency context;
- activation context;
- the base below a managed overlay;
- a compatibility/readiness dependency.

It is not Blueprint-owned desired state.

Package-, distro- and Omarchy-owned base units remain external.

### 6. Systemd supplies effective topology; approved persistent source files supply portable content

Blueprint must not recreate systemd's unit-loading rules through a blind recursive filesystem scan.

Read-only inspection uses systemd's effective unit topology and source information where available, including facts such as:

- effective fragment path;
- effective drop-in paths;
- unit kind;
- dependency relationships;
- enablement state;
- template/instance relationships;
- active-state evidence.

Blueprint then maps that effective state back to persistent source files.

Portable content is captured only from persistent user-controlled source files the user has approved.

Runtime/generated locations are never promoted into portable service definitions.

Filesystem location is ownership evidence, not absolute proof of authorship.

### 7. Discovery is provenance-aware

Candidate discovery classifies persistent sources conservatively.

Conceptually:

```text
strong custom candidate
    persistent user configuration
    no stronger external ownership evidence

ambiguous user-space candidate
    persistent user data/application location
    authorship not established

external
    package / Omarchy / distro / system-owned base

runtime/generated
    never a Capture source
```

A source under the user's configuration directory is a strong candidate, not automatic ownership.

A source installed into user data by an application may be surfaced as an advanced/ambiguous candidate.

External definitions are not offered as base-unit Capture candidates, although their user-owned overlays may be.

User-facing explanations must state why Blueprint classified a candidate the way it did.

### 8. Dependencies are recommendations, not acquired ownership

When a user selects a service target, Blueprint inspects the effective custom user-unit relationships relevant to reconstruction.

Related custom units are recommended as dependencies.

Recommended dependencies are preselected in the reviewed Capture flow once their parent is selected.

The user may deselect them.

A recommendation never silently becomes ownership without the reviewed Capture being approved.

If the user leaves a dependency unmanaged:

- Blueprint records no ownership over that dependency;
- the managed unit may still be captured;
- Restore reports the dependency as external;
- missing or incompatible dependencies may reduce or block activation or other affected operations.

Blueprint does not infer arbitrary executable/package dependencies from command strings in Services v1.

### 9. Unit files remain exact content; Blueprint reasons about them semantically

Blueprint does not round-trip systemd unit files through its own serializer.

Managed definitions and drop-ins retain their captured file content.

Separately, Blueprint maintains semantic metadata needed for safe reconstruction, including:

- stable unit identity;
- management/ownership mode;
- desired presence;
- unit kind;
- enablement intent;
- mask intent;
- template instances;
- activation preference;
- captured active-state evidence.

This hybrid model preserves authored systemd syntax while allowing semantic planning and verification.

### 10. Persistent enablement is richer than enabled/disabled

Blueprint normalizes persistent service intent into user-understandable concepts.

At minimum:

- **Starts automatically** — persistent enablement is desired.
- **Available but not automatic** — definition exists but persistent enablement is not desired.
- **Blocked from starting** — a persistent mask is desired.
- **Started by another service** — no direct writable enablement intent applies.

Raw systemd states may be shown as technical detail but are not the primary user-facing vocabulary.

Runtime-only enablement or mask states are observations, not portable persistent intent.

### 11. Persistent masks are manageable overlays

A persistent user mask is meaningful desired state.

Blueprint may manage a mask even when the underlying base service is externally owned.

Managing such a mask does not transfer ownership of the base unit.

Removing a previously Blueprint-managed mask may become explicit desired absence through reviewed Capture and may be applied under Exact when safe.

Unrelated masks remain untouched.

### 12. Linked units remain explicit dependencies

Units whose definitions live outside normal persistent user-unit configuration are discoverable as advanced candidates.

Blueprint must not blindly copy or recreate their source.

The linked source is treated as an explicit dependency.

If another Blueprint category, such as Resources, owns that source, Services may describe the relationship.

If the source is external, Restore requires that source to exist before it may safely recreate/use the link.

A missing external linked source must not become a dangling silent success.

### 13. Desired absence is explicit and bounded

A managed unit, drop-in, mask or configured instance becoming locally absent does not automatically erase desired state.

During reviewed Capture, Blueprint may offer:

```text
Removed locally
Record as no longer desired
```

Approving that change creates explicit desired absence for the exact previously managed target.

Under Restore:

- Additive does not remove artifacts solely because desired absence exists;
- Exact may remove the exact Blueprint-managed target when all safety and compatibility evidence allows it;
- Exact never deletes arbitrary unfamiliar services;
- Exact never deletes an externally owned base unit because a managed overlay disappeared.

Disabled is positive service state, not desired absence.

### 14. Additive/Exact governs absence, not positive service values

Positive semantic service intent still converges under normal Restore.

For an explicitly managed target, states such as:

- enabled;
- disabled;
- masked;

are values Blueprint may restore subject to safety/conflict rules.

Additive versus Exact primarily controls removal authority for explicit desired absence and obsolete managed service artifacts.

It does not redefine `disabled` as an absence tombstone.

### 15. Runtime activity is activation evidence, not convergence state

Blueprint may record that a managed unit was active when captured.

That observation is not a requirement that the unit remain continuously active.

Blueprint never tries to reproduce a process snapshot.

Captured runtime activity is used only as evidence for an explicit post-Restore activation decision.

An inactive source unit never becomes authority to stop a running destination unit.

### 16. Post-Restore activation is a separate explicit authority

Services v1 supports a post-Restore activation choice independent of:

- Capture policy;
- Restore Apply/Skip;
- Safe/Force;
- Additive/Exact;
- compatibility authority.

Conceptually the user can choose:

- **Restore working state** — activate eligible managed functionality that was active when captured;
- **Persistent state only** — reconstruct definitions and persistent systemd state but start nothing;
- **Review activation** — approve affected activation operations individually.

Per-target activation preferences may further narrow that behavior.

Activation requires the intersection of:

```text
captured active evidence
+ target activation preference
+ current Restore activation permission
+ provider safety
+ compatibility/readiness
```

No one of these grants authority by itself.

Interactive Restore may recommend restoring working state.

Headless/unattended Restore must not start units unless activation authority was explicitly granted for that run.

### 17. Activation is asymmetric

Services v1 may start an inactive eligible unit when activation was explicitly authorized.

Services v1 does not automatically:

- stop a unit because it was inactive at Capture time;
- restart an already-running unit because its definition changed;
- reload an arbitrary service process;
- turn Force into activation authority.

If a definition changes while its unit is already active, Blueprint warns that the running process may still reflect its previous configuration.

A future explicit restart/reload feature requires its own authority model.

### 18. Activation follows systemd semantics

Blueprint activates the appropriate systemd entry point rather than flattening relationships into a list of processes.

Examples:

```text
timer → service
socket → service
path → service
target → dependency group
```

If an active timer was captured, Blueprint may activate the timer rather than immediately starting its service.

Activation preview should surface known systemd dependency effects where practical.

Blueprint does not promise to infer arbitrary side effects inside `ExecStart` or external programs.

### 19. Restore uses systemd semantic operations

Services v1 reconstructs source content safely, then uses systemd for systemd semantics.

Conceptually:

```text
validate proposed persistent unit set
→ write/remove approved managed source files
→ systemctl --user daemon-reload
→ restore approved enablement/mask state
→ perform explicitly authorized activation
→ verify
```

Blueprint must not fake enablement by manually constructing systemd symlinks when a usable user manager is unavailable.

A usable systemd user manager/session is a Services Restore requirement.

Blueprint does not invoke `sudo`, enable lingering, or mutate system-wide service configuration to satisfy that requirement.

### 20. Proposed unit content is validated before mutation

Where the target system provides an authoritative non-mutating systemd verification mechanism, Blueprint uses it during planning/preflight.

A proposed definition that systemd cannot parse or meaningfully load becomes a visible blocking issue before mutation.

Validation itself must remain read-only.

### 21. Existing conflict and safety authority remains unchanged

Services participates in Blueprint's existing Safe/Force model.

Under Safe:

- an unexpected conflicting destination definition or managed artifact is preserved;
- the conflict is shown before mutation.

Force may resolve an explicitly reviewed conflict only where Services already has legitimate user-level mutation authority.

Force does not allow Blueprint to:

- replace a package-/Omarchy-/system-owned base unit;
- bypass secret/sensitivity safety;
- bypass compatibility;
- manage a system/root unit;
- start or restart units without activation permission.

Category safety wins.

### 22. Services participates deliberately in migration compatibility

Services must contribute explicit compatibility evidence to the shared Restore compatibility report.

It must not receive implicit Supported status merely because the provider exists.

Relevant evidence includes, as applicable:

- user-manager availability;
- target support for the selected unit semantics;
- presence of an external base required by a managed overlay;
- availability of linked external sources;
- dependency resolution;
- target-side unit validation.

Compatibility can leave authority unchanged, reduce it or block it.

It can never grant service mutation authority.

Unknown compatibility can never increase Exact deletion or activation authority.

### 23. Sensitive content is not blindly captured

Unit definitions and drop-ins can contain credentials or other sensitive values.

Services routes proposed captured content through Blueprint's existing sensitivity/secret handling rules.

In particular:

- inline sensitive material is not blindly accepted into a portable profile;
- references such as environment/credential files do not automatically cause those referenced files to be captured;
- uncaptured credential dependencies are surfaced as dependencies;
- Force does not override sensitivity safety.

Services v1 does not become a secret manager.

### 24. Verify proves the approved persistent intent

Services Verify uses the same effective intent as planning.

It verifies, where applicable:

- managed definition content;
- managed drop-in content;
- explicit desired absence;
- persistent masks;
- persistent enablement intent;
- template-instance intent;
- systemd's ability to load the resulting units;
- activation outcome only when activation was authorized.

Verify does not require a unit to be active when the user selected persistent-state-only Restore.

Activation verification must respect unit semantics; a successfully activated oneshot-like unit must not be declared failed merely because it is not a long-lived active process.

Incidental runtime state is never part of ordinary service convergence.

### 25. User-facing terminology explains consequences before systemd vocabulary

Services uses user-facing intent as the primary language.

Examples:

- `enabled` → **Starts automatically**
- `disabled` → **Available but not automatic**
- `masked` → **Blocked from starting**
- `static` / `indirect` → **Started by another service**
- `linked` → **Definition stored elsewhere**
- `active` → **Currently running**
- managed external overlay → **Managed customization**
- external base → **External service**

Raw systemd terminology may appear as secondary technical detail.

Recommendations and warnings must include an understandable reason.

### 26. CLI and TUI use the same workflow/domain result

Services is a normal Blueprint category.

CLI and TUI share:

- candidate discovery;
- selection semantics;
- policy resolution;
- service planning;
- dependency recommendations;
- compatibility;
- activation planning;
- verification.

The TUI must not independently calculate service ownership or safety.

Reviewed CLI Capture provides the non-TUI interactive path for first adoption.

JSON preview exposes the same machine-readable candidate/plan facts without mutating ownership.

### 27. Services v1 remains strictly user-level

Services v1 does not manage:

- `systemd --system`;
- root-owned system units;
- `/etc/systemd/system`;
- firewall configuration;
- privileged ports/configuration;
- package installation merely because a service references a binary;
- Omarchy service recipes as a bundled abstraction;
- system-wide lingering configuration.

A future system/root Services tranche is a separate high-risk design problem.

## Profile representation

The exact Go structs may be refined during implementation, but the profile contract is human-readable and conceptually separates semantic metadata from exact authored content.

A likely layout is:

```text
services/
├── services.toml
└── units/
    ├── backup.service
    ├── backup.timer
    ├── backup.service.d/
    │   └── 10-environment.conf
    └── pipewire.service.d/
        └── 10-custom.conf
```

`services.toml` records semantic intent such as:

- unit identity;
- management mode;
- desired presence/absence;
- normalized enablement/mask intent;
- configured template instances;
- activation preference;
- captured active-state evidence.

Definitions and drop-ins remain ordinary readable systemd files.

An external base definition is not copied into `services/units/` merely because Blueprint manages a drop-in or mask for it.

## Consequences

### Positive

- Blueprint reconstructs an important class of user-controlled machine behavior.
- The category preserves semantic service intent rather than snapshotting a directory.
- Discovery remains convenient without silently transferring ownership.
- Package/Omarchy units stay protected.
- Drop-in-only customization becomes portable without copying external bases.
- Advanced systemd users can preserve sockets, paths, targets and slices explicitly.
- Template services are represented naturally.
- Exact remains bounded to explicit Blueprint-managed absence.
- Persistent state and runtime activation remain separate authorities.
- Users can choose whether reconstruction should also restore working functionality immediately.
- CLI and TUI can explain service behavior in approachable language.
- Migration Safety applies naturally to the new category.
- The profile remains human-readable.

### Negative

- Services needs more classification logic than a filesystem-copy provider.
- Some ownership cases will remain ambiguous and require user review.
- Dependency discovery cannot prove arbitrary dependencies embedded in service programs.
- An external dependency may make a captured service only partially reconstructable.
- Activation adds side-effect authority that must be previewed and tested independently.
- Systemd has many raw unit/enablement states that Blueprint must normalize carefully.
- Advanced users may encounter unsupported unit types in v1.
- A changed running service may require a manual restart after Restore.
- Captured activity evidence adds contextual state to the profile that is intentionally not desired convergence state.

## Rejected alternatives

### Snapshot `~/.config/systemd/user`

Rejected because path contents alone do not establish ownership or effective systemd semantics, and a blind snapshot cannot safely reason about external bases, links, generated state, enablement, masks or dependencies.

### Automatically manage every apparent custom user unit

Rejected because applications and agents can create persistent user units. Discovery is not enough evidence to transfer ownership to Blueprint.

### Require an explicit `service track` command before discovery

Rejected as the primary UX because it weakens Blueprint's “running system is the configuration editor” model. Reviewed Capture candidates preserve that model while keeping adoption explicit.

### Support only `.service` and `.timer`

Rejected because `.socket`, `.path`, `.target` and `.slice` can encode legitimate portable user intent. They remain advanced explicit selections instead.

### Manage every systemd user unit type

Rejected because several unit kinds represent transient runtime state, hardware or other domains that Services v1 should not own.

### Copy package/Omarchy unit definitions when customized

Rejected because a user-owned drop-in or mask does not imply ownership of its external base definition.

### Treat current active/inactive state as desired convergence

Rejected because runtime activity is transient. Blueprint stores activity only as activation evidence.

### Never activate anything after Restore

Rejected because a technically correct persistent restore may still leave important user functionality apparently missing until later activation.

### Automatically start/restart everything

Rejected because service activation has side effects and restart authority is materially more disruptive than persistent reconstruction.

### Use Force as activation or compatibility override

Rejected because conflict authority, compatibility and runtime activation are independent safety dimensions.

### Manually recreate systemd enablement symlinks without a user manager

Rejected because Blueprint should use systemd's own semantic mechanisms rather than emulate them when the target environment cannot provide them.

### Include system/root services in v1

Rejected because privileged service management has a substantially larger ownership, security and recovery surface and requires a separate design.

## References

- ADR 0012: Portable Resources
- ADR 0013: Baseline-Aware Config Overlay
- ADR 0020: CLI policy and Capture parity
- ADR 0021: Real-Omarchy Reconstruction Assurance
- ADR 0022: Fresh-package readiness and elevated Restore
- ADR 0023: Plan-Integrated Migration Compatibility Assessment
- `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`
- `docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md`
