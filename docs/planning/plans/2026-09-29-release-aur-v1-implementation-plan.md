# Release and AUR Publication v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish human-gated `vX.Y.Z` upstream releases and a validated, protected publication path for the source-built `omarchy-blueprint` AUR package, beginning with the intended `v0.1.0` release.

**Architecture:** Keep release identity, Arch packaging, and downstream publication as separate trust boundaries. Inject build identity through Go linker flags; produce an immutable deterministic source archive; render a concrete PKGBUILD from explicit release inputs; validate the package in a disposable Arch environment; prepare draft GitHub Releases from tags; and expose AUR credentials only to a protected publication job after final package validation and human approval.

**Tech Stack:** Go/Cobra, Bash, Python 3 standard library, Git, GitHub Actions, GitHub CLI, Arch `makepkg`/pacman/namcap, Docker job containers, SSH/AUR Git.

**Spec:** `docs/planning/specs/2026-09-29-release-aur-v1-design.md`

**ADR:** `docs/adr/0025-human-gated-tagged-releases-and-protected-aur-publication.md`

## Global Constraints

- Implementation starts from the latest merged `main`; do not pin implementation to the design-time SHA. At plan authoring, `main` is `4261ba0dbe4808d02e9cf6b3573b0038fb100736` and ADR 0024 is already owned by semantic user-service reconstruction.
- Use an isolated worktree before code changes. Do not work in or depend on another agent's systemd-service branch/worktree.
- Before changes run: `go mod download`, `go test ./...`, `go vet ./...`, `go build ./cmd/omarchy-blueprint`, `git status --short`. Stop and report any pre-existing failure rather than folding unrelated fixes into this tranche.
- Copy the approved spec, this plan, and ADR 0025 into the repository unchanged before implementation work begins.
- No task in this plan authorizes creating a Git tag, publishing a GitHub Release, configuring GitHub secrets/environments, creating an AUR account/key, or pushing to AUR. Those are later human-authorized operational actions.
- The first intended upstream version is `v0.1.0`, but implementation workflows must support any strict `vMAJOR.MINOR.PATCH` release tag.
- Application version strings exclude the leading tag `v` (`v0.1.0` → `0.1.0`).
- Stable AUR package name is exactly `omarchy-blueprint`; do not create `-git`, `-bin`, or alternate-name packages.
- Upstream Blueprint code remains MIT. AUR package-source files authored for publication use 0BSD.
- AUR source is the immutable Blueprint release asset `omarchy-blueprint-<version>.tar.gz` with an exact SHA-256; never `SKIP`, never a moving branch.
- `.SRCINFO` is generated only from the rendered concrete PKGBUILD using `makepkg --printsrcinfo`.
- Initial architecture is `x86_64` only.
- Do not invent a pacman dependency named `omarchy`; audit actual runtime/package dependencies and document Omarchy 4+ as an application requirement when no truthful package dependency represents it.
- Package installation must not create/modify user profiles, home-directory state, systemd units, hooks, or default configuration.
- Normal local builds report `dev`; release/package builds inject the release version using `-ldflags -X`.
- The tag workflow prepares a draft GitHub Release only. A human publishes the draft.
- Final AUR validation starts only from a published GitHub Release asset.
- AUR credentials are available only in the protected `aur-release` environment after validation and required approval.
- No `pull_request_target`; no AUR credentials on PR/push/package-validation jobs.
- Use a dedicated AUR SSH key. Never reuse a general maintainer identity and never disable host verification.
- The protected environment supplies a manually verified `AUR_KNOWN_HOSTS` trust value (public data) plus secret `AUR_SSH_PRIVATE_KEY`. Do not obtain trust solely by `ssh-keyscan` in the credential-bearing job.
- Never force-push AUR history.
- Published release assets are immutable by policy. Application fixes require a new version; packaging-only fixes use a higher `pkgrel` against the same source asset.
- Credential-bearing GitHub Actions must be pinned to full commit SHAs where practical, with a comment naming the semantic action version for maintainability.
- Keep substantial release/rendering logic in repository scripts rather than opaque marketplace actions.
- Do not modify Capture, Restore, compatibility, Reconstruction Assurance, or provider semantics in this tranche.

## Review Focus

1. **`--version` on an unconfigured/non-Omarchy host:** it must return build identity without profile loading, TUI startup, Omarchy detection, or mutation. Task 1 pins this.
2. **Same tag/ref archived twice:** generated source archives and SHA-256 must be byte-identical; malformed or divergent release tags must be rejected. Task 2 pins this.
3. **Rendered PKGBUILD input injection or stale metadata:** invalid version/pkgrel/SHA/URL/maintainer values must fail, the release source checksum must never become `SKIP`, and `.SRCINFO` must be generated from the rendered PKGBUILD. Task 3 pins this.
4. **Published upstream release with failed/retried AUR publication:** final validation must use the existing immutable release asset, and a retry must no-op on identical AUR content but fail on same-version different content. Task 6 pins this.
5. **Credential boundary regression:** PR/push/tag verification jobs must not reference AUR secrets; only the protected publish job may read the private key, and SSH host checking must remain enabled. Task 6 pins this structurally and Task 7 documents the operational check.

