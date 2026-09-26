# Reconstruction Assurance v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a required GitHub Actions reconstruction gate that installs a pinned official Omarchy release under KVM, captures representative desired state from one disposable real Omarchy machine, restores only the portable profile onto a fresh independent machine, and independently proves the resulting state.

**Architecture:** Reuse the proven QEMU/KVM and unattended Omarchy-install mechanics from the feasibility spike, but move them into focused scripts under `test/reconstruction/` instead of embedding the test in workflow YAML. Build one pristine Omarchy base per PR, run source and target as sequential qcow2 overlays, hand off only a deterministic profile archive, exercise the normal CLI Restore prompt/recalculation path, and grade Machine B with native system assertions after Blueprint Verify succeeds.

**Tech Stack:** GitHub Actions `ubuntu-24.04`, QEMU/KVM + OVMF + qcow2, Bash, Python 3 standard library, OpenSSH, OpenSSL + Python standard-library HTTPS server for a dumb-HTTP Git fixture remote, Go 1.25+/1.26 existing CI, Omarchy 4.0.4 official ISO, existing `omarchy-blueprint` CLI.

**Spec:** `docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md`

**ADR:** `docs/adr/0021-real-omarchy-reconstruction-assurance.md`

## Global Constraints

- Implementation base for PR 1 is current `main` at `8c709290f6c418cdb2c0bcb91cbde1157e179700`; every later PR starts from the latest merged `main`, not this original SHA.
- The approved ADR and design spec are authoritative. If implementation exposes a contradiction, amend the design explicitly in a reviewed documentation commit before changing behavior.
- Production Reconstruction Assurance uses the pinned official Omarchy 4.0.4 ISO: `https://iso.omarchy.org/omarchy-4.0.4.iso`.
- The pinned SHA-256 is `ddeded2758c48318d201dfdac905ecb28f570441883f0c052ea3cd5d05acf92d`.
- Do not force a kernel in unattended cidata. Allow Omarchy 4.0.4 to choose its normal default kernel/header pair.
- Use real Omarchy/system commands. No fake `omarchy`, mocked pacman/systemd, or container substitute is permitted in the Reconstruction Assurance layer.
- Build the exact Blueprint binary from the pull-request checkout once and copy that same binary to both guests.
- Build one pristine Omarchy disk per PR; source and target are separate writable qcow2 overlays from that immutable base and run sequentially.
- Machine A must be destroyed before Machine B Restore begins.
- Only the portable Blueprint profile archive crosses from source to target. Never copy Blueprint local state, machine bindings, journals, package cache, source home, or source disk state.
- Blueprint logical machine IDs are exactly `source` and `target`.
- Guest hostnames are exactly `blueprint-ra-source` and `blueprint-ra-target`.
- Canonical Restore intent is Safe + Additive, persisted as the `target` machine's Restore defaults through the public CLI and verified through public JSON inspection. Do not pass one-run `--force`, `--exact`, `--conflicts force`, or `--convergence exact` overrides.
- The real Restore invocation must not pass `--yes` or `--json`; the harness waits for `Apply this restore? [y/N] ` and only then sends `yes` on stdin.
- The structured preflight invocation is `restore --dry-run --json`; it is inspection only, never mutation authority.
- An unexpected interactive Restore operation is a hard failure. Do not switch to TUI automation or preapproval.
- Blueprint's changed-after-approval failure is a hard failure and is never automatically retried.
- Retry/poll only idempotent infrastructure readiness/transfer. Never automatically retry Omarchy installation after a meaningful installer failure, source customization, Capture, Restore planning, approval, Restore application, Blueprint verification, or independent verification.
- Canonical source state is fixed for v1:
  - official package: `alacritty`;
  - Omarchy default terminal: `alacritty`;
  - built-in theme: `catppuccin`;
  - Shell: `idle.lock = 600`, `bar.position = "bottom"`;
  - Config file: `~/.config/blueprint-ra/config.toml` with exact bytes `mode = "reconstructed"\n`;
  - Hook: `~/.config/omarchy/hooks/post-update.d/blueprint-ra` with exact executable fixture bytes;
  - local plugin ID: `blueprint.ra.fixture`;
  - copied Resource ID: `helper-script`, portable path `~/.local/bin/blueprint-ra-helper`;
  - target mapping: `helper-script -> ~/bin/blueprint-ra-helper`;
  - Git Resource ID: `git-fixture`, path `~/Projects/blueprint-ra-fixture`;
  - Restore-disabled Resource ID: `target-only-skip`, path `~/.local/share/blueprint-ra/skip.txt`.
- The deterministic Git fixture commit is `e161ca52ec7604e6fb335ef15fc212bba03a2994`; the worker must preserve the exact fixture file, commit message, author, email, and timestamps that produce that revision.
- Configure the `target` machine's Restore Skip for `target-only-skip` through the public policy CLI from PR #39, and assert the effective value, explicit machine-scope source, and Resource target through its public JSON inspection. Do not edit policy TOML or add a CI-only Blueprint command.
- Machine A Capture uses the public non-mutating preview and normal reviewed CLI approval from PR #39; changed approval authority fails `CAPTURE` with no retry or reapproval. Do not use plain scripted Capture to bypass review.
- PR 2 starts from the latest merged `main` after PR #39. The command examples below reflect PR #39's currently reviewed surface; re-check the merged CLI syntax and JSON envelope before implementation, and adjust examples to the shipped commands rather than inventing a CI-only variant.
- The acceptance workflow must require no repository secrets and use `permissions: contents: read`.
- The workflow name and job name in the production PR are both `Reconstruction Assurance` so the required status context is stable.
- No `continue-on-error` on the canonical job.
- Do not add persistent/cached VM base images, self-hosted runners, multi-Omarchy matrices, qcow2 artifact uploads, or broad test infrastructure abstractions in v1.
- Ordinary `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint` remain unchanged in purpose and must continue to pass.
- Every code-bearing task follows TDD: first add the smallest focused failing harness/unit test or executable assertion, confirm the failure, implement the minimum behavior, rerun focused verification, then run the relevant wider suite before committing.

## Review Focus

1. **Hosted runner has less disk than the spike observed:** the harness must fail early as `INFRASTRUCTURE` with free-space/KVM diagnostics before downloading/installing, rather than failing later with a misleading Omarchy/Blueprint error. Task 4 pins this behavior.
2. **Guest is slow to become reachable but QEMU is healthy:** readiness must poll with a bounded timeout and must distinguish timeout/QEMU exit from product failures; it must not relaunch the semantic operation. Tasks 3-4 pin this behavior.
3. **Source state leaks into the target through VM/profile plumbing:** target preflight must fail before Restore if any canonical customization is already present, especially either Resource path. Task 10 pins this behavior.
4. **Dry-run contains an unexpected removal, Force/Exact effect, interactive operation, unmapped Resource destination, or missing policy skip:** the plan contract must reject it before approval. Tasks 9 and 11 pin this behavior.
5. **Approval prompt never appears or Blueprint rejects the plan after approval:** the harness must not send approval early, retry, or silently re-run; it must preserve the transcript and fail `RESTORE_APPROVAL`. Task 12 pins this behavior.

---

## File Structure and Ownership Map

The implementation should converge on these files and responsibilities:

- `.github/workflows/reconstruction-assurance.yml` — thin GitHub Actions orchestration only; stable production check name.
- `test/reconstruction/README.md` — maintainer-facing explanation, local prerequisites, fixture contract, failure phases.
- `test/reconstruction/run.sh` — top-level phase orchestration, first-failure preservation, cleanup, summary.
- `test/reconstruction/lib/common.sh` — shared constants, logging, phase/status recording, retry/readiness helpers, SSH/scp wrappers.
- `test/reconstruction/lib/contract.py` — pure JSON/profile-plan contract checks; no VM/process orchestration.
- `test/reconstruction/tests/test_contract.py` — standard-library unit tests for plan/final-convergence checks.
- `test/reconstruction/tests/test_approve_capture.py` — standard-library subprocess tests proving reviewed Capture approval waits for the real prompt and never retries changed authority.
- `test/reconstruction/tests/test_approve_restore.py` — standard-library subprocess tests proving Restore approval is sent only after the prompt and failures are preserved.
- `test/reconstruction/vm/make-cidata.py` — generate supported Omarchy unattended cidata JSON/ISO inputs; deliberately omit `kernels`.
- `test/reconstruction/vm/install-omarchy.sh` — download/check ISO, create pristine disk/NVRAM, install, validate, power off.
- `test/reconstruction/vm/guest.sh` — create overlay/NVRAM copy, boot/stop guest, freshen identity, readiness polling.
- `test/reconstruction/fixtures/plugin/manifest.json` — minimal valid third-party service plugin manifest.
- `test/reconstruction/fixtures/plugin/Service.qml` — inert QML service fixture.
- `test/reconstruction/fixtures/git-resource/README.md` — deterministic Git Resource content.
- `test/reconstruction/fixtures/scripts/blueprint-ra-helper` — copied executable Resource fixture.
- `test/reconstruction/fixtures/hooks/blueprint-ra` — harmless executable Hook fixture.
- `test/reconstruction/fixtures/skip/skip.txt` — Restore-disabled copied Resource fixture.
- `test/reconstruction/fixtures/expected.env` — immutable expected IDs, paths, hashes/revision, theme/default values.
- `test/reconstruction/scenario/serve_git.py` — standard-library HTTPS static server for the dumb-HTTP Git fixture remote.
- `test/reconstruction/scenario/build-fixtures.sh` — construct deterministic bare Git remote, its fixture TLS certificate, and start/stop the host HTTPS Git server.
- `test/reconstruction/scenario/customize-source.sh` — configure Machine A and independently assert source state before Capture.
- `test/reconstruction/scenario/capture-source.sh` — initialize profile/machines, track Resources, configure target policy through public CLI, preview/review/approve Capture, check, archive/digest profile.
- `test/reconstruction/scenario/approve_capture.py` — drive the real TTY-required Capture review prompt once and preserve its transcript/exit status.
- `test/reconstruction/scenario/preflight-target.sh` — prove Machine B is fresh and profile binding is target-local.
- `test/reconstruction/scenario/approve_restore.py` — execute real remote Restore, wait for prompt, send `yes`, preserve transcript/exit status.
- `test/reconstruction/scenario/restore-target.sh` — dry-run, plan contract, approval/apply, journal and Blueprint Verify evidence.
- `test/reconstruction/verify/verify-target.sh` — native independent target assertions plus final no-actionable-work dry-run.
- `docs/adr/0021-real-omarchy-reconstruction-assurance.md` — approved ADR, renumbered to avoid PR #39's ADR 0020.
- `docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md` — approved design copied unchanged at execution preflight.
- `docs/planning/plans/2026-09-25-reconstruction-assurance-v1-implementation-plan.md` — this plan copied unchanged at execution preflight.

