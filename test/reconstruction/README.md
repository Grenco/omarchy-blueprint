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

The installed base has no pacman sync databases, and the only supported way to
make it package-ready is Omarchy's own `omarchy update` (ADR 0022). Both
machines therefore become ready the same way (`SOURCE_READINESS`,
`TARGET_READINESS`): `omarchy update -y` over a real terminal, where the
fixture user answers only `sudo`'s own prompt, then a reboot into the updated
system. Machine A does this before customization. Machine B does it only after
target preflight has proven the untouched fresh state and Blueprint's demand
for readiness. Both must reach the same ready Omarchy version, recorded in the
summary; a mismatch fails and is never retried. The harness never runs
`pacman -Sy`, pre-warms sudo, adds `NOPASSWD`, or runs Blueprint as root.
Common staging on both guests is limited to harness inputs: the Blueprint
build, the fixture certificate, fixture files and desktop-session readiness.

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
  changing profile bytes, persists Safe + Additive Restore defaults, and must pass `check` and a Restore
  dry-run with its package state untouched (below).

## Fresh target package state

A fresh Omarchy install has no pacman sync databases. Blueprint handles that
state without changing it (ADR 0022): it detects packages from the local
database only, reports package origin as unavailable, and keeps Capture and
Exact safe. Target preflight therefore requires, on the untouched Machine B:

- no sync database for any configured repository
  (`/var/lib/pacman/sync/<repo>.db` for each repository in
  `pacman-conf --repo-list`), so the harness cannot have prepared package
  state; the listing is kept in `target/pacman-sync-state.txt`;
- `check` succeeds and its JSON `notes` report the unavailable package origin,
  proving Blueprint saw the fresh state;
- `restore --dry-run --json` returns a plan whose only requirement is
  `packages.metadata`, naming `omarchy update` and gating the interactive
  `omarchy pkg add` of the canonical package, and Blueprint plans no metadata
  sync of its own;
- a real `restore packages --yes` refuses with that remediation, leaves the
  package absent, and creates no restore journal
  (`target/restore-refusal.txt`).

`target/pacman-queries.txt` records pacman's own `-Qq`/`-Qqe`/`-Qqen`/`-Qqem`
exit codes and line counts as evidence. Never make this pass by refreshing
Machine B's databases, pre-warming sudo, adding `NOPASSWD`, or running
Blueprint as root.

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