---

## File Structure and Ownership Map

The implementation should converge on these files and responsibilities:

- `docs/adr/0025-human-gated-tagged-releases-and-protected-aur-publication.md` — durable architectural decision.
- `docs/planning/specs/2026-09-29-release-aur-v1-design.md` — approved design.
- `docs/planning/plans/2026-09-29-release-aur-v1-implementation-plan.md` — this execution contract.
- `internal/buildinfo/buildinfo.go` — mutable-at-link-time application version with `dev` default.
- `internal/app/version_test.go` — focused CLI version behavior and no-runtime-initialization tests.
- `internal/app/app.go` — wire Cobra root `Version` to `buildinfo.Version`; no other CLI behavior change.
- `scripts/release/verify-release-tag.sh` — validate strict tag syntax and ancestry on `main`; print normalized version.
- `scripts/release/make-source-archive.sh` — deterministic `git archive` source asset and checksum generation from explicit ref/version.
- `scripts/release/render-aur-package.py` — strict renderer for the concrete PKGBUILD from explicit immutable inputs.
- `scripts/release/aur-content-status.sh` — compare validated publication files to an AUR checkout and classify `new`, `same`, `update`, or conflicting same revision.
- `scripts/release/tests/test_release_tools.py` — offline/unprivileged tests for tag validation, reproducible archives, renderer validation, and AUR content classification.
- `packaging/aur/PKGBUILD.template` — canonical source-built Arch package recipe.
- `packaging/aur/LICENSE` — 0BSD package-source license.
- `.github/workflows/aur-package-validation.yml` — non-secret pre-release package build/install validation on reviewed code.
- `.github/workflows/release.yml` — tag verification, product verification, deterministic assets, draft GitHub Release preparation.
- `.github/workflows/aur-publish.yml` — published-release final package validation, protected AUR push, and manual packaging-only `pkgrel` revision path.
- `docs/releasing.md` — one-time setup, recurring releases, pkgrel revisions, retries, key rotation, and explicit mutation gates.
- `README.md` — user-facing AUR/source installation and version-reporting guidance.
- `docs/README.md` — link to release maintainer documentation where appropriate.

Do not add a second generated copy of `PKGBUILD` to the Blueprint repository. Concrete PKGBUILD and `.SRCINFO` are workflow/worktree outputs; the AUR repository is their publication destination.

## Pull Request Topology

Implement and review sequentially from the latest merged `main`:

1. **PR A — `feat: add release identity and AUR package primitives`**
   - Land approved ADR/spec/plan.
   - Add build version surface, deterministic release tooling, PKGBUILD template, renderer, content comparison, and focused tests.
   - No publication workflow and no credentials.

2. **PR B — `ci: validate Arch packages and prepare draft releases`**
   - Add disposable Arch package validation and tag-triggered draft GitHub Release preparation.
   - Update user release/install documentation needed to understand artifacts.
   - No AUR secret or push capability yet.

3. **PR C — `ci: publish validated releases to AUR`**
   - Add final validation from published release assets, protected AUR publication, packaging-only `pkgrel` dispatch, idempotence checks, and maintainer runbook.
   - Workflow references protected environment configuration but does not create/configure external secrets or publish anything merely by merging.

Do not start the next PR until the previous one is reviewed and merged unless the human explicitly authorizes stacked work.

---

## Execution Preflight

Before Task 1:

- create/use an isolated worktree from the latest merged `main`;
- verify ADR number 0025 is still free; if another parallel tranche has taken it, stop and renumber the ADR consistently before copying documents;
- copy the approved ADR/spec/plan into their repository paths unchanged;
- run the clean baseline commands from Global Constraints;
- commit only the approved planning documents:

```bash
git add docs/adr/0025-human-gated-tagged-releases-and-protected-aur-publication.md \
        docs/planning/specs/2026-09-29-release-aur-v1-design.md \
        docs/planning/plans/2026-09-29-release-aur-v1-implementation-plan.md
git commit -m "docs: define release and AUR publication v1"
```

No GitHub mutation is authorized by this preflight.

---

### Task 1: Add immutable-at-runtime build identity and `--version`

**PR:** A

**Files:**
- Create: `internal/buildinfo/buildinfo.go`
- Create: `internal/app/version_test.go`
- Modify: `internal/app/app.go`

