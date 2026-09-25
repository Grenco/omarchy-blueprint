#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib/common.sh"
trap 'ra_finish "$?"' EXIT

mkdir -p "$RA_ARTIFACTS"/{host,base,source,profile,target}

# PR 1 stops after substrate/overlay validation; product phases arrive later.
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
ra_pass INFRASTRUCTURE

source "$RA_ROOT/vm/install-omarchy.sh"
ra_phase OMARCHY_INSTALL
ra_install_base "$RA_WORK"
ra_pass OMARCHY_INSTALL
