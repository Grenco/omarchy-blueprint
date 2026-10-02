# Reconstruction Assurance harness

This harness runs against an official installed Omarchy guest under QEMU/KVM
(ADR 0021). It installs one pristine base from the pinned ISO, then runs
Machine A (`source`) and Machine B (`target`) as sequential overlays. Both
machines become package-ready through Omarchy's own `omarchy update` and must
reach the same Omarchy version. Machine A is customized with native Omarchy
commands, captured through a normal reviewed Capture, and destroyed. Machine B
proves its fresh state, receives only the profile archive, becomes ready, and is
restored through the normal approved Restore at a real terminal, including the
real `sudo` prompt. It is then graded independently. Run `run.sh` from the
repository checkout on a KVM-capable Ubuntu host with QEMU, OVMF, genisoimage,
OpenSSL, Python 3, OpenSSH and a Blueprint build at `$RA_BLUEPRINT_BIN` (default
`/tmp/omarchy-blueprint-ra`). The `Reconstruction Assurance` workflow sets up
those tools on `ubuntu-24.04`.

## Fixture contract

`fixtures/expected.env` holds the canonical IDs, paths and pinned values. The
Git Resource fixture is rebuilt on the host with fixed metadata so its commit is
always `e161ca52ec7604e6fb335ef15fc212bba03a2994`, and is served to guests as a
dumb-HTTP remote at `https://10.0.2.2:9443/blueprint-ra.git` (host loopback via
QEMU user networking). Blueprint only accepts `https`, `ssh` or scp-like remotes
as portable, so a `git://` daemon would not do. Each guest trusts the per-run
certificate for that URL only, through the system Git config, which is outside
`$HOME` and is not Blueprint state.

The Services fixture owns `blueprint-ra-marker.service`, its timer, and the
exact `10-ra.conf` drop-in (mode `0640`). The timer names the service explicitly
and uses a 365-day interval: no gate assertion waits for a timer tick. Machine A
starts the timer and invokes the harmless oneshot natively once to prove its
drop-in-dependent marker. An unselected custom user service is an ownership
sentinel; neither its metadata nor its bytes may enter the exported profile.

First adoption uses a dedicated `capture services --review` terminal pass before
aggregate Capture. The driver answers exact per-unit Manage prompts, approves
only the two canonical units, declines all other candidates, and refuses missing
or repeated selections. JSON previews must show the new units eligible but
unselected and must not mutate the profile. The exported TOML, definition bytes,
hashes, timer enablement/active evidence, and drop-in mode are checked independently.

This canonical lane gates persistent Services reconstruction and **no implicit
activation**, retaining its existing approved default Restore without a one-run
activation override. Machine B independently checks effective manager fragment
and drop-in paths, exact bytes/mode, parser loadability, persistent timer
enablement, and absence of both an active timer and the marker. Activation
authority/oneshot/entry-point behavior remains covered by the PR D integration
tests; this lane does not claim Blueprint activation evidence or depend on
wall-clock timer firing. Unresolved template/instance Capture remains a separate
carry-forward requirement, so this fixture alone does not mark Services v1 complete.

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
  preview; dedicated first Services adoption followed by one aggregate
  `capture --review` approved at its real prompt
  (`scenario/approve_capture.py`, never retried); `check`; and assertions on
  the exported archive's profile.
- `PROFILE_HANDOFF`: the archive digest is verified on the host and Machine A
  is powered off and its disk deleted.
- `TARGET_PREFLIGHT`: Machine B boots with a distinct identity, has none of the
  source state, verifies and extracts the archive, binds `target` without
  changing profile bytes, persists Safe + Additive Restore defaults, and must
  pass `check` and a Restore dry-run with its package state untouched (below).
- `TARGET_READINESS`: Machine B runs `omarchy update`, must reach Machine A's
  ready Omarchy version, and must still have none of the canonical
  customizations.
- `RESTORE_PLAN`: a completely new plan; the pre-readiness dry-run is never
  approved. The persisted defaults are Safe + Additive, and the plan matches the
  canonical contract. Its only interactive step is the `omarchy pkg add`, which
  carries the administrator-authentication notice.