Keep scripts focused. If a shell file exceeds roughly 250-300 lines because it owns two responsibilities, split the responsibility rather than growing a generic framework.

## Execution Preflight

The approved ADR/design/plan currently exist outside the repository working tree. Before Task 1, copy them into the repository unchanged:

```text
docs/adr/0021-real-omarchy-reconstruction-assurance.md
docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md
docs/planning/plans/2026-09-25-reconstruction-assurance-v1-implementation-plan.md
```

Commit them first on the PR 1 branch:

```bash
git add docs/adr/0021-real-omarchy-reconstruction-assurance.md \
        docs/planning/specs/2026-09-25-reconstruction-assurance-v1-design.md \
        docs/planning/plans/2026-09-25-reconstruction-assurance-v1-implementation-plan.md
git commit -m "docs: define reconstruction assurance v1"
```

Do not rewrite approved semantics while copying. If a later task proves an assumption false, stop that task and update the design in a distinct reviewed documentation commit before adapting the harness.

## Pull Request Topology

Implement and merge in this order. Do not begin PR N+1 until PR N is reviewed, CI-green, and merged.

1. **PR 1 — `test: establish real Omarchy reconstruction harness`**
   - Land approved docs.
   - Extract/rework the feasibility spike into reusable host/VM primitives.
   - Add pure harness tests and a manually dispatchable substrate workflow.
   - Success means: official pinned Omarchy installs once, pristine base is validated and shut down, source/target overlays can boot independently with fresh identity.

2. **PR 2 — `test: capture canonical reconstruction profile`**
   - Add deterministic fixtures, source customization, machine overlays/policy through public CLI, Resource tracking, reviewed Capture/check, profile-only handoff, and target-freshness preflight.
   - Extend the workflow to run automatically on pull requests while still ending before Restore.
   - Success means: Machine A produces a valid captured profile; A is destroyed; Machine B starts clean and receives exactly the archive digest produced by A.

3. **PR 3 — `test: require end-to-end reconstruction assurance`**
   - Add dry-run contract, real approval helper, Restore/journal checks, independent target verification, final no-actionable-work check, diagnostics, and production workflow naming.
   - This PR turns the workflow into the complete Reconstruction Assurance gate. After it merges, configure the repository rules/branch protection to require the `Reconstruction Assurance` check for every PR.
   - Success means the full canonical A → profile → B lifecycle is green on the exact head SHA with focused failure artifacts.

**Prerequisite between PR 2 and PR 3 — fresh-package readiness.** Before PR 3, Blueprint must be able to inspect, `check`, and `status` a freshly installed supported Omarchy machine that has no pacman sync databases. Restore must also provide a safe, supported path for package resolution and elevation, without the harness pre-refreshing databases, pre-warming sudo, adding `NOPASSWD`, or running Blueprint wholesale as root. PR 2's hosted runs exposed two separate product defects:

- **Read path:** with no sync databases, native-package detection fails. Blueprint `check` on the fresh Machine B exits 1 with `check packages: detect explicitly installed native packages: pacman -Qqen: exit status 1: warning: database file for '<repo>' does not exist (use '-Sy' to download)`. `status` and inspection share that detection.
- **Write path:** Blueprint plans missing official packages as `omarchy pkg add <packages...>`, and normal Restore runs non-interactive operations through `SystemRunner.Run` (`CombinedOutput()`, with no stdin or terminal). For a non-root user, Omarchy 4.0.4's `omarchy pkg add` runs `sudo pacman -S --noconfirm --needed ...` and only skips sudo when its own EUID is 0. With cold sudo credentials, Restore cannot install `alacritty`, and it also needs package metadata the fresh install lacks. Running the outer SSH with `-tt` does not help, because Blueprint launches the command through its non-interactive runner.

The metadata question needs deliberate design rather than blindly embedding `pacman -Sy` in Blueprint; Omarchy's own full package refresh is substantially broader than a metadata-only refresh. Both defects must be solved before the canonical PR 3 Restore is allowed to go green. PR 3 then exercises the resulting contract on the unmodified fresh target.

The harness must never work around these gaps. Prohibited: pre-warming sudo credentials, `NOPASSWD` sudoers rules, running Blueprint wholesale as root, refreshing Machine B's package databases, or otherwise granting Blueprint authority or preparation that a real user's fresh machine would not have. A separate focused regression for these cases may follow later, but it does not replace the canonical fresh-target lifecycle.

Do not merge the old spike workflow from `spike/omarchy-vm-feasibility`; use it as implementation evidence only. Once PR 3 is complete, the production workflow supersedes the spike.

---

# PR 1 — `test: establish real Omarchy reconstruction harness`

**PR goal:** Turn the successful feasibility spike into focused, reusable test infrastructure without adding the canonical product scenario yet.

**Must remain unchanged:** Blueprint profile schema, provider/category semantics, CLI behavior, TUI behavior, existing `.github/workflows/ci.yml` commands.

### Task 1: Add harness constants, phase recording, and first-failure semantics

**Files:**
- Create: `test/reconstruction/lib/common.sh`
- Create: `test/reconstruction/run.sh`
- Create: `test/reconstruction/README.md`

**Interfaces:**
- Produces shell constants `RA_ROOT`, `RA_WORK`, `RA_ARTIFACTS`, `RA_USER`, `RA_SSH_PORT`, `RA_MIN_FREE_BYTES`, `OMARCHY_ISO_URL`, `OMARCHY_ISO_SHA256`.
- Produces functions `ra_phase <NAME>`, `ra_pass <NAME>`, `ra_fail <NAME> <message>`, `ra_note <message>`, `ra_cleanup_add <command...>`, `ra_finish`.
- `run.sh` becomes the only workflow entry point.

- [ ] **Step 1: Write a failing smoke test for first-failure preservation**

Create a temporary self-test block in `test/reconstruction/run.sh` activated only by `RA_SELF_TEST=1`:

```bash
if [[ ${RA_SELF_TEST:-0} == 1 ]]; then
  ra_phase CAPTURE
  ra_fail CAPTURE "capture failed first" || true
  ra_phase CLEANUP
  false || true
  ra_finish
fi
```

Run:

```bash
RA_SELF_TEST=1 bash test/reconstruction/run.sh
```

Expected before implementation: command fails because `common.sh`/phase helpers do not exist.

- [ ] **Step 2: Implement the phase state and cleanup stack**

`common.sh` must start with:

```bash
#!/usr/bin/env bash
set -euo pipefail

RA_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RA_WORK=${RA_WORK:-/mnt/omarchy-blueprint-ra}
RA_ARTIFACTS=${RA_ARTIFACTS:-$RA_WORK/artifacts}
RA_USER=${RA_USER:-spike}
RA_SSH_PORT=${RA_SSH_PORT:-2222}
RA_MIN_FREE_BYTES=${RA_MIN_FREE_BYTES:-19327352832} # 18 GiB
OMARCHY_ISO_URL=${OMARCHY_ISO_URL:-https://iso.omarchy.org/omarchy-4.0.4.iso}
OMARCHY_ISO_SHA256=${OMARCHY_ISO_SHA256:-ddeded2758c48318d201dfdac905ecb28f570441883f0c052ea3cd5d05acf92d}

RA_FIRST_FAILURE_PHASE=""
RA_FIRST_FAILURE_MESSAGE=""
RA_CLEANUP_CMDS=()
```

`ra_fail` records only the first phase/message and returns non-zero. `ra_finish` writes `$RA_ARTIFACTS/summary.txt`, runs cleanup commands in reverse order with errors downgraded to warnings, then exits 1 when a first failure exists and 0 otherwise.

- [ ] **Step 3: Run the self-test and inspect summary**

```bash
set +e
RA_SELF_TEST=1 bash test/reconstruction/run.sh
status=$?
set -e
test "$status" -eq 1
grep -F 'phase: CAPTURE' /mnt/omarchy-blueprint-ra/artifacts/summary.txt
grep -F 'capture failed first' /mnt/omarchy-blueprint-ra/artifacts/summary.txt
```

Expected: PASS; cleanup never replaces `CAPTURE` as the primary failure.

- [ ] **Step 4: Remove the inline self-test path and document the phase contract**

`README.md` must list exactly:

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

Also document that semantic steps never auto-retry.

- [ ] **Step 5: Shell syntax verification**

```bash
bash -n test/reconstruction/lib/common.sh test/reconstruction/run.sh
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add test/reconstruction/lib/common.sh test/reconstruction/run.sh test/reconstruction/README.md
git commit -m "test: add reconstruction harness lifecycle"
```

### Task 2: Add pure contract helpers and standard-library tests

**Files:**
- Create: `test/reconstruction/lib/contract.py`
- Create: `test/reconstruction/tests/test_contract.py`

**Interfaces:**
- Produces `load_envelope(path: Path) -> dict`.
- Produces `restore_plan(envelope: dict) -> dict`.
- Produces `assert_no_destructive_ops(plan: dict) -> None`.
- Produces `assert_no_interactive_ops(plan: dict) -> None`.
- Produces `assert_provider_present(plan: dict, provider: str) -> None`.
- Produces `assert_skip(plan: dict, provider: str, resource_substring: str, reason_substring: str) -> None`.
- Produces `assert_copy_destination(plan: dict, resource_substring: str, destination: str) -> None`.
- Produces `assert_final_convergence(plan: dict, allowed_skip_substring: str) -> None`.

- [ ] **Step 1: Write failing unit tests for destructive/interactivity/mapping/skip behavior**

Use `unittest` only. Include cases equivalent to:

