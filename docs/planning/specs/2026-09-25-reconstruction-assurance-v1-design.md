# Reconstruction Assurance v1 Design

## Status

Proposed.

This design is the first implementation of the roadmap's Reconstruction Assurance phase. It follows the completed GitHub Actions/KVM feasibility spike and is intended to become a required pull-request gate from its first production version.

The design should be implemented as a small sequence of independently reviewable changes rather than one large infrastructure PR.

## Summary

Reconstruction Assurance v1 proves Omarchy Blueprint's central product promise against real Omarchy:

```text
real Omarchy Machine A
→ customize through real supported system mechanisms
→ Blueprint Capture
→ portable profile
→ destroy Machine A
→ fresh independent Machine B
→ Blueprint Restore
→ Blueprint Verify
→ independent machine verification
```

The acceptance layer is intentionally not a second implementation of Omarchy. It runs a real official Omarchy installation under KVM on GitHub-hosted CI and invokes the exact Blueprint binary built from the pull-request commit.

The first production gate has one canonical "portable developer" lifecycle. It covers representative mature categories and one machine-specific Restore mapping/skip behavior without expanding immediately into a persona matrix or destructive Restore modes.

A green Reconstruction Assurance check means:

> this Blueprint commit captured representative portable desired state from one real compatible Omarchy machine and safely reconstructed the selected intent on another fresh real compatible Omarchy machine.

## Background

Blueprint's ordinary tests already exercise policy resolution, provider logic, profile serialization, Capture/Restore planning, transactions, verification, conflicts, machine overlays, and presentation behavior. These tests are fast and precise and remain the correct place for exhaustive domain permutations.

They do not prove all of the following at once:

- the official Omarchy install path produces an environment Blueprint can actually inspect and mutate;
- Blueprint invokes contemporary Omarchy/system interfaces correctly;
- profile artifacts survive a real machine boundary;
- package/theme/plugin/default operations work against the installed system;
- permissions and filesystem semantics behave as expected;
- Restore journals and verification work in a complete reconstruction run.

The roadmap explicitly identifies VM reconstruction tests as critical and positions Reconstruction Assurance before Migration Safety and Sync.

### Feasibility evidence

The completed spike demonstrated that a standard public-repository GitHub-hosted `ubuntu-24.04` runner can:

- expose usable KVM acceleration;
- boot a nested Linux guest;
- install the pinned official Omarchy release using its supported unattended cidata path;
- boot the installed Omarchy guest;
- reach real systemd, pacman, Git, the user's home, and Omarchy tooling.

The spike also showed that the official unattended installer should be allowed to select Omarchy's current default kernel rather than having the test harness force an implementation detail.

Observed runner capacity was larger than the documented standard-runner storage specification. The production harness therefore treats runtime resource measurements as diagnostics and never assumes that observed excess capacity is guaranteed.

## Goals

1. Make real-machine reconstruction a required pull-request gate.
2. Exercise the exact Blueprint binary from the pull-request commit.
3. Exercise a real official Omarchy installation rather than a mocked Omarchy environment.
4. Prove that only portable profile state is needed to move selected intent from Machine A to Machine B.
5. Prove representative reconstruction across Packages, Themes, Installed plugins, Shell, Config, Hooks, Defaults, and Resources.
6. Prove one machine-specific Resource path mapping and one Restore-disabled target.
7. Exercise the real Restore preview/approval/post-approval-recalculation lifecycle.
8. Require both Blueprint verification and independent machine-level assertions.
9. Keep VM orchestration, scenario behavior, and verification separately understandable.
10. Make failures actionable enough for a required gate.
11. Keep the first acceptance suite narrow enough that a failure has a clear meaning.
12. Create a foundation that Migration Safety, Services, Sync, and later scenarios can extend without redesigning the harness.

## Non-goals

Reconstruction Assurance v1 does not attempt to provide exhaustive real-machine coverage.

It does not require:

- Force Restore;
- Exact Restore;
- desired-absence convergence;
- Capture Preserve;
- dirty Git Resource reconstruction;
- AUR packages;
- third-party internet-hosted plugin/theme fixtures;
- rollback fault injection;
- recovery from failed verification;
- secret/sensitive-content scenarios;
- cross-Omarchy-version migrations (Machine A and Machine B must reach the same ready Omarchy runtime before Capture and Restore; see Substrate pinning);
- multiple personas;
- GUI/visual equivalence;
- monitor or hardware reconstruction;
- custom systemd Services;
- Sync Engine or Auto Sync;
- persistent VM base-image infrastructure;
- self-hosted runners;
- a multi-version Omarchy matrix.