- `RESTORE_APPROVAL`, `RESTORE_APPLY`, `BLUEPRINT_VERIFY`: the normal `restore`
  runs over a real terminal (`scenario/drive_terminal.py`). The harness sends
  `yes` once at Blueprint's prompt and answers only `sudo`'s own prompt; there
  is no `--yes`, `--json` or one-run override, and nothing is retried.
  Blueprint's journal must end with a successful verification.
- `INDEPENDENT_VERIFY`: native assertions (`verify/verify-target.sh`), then a
  final dry-run with no actionable work beyond the intentional
  `target-only-skip` and the exact timer Persistent-state-only activation notice.

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

## CI console normalization

GitHub-hosted QEMU/OVMF advertises a firmware serial console that causes
systemd-stub to infer a kernel serial console (`console=uart,io,0x3f8`) when a
boot entry has no explicit `console=`. The harness normalizes this CI-only
virtualization artifact in two steps:

1. The base builder (the QEMU process that runs the official ISO install and
   the installed system's first disk boot) has no serial device
   (`-serial none`), so the firmware presents no serial console for
   systemd-stub to infer. The pinned ISO offers no supported way to change the
   installed system's kernel arguments before that first boot: its unattended
   installer never runs archinstall `custom_commands` and regenerates
   `limine.conf` with `limine-update` at the end of the install.
2. After that first boot, the harness adds Omarchy's own
   `limine-entry-tool` drop-in
   (`/etc/limine-entry-tool.d/zz-blueprint-ra-ci-console.conf`,
   `KERNEL_CMDLINE[default]+=" console=tty0"`), runs `limine-update`, and
   freezes the base only if every generated `cmdline:` entry carries
   `console=tty0`. The drop-in survives later regeneration, such as kernel
   upgrades during `omarchy update`.

Machine A and Machine B keep QEMU's serial device and `serial.log`. This is not
Blueprint desired state and not an Omarchy workaround for real machines.

The booted `/proc/cmdline` is checked at every stage, or the run fails
`INFRASTRUCTURE`:

- the base's first disk boot (`base/first-disk-boot-cmdline.txt`) must have no
  serial kernel console (`console=uart…` or `console=ttyS…`); explicit
  `console=tty0` is not expected yet;
- Machine A after its first boot and Machine B after its identity reboot
  (`<role>/proc-cmdline.txt`), and each machine after its `omarchy update`
  reboot (`<role>/proc-cmdline-post-update.txt`), must contain `console=tty0`
  and no serial kernel console.

Because the overlays start from the base builder's NVRAM, the run also
records how much firmware boot-manager output still reaches each machine's
`serial.log`.

Why (PR #47, Reconstruction Boot Soak, fresh-overlay trials): early boots with a
serial kernel console reproduced an idle hang after the initramfs resume message
(14 of 515). Explicit `console=tty0` early boots had no failures in the measured
sample (0 of 200). The exact kernel or userspace mechanism of the hang is not
known. The soak keeps the untouched `installer` image, built with the original
serial device, as the substrate control.

## Boot soak (diagnostic, never a gate)

`soak.sh <cycles> [debug]`, dispatched through the separate **Reconstruction
Boot Soak** workflow, installs the pinned base with the same autologin setup
and reboots one guest repeatedly, alternating warm reboots and cold power
cycles. It classifies each boot as `ok`, `slow` or `hung` and keeps boot
timings, serial tails and screenshots. `debug` rebuilds the boot image with
serial-console kernel and systemd output, so compare it against a non-debug
soak. It runs no Capture or Restore, so it can never produce the
`Reconstruction Assurance` check; `run.sh` has no diagnostic mode.

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

`$RA_WORK/artifacts/summary.txt` records phase status, both ready Omarchy versions
and both guests' Blueprint binary hashes. Host, base, source and
target diagnostics stay below `artifacts/`; guest disks and SSH private keys
are never uploaded. Cleanup is best-effort and cannot replace the first error.
Infrastructure readiness and transfers may be polled; semantic operations must
never auto-retry.
