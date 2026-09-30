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
| **Release** | pushing a `vMAJOR.MINOR.PATCH` tag | verifies the tag is on `main`, tests the exact tag, builds `omarchy-blueprint-<version>.tar.gz` and `SHA256SUMS`, prepares a **draft** GitHub Release | `contents: write` for the draft job only |
| **AUR Publish** | publishing a GitHub Release, or manual dispatch for a `pkgrel` revision | validates the final package from the public release URL, then (after approval) pushes `PKGBUILD`, `.SRCINFO` and `LICENSE` to the AUR | the AUR key, only in the protected `publish` job |

Package validation runs in a fresh official `archlinux:base-devel` container
that is discarded afterwards, with `makepkg` as an unprivileged user. It is
not Arch `devtools`' full systemd-nspawn clean chroot.

A green package validation means the recipe builds, passes its checks,
installs, and reports its version. It does not mean Omarchy behavior was
revalidated; that is Reconstruction Assurance's job.

## One-time setup (human authorization)

Do these once, before the first AUR publication.

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
6. **Add repository variables** `AUR_MAINTAINER_NAME` and
   `AUR_MAINTAINER_EMAIL`. They are public: they appear in the PKGBUILD's
   `# Maintainer:` line and as the AUR commit author.
7. **Delete the local private key** (`aur-ci`) once it is stored as the
   secret.
8. **Check the tag ruleset.** *Immutable release tags* (*Settings → Rules →
   Rulesets*) blocks updating or deleting `refs/tags/v*` tags, so a tag can't
   be moved under a draft awaiting publication. Keep it active with no bypass.

### Rotating or revoking the CI key (human authorization)

1. Generate a new dedicated key and add its public key to the AUR account.
2. Replace `AUR_SSH_PRIVATE_KEY` in the `aur-release` environment.
3. Remove the old public key from the AUR account. That revokes it
   immediately, without affecting any other identity.

If the key may have leaked, remove it from the AUR account first.

## Releasing a new version

1. **Merge and check `main`.** The chosen commit is on `main`, and its
   required checks and AUR Package Validation are green.
2. **Choose the version.** Use `vMAJOR.MINOR.PATCH`, with no prerelease or
   build suffix. The first intended release is `v0.1.0`; nothing in the
   workflows special-cases it.
3. **Tag and push (human authorization).** Tag the exact commit and push it:

   ```sh
   git tag -a v0.1.0 -m "v0.1.0" <commit>
   git push origin v0.1.0
   ```

   Tags are immutable once pushed, so check the commit first.
4. **Review the draft.** The Release workflow verifies the tag, tests it,
   builds the assets and prepares a draft. Check that:
   - the draft has exactly `omarchy-blueprint-<version>.tar.gz` and
     `SHA256SUMS`;
   - the step summary shows the verified commit;
   - the notes are right. Edit the generated notes as needed.
5. **Publish the draft (human authorization).** This is the upstream release.
   Published assets never change.
6. **Final validation runs automatically.** AUR Publish:
   - downloads the release assets from their public URLs and verifies
     `SHA256SUMS`;
   - rebuilds the source archive from the tag and requires identical
     contents;
   - renders the final PKGBUILD with the tag's recipe and `pkgrel=1`;
   - builds it from the public URL, then runs checks, `namcap`, the contents
     check, installation and the `--version` check.
7. **Approve the `aur-release` deployment (human authorization).** Review the
   validated `PKGBUILD` and `.SRCINFO` in the `aur-publication` artifact
   first. The publish job then pushes exactly those three files as
   `omarchy-blueprint <version>-1`, without force-pushing.
8. **Verify the result.** Check the AUR package page. Then, on a clean
   Omarchy system:
   - install it with your AUR helper, e.g. `yay -S omarchy-blueprint`, or
     `git clone` plus `makepkg -si`;
   - check that `omarchy-blueprint --version` reports the release version;
   - check that `pacman -Qo /usr/bin/omarchy-blueprint` names
     `omarchy-blueprint`.

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

### AUR already has this revision with different content

The publish job stops before committing. Nothing is overwritten and AUR
history is never rewritten. Find out how the AUR copy diverged before doing
anything else. Publish any correction as a new `pkgrel`.

## Packaging-only revisions (`pkgrel` 2 or higher)

Use these when only the AUR recipe needs to change and the upstream source
does not.

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

- Replace or re-upload assets of a published release.
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