Those are later acceptance scenarios or later product tranches.

## Product principles

### Test the product promise, not implementation trivia

The acceptance gate should state product-level invariants:

- Capture produced portable desired state;
- Restore reconstructed expected intent;
- machine-specific policy was respected;
- safety/approval boundaries held;
- the resulting machine state is correct.

It should not freeze planner formatting, operation IDs, or incidental implementation details.

### Real system boundaries matter

Reconstruction Assurance is deliberately black-box with respect to Blueprint application behavior.

It may inspect profile output and journals as evidence, but Capture and Restore are invoked through the shipped CLI and state is graded through native system interfaces.

### Safety is part of reconstruction

A test that reaches the desired final bytes by bypassing preview, approval, policy, or provider safety does not prove Blueprint's product promise.

The acceptance lifecycle must therefore retain:

```text
inspect
→ calculate
→ preview
→ approve
→ re-inspect/recalculate
→ apply
→ verify
```

### The running system remains the configuration editor

Machine A is customized through normal supported system/Omarchy mechanisms. Test fixtures provide deterministic inputs; they do not become a declarative alternate configuration system for Blueprint.

### CI convenience cannot create extra product authority

The harness must not:

- invoke private Blueprint workflow/provider APIs to bypass the CLI;
- silently patch an Omarchy guest to make a fixture pass;
- override Restore Skip with Force/Exact;
- automatically retry failed semantic operations;
- mutate Machine B outside normal fixture setup and Blueprint Restore in ways that manufacture the expected result.

## Architecture overview

```text
GitHub Actions
      │
      ▼
Host/bootstrap
      │
      ▼
Pinned official Omarchy ISO
      │
      ▼
Pristine installed base qcow2
      │
      ├───────────────┐
      ▼               ▼
source overlay     target overlay
Machine A          Machine B
      │               ▲
      │ Capture       │ Restore
      ▼               │
portable profile ─────┘
      │
      ▼
independent verifier
```

Only one VM runs at a time.

The pristine base is immutable after validation.

Machine A is destroyed before Machine B Restore.

Only the captured Blueprint profile crosses the source-to-target boundary.

## Harness boundaries

The harness has five responsibilities.

### 1. Host/bootstrap layer

Owns CI-host concerns only:

- verify KVM availability and usable permissions;
- install minimal QEMU/OVMF/host dependencies;
- record CPU, memory, disk, and virtualization diagnostics;
- download the pinned official Omarchy ISO;
- verify the ISO checksum;
- create an isolated working directory;
- build the exact Blueprint binary from the pull-request commit.

The production gate has no automatic software-emulation fallback. If KVM is unavailable, the gate fails as an infrastructure failure.

### 2. Pristine Omarchy builder

Builds one clean installed Omarchy disk from the official ISO for the current run.

Responsibilities:

```text
generate supported unattended cidata
→ boot official ISO under KVM
→ complete Omarchy install
→ boot installed disk
→ validate basic guest health
→ cleanly shut down
→ mark disk immutable for the remainder of the run
```

Guest health must prove at minimum:

- systemd is PID 1 and healthy enough for the scenario;
- pacman is available;
- Git is available;
- the expected user/home exists;
- Omarchy tooling is present;
- kernel/header state is internally consistent enough for the installed system.

The unencrypted unattended base boots to SDDM, before a user desktop session
starts. The base/overlay boot health probe may use headless Omarchy commands
(`omarchy commands --json`, `omarchy theme current`) and check that
`omarchy-shell` is installed, but must not require Shell IPC such as
`omarchy plugin list --json`: upstream returns `omarchy-shell is not running`
before login. The later plugin acceptance scenario must establish a real Omarchy
user session before testing session-dependent plugin discovery; a fake Shell
service or bypass is not an acceptable substitute.

The builder must not force Omarchy internal defaults that the supported unattended install path can choose itself.

The base contains no initialized Blueprint profile and no canonical-scenario customization.

### 3. Guest lifecycle runner

Provides small host-side VM operations:

```text
create overlay
prepare fresh guest identity
boot
wait for SSH
execute command
copy controlled input
copy artifact out
collect diagnostics
shutdown
destroy overlay
```