**Interfaces:**
- Produces: `buildinfo.Version string`, default value exactly `"dev"`, replaceable with `go build -ldflags "-X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=<version>"`.
- Produces: Cobra root version output reachable through `omarchy-blueprint --version` without invoking normal command runtime behavior.
- Later tasks rely on the linker variable path exactly as written above.

- [ ] **Step 1: Write the failing development-version test**

Add `TestVersionFlagReportsBuildVersionWithoutRuntimeInitialization` in `internal/app/version_test.go`.

The test temporarily sets `buildinfo.Version = "0.1.0"`, invokes `Execute(..., []string{"--version"}, deps)` with dependencies whose runtime/TUI callbacks panic if called, and asserts:

```text
exit code == 0
stdout == "omarchy-blueprint version 0.1.0\n"
stderr == ""
```

Restore the package global with `t.Cleanup`.

- [ ] **Step 2: Run the focused test and confirm RED**

Run:

```bash
go test ./internal/app -run TestVersionFlagReportsBuildVersionWithoutRuntimeInitialization -count=1
```

Expected: FAIL because `internal/buildinfo`/root `Version` does not exist yet.

- [ ] **Step 3: Add `internal/buildinfo/buildinfo.go`**

Expose exactly:

```go
package buildinfo

var Version = "dev"
```

No generated file and no runtime lookup.

- [ ] **Step 4: Wire the Cobra root version**

In `internal/app/app.go`, import `internal/buildinfo` and set the root command's `Version: buildinfo.Version` without adding a custom subcommand or runtime preflight.

Do not change root command behavior beyond the standard Cobra version surface.

- [ ] **Step 5: Run the focused test and wider Go verification**

Run:

```bash
go test ./internal/app -run TestVersionFlagReportsBuildVersionWithoutRuntimeInitialization -count=1
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: all PASS.

- [ ] **Step 6: Verify linker injection end-to-end**

Run:

```bash
go build -trimpath -ldflags "-X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=0.1.0" -o /tmp/omarchy-blueprint-version ./cmd/omarchy-blueprint
/tmp/omarchy-blueprint-version --version
```

Expected exact output:

```text
omarchy-blueprint version 0.1.0
```

- [ ] **Step 7: Commit**

```bash
git add internal/buildinfo/buildinfo.go internal/app/app.go internal/app/version_test.go
git commit -m "feat: expose build version"
```

---

### Task 2: Add release-tag verification and deterministic source artifacts

**PR:** A

**Files:**
- Create: `scripts/release/verify-release-tag.sh`
- Create: `scripts/release/make-source-archive.sh`
- Create: `scripts/release/tests/test_release_tools.py`

**Interfaces:**
- `verify-release-tag.sh <tag> <main-ref>` validates strict core SemVer `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$` (no prerelease/build metadata), verifies the peeled tag commit is an ancestor of `<main-ref>`, and prints only the normalized version without `v` on stdout.
- `make-source-archive.sh <git-ref> <version> <output-dir>` validates `<version>` as `MAJOR.MINOR.PATCH`, writes `<output-dir>/omarchy-blueprint-<version>.tar.gz`, and writes `<output-dir>/SHA256SUMS` containing one sha256sum line for that archive.
- The archive top-level directory is exactly `omarchy-blueprint-<version>/`.

- [ ] **Step 1: Add failing offline tests for release tags**

In `scripts/release/tests/test_release_tools.py`, use `unittest` + temporary Git repositories to add:

- `test_verify_release_tag_accepts_main_ancestor_and_prints_version`
- `test_verify_release_tag_rejects_malformed_tag`
- `test_verify_release_tag_rejects_tag_not_contained_in_main`

Use fixed Git author/committer dates in the temporary repos so fixtures are deterministic.

- [ ] **Step 2: Add failing reproducibility tests**

Add:

- `test_source_archive_is_reproducible_for_same_ref`
- `test_source_archive_has_versioned_top_level_directory`
- `test_source_archive_rejects_invalid_version`

The reproducibility test calls the script twice into different output directories and asserts the archive SHA-256 values and bytes are identical.

- [ ] **Step 3: Run and confirm RED**

Run:

```bash
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
```

Expected: FAIL because the scripts do not exist.

- [ ] **Step 4: Implement `verify-release-tag.sh`**

Use `set -euo pipefail`, strict argument count, the exact strict-core-SemVer tag regex above, `git rev-parse "$tag^{commit}"`, and `git merge-base --is-ancestor <tag-commit> <main-ref>`.

Reject lightweight/annotated ambiguity by always peeling to a commit. Print only the normalized version on success; diagnostics go to stderr.

- [ ] **Step 5: Implement `make-source-archive.sh`**

Use the explicit ref and explicit normalized version; do not derive version from branch state.

Generate the tar stream with `git archive --format=tar --prefix="omarchy-blueprint-${version}/" "$ref"`, compress with deterministic gzip metadata (`gzip -n`), then compute SHA-256 from the final bytes.

Refuse to overwrite an existing archive or `SHA256SUMS` with different content. Identical reruns may succeed idempotently.

- [ ] **Step 6: Run focused release-tool tests**

Run:

```bash
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
bash -n scripts/release/verify-release-tag.sh scripts/release/make-source-archive.sh
```

Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add scripts/release
git commit -m "feat: add deterministic release artifacts"
```

