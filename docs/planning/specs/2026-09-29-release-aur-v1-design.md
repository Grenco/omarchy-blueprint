# Release and AUR Publication v1 Design

## Status

Proposed.

This design follows the approved conversational architecture review for publishing Omarchy Blueprint as a versioned upstream release and an Arch User Repository package.

It is based on `main` at `4261ba0dbe4808d02e9cf6b3573b0038fb100736` on 2026-09-29. At design time the repository has normal Go CI but no GitHub Releases and no AUR package publication pipeline.

This document is a design specification, not an implementation plan. It does not authorize production changes, GitHub mutation, AUR publication, credential creation, or release creation by itself.

## Summary

The first public distribution milestone is:

> An Omarchy 4+ user can install `omarchy-blueprint` from the AUR, receive a package built from an immutable Blueprint release, and report the exact Blueprint version they are running.

The initial release line is pre-1.0 and begins with `v0.1.0`.

The publication model is:

```text
human chooses version
→ tag exact main commit
→ verify exact tag
→ build immutable release source archive
→ prepare draft GitHub Release with assets
→ human reviews notes and publishes GitHub Release
→ render exact AUR package metadata
→ build/test package in clean Arch environment
→ human-approved AUR publish job
→ push PKGBUILD/.SRCINFO/package-source license to AUR
```

The AUR package is source-built and named exactly:

```text
omarchy-blueprint
```

There is no `-git` or `-bin` variant in v1.

The long-term distribution sequence is intentionally:

```text
AUR release
→ public announcement
→ real-world Omarchy user testing and security feedback
→ stabilization
→ later proposal to the Omarchy package repository
```

Official Arch repository inclusion and a self-hosted pacman repository are not goals of this tranche.

## Product and release intent

Blueprint is moving from repository-only software to a public package people can install and report bugs against.

Publication therefore needs more than a PKGBUILD. It needs a stable upstream release identity, immutable source inputs, an auditable package recipe, package-level validation, and tightly scoped publication credentials.

The release system must optimize for these properties:

1. A release is a deliberate maintainer decision, never an automatic consequence of merging code.
2. A released version maps to one exact Git commit.
3. Released source bytes do not change after publication.
4. The AUR package builds from those released bytes rather than a moving branch.
5. The installed binary can identify its upstream release version.
6. A failed AUR publication can be retried without creating a new upstream version.
7. AUR credentials are never exposed to untrusted pull-request code.
8. Packaging failures do not weaken normal product CI or Reconstruction Assurance.
9. Omarchy-specific runtime requirements remain truthful rather than being represented by invented pacman dependencies.
10. The first packaging system stays small enough to maintain confidently.

## Goals

1. Establish semantic-versioned upstream releases beginning at `v0.1.0`.
2. Expose the installed Blueprint build version through `omarchy-blueprint --version`.
3. Produce an immutable source archive and SHA-256 for each upstream release.
4. Add a canonical AUR packaging recipe to the Blueprint repository.
5. Generate `.SRCINFO` from the rendered PKGBUILD rather than maintaining it independently.
6. Validate the package in a clean Arch environment before publication.
7. Publish/update `omarchy-blueprint` on the AUR through a dedicated protected workflow.
8. Keep publication credentials isolated and revocable.
9. Document the one-time AUR maintainer setup and recurring release procedure.
10. Make AUR publication independently retryable for an already-created upstream release.

## Non-goals

Release/AUR v1 does not add:

- an `omarchy-blueprint-git` AUR package;
- an `omarchy-blueprint-bin` AUR package;
- a self-hosted pacman repository;
- inclusion in Arch `core`/`extra` or any other official Arch repository;
- inclusion in the Omarchy package repository;
- package signing with a Blueprint-owned pacman repository key;
- automatic version selection or automated semantic-release behavior;
- automatic changelog generation from commit messages;
- multi-architecture support beyond architectures proven by the package validation design;
- Homebrew, Nix, Debian, RPM, Flatpak, Snap, container, or other distribution formats;
- any change to Capture, Restore, compatibility, Reconstruction Assurance, or provider semantics;
- release-time mutation of user profiles;
- telemetry.