This layer knows nothing about Packages, Themes, Capture, Restore, or Blueprint policy.

Shell is the preferred implementation for v1 unless complexity proves that a stronger abstraction is needed. A generic Go VM framework is not a goal.

### 4. Scenario driver

Owns the product story and fixture sequence.

Machine A:

```text
create source overlay
→ establish fresh OS identity
→ boot
→ install exact PR Blueprint binary
→ create/select Blueprint machine "source"
→ apply canonical source customizations
→ independently assert customizations exist
→ Capture through shipped CLI
→ validate/check captured profile
→ export profile to host
→ record handoff digest
→ destroy source overlay
```

Machine B:

```text
create target overlay
→ establish fresh OS identity
→ boot
→ install exact PR Blueprint binary
→ independently assert source customizations did not leak
→ import exact captured profile
→ verify handoff digest
→ explicitly select Blueprint machine "target"
→ structured Restore dry-run
→ assert plan contract
→ run normal Restore approval flow
→ require successful apply and Blueprint verification
→ independently verify target machine
→ assert final managed state has no actionable differences
→ destroy target overlay
```

### 5. Independent verifier

Grades the resulting Machine B through native system interfaces rather than asking Blueprint alone.

The verifier is logically and physically separate from scenario execution scripts where practical.

## Suggested repository layout

The exact names may be refined during implementation, but the responsibilities should remain visible:

```text
test/
└── reconstruction/
    ├── README.md
    ├── run.sh
    ├── vm/
    │   ├── install-omarchy.sh
    │   ├── guest.sh
    │   └── cidata/
    ├── scenario/
    │   ├── customize-source.sh
    │   ├── capture.sh
    │   └── restore-target.sh
    ├── verify/
    │   └── verify-target.sh
    └── fixtures/
        ├── plugin/
        ├── git-resource/
        ├── scripts/
        └── expected/
```

The GitHub Actions workflow remains thin:

```text
checkout
→ setup host dependencies
→ run test/reconstruction/run.sh
→ upload focused diagnostics on failure
```

Workflow YAML is orchestration, not the test implementation.

## CI trigger and required-check behavior

The production workflow runs on every pull request.

It should also remain manually dispatchable for maintenance/debugging.

Ordinary Go CI and Reconstruction Assurance run independently so a failure in one does not suppress useful evidence from the other.

The repository's merge rules must mark the production Reconstruction Assurance check as required from its first release.

The canonical job must not use `continue-on-error`.

The workflow should use least-privilege GitHub permissions and require no repository secrets for the canonical public-repository scenario.

## Substrate pinning

The gate pins test substrate identity:

- `ubuntu-24.04` hosted runner;
- an explicit supported official Omarchy ISO version;
- the exact published checksum for that ISO;
- the exact Blueprint PR commit;
- repository-controlled fixture bytes and expected Git revision.

The workflow must not follow an unpinned "latest" Omarchy release for its install substrate.

Updating the pinned Omarchy release is a deliberate repository change that must pass the gate.

### Amendment (ADR 0022): install substrate, ready runtime, and symmetry

A fresh official Omarchy install has no pacman sync databases. The only supported way to make it ready to manage packages is Omarchy's own `omarchy update`, which is a full system upgrade. Blueprint deliberately never syncs package metadata itself (ADR 0022). The old "never follow latest" statement therefore cannot hold literally for the runtime a Restore runs on. The gate distinguishes:

- **Pinned install substrate:** the official Omarchy ISO version and checksum above. Every run installs exactly this.
- **Authoritative ready runtime:** the exact Omarchy release that `omarchy update` resolves during that run. Omarchy chooses it; the harness never chooses, pins or manufactures a newer state.
- **Invariant:** Machine A and Machine B each run `omarchy update` from the pinned substrate, and must reach the same ready runtime (`omarchy version`) before customization and Restore respectively. A mismatch, for example because Omarchy published a release between the two updates, fails the run loudly and is never retried until the versions happen to match. Both versions are recorded in the artifacts and the summary.

Capture and Restore therefore always happen on the same Omarchy version, so v1 stays reconstruction rather than migration. This is an intentional relaxation of the substrate pinning, forced by the supported product path, and it is limited to the runtime; the install substrate stays pinned.

The package manager inside the pinned guest may use normal supported repository resolution. Reconstruction Assurance is not intended to become a hermetic Linux distribution build.

