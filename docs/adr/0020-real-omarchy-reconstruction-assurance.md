# ADR 0020: Real-Omarchy Reconstruction Assurance in Required CI

## Status

Proposed

## Context

Omarchy Blueprint's core product promise is reconstruction:

```text
configure a real Omarchy machine
→ Capture meaningful user-controlled intent
→ move the portable profile
→ Restore onto a fresh compatible Omarchy machine
→ obtain the selected environment safely and predictably
```

Blueprint already has substantial unit and integration coverage around providers, policy, Capture, Restore planning, execution, verification, transactions, machine overlays, and the TUI. Those tests are necessary, but they cannot prove that the complete product works against a real Omarchy installation using real system interfaces.

The project roadmap therefore makes Reconstruction Assurance the foundation before Migration Safety, Services, and multi-machine sync. Those later capabilities will change profile compatibility, reconstruction behavior, and eventually unattended application of desired state. They need an end-to-end gate that proves the underlying reconstruction loop still works.

A CI feasibility spike established that a standard GitHub-hosted `ubuntu-24.04` runner can use KVM acceleration and can install and boot the pinned official Omarchy release in a disposable QEMU guest. The successful spike also showed that sequential guests fit comfortably enough to justify CI-first implementation. The observed runner disk capacity exceeded GitHub's documented storage guarantee, so the production harness must measure available resources at runtime rather than depending on observed excess capacity.

The spike also exposed an important testing principle: the acceptance environment must follow Omarchy's supported install path rather than reproducing or overriding Omarchy's internal assumptions. The first failed attempt explicitly selected the stock `linux` kernel; allowing current Omarchy defaults selected the supported Omarchy kernel/header combination and completed successfully.

## Decision

### 1. Reconstruction Assurance uses real Omarchy, not an Omarchy simulation

The acceptance layer will run against a real installation produced from a checksum-verified official Omarchy ISO.

The acceptance environment will use real:

- Omarchy commands;
- systemd;
- pacman and supported Omarchy package operations;
- Git;
- filesystem and permissions;
- Blueprint CLI and workflow behavior.

The Reconstruction Assurance layer will not provide a fake `omarchy` executable, mocked package manager, mocked systemd, or a container that claims to represent an installed Omarchy machine.

Pure Blueprint unit and integration tests remain valuable underneath this layer. They are not replaced by VM acceptance tests.

### 2. Reconstruction Assurance is a required pull-request gate from its first production version

The production Reconstruction Assurance workflow runs on every pull request and is required for merge.

A red Reconstruction Assurance check blocks merge regardless of whether the apparent cause is Blueprint, Omarchy, GitHub-hosted infrastructure, or an external dependency. The distinction affects diagnosis and remediation, not whether the gate passed.

The workflow must not use `continue-on-error` for the canonical reconstruction job.

If an upstream condition makes the gate broadly unusable, changing or temporarily disabling the requirement is an explicit maintainer decision rather than an automatic bypass encoded in the harness.

### 3. Each pull request builds one pristine Omarchy base, then runs source and target as sequential disposable overlays

The workflow will:

```text
pinned official Omarchy ISO
        ↓
fresh pristine base disk
      ↙              ↘
source overlay    target overlay
Machine A         Machine B
```

The pristine base is built inside the pull-request run and remains immutable after validation.

Machine A and Machine B are separate qcow2 overlays derived from that base and run sequentially, never concurrently.

This avoids paying for two full Omarchy installations while still giving both test machines independent writable state.

The pristine base is test infrastructure only. It must never carry Blueprint desired state or Machine A customizations into Machine B.

Persistent or cached golden VM images are not part of Reconstruction Assurance v1.

### 4. Only the captured Blueprint profile crosses the source-to-target boundary

Machine A has its own:

- OS identity;
- hostname;
- Blueprint local state;
- machine binding;
- Restore/Capture journals and temporary state.

