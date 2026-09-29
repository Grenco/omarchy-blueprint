# Services v1 Design

## Status

Proposed.

This design follows ADR 0024 and the approved conversational architecture review for the Services v1 roadmap tranche.

It assumes Blueprint's existing safety lifecycle, machine-aware Capture/Restore policy, Safe/Force conflict authority, Additive/Exact convergence, plan-stability checks, Reconstruction Assurance and Migration Safety architecture remain authoritative.

This document is a design specification, not an implementation plan. No production implementation or PR decomposition is authorized by this document alone.

## Summary

Services v1 adds semantic reconstruction of persistent custom `systemd --user` behavior.

The goal is not:

> copy my systemd directory.

It is:

> preserve the user-controlled service behavior that makes this machine behave like mine, without taking ownership of service state that belongs to Omarchy, packages, applications or the runtime.

The core model is:

```text
read-only user-manager inspection
        ↓
discover persistent candidates
        ↓
classify ownership/provenance
        ↓
reviewed user selection
        ↓
capture persistent service intent
        ├─ exact unit definition content
        ├─ exact selected drop-ins
        ├─ persistent enablement / mask intent
        ├─ template-instance intent
        ├─ explicit desired absence
        ├─ activation preference
        └─ active-at-capture evidence
        ↓
semantic Restore plan
        ↓
validate
        ↓
preview / approve
        ↓
re-inspect / recalculate
        ↓
require stable authority
        ↓
restore persistent content
        ↓
systemd daemon-reload
        ↓
restore systemd persistent state
        ↓
optionally activate approved working state
        ↓
verify the same approved intent
```

Services v1 remains entirely user-level.

System/root services are explicitly out of scope.

## Product goal

A user should be able to customize their machine naturally—for example by creating a background service, timer or path watcher—and later have Blueprint discover that behavior and offer to preserve it.

The user should not need to understand every systemd implementation detail to answer:

- What does this service do at a high level?
- Why is Blueprint recommending it?
- Is Blueprint managing the whole service or only my customization?
- Will it start automatically?
- Will Restore launch it immediately?
- What other units does it depend on?
- What will Blueprint overwrite or delete?
- What is external and therefore not guaranteed by this profile?

An advanced user should still be able to inspect the underlying unit names, paths and raw systemd states.

## Goals

1. Add a first-class `Services` Blueprint category.
2. Preserve persistent custom `systemd --user` behavior.
3. Discover useful service state without silently acquiring ownership.
4. Make first adoption a reviewed Capture decision.
5. Support custom unit definitions and user-owned overlays on external units.
6. Support `.service` and `.timer` as recommended unit types.
7. Allow explicit advanced selection of `.socket`, `.path`, `.target` and `.slice`.
8. Support systemd templates and configured instances.
9. Preserve exact unit/drop-in content without writing a competing systemd parser/serializer.
10. Model persistent enablement, disablement and masking semantically.
11. Preserve meaningful user masks on externally owned services.
12. Support explicit desired absence only for previously managed service state.
13. Keep Exact deletion authority tightly bounded.
14. Recommend related custom unit dependencies without silently owning them.
15. Treat current runtime activity as evidence, not desired convergence.
16. Allow users to choose whether Restore should activate previously working service functionality.
17. Never infer inactive-at-capture as authority to stop something.
18. Avoid automatic restart/reload of running application processes in v1.
19. Use systemd's semantic operations for reload, enablement, masking and activation.
20. Validate proposed unit content before mutation when authoritative read-only validation is available.
21. Integrate Services with Migration Safety deliberately.
22. Preserve CLI/TUI parity.
23. Keep profiles portable and human-readable.
24. Extend Reconstruction Assurance with deterministic Services coverage before Services v1 is considered complete.

## Non-goals

Services v1 does not add:

- root/system service management;
- `/etc/systemd/system` ownership;
- privileged service installation;
- automatic `sudo`;
- automatic `loginctl enable-linger`;
- firewall management;
- package installation inferred from `ExecStart`;
- arbitrary dependency provisioning;
- Omarchy service-recipe orchestration;
- process checkpoint/restore;
- PID preservation;
- uptime preservation;
- current timer countdown preservation;
- runtime failure-state preservation;
- automatic restart of changed running services;
- automatic stop of destination services;
- service log capture;
- secret management;
- automatic capture of credential/environment files referenced by units;
- support for `.scope`, `.device`, `.mount`, `.automount` or `.swap` as Services desired state;
- automatic ownership of every unit found under a user-writable path;
- cross-category dependency solving;
- systemd replacement/emulation.

## Product principles

### Running system as editor, reviewed ownership as authority

Blueprint should discover the configuration the user created through normal machine use.

That convenience stops at the ownership boundary.

A discovered unit is a candidate, not desired state.

The reviewed Capture is the moment the user grants Blueprint ownership over the selected service intent.

### Persistent intent before runtime observation

The portable profile is primarily about:

- definitions;
- customizations;
- persistent enablement;
- masks;
- configured instances;
- explicit desired absence.

Whether a service happened to be active is contextual evidence used for optional activation after Restore.

It is not ordinary desired-state convergence.

### External ownership stays external

A user customization layered over an external service does not convert that service into a Blueprint-owned definition.

This applies to:

- drop-ins;
- masks;
- dependency relationships;
- linked external sources.

