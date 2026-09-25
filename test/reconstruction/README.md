# Real Omarchy reconstruction harness

This harness runs against an official installed Omarchy guest under QEMU/KVM. PR 1
validates a pristine base and separate sequential guest overlays; later PRs add
Capture, profile handoff, Restore and independent verification. Run `run.sh` from
the repository checkout on a KVM-capable Ubuntu host with QEMU, OVMF, genisoimage,
Python 3 and OpenSSH. The workflow sets up those tools on `ubuntu-24.04`.

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