```python
class PlanContractTests(unittest.TestCase):
    def test_rejects_delete_payload(self):
        plan = {"operations": [{"provider": "config", "action": "write", "resource": "x", "delete": {"destination": "/tmp/x"}}]}
        with self.assertRaisesRegex(AssertionError, "destructive"):
            contract.assert_no_destructive_ops(plan)

    def test_rejects_interactive_operation(self):
        plan = {"operations": [{"provider": "plugins", "action": "install", "resource": "plugin:x", "interactive": True}]}
        with self.assertRaisesRegex(AssertionError, "interactive"):
            contract.assert_no_interactive_ops(plan)

    def test_requires_mapped_copy_destination(self):
        plan = {"operations": [{"provider": "resources", "action": "copy", "resource": "resource:helper-script", "copy": {"destination": "/home/spike/bin/blueprint-ra-helper"}}]}
        contract.assert_copy_destination(plan, "helper-script", "/home/spike/bin/blueprint-ra-helper")

    def test_final_convergence_allows_only_policy_skip(self):
        plan = {"operations": [], "skipped": [{"provider": "resources", "resource": "resource:target-only-skip", "reason": "restore disabled for machine \"target\""}]}
        contract.assert_final_convergence(plan, "target-only-skip")
```

- [ ] **Step 2: Run tests and confirm they fail on missing module/functions**

```bash
python3 -m unittest discover -s test/reconstruction/tests -p 'test_contract.py' -v
```

Expected: FAIL.

- [ ] **Step 3: Implement the pure helpers**

`load_envelope` must require:

```python
assert payload["api_version"] == 1
assert payload["command"] == "restore"
assert payload["ok"] is True
```

`restore_plan` reads `envelope["data"]["plan"]` and requires `operations` to be a list.

`assert_no_destructive_ops` rejects any operation where:

```python
op.get("action") in {"remove", "delete"} or op.get("delete") is not None
```

and rejects filesystem writes/copies/symlinks that declare `replace_existing: true` in the canonical Safe+Additive scenario.

`assert_final_convergence` requires zero operations and permits skipped entries only when their resource contains `allowed_skip_substring` and reason contains `restore disabled`.

- [ ] **Step 4: Run helper tests**

```bash
python3 -m unittest discover -s test/reconstruction/tests -p 'test_contract.py' -v
```

Expected: PASS.

- [ ] **Step 5: Run ordinary Go verification to prove no product change**

```bash
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add test/reconstruction/lib/contract.py test/reconstruction/tests/test_contract.py
git commit -m "test: add reconstruction plan contract helpers"
```

### Task 3: Generate supported unattended Omarchy cidata without overriding kernel policy

**Files:**
- Create: `test/reconstruction/vm/make-cidata.py`
- Create: `test/reconstruction/tests/test_cidata.py`

**Interfaces:**
- CLI: `python3 test/reconstruction/vm/make-cidata.py --output-dir <dir> --ssh-public-key <path>`.
- Produces `user_configuration.json`, `user_credentials.json`, `user_encrypt_installation.txt`, `authorized_keys`, and `cidata.iso` inputs; ISO assembly remains shell-owned in Task 4.
- Configuration must not contain a `kernels` key.

- [ ] **Step 1: Write failing config-generation tests**

Test the pure `build_configuration()` value:

```python
config = make_cidata.build_configuration()
self.assertNotIn("kernels", config)
self.assertEqual(config["hostname"], "blueprint-ra-base")
self.assertEqual(config["bootloader"], "Limine")
self.assertEqual(config["timezone"], "UTC")
self.assertEqual(config["disk_config"]["device_modifications"][0]["device"], "/dev/vda")
```

Also assert the mirror list includes Omarchy's mirror and Arch fallback mirrors copied from the successful spike, and the packages include `base-devel`, `git`, `omarchy-keyring`, and `snapper`.

- [ ] **Step 2: Run and confirm failure**

```bash
python3 -m unittest discover -s test/reconstruction/tests -p 'test_cidata.py' -v
```

Expected: FAIL.

- [ ] **Step 3: Implement `make-cidata.py` from the successful spike**

Use only Python stdlib plus invoking `openssl passwd -6 spike` for the encrypted password. Preserve the successful 40 GiB unencrypted Btrfs layout and config schema `version: "3.0.9"`, but deliberately omit the old spike line that forced `kernels: ["linux"]`.

The generated root partition/subvolume layout must retain:

```text
/      -> @
/home  -> @home
/var/log -> @log
/var/cache/pacman/pkg -> @pkg
```

- [ ] **Step 4: Run tests and inspect JSON**

```bash
python3 -m unittest discover -s test/reconstruction/tests -p 'test_cidata.py' -v
python3 test/reconstruction/vm/make-cidata.py --help
```

Expected: PASS; no `kernels` key appears.

- [ ] **Step 5: Commit**

```bash
git add test/reconstruction/vm/make-cidata.py test/reconstruction/tests/test_cidata.py
git commit -m "test: generate supported Omarchy unattended config"
```

### Task 4: Build and validate one pristine Omarchy base disk

**Files:**
- Create: `test/reconstruction/vm/install-omarchy.sh`
- Modify: `test/reconstruction/run.sh`

**Interfaces:**
- `install-omarchy.sh <work-dir>` produces:
  - `<work-dir>/base/omarchy-base.qcow2`
  - `<work-dir>/base/OVMF_VARS.base.fd`
  - `<artifacts>/base/omarchy-serial.log`
  - `<artifacts>/base/guest-checks.log`
- Consumes constants/functions from `lib/common.sh`.

- [ ] **Step 1: Add an early infrastructure assertion to `run.sh`**

Before any download:

```bash
ra_phase INFRASTRUCTURE
[[ -e /dev/kvm ]] || ra_fail INFRASTRUCTURE "/dev/kvm missing"
if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
  sudo chgrp "$(id -gn)" /dev/kvm
fi
[[ -r /dev/kvm && -w /dev/kvm ]] || ra_fail INFRASTRUCTURE "/dev/kvm unusable"
free_bytes=$(df -B1 --output=avail /mnt | tail -1 | tr -d ' ')
(( free_bytes >= RA_MIN_FREE_BYTES )) || ra_fail INFRASTRUCTURE "need at least $RA_MIN_FREE_BYTES free bytes on /mnt; have $free_bytes"
```

Record `lscpu`, `free -h`, `df -h / /mnt`, `/dev/kvm` permissions, and QEMU version under `artifacts/host/`.

- [ ] **Step 2: Run the infrastructure check on a non-KVM development machine and confirm a focused failure**

```bash
RA_WORK=$(mktemp -d) bash test/reconstruction/run.sh
```

Expected outside the hosted KVM runner: fail in `INFRASTRUCTURE`, before ISO download. On a KVM-capable runner, proceed to the next unimplemented phase.

- [ ] **Step 3: Implement verified ISO download and cidata assembly**

Use:

```bash
curl -fL --retry 3 --retry-all-errors -o "$work/omarchy.iso" "$OMARCHY_ISO_URL"
printf '%s  %s\n' "$OMARCHY_ISO_SHA256" "$work/omarchy.iso" | sha256sum -c -
genisoimage -quiet -output "$work/cidata.iso" -volid cidata -joliet -rock \
  "$work/cidata/user_configuration.json" \
  "$work/cidata/user_credentials.json" \
  "$work/cidata/user_encrypt_installation.txt" \
  "$work/cidata/authorized_keys"
```

Network transfer may retry; checksum mismatch must not retry as a semantic success.

- [ ] **Step 4: Implement the install QEMU invocation from the successful spike**

Use KVM, q35, host CPU, 4 vCPUs, 8192 MiB RAM, a 40 GiB qcow2 disk, OVMF CODE/VARS, VGA console, serial log, ISO + cidata CD-ROMs, and user networking with SSH port forwarding.

Do not include the old Ubuntu cloud-image nested-boot smoke test in production; the real Omarchy install itself is the substrate test.

- [ ] **Step 5: Poll installed-guest readiness without restarting QEMU**

The readiness command must require all of:

```bash
test "$(cat /proc/1/comm)" = systemd
test ! -e /run/archiso/bootmnt
findmnt -no SOURCE / | grep -q '^/dev/vda'
command -v pacman
command -v git
command -v omarchy
omarchy commands --json >/dev/null
omarchy theme current >/dev/null
command -v omarchy-shell
test -d "$HOME"
kernel=$(cat "/usr/lib/modules/$(uname -r)/pkgbase")
pacman -Q "$kernel" "$kernel-headers"
test "$(cat "/usr/lib/modules/$(uname -r)/build/include/config/kernel.release")" = "$(uname -r)"
```

The unattended unencrypted base boots to SDDM. Before a user desktop session,
`omarchy plugin list --json` returns `omarchy-shell is not running`; do not
misclassify that expected pre-login state as an installer failure. PR 2's
plugin scenario must establish a real Omarchy session before checking plugin
discovery over Shell IPC. This is a documented design correction from hosted
PR 1 run `36166325222`, not permission to simulate Shell.

Poll for at most 8 minutes. If QEMU exits, fail immediately. If readiness times out, fail `OMARCHY_INSTALL`; do not restart the installer.

- [ ] **Step 6: Cleanly power off and freeze the base**

After health succeeds:

```bash
ssh ... 'sudo systemctl poweroff'
# wait for QEMU exit
chmod 0444 "$work/base/omarchy-base.qcow2"
cp "$work/vars.fd" "$work/base/OVMF_VARS.base.fd"
chmod 0444 "$work/base/OVMF_VARS.base.fd"
```

Record `qemu-img info`, actual qcow2 bytes, install duration, minimum host free bytes, `uname -r`, and matching kernel/header packages.

- [ ] **Step 7: Wire PR 1 `run.sh` to stop after pristine-base validation**

Expected phase summary:

```text
INFRASTRUCTURE PASS
OMARCHY_INSTALL PASS
```

No source/target product phases run yet.

- [ ] **Step 8: Run the real hosted experiment with `workflow_dispatch` before declaring Task 4 complete**

Expected: official 4.0.4 install succeeds, base powers off, artifacts are focused, no product files changed.

- [ ] **Step 9: Commit**

```bash
git add test/reconstruction/vm/install-omarchy.sh test/reconstruction/run.sh
git commit -m "test: build pristine Omarchy reconstruction base"
```

### Task 5: Add reusable sequential overlay guest lifecycle

**Files:**
- Create: `test/reconstruction/vm/guest.sh`
- Modify: `test/reconstruction/run.sh`