## Pristine base and overlay model

### One install, two independent machines

Each run installs Omarchy once.

The installed disk becomes a pristine read-only backing image.

Machine A and Machine B each receive a distinct writable qcow2 overlay.

They never run concurrently.

### Fresh identity

Each overlay must establish a fresh machine identity before Blueprint runs.

At minimum the harness assigns distinct hostnames:

```text
blueprint-ra-source
blueprint-ra-target
```

and regenerates OS machine identity where required by the cloning mechanism.

Blueprint's logical machine selections are fixture roles:

```text
source
target
```

Blueprint machine binding remains local state and is never copied between guests.

### Base immutability

After base validation:

- the base disk is not booted writable;
- Machine A customization occurs only in the source overlay;
- Machine B Restore occurs only in the target overlay;
- no source overlay blocks, snapshots, or home-directory state become target backing state.

## Profile handoff contract

The portable profile is the only product artifact crossing from Machine A to Machine B.

Expected profile content may include:

```text
profile.toml
packages/
themes/
plugins/
shell/
config/
hooks/
defaults/
resources/
machines/
policy/
other provider-owned portable state
```

Portable machine overlay definitions are part of the profile.

The handoff explicitly excludes source-local runtime state:

```text
$XDG_STATE_HOME/omarchy-blueprint
machine binding
Restore/Capture journals
temporary staging state
package cache
guest credentials
QEMU disk state
Machine A home directory as a reconstruction shortcut
```

After Machine A Capture:

1. export the profile to the host;
2. compute a deterministic archive/directory digest;
3. destroy Machine A;
4. create Machine B;
5. import the exact profile;
6. recompute and require the same digest before Restore.

Selecting Blueprint machine `target` must not modify portable profile bytes.

## Canonical scenario: portable developer

The v1 scenario represents a modest personalized developer machine.

The exact fixture values are implementation choices validated against the pinned Omarchy release. The scenario shape is the contract.

### Packages

Machine A installs one small additional official package that is absent from the pristine base.

Purpose:

- real package detection;
- Capture of explicit user package intent;
- reconstruction through the supported Omarchy/package path;
- native package-state verification on Machine B.

Avoid selecting a package that is already part of the base, hardware-specific, unusually large, or likely to disappear from the pinned release's repositories.

### Theme

Machine A switches to a known built-in Omarchy theme different from the pristine active theme.

Purpose:

- semantic active-theme Capture;
- native Omarchy theme reconstruction;
- semantic verification rather than copied theme bytes.

### Installed plugins

Machine A installs or registers one tiny repository-controlled local custom plugin fixture through supported Omarchy mechanisms, then validates/rescans it using real Omarchy tooling.

Purpose:

- local plugin provenance/content Capture;
- plugin validation and reconstruction;
- avoidance of a random third-party internet repository as a required-gate dependency.

The fixture must not impersonate Omarchy commands. Omarchy remains real.

### Shell

Machine A makes one deterministic customization supported by the current Shell provider. Where practical, it references/enables the canonical custom plugin so the Restore plan exercises plugin-before-Shell dependency ordering.

Purpose:

- semantic Shell Capture;
- captured baseline/desired intent;
- supported semantic merge on a fresh target;
- relationship between plugin reconstruction and Shell state.

### Config

Machine A changes one known portable Config target using a harmless deterministic marker/value.

Purpose:

- Config ownership/discovery;
- Capture of portable user configuration;
- safe reconstruction onto the target.

The chosen target must not require a GUI session to verify.

### Hooks

Machine A creates one harmless deterministic executable hook under the supported Hooks surface.

Purpose:

- hook discovery and Capture;
- content restoration;
- executable-mode preservation;
- real Restore journaling/verification for executable user content.

The hook must not perform privileged or externally destructive work.

### Defaults

Machine A selects one low-risk Omarchy-managed default with a supported alternative value.

Purpose:

- semantic Defaults Capture;
- reconstruction through native `omarchy default ...` behavior;
- proof that Blueprint does not merely copy Omarchy-generated output files.

Do not use the agent default in the canonical scenario because setting it can intentionally invoke interactive/external behavior.

### Copied Resource

Machine A tracks one tiny executable helper script.

Portable logical path:

```text
~/.local/bin/blueprint-ra-helper
```

The profile's `target` machine overlay maps the Resource to:

```text
~/bin/blueprint-ra-helper
```

Purpose:

- copied Resource Capture and Restore;
- mode/hash preservation;
- machine-specific effective path resolution.

On Machine B the mapped path must exist and the original unmapped destination must remain absent.

### Git-backed Resource

Machine A tracks one clean tiny Git repository.

The fixture is repository-controlled and served to guests through a deterministic test remote reachable from QEMU, rather than depending on an unrelated third-party repository.

Purpose:

- semantic Git provenance;
- reconstruction from authoritative source;
- exact expected revision;
- clean worktree verification.

The acceptance harness must perform a real Git clone/fetch/checkout path rather than copying the repository tree into Machine B.

### Restore-disabled Resource

Machine A captures a second tiny Resource.

The portable `target` machine policy sets Restore Disabled for that Resource.

Purpose:

- prove machine-specific Restore Skip;
- prove Restore Skip is visible in the plan;
- prove Machine B remains untouched for intentionally non-applicable desired state.

This Resource exists on Machine A before Capture so the profile genuinely carries desired state.

## Source-state preconditions

Before Capture, independent assertions on Machine A must establish that every canonical customization actually exists.

At minimum:

```text
package:
  selected package installed

theme:
  selected built-in theme active

plugin:
  fixture plugin present and accepted by real Omarchy validation/rescan

shell:
  expected supported customization present

config:
  expected deterministic change present

hook:
  expected bytes and executable mode present

default:
  expected Omarchy-managed value selected

copied Resource:
  expected path, hash, and executable mode present

Git Resource:
  expected remote, revision, and clean worktree present

Restore-disabled Resource:
  expected fixture state present
```

If source customization cannot be proven, Capture does not run.

## Capture contract

Capture is invoked through the shipped Blueprint CLI.

The canonical scenario first runs a non-mutating public CLI Capture preview and
asserts the expected candidate outcomes. It then invokes the normal reviewed
Capture CLI with a real approval prompt, sends explicit approval once, and
requires Blueprint's re-inspection/recalculation to accept the approved
authority before Capture writes the profile. Changed authority fails `CAPTURE`;
the harness does not retry or reapprove. The harness must not invoke internal
Go APIs or use plain scripted Capture to bypass review.

Capture must:

- succeed;
- produce/update the expected portable profile;
- leave no incomplete transactional/staging state;
- produce a profile that Blueprint can load/check;
- carry machine overlay/policy needed for the target scenario.

The scenario does not snapshot-test the complete profile serialization. Focused assertions may inspect expected semantic records and artifacts where useful.

## Target freshness contract

Before Restore, Machine B independently proves that reconstructable source customizations did not leak through the base/overlay mechanism.

At minimum:

- selected additional package is absent;
- source theme is not already the selected target theme;
- custom plugin is absent;
- Config marker is absent;
- Hook fixture is absent;
- copied Resource is absent from both default and mapped destinations;
- Git Resource is absent;
- Restore-disabled Resource destination is absent/unchanged.

For categories that legitimately have baseline state, such as Shell or Defaults, the assertion is that target state does not already equal the captured customization.

Failure of these preconditions is an isolation/test-harness failure, not a successful no-op Restore.

## Restore intent

The canonical scenario uses:

```text
Conflicts:   Safe
Convergence: Additive
```

with no one-run Force or Exact override.

After verifying the captured profile's transfer and local machine binding,
Machine B explicitly persists Safe + Additive as `target`'s Restore defaults
through the public Blueprint CLI and verifies the effective settings through
public JSON inspection. The ordinary Restore invocation consumes those saved
defaults without one-run intent flags. The target-side policy setting is a
deliberate public CLI mutation after the profile-only handoff, not a change to
Machine A's captured desired state.

Machine-specific Restore Skip remains authoritative and cannot be broadened by Restore intent.

## Restore planning contract

### Structured dry-run

Machine B first runs a machine-readable dry-run, conceptually:

```bash
omarchy-blueprint \
  --profile "$PROFILE" \
  --machine target \
  --json \
  restore --dry-run
```

The harness validates a semantic contract rather than snapshotting the entire plan.

### Required effects

The dry-run must include work representing:

- package reconstruction;
- theme activation;
- plugin reconstruction;
- Shell reconstruction;
- Config reconstruction;
- Hook reconstruction;
- Default selection;
- mapped copied Resource reconstruction;
- Git Resource reconstruction;
- explicit skip of the Restore-disabled Resource for target policy.