---

### Task 3: Add the canonical AUR package template and strict renderer

**PR:** A

**Files:**
- Create: `packaging/aur/PKGBUILD.template`
- Create: `packaging/aur/LICENSE`
- Create: `scripts/release/render-aur-package.py`
- Modify: `scripts/release/tests/test_release_tools.py`

**Interfaces:**
- Renderer CLI:

```text
render-aur-package.py \
  --version <MAJOR.MINOR.PATCH> \
  --pkgrel <positive-int> \
  --source-url <https-url> \
  --sha256 <64-lowercase-hex> \
  --maintainer-name <non-empty> \
  --maintainer-email <email-like-value> \
  --output-dir <dir>
```

- Produces exactly `<output-dir>/PKGBUILD` and copies `packaging/aur/LICENSE` to `<output-dir>/LICENSE`.
- `.SRCINFO` is deliberately not produced by the renderer; Arch tooling generates it from the concrete PKGBUILD.

- [ ] **Step 1: Add failing renderer tests**

Add tests named:

- `test_render_aur_package_pins_release_identity`
- `test_render_aur_package_rejects_bad_version_pkgrel_url_or_sha`
- `test_render_aur_package_never_uses_skip_checksum`
- `test_render_aur_package_injects_linker_version`
- `test_render_aur_package_keeps_omarchy_as_documented_runtime_not_invented_dependency`

Pin expected metadata:

```text
pkgname=omarchy-blueprint
pkgver=0.1.0
pkgrel=1
arch=('x86_64')
license=('MIT')
makedepends=('go')
checkdepends=('git')
```

and exact source asset basename `omarchy-blueprint-0.1.0.tar.gz`.

- [ ] **Step 2: Run renderer tests and confirm RED**

Run:

```bash
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
```

Expected: FAIL because renderer/template are missing.

- [ ] **Step 3: Add the 0BSD packaging-source license**

`packaging/aur/LICENSE` contains the standard 0BSD license text and applies to the package-source repository files, not the upstream MIT program.

- [ ] **Step 4: Add `PKGBUILD.template`**

Template requirements:

- package metadata from Global Constraints;
- `source=("omarchy-blueprint-${pkgver}.tar.gz::<rendered HTTPS URL>")`;
- pinned `sha256sums` value, never `SKIP`;
- `prepare()` sets `GOPATH="${srcdir}"` and downloads modules with `go mod download -modcacherw` without modifying `go.mod`/`go.sum`;
- `build()` exports Arch `CGO_CPPFLAGS`, `CGO_CFLAGS`, `CGO_CXXFLAGS`, `CGO_LDFLAGS`, `GOPATH`, and `GOFLAGS="-buildmode=pie -trimpath -mod=readonly -modcacherw"`;
- `go build` targets `./cmd/omarchy-blueprint`, writes a local build output, and uses `-ldflags "-linkmode=external -X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=${pkgver}"`;
- `check()` exports the same `GOPATH`/read-only module flags needed by the build and runs `go test ./...`;
- declare `checkdepends=('git')` because the current upstream test suite invokes the Git executable (for example `internal/gittest`);
- `package()` installs only the binary under `/usr/bin` and upstream `LICENSE` under `/usr/share/licenses/omarchy-blueprint/LICENSE`.

Do not install profile/config/state files.

- [ ] **Step 5: Implement the strict Python renderer**

Use only Python standard library. Do not invoke a shell to substitute template values.

Validate all dynamic values before replacement. Require HTTPS for `source-url`, positive integer `pkgrel`, strict normalized SemVer for version, lowercase 64-character SHA-256, and reject maintainer values containing newlines/control characters.

Refuse to leave unresolved template markers.

- [ ] **Step 6: Run renderer/release tests**

Run:

```bash
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
```

Expected: all PASS.

- [ ] **Step 7: Audit runtime dependency metadata before freezing the template**

Search current production command execution for external runtime tools and document the result in the PR description/review notes. Use `namcap` later as supporting evidence.

Do not add an `omarchy` dependency unless a real package can truthfully provide/version that contract. Any newly added `depends`/`optdepends` must name a real Arch package and be justified by current runtime use.

