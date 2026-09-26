#!/usr/bin/env bash
# Diagnostic only, never a gate: install the pinned base, then reboot one
# guest repeatedly to characterize boot stalls (vm/boot-soak.sh). It runs no
# Capture or Restore, so it has its own workflow and check name.
# Usage: soak.sh <cycles> [debug]
set -euo pipefail
RA_TITLE="Reconstruction Boot Soak (diagnostic, not reconstruction)"
source "$(dirname "$0")/lib/common.sh"
trap 'ra_finish "$?"' EXIT
cycles=${1:?usage: soak.sh <cycles> [debug]}
boot_debug=${2:-}
[[ $cycles =~ ^[1-9][0-9]*$ ]] || { ra_note "cycles must be a positive integer"; exit 2; }

mkdir -p "$RA_ARTIFACTS"/{host,base,source}
ra_phase INFRASTRUCTURE
ra_check_host
ra_pass INFRASTRUCTURE

source "$RA_ROOT/vm/install-omarchy.sh"
source "$RA_ROOT/vm/guest.sh"
source "$RA_ROOT/vm/boot-soak.sh"
source "$RA_ROOT/scenario/stage-guest.sh"
ra_phase OMARCHY_INSTALL
ra_install_base "$RA_WORK" "$boot_debug"
# The same guest setup as the canonical run, including the autologin session.
ra_guest_create source
ra_guest_start source blueprint-ra-source
ra_guest_enable_session source
ra_guest_freshen_identity source blueprint-ra-source
ra_pass OMARCHY_INSTALL

ra_boot_soak "$cycles" "${boot_debug:-none}"
ra_guest_stop source || true