### Semantic operations over filesystem tricks

Blueprint may store authored files exactly, but it uses systemd to perform systemd semantics.

In particular, Blueprint must not substitute hand-created enablement symlinks for systemd's enable/disable/mask semantics.

### Explain consequences in user language

Raw systemd terminology is secondary.

The primary UX describes behavior:

```text
Starts automatically
Available but not automatic
Blocked from starting
Started by another service
Currently running
Definition stored elsewhere
Managed customization
External service
```

Every recommendation, warning or protection should have a short reason.

## Domain boundaries

### Services category

`Services` is a normal Blueprint category participating in:

- Capture Update/Preserve;
- Restore Apply/Skip;
- profile/machine policy inheritance;
- Safe/Force;
- Additive/Exact;
- Restore plan approval stability;
- compatibility;
- journaled Restore;
- Verify;
- CLI/TUI parity.

Services does not reinterpret any of those existing concepts.

### Stable target identity

The stable top-level identity is the systemd user-unit name.

Examples:

```text
backup.service
backup.timer
backup@.service
backup@photos.service
watch-downloads.path
```

The implementation may prefix the category internally, for example:

```text
user:backup.service
```

but the user-facing identity remains the normal unit name.

A managed unit identity may contain subordinate service state:

```text
unit definition
drop-ins
persistent enablement
persistent mask
template instances
activation preference
```

Subordinate file artifacts also require deterministic internal identities so Exact and verification can refer to one exact managed object without broad deletion authority.

### Management modes

Each selected unit is classified into one of these management modes.

#### Managed definition

Blueprint owns the selected persistent definition itself.

It may additionally own selected:

- drop-ins;
- enablement/mask state;
- instances;
- activation preference.

#### Managed customization

The base unit is external.

Blueprint owns only selected user-layer intent such as:

- drop-ins;
- persistent user mask;
- optionally user-level enablement intent where safe and explicitly selected.

The external definition is never stored as Blueprint-owned content.

#### External context

Blueprint has no ownership.

The unit may still be visible because it:

- is a dependency;
- is activated by/activates a managed unit;
- provides the base below an overlay;
- is required for compatibility.

External context never becomes removal authority.

## Unit-type support

### Recommended

The primary Services v1 experience supports:

```text
.service
.timer
```

These appear in the normal candidate view.

### Advanced selectable

The user may expand advanced Services candidates and explicitly select:

```text
.socket
.path
.target
.slice
```

Advanced candidates:

- are clearly labeled;
- include a plain-language explanation of their effect;
- start unselected;
- receive the same ownership and safety treatment as recommended units once explicitly included.

### Excluded from portable Services desired state

Services v1 does not capture:

```text
.scope
.device
.mount
.automount
.swap
```

or generated/transient unit state.

Unknown unit kinds remain unmanaged until Blueprint has an explicit semantic design for them.

## Discovery

### Discovery source of truth

Services discovery starts from the effective user-manager view rather than a directory crawl.

Read-only inspection should obtain, where supported:

- known user units;
- effective fragment path;
- effective drop-in paths;
- load state;
- unit kind;
- enablement state;
- active-state evidence;
- template/instance relationships;
- effective systemd dependency relationships relevant to reconstruction.

Blueprint maps those facts back to persistent sources.

### Persistent-source classification

Conceptually, source paths fall into four classes.

#### Strong custom candidate

A persistent definition/customization lives in user configuration space and Blueprint has no stronger evidence that it is externally owned.

Example UX:

```text
backup-projects.service

Source
  ~/.config/systemd/user/backup-projects.service

Recommendation
  Custom user service

[ ] Manage this service
```

The checkbox begins unselected because discovery still does not imply ownership.

#### Ambiguous user-space candidate

A persistent source lives in user space but may have been installed or maintained by an application.

Example:

```text
syncthing-notify.service

Source
  ~/.local/share/systemd/user/syncthing-notify.service

Recommendation
  Leave externally managed unless you intentionally own this definition

[ ] Manage definition anyway
```

The actual roots are resolved using the target's user/XDG configuration rather than assuming one literal home path.

#### External

The effective definition belongs to package/distro/Omarchy/system state.

It is not offered as a base-definition Capture candidate.

User-owned drop-ins or masks layered above it may be offered independently.

#### Runtime/generated

Runtime, generated and transient sources are excluded from Capture.

They may contribute inspection context only.

### Ownership classification is evidence-based

Path alone must not be described as proof that a human authored a unit.

Blueprint may combine available evidence such as:

- source location;
- package ownership where authoritative;
- known Omarchy ownership;
- generated/transient classification;
- whether the source is a link;
- relationship to an externally owned base.

When authorship remains uncertain, the UX says so.

Unknown provenance never silently becomes Blueprint ownership.

## Capture candidate model

### New top-level candidates

New discovered units are presented during reviewed Capture.

They begin unselected.

This keeps the first acquisition explicit even when Blueprint strongly recommends the unit.

The UI may provide convenience actions such as:

```text
Select recommended
Show advanced
```

but a plain unattended Capture never silently acquires new service targets.

### Dependency recommendations

Once the user selects a candidate, Blueprint identifies relevant custom persistent unit dependencies.

Those dependencies are shown beneath the selected target and are preselected by default.

Example:

```text
[x] watch-downloads.path

    Recommended related units
    [x] organize-downloads.service
        Activated by this path

    [x] background.slice
        Resource group used by organize-downloads.service
```

The user can turn any recommendation off.

Turning one off means:

> do not make this dependency Blueprint-managed.

It does not rewrite the selected unit to remove the dependency.

### External dependency choice

If the user deliberately leaves a dependency external, Capture remains valid when the managed unit itself is safe to capture.

The profile's managed-target set expresses the ownership decision.

The dependency graph is recalculated from unit semantics rather than treated as a separate source of mutation authority.

Restore may later report:

```text
organize-downloads.service depends on background.slice

background.slice is not managed by this profile.
It must already exist on this machine.
```

### Dependency scope

Services v1 reasons about systemd unit relationships that are authoritative enough to obtain from the user-manager/unit semantics.

It does not try to infer arbitrary application dependencies such as:

- binaries named by `ExecStart`;
- network services contacted by the program;
- packages that may contain an executable;
- files an arbitrary script happens to read.

Known cross-category relationships may be surfaced when obvious, but Services v1 is not a general dependency solver.

## Templates and instances

Template units are normal managed definitions.

Example:

```text
backup@.service
```

The definition is stored once.

Configured instance intent is represented separately:

```text
backup@photos.service
backup@projects.service
```

An instance may have:

- persistent enablement intent;
- activation evidence/preference;
- instance-specific drop-ins.

Blueprint does not duplicate the template bytes into each instance.

Dependency and activation explanations should preserve the normal systemd instance name so an advanced user can map Blueprint output directly to systemd.

## Managed customizations on external units

### Drop-ins

Blueprint may manage selected user-owned drop-ins while leaving the base definition external.

Example:

```text
External service
  pipewire.service

Managed customization
  ~/.config/systemd/user/pipewire.service.d/10-custom.conf
```

Capture stores only the selected custom drop-in.

Restore requires the external base to be meaningfully available when that fact is required for the selected intent.

Blueprint never fills in a missing external base by copying a captured package-owned definition.

### Persistent masks

A persistent user mask is supported as managed customization.

Example UX:

```text
vendor-agent.service

Blocked from starting

Why:
You explicitly prevented this external service from being activated.
Blueprint can preserve that preference without managing the service itself.

Systemd detail:
masked
```

A mask is not represented as deletion of the external base.

### User-level enablement on external units

Where systemd semantics and provider safety make it meaningful, Blueprint may preserve explicitly selected user-level persistent enablement intent for an external base without owning the base content.

This remains subject to compatibility and base availability.

Blueprint must not claim full service reconstruction if the external base is absent.

## Exact authored content

Definitions and drop-ins are stored byte-faithfully as portable authored content.

Blueprint does not attempt to serialize a parsed AST back into systemd syntax.

Semantic inspection exists alongside that content.

This avoids changing:

- comments;
- formatting;
- directive ordering;
- valid syntax Blueprint does not itself understand.

Systemd remains the ultimate parser/semantic authority.

## Proposed profile layout

The exact Go types and final field spelling may change during implementation, but the profile should remain straightforward to inspect and edit.

Conceptually:

```text
services/
├── services.toml
└── units/
    ├── backup.service
    ├── backup.timer
    ├── backup.service.d/
    │   └── 10-environment.conf
    ├── backup@.service
    └── pipewire.service.d/
        └── 10-custom.conf
```

`services.toml` records semantic metadata.

Conceptually:

```toml
[[unit]]
name = "backup.service"
management = "definition"
presence = "present"
enablement = "enabled"
activation_preference = "restore-working-state"
observed_active = true
definition = "units/backup.service"

[[unit]]
name = "backup.timer"
management = "definition"
presence = "present"
enablement = "enabled"
activation_preference = "restore-working-state"
observed_active = true
definition = "units/backup.timer"

[[unit]]
name = "pipewire.service"
management = "customization"
presence = "present"
enablement = "not-managed"
activation_preference = "persistent-only"

dropins = [
  "units/pipewire.service.d/10-custom.conf",
]
```

This is illustrative rather than a frozen serialization schema.

Architectural requirements are:

- metadata and authored content are separate;
- user files remain readable;
- external base definitions are not copied;
- desired absence is explicit;
- runtime evidence is distinguishable from desired state.

## Persistent state model

### Definition presence

For a Blueprint-managed definition:

```text
present
explicitly absent
```

`explicitly absent` exists only after reviewed removal of previously managed state.

An unmanaged missing candidate does not create a tombstone.

### Drop-in presence

Each Blueprint-managed drop-in independently supports:

```text
present
explicitly absent
```

This allows Exact to remove one retired Blueprint-managed customization without granting authority over neighboring external files.

### Enablement

Blueprint normalizes persistent direct enablement to user-facing intent.

#### Starts automatically

Blueprint intends the relevant persistent user enablement.

#### Available but not automatic

The definition remains available, but Blueprint intends it not to have direct persistent enablement.

This is positive desired state, not desired absence.

#### Started by another service

Direct enable/disable does not meaningfully apply to the unit.

The unit's role is explained through activation/dependency relationships instead.

### Masks

`Blocked from starting` is positive persistent intent.

A managed mask can exist:

- on a Blueprint-owned definition;
- as a user customization over an external definition.

Runtime-only masks are not persisted.

### Raw systemd state