- [ ] **Step 8: Commit**

```bash
git add packaging/aur scripts/release/render-aur-package.py scripts/release/tests/test_release_tools.py
git commit -m "feat: add AUR package recipe"
```

---

### Task 4: Add deterministic AUR content classification for retry safety

**PR:** A

**Files:**
- Create: `scripts/release/aur-content-status.sh`
- Modify: `scripts/release/tests/test_release_tools.py`

**Interfaces:**
- `aur-content-status.sh <validated-publication-dir> <aur-checkout-dir> <pkgver> <pkgrel>` prints exactly one status: `new`, `same`, or `update`.
- Exit non-zero with a stable diagnostic when the AUR checkout already claims the same `pkgver-pkgrel` but publication files differ.
- Comparison scope is exactly `PKGBUILD`, `.SRCINFO`, and `LICENSE`.

- [ ] **Step 1: Add failing classification tests**

Add:

- `test_aur_content_status_new_for_empty_checkout`
- `test_aur_content_status_same_for_identical_publication`
- `test_aur_content_status_update_for_older_revision`
- `test_aur_content_status_rejects_same_revision_different_content`
- `test_aur_content_status_rejects_unexpected_publication_files`

- [ ] **Step 2: Run and confirm RED**

Run the release-tool unittest suite; expected FAIL because classifier is missing.

- [ ] **Step 3: Implement `aur-content-status.sh`**

Read `pkgver`/`pkgrel` from the AUR checkout's `.SRCINFO` when present rather than parsing arbitrary PKGBUILD shell.

Require validated-publication-dir to contain exactly the expected publication files. Classify an empty checkout as `new`, byte-identical expected files as `same`, and an older/different revision as `update`; reject same revision with different bytes.

- [ ] **Step 4: Run release-tool tests and shell syntax checks**

```bash
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
bash -n scripts/release/*.sh
```

Expected: all PASS.

- [ ] **Step 5: Run PR A full verification and commit**

```bash
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
git status --short
```

Then:

```bash
git add scripts/release/aur-content-status.sh scripts/release/tests/test_release_tools.py
git commit -m "feat: make AUR publication retry-safe"
```

PR A is ready for review after this task. Do not add workflows before PR A is merged.

---

### Task 5: Add disposable Arch package validation on reviewed code

**PR:** B

**Files:**
- Create: `.github/workflows/aur-package-validation.yml`
- Modify: `README.md`

**Interfaces:**
- Workflow produces no external publication and uses no release/AUR secret.
- It validates current reviewed code as synthetic version `0.0.0` from a locally generated source archive.
- It renders the same PKGBUILD template used for releases and generates `.SRCINFO` with `makepkg --printsrcinfo`.

- [ ] **Step 1: Add a local workflow-contract check before YAML**

Extend `scripts/release/tests/test_release_tools.py` with `test_packaging_template_can_render_pr_validation_version`, rendering version `0.0.0`, pkgrel `1`, and an `https://example.invalid/...` URL with the local archive checksum. This pins that pre-release validation does not require a public release.

Run and confirm PASS before workflow work begins.

- [ ] **Step 2: Create `aur-package-validation.yml`**

Trigger on `pull_request` and `push` to `main`. Set `permissions: contents: read`.

Use a fresh `archlinux:base-devel` job container. The job may run package-manager setup as container root, but `makepkg` itself must run as an unprivileged `builder` user.

Install only the validation tools needed from Arch repositories (`go`, `git`, `namcap`, and SSH tooling if the container lacks it). `git` is also a declared package `checkdepends` because the upstream Go test suite executes Git. Chown only the checked-out workspace/build paths needed by `builder`.

- [ ] **Step 3: Build the synthetic local release source**

From checkout `HEAD`, call:

```text
make-source-archive.sh HEAD 0.0.0 <dist-dir>
```

Render a PKGBUILD for version `0.0.0`, pkgrel `1`, with a syntactically valid dummy HTTPS release URL and the local archive checksum. Copy the archive beside the concrete PKGBUILD under the exact source filename so `makepkg` uses the already-present local source rather than networking to the dummy URL.

Generate `.SRCINFO` from that PKGBUILD.

- [ ] **Step 4: Build/check as non-root and run package diagnostics**

As `builder`, run `makepkg` with clean build outputs and checks enabled. Then run `namcap` on the concrete PKGBUILD and resulting `.pkg.tar.zst`.

Treat `namcap` errors as failures. Warnings must remain visible in the workflow log and be reviewed; do not blanket-ignore all output.

- [ ] **Step 5: Inspect and install the built package**

Assert the package file list contains `/usr/bin/omarchy-blueprint` and `/usr/share/licenses/omarchy-blueprint/LICENSE`, contains no `/usr/local`, and contains no user-home/profile state.