After Capture, the harness exports the portable Blueprint profile to the CI host, records a digest, destroys Machine A, creates Machine B from an independent overlay, and imports exactly that profile.

The harness must not transfer:

- Machine A's home directory as a reconstruction mechanism;
- `$XDG_STATE_HOME/omarchy-blueprint`;
- machine bindings;
- Restore journals;
- package caches;
- guest disk state;
- credentials or SSH state.

Portable machine overlay definitions that intentionally live inside the profile are part of the profile and therefore do cross the boundary.

### 5. Reconstruction Assurance is black-box with respect to Blueprint application behavior

The harness invokes the shipped Blueprint CLI produced from the exact pull-request commit.

It may inspect generated profile files and machine state as evidence, but it must not import Blueprint `internal/` packages or invoke workflow/provider functions directly to manufacture Capture or Restore success.

The acceptance layer grades externally observable behavior.

### 6. Reconstruction Assurance exercises the real Restore approval boundary

The canonical Restore flow is:

```text
structured dry-run
→ assert required/forbidden plan effects
→ invoke normal Restore without --yes
→ wait for the rendered approval prompt
→ send explicit approval
→ Blueprint re-inspects and recalculates
→ require approved plan stability
→ apply
→ Blueprint verify
→ independent machine verification
```

The first structured dry-run is test inspection, not the authority for later mutation.

The authoritative approval is the plan rendered by the normal Restore invocation immediately before the harness submits `yes`.

Blueprint's existing post-approval recalculation remains authoritative. If the plan changes after approval, the acceptance run fails. The harness must not automatically retry the Restore.

No acceptance-only Restore shortcut, approval token, internal workflow entry point, or bypass is introduced.

The v1 canonical scenario must not require an operation that Blueprint marks interactive. An unexpected interactive requirement fails the gate rather than causing the harness to switch to TUI automation or preapproval.

### 7. Blueprint verification is necessary but not sufficient

A successful Restore must pass Blueprint's own verification using the same effective Restore intent used for planning.

The harness then independently verifies Machine B through native system interfaces.

Examples include:

- `pacman` for package state;
- Omarchy theme/default/plugin queries;
- direct Shell/Config/Hook inspection;
- file hashes and modes for copied Resources;
- `git` remote, revision, and worktree state for Git Resources.

This prevents Blueprint from being the sole judge of its own result.

### 8. The v1 gate contains one canonical, representative lifecycle

Reconstruction Assurance v1 has one "portable developer" scenario rather than a persona matrix.

It covers representative mature state:

- one additional official package;
- one built-in theme selection;
- one controlled local custom plugin fixture validated by real Omarchy tooling;
- one supported Shell customization;
- one portable Config change;
- one harmless executable Hook;
- one low-risk Omarchy-managed Default;
- one copied Resource;
- one clean Git-backed Resource;
- one machine-specific Resource path mapping;
- one Restore-disabled Resource.

The concrete package/theme/default choices are fixture details, not architectural contracts. They should be selected for stability against the pinned Omarchy release.

The normal Restore intent is Safe + Additive.

Force, Exact, desired absence, dirty Git, AUR, rollback injection, migration, secret handling, GUI equivalence, and additional personas are follow-on scenarios.

### 9. Machine-aware behavior is part of the canonical acceptance claim

The profile contains stable logical Blueprint machine roles:

```text
source
target
```

The target machine has at least:

- a Resource path mapping that causes one captured Resource to restore to a different live path than on the source;
- a Restore-disabled Resource that must remain untouched.

Machine B explicitly selects `target`.

Independent verification must prove both the mapped destination and the untouched Restore-disabled state.

Capture policy, Restore policy, Restore intent, and safety remain independent concepts. The acceptance harness must not weaken provider safety to make a scenario pass.

### 10. Retry infrastructure observation and transport, never Blueprint semantics

Bounded retry or polling is permitted for inherently idempotent infrastructure behavior such as:

- ISO download transfer;
- SSH readiness;
- guest boot readiness;
- guest shutdown readiness.

Once an installer, customization, Capture, Restore, or verification operation has begun and fails, the semantic operation is not automatically retried.

In particular, the harness must not retry:

- source customization;
- Blueprint Capture;
- Restore planning;
- approval;
- Restore application;
- Blueprint verification;
- independent verification.

The first meaningful semantic failure is preserved.

### 11. Pin test substrate identity, not every package in the machine

The required gate pins:

- the GitHub runner image family, initially `ubuntu-24.04`;
- an explicit supported official Omarchy ISO version and checksum;
- the exact Blueprint pull-request commit;
- repository-controlled fixture bytes and Git fixture revision.

Inside that substrate, normal supported package resolution remains real.

The workflow must not follow an unversioned "latest" Omarchy release on every pull request. Updating the supported Omarchy pin is a deliberate repository change that itself passes Reconstruction Assurance.

### 12. Failure diagnostics are a first-class requirement

Failures must identify a phase such as:

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

The harness keeps focused diagnostics including host/VM metadata, QEMU serial logs, installer evidence, Blueprint output, profile handoff evidence, Restore plan/output/journal, and independent verification results.

Large VM disk images are not uploaded by default.

Cleanup is best-effort and must not replace the original failure status.

## Consequences

### Positive

- every merge is continuously checked against Blueprint's central reconstruction promise;
- acceptance tests exercise real Omarchy behavior instead of Blueprint's imitation of Omarchy;
- sequential overlays keep peak VM resource use low;
- rebuilding the pristine base on every pull request continues to validate the supported Omarchy installation path;
- only-profile handoff creates a strong portability boundary;
- the real approval/recalculation flow remains part of the tested safety model;
- independent verification reduces the risk of shared implementation/test bugs;
- one canonical scenario keeps the first required gate understandable and debuggable;
- later Migration Safety and Sync work inherit a trustworthy end-to-end foundation.

### Negative

- every pull request depends on a real VM, a real Omarchy installation, and external package/network infrastructure;
- required CI may occasionally block merges because of upstream or hosted-runner failures;
- building Omarchy on every pull request costs more runtime than using a persistent golden image;
- fixture choices must be maintained when the pinned Omarchy release changes;
- VM diagnostics and cleanup add repository maintenance surface;
- the first gate does not cover every safety mode or reconstruction category edge case.

## Rejected alternatives

- **Mock Omarchy/system commands for the acceptance layer:** risks proving Blueprint's imitation of Omarchy rather than real reconstruction.
- **Container-only acceptance:** does not prove the installed Omarchy/systemd/package-manager environment.
- **Install Omarchy twice per pull request:** gives little additional assurance over two independent overlays from a freshly built pristine base while duplicating the slowest external step.
- **Persistent/cached golden base image in v1:** introduces image lifecycle, invalidation, and storage concerns while ceasing to prove the contemporary official install path on every pull request.
- **Run source and target concurrently:** increases RAM and disk pressure without improving the reconstruction claim.
- **Advisory, nightly, or manual-only acceptance:** allows reconstruction regressions to merge before the end-to-end test runs.
- **`restore --yes` as the only Restore path:** does not exercise the real preview/approval boundary.
- **TUI automation for canonical Restore:** adds terminal interaction fragility without improving the core reconstruction assertion.
- **Blueprint verification as the only oracle:** risks the same bug existing in execution and verification.
- **Automatic rerun after Capture/Restore failure:** can hide nondeterministic product failures and proves eventual luck rather than deterministic reconstruction.

## References

- `ROADMAP.md` — Phase 7 Reconstruction Assurance and VM testing strategy.
- `docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md`
- `docs/adr/0016-machine-overlay-resource-path-mappings.md`
- `docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`
- CI feasibility spike: `spike/omarchy-vm-feasibility`