The Omarchy repository is a deliberate next phase after public AUR soak and stabilization.

## Current repository findings

### Existing CI is sufficient as the product verification base

Current CI runs on push and pull request, tests Go 1.25.x and 1.26.x, and executes:

```text
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Release verification should reuse the same product expectations on the exact tag rather than invent a second product-quality standard.

### No upstream release identity exists yet

At design time the GitHub Releases collection is empty.

The CLI root currently has no release build identity wired into Cobra. A public package needs an exact installed version for bug reports and support.

### The project already has an appropriate upstream license

Blueprint is MIT licensed. The installed package must include the upstream MIT license file under the normal Arch license path.

The AUR package-source repository is a separate packaging work product. Its local packaging files should follow current Arch guidance and use 0BSD so they are eligible for future promotion workflows if ever relevant.

## Decision

### 1. Human-selected semantic versions

Upstream releases use SemVer-style tags:

```text
vMAJOR.MINOR.PATCH
```

The first intended public release is:

```text
v0.1.0
```

Pre-1.0 communicates that public interfaces and behavior are still stabilizing through real user feedback.

CI never decides when to release and never increments versions automatically.

A maintainer deliberately creates a release tag only after the intended commit is merged to `main` and its required checks have passed.

The release workflow must reject:

- malformed release tags;
- tags whose commit is not contained in `main`;
- a release version whose release asset already exists with different bytes;
- attempts to republish a version from a different commit.

A tag is release authority, but only after the release workflow verifies these invariants.

### 2. Build version is injected, not generated into source

Introduce one small build-information boundary, conceptually:

```go
package buildinfo

var Version = "dev"
```

Normal local builds report `dev`.

Release and Arch package builds inject the upstream version through Go linker flags. No generated Go source file is committed and no release job rewrites source code merely to embed a version.

The Cobra root exposes this as the normal version surface:

```text
omarchy-blueprint --version
```

The release contract is that a `v0.1.0` package reports version `0.1.0` (the application version excludes the Git tag's leading `v`).

`--version` must not initialize a profile, detect Omarchy, require a TTY, or perform any mutation. It is usable for support and package validation even outside a configured Blueprint profile.

### 3. The canonical release source artifact is produced by Blueprint

The AUR package must not build from `main`, another moving ref, or a convenience archive whose byte identity is outside the release workflow's control.

For each release, the workflow creates:

```text
omarchy-blueprint-<version>.tar.gz
SHA256SUMS
```

from the exact tagged commit using a deterministic `git archive`-based process.

The archive must expand beneath one versioned top-level directory suitable for a conventional PKGBUILD source tree, for example:

```text
omarchy-blueprint-0.1.0/
```

The archive generation process must normalize the properties needed for repeatable bytes from the same Git object. The implementation plan must include an automated assertion that producing the source archive twice from the same commit yields the same SHA-256.

`SHA256SUMS` records the release asset checksum and is uploaded beside the archive.

Once a GitHub Release is published, that version's source archive is immutable. A publication retry reuses the same asset and checksum; it does not rebuild-and-replace the release bytes.

### 4. GitHub Release is the upstream publication boundary

The release flow has separate logical stages:

```text
tag pushed
→ verify tag/commit
→ product tests
→ create/check deterministic source archive
→ prepare draft GitHub Release with assets
→ human reviews/edits release notes and publishes the release
→ release:published event
→ validate the exact final AUR package against the published asset
→ protected AUR publication
```

The tag-triggered workflow may create/update only the draft release for that verified tag. It does not make the upstream release public automatically.

A maintainer reviews the draft assets and release notes, then deliberately publishes the GitHub Release. That human publish action is the upstream publication boundary.

Draft preparation and GitHub Release publication require only the permissions needed to create the release and upload assets. They must not receive AUR credentials.

A successful published GitHub Release remains valid even if the later AUR publication stage fails. The AUR stage is therefore retryable against a known upstream release.

### 5. The initial AUR package is source-built and unsuffixed

The v1 package name is:

```text
omarchy-blueprint
```

It builds a specific released version from source, so it has neither a VCS suffix nor a binary-package suffix.

A `-git` package would introduce a second update/support channel before the stable release process has proven itself. A `-bin` package would add precompiled-asset architecture and supply-chain work without a demonstrated need.

Both are deferred.

### 6. Canonical packaging source lives in the Blueprint repository

Blueprint is the source of truth for the AUR packaging recipe.

The intended repository shape is conceptually:

```text
packaging/aur/
  PKGBUILD.template
  LICENSE