Install the exact built package with pacman in the disposable container and assert:

```text
omarchy-blueprint --version → omarchy-blueprint version 0.0.0
pacman -Qo /usr/bin/omarchy-blueprint → owned by omarchy-blueprint
```

This package test does not run Capture/Restore.

- [ ] **Step 6: Document the hosted-runner clean-build limitation**

Add a short maintainer note in `README.md` or the later release doc: CI uses a fresh official Arch container as the practical GitHub-hosted clean packaging environment; it is disposable and unprivileged for `makepkg`, but it is not Arch `devtools`' full systemd-nspawn clean-chroot path. Do not overstate the guarantee.

- [ ] **Step 7: Verify PR B package workflow without publication**

Run all local script/Go checks, validate the workflow syntax through repository review tooling available to the worker, and push only after human authorization if GitHub mutation is requested. Observe the workflow on a normal PR/push; no secret setup is needed.

- [ ] **Step 8: Commit**

```bash
git add .github/workflows/aur-package-validation.yml README.md scripts/release/tests/test_release_tools.py
git commit -m "ci: validate the Arch package"
```

---

### Task 6: Add tag verification and draft GitHub Release preparation

**PR:** B

**Files:**
- Create: `.github/workflows/release.yml`
- Modify: `README.md`

**Interfaces:**
- Trigger: push tag matching `v*`.
- Successful workflow state is a **draft** GitHub Release with `omarchy-blueprint-<version>.tar.gz` and `SHA256SUMS` assets.
- The workflow never publishes the draft and never sees AUR credentials.

- [ ] **Step 1: Define least privilege and immutable action pins**

Use separate job permissions where practical. Verification jobs use `contents: read`; the draft-release mutation job uses only `contents: write` required by GitHub Release APIs.

Resolve every GitHub Action used in this credential-bearing workflow to a full commit SHA at implementation time and annotate the semantic version in a comment.

- [ ] **Step 2: Verify tag authority**

Checkout with complete history, fetch `origin/main`, and call:

```text
verify-release-tag.sh "$GITHUB_REF_NAME" origin/main
```

Capture the normalized version as a job output. Reject malformed/divergent tags before any write-capable job.

- [ ] **Step 3: Rerun product verification on the exact tag**

Run:

```bash
go mod download
go test ./...
go vet ./...
go build -trimpath -ldflags "-X github.com/Grenco/omarchy-blueprint/internal/buildinfo.Version=${VERSION}" ./cmd/omarchy-blueprint
```

Run the release-tool unittest suite too.

- [ ] **Step 4: Produce and independently verify release assets**

Generate assets with `make-source-archive.sh "$GITHUB_REF_NAME" "$VERSION" dist`.

Run `sha256sum -c dist/SHA256SUMS` and verify the archive top-level directory before handing the artifact to the write-capable job.

- [ ] **Step 5: Prepare the draft GitHub Release idempotently**

Using `gh` and the workflow token:

- if no release exists for the tag, create a draft release (generated notes are allowed as an editing starting point) and upload the two assets;
- if a draft already exists, download/compare its assets and succeed only if bytes match; do not silently replace mismatched bytes;
- if a published release already exists, do not overwrite its assets.

The workflow summary must say that a maintainer must review/edit notes and publish the draft manually.

- [ ] **Step 6: Update user-facing installation/release wording**

README may mention that tagged releases exist and AUR publication follows the published upstream release, but do not claim the AUR package is live until first publication actually occurs. Keep build-from-source instructions valid before launch.

- [ ] **Step 7: Full PR B verification and commit**

Run Go/script checks locally. Review workflow permissions and confirm no `AUR_` secret reference exists anywhere in `release.yml` or `aur-package-validation.yml`.

Commit:

```bash
git add .github/workflows/release.yml README.md
git commit -m "ci: prepare verified draft releases"
```

PR B is ready for review after this task. Merely merging PR B must not create a tag or release.

---

### Task 7: Add final published-release validation and protected AUR publication

**PR:** C

**Files:**
- Create: `.github/workflows/aur-publish.yml`
- Modify: `scripts/release/tests/test_release_tools.py` if workflow-input helpers need focused tests

