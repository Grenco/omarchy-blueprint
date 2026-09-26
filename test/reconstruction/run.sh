#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib/common.sh"
trap 'ra_finish "$?"' EXIT

mkdir -p "$RA_ARTIFACTS"/{host,base,source,profile,target}

ra_phase INFRASTRUCTURE
ra_note "workspace: $RA_WORK"
{
  uname -a
  lscpu
  free -h
  df -h / /mnt
  ls -l /dev/kvm || true
  qemu-system-x86_64 --version || true
} > "$RA_ARTIFACTS/host/runner.txt" 2>&1
free_bytes=$(df -B1 --output=avail /mnt | tail -1 | tr -d ' ')
if (( free_bytes < RA_MIN_FREE_BYTES )); then
  ra_fail INFRASTRUCTURE "need at least $RA_MIN_FREE_BYTES free bytes on /mnt; have $free_bytes"
fi
[[ -e /dev/kvm ]] || ra_fail INFRASTRUCTURE "/dev/kvm missing"
if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
  sudo chgrp "$(id -gn)" /dev/kvm
fi
[[ -r /dev/kvm && -w /dev/kvm ]] || ra_fail INFRASTRUCTURE "/dev/kvm unusable"
command -v qemu-system-x86_64 >/dev/null || ra_fail INFRASTRUCTURE "QEMU missing"
[[ -x $RA_BLUEPRINT_BIN ]] || ra_fail INFRASTRUCTURE "Blueprint build missing: $RA_BLUEPRINT_BIN"
source "$RA_ROOT/scenario/build-fixtures.sh"
ra_build_git_fixture || ra_fail INFRASTRUCTURE "could not build the pinned Git fixture"
ra_start_git_server || ra_fail INFRASTRUCTURE "fixture Git server did not start"
ra_pass INFRASTRUCTURE

source "$RA_ROOT/vm/install-omarchy.sh"
source "$RA_ROOT/vm/boot-soak.sh"
ra_phase OMARCHY_INSTALL
ra_install_base "$RA_WORK"

source "$RA_ROOT/vm/guest.sh"
source "$RA_ROOT/scenario/stage-guest.sh"
source "$RA_ROOT/scenario/capture-source.sh"
source "$RA_ROOT/scenario/preflight-target.sh"
source "$RA_ROOT/scenario/readiness.sh"
source "$RA_ROOT/scenario/restore-target.sh"
ra_guest_create source
ra_guest_start source blueprint-ra-source
ra_guest_enable_session source
ra_guest_freshen_identity source blueprint-ra-source
ra_pass OMARCHY_INSTALL

# Diagnostic only: repeated reboots to classify boot stalls; no product phases.
if (( ${RA_BOOT_SOAK:-0} > 0 )); then
  ra_boot_soak "$RA_BOOT_SOAK"
  ra_guest_stop source || true
  exit 0
fi

ra_phase SOURCE_READINESS
ra_guest_stage source || ra_fail SOURCE_READINESS "could not stage test inputs on Machine A"
ra_omarchy_ready source SOURCE_READINESS
ra_pass SOURCE_READINESS

ra_phase SOURCE_CUSTOMIZATION
ra_customize_source
ra_pass SOURCE_CUSTOMIZATION

ra_phase CAPTURE
ra_capture_source
ra_pass CAPTURE

ra_phase PROFILE_HANDOFF
ra_handoff_profile
ra_pass PROFILE_HANDOFF

ra_phase TARGET_PREFLIGHT
ra_boot_target
ra_assert_target_pristine
ra_import_target_profile
ra_pass TARGET_PREFLIGHT

# The pre-readiness dry-run only proved the demand; it is never approved.
ra_phase TARGET_READINESS
ra_omarchy_ready target TARGET_READINESS
ra_require_same_ready_runtime
ra_assert_target_pristine TARGET_READINESS ready
ra_pass TARGET_READINESS

# A completely new plan: only this one is previewed, approved and applied.
ra_phase RESTORE_PLAN
ra_restore_plan
ra_pass RESTORE_PLAN

ra_apply_approved_restore

ra_phase INDEPENDENT_VERIFY
ra_verify_target
ra_pass INDEPENDENT_VERIFY

ra_guest_stop target || ra_fail INDEPENDENT_VERIFY "Machine B did not shut down"