Providers may add safe validation/rescan/dependency operations without breaking the test.

### Forbidden effects

The canonical plan must not contain:

- removal operations;
- Exact-only convergence effects;
- Force conflict replacement;
- mutation of the Restore-disabled Resource;
- copied-Resource mutation at the unmapped Machine A/default destination;
- unexpected interactive operations;
- operations outside the captured canonical fixture surface that would mutate unrelated user state.

### Non-contractual plan details

The acceptance test should not fail solely because of changes to:

- operation IDs;
- wording/presentation;
- journal timestamps;
- operation ordering where dependencies still guarantee correctness;
- extra safe validation/check operations;
- non-semantic JSON fields.

## Real approval contract

After the structured dry-run, the harness starts a normal Restore invocation without `--yes` and without JSON output.

The harness:

1. captures the rendered Restore plan/output;
2. waits until Blueprint emits the normal approval prompt;
3. records focused pre-approval machine fingerprints for mutation-sensitive targets;
4. writes `yes` to stdin;
5. waits for Blueprint to complete.

The first dry-run is not reused as an approval token.

Blueprint must perform its normal post-approval recalculation.

If Blueprint reports that the approved plan changed, the run fails immediately.

The harness must not retry or automatically reapprove a changed plan.

An operation marked interactive is outside the canonical v1 scenario and fails the gate.

## Restore application contract

A successful Restore must:

- execute every planned non-skipped operation successfully;
- produce a real Restore journal when operations are applied;
- preserve Restore-disabled state;
- retain the planned Safe + Additive intent through verification;
- report successful Blueprint verification.

The Restore journal is product evidence, not the sole oracle.

## Independent target verification

After Blueprint verification succeeds, the harness grades Machine B independently.

### Package

Use native package state, e.g. `pacman`, to prove the selected package is installed.

### Theme

Use the real Omarchy theme query to prove the expected theme is active.

### Installed plugin

Use real Omarchy plugin state plus direct fixture presence/validation as appropriate to prove the custom plugin exists and is valid.

### Shell

Inspect the actual Shell document/state and prove the intended semantic customization is present.

Do not accept Blueprint's own verification output as the only assertion.

### Config

Inspect the real managed config target and prove the intended result.

### Hook

Prove:

- expected hook path exists;
- bytes/hash match;
- executable mode matches.

### Default

Use the real Omarchy default query and require the expected semantic value.

### Copied Resource

Require:

- `~/bin/blueprint-ra-helper` exists;
- expected hash/content matches;
- executable mode matches;
- `~/.local/bin/blueprint-ra-helper` remains absent on Machine B.

### Git Resource

Require:

- expected repository exists;
- expected remote URL;
- exact expected HEAD revision;
- clean worktree.

### Restore-disabled Resource

Require the destination remains absent/unchanged.

This assertion is mandatory even if Blueprint's plan reported the target as skipped.

## Final Blueprint state

The scenario finishes with Blueprint status/difference inspection.

It does not require literal equality across the entire operating system.

It requires:

> no actionable differences for canonical targets that the `target` machine is expected to manage and converge.

The Restore-disabled Resource remains an intentional difference and must not make the canonical scenario fail merely because the desired profile contains state that policy intentionally leaves unapplied.

## Acceptance contract

A Reconstruction Assurance v1 run passes only when all mandatory assertions succeed.

| Phase | Mandatory assertion |
| --- | --- |
| Substrate | Pinned official Omarchy ISO installs under KVM and the pristine guest boots healthy. |
| Isolation | Source and target are independent overlays with fresh identity; only the portable profile crosses between them. |
| Source state | Every canonical customization is independently proven present on Machine A before Capture. |
| Capture | Blueprint Capture succeeds through the shipped CLI. |
| Profile integrity | Captured profile is loadable/checkable and its handoff digest remains unchanged. |
| Fresh target | Machine B proves reconstructable source customizations did not leak before Restore. |
| Machine intent | `target` is explicitly selected and mapping/Restore Skip resolve as expected. |
| Plan | Safe + Additive dry-run contains required effects/skips and no forbidden destructive/unexpected effects. |
| Approval | Normal CLI preview is approved explicitly and post-approval plan stability holds. |
| Apply | Planned non-skipped Restore operations succeed and a Restore journal is produced when mutation occurs. |
| Blueprint Verify | Blueprint verification succeeds with the same effective Restore intent. |
| Independent Verify | Native system assertions prove the resulting Machine B state. |
| Policy preservation | Restore-disabled state remains untouched. |
| Final state | No actionable differences remain for canonical targets expected to converge. |