**Interfaces:**
- Trigger A: GitHub `release` event, type `published`; requires tag `vMAJOR.MINOR.PATCH`, uses `pkgrel=1`.
- Trigger B: `workflow_dispatch` with existing `version` (`MAJOR.MINOR.PATCH`) and explicit integer `pkgrel >= 2`; this path updates only AUR packaging and never modifies the GitHub Release.
- For published-release `pkgrel=1`, validation checks out `refs/tags/v<version>` so the renderer/template are exactly the packaging code reviewed in that release commit, even if `main` advanced while the draft waited for publication.
- For manual packaging-only `pkgrel>=2`, validation checks out current `main`, because that path exists specifically to publish a newly reviewed packaging correction against the old immutable upstream source.
- Validation job receives no AUR private key.
- Publish job uses `environment: aur-release` and reads:
  - secret `AUR_SSH_PRIVATE_KEY`;
  - protected/public environment variable `AUR_KNOWN_HOSTS` containing a manually verified known-hosts entry;
  - repository/environment variables `AUR_MAINTAINER_NAME` and `AUR_MAINTAINER_EMAIL`.

- [ ] **Step 1: Validate event/input semantics before package work**

For a published release, normalize tag to version, require `pkgrel=1`, and checkout `refs/tags/v<version>` for all renderer/template/release-script inputs.

For manual dispatch, require strict normalized version and `pkgrel >= 2`, verify the corresponding published `v<version>` GitHub Release exists, and checkout current `main` for the reviewed packaging correction.

Reject prerelease/draft GitHub Releases for automatic AUR publication in v1.

- [ ] **Step 2: Download and verify immutable upstream assets without AUR secrets**

Download `omarchy-blueprint-<version>.tar.gz` and `SHA256SUMS` from the published GitHub Release. Verify the checksum and refuse missing/duplicate/mismatched entries.

Construct the exact public HTTPS asset URL and render the final concrete PKGBUILD using the release checksum and configured maintainer identity.

Generate `.SRCINFO` with `makepkg --printsrcinfo`; copy the 0BSD package-source `LICENSE`.

- [ ] **Step 3: Run the same final Arch package validation**

In a disposable Arch environment, build/check the final concrete PKGBUILD from its public release URL, run `namcap`, inspect/install the package, verify pacman ownership, and require exact release version output.

Unlike Task 5, do not seed a local source archive to bypass the public URL: this gate proves the exact AUR source URL is retrievable and matches the published checksum.

- [ ] **Step 4: Produce a minimal validated publication artifact**

Upload only:

```text
PKGBUILD
.SRCINFO
LICENSE
PUBLICATION_SHA256SUMS
```

`PUBLICATION_SHA256SUMS` covers the first three files. No SSH configuration/private material is present.

The publish job must verify this checksum manifest before network mutation and must not rerun repository rendering/build scripts after the private key is exposed.

- [ ] **Step 5: Add the protected `aur-release` publish job**

Set `environment: aur-release`; this is the human approval boundary.

After approval:

- create an ephemeral `~/.ssh` with mode 0700;
- write the private key with mode 0600;
- write `AUR_KNOWN_HOSTS` to `known_hosts` and keep strict host checking enabled;
- configure Git author from the declared AUR maintainer variables;
- clone `ssh://aur@aur.archlinux.org/omarchy-blueprint.git` with local default branch `master` (an empty clone is valid for first publication);
- verify no unexpected non-publication files are introduced;
- call `aur-content-status.sh` before mutation.

Never print secret material.

- [ ] **Step 6: Implement publication behavior by status**

- `same`: emit a successful no-op summary and do not commit/push.
- `new` or `update`: replace only `PKGBUILD`, `.SRCINFO`, `LICENSE`; stage only those files; require the staged diff contains no other path; commit message `omarchy-blueprint <version>-<pkgrel>`; push `master` without force.
- conflicting same revision: fail before commit/push.

For first publication, the human runbook owns the separate package-name/ownership check; automation must not rename the package if the push is rejected.

- [ ] **Step 7: Pin credential-bearing workflow dependencies**

Any action in the publish job (including artifact download) is pinned to full commit SHA with semantic-version comment. Prefer built-in shell/git/ssh after artifact download; do not introduce an AUR-specific marketplace publishing action.

- [ ] **Step 8: Add structural security assertions to review**

Before commit, search workflow files and prove:

```text
AUR_SSH_PRIVATE_KEY appears only in aur-publish.yml publish job
pull_request_target appears nowhere
StrictHostKeyChecking=no appears nowhere
force push flags appear nowhere in AUR publication
```

Also inspect workflow job dependencies so the protected publish job cannot start unless final validation succeeds.

- [ ] **Step 9: Commit**

```bash
git add .github/workflows/aur-publish.yml scripts/release/tests/test_release_tools.py
git commit -m "ci: publish validated releases to AUR"
```

---

### Task 8: Add release/AUR operator documentation and first-publication checklist

**PR:** C

**Files:**
- Create: `docs/releasing.md`
- Modify: `README.md`
- Modify: `docs/README.md`

**Interfaces:**
- User docs distinguish AUR install, source build, Omarchy 4+ support, and `--version` issue-reporting identity.
- Maintainer docs distinguish code merge from external publication authority.