Raw `systemctl` state may be retained inside current inspection/planning evidence where useful.

The profile contract uses Blueprint's normalized intent rather than promising that every raw systemd state string is a stable portable enum.

## Capture policy

### Capture Update

For an already managed Services target, Capture Update may update:

- definition content;
- selected drop-ins;
- persistent enablement/mask intent;
- template-instance state;
- activation evidence/preference.

A newly discovered unmanaged service is still not automatically adopted.

### Capture Preserve

Capture Preserve freezes the existing desired Services state.

Current-machine changes remain visible as differences/candidates but do not overwrite the profile.

### Reviewed removal

When an already managed artifact is missing locally, reviewed Capture may offer:

```text
backup.timer
Removed locally

[x] Record as no longer desired
```

Approval records explicit desired absence.

Rejecting that candidate preserves the existing desired state.

Capture Update alone must not infer that any temporary missing service should become a deletion tombstone.

## Restore policy

Restore Apply/Skip uses the existing machine-aware policy model.

Restore Skip cannot be overridden by:

- Force;
- Exact;
- activation options.

A skipped Services target contributes no service mutation authority.

## Additive and Exact

### Additive

Additive may:

- create missing desired managed definitions;
- update managed definitions where conflict policy allows;
- create/update managed drop-ins;
- converge positive enablement/mask state;
- restore configured managed instances;
- perform explicitly authorized activation.

Additive does not apply explicit desired-absence removals.

### Exact

Exact includes Additive behavior and may additionally remove exact service state represented as explicit desired absence, provided:

- the target was previously Blueprint-managed;
- current ownership remains safe;
- compatibility evidence does not reduce that authority;
- the operation does not target an external base.

Exact never means:

> make the entire destination systemd user namespace equal to the profile.

Unfamiliar/unmanaged units are outside Exact authority.

## Safe and Force

### Safe

Safe preserves unexpected conflicting destination state.

Examples include:

- a different unmanaged definition already exists at the managed unit name;
- a Blueprint-managed drop-in path now contains unexpected content;
- destination mask/enablement state reflects a conflicting user customization that Services cannot safely overwrite without explicit conflict authority.

The plan explains the conflict and does not mutate that target.

### Force

Force may resolve reviewed conflicts only inside the authority Services already legitimately owns.

Force may not:

- overwrite a package-/Omarchy-/system-owned base definition;
- convert an external unit into a managed definition;
- bypass sensitivity rules;
- bypass compatibility blockers/reductions;
- authorize Exact deletion;
- authorize activation;
- authorize restart/stop;
- manage root/system units.

Conflict authority remains only conflict authority.

## Linked unit definitions

A linked unit whose source is outside the normal persistent user-unit configuration is an advanced candidate.

Example:

```text
my-agent.service

Definition stored elsewhere
  ~/Projects/agent/systemd/my-agent.service
```

Blueprint does not silently copy that source into the Services profile.

Instead it records/surfaces the source relationship.

### Blueprint-managed source

If another Blueprint category owns the linked source, Services can surface that relationship.

For example:

```text
Resource: agent-project
    provides
my-agent.service
```

Services still does not take ownership of the Resource's content.

### External source

If the linked source remains external, Restore may only recreate/use the link when the source exists and passes the relevant safety/compatibility checks.

A missing source produces a requirement, reduced authority or blocker appropriate to the selected operation.

Blueprint does not deliberately create a dangling link and report successful reconstruction.

## Sensitivity and credential references

Systemd unit content can contain sensitive material.

Examples include inline environment values or command arguments containing credentials.

Services must use Blueprint's existing sensitivity contract before portable content becomes desired profile state.

Services v1 does not invent a “Force capture secrets” bypass.

### Referenced secret files

A directive that references another file does not automatically enroll that file into Blueprint.

Examples may include:

- environment files;
- credential sources;
- application-specific configuration.

Blueprint may explain:

```text
This service refers to a file that this Services target does not manage.
```

If that file is managed elsewhere by Blueprint, the UI may link the relationship.

If not, it remains an external dependency.

Services is not a secret manager.

## Runtime evidence

### Active-at-capture

For an approved managed target, Blueprint may persist that the unit was active during Capture.

This field is explicitly contextual evidence.

It does not mean:

```text
desired_active = true forever
```

and it is not part of Additive/Exact convergence.

### Inactive-at-capture

Inactive evidence never grants authority to stop an active destination unit.

Services v1 has no “converge to stopped” operation.

### Other runtime state

Blueprint does not persist ordinary service intent from:

- PID;
- uptime;
- failed state;
- exit code;
- invocation ID;
- resource consumption;
- current timer countdown.

These may be diagnostic observations but are not profile desired state.

## Activation policy

### Why activation exists

Persistent reconstruction alone can leave a newly restored machine in a confusing state.

A service may be correctly installed and enabled but not currently active until:

- the next login;
- another trigger;
- a manual start.

For a user whose background agent or watcher was working before migration, that can appear as failed reconstruction.

Services therefore supports explicit post-Restore activation without treating runtime activity as desired-state convergence.

### Activation choices

At Restore time Blueprint supports the semantic choices:

#### Restore working state

Eligible managed functionality that:

- was active when captured;
- is allowed by its target preference;
- is safe and compatible to activate;

is proposed for activation.

#### Persistent state only

