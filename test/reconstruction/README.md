# Real Omarchy reconstruction harness

This harness runs against an official installed Omarchy guest under QEMU/KVM.
It installs one pristine base, then runs Machine A (`source`) and Machine B
(`target`) as sequential overlays. Machine A is customized with native Omarchy
commands, captured through the public CLI with a normal reviewed Capture, and
destroyed; Machine B boots fresh and receives only the profile archive. PR 3
adds Restore and independent verification. Run `run.sh` from the repository
checkout on a KVM-capable Ubuntu host with QEMU, OVMF, genisoimage, OpenSSL,
Python 3, OpenSSH and a Blueprint build at `$RA_BLUEPRINT_BIN` (default
`/tmp/omarchy-blueprint-ra`). The workflow sets up those tools on `ubuntu-24.04`.

## Fixture contract

`fixtures/expected.env` holds the canonical IDs, paths and pinned values. The
Git Resource fixture is rebuilt on the host with fixed metadata so its commit is
always `e161ca52ec7604e6fb335ef15fc212bba03a2994`, and is served to guests as a
dumb-HTTP remote at `https://10.0.2.2:9443/blueprint-ra.git` (host loopback via
QEMU user networking). Blueprint only accepts `https`, `ssh` or scp-like remotes
as portable, so a `git://` daemon would not do. Each guest trusts the per-run
certificate for that URL only, through the system Git config, which is outside
`$HOME` and is not Blueprint state.

The installed base has no pacman sync databases. Machine A refreshes them
(`pacman -Sy`, retried as a network transfer, no upgrade) as source fixture
preparation. Machine B never does: the canonical target keeps the official
fresh-install package-manager state, because reconstructing onto that machine is
the point. Common staging on both guests is limited to harness inputs: the
Blueprint build, the fixture certificate, fixture files and desktop-session
readiness. Never make Restore pass by pre-warming sudo, adding `NOPASSWD`,
running Blueprint as root, or preparing Machine B's packages.

The ISO only enables SDDM autologin for encrypted installs, and this
unattended install is unencrypted, so each guest gets the same
`/etc/sddm.conf.d/autologin.conf` (`Session=omarchy.desktop`) before its identity
reboot. Staging waits until `omarchy-shell shell ping` answers and Omarchy's
first-login provisioning (`omarchy-provision-first-run`, which installs mise
tools) has finished, so it cannot race the scenario.

Guest commands run with the graphical session's environment
(`scenario/guest-env.sh`), the same for source customization and Blueprint.
`omarchy pkg add` needs root and sudo cannot prompt without a terminal, so
source customization runs it through `sudo -S` (its supported root path) with
the fixture password from a throwaway `/tmp` helper removed afterwards.

Phase boundaries:

- `SOURCE_CUSTOMIZATION`: pristine preconditions, native customization, and one
  independent `PASS` line per target in `source/pre-capture.txt`.
- `CAPTURE`: profile, machine overlays, Resources, target mapping and Restore
  skip through the public CLI; a non-mutating `capture --dry-run --json`
  preview; one `capture --review` approved at its real prompt
  (`scenario/approve_capture.py`, never retried); `check`; and assertions on
  the exported archive's profile.
- `PROFILE_HANDOFF`: the archive digest is verified on the host and Machine A
  is powered off and its disk deleted.
- `TARGET_PREFLIGHT`: Machine B boots with a distinct identity, has none of the
  source state, verifies and extracts the archive, binds `target` without
  changing profile bytes, persists Safe + Additive Restore defaults, and runs
  `check` under the known-gap sentinel below.

## Known gap: `packages.fresh-sync-database-readiness`

Blueprint cannot yet `check` a fresh Omarchy install that has no pacman sync
databases: native-package detection (`pacman -Qqen`) fails. Until the
fresh-package readiness prerequisite before PR 3 lands, target preflight pins
that failure narrowly:

- Machine B must still have no `/var/lib/pacman/sync/*.db`, so a target refresh
  cannot hide the sentinel.
- `check` must exit 1 with no successful envelope, and its stderr must carry the
  fingerprint `check packages:` / `detect explicitly installed native
  packages:` / `pacman -Qqen:` / `exit status 1` / `database file for '<repo>'
  does not exist (use '-Sy' to download)`. The repository list is not matched.
  This passes preflight and adds `known gap: packages.fresh-sync-database-readiness`
  to `summary.txt`; stderr is kept in `target/check.stderr`.
- Unexpected success fails `TARGET_PREFLIGHT` ("gap unexpectedly resolved"). The
  fixing product PR must then switch this step back to requiring `check` success.
- Any other failure fails `TARGET_PREFLIGHT`.

The product prerequisite covers both the read path (inspection, `check` and
`status` on a fresh install) and the write path (Restore's `omarchy pkg add`
needs package metadata and elevation).

The disposable `spike` account uses a fixture password for its unattended
configuration and authenticated guest reboot/poweroff; it is passed to real
`sudo` on stdin. No external credentials are required.

Phases (the first failure is retained; later phases remain `NOT RUN`):

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

`$RA_WORK/artifacts/summary.txt` records phase status. Host, base, source and
target diagnostics stay below `artifacts/`; guest disks and SSH private keys
are never uploaded. Cleanup is best-effort and cannot replace the first error.
Infrastructure readiness and transfers may be polled; semantic operations must
never auto-retry.