Any mandatory assertion failure makes the required check red.

## Failure taxonomy

The harness attributes the first meaningful failure to one phase:

```text
INFRASTRUCTURE
OMARCHY_INSTALL
SOURCE_CUSTOMIZATION
CAPTURE
PROFILE_HANDOFF
TARGET_PREFLIGHT
RESTORE_PLAN
RESTORE_APPROVAL
RESTORE_APPLY
BLUEPRINT_VERIFY
INDEPENDENT_VERIFY
```

A concise summary should show PASS/FAIL/NOT RUN for the major lifecycle stages.

Example:

```text
Reconstruction Assurance
────────────────────────
Base install            PASS
Machine A customization  PASS
Capture                   PASS
Profile handoff           PASS
Target preflight          PASS
Restore plan              PASS
Approval stability        PASS
Restore apply             FAIL
Blueprint verify          NOT RUN
Independent verify        NOT RUN

Failure:
  phase: RESTORE_APPLY
```

The human-facing summary is the first diagnostic surface. Detailed logs are drill-down evidence.

## Retry policy

### Allowed bounded retry/polling

Only inherently idempotent infrastructure observation/transfer:

- ISO/checksum download transfer;
- waiting for KVM/guest process readiness where appropriate;
- SSH readiness polling;
- guest boot readiness;
- guest shutdown readiness.

### No automatic semantic retries

Do not automatically retry:

- Omarchy installation after the installer has meaningfully failed;
- source customization;
- Blueprint initialization/machine setup;
- Capture;
- Restore dry-run;
- approval;
- Restore application;
- Blueprint verification;
- independent verification.

Do not wrap semantic commands in `command || command` or rerun the job automatically from inside the workflow.

The first semantic failure is evidence and must remain visible.

## Timeouts

Each major lifecycle transition has its own timeout rather than relying only on the GitHub Actions job timeout.

At minimum:

- ISO/base installation;
- guest boot/SSH readiness;
- guest shutdown;
- source customization;
- Capture;
- Restore;
- independent verification.

A timeout reports the phase that expired.

Implementation should choose conservative values from measured spike/initial production runs rather than treating exact timeout numbers as architectural constants.

## Diagnostic artifacts

The harness creates a diagnostics tree from the start of the run:

```text
artifacts/
├── summary.txt
├── host/
├── base/
├── source/
├── profile/
└── target/
```

### Host

Retain:

- runner CPU/memory/disk summary;
- `/dev/kvm` state;
- QEMU version;
- relevant QEMU invocation metadata;
- host-space measurements.

### Base install

Retain on failure:

- generated cidata;
- pinned ISO version/checksum metadata;
- installer/serial output;
- final screenshot when useful;
- reachable guest health/kernel/header information.

### Source

Retain:

- source customization log;
- pre-Capture assertion report;
- Capture stdout/stderr;
- captured profile;
- handoff digest.

### Target

Retain:

- pre-Restore/freshness report;
- structured dry-run JSON;
- rendered Restore output and approval transcript;
- Restore stdout/stderr;
- Restore journal;
- Blueprint verification/status output;
- independent verification report.

### Upload policy

On failure, upload focused diagnostics required for investigation.

On success, keep workflow logs concise and upload only small artifacts that are genuinely useful for trend/debugging.

Do not upload qcow2 guest disks by default.

Diagnostics must not contain secrets. The canonical fixture does not require external credentials.

## Cleanup semantics

Cleanup is best-effort and never replaces the original scenario exit status.

Conceptually:

```text
run scenario
remember first meaningful status
collect diagnostics
attempt guest shutdown
attempt QEMU cleanup
remove temporary files
exit with original status
```

A cleanup problem after a Restore failure may be recorded as an additional warning, but the run remains attributed to the Restore failure.

## Fixture design

Fixtures are deterministic test inputs, not mocks of Omarchy.

Acceptable fixtures include:

- custom plugin source consumed by real Omarchy plugin tooling;
- tiny executable script files;
- tiny config/hook markers;
- a tiny Git repository served through a real Git transport;
- expected hashes/revisions.