Definitions and persistent systemd state are reconstructed.

No inactive service is started by Services.

#### Review activation

Eligible activation operations are presented individually for approval.

The exact CLI/TUI control names and flags belong in the implementation plan, but both interfaces must expose the same authority.

### Per-target activation preference

A managed target may carry a preference such as:

```text
restore working state
do not activate automatically
ask during Restore
```

This lets users exclude expensive, sensitive or machine-specific services without disabling persistent reconstruction entirely.

### Activation authority equation

An activation operation exists only when all relevant authority is present:

```text
captured active evidence
AND
target activation preference permits it
AND
current Restore run permits it
AND
service safety permits it
AND
compatibility/readiness permits it
```

If any element withholds authority, activation is absent or reduced accordingly.

### Headless behavior

Headless/unattended Restore never gains activation authority merely because the profile contains active-at-capture evidence.

The caller must explicitly authorize activation semantics for that run.

JSON preview must make proposed activation operations machine-readable.

## Activation semantics by unit type

Blueprint activates the systemd entry point representing the captured working behavior.

Examples:

### Timer

```text
backup.timer
    activates
backup.service
```

If the timer was active and activation is authorized, Blueprint activates the timer.

It does not invoke `backup.service` immediately merely because the service is connected to the timer.

### Socket

```text
assistant.socket
    activates
assistant.service
```

Blueprint activates the socket and lets systemd perform socket activation later.

### Path

```text
watch-downloads.path
    activates
organize-downloads.service
```

Blueprint activates the path watcher.

### Target

Activating a target may fan out to dependency units.

The preview should show known affected relationships so the user understands the consequence.

### Service

A directly active managed service may be started if inactive on the destination and activation authority exists.

### Slice

A slice generally does not receive a simplistic “restore running process” treatment.

Its persistent definition/dependency role is the meaningful managed intent.

## No automatic stop/restart in v1

Services v1 intentionally supports activation asymmetrically.

It may start an inactive eligible target.

It does not automatically stop a destination unit because the source was inactive.

It does not automatically restart a running unit because Blueprint changed its definition.

Example:

```text
notes-sync.service

Definition updated.

Currently running:
Yes

Blueprint will not restart this service automatically.
The running process may continue using its previous configuration.
```

A later restart/reload capability requires separate explicit side-effect authority.

## Systemd user-manager readiness

Restore semantics require a usable systemd user environment.

Services must not emulate semantic operations when that environment is absent.

Read-only planning determines whether required target capabilities are available.

Services does not:

- invoke `sudo`;
- enable user lingering;
- start hidden privileged infrastructure;
- mutate the machine merely to make inspection succeed.

An unmet supported precondition is represented through the existing Restore requirement/compatibility mechanisms.

Remediation is external and followed by complete replanning.

## Proposed-content validation

Before Apply, Blueprint should validate proposed effective unit content using authoritative non-mutating systemd facilities where available.

Validation needs to consider the proposed set, not only each file in isolation where relationships matter.

A unit that cannot be meaningfully parsed/loaded becomes a pre-Apply problem.

Validation does not:

- write the target configuration;
- daemon-reload as a probe;
- start a service;
- repair the profile.

The exact command/API belongs in implementation investigation; the design requirement is authoritative read-only validation before mutation where the platform provides it.

## Restore planning

A Services Restore plan is derived from:

```text
profile Services desired state
+ effective Services Restore policy
+ Safe/Force
+ Additive/Exact
+ read-only current user-manager/filesystem inspection
+ migration compatibility
+ activation choice
```

Conceptual operation kinds include:

```text
create/update managed definition
remove explicit-absent managed definition
create/update managed drop-in
remove explicit-absent managed drop-in
enable
disable
mask
unmask
configure instance intent
daemon-reload
activate
skip/conflict
```

These names are not a frozen Go enum.

### Ordering

The semantic order is:

```text
1. validate all proposed persistent content
2. apply approved file/absence mutations
3. daemon-reload once at the appropriate boundary
4. converge approved enablement/mask/instance state
5. perform explicitly authorized activation
6. verify
```

Planning may optimize no-op cases but cannot reorder in a way that changes safety or approval meaning.

### Atomic file writes

Managed file creation/update should use the project's safe write patterns so a partially written unit file is not exposed as valid desired state.

Services relies on the existing Restore journal for operation history/recovery semantics.

The design does not introduce a separate Services transaction log.

## Approval stability

Services uses the existing approved-plan stability rule.

After approval and before mutation, Blueprint:

- re-inspects current service state;
- recalculates provenance/ownership;
- re-evaluates compatibility;
- re-evaluates dependencies;
- revalidates proposed operations;
- recalculates activation operations.

If the new plan differs from the approved plan, the old approval is rejected.

This includes changes to:

- conflicting file content;
- external-base availability;
- linked-source availability;
- compatibility state/authority;
- dependency classification;
- enablement/mask state that changes mutation authority;
- activation eligibility.

Approval never silently authorizes newly different service effects.

## Compatibility

Services participates in ADR 0023's compatibility model deliberately.

There is no implicit Services `Supported`.

### Possible evidence

Provider-specific compatibility evidence may include:

- usable target user manager;
- selected unit type supported by the provider/runtime contract;
- target parser accepts the proposed unit set;
- external base exists for a managed customization when required;
- linked source exists;
- required custom dependency exists or is also managed;
- necessary semantic systemd operation is available.

