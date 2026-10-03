# Releasing Omarchy Blueprint

This runbook is for maintainers. It covers upstream releases on GitHub and
the `omarchy-blueprint` package on the Arch User Repository (AUR). The design
is [ADR 0025](adr/0025-human-gated-tagged-releases-and-protected-aur-publication.md).

Merging code never releases anything. Every step that changes GitHub or the
AUR is marked **(human authorization)**: someone with that authority decides
to do it.

## What runs where

| Workflow | Trigger | Does | Credentials |
|---|---|---|---|
| **AUR Package Validation** | pull requests and pushes to `main` | builds the current code as a synthetic `0.0.0` package from a local archive, checks, inspects and installs it | none |
| **Release** | pushing a `vMAJOR.MINOR.PATCH` tag | verifies the tag is on `main`, tests the exact tag, builds `omarchy-blueprint-<version>.tar.gz` and `SHA256SUMS`, renders the release's `pkgrel` 1 `PKGBUILD` for the archive's public URL and validates it from the local archive, prepares a **draft** GitHub Release with all three | `contents: write` for the draft job only |
| **AUR Publish** | publishing a GitHub Release, or manual dispatch | validates the final package from the public release URL (for `pkgrel` 1 it must equal the release's `PKGBUILD`), then, only when `AUR_PUBLISHING_ENABLED` is `true` and after approval, pushes `PKGBUILD`, `.SRCINFO` and `LICENSE` to the AUR | the AUR key, only in the protected `publish` job |

Package validation runs in a fresh official `archlinux:base-devel` container
that is discarded afterwards, with `makepkg` as an unprivileged user. It is
not Arch `devtools`' full systemd-nspawn clean chroot.

A green package validation means the recipe builds, passes its checks,
installs, and reports its version. It does not mean Omarchy behavior was
revalidated; that is Reconstruction Assurance's job.

### What a release contains

| Asset | Purpose |
|---|---|
| `omarchy-blueprint-<version>.tar.gz` | The canonical source: a deterministic archive of the tagged commit. Every package builds from it. |
| `SHA256SUMS` | Its checksum. The `PKGBUILD` pins the same value. |
| `PKGBUILD` | The `pkgrel` 1 AUR recipe for this release, rendered from `packaging/aur/PKGBUILD.template`. Beta testers build it with `makepkg -si` while the package is not on the AUR, and AUR publication of `pkgrel` 1 must push this exact file. |

There is no prebuilt binary: the package is source-built. `.SRCINFO` and the
0BSD package-source `LICENSE` are generated or copied only for the AUR
repository, which needs them; `makepkg` does not.

### The AUR is optional

AUR publication is a separate, later step. Without an AUR account the whole
release still works: the draft is prepared, the GitHub Release is published,
AUR Publish validates the public package, and its `publish` job is skipped
until the repository variable `AUR_PUBLISHING_ENABLED` is `true`. Beta
testers install from the release's `PKGBUILD` (see the README). Once AUR
access exists, publish the same release as described in
[Publishing a release to the AUR later](#publishing-a-release-to-the-aur-later).

## One-time setup (human authorization)

### Before the first release

1. **Add repository variables** `AUR_MAINTAINER_NAME` and
   `AUR_MAINTAINER_EMAIL`. They are public: they appear in the PKGBUILD's
   `# Maintainer:` line, which the Release workflow renders, and as the AUR
   commit author. They need no AUR account. Don't change them while a
   release is still waiting for its first AUR publication: its `pkgrel` 1
   `PKGBUILD` would no longer match.
2. **Check the tag ruleset.** *Immutable release tags* (*Settings → Rules →
   Rulesets*) blocks updating or deleting `refs/tags/v*` tags, so a tag can't
   be moved under a draft awaiting publication. Keep it active with no bypass.
3. **Leave `AUR_PUBLISHING_ENABLED` unset** until the AUR setup below is done.

### Before the first AUR publication

Do these once AUR account registration is available.

1. **Confirm the package name.** Check that
   `https://aur.archlinux.org/packages/omarchy-blueprint` does not exist, or
   is already yours. If someone else owns it, stop: automation never picks
   another name.
2. **Create or use an AUR account** at <https://aur.archlinux.org>.
3. **Create a dedicated CI key.** Never reuse a personal or GitHub key:

   ```sh
   ssh-keygen -t ed25519 -N '' -C 'omarchy-blueprint AUR CI' -f aur-ci
   ```

   Add `aur-ci.pub` to the AUR account under *My Account → SSH Public Key*.
   An account can hold several keys, so this one can be revoked on its own.
4. **Verify the AUR host key** through a trusted channel. Compare the
   fingerprints that `ssh-keyscan aur.archlinux.org | ssh-keygen -lf -`
   shows with the fingerprints published on the AUR home page and ArchWiki,
   over HTTPS. Keep only the verified lines, e.g.
   `aur.archlinux.org ssh-ed25519 AAAA…`. The workflow never scans host keys
   itself, and host checking always stays on.
5. **Create the `aur-release` environment** in *Settings → Environments*:
   - add required reviewers, the people who approve each AUR push;
   - under deployment branches and tags, choose *Selected branches and
     tags* and allow only `main` (packaging-only revisions) and `v*` tags
     (published releases);
   - add the secret `AUR_SSH_PRIVATE_KEY` with the contents of `aur-ci`;
   - add the environment variable `AUR_KNOWN_HOSTS` with the verified
     host-key lines.
6. **Delete the local private key** (`aur-ci`) once it is stored as the
   secret.
7. **Set the repository variable `AUR_PUBLISHING_ENABLED` to `true`.** From
   then on, publishing a GitHub Release leads to the AUR approval gate.

### Rotating or revoking the CI key (human authorization)

1. Generate a new dedicated key and add its public key to the AUR account.
2. Replace `AUR_SSH_PRIVATE_KEY` in the `aur-release` environment.
3. Remove the old public key from the AUR account. That revokes it
   immediately, without affecting any other identity.

If the key may have leaked, remove it from the AUR account first.

## Releasing a new version

Prerequisites: push access to `main` and tags, permission to publish
releases, the [first-release setup](#before-the-first-release) done, and an
Omarchy 4+ x86_64 machine to test the package on.

1. **Merge and check `main`.** The chosen commit is on `main`, and its
   required checks and AUR Package Validation are green. Releases are never
   cut from another branch or a local commit: the Release workflow rejects a
   tag whose commit is not on `main`.
2. **Choose the version.** Use `vMAJOR.MINOR.PATCH`, with no prerelease or
   build suffix (ADR 0025). Pre-1.0 versions are the public beta: the first
   is `v0.1.0`. Bump PATCH for fixes and MINOR for new behavior; nothing in
   the workflows special-cases either. The tag is the only place the version
   is chosen: the archive name, `pkgver` and the binary's `--version` all
   derive from it.
3. **Verify the commit locally** from a clean checkout of it:

   ```sh
   git status --short        # must print nothing
   git switch --detach <commit>
   go test ./...
   go vet ./...
   go build ./cmd/omarchy-blueprint
   python3 -m unittest discover -s scripts/release/tests -p 'test_*.py'
   ```

   Optionally [check the package locally](#checking-a-package-locally) too.
4. **Tag and push (human authorization).** Tag the exact commit and push it:

   ```sh
   git tag -a v0.1.0 -m "v0.1.0" <commit>
   git push origin v0.1.0
   ```

   Tags are immutable once pushed, so check the commit first.
5. **Review the draft.** The Release workflow verifies the tag, tests it,
   builds the source archive and `SHA256SUMS`, renders the `pkgrel` 1
   `PKGBUILD` for the archive's public URL with
   `scripts/release/render-aur-package.py`, validates it with
   `scripts/release/validate-arch-package.sh` (`.SRCINFO` from
   `makepkg --printsrcinfo`, build, `check()`, `namcap`, contents, install,
   `--version`), and prepares a draft with
   `scripts/release/prepare-draft-release.sh`. Check that:
   - the draft has exactly `omarchy-blueprint-<version>.tar.gz`,
     `SHA256SUMS` and `PKGBUILD`;
   - the `PKGBUILD` has `pkgver=<version>`, `pkgrel=1`, the release URL and
     the digest in `SHA256SUMS`;
   - the step summary shows the verified commit;
   - the notes are right. Edit the generated notes as needed, and say it is a
     beta: review Restore plans before applying them, and keep independent
     backups of important personal data.
6. **Test the install from the draft.** The draft's URLs aren't public yet,
   so give `makepkg` the archive beside the `PKGBUILD`; it still checks the
   pinned SHA-256. On an Omarchy machine:

   ```sh
   mkdir omarchy-blueprint-0.1.0 && cd omarchy-blueprint-0.1.0
   gh release download v0.1.0 --repo Grenco/omarchy-blueprint
   sha256sum --check SHA256SUMS
   makepkg -si
   omarchy-blueprint --version          # omarchy-blueprint version 0.1.0
   omarchy-blueprint --help
   pacman -Qo /usr/bin/omarchy-blueprint
   ```

7. **Publish the draft (human authorization).** This is the upstream release.
   Published assets never change. Don't mark it as a prerelease: prereleases
   never reach the AUR, and the README's `releases/latest` link skips them.
8. **Final validation runs automatically.** AUR Publish:
   - downloads the release assets from their public URLs and verifies
     `SHA256SUMS`;
   - rebuilds the source archive from the tag and requires identical
     contents;
   - renders the final PKGBUILD with the tag's recipe and `pkgrel=1`, and
     requires it to equal the release's `PKGBUILD`;
   - builds it from the public URL, then runs checks, `namcap`, the contents
     check, installation and the `--version` check.

   This needs no AUR access. While `AUR_PUBLISHING_ENABLED` is not `true`,
   the run ends here and its `publish` job is skipped.
9. **Check the beta install path.** Follow the README's installation steps,
   which download `releases/latest/download/PKGBUILD`, and check
   `omarchy-blueprint --version` reports the release version.
10. **Approve the `aur-release` deployment (human authorization),** when AUR
    publishing is enabled. Review the validated `PKGBUILD` and `.SRCINFO` in
    the `aur-publication` artifact first. The publish job then pushes
    exactly those three files as `omarchy-blueprint <version>-1`, without
    force-pushing.
11. **Verify the result.** Check the AUR package page. Then, on a clean
    Omarchy system:
    - install it with your AUR helper, e.g. `yay -S omarchy-blueprint`, or
      `git clone` plus `makepkg -si`;
    - check that `omarchy-blueprint --version` reports the release version;
    - check that `pacman -Qo /usr/bin/omarchy-blueprint` names
      `omarchy-blueprint`.

## Publishing a release to the AUR later

For a release published while AUR publishing was off. Only publish the
newest release: the AUR holds one version, and publication refuses to go
backwards.

1. Do the [AUR setup](#before-the-first-aur-publication), including
   `AUR_PUBLISHING_ENABLED=true`.
2. **(Human authorization)** Run *Actions → AUR Publish → Run workflow* on
   `main` with the release's `version` (e.g. `0.1.0`) and `pkgrel` `1`.
3. The workflow renders the recipe from the release tag, not from `main`,
   and requires it to equal the release's `PKGBUILD`, so the AUR gets
   exactly the file testers have been building. It runs the same final
   validation, then waits for `aur-release` approval **(human
   authorization)**.
4. Verify the result as in step 11 above. Testers who installed from the
   release `PKGBUILD` already have `<version>-1`, so their AUR helper sees
   the same package and only updates on the next release or `pkgrel`.

If the comparison fails (the maintainer variables changed, say), publish a
packaging-only revision with `pkgrel` 2 instead.

## Checking a package locally

The workflows are authoritative, but the same scripts run locally. To render
and check the current checkout as a release would, without a tag:

```sh
repo=$PWD dist=$(mktemp -d) pkg=$(mktemp -d)
bash scripts/release/make-source-archive.sh HEAD 0.1.0 "$dist"
python3 scripts/release/render-aur-package.py --version 0.1.0 --pkgrel 1 \
  --source-url https://github.com/Grenco/omarchy-blueprint/releases/download/v0.1.0/omarchy-blueprint-0.1.0.tar.gz \
  --sha256 "$(cut -d' ' -f1 "$dist/SHA256SUMS")" \
  --maintainer-name "Your Name" --maintainer-email you@example.com \
  --output-dir "$pkg"
cp "$dist"/omarchy-blueprint-0.1.0.tar.gz "$pkg"/   # seed the not-yet-public source
cd "$pkg"
makepkg --printsrcinfo > .SRCINFO
makepkg --cleanbuild --check
namcap PKGBUILD omarchy-blueprint-0.1.0-1-x86_64.pkg.tar.zst
./pkg/omarchy-blueprint/usr/bin/omarchy-blueprint --version
```

`makepkg` needs `go` and `git` from pacman (`--syncdeps` installs them). The
full validation, including installing the package, runs as root in a
disposable container, and writes `.SRCINFO` back to `$pkg`:

```sh
docker run --rm -v "$repo":/src:ro -v "$pkg":/package archlinux:base-devel bash -c \
  'pacman -Syu --noconfirm --needed git go namcap &&
   bash /src/scripts/release/validate-arch-package.sh /package 0.1.0 1'
```

## When something goes wrong

### Release workflow fails before the draft exists

Nothing was released. Fix the cause on `main` through a normal PR. Because
the tag can't be moved, release the fix as a new version.

### AUR Publish fails after the GitHub Release is published

The GitHub Release is still valid; don't replace its assets or bump the
version.

- **Infrastructure failure** (network, runner, AUR unavailable): re-run the
  failed workflow run. The same assets, checksum and `pkgver-pkgrel` are
  used. If the AUR already has the identical revision, the publish job
  reports success without committing.
- **Validation failure caused by the recipe:** fix the recipe on `main`, then
  publish a packaging-only revision (below).
- **Validation failure caused by the application:** release a new upstream
  version.

### A bad release was published

Published assets and tags never change, so correct forward:

1. Edit the bad release's notes to say what is wrong and which version
   replaces it. Leave its assets alone.
2. Fix the cause on `main` through a normal PR.
3. Release the fix as the next PATCH version. It becomes the latest release,
   so the README install steps and the AUR pick it up.
4. If the problem is only in the package recipe and the package is on the
   AUR, a packaging-only revision is enough.

Don't delete the release or its tag: testers and AUR history refer to it.

### AUR already has this revision with different content

The publish job stops before committing. Nothing is overwritten and AUR
history is never rewritten. Find out how the AUR copy diverged before doing
anything else. Publish any correction as a new `pkgrel`.

## Packaging-only revisions (`pkgrel` 2 or higher)

Use these when only the AUR recipe needs to change and the upstream source
does not. They need AUR publishing: a release's `PKGBUILD` asset is always
`pkgrel` 1 and never changes. While the package is not on the AUR, fix a
broken recipe with a new upstream PATCH release instead, so testers get the
fix from the release's `PKGBUILD`.

1. Change `packaging/aur/PKGBUILD.template` through a reviewed PR and merge it
   to `main`.
2. **(Human authorization)** Run *Actions → AUR Publish → Run workflow* on
   `main` with the existing `version` (e.g. `0.1.0`) and the next `pkgrel`
   (e.g. `2`).
3. The workflow checks that published release `v<version>` exists. It reuses
   and verifies its immutable source archive, and renders the recipe at the
   exact commit `main` pointed to when you dispatched. Later pushes to `main`
   do not change what that run validates or publishes. It runs the same final
   validation, then waits for `aur-release` approval **(human
   authorization)**. Before approving, check that the run's commit is the
   one you meant.

The GitHub Release is never created or changed by this path. Application
changes always need a new upstream version, which resets `pkgrel` to 1.

## Never

- Replace or re-upload assets of a published release, or hand-edit a
  release's `PKGBUILD`.
- Commit a rendered, version-pinned `PKGBUILD` or `.SRCINFO` to this
  repository: `packaging/aur/PKGBUILD.template` is the only recipe source.
- Move or delete a release tag.
- Force-push the AUR repository or rewrite its history.
- Give the AUR key to any job other than the protected publish job, or use a
  personal key.
- Disable SSH host-key checking.
- Publish a `-git` or `-bin` package, or rename the package to avoid a
  conflict.

## Before proposing inclusion in the Omarchy package repository

That is a separate, later decision (ADR 0025). Gather evidence of:

- releases shipped through this process;
- a maintained AUR package;
- successful installs by users other than the maintainers;
- resolved packaging and runtime issues;
- no unresolved high-severity security issue;
- clear Omarchy 4+ compatibility expectations;
- issue reports that carry exact `--version` output.
