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

Guest commands run with the graphical session's environment
(`scenario/guest-env.sh`), the same for source customization and Blueprint.
`omarchy pkg add` calls `sudo` itself; during source customization only, a
throwaway `SUDO_ASKPASS` helper under `/tmp` answers it for the fixture account.

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
  changing profile bytes, persists Safe + Additive Restore defaults, and passes
  `check`.

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