Version equality alone is not Services compatibility evidence.

### Unknown

Unknown remains explicit.

Examples may include a dependency whose ownership/availability cannot be established read-only.

Unknown may:

- leave bounded persistent work unchanged;
- reduce activation;
- reduce Exact removal;
- block the selected target;

depending on what fact is actually required for the proposed authority.

### Incompatible

Affirmative evidence that the selected service intent cannot safely apply blocks that effective intent.

Force does not override this.

### Exact

Compatibility uncertainty never increases Exact deletion authority.

### Activation

Compatibility uncertainty never grants activation authority.

A service may be safely reconstructable as persistent content while its immediate activation is withheld.

That distinction should appear in the plan instead of turning every activation uncertainty into an unnecessary loss of persistent reconstruction.

## Dependency handling during Restore

### Managed dependency

If a selected dependency is also Blueprint-managed, both participate in the same Services plan.

Planning orders operations so definitions are present before semantic operations require them.

### External dependency present

The plan explains that the managed service relies on external state but may proceed where the dependency is affirmatively usable.

### External dependency missing

Blueprint does not silently acquire the dependency.

Depending on the relationship:

- persistent definition reconstruction may remain safe;
- enablement may remain possible;
- activation may be withheld;
- the selected operation may be blocked if the dependency is required for meaningful/safe application.

The provider should narrow authority rather than pretend missing external state does not matter.

## Enablement and masks during Restore

Positive persistent service values are converged subject to ordinary safety.

Examples:

```text
desired: Starts automatically
target: Available but not automatic
→ enable

desired: Available but not automatic
target: Starts automatically
→ disable

desired: Blocked from starting
target: unmasked
→ mask
```

If destination state represents an unexpected conflicting customization outside Blueprint's authority, Safe preserves it and reports the conflict.

### Removing a mask

There are two materially different cases.

#### Mask conflicts with positive managed unit intent

If Blueprint legitimately manages the unit and the destination has an unexpected user mask, unmasking is a conflict-resolution operation subject to Safe/Force.

#### Blueprint-managed overlay mask is explicitly desired absent

Removing that previously managed mask is desired-absence work and therefore requires Exact plus all normal safety/compatibility authority.

An unmanaged external mask is never removed merely because the profile does not mention it.

## Capture review UX

The exact layout is a UI concern, but the workflow must communicate the following information.

### Recommended candidates

Example:

```text
Services

Custom user services

[ ] backup-projects.service
    Runs a custom background service.

[ ] backup-projects.timer
    Schedules backup-projects.service.

    Why recommended:
    Persistent user configuration.
```

New top-level candidates remain unselected until the user chooses them.

### Dependency expansion

After selecting the timer:

```text
[x] backup-projects.timer

    Related unit
    [x] backup-projects.service
        Recommended because this timer activates it.
```

The dependency is preselected but easily disabled.

### Advanced types

```text
Additional advanced units

[ ] dev-server.socket
    Listens for connections and activates dev-server.service.

[ ] watch-downloads.path
    Watches filesystem changes and activates organize-downloads.service.

[ ] development.target
    Groups several user units.

[ ] background.slice
    Applies resource controls to a user service group.
    Review portability on machines with different resources.
```

### External customization

```text
pipewire.service

External service:
Package/Omarchy managed

Your customization:
  10-low-latency.conf

[ ] Preserve my customization

Blueprint will not copy the base service.
```

### Mask

```text
vendor-agent.service

Blocked from starting

Why:
You explicitly prevented this external service from activating.

[ ] Preserve this preference
```

### Linked source

```text
my-agent.service

Definition stored elsewhere:
~/Projects/agent/systemd/my-agent.service

The source is not owned by Services.

[ ] Manage this service intent
```

The UI then explains whether the linked source is another Blueprint-managed Resource or an external requirement.

## Restore preview UX

Human-readable preview should answer:

- what persistent files will change;
- what systemd persistent state will change;
- what is external;
- what dependencies are involved;
- what will start now;
- what will not be restarted;
- what is blocked/reduced and why.

Example:

```text
backup.timer

Create definition
Start automatically
Restore working state after Restore

Why activate:
This timer was active when captured.

Activates:
backup.service
```

Example changed running unit:

```text
notes-sync.service

Update definition

Currently running:
Yes

Blueprint will not restart it automatically.
The new definition will apply on its next normal restart.
```

Example missing external dependency:

```text
agent-helper.service

Definition can be restored.

Activation withheld:
agent-runtime.target is external and was not found on this machine.
```

## User-facing terminology

Systemd vocabulary remains available but secondary.

Preferred mappings:

| Systemd concept                 | Primary Blueprint language          |
| ------------------------------- | ----------------------------------- |
| enabled                         | Starts automatically                |
| disabled                        | Available but not automatic         |
| masked                          | Blocked from starting               |
| static/indirect                 | Started by another service          |
| active                          | Currently running                   |
| linked                          | Definition stored elsewhere         |
| external base + managed drop-in | Managed customization               |
| package/Omarchy base            | External service                    |
| daemon-reload                   | Refresh systemd service definitions |

Reasons should describe consequence rather than implementation.

Bad:

```text
State: static
```

Better:

```text
Started by another service

This unit has no direct automatic-start setting.
```

