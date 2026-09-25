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

RA_PHASES=(INFRASTRUCTURE OMARCHY_INSTALL SOURCE_CUSTOMIZATION CAPTURE PROFILE_HANDOFF
  TARGET_PREFLIGHT RESTORE_PLAN RESTORE_APPROVAL RESTORE_APPLY BLUEPRINT_VERIFY INDEPENDENT_VERIFY)
declare -A RA_PHASE_STATUS=()
RA_CURRENT_PHASE=""
RA_FIRST_FAILURE_PHASE=""
RA_FIRST_FAILURE_MESSAGE=""
RA_CLEANUP_CMDS=()

ra_note() { printf '[reconstruction] %s\n' "$*" >&2; }

ra_phase() {
  RA_CURRENT_PHASE=$1
  ra_note "starting $1"
}

ra_pass() {
  RA_PHASE_STATUS["$1"]=PASS
  ra_note "$1 PASS"
}

ra_fail() {
  local phase=$1 message=$2
  RA_PHASE_STATUS["$phase"]=FAIL
  if [[ -z $RA_FIRST_FAILURE_PHASE ]]; then
    RA_FIRST_FAILURE_PHASE=$phase
    RA_FIRST_FAILURE_MESSAGE=$message
  fi
  ra_note "$phase: $message"
  return 1
}

ra_cleanup_add() {
  local command
  printf -v command '%q ' "$@"
  RA_CLEANUP_CMDS+=("$command")
}

ra_finish() {
  local status=${1:-0} i phase
  trap - EXIT
  set +e
  if (( status != 0 )) && [[ -z $RA_FIRST_FAILURE_PHASE ]]; then
    ra_fail "${RA_CURRENT_PHASE:-INFRASTRUCTURE}" "command exited with status $status" || true
  fi
  mkdir -p "$RA_ARTIFACTS"
  {
    printf 'Reconstruction Assurance\n'
    for phase in "${RA_PHASES[@]}"; do
      printf '%s %s\n' "$phase" "${RA_PHASE_STATUS[$phase]:-NOT RUN}"
    done
    if [[ -n $RA_FIRST_FAILURE_PHASE ]]; then
      printf 'phase: %s\nmessage: %s\n' "$RA_FIRST_FAILURE_PHASE" "$RA_FIRST_FAILURE_MESSAGE"
    fi
  } > "$RA_ARTIFACTS/summary.txt"
  for (( i=${#RA_CLEANUP_CMDS[@]}-1; i>=0; i-- )); do
    eval "${RA_CLEANUP_CMDS[i]}" || ra_note "cleanup warning: ${RA_CLEANUP_CMDS[i]}"
  done
  if [[ -n $RA_FIRST_FAILURE_PHASE ]]; then
    ra_note "first failure: $RA_FIRST_FAILURE_PHASE: $RA_FIRST_FAILURE_MESSAGE"
    exit 1
  fi
  exit 0
}