Fixtures should:

- be small;
- be repository-controlled;
- contain no credentials;
- avoid nondeterministic generated content;
- be understandable when reading the acceptance harness;
- prefer local authoritative sources over unrelated public third-party repositories.

## Local Git fixture

The Git-backed Resource should reconstruct from an authoritative Git source without requiring an unrelated external host.

The CI host may serve a repository-controlled bare repository to the guest through a deterministic address/port reachable through QEMU networking.

The test must still perform real Git operations.

The profile records the expected remote/revision in the same semantic form Blueprint uses for ordinary Git Resources.

## Security and permissions

The canonical required workflow should not require repository secrets.

Use least-privilege GitHub token permissions.

Do not log private SSH keys used solely for guest control.

Any temporary guest-control key is ephemeral to the workflow run and is not part of the captured Blueprint profile.

The custom plugin/hook fixtures must be obviously harmless and scoped to the disposable guest user.

No root-level arbitrary fixture service is introduced in v1.

## Relationship to existing tests

Reconstruction Assurance does not replace ordinary Go tests.

Unit/domain tests remain the primary place for exhaustive logic such as:

- policy inheritance;
- Capture transition tables;
- desired absence;
- Exact semantics;
- Force behavior;
- fingerprint stability;
- provider edge cases;
- transaction rollback;
- target identity;
- profile migration fixtures.

Reconstruction Assurance proves that a selected representative path survives complete system integration.

Where the canonical gate discovers a product regression, the fix should normally add the smallest focused lower-level regression test as well.

## Relationship to manual acceptance

Existing manual real-machine checks remain useful for UX and hardware/session-specific behavior that the headless canonical VM cannot cover.

The required gate takes ownership of the foundational reconstruction promise:

```text
Capture on real Omarchy
→ portable profile boundary
→ Restore on fresh real Omarchy
→ independently verified selected state
```

Manual testing should not be the only evidence for that loop once v1 ships.

## Future expansion

After the canonical gate is stable, additional Reconstruction Assurance scenarios can be added without changing the fundamental harness.

Likely follow-ons include:

1. Safe conflict preservation.
2. Exact convergence for explicitly managed desired absence.
3. Force conflict replacement where supported.
4. Capture Preserve with pre-existing desired state and a second Capture.
5. Dirty Git Resource reconstruction.
6. Failure/rollback injection.
7. Secret rejection.
8. Multiple personas where materially different coverage is justified.
9. Migration Safety fixtures across profile/Omarchy versions.
10. Services v1.
11. Sync Engine and Auto Sync multi-machine reconciliation scenarios.

New scenarios should remain product-promise oriented rather than becoming a combinatorial substitute for unit tests.

## Implementation boundaries

The implementation plan should preserve these boundaries:

- no product schema changes solely for the harness;
- no new category/provider semantics solely to make the canonical scenario possible;
- no acceptance-only Blueprint execution mode;
- no fake Omarchy/system commands;
- no persistent base-image pipeline in v1;
- no required use of local developer VM storage;
- no redesign of the roadmap;
- no expansion into Migration Safety or Services.

If the canonical persona exposes a genuine existing product defect, fix the defect through normal product architecture and regression tests rather than hiding it in acceptance scripts.

## Success criteria

Reconstruction Assurance v1 is complete when:

1. a dedicated production GitHub Actions workflow runs on every pull request;
2. the workflow is configured as a required merge check;
3. it builds a pristine installation from the pinned official Omarchy ISO on a standard hosted `ubuntu-24.04` runner using KVM;
4. it creates independent sequential source and target overlays;
5. it executes the approved canonical persona;
6. it hands off only the portable Blueprint profile;
7. it exercises the normal Safe + Additive Restore preview/approval/recalculation/apply/verify path;
8. it proves machine-specific Resource mapping and Restore Skip;
9. Blueprint verification succeeds;
10. independent machine verification succeeds;
11. failures produce focused phase-specific diagnostics;
12. ordinary Blueprint tests remain unchanged in purpose and continue to pass.

## References

- `ROADMAP.md`
- `docs/adr/0021-real-omarchy-reconstruction-assurance.md`
- `docs/adr/0016-machine-overlay-resource-path-mappings.md`
- `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`
- `docs/manual-test-checklist.md`
- CI feasibility spike branch: `spike/omarchy-vm-feasibility`