**Interfaces:**
- `ra_guest_create <role>` creates `$RA_WORK/guests/<role>.qcow2` and role-specific OVMF vars from the pristine base.
- `ra_guest_start <role> <hostname>` starts exactly one QEMU process and waits for SSH/Omarchy readiness.
- `ra_guest_exec <role> <command...>` executes as `spike`.
- `ra_guest_copy_to <role> <local> <remote>` and `ra_guest_copy_from <role> <remote> <local>` wrap scp.
- `ra_guest_freshen_identity <role> <hostname>` regenerates machine identity and hostname, reboots once, and re-waits for readiness.
- `ra_guest_stop <role>` powers off and waits for QEMU exit.

- [ ] **Step 1: Add shell-level argument/one-guest guards**

Before implementation, source `guest.sh` in a small shell snippet and call `ra_guest_start source ...` without a created overlay; expect a descriptive non-zero failure.

- [ ] **Step 2: Implement overlay/NVRAM creation**

```bash
qemu-img create -f qcow2 -F qcow2 -b "$RA_WORK/base/omarchy-base.qcow2" "$overlay"
cp "$RA_WORK/base/OVMF_VARS.base.fd" "$vars"
```

Never make the base writable.

- [ ] **Step 3: Implement one-active-guest enforcement**

Keep `RA_ACTIVE_GUEST` and refuse to start a second role while the first QEMU PID is alive.

- [ ] **Step 4: Implement fresh identity before Blueprint use**

For each role, before installing/running Blueprint:

```bash
sudo hostnamectl hostname "$hostname"
sudo rm -f /etc/machine-id /var/lib/dbus/machine-id
sudo systemd-machine-id-setup
sudo ln -sf /etc/machine-id /var/lib/dbus/machine-id
sudo systemctl reboot
```

Wait for SSH to disappear and return, then assert hostname and non-empty machine ID. Save each role's machine ID under artifacts and assert source != target once both have been created.

- [ ] **Step 5: Add a PR 1 overlay smoke lifecycle**

`run.sh` should:

```text
create source → start → freshen identity → record ID → stop → destroy source overlay
create target → start → freshen identity → record ID → assert different → stop → destroy target overlay
```

Do not run Blueprint yet.

- [ ] **Step 6: Verify sequential resource behavior on hosted runner**

Record peak disk and memory while each overlay is active. Assert only one QEMU PID exists at a time.

- [ ] **Step 7: Commit**

```bash
git add test/reconstruction/vm/guest.sh test/reconstruction/run.sh
git commit -m "test: add sequential reconstruction guests"
```

### Task 6: Add PR 1 manually dispatched substrate workflow

**Files:**
- Create: `.github/workflows/reconstruction-assurance.yml`

**Interfaces:**
- Initial PR 1 workflow name: `Reconstruction Harness`.
- Initial job name: `Omarchy VM substrate`.
- Trigger: `workflow_dispatch` only in PR 1.

- [ ] **Step 1: Add workflow syntax with least privilege**

```yaml
name: Reconstruction Harness

on:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  substrate:
    name: Omarchy VM substrate
    runs-on: ubuntu-24.04
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v6
        with:
          go-version: '1.26.x'
          cache: true
      - name: Install VM dependencies
        run: |
          sudo apt-get update -qq
          sudo apt-get install -y --no-install-recommends qemu-system-x86 qemu-utils ovmf genisoimage openssh-client
      - name: Build Blueprint
        run: go build -o /tmp/omarchy-blueprint-ra ./cmd/omarchy-blueprint
      - name: Prepare reconstruction workspace
        run: sudo mkdir -p /mnt/omarchy-blueprint-ra && sudo chown "$USER:$USER" /mnt/omarchy-blueprint-ra
      - name: Run substrate harness
        run: bash test/reconstruction/run.sh
      - uses: actions/upload-artifact@v4
        if: failure()
        with:
          name: reconstruction-harness-failure
          path: /mnt/omarchy-blueprint-ra/artifacts
          if-no-files-found: ignore
```

Do not recursively change ownership of `/mnt`; only create/chown `/mnt/omarchy-blueprint-ra`.

- [ ] **Step 2: Validate workflow YAML structurally**

Use repository-available tooling if present; at minimum inspect with Ruby/Python YAML only if available. Do not add a product dependency just to parse Actions YAML.

- [ ] **Step 3: Dispatch the workflow twice on the same head SHA**

Expected: both runs green; KVM/base install/overlay identities succeed without semantic retries.

- [ ] **Step 4: Run ordinary CI commands locally/CI**

```bash
go test ./...
go vet ./...
go build ./cmd/omarchy-blueprint
python3 -m unittest discover -s test/reconstruction/tests -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/reconstruction-assurance.yml
git commit -m "ci: add real Omarchy reconstruction substrate"
```

**PR 1 acceptance:** manually dispatched substrate workflow is green twice at exact PR head; ordinary Go CI is green; no Blueprint runtime/schema semantics changed.

---

# PR 2 — `test: capture canonical reconstruction profile`

**PR goal:** Build deterministic canonical source state on a real guest, Capture it through the shipped CLI, destroy Machine A, and prove a clean Machine B receives exactly the portable profile and nothing else.

### Task 7: Add deterministic fixture files and local Git remote builder

**Files:**
- Create: `test/reconstruction/fixtures/plugin/manifest.json`
- Create: `test/reconstruction/fixtures/plugin/Service.qml`
- Create: `test/reconstruction/fixtures/git-resource/README.md`
- Create: `test/reconstruction/fixtures/scripts/blueprint-ra-helper`
- Create: `test/reconstruction/fixtures/hooks/blueprint-ra`
- Create: `test/reconstruction/fixtures/skip/skip.txt`
- Create: `test/reconstruction/fixtures/expected.env`
- Create: `test/reconstruction/scenario/build-fixtures.sh`

**Interfaces:**
- `ra_build_git_fixture` produces `$RA_WORK/git/blueprint-ra.git` and asserts HEAD `e161ca52ec7604e6fb335ef15fc212bba03a2994`.
- `ra_start_git_server` serves `https://10.0.2.2:9443/blueprint-ra.git` to guests (dumb HTTP over TLS, bound to host loopback, which QEMU user networking exposes as `10.0.2.2`).
- `ra_stop_git_server` stops it.
- `ra_guest_trust_git_fixture <role>` installs the fixture certificate as `/etc/blueprint-ra/git-fixture.pem` and scopes it with `git config --system http.https://10.0.2.2:9443/.sslCAInfo`. This is identical guest network infrastructure applied to both machines before any customization or preflight; it lives outside `$HOME` and is never Blueprint state.

> **Amendment (PR 2):** Blueprint deliberately treats only `https`, `ssh`, and scp-like Git remotes as portable, and `git remote get-url` expands `insteadOf`, so the originally planned `git://10.0.2.2:9418` daemon cannot be tracked as a Git Resource. The fixture is therefore served over HTTPS with a per-run self-signed certificate. The design only requires a deterministic repository-controlled remote reachable from QEMU; the commit and its revision are unchanged.

- [ ] **Step 1: Add exact fixture contents**

`manifest.json`:

```json
{
  "schemaVersion": 1,
  "id": "blueprint.ra.fixture",
  "name": "Blueprint RA Fixture",
  "version": "1.0.0",
  "kinds": ["service"],
  "entryPoints": {"service": "Service.qml"}
}
```

`Service.qml`:

```qml
import QtQuick

QtObject {}
```

`git-resource/README.md` exact bytes:

```text
Blueprint Reconstruction Assurance Git fixture.
```

`blueprint-ra-helper`:

```bash
#!/usr/bin/env bash
printf 'blueprint-ra-helper\n'
```

`hooks/blueprint-ra`:

```bash
#!/usr/bin/env bash
printf 'blueprint-ra-hook\n'
```

`skip/skip.txt`:

```text
This resource is intentionally skipped on the target machine.
```

Make both executable fixtures mode `0755` in Git.

- [ ] **Step 2: Write `expected.env` with immutable contract values**

```bash
RA_PACKAGE=alacritty
RA_THEME=catppuccin
RA_DEFAULT_TERMINAL=alacritty
RA_PLUGIN_ID=blueprint.ra.fixture
RA_HELPER_RESOURCE=helper-script
RA_HELPER_SOURCE=.local/bin/blueprint-ra-helper
RA_HELPER_TARGET=bin/blueprint-ra-helper
RA_GIT_RESOURCE=git-fixture
RA_GIT_PATH=Projects/blueprint-ra-fixture
RA_GIT_REMOTE=https://10.0.2.2:9443/blueprint-ra.git
RA_GIT_REVISION=e161ca52ec7604e6fb335ef15fc212bba03a2994
RA_SKIP_RESOURCE=target-only-skip
RA_SKIP_PATH=.local/share/blueprint-ra/skip.txt
RA_CONFIG_PATH=.config/blueprint-ra/config.toml
RA_HOOK_PATH=.config/omarchy/hooks/post-update.d/blueprint-ra
```

- [ ] **Step 3: Implement deterministic Git fixture construction**

Use fixed metadata:

```bash
git init -q -b main "$work/repo"
git -C "$work/repo" config user.name 'Blueprint RA'
git -C "$work/repo" config user.email 'ra@example.invalid'
cp "$RA_ROOT/fixtures/git-resource/README.md" "$work/repo/README.md"
git -C "$work/repo" add README.md
GIT_AUTHOR_DATE='2026-09-25T00:00:00Z' \
GIT_COMMITTER_DATE='2026-09-25T00:00:00Z' \
  git -C "$work/repo" commit -q -m 'fixture: seed reconstruction resource'
test "$(git -C "$work/repo" rev-parse HEAD)" = "$RA_GIT_REVISION"
git clone -q --bare "$work/repo" "$RA_WORK/git/blueprint-ra.git"
git -C "$RA_WORK/git/blueprint-ra.git" update-server-info
```

Generate a fresh self-signed certificate per run with subject alternative names `IP:10.0.2.2` and `IP:127.0.0.1`. Its private key stays on the host.

- [ ] **Step 4: Start the HTTPS Git server and prove host-local clone works**

```bash
python3 "$RA_ROOT/scenario/serve_git.py" --root "$RA_WORK/git" --cert ... --key ... --port 9443 &
RA_GIT_SERVER_PID=$!
test "$(git -c http.sslCAInfo="$cert" ls-remote https://127.0.0.1:9443/blueprint-ra.git refs/heads/main | awk '{print $1}')" = "$RA_GIT_REVISION"
```

Then on the next real guest smoke, assert `git ls-remote "$RA_GIT_REMOTE"` works from QEMU before source customization.