- [ ] **Step 1: Write the one-time AUR setup section**

Document human-only setup:

1. confirm `omarchy-blueprint` package-base availability/ownership;
2. create/use an AUR account;
3. create a dedicated CI Ed25519 key (not a general identity) and register its public key;
4. manually verify `aur.archlinux.org` host key and configure protected `AUR_KNOWN_HOSTS`;
5. create GitHub Environment `aur-release` with required reviewers;
6. configure `AUR_SSH_PRIVATE_KEY`, `AUR_MAINTAINER_NAME`, `AUR_MAINTAINER_EMAIL`, and the known-hosts value in the appropriate protected configuration;
7. revoke/rotate the CI key procedure.

Explicitly mark these as external mutations that require human authorization.

- [ ] **Step 2: Write the normal upstream release procedure**

Document:

```text
merge/review main
→ choose version
→ create/push vX.Y.Z tag (human authorization)
→ release workflow prepares draft
→ inspect checksum/assets/notes
→ publish draft GitHub Release (human authorization)
→ final AUR validation
→ approve aur-release environment (human authorization)
→ verify AUR package page/install
```

State that first intended release is `v0.1.0`, not a hard-coded workflow special case.

- [ ] **Step 3: Write failure/retry and `pkgrel` procedures**

Cover separately:

- rerunning a failed AUR job for unchanged `<pkgver>-<pkgrel>`;
- packaging-only change merged to main then manual dispatch with explicit higher `pkgrel`;
- application/source fix requiring a new upstream version;
- prohibition on replacing published release assets;
- what to do if AUR has same revision with different content (stop/investigate).

- [ ] **Step 4: Update README installation/support guidance**

Before first real publication, word AUR instructions conditionally or as the intended release channel; after the human first-publication step, the release PR/operator may change wording to direct `yay -S omarchy-blueprint` / equivalent AUR helper usage.

Always include a helper-independent package identity and source-build path, Omarchy 4+ requirement, and `omarchy-blueprint --version` for bug reports.

Do not describe AUR helpers as pacman itself.

- [ ] **Step 5: Link maintainer documentation**

Add `docs/releasing.md` to `docs/README.md` in the appropriate maintainer/development area.

- [ ] **Step 6: Run final tranche verification**

Run:

```bash
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
python3 -m unittest discover -s scripts/release/tests -p 'test_*.py' -v
bash -n scripts/release/*.sh
git diff --check
git status --short
```

Review all three workflow files for least privilege and secret references.

- [ ] **Step 7: Commit**

```bash
git add docs/releasing.md README.md docs/README.md
git commit -m "docs: add release and AUR runbook"
```

PR C is ready for full review after this task.

---

## Post-Implementation Verification Before Any First Release

After PR C is merged, stop. Infrastructure being merged does **not** authorize publication.

Before `v0.1.0`, a human should verify:

- all required normal CI and package-validation checks are green on chosen `main`;
- the release/AUR docs match the merged workflows;
- the AUR package base is available/controlled;
- dedicated key/environment/reviewer configuration is complete;
- maintainer identity variables are correct and intentionally public;
- the known-hosts value was manually verified through a trusted channel;
- no unresolved known high-severity security issue blocks public release;
- the first rendered package has been manually reviewed.

Only then may a human explicitly authorize the actual tag/release/AUR publication sequence.

## First Public Release Acceptance Evidence

When `v0.1.0` is eventually authorized and executed, retain evidence that:

1. the tag commit is on `main`;
2. product tests passed on the exact tag;
3. the GitHub Release source archive checksum matches `SHA256SUMS`;
4. the release was human-published from a reviewed draft;
5. the final PKGBUILD points to that public asset and exact checksum;
6. final Arch package validation passed from the public URL;
7. protected AUR approval occurred after validation;
8. AUR contains only the expected publication files for `0.1.0-1`;
9. a fresh AUR install produces an owned `/usr/bin/omarchy-blueprint` whose `--version` reports `0.1.0`.

These are operational acceptance records, not a reason to make AUR publication part of Reconstruction Assurance.

## Plan Self-Review Notes

- **Spec coverage:** every release-state transition, package boundary, secret boundary, retry path, `pkgrel` path, docs requirement, and non-goal in the approved spec maps to a task above.
- **Type/interface consistency:** all build-version injection uses the same `internal/buildinfo.Version` linker path; all package rendering uses normalized version without `v`; source archive filenames and top-level directory names are consistent across tasks.
- **Review Focus:** the five highest-risk edge classes each have an explicit test/structural check in Tasks 1, 2, 3, and 7.
- **Scope:** the plan deliberately stops after infrastructure and documentation. It does not create secrets, tags, releases, AUR repos, or the later Omarchy package-repository submission.
