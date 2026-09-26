#!/usr/bin/env bash
set -euo pipefail

RA_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RA_WORK=${RA_WORK:-/mnt/omarchy-blueprint-ra}
RA_ARTIFACTS=${RA_ARTIFACTS:-$RA_WORK/artifacts}
RA_USER=${RA_USER:-spike}
RA_SSH_PORT=${RA_SSH_PORT:-2222}
RA_BLUEPRINT_BIN=${RA_BLUEPRINT_BIN:-/tmp/omarchy-blueprint-ra}
RA_GUEST_PROFILE=/home/$RA_USER/omarchy-profile
# Measured (run 36232762318): with autologin, a guest can take ~200s after a
# reboot before it passes health, and its sshd can miss a 2s banner window
# while the desktop session and first-login provisioning load it. Deadlines
# are wall-clock and bounded; nothing semantic is retried.
RA_BOOT_DEADLINE=${RA_BOOT_DEADLINE:-240}
RA_REBOOT_DEADLINE=${RA_REBOOT_DEADLINE:-300}
RA_MIN_FREE_BYTES=${RA_MIN_FREE_BYTES:-19327352832} # 18 GiB
OMARCHY_ISO_URL=${OMARCHY_ISO_URL:-https://iso.omarchy.org/omarchy-4.0.4.iso}
OMARCHY_ISO_SHA256=${OMARCHY_ISO_SHA256:-ddeded2758c48318d201dfdac905ecb28f570441883f0c052ea3cd5d05acf92d}

RA_PHASES=(INFRASTRUCTURE OMARCHY_INSTALL SOURCE_READINESS SOURCE_CUSTOMIZATION CAPTURE PROFILE_HANDOFF
  TARGET_PREFLIGHT TARGET_READINESS RESTORE_PLAN RESTORE_APPROVAL RESTORE_APPLY BLUEPRINT_VERIFY INDEPENDENT_VERIFY)
declare -A RA_PHASE_STATUS=()
RA_CURRENT_PHASE=""
RA_FIRST_FAILURE_PHASE=""
RA_FIRST_FAILURE_MESSAGE=""
RA_CLEANUP_CMDS=()
RA_FACTS=()
RA_MIN_OBSERVED_DISK=-1
RA_MIN_OBSERVED_MEMORY=-1

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

# A run fact worth reading in the summary, such as a ready Omarchy version.
ra_record() {
  RA_FACTS+=("$1")
  ra_note "$1"
}

# Timestamped boot milestones, so slow boots can be told apart from hangs.
ra_boot_mark() {
  mkdir -p "$RA_ARTIFACTS/$1"
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)" "$2" >> "$RA_ARTIFACTS/$1/boot-timings.txt"
}

# Best effort: a screenshot through the QEMU monitor never replaces the real error.
ra_screendump() {
  python3 - "$1" "$2" <<'PY' || true
import socket
import sys
import time

with socket.socket(socket.AF_UNIX) as monitor:
    monitor.settimeout(5)
    monitor.connect(sys.argv[1])
    monitor.recv(4096)
    monitor.sendall(f"screendump {sys.argv[2]}\n".encode())
    time.sleep(2)
    monitor.recv(4096)
PY
}

ra_cleanup_add() {
  local command
  printf -v command '%q ' "$@"
  RA_CLEANUP_CMDS+=("$command")
}

ra_kill_if_running() {
  if kill -0 "$1" 2>/dev/null; then
    kill "$1"
  fi
}

RA_SSH_OPTS=(-p "$RA_SSH_PORT" -i "$RA_WORK/control_key" -o BatchMode=yes
  -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -o LogLevel=ERROR)

ra_ssh() {
  ssh "${RA_SSH_OPTS[@]}" "$RA_USER@127.0.0.1" "$@"
}

ra_ssh_sudo() {
  local quoted
  printf -v quoted '%q' "$1"
  # The ephemeral unattended user has the known fixture password used in cidata.
  printf '%s\n' "$RA_USER" | ra_ssh "sudo -S -p '' bash -c $quoted"
}