Bad:

```text
linked source unavailable
```

Better:

```text
This service's definition comes from
~/Projects/agent/systemd/my-agent.service,
but that source is not available on this machine.
```

## CLI behavior

Services uses existing category-scoped Blueprint workflows.

### Capture

Plain:

```text
omarchy-blueprint capture
```

updates already managed Services state according to Capture policy.

It does not silently enroll newly discovered Services candidates.

Reviewed Capture:

```text
omarchy-blueprint capture --review
```

is the CLI path for:

- new service adoption;
- dependency recommendations;
- advanced unit selection;
- reviewed desired-absence changes;
- activation-preference review.

JSON reviewed Capture remains preview-only according to the existing CLI contract.

### Restore

Services participates in:

```text
omarchy-blueprint restore services
omarchy-blueprint restore services --dry-run
omarchy-blueprint restore services --dry-run --json
```

Exact flag spelling for activation options is deferred to implementation planning, but the CLI must expose the same activation authority that the TUI exposes.

A headless mutating Restore cannot gain service activation authority implicitly.

### Policy

Services participates in existing:

```text
policy show
policy set capture
policy set restore
policy clear
```

semantics using the same machine/profile inheritance model.

No separate Services policy engine is introduced.

## TUI behavior

The TUI uses shared Services workflow/domain results.

It may provide richer interaction for:

- candidate selection;
- dependency expansion;
- advanced-unit disclosure;
- activation review;
- reasons/help text.

It does not independently decide:

- ownership;
- dependency authority;
- compatibility;
- enablement;
- Exact safety;
- activation eligibility.

CLI/TUI parity remains an explicit invariant.

## Verify

Services Verify receives the same resolved Restore context and intent used by the plan.

### Persistent content

Verify confirms selected managed definitions and drop-ins match desired content.

### Explicit absence

Under Exact operations that actually applied desired absence, Verify confirms those exact managed artifacts are absent.

It does not scan for or complain about unrelated units.

### Enablement/masks

Verify checks the normalized persistent intent using authoritative systemd state.

### Templates/instances

Verify checks configured instance intent that Restore was responsible for applying.

### Loadability

Verify confirms the resulting managed unit set can be meaningfully loaded by the target systemd user environment.

### Activation

Activation is verified only when activation was actually approved.

Verification must understand that successful activation does not always imply a permanently `active` process.

For unit types whose successful activation is expected to remain active, current state may be affirmative evidence.

For oneshot-like behavior, successful systemd activation and type-specific outcome is the relevant contract.

### Running changed service

If Blueprint changed persistent content for an already-running unit but did not restart it, Verify may still pass persistent-state reconstruction while surfacing the runtime warning.

It must not claim the running process has consumed the new configuration.

## Error behavior

### Read-only inspection failure

If Blueprint cannot establish required service facts read-only, it reports the failure/Unknown compatibility rather than mutating the machine to find out.

### Invalid proposed unit

Blocks before Apply.

### User manager unavailable

Produces the applicable requirement/compatibility result.

Blueprint does not silently enable linger or use privileged system service operations.

### File mutation failure

Restore stops according to existing journal/error behavior.

No later semantic operation should proceed as if the failed persistent state were present.

### daemon-reload failure

Restore fails before applying activation that depends on the new effective definitions.

### enable/disable/mask failure

Restore fails for the selected Services operation and Verify cannot claim convergence.

### activation failure

If activation was explicitly part of the approved plan, activation failure is a Restore failure for that approved effect.

Persistent service state may already have been reconstructed; the journal/output must make that distinction visible.

Services v1 does not promise rollback of arbitrary service side effects.

## Security considerations

A user service is executable persistent behavior.

Restoring and especially activating one may:

- execute arbitrary commands as the user;
- make network connections;
- write files;
- open sockets;
- watch filesystem paths;
- spawn agents;
- activate dependency units.

Therefore:

- first acquisition is reviewed;
- authored content is visible/human-readable;
- activation is an explicit separate authority;
- dependency effects are surfaced where systemd makes them knowable;
- inline sensitive content receives normal secret/sensitivity treatment;
- package/system ownership boundaries cannot be bypassed by Force;
- headless activation requires explicit permission.

Blueprint does not attempt to sandbox arbitrary service programs.

## Reconstruction Assurance

Services v1 should extend the existing real-Omarchy Reconstruction Assurance coverage before the tranche is declared complete.

The acceptance scenario should use deterministic, non-privileged user services with no network dependency.

At minimum it should prove reconstruction of:

- a custom user definition;
- a selected drop-in;
- persistent enablement;
- a timer/service relationship or another deterministic dependency;
- Services verification on Machine B.

The acceptance fixture should also exercise post-Restore activation if that can be done deterministically without introducing flaky timing.

Independent native assertions should confirm the relevant systemd state rather than relying only on Blueprint Verify.

System/root service coverage remains excluded.

The exact fixture and harness changes belong in the implementation plan.

## Testing strategy

Implementation should provide focused tests for the following contracts.

### Discovery/classification

- persistent user configuration candidate;
- ambiguous application/user-data candidate;
- external package/Omarchy base;
- generated/transient exclusion;
- drop-in-only customization;
- persistent external-unit mask;
- linked source;
- template/instance classification;
- advanced-unit disclosure;
- unsupported unit type.