- [ ] **Step 5: Commit**

```bash
git add test/reconstruction/fixtures test/reconstruction/scenario/build-fixtures.sh
git commit -m "test: add deterministic reconstruction fixtures"
```

### Task 8: Customize Machine A with the canonical portable-developer state

**Files:**
- Create: `test/reconstruction/scenario/customize-source.sh`
- Modify: `test/reconstruction/run.sh`

**Interfaces:**
- `customize-source.sh` runs inside Machine A as user `spike` with fixture files staged under `/tmp/blueprint-ra-fixtures`.
- It performs source mutations and then independently proves each one before returning success.

- [ ] **Step 1: Add source pristine preconditions before mutation**

Require:

```bash
! pacman -Q alacritty >/dev/null 2>&1
test "$(cat "$HOME/.local/state/omarchy/current/theme.name")" != catppuccin
test "$(omarchy default terminal)" != alacritty
test ! -e "$HOME/.config/omarchy/plugins/blueprint.ra.fixture"
test ! -e "$HOME/.config/blueprint-ra/config.toml"
test ! -e "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra"
test ! -e "$HOME/.local/bin/blueprint-ra-helper"
test ! -e "$HOME/Projects/blueprint-ra-fixture"
```

> **Amendment (PR 2):** `omarchy theme current` prints a display name (`Catppuccin`), so theme assertions compare Omarchy's native `~/.local/state/omarchy/current/theme.name` (`catppuccin`) instead of the display string.

If any fail, classify `SOURCE_CUSTOMIZATION` with "fixture no longer distinguishes pristine Omarchy state"; do not silently choose another value.

- [ ] **Step 2: Install Alacritty and set the terminal default**

`omarchy pkg add` invokes `sudo` itself, and the guest's sudo neither prompts nor honours `SUDO_ASKPASS` without a terminal. The harness therefore runs the same native command as root through `sudo -S` (its supported `EUID == 0` path), fed the disposable fixture password from a throwaway `/tmp` helper for this customization only (no sudoers change).

> **Amendment (PR 2):** the installed Omarchy 4.0.4 base has no pacman sync databases (`database file for 'core' does not exist`), so package resolution fails before any customization. Only Machine A refreshes them, with `pacman -Sy` (source fixture preparation, retryable, no package upgrade), because the harness must create the source customization. Machine B is never refreshed: the canonical target intentionally keeps the package-manager state produced by the official fresh install, because whether Blueprint can reconstruct onto that machine is exactly what Reconstruction Assurance asks. Common staging for both guests is limited to harness inputs: the Blueprint binary, the fixture certificate, fixture files, and desktop-session readiness.