ra_measure_host() {
  local disk memory
  disk=$(df -B1 --output=avail /mnt | tail -1 | tr -d ' ')
  memory=$(free -b | awk '$1 == "Mem:" {print $7}')
  if (( RA_MIN_OBSERVED_DISK < 0 || disk < RA_MIN_OBSERVED_DISK )); then
    RA_MIN_OBSERVED_DISK=$disk
  fi
  if (( RA_MIN_OBSERVED_MEMORY < 0 || memory < RA_MIN_OBSERVED_MEMORY )); then
    RA_MIN_OBSERVED_MEMORY=$memory
  fi
}

ra_guest_health() {
  ra_ssh 'set -e
    source /usr/share/omarchy/default/bash/env-bootstrap
    test "$(cat /proc/1/comm)" = systemd
    test ! -e /run/archiso/bootmnt
    findmnt -no SOURCE / | grep -q "^/dev/vda"
    command -v pacman && command -v git && command -v omarchy
    omarchy commands --json >/dev/null
    omarchy theme current >/dev/null
    command -v omarchy-shell
    test -d "$HOME"
    kernel=$(cat "/usr/lib/modules/$(uname -r)/pkgbase")
    pacman -Q "$kernel" "$kernel-headers"
    test "$(cat "/usr/lib/modules/$(uname -r)/build/include/config/kernel.release")" = "$(uname -r)"
    uname -r'
}

ra_wait_ready() {
  local pid=$1 log=$2 deadline=$3 end=$((SECONDS + $3))
  while (( SECONDS < end )); do
    ra_measure_host
    if ra_guest_health > "$log" 2>&1; then
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      ra_note "QEMU exited before guest readiness"
      return 1
    fi
    sleep 3
  done
  ra_note "guest readiness timed out after ${deadline}s; last SSH attempt: $log"
  return 1
}

ra_wait_exit() {
  local pid=$1 deadline=$2 i
  for (( i=0; i<deadline; i++ )); do
    if ! kill -0 "$pid" 2>/dev/null; then
      wait "$pid"
      return $?
    fi
    sleep 1
  done
  return 1
}

ra_finish() {
  local status=${1:-0} i phase
  trap - EXIT
  set +e
  if (( status != 0 )) && [[ -z $RA_FIRST_FAILURE_PHASE ]]; then
    ra_fail "${RA_CURRENT_PHASE:-INFRASTRUCTURE}" "command exited with status $status" || true
  fi
  mkdir -p "$RA_ARTIFACTS/host"
  {
    printf 'Reconstruction Assurance\n'
    for phase in "${RA_PHASES[@]}"; do
      printf '%s %s\n' "$phase" "${RA_PHASE_STATUS[$phase]:-NOT RUN}"
    done
    for phase in "${RA_FACTS[@]}"; do
      printf '%s\n' "$phase"
    done
    if [[ -n $RA_FIRST_FAILURE_PHASE ]]; then
      printf 'phase: %s\nmessage: %s\n' "$RA_FIRST_FAILURE_PHASE" "$RA_FIRST_FAILURE_MESSAGE"
    fi
  } > "$RA_ARTIFACTS/summary.txt"
  printf 'minimum_host_free_bytes=%s\nminimum_host_available_memory_bytes=%s\n' \
    "$RA_MIN_OBSERVED_DISK" "$RA_MIN_OBSERVED_MEMORY" > "$RA_ARTIFACTS/host/metrics.txt"
  while IFS= read -r phase; do ra_note "$phase"; done < "$RA_ARTIFACTS/summary.txt"
  while IFS= read -r phase; do ra_note "$phase"; done < "$RA_ARTIFACTS/host/metrics.txt"
  for (( i=${#RA_CLEANUP_CMDS[@]}-1; i>=0; i-- )); do
    eval "${RA_CLEANUP_CMDS[i]}" || ra_note "cleanup warning: ${RA_CLEANUP_CMDS[i]}"
  done
  if [[ -n $RA_FIRST_FAILURE_PHASE ]]; then
    ra_note "first failure: $RA_FIRST_FAILURE_PHASE: $RA_FIRST_FAILURE_MESSAGE"
    exit 1
  fi
  exit 0
}