### Candidate ownership

- new top-level candidate is not acquired by plain Capture;
- reviewed inclusion creates managed desired state;
- selected dependency is recommended/preselected;
- dependency deselection does not become ownership;
- Preserve freezes managed desired state;
- reviewed removal creates explicit absence;
- unreviewed missing state does not.

### Profile

- human-readable round trip;
- exact definition bytes preserved;
- exact drop-in bytes preserved;
- external base never copied for overlay-only management;
- template definition stored once;
- runtime evidence distinguishable from desired state.

### Planning

- Safe collision;
- Force within legitimate user-level authority;
- Force cannot overwrite protected external base;
- Additive with desired absence;
- Exact bounded removal;
- enabled/disabled/masked positive convergence;
- mask overlay on external base;
- missing external dependency reduces appropriate authority;
- invalid unit blocks pre-Apply;
- user-manager requirement;
- compatibility participates explicitly.

### Activation

- no permission means no activation;
- persistent-only means no activation;
- restore-working-state requires active-at-capture evidence;
- inactive-at-capture never stops destination;
- timer activation starts timer rather than service directly;
- socket/path activation uses their entry point;
- already-active changed service is not restarted;
- headless Restore does not implicitly activate;
- per-target preference narrows run-level activation permission;
- compatibility/safety can withhold activation independently of persistent reconstruction.

### Approval stability

Changes after approval to:

- definition bytes;
- drop-ins;
- ownership/provenance;
- dependencies;
- external-base availability;
- mask/enablement conflict state;
- compatibility;
- activation eligibility;

must invalidate the prior approval when they change the plan.

### Verify

- definition;
- drop-ins;
- absence;
- enablement;
- mask;
- templates/instances;
- loadability;
- authorized activation;
- oneshot/non-long-lived activation semantics;
- no runtime requirement when persistent-only was selected;
- unrelated service state ignored.

### CLI/TUI parity

Both interfaces consume identical domain/workflow results for:

- discovery;
- reasons;
- selection;
- policy;
- plan;
- compatibility;
- activation;
- verify.

## Rollout shape

The implementation plan should decompose Services v1 so safety/domain semantics land before convenience UI.

A reasonable dependency direction is:

```text
domain/profile representation
→ read-only discovery/classification
→ Capture candidate ownership
→ persistent Restore/Verify
→ compatibility integration
→ activation authority
→ CLI/TUI review UX
→ Reconstruction Assurance extension
```

This ordering is illustrative only.

The implementation plan should determine exact PR boundaries after inspecting the final post-Migration-Safety codebase.

## Completion criteria

Services v1 is complete when all of the following are true:

1. Services is a first-class Blueprint category.
2. Read-only discovery finds supported persistent user-unit candidates.
3. Discovery does not silently acquire ownership.
4. Reviewed Capture can include custom definitions and managed customizations.
5. Dependency recommendations are preselected after parent selection and can be declined.
6. Recommended and advanced unit-type boundaries behave as designed.
7. Templates/instances are represented correctly.
8. Exact unit/drop-in content is human-readable in the profile.
9. Persistent enablement/disablement/masking reconstructs semantically.
10. External bases remain external.
11. Explicit desired absence is bounded to previously managed state.
12. Exact never deletes unrelated service state.
13. Safe/Force preserve their existing meaning.
14. Services contributes deliberate migration compatibility evidence.
15. Proposed content is validated read-only before mutation where supported.
16. Restore uses systemd semantics rather than handcrafted enablement.
17. Persistent reconstruction works without automatically starting units.
18. Approved restore-working-state activation works through its separate authority.
19. Inactive-at-capture never causes automatic stopping.
20. Changed active units are not automatically restarted.
21. Verify checks the same intent the plan applied.
22. User-facing reasons explain systemd concepts in understandable language.
23. CLI and TUI have feature parity.
24. Real-Omarchy Reconstruction Assurance covers Services reconstruction.
25. No root/system-service authority has been introduced.

## Open implementation questions

These are implementation questions, not unresolved product-design questions:

1. Which exact read-only systemd command/API combination gives the most deterministic effective fragment, drop-in, dependency and enablement data?
2. Which authoritative non-mutating systemd verification path works reliably on the supported Omarchy baseline?
3. What exact internal target/subtarget identity encoding best fits existing policy and plan models?
4. What final TOML field names best fit the current profile schema conventions?
5. What exact CLI flag spelling exposes activation mode without colliding with existing Restore options?
6. What deterministic Services fixture best extends Reconstruction Assurance without timing/network flakiness?
7. Which current shared secret/sensitivity helper should Services use?
8. Which compatibility findings belong in Services after the Migration Safety integration tranche has fully landed?

These questions may be answered by repository inspection and implementation planning without changing the architectural decisions in this document.

## References

- ADR 0012: Portable Resources
- ADR 0013: Baseline-Aware Config Overlay
- ADR 0020: CLI policy and Capture parity
- ADR 0021: Real-Omarchy Reconstruction Assurance
- ADR 0022: Fresh-package readiness and elevated Restore
- ADR 0023: Plan-Integrated Migration Compatibility Assessment
- ADR 0024: Semantic User Service Reconstruction
- `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`
- `docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md`
- `docs/planning/specs/2026-09-26-migration-safety-compatibility-v1-design.md`