> **Amendment (PR 2):** the Omarchy ISO only writes SDDM autologin for encrypted targets, so the unencrypted unattended guest stops at the greeter with no desktop session, no notification daemon, and no running `omarchy-shell` (which Blueprint's plugin inspection requires). Both guests receive the same `/etc/sddm.conf.d/autologin.conf` (`User=spike`, `Session=omarchy.desktop`) the ISO writes for encrypted installs, before their identity reboot, and staging waits until `omarchy-shell shell ping` succeeds.

> **Amendment (PR 2):** the first Hyprland login autostarts `omarchy-provision-first-run`, which installs mise tools (for example `codex`) and edits `~/.config/mise/config.toml` over several minutes. A hosted run approved Capture while this was still running, and Blueprint correctly failed closed with a changed review. Session readiness on both guests therefore also waits, up to 15 minutes, until first-login provisioning has finished: `~/.local/state/omarchy/first-run.log` exists and no `omarchy-provision-first-run` process remains. The completion marker is not used, because Omarchy only writes it when every step succeeds, and some steps (such as speaker tuning) may not apply in a VM. The first-run log is kept as a diagnostic.

```bash
omarchy pkg add alacritty
omarchy default terminal alacritty
pacman -Q alacritty
test "$(omarchy default terminal)" = alacritty
```

This one package intentionally serves both Packages and Defaults coverage.

- [ ] **Step 3: Switch to Catppuccin through the native Omarchy command**

```bash
omarchy theme set catppuccin
test "$(cat "$HOME/.local/state/omarchy/current/theme.name")" = catppuccin
```

Do not edit Omarchy theme selection files directly. If the pinned guest requires a supported headless environment variable for native theme application, apply the same environment consistently to source customization and the later Blueprint Restore process and document it in `README.md`; do not patch Omarchy.

- [ ] **Step 4: Install the local plugin fixture using the real plugin contract**

```bash
mkdir -p "$HOME/.config/omarchy/plugins/blueprint.ra.fixture"
cp -a /tmp/blueprint-ra-fixtures/plugin/. "$HOME/.config/omarchy/plugins/blueprint.ra.fixture/"
omarchy plugin validate "$HOME/.config/omarchy/plugins/blueprint.ra.fixture"
omarchy-shell shell rescanPlugins
omarchy plugin list --json > /tmp/plugins.json
python3 - <<'PY'
import json
items = json.load(open('/tmp/plugins.json'))
assert any(item['id'] == 'blueprint.ra.fixture' and not item['firstParty'] for item in items)
PY
```

- [ ] **Step 5: Create the supported Shell customization**

Use Python to load the current user Shell document if present, otherwise `/usr/share/omarchy/config/omarchy/shell.json`, then write canonical JSON to `~/.config/omarchy/shell.json` with:

```python
doc["idle"]["lock"] = 600
doc["bar"]["position"] = "bottom"
```

Assert version remains `1`, lock is 600, and position is `bottom`.

- [ ] **Step 6: Create explicit Config and Hook fixtures**

```bash
mkdir -p "$HOME/.config/blueprint-ra"
printf 'mode = "reconstructed"\n' > "$HOME/.config/blueprint-ra/config.toml"

mkdir -p "$HOME/.config/omarchy/hooks/post-update.d"
cp /tmp/blueprint-ra-fixtures/hooks/blueprint-ra "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra"
chmod 0755 "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra"
```

- [ ] **Step 7: Create Resource fixtures and clone the Git fixture through the host HTTPS server**

```bash
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/blueprint-ra" "$HOME/Projects"
cp /tmp/blueprint-ra-fixtures/scripts/blueprint-ra-helper "$HOME/.local/bin/blueprint-ra-helper"
chmod 0755 "$HOME/.local/bin/blueprint-ra-helper"
cp /tmp/blueprint-ra-fixtures/skip/skip.txt "$HOME/.local/share/blueprint-ra/skip.txt"
git clone "$RA_GIT_REMOTE" "$HOME/Projects/blueprint-ra-fixture"
test "$(git -C "$HOME/Projects/blueprint-ra-fixture" rev-parse HEAD)" = "$RA_GIT_REVISION"
test -z "$(git -C "$HOME/Projects/blueprint-ra-fixture" status --porcelain)"
```

- [ ] **Step 8: Copy an independent source assertion report to artifacts**

The script must output one `PASS <target>` line for package/theme/default/plugin/shell/config/hook/helper/git/skip; `run.sh` copies this to `artifacts/source/pre-capture.txt`.

- [ ] **Step 9: Run source customization on the real hosted guest and confirm every assertion**

Expected: PASS without Blueprint Capture yet.

- [ ] **Step 10: Commit**

```bash
git add test/reconstruction/scenario/customize-source.sh test/reconstruction/run.sh
git commit -m "test: create canonical reconstruction source state"
```

### Task 9: Initialize profile, machine overlays, Resources, policy, and Capture on Machine A

**Files:**
- Create: `test/reconstruction/scenario/capture-source.sh`
- Create: `test/reconstruction/scenario/approve_capture.py`
- Create: `test/reconstruction/tests/test_approve_capture.py`
- Modify: `test/reconstruction/run.sh`

**Interfaces:**
- Uses guest Blueprint binary at `/usr/local/bin/omarchy-blueprint` copied from the exact PR build.
- Produces `/tmp/blueprint-ra-profile.tar` and `/tmp/blueprint-ra-profile.sha256` on the CI host.

- [ ] **Step 1: Initialize profile and source/target machine overlays through CLI**

Inside Machine A:

```bash
PROFILE="$HOME/omarchy-profile"
omarchy-blueprint init "$PROFILE" --name reconstruction-assurance
omarchy-blueprint --profile "$PROFILE" machine add source
omarchy-blueprint --profile "$PROFILE" machine add target --no-use
```

Assert `machine current --json` reports `source` with binding source.

- [ ] **Step 2: Explicitly include the deterministic Config path**

```bash
omarchy-blueprint --profile "$PROFILE" include config:.config/blueprint-ra/config.toml
```

This avoids depending on unknown-app automatic Config surface discovery.

- [ ] **Step 3: Track the three Resources through the public CLI**

```bash
omarchy-blueprint --profile "$PROFILE" track "$HOME/.local/bin/blueprint-ra-helper" --id helper-script --strategy copy
omarchy-blueprint --profile "$PROFILE" track "$HOME/Projects/blueprint-ra-fixture" --id git-fixture --strategy git
omarchy-blueprint --profile "$PROFILE" track "$HOME/.local/share/blueprint-ra/skip.txt" --id target-only-skip --strategy copy
```

- [ ] **Step 4: Configure the target Resource mapping through the public CLI**

```bash
omarchy-blueprint --profile "$PROFILE" machine use target
omarchy-blueprint --profile "$PROFILE" machine map resource:helper-script ~/bin/blueprint-ra-helper
omarchy-blueprint --profile "$PROFILE" machine use source
```

Assert `tracked --json` under `--machine target` reports helper effective path `/home/spike/bin/blueprint-ra-helper` while its portable path remains `~/.local/bin/blueprint-ra-helper`.

- [ ] **Step 5: Set and inspect the target Restore Skip through the public policy CLI**

With PR #39's currently reviewed syntax (verify against its **merged** CLI before implementing):

```bash
omarchy-blueprint --profile "$PROFILE" policy set restore resources skip resource:target-only-skip --scope machine:target
omarchy-blueprint --profile "$PROFILE" --json policy show resources resource:target-only-skip --scope machine:target > /tmp/target-policy.json
```

Require a successful public `policy show` envelope with scope `machine:target` and the canonical `resources` target `resource:target-only-skip`. Its effective Restore value must be `skip`, `explicit` must be true, and the source must identify the target machine's explicit target rule (currently `kind: machine-target`, `machine: target`). Also require the explicit Restore rule in the reported scope for that same Resource. Do not edit `machines/target.toml`, desired Resource state, or Capture output to manufacture the result. Keep the Resource path mapping through `machine map` in Step 4.

- [ ] **Step 6: Preview Capture and assert candidate outcomes without mutation**

With PR #39's reviewed inspection surface (verify merged syntax and JSON fields):

```bash
omarchy-blueprint --profile "$PROFILE" --machine source --json capture --dry-run > /tmp/capture-preview.json
```

Require a successful public Capture preview envelope for machine `source`; inspect its semantic sections/targets for the canonical package, theme, plugin, Shell, Config, Hook, Default, and Resource candidates. Each intended captured customization must have an actionable `add` or `update` outcome, not `blocked` or `preserve`. Keep the preview in `artifacts/source/` and assert the profile has not been mutated by the dry-run. Do not treat this preview as later approval authority.

- [ ] **Step 7: Execute a normal reviewed Capture with explicit prompt-bound approval**

Add focused standard-library subprocess tests for a small Capture-specific `approve_capture.py`: a child must not receive `yes` before the exact `Apply this Capture? [y/N] ` prompt; a missing prompt must fail; a changed-review/authority error after approval must remain in the transcript and fail without rerunning or reapproving. PR #39 currently requires a TTY for `capture --review`, so provide a real local PTY and SSH `-tt` to the shipped CLI (not TUI automation):

```text
approve_capture.py --transcript <artifacts/source/capture-transcript.txt> -- \
  ssh -tt ... spike@127.0.0.1 omarchy-blueprint --profile /home/spike/omarchy-profile --machine source capture --review
```

Use the final merged PR #39 command. Do not pass `--json` or `--yes` to the mutating invocation: `--json capture --review` is inspection-only in the currently reviewed CLI. Wait for the rendered prompt, send `yes` once, and let Blueprint re-inspect/recalculate before it applies. A changed-approved-outcome/value failure is a hard `CAPTURE` failure, never an automatic retry. Persist the transcript and exit status; do not use internal APIs or edit the resulting profile to imitate Capture.

- [ ] **Step 8: Run Blueprint check after approved Capture**

```bash
omarchy-blueprint --profile "$PROFILE" --machine source --json check > /tmp/check.json
```

Require exit 0.

- [ ] **Step 9: Assert focused profile semantics before export**

Use small Python/TOML/text assertions rather than snapshotting the whole profile:

- packages contain `alacritty` as desired-present;
- theme current is `catppuccin`;
- plugin metadata/artifact contains `blueprint.ra.fixture`;
- Shell metadata has a non-empty captured hash;
- Config carries `.config/blueprint-ra/config.toml`;
- Hooks carry `post-update.d/blueprint-ra`;
- Defaults terminal is `alacritty`;
- Resources contain `helper-script`, `git-fixture`, `target-only-skip`;
- Git Resource revision is `e161ca52ec7604e6fb335ef15fc212bba03a2994`;
- target machine mapping and Restore Disabled rule remain present.

- [ ] **Step 10: Create deterministic profile handoff archive and digest**

On Machine A:

```bash
tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner \
  -C "$HOME" -cf /tmp/blueprint-ra-profile.tar omarchy-profile
sha256sum /tmp/blueprint-ra-profile.tar > /tmp/blueprint-ra-profile.sha256
```

Copy only these two profile-handoff files out to the host plus the Capture preview, reviewed transcript and check logs as focused diagnostics (not as reconstruction state).

- [ ] **Step 11: Stop and delete Machine A before target creation**

`run.sh` must call `ra_guest_stop source`, remove the source overlay/NVRAM, and assert no source QEMU PID before `ra_guest_create target`.

- [ ] **Step 12: Commit**

```bash
git add test/reconstruction/scenario/capture-source.sh \
        test/reconstruction/scenario/approve_capture.py \
        test/reconstruction/tests/test_approve_capture.py \
        test/reconstruction/run.sh
git commit -m "test: capture canonical portable profile"
```

### Task 10: Prove Machine B freshness and exact profile-only handoff

**Files:**
- Create: `test/reconstruction/scenario/preflight-target.sh`
- Modify: `test/reconstruction/run.sh`

**Interfaces:**
- Target preflight runs before profile extraction/Restore mutation.
- Produces `artifacts/target/preflight.txt`.

- [ ] **Step 1: Create/freshen Machine B and assert distinct OS identity**

Start target from a fresh overlay, hostname `blueprint-ra-target`, regenerate machine ID, and assert it differs from saved source machine ID.

- [ ] **Step 2: Assert source customizations are absent before importing profile**

Require all:

```bash
! pacman -Q alacritty >/dev/null 2>&1
test "$(cat "$HOME/.local/state/omarchy/current/theme.name")" != catppuccin
test "$(omarchy default terminal)" != alacritty
test ! -e "$HOME/.config/omarchy/plugins/blueprint.ra.fixture"
test ! -e "$HOME/.config/blueprint-ra/config.toml"
test ! -e "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra"
test ! -e "$HOME/.local/bin/blueprint-ra-helper"
test ! -e "$HOME/bin/blueprint-ra-helper"
test ! -e "$HOME/Projects/blueprint-ra-fixture"
test ! -e "$HOME/.local/share/blueprint-ra/skip.txt"
```

For Shell, assert current state is not simultaneously `idle.lock=600` and `bar.position=bottom`.

Any failure is `TARGET_PREFLIGHT`; do not run Restore.

- [ ] **Step 3: Import only the profile archive and verify digest before extraction**

Copy `/tmp/blueprint-ra-profile.tar` and `.sha256` from host to target `/tmp`; run:

```bash
cd /tmp
sha256sum -c blueprint-ra-profile.sha256
tar -C "$HOME" -xf blueprint-ra-profile.tar
```

No source `$XDG_STATE_HOME`, home tarball, package cache, or journal is transferred.

- [ ] **Step 4: Select target machine locally and prove profile bytes do not change**

Compute deterministic archive SHA before binding, run:

```bash
omarchy-blueprint --profile "$HOME/omarchy-profile" machine use target
```

Recompute the deterministic profile archive SHA and require equality. Then require `machine current --json` reports target with local binding.

- [ ] **Step 5: Persist and inspect the target machine's Safe + Additive Restore defaults**

First verify the imported archive digest and unchanged portable profile bytes through Step 4. Then, through the public PR #39 CLI on Machine B, explicitly persist the canonical `target` Restore defaults and inspect them using public JSON (examples reflect the currently reviewed PR #39 syntax; verify against its merged CLI before implementation):

```bash
omarchy-blueprint --profile "$HOME/omarchy-profile" machine restore-defaults set target --conflicts safe --convergence additive
omarchy-blueprint --profile "$HOME/omarchy-profile" --json machine restore-defaults target > /tmp/target-restore-defaults.json
```

Require the inspection envelope to identify machine `target` with `conflicts: safe` and `convergence: additive`, and preserve it in `artifacts/target/`. This intentional, public CLI machine-policy change occurs **after** the captured profile's transfer/binding digest checks; it is not a changed Capture result or a shortcut through Restore. Do not pass a one-run Force/Exact (or redundant Safe/Additive) override when later invoking Restore. PR 3 must prove the normal Restore consumes these persisted values.

- [ ] **Step 6: Run Blueprint check on target profile**

```bash
omarchy-blueprint --profile "$HOME/omarchy-profile" --machine target --json check > /tmp/target-check.json
```

Require exit 0.

> **Amendment (PR 2): known-gap sentinel.** Until the fresh-package readiness prerequisite lands, this `check` fails on the unmodified Machine B (read-path defect). PR 2 pins that failure narrowly instead of either requiring success or ignoring the result:
>
> 1. Before running `check`, require that Machine B still has no pacman sync databases (no `/var/lib/pacman/sync/*.db`), so an accidental target refresh cannot silently hide the sentinel.
> 2. Run `check`, keeping stdout, stderr and the exit status.
> 3. If it exits 1 with no successful envelope, and stderr carries the semantic fingerprint (`check packages:`, `detect explicitly installed native packages:`, `pacman -Qqen:`, `exit status 1`, `database file for '<repo>' does not exist`, `use '-Sy' to download`), record `known gap: packages.fresh-sync-database-readiness` in the summary and keep the stderr as an artifact. `TARGET_PREFLIGHT` still passes. The exact list of repository warnings is incidental and is not byte-matched.
> 4. If it exits 0, fail `TARGET_PREFLIGHT` with "fresh-machine package-readiness gap unexpectedly resolved; update the harness to require check success".
> 5. Any other failure fails `TARGET_PREFLIGHT` as an unexpected `check` failure.
>
> The sentinel is a temporary executable TODO, not accepted debt. The product PR that fixes fresh-machine detection turns Reconstruction Assurance red, and must switch this step back to requiring `check` success.

- [ ] **Step 7: Commit**

```bash
git add test/reconstruction/scenario/preflight-target.sh test/reconstruction/run.sh
git commit -m "test: enforce reconstruction profile boundary"
```

### Task 11: Make PR 2 run source Capture + target preflight on pull requests

**Files:**
- Modify: `.github/workflows/reconstruction-assurance.yml`

**Interfaces:**
- Workflow remains named `Reconstruction Harness` until PR 3.
- Add `pull_request` trigger in PR 2.

- [ ] **Step 1: Add `pull_request` trigger and fixture/helper tests before VM work**

```yaml
on:
  pull_request:
  workflow_dispatch:
```

Run:

```bash
python3 -m unittest discover -s test/reconstruction/tests -v
bash -n test/reconstruction/run.sh test/reconstruction/lib/common.sh test/reconstruction/vm/*.sh test/reconstruction/scenario/*.sh
```

before downloading the ISO.

- [ ] **Step 2: Upload focused artifacts only on failure**

Artifact paths must include summary, host/base logs, source Capture/check logs, profile archive digest/selected profile files, and target preflight/check logs. Do not include qcow2 disks or guest private SSH key.

- [ ] **Step 3: Verify one real PR run reaches target preflight green**

The run must prove source destroyed before target starts and target machine ID differs.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/reconstruction-assurance.yml
git commit -m "ci: exercise profile handoff on pull requests"
```

**PR 2 acceptance:** exact head has ordinary Go CI green plus `Reconstruction Harness` green; reviewed source Capture includes all canonical providers; the transferred profile digest is verified before target-side CLI policy setup; target preflight proves no source-state leakage, and public JSON reports persisted Safe + Additive defaults for `target`.

---

# PR 3 — `test: require end-to-end reconstruction assurance`

**Prerequisite:** the fresh-package readiness prerequisite described under Pull Request Topology (read path and write path) is resolved and merged first, and the PR 2 known-gap sentinel has been switched back to requiring target `check` success. Machine B keeps its official fresh-install package-manager state.

**PR goal:** Complete the real Restore/approval/verification path, rename the check to its production stable context, and make it suitable for required branch protection.

### Task 12: Add the canonical structured Restore-plan contract

**Files:**
- Modify: `test/reconstruction/lib/contract.py`
- Modify: `test/reconstruction/tests/test_contract.py`
- Create: `test/reconstruction/scenario/restore-target.sh`

**Interfaces:**
- `python3 contract.py assert-canonical-plan <plan.json>` exits 0 only when the canonical Safe+Additive plan is acceptable.
- `python3 contract.py assert-final-plan <plan.json>` exits 0 only when no actionable operations remain and the only policy skip is `target-only-skip`.

- [ ] **Step 1: Add failing canonical-plan tests**

Fixtures in the unit test must cover:

- missing provider operation for packages/themes/plugins/shell/config/hooks/defaults/resources;
- unexpected `remove` action;
- any `delete` payload;
- `interactive: true`;
- helper Resource copy to `/home/spike/.local/bin/blueprint-ra-helper` instead of mapped `/home/spike/bin/blueprint-ra-helper`;
- absent policy skip for `target-only-skip`;
- policy skip with wrong reason;
- valid plan with extra safe validation operations.

- [ ] **Step 2: Implement canonical checks**

Require providers with at least one operation:

```python
{"packages", "themes", "plugins", "shell", "config", "hooks", "defaults", "resources"}
```

For Resources require:

```python
assert_copy_destination(plan, "helper-script", "/home/spike/bin/blueprint-ra-helper")
assert_skip(plan, "resources", "target-only-skip", "restore disabled")
```

Do not require exact operation IDs/order.

- [ ] **Step 3: Add target dry-run to `restore-target.sh`**

Before planning, re-inspect `target`'s persisted Restore defaults through the public JSON CLI and require `conflicts: safe` and `convergence: additive`, as set through public CLI on Machine B in PR 2. The normal Restore must use those defaults rather than one-run override flags. Then run the structured dry-run without `--force`, `--exact`, `--conflicts`, or `--convergence`:

```bash
set +e
omarchy-blueprint --profile "$HOME/omarchy-profile" --machine target --json restore --dry-run > /tmp/restore-plan.json
status=$?
set -e
test "$status" -eq 0
python3 /tmp/blueprint-ra-harness/lib/contract.py assert-canonical-plan /tmp/restore-plan.json
```

Copy JSON to `artifacts/target/restore-plan.json` before mutation.

- [ ] **Step 4: Run pure helper tests**

```bash
python3 -m unittest discover -s test/reconstruction/tests -p 'test_contract.py' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add test/reconstruction/lib/contract.py test/reconstruction/tests/test_contract.py test/reconstruction/scenario/restore-target.sh
git commit -m "test: validate canonical restore plan"
```

### Task 13: Add real prompt-bound approval without `--yes`

**Files:**
- Create: `test/reconstruction/scenario/approve_restore.py`
- Create: `test/reconstruction/tests/test_approve_restore.py`
- Modify: `test/reconstruction/scenario/restore-target.sh`

**Interfaces:**
- CLI: `approve_restore.py --transcript <path> -- <command...>`.
- It waits for exact byte sequence `Apply this restore? [y/N] `, then writes `yes\n` once.
- It propagates the child exit code and never retries.

- [ ] **Step 1: Write failing subprocess tests**

Test three tiny Python child commands:

1. child prints `BEFORE`, sleeps briefly, prints the exact prompt without newline, reads stdin, exits 0 only on `yes`; assert transcript contains prompt and helper succeeds;
2. child exits 3 before prompt; assert helper exits 3 and never writes approval;
3. child prints prompt, reads yes, then prints `restore plan changed after approval; inspect the new plan and approve again` and exits 1; assert helper exits 1 and transcript preserves message without retry.

- [ ] **Step 2: Implement prompt-bound approval using `subprocess.Popen` and `selectors`**

Read stdout/stderr incrementally as bytes so the no-newline prompt is detectable. On first prompt match:

```python
proc.stdin.write(b"yes\n")
proc.stdin.flush()
approved = True
```

If EOF occurs before prompt, return child exit code or 1. Never send input before the prompt. Never send a second approval.

- [ ] **Step 3: Run approval helper tests**

```bash
python3 -m unittest discover -s test/reconstruction/tests -p 'test_approve_restore.py' -v
```

Expected: PASS.

- [ ] **Step 4: Use the helper around remote normal Restore**

From the host, run the SSH command through `approve_restore.py` without allocating a pseudo-TTY and without `--yes`/`--json`:

```text
ssh ... spike@127.0.0.1 omarchy-blueprint --profile /home/spike/omarchy-profile --machine target restore
```

Do not pass any one-run Restore intent override here. The reviewed CLI invocation consumes the `target` machine's previously persisted Safe + Additive defaults; its plan/transcript must remain consistent with the preflight inspection.

Save transcript to `artifacts/target/restore-transcript.txt`.

- [ ] **Step 5: Fingerprint mutation-sensitive target paths before approval**

Immediately before invoking approval, save `stat`/hash/existence for:

- mapped helper destination;
- unmapped helper destination;
- Restore-disabled Resource destination;
- Config file;
- Hook path.

This report is diagnostic evidence and later verification input.

- [ ] **Step 6: Commit**

```bash
git add test/reconstruction/scenario/approve_restore.py \
        test/reconstruction/tests/test_approve_restore.py \
        test/reconstruction/scenario/restore-target.sh
git commit -m "test: exercise real restore approval boundary"
```

### Task 14: Require Restore apply, journal evidence, and Blueprint verification

**Files:**
- Modify: `test/reconstruction/scenario/restore-target.sh`
- Modify: `test/reconstruction/run.sh`

**Interfaces:**
- Successful Restore must leave transcript + journal copy under `artifacts/target/`.

- [ ] **Step 1: Fail the real scenario if Restore exits non-zero**

Map any non-zero exit after approval to:

- `RESTORE_APPROVAL` if prompt was not observed or changed-after-approval appears;
- `RESTORE_APPLY` for execution failure;
- `BLUEPRINT_VERIFY` when output/journal shows execution completed but verification failed.

Do not rerun Restore to classify it.

- [ ] **Step 2: Locate and copy the real Restore journal**

Use the path emitted by the successful CLI transcript (`Restore verified. Journal: ...`) as the primary source. Assert the path is under the target user's Blueprint state directory, copy it to `artifacts/target/restore-journal.jsonl`, and require a `VERIFY_COMPLETED` event with `ok=true`.

- [ ] **Step 3: Preserve plan/verification intent evidence**

The preflight dry-run JSON plus successful normal Restore transcript/journal are the acceptance evidence. Do not call internal workflow APIs.

- [ ] **Step 4: Run a real hosted target Restore**

Expected: no `--yes`, prompt observed, approval sent once, no changed-after-approval, Restore returns 0, journal contains verification completion.

- [ ] **Step 5: Commit**

```bash
git add test/reconstruction/scenario/restore-target.sh test/reconstruction/run.sh
git commit -m "test: require verified restore execution"
```

### Task 15: Independently grade Machine B and prove final convergence

**Files:**
- Create: `test/reconstruction/verify/verify-target.sh`
- Modify: `test/reconstruction/run.sh`
- Modify: `test/reconstruction/lib/contract.py`
- Modify: `test/reconstruction/tests/test_contract.py`

**Interfaces:**
- `verify-target.sh` returns non-zero on any native assertion failure and writes one PASS line per canonical target.
- Final Blueprint no-actionable-work proof uses a second structured Restore dry-run after independent verification.

- [ ] **Step 1: Add exact native assertions**

Machine B must satisfy:

```bash
pacman -Q alacritty
test "$(cat "$HOME/.local/state/omarchy/current/theme.name")" = catppuccin
test "$(omarchy default terminal)" = alacritty
```

Plugin:

```bash
omarchy plugin validate "$HOME/.config/omarchy/plugins/blueprint.ra.fixture"
omarchy plugin list --json > /tmp/plugins.json
python3 - <<'PY'
import json
items = json.load(open('/tmp/plugins.json'))
assert any(item['id'] == 'blueprint.ra.fixture' and not item['firstParty'] for item in items)
PY
```

Shell:

```python
import json, pathlib
p = pathlib.Path.home()/'.config/omarchy/shell.json'
doc = json.loads(p.read_text())
assert doc['version'] == 1
assert doc['idle']['lock'] == 600
assert doc['bar']['position'] == 'bottom'
```

Config:

```bash
printf 'mode = "reconstructed"\n' | cmp - "$HOME/.config/blueprint-ra/config.toml"
```

Hook:

```bash
cmp /tmp/blueprint-ra-fixtures/hooks/blueprint-ra "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra"
test "$(stat -c %a "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra")" = 755
```

Mapped helper:

```bash
cmp /tmp/blueprint-ra-fixtures/scripts/blueprint-ra-helper "$HOME/bin/blueprint-ra-helper"
test "$(stat -c %a "$HOME/bin/blueprint-ra-helper")" = 755
test ! -e "$HOME/.local/bin/blueprint-ra-helper"
```

Git Resource:

```bash
test "$(git -C "$HOME/Projects/blueprint-ra-fixture" remote get-url origin)" = 'https://10.0.2.2:9443/blueprint-ra.git'
test "$(git -C "$HOME/Projects/blueprint-ra-fixture" rev-parse HEAD)" = 'e161ca52ec7604e6fb335ef15fc212bba03a2994'
test -z "$(git -C "$HOME/Projects/blueprint-ra-fixture" status --porcelain)"
```

Restore Skip:

```bash
test ! -e "$HOME/.local/share/blueprint-ra/skip.txt"
```

- [ ] **Step 2: Write independent report to artifacts**

One line each:

```text
PASS package:alacritty
PASS theme:catppuccin
PASS plugin:blueprint.ra.fixture
PASS shell:idle-lock-and-bar
PASS config:.config/blueprint-ra/config.toml
PASS hook:post-update.d/blueprint-ra
PASS default:terminal=alacritty
PASS resource:helper-script:mapped
PASS resource:git-fixture
PASS resource:target-only-skip:untouched
```

- [ ] **Step 3: Run Blueprint status for evidence without treating intentional skip as generic failure**

Capture:

```bash
set +e
omarchy-blueprint --profile "$HOME/omarchy-profile" --machine target --json status > /tmp/final-status.json
status_code=$?
set -e
test "$status_code" -eq 0 -o "$status_code" -eq 2
```

Copy output to artifacts.

- [ ] **Step 4: Make final actionability mandatory with a second Restore dry-run**

```bash
omarchy-blueprint --profile "$HOME/omarchy-profile" --machine target --json restore --dry-run > /tmp/final-plan.json
python3 /tmp/blueprint-ra-harness/lib/contract.py assert-final-plan /tmp/final-plan.json
```

Require zero operations and allow only the `target-only-skip` Restore-disabled skip. This proves no actionable canonical Restore work remains without misclassifying the intentional difference.

- [ ] **Step 5: Run real hosted independent verification**

Any failed native assertion maps to `INDEPENDENT_VERIFY` and preserves all prior Restore evidence.

- [ ] **Step 6: Commit**

```bash
git add test/reconstruction/verify/verify-target.sh \
        test/reconstruction/run.sh \
        test/reconstruction/lib/contract.py \
        test/reconstruction/tests/test_contract.py
git commit -m "test: independently verify reconstructed machine"
```

### Task 16: Harden focused diagnostics and timeout behavior

**Files:**
- Modify: `test/reconstruction/lib/common.sh`
- Modify: `test/reconstruction/run.sh`
- Modify: `test/reconstruction/vm/install-omarchy.sh`
- Modify: `test/reconstruction/vm/guest.sh`
- Modify: `test/reconstruction/README.md`

**Interfaces:**
- Artifacts tree is stable:

```text
artifacts/
├── summary.txt
├── host/
├── base/
├── source/
├── profile/
└── target/
```

- [ ] **Step 1: Add phase-specific timeout wrappers**

Use `timeout` only around bounded top-level operations/readiness loops. At minimum support environment-configurable defaults for base install, guest ready, source customization, Capture, Restore, and independent verification. Timeout exits must preserve the active phase name.

- [ ] **Step 2: Add QEMU serial/screenshot diagnostics on VM failures**

Keep serial logs always. On failure, use the QEMU monitor to `screendump` a PPM when a guest process is still alive. Do not make screenshot failure override the primary error.

- [ ] **Step 3: Add host resource measurements at beginning/end and after each guest**

Record `df -h /mnt`, `free -h`, and `qemu-img info`/`du -h` for the active overlay. These remain diagnostic-only unless the explicit early free-space guard fails.

- [ ] **Step 4: Assert artifact hygiene**

Before workflow upload, reject accidental guest private key or `*.qcow2` under `artifacts/`:

```bash
! find "$RA_ARTIFACTS" -type f \( -name 'guest_key' -o -name '*.qcow2' \) -print -quit | grep -q .
```

- [ ] **Step 5: Update README with local/CI debugging contract**

Document:

- hosted KVM is required for the full run;
- pure Python tests and shell syntax checks can run locally without VM storage;
- semantic phases never auto-retry;
- how to identify the first failure from `summary.txt`;
- exact canonical fixture values;
- no VM disks are uploaded by default.

- [ ] **Step 6: Run pure tests + shell syntax**

```bash
python3 -m unittest discover -s test/reconstruction/tests -v
find test/reconstruction -name '*.sh' -print0 | xargs -0 -n1 bash -n
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add test/reconstruction
git commit -m "test: harden reconstruction failure diagnostics"
```

### Task 17: Promote the workflow to the required production Reconstruction Assurance check

**Files:**
- Modify: `.github/workflows/reconstruction-assurance.yml`
- Modify: `test/reconstruction/README.md`

**Interfaces:**
- Workflow name: `Reconstruction Assurance`.
- Job name: `Reconstruction Assurance`.
- Triggers: `pull_request`, `workflow_dispatch`.

- [ ] **Step 1: Rename workflow/job to stable required context**

Final YAML header:

```yaml
name: Reconstruction Assurance

on:
  pull_request:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  reconstruction:
    name: Reconstruction Assurance
    runs-on: ubuntu-24.04
    timeout-minutes: 60
```

Keep the workflow thin: checkout/setup, install host packages, build exact Blueprint binary, run pure helper tests, run `test/reconstruction/run.sh`, append `artifacts/summary.txt` to `$GITHUB_STEP_SUMMARY`, upload focused artifacts on failure.

- [ ] **Step 2: Ensure the exact PR binary is the binary both guests receive**

Workflow:

```bash
go build -trimpath -o /tmp/omarchy-blueprint-ra ./cmd/omarchy-blueprint
sha256sum /tmp/omarchy-blueprint-ra | tee /tmp/omarchy-blueprint-ra.sha256
```

`run.sh` copies that exact file to source and target and independently records its hash in each guest report; both must match the host hash.

- [ ] **Step 3: Run full exact-head verification**

On the PR head, require all of:

```text
existing CI / Go 1.25.x: green
existing CI / Go 1.26.x: green
Reconstruction Assurance: green
```

For the RA run inspect job `head_sha` and confirm it equals the current PR head before review/merge readiness.

- [ ] **Step 4: Re-run Reconstruction Assurance once on the same exact head**

Because this becomes required immediately, require two green full runs on the final implementation head before merge to catch obvious harness nondeterminism during initial rollout. This is a one-time implementation acceptance criterion, not an automatic production retry policy.

- [ ] **Step 5: Configure repository merge rules to require `Reconstruction Assurance` after PR 3 merges**

This is a repository setting, not code. The maintainer/authorized worker must add the stable status context `Reconstruction Assurance` to the required checks for the default branch/ruleset. Do not mark the workflow advisory or use `continue-on-error` instead.

If the implementation worker lacks administration permission, report this as the only remaining release action with the exact status context; do not claim v1 fully deployed until the required-check setting is confirmed.

- [ ] **Step 6: Verify the next trivial PR is blocked when RA is absent/red and allowed when green**

Use a documentation-only test PR or the next normal PR; do not intentionally break product code on `main`. Confirm the repository UI treats `Reconstruction Assurance` as required.

- [ ] **Step 7: Final documentation verification**

Confirm the approved ADR/design still match the implemented architecture. Update only operational details (for example the finalized workflow/check name or local commands) if needed; semantic changes require explicit design review.

- [ ] **Step 8: Commit**

```bash
git add .github/workflows/reconstruction-assurance.yml test/reconstruction/README.md
git commit -m "ci: require reconstruction assurance on pull requests"
```

**PR 3 acceptance:** two full Reconstruction Assurance runs are green on the exact final head; both existing Go matrix jobs are green; source/target identities and binary hashes are distinct/equal where expected; profile digest is preserved; normal Restore prompt/recalculation path succeeds; native independent verification succeeds; final dry-run has zero actionable operations and only the intentional Restore-disabled skip; required status context is configured after merge.

---

## Whole-Tranche Verification Checklist

Before declaring Reconstruction Assurance v1 complete:

- [ ] Approved ADR/spec/plan are present at the canonical repository paths.
- [ ] `.github/workflows/ci.yml` still runs `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint` for Go 1.25.x and 1.26.x.
- [ ] Production workflow is `Reconstruction Assurance` on `ubuntu-24.04` with `contents: read` only.
- [ ] Official Omarchy 4.0.4 ISO URL and SHA-256 are pinned in one shared harness location; no `latest` URL is used.
- [ ] Unattended cidata contains no explicit `kernels` override.
- [ ] Base install validates real systemd, pacman, Git, Omarchy, theme/plugin query readiness, and matching kernel headers.
- [ ] Base disk/NVRAM template is immutable during source/target scenarios.
- [ ] Source and target overlays never run concurrently.
- [ ] Source and target OS machine IDs differ.
- [ ] Exact PR Blueprint binary hash is identical in host/source/target evidence.
- [ ] Source preconditions prove canonical state was not already present before customization.
- [ ] Source post-customization assertions pass before Capture.
- [ ] Capture runs through shipped CLI and Blueprint check passes.
- [ ] Only profile archive + digest crosses from source to target.
- [ ] Source VM is destroyed before target VM is created/restored.
- [ ] Target preflight proves canonical source changes did not leak.
- [ ] Selecting machine `target` changes local binding without changing profile digest.
- [ ] Dry-run contract proves all required providers/effects are represented and rejects destructive/interactive/unmapped behavior.
- [ ] Real Restore is run without `--yes` and approval is sent only after exact prompt text.
- [ ] Changed-after-approval is not retried.
- [ ] Successful mutation produces a journal with `VERIFY_COMPLETED ok=true`.
- [ ] Native independent verification passes every canonical target.
- [ ] Restore-disabled Resource remains absent/untouched.
- [ ] Final Restore dry-run contains no operations and only the intentional `target-only-skip` skip.
- [ ] Failures preserve first meaningful phase and focused artifacts; qcow2/private keys are not uploaded.
- [ ] Pure harness unit tests pass with Python stdlib only.
- [ ] All shell files pass `bash -n`.
- [ ] `go test ./...`, `go vet ./...`, and `go build ./cmd/omarchy-blueprint` pass.
- [ ] Two full RA runs pass on final exact head before first merge as a required production gate.
- [ ] Repository merge rules require status context `Reconstruction Assurance`.

## Post-v1 Follow-ons — Explicitly Not Part of This Plan

Do not fold these into PRs 1-3:

1. Safe conflict-preservation VM scenario.
2. Exact desired-absence VM scenario.
3. Force replacement VM scenario.
4. Capture Preserve second-Capture scenario.
5. Dirty Git Resource reconstruction scenario.
6. Restore failure/rollback injection.
7. Secret rejection acceptance.
8. Multiple personas.
9. Profile/Omarchy migration fixtures.
10. Services v1 acceptance.
11. Sync Engine / Auto Sync multi-machine acceptance.
12. Persistent base image cache or self-hosted runner optimization.

The canonical v1 gate should remain deliberately boring: Capture on a real machine, destroy it, Restore on a fresh real machine, independently prove selected intent.