scripts/release/
  make-source-archive.sh
  render-aur-package.sh
```

Exact filenames may be refined in the implementation plan if a clearer existing repository convention is discovered, but ownership remains the same:

- Blueprint repository: authored packaging template and release tooling;
- AUR Git repository: rendered publication output only.

The AUR repository contains the minimal publication set:

```text
PKGBUILD
.SRCINFO
LICENSE
```

Additional local package-source files are added only if the package genuinely needs them.

`LICENSE` in the AUR package-source repository covers the packaging source itself and uses 0BSD. It does not replace Blueprint's MIT upstream license.

### 7. PKGBUILD version and checksum are rendered from the release

The PKGBUILD template is not allowed to guess the current release.

The renderer receives explicit immutable inputs:

- upstream version;
- release asset URL;
- release asset SHA-256;
- release commit/tag identity where useful for verification.

It produces a concrete PKGBUILD whose `pkgver` and `sha256sums` exactly match the upstream release.

The generated PKGBUILD must not contain `SKIP` for the upstream release source.

`.SRCINFO` is always generated from the concrete PKGBUILD with:

```text
makepkg --printsrcinfo
```

It is never hand-edited and never treated as an independently authored source of truth.

### 8. Initial package metadata

The intended package identity is:

```text
pkgname=omarchy-blueprint
pkgver=<release version without v>
pkgrel=1
pkgdesc='Portable capture and restore for Omarchy'
arch=('x86_64')
url='https://github.com/Grenco/omarchy-blueprint'
license=('MIT')
makedepends=('go')
```

`x86_64` is the initial architecture because that is the environment this release pipeline is intended to validate first. Additional architectures require an explicit successful package/runtime validation decision rather than being advertised optimistically.

Runtime `depends` and `optdepends` are not guessed in this design. The implementation must audit the built binary and Blueprint's external command usage, use `namcap` as supporting evidence, and include only truthful package dependencies.

In particular, do not invent a pacman dependency named `omarchy` merely because Blueprint requires Omarchy 4+. If the target system does not expose an appropriate package dependency that can truthfully represent that requirement, keep Omarchy 4+ as an application/runtime requirement in documentation and runtime validation.

### 9. Arch Go build follows current packaging guidance

The PKGBUILD builds the application directly rather than wrapping normal repository CI.

The build should follow current Arch Go package guidance, including the appropriate environment and flags for:

- PIE/hardening;
- `-trimpath`;
- read-only Go module behavior;
- Arch compiler/linker flags where applicable;
- deterministic release version injection.

The exact flag string belongs in the implementation plan because it must be verified against the current Go toolchain and whether Blueprint uses CGO for the package build.

The package's `check()` function runs the upstream Go test suite.

The packaging build must not modify `go.mod` or `go.sum`.

### 10. Package installation layout is conventional and minimal

The package installs at least:

```text
/usr/bin/omarchy-blueprint
/usr/share/licenses/omarchy-blueprint/LICENSE
```

No files are installed under `/usr/local`.

No user profile, configuration, cache, state directory, systemd unit, shell hook, or default profile is created by package installation unless a later separately reviewed product requirement introduces one.

Installing the package is therefore non-mutating with respect to a user's Blueprint state.

### 11. Clean Arch package validation is a distinct gate

Ubuntu Go CI proves the upstream Go project builds and tests.

AUR validation must additionally prove that the Arch package recipe itself works in an Arch environment.

There are two package-validation moments:

1. **Pre-release packaging validation** on pull requests/main builds the current checkout through the same PKGBUILD template using a locally produced source archive. This catches packaging regressions before anyone creates a release tag, but it is not publication evidence because no public release asset exists yet.
2. **Final release packaging validation** runs after the immutable GitHub Release asset exists and uses the exact public URL/checksum that will be pushed to the AUR. Only this final validation authorizes AUR publication.

Before AUR publication, final validation must:

1. render the exact final PKGBUILD using the public release asset URL and checksum;
2. generate `.SRCINFO`;
3. build in a clean Arch environment using Arch packaging tooling;
4. run the PKGBUILD `check()` phase;
5. run `namcap` against the PKGBUILD;
6. run `namcap` against the built package;
7. inspect the package file list;
8. install the built package into a disposable Arch environment;
9. execute `omarchy-blueprint --version` and require the exact release version;
10. prove the installed binary comes from the package under test.

Prefer Arch `devtools`/clean-chroot mechanisms where feasible over a container polluted by arbitrary preinstalled build dependencies. If GitHub-hosted runner constraints make a container necessary, the implementation plan must preserve an equivalent clean-build guarantee and document the limitation.

This package validation does not attempt to run Capture/Restore. Reconstruction Assurance and other product tests remain responsible for real Omarchy behavior. The package job proves packaging/installability/version identity.

### 12. Release workflow trust is split by capability

Release automation must keep these trust domains distinct:

#### Verification

Runs without publication secrets.

It may read repository contents, fetch the `main` ref, run tests, and build artifacts.

#### GitHub Release publication

Receives only the minimum GitHub permission necessary to create the GitHub Release and upload the already-verified deterministic source artifacts.

It does not receive the AUR private key.

#### AUR publication

Receives the AUR publishing credential only after all package validation has passed.

It uses a protected GitHub Environment such as:

```text
aur-release
```

with required human approval.

The AUR credential is unavailable to pull-request workflows, ordinary pushes, package validation jobs, and arbitrary reusable jobs that run repository-controlled commands before the approval boundary.

### 13. AUR authentication uses a dedicated revocable key

The AUR publisher uses a dedicated SSH key created only for Blueprint publication.

The public key is added to the maintainer's AUR account. The private key is stored only in the protected release environment.

The key must not be:

- a maintainer's general GitHub SSH key;
- a personal workstation identity reused for convenience;
- committed to the repository;
- printed in workflow logs.

SSH host verification remains enabled.

The workflow must establish the trusted `aur.archlinux.org` host key through a documented/pinned trusted mechanism. `StrictHostKeyChecking=no` is prohibited.

Multiple AUR account keys are acceptable and preferred because the CI-specific key can then be revoked independently.

### 14. First AUR publication has a human ownership gate

Before first publication, a human maintainer must establish that the intended package base is available or appropriately under their control.

Automation may preflight package existence, but it must not solve a naming conflict by inventing a different package name.

If `omarchy-blueprint` is unexpectedly owned by another AUR maintainer, the publication job stops and reports the conflict for human resolution.

The initial AUR push targets the required `master` branch and includes `.SRCINFO` in the first commit.

The initial push must not occur until the human has:

- created/configured the AUR account;
- registered the dedicated CI public key;
- configured the protected GitHub Environment/secrets;
- supplied the AUR maintainer identity used in the PKGBUILD comments;
- reviewed the rendered first-release package.

### 15. Subsequent AUR releases update one package base

For later upstream versions, the publication job obtains the existing AUR Git repository, replaces the rendered publication files, verifies the diff is limited to expected package-source files, commits the update, and pushes `master`.

AUR commit messages should identify the published package version, for example:

```text
omarchy-blueprint 0.1.1-1
```

The automation must fail rather than force-push or rewrite published AUR history.

### 16. Upstream version and pkgrel have different meanings

Application changes require a new upstream release:

```text
v0.1.0 → v0.1.1
pkgrel resets to 1
```

Packaging-only changes for unchanged upstream source use an Arch `pkgrel` increment:

```text
0.1.0-1 → 0.1.0-2
```

A packaging-only AUR correction must continue to use the exact same immutable upstream source archive and checksum.

Release/AUR v1 supports both cases explicitly:

- a `vX.Y.Z` upstream release publishes `pkgrel=1`;
- a packaging-only correction is first reviewed and merged as a packaging change on `main`, then a maintainer manually dispatches an AUR-revision workflow with an existing upstream version and an explicit higher `pkgrel`.

The AUR-revision path must download and verify the existing immutable GitHub Release source asset, render the newly reviewed package metadata, run the same final clean Arch validation, require the same protected AUR environment approval, and update only the AUR package. It must not create, modify, or replace the existing GitHub Release.

Infrastructure retry of an unchanged `pkgver-pkgrel` is separate from a packaging correction and remains idempotent; it does not increment `pkgrel`.

### 17. Publication is retry-safe

These outcomes are intentionally independent:

```text
GitHub Release succeeded + AUR failed
```

is not an invalid release.

The AUR job can be rerun for the same release only if it proves the GitHub Release asset checksum still matches the expected package input.

A rerun must be idempotent:

- if the AUR commit for that exact `pkgver-pkgrel` and content is already present, report success without creating meaningless duplicate history;
- if the same version is present with different package content, fail for human investigation;
- never bump the application version merely to recover from infrastructure failure.

### 18. Release assets are immutable by policy

Once published, release assets for a version are not overwritten with different bytes.

If application source or release content is wrong, cut a new upstream version.

If only AUR packaging metadata is wrong, use the `pkgrel` mechanism against the original release asset.

This protects the checksum relationship users and the AUR package rely upon.

### 19. Release notes are human-reviewed before publication

The tag workflow creates a **draft** GitHub Release after verification and asset generation.

The draft may start with generated release notes as an editing aid, but generated text is not publication authority. A maintainer reviews/edits the notes and deliberately publishes the release through GitHub.

Only the published release is a supported upstream distribution point, and only the `release: published` event may advance to final AUR validation.

This keeps release communication human-owned without requiring release-note files to be committed for every version.

### 20. GitHub Actions publication dependencies are pinned

Credential-bearing release workflows should pin third-party GitHub Actions to immutable commit SHAs where practical.

First-party GitHub actions should also be evaluated for pinning in the release workflow rather than inheriting the looser convenience policy of ordinary CI.

Repository scripts should contain the release logic where practical so critical behavior is reviewable in the project rather than hidden in opaque marketplace actions.

### 21. Publication cannot be triggered by untrusted pull-request code

No path from a fork pull request or ordinary branch push can obtain AUR credentials or directly publish a release.

At minimum:

- tag verification confirms the release commit is on `main`;
- AUR publication requires the protected environment;
- package validation completes before environment-secret access;
- workflows do not use `pull_request_target` to execute untrusted repository code with publication permissions.

### 22. Documentation distinguishes installation channels

The README gains an installation section that clearly distinguishes:

```text
AUR installation
building from source
```

The first public announcement may recommend an AUR helper for convenience, but project documentation should also describe the underlying AUR package identity and avoid implying that AUR helpers are part of pacman itself.

The docs must state the runtime support boundary:

```text
Omarchy 4+
```

and explain how to report:

```text
omarchy-blueprint --version
```

when filing issues.

### 23. Release maintainer documentation is required

Add a maintainer-facing release document covering:

- prerequisites and required permissions;
- one-time AUR account/key/environment setup;
- how to choose and verify a version;
- how to create the release tag;
- how to review package validation output;
- how protected AUR approval works;
- how to retry a failed AUR publication;
- how to perform a packaging-only `pkgrel` fix;
- how to revoke/replace the AUR CI key;
- what must never be changed in an existing release;
- what evidence to collect before approaching the Omarchy package repository.

The runbook must make clear which steps mutate GitHub or AUR and therefore require human authority.

## Initial repository ownership map

The implementation should converge on responsibilities like these:

```text
internal/buildinfo/
  release build identity

