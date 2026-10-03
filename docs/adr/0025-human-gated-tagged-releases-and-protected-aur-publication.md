# ADR 0025: Human-Gated Tagged Releases and Protected AUR Publication

## Status

Proposed

## Context

Omarchy Blueprint is ready to move from repository-only software to a public installable package. The Arch User Repository (AUR) was the intended first package channel. Because AUR onboarding was unavailable when the first public beta was ready, the validated PKGBUILD attached to each GitHub Release became the initial beta channel (section 15), with the AUR to follow and the Omarchy package repository intentionally deferred until Blueprint has accumulated real user installs, bug reports, security feedback, and a visible maintenance history.

Publishing to the AUR is not only a packaging task. A trustworthy package needs an upstream release identity, immutable source bytes, a version users can report, package-specific validation, and publication credentials that are isolated from untrusted code.

At this decision point:

- normal CI runs Go tests, vet, and build checks;
- Blueprint requires Omarchy 4 or newer;
- the repository has no established upstream release pipeline;
- the CLI does not yet expose an injected upstream build version;
- there is no canonical AUR PKGBUILD or `.SRCINFO` generation path;
- there is no AUR publication credential or workflow;
- Reconstruction Assurance, migration compatibility, and package publication are independent assurance domains and must remain separate.

The product goal for the first release is:

> An Omarchy 4+ user can install `omarchy-blueprint` as a pacman-owned package built from an immutable Blueprint release, initially from the release's PKGBUILD and later from the AUR, and report the exact Blueprint version they are running.

The first intended upstream release is `v0.1.0`.

## Decision

### 1. A release is a deliberate human decision represented by a SemVer tag

Blueprint upstream releases use tags of the form:

```text
vMAJOR.MINOR.PATCH
```

CI never chooses or increments an application version automatically.

A maintainer tags an exact commit only after that commit is merged to `main` and its required checks have passed. The release workflow verifies that the tag is well formed and that its commit is contained in `main` before preparing any public artifact.

The first intended public release is `v0.1.0`. Pre-1.0 communicates that the project is deliberately collecting real-world feedback before making stronger stability commitments.

### 2. Installed binaries expose injected build identity

Blueprint gains one small build-information boundary whose development default is `dev`.

Release and Arch package builds inject the application version through Go linker flags rather than rewriting or generating source code.

The root CLI exposes the normal Cobra version surface:

```text
omarchy-blueprint --version
```

A binary built for tag `v0.1.0` identifies application version `0.1.0`.

The version path must not require a profile, Omarchy detection, a TTY, or other runtime initialization.

### 3. Blueprint owns the immutable release source artifact

The canonical package source for a release is a release-owned deterministic archive created from the exact tagged Git object:

```text
omarchy-blueprint-<version>.tar.gz
SHA256SUMS
```

The archive expands beneath one versioned top-level directory.

The AUR package does not build from `main`, another moving branch, or an unpinned source. Its concrete PKGBUILD names the release archive and pins its SHA-256.

From the first public beta, each release also carries its validated `pkgrel=1` PKGBUILD (section 15).

Once a GitHub Release is public, its source archive is immutable by policy. A failed downstream publication reuses the same bytes. Application-source corrections require a new upstream version; packaging-only corrections use Arch `pkgrel` against the original source archive.

### 4. GitHub Release publication is human-gated

A pushed release tag starts verification and prepares a **draft** GitHub Release with the verified source archive, checksum and validated `pkgrel=1` PKGBUILD.

The workflow does not automatically make the upstream release public.

A maintainer reviews the draft release notes and assets and explicitly publishes the GitHub Release. That publication is the upstream distribution boundary.

Only a published GitHub Release may advance to final AUR validation and publication. AUR publication may also happen later, for an already published release (section 15).

A valid GitHub Release remains valid if AUR publication later fails.

### 5. The v1 AUR package is source-built and named `omarchy-blueprint`

The first AUR package is exactly:

```text
omarchy-blueprint
```

There is no `-git` variant and no `-bin` variant in v1.

The package builds released source with Arch-appropriate Go hardening/reproducibility flags, runs the upstream Go tests in `check()`, installs the binary to `/usr/bin/omarchy-blueprint`, and installs the upstream MIT license under `/usr/share/licenses/omarchy-blueprint/`.

The package must not invent an `omarchy` pacman dependency if no truthful package dependency represents the Omarchy 4+ runtime contract. Runtime package dependencies are audited from actual behavior and package evidence rather than guessed.

### 6. Blueprint is the packaging source of truth; the AUR repository is publication output

The Blueprint repository owns:

- the PKGBUILD template;
- packaging-source licensing;
- deterministic release tooling;
- AUR rendering and validation tooling;
- release and publication workflow definitions;
- release-maintainer documentation.

The AUR Git repository contains only the rendered publication files needed for that package revision, initially:

```text
PKGBUILD
.SRCINFO
LICENSE
```

`.SRCINFO` is always mechanically generated with `makepkg --printsrcinfo`; it is never hand-maintained as an independent source of truth.

Packaging-source files use 0BSD. Blueprint itself remains MIT licensed.

### 7. Arch package validation is separate from normal Go CI

Normal CI continues to prove the Go project builds and tests.

A distinct Arch package validation gate proves the package recipe itself. There are two moments:

1. pre-release validation against the reviewed checkout, using a locally produced deterministic source archive;
2. final validation after GitHub Release publication, using the exact public release URL and checksum that will be pushed to the AUR.

Final validation must render the concrete PKGBUILD, generate `.SRCINFO`, build and test it in a disposable clean Arch packaging environment, run `namcap`, inspect the installed files, install the package, and require the installed CLI to report the expected release version.

This gate does not rerun Reconstruction Assurance or claim to validate all Omarchy runtime behavior.

### 8. Publication trust is split into independent capabilities

Release automation has three trust domains:

**Verification:** no publication secrets.

**GitHub draft release preparation:** only the GitHub permission necessary to prepare the draft release and upload verified assets; no AUR key.

**AUR publication:** the AUR credential becomes available only in a protected `aur-release` GitHub Environment after package validation and required human approval.

Pull requests, ordinary pushes, and pre-publication validation cannot receive the AUR private key.

The publication workflow must not use `pull_request_target` to execute untrusted repository code with elevated publication authority.

### 9. AUR authentication uses a dedicated revocable SSH identity

The AUR publisher uses a CI-specific SSH key registered with the maintainer's AUR account.

It must not reuse a maintainer's general GitHub/workstation SSH identity.

SSH host verification remains enabled. The protected publication environment supplies a manually verified `aur.archlinux.org` known-hosts value or equivalent pinned trust input. `StrictHostKeyChecking=no` is prohibited.

The CI key can be revoked independently without affecting other identities.

### 10. First publication retains a human ownership gate

Before the first AUR push, a maintainer confirms that the `omarchy-blueprint` package base is available or appropriately under their control, configures the AUR account/key and GitHub protected environment, and reviews the rendered first-release package.

Automation does not resolve a naming conflict by inventing another package name.

The AUR branch is `master`, and the initial commit includes `.SRCINFO`.

### 11. Application versions and Arch `pkgrel` have distinct meanings

Application/source changes require a new upstream release and reset `pkgrel` to 1:

```text
0.1.0-1 → 0.1.1-1
```

Packaging-only corrections against unchanged upstream source increment `pkgrel`:

```text
0.1.0-1 → 0.1.0-2
```

A packaging-only revision is reviewed and merged in Blueprint first, then dispatched explicitly with the existing upstream version and a higher `pkgrel`. It reuses and verifies the immutable release source asset, performs the same final package validation, and requires the same protected AUR approval.

Infrastructure retries of unchanged publication content do not change `pkgver` or `pkgrel`.

### 12. AUR publication is retry-safe and never rewrites history

If the GitHub Release succeeds but AUR publication fails, the workflow can be rerun against the same release asset and checksum.

If the exact `pkgver-pkgrel` and publication content already exist on AUR, a retry reports success without creating a meaningless duplicate commit.

If the same package revision exists with different content, publication fails for human investigation.

The workflow never force-pushes AUR history.

### 13. Critical release behavior stays in reviewable repository tooling

Credential-bearing workflows minimize opaque marketplace dependencies. GitHub Actions used by publication workflows are pinned to immutable commit SHAs where practical, and substantial release/rendering logic lives in reviewed repository scripts rather than hidden inside third-party release actions.

All external version/tag inputs are strictly validated before interpolation into shell commands or paths.

### 14. AUR is the long-term public package channel; Omarchy repository inclusion is later

Release/AUR v1 does not operate a custom pacman repository and does not target Arch official repositories.

The intended sequence is:

```text
GitHub Release beta (release PKGBUILD, section 15)
→ AUR publication
→ public user/security soak and maintenance history
→ later proposal to the Omarchy package repository
```

The Omarchy repository proposal is a separate human-reviewed tranche after the project has evidence of successful installs, responsive maintenance, and no unresolved high-severity security issue.

### 15. Amendment: pre-AUR beta distribution through GitHub Releases

AUR remains the intended long-term package channel. Because new AUR account registration was unavailable when the first public beta was ready, each GitHub Release also carries the release's validated `pkgrel=1` PKGBUILD, so users can install a release with ordinary Arch tooling before the AUR package exists:

```text
omarchy-blueprint-<version>.tar.gz
SHA256SUMS
PKGBUILD
```

- The release PKGBUILD is rendered by the same tooling from `packaging/aur/PKGBUILD.template`, which stays the single recipe source of truth. Rendered PKGBUILDs and `.SRCINFO` are outputs and are never committed.
- Before the draft is prepared, the Release workflow renders it for the source archive's final public URL and checksum and runs the full Arch package validation against the locally built archive. Users build it with `makepkg -si`, which verifies the pinned SHA-256 and gives normal pacman ownership.
- `.SRCINFO` and the 0BSD packaging `LICENSE` are not release assets: `makepkg` does not need them, and they are still generated for the AUR repository. No prebuilt binary is published.
- The release PKGBUILD is an immutable release asset like the archive. While the package is not on the AUR, a recipe correction therefore requires a new upstream PATCH release; `pkgrel` revisions apply once AUR publication exists.
- AUR publication is a later, explicit step that does not block a GitHub Release. The credential-bearing AUR push runs only when the repository enables it (`AUR_PUBLISHING_ENABLED`); final package validation from the public URL always runs.
- The first AUR publication of a release, whether from the `release: published` event or a later manual dispatch with `pkgrel=1`, renders the recipe from the release tag and must be byte-identical to the release's `PKGBUILD` asset. Users who installed from the release therefore have exactly the package the AUR later serves.

## Consequences

### Positive

- Every public package maps to an exact upstream release and checksum.
- Users can report an exact Blueprint version.
- AUR packaging is reviewable in the upstream repository.
- Upstream release success is independent from downstream AUR availability.
- Packaging-only fixes do not require fake application releases.
- AUR credentials are isolated behind validation and human approval.
- AUR publication is retryable without rewriting release bytes or history.
- The first public channel is simple enough to maintain while the project gathers real-world feedback.
- Beta users can install a validated, pacman-owned release before AUR onboarding is possible, and later receive the identical package from the AUR.
- The packaging recipe can later provide useful evidence for an Omarchy package-repository proposal.

### Negative

- GitHub Release publication is the deliberate human release gate. AUR publication has its own protected human approval gate, which may come later.
- The repository gains additional release scripts, workflow definitions, and packaging tests.
- GitHub-hosted package validation may use a disposable Arch container rather than Arch's strongest `devtools` clean-chroot machinery; this limitation must be documented if retained.
- A dedicated AUR key and protected environment require one-time operational setup.
- AUR users build from source, so installation is slower than a precompiled `-bin` package.
- Until the package is on the AUR, users update by downloading the next release's PKGBUILD, and recipe-only fixes cost a PATCH release.
- The release-asset PKGBUILD and the first AUR publication are coupled: the maintainer identity in the rendered recipe must not change between them, or the release must be published to the AUR with `pkgrel >= 2`.

## Rejected alternatives

### Publish from `main`

Rejected because a moving branch is not a stable support or checksum identity.

### Automatically choose versions from commits

Rejected because release timing and versioning are product decisions, not a side effect of merging code.

### Automatically publish GitHub Releases from tags

Rejected because release notes and public upstream publication remain human-owned. Tag automation prepares a draft; a maintainer publishes it.

### Start with `omarchy-blueprint-git`

Rejected because it creates a continuously moving support channel during the project's public stabilization period.

### Start with `omarchy-blueprint-bin`

Rejected because it adds precompiled-binary architecture, artifact trust, and distribution work before source-build usability has proven insufficient.

### Use GitHub convenience source archives as the AUR source contract

Rejected in favor of a release-owned deterministic archive and checksum produced by Blueprint's release process.

### Maintain `.SRCINFO` independently

Rejected because `.SRCINFO` is derived metadata and must be generated from the concrete PKGBUILD.

### Give the release job the AUR private key

Rejected because upstream verification/publication and downstream registry publication do not require the same authority.

### Disable SSH host verification in CI

Rejected because convenience does not justify surrendering the authenticity guarantee of the only credential-bearing network mutation.

### Host a Blueprint pacman repository now

Rejected because AUR satisfies the initial distribution goal without taking on repository signing, hosting, database, retention, and availability responsibilities.

### Target official Arch or the Omarchy package repository immediately

Rejected as the first release target. The project wants public AUR soak and a maintenance/security track record first.

## References

- `docs/planning/specs/2026-09-29-release-aur-v1-design.md`
- Arch package guidelines
- Arch Go package guidelines
- Arch creating-packages / clean-build guidance
- AUR submission guidelines
- Arch `.SRCINFO` documentation
