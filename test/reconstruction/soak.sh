#!/usr/bin/env bash
# Diagnostic only, never a gate: install the pinned base, then reboot one
# guest repeatedly to characterize boot stalls (vm/boot-soak.sh). It runs no
# Capture or Restore, so it has its own workflow and check name.
# Usage: soak.sh <reboot-cycles|fresh-overlay> <count> [debug]
#   reboot-cycles: one guest, alternating warm reboots and cold power cycles
#   fresh-overlay: new overlay per trial; first boot, then identity or plain reboot
set -euo pipefail
RA_TITLE="Reconstruction Boot Soak (diagnostic, not reconstruction)"
source "$(dirname "$0")/lib/common.sh"
trap 'ra_finish "$?"' EXIT
experiment=${1:?usage: soak.sh <reboot-cycles|fresh-overlay> <count> [debug]}
count=${2:?usage: soak.sh <reboot-cycles|fresh-overlay> <count> [debug]}
boot_debug=${3:-}
[[ $experiment == reboot-cycles || $experiment == fresh-overlay ]] || { ra_note "unknown experiment: $experiment"; exit 2; }
[[ $count =~ ^[1-9][0-9]*$ ]] || { ra_note "count must be a positive integer"; exit 2; }

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
ra_pass OMARCHY_INSTALL

if [[ $experiment == fresh-overlay ]]; then
  ra_fresh_overlay_trials "$count" "${boot_debug:-none}"
else
  # The same guest setup as the canonical run, including the autologin session.
  ra_guest_create source
  ra_guest_start source blueprint-ra-source
  ra_guest_enable_session source
  ra_guest_freshen_identity source blueprint-ra-source
  ra_boot_soak "$count" "${boot_debug:-none}"
  ra_guest_stop source || true
fi