cmd/omarchy-blueprint or internal/app/
  expose --version without runtime/profile initialization

packaging/aur/
  canonical package template and package-source licensing

scripts/release/
  deterministic source archive
  AUR rendering/validation helpers

.github/workflows/
  package validation
  tag-triggered release publication
  protected AUR publication

docs/
  user installation guidance
  maintainer release runbook
```

The implementation plan should prefer small focused files over embedding substantial shell logic directly inside workflow YAML.

## Release workflow state model

A useful conceptual state model is:

```text
TAGGED
  ↓
VERIFIED
  ↓
SOURCE_BUILT
  ↓
DRAFT_RELEASE_READY
  ↓  (human publishes)
GITHUB_RELEASED
  ↓
PACKAGE_VALIDATED
  ↓
AWAITING_AUR_APPROVAL
  ↓
AUR_PUBLISHED
```

Failures before `GITHUB_RELEASED` mean there is no public upstream release produced by this workflow.

Failures during final package validation or AUR publication occur after the immutable upstream release exists. They leave a valid GitHub Release and are resumed against that same release asset; they do not justify replacing the asset or minting a new application version solely for infrastructure recovery.

The workflow must surface this distinction clearly in job names and summaries.

The pre-release packaging-validation workflow is intentionally outside this release-state chain: it proves the packaging template on normal reviewed code before a tag exists, while the `PACKAGE_VALIDATED` state above means the final AUR package was built from the published release asset.


## Package verification contract

A successful AUR validation result means only:

> The exact released source can be built into a policy-conforming Arch package in a clean Arch packaging environment, installed successfully, and the installed CLI reports the expected Blueprint release version.

It does not mean:

- all Omarchy runtime behavior has been revalidated;
- Reconstruction Assurance passed as part of packaging;
- every supported Omarchy version was tested;
- the package is admitted to any official repository.

These guarantees remain separate to avoid one green check overstating what it proves.

## Security and supply-chain requirements

Release/AUR v1 must preserve these security boundaries:

1. No AUR private key in repository contents, artifacts, caches, or logs.
2. No AUR secret exposure to pull requests.
3. No disabled SSH host verification.
4. No release source with skipped checksum validation.
5. No moving branch as AUR stable-package source.
6. No force-pushing AUR publication history.
7. No replacement of already-published release bytes.
8. No package build as root.
9. No `curl | sh` or equivalent remote code execution pattern in packaging.
10. No release metadata interpolated into shell commands without strict validation/quoting.
11. No package installation scripts that mutate user home directories.
12. No automatic privilege escalation by package install beyond normal pacman installation mechanics.
13. Publication Actions and scripts must use least privilege.
14. Artifacts uploaded from validation must not contain SSH keys, tokens, or temporary secret material.

## Testing strategy

### Unit/focused tests

Add focused tests for build version behavior:

- default development version;
- release-injected version formatting where testable without rebuilding the whole application;
- `--version` does not invoke profile/Omarchy-dependent startup paths.

Release scripts should have deterministic, offline-friendly tests where practical, especially:

- SemVer/tag validation;
- tag-to-version normalization;
- PKGBUILD rendering;
- checksum substitution;
- source archive reproducibility;
- idempotent AUR content comparison;
- rejection of malformed/untrusted version input.

### Existing Go verification

The release commit must pass the existing Go test/vet/build expectations.

### Arch package validation

The rendered package must pass the clean Arch packaging gate described above.

### Workflow testing

Publishing credentials must not be required to test most of the workflow logic.

Dry-run/local script modes should allow an implementation worker and reviewers to verify rendered artifacts without mutating GitHub or AUR.

The first real AUR publication remains a human-observed release event rather than a blind test of deployment code.

## Operational rollout

### Phase 1 — land release/AUR infrastructure without publishing

Merge the reviewed implementation with:

- build version support;
- packaging sources;
- release scripts;
- validation workflow;
- release/AUR workflow definitions;
- documentation.

Secrets may remain unconfigured at this point.

The merged code must be testable in dry-run/validation mode without an AUR account.

### Phase 2 — one-time human publication setup

A maintainer:

- confirms the AUR package name;
- creates/configures the AUR account if needed;
- creates the dedicated publishing key;
- registers the public key;
- configures the GitHub protected environment and private key;
- configures required environment reviewers;
- supplies the final package maintainer identity;
- reviews the first rendered PKGBUILD.

### Phase 3 — create `v0.1.0`

A maintainer deliberately tags the chosen merged `main` commit.

The tag pipeline verifies, packages, and prepares the draft GitHub Release. A maintainer reviews the draft and publishes it. The published-release workflow then validates the exact final AUR package, pauses for protected AUR approval, and publishes the first AUR revision.

### Phase 4 — public soak

After publication:

- announce the package;
- collect installation failures, runtime defects, security reports, and packaging issues;
- release fixes through normal version/pkgrel semantics;
- build a visible maintenance history.

### Phase 5 — Omarchy package repository proposal

Only after the AUR release has had meaningful public use and the project has demonstrated stable maintenance should Blueprint prepare a separate proposal/PR for the Omarchy package repository.

That later tranche may reuse the validated package recipe but is not authorized or designed here.

## Criteria for approaching the Omarchy package repository later

This design does not set an arbitrary download threshold, but the follow-up should have evidence of:

- a working upstream release process;
- a maintained AUR package;
- successful installs by users other than the maintainers;
- resolved initial packaging/runtime issues;
- no known unresolved high-severity security issue;
- clear Omarchy 4+ compatibility expectations;
- a support/reporting path using exact Blueprint versions;
- confidence that package updates will be maintained promptly.

The decision to submit to the Omarchy repository remains a new human-reviewed tranche.

## Rejected alternatives

### Publish directly from `main`

Rejected because a moving branch is not a release identity and makes support, rollback, checksums, and AUR reproducibility weaker.

### Let CI automatically choose versions

Rejected because publication is a product/communication decision, not merely a build event.

### Start with `omarchy-blueprint-git`

Rejected because it creates a second support channel and gives early users continuously moving code when the project specifically wants a public stabilization period.

### Start with `omarchy-blueprint-bin`

Rejected because it adds prebuilt binary architecture, artifact signing/trust, and multi-platform concerns before there is evidence source builds are a usability problem.

### Host a private pacman repository

Rejected because AUR already provides an appropriate first public Arch distribution channel, while operating a pacman repository adds signing-key, repository-database, hosting, retention, and availability responsibilities.

### Target official Arch repositories now

Rejected as an immediate deliverable because official repository adoption is controlled by Arch maintainers and is not something Blueprint's release CI can publish into directly.

### Target the Omarchy package repository before AUR soak

Rejected for the first release because the project wants public user testing and a maintenance/security track record before asking Omarchy to distribute Blueprint as part of its repository ecosystem.

### Store `.SRCINFO` as an independently edited template

Rejected because `.SRCINFO` is derived package metadata and must remain mechanically generated from the concrete PKGBUILD.

### Use GitHub's moving/convenience source archive as the sole AUR source contract

Rejected in favor of a release-owned archive whose bytes and checksum are produced and retained by the Blueprint release workflow.

### Give AUR credentials to the whole release job

Rejected because verification and upstream publication do not need downstream package-registry credentials.

## External packaging references

Implementation and review should verify behavior against current upstream Arch documentation, particularly:

- Arch package guidelines: `https://wiki.archlinux.org/title/Arch_package_guidelines`
- Go package guidelines: `https://wiki.archlinux.org/title/Go_package_guidelines`
- Creating packages / clean build guidance: `https://wiki.archlinux.org/title/Creating_packages`
- AUR submission guidelines: `https://wiki.archlinux.org/title/AUR_submission_guidelines`
- `.SRCINFO`: `https://wiki.archlinux.org/title/.SRCINFO`

Arch guidance can evolve. If implementation-time current guidance conflicts with a mechanical detail in this design, stop and bring the difference back for review rather than silently weakening integrity or security requirements.

## Review gate

After this spec is approved, the next step is a detailed implementation plan.

That plan should decompose work into reviewable slices, likely:

1. build version and release artifact primitives;
2. AUR packaging and clean Arch validation;
3. protected release/AUR publication workflow and documentation;
4. first-publication operational checklist.

No actual release, tag, GitHub Release, AUR credential configuration, or AUR push should occur merely because the implementation lands. First publication remains an explicit human-authorized operation.
