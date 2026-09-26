#!/usr/bin/env bash
# Sourced by soak.sh only: a diagnostic, never a gate. It
# reboots one guest repeatedly, alternating warm reboots with cold power
# cycles, and classifies each boot so the occasional stall can be diagnosed
# from evidence: "ok", "slow" (SSH answered, health late) or "hung" (SSH never
# answered; serial tail and screenshot kept, then the guest is cold-started).

# Adds serial-console kernel and systemd output to the pinned install's boot
# entries through Omarchy's own Limine configuration, before overlays exist.
# Note: this rebuilds the unified kernel image, so a debug soak does not boot
# the installer-built image; compare it with a non-debug soak.
ra_base_enable_boot_debug() {
  ra_ssh_sudo "printf '%s\n' 'KERNEL_CMDLINE[default]+=\" console=tty0 console=ttyS0,115200 loglevel=7 systemd.show_status=1\"' >> /etc/default/limine && limine-update" \
    > "$RA_ARTIFACTS/base/boot-debug.log" 2>&1
}

ra_soak_health() {
  local role=$1 end=$((SECONDS + RA_REBOOT_DEADLINE))
  while (( SECONDS < end )); do
    ra_guest_health > "$RA_ARTIFACTS/$role/soak-health.log" 2>&1 && return 0
    sleep 3
  done
  return 1
}

ra_soak_record() {
  printf '%s %s %s %ss\n' "$(date -u +%H:%M:%S)" "$1" "$2" "$3" | tee -a "$RA_ARTIFACTS/soak.txt" >&2
}

ra_soak_recover() {
  local role=$1
  ra_guest_timeout_diagnostics "$role"
  ra_kill_if_running "$RA_GUEST_PID"
  wait "$RA_GUEST_PID" 2>/dev/null || true
  RA_ACTIVE_GUEST="" RA_GUEST_PID=""
  ra_guest_start "$role" "blueprint-ra-$role" || return 1
}

ra_boot_soak() {
  local role=source cycles=$1 i kind started result
  : > "$RA_ARTIFACTS/soak.txt"
  # Evidence of which boot configuration the soak measured.
  ra_guest_exec "$role" 'cat /proc/cmdline' > "$RA_ARTIFACTS/soak-cmdline.txt" 2>&1 || true
  ra_record "boot soak debug console: $2"
  for (( i=1; i<=cycles; i++ )); do
    kind=warm
    (( i % 2 == 0 )) && kind=cold
    started=$SECONDS
    if [[ $kind == warm ]]; then
      if ! ra_guest_reboot "$role"; then
        ra_soak_record "$i" "warm-hung" "$((SECONDS - started))"
        ra_soak_recover "$role" || ra_fail INFRASTRUCTURE "could not recover the soak guest"
        continue
      fi
    else
      ra_guest_stop "$role" || ra_fail INFRASTRUCTURE "soak guest did not power off"
      if ! ra_guest_start "$role" "blueprint-ra-$role"; then
        ra_soak_record "$i" "cold-hung" "$((SECONDS - started))"
        ra_soak_recover "$role" || ra_fail INFRASTRUCTURE "could not recover the soak guest"
        continue
      fi
    fi
    result=ok
    ra_soak_health "$role" || result=slow
    ra_soak_record "$i" "$kind-$result" "$((SECONDS - started))"
  done
  ra_record "boot soak: $(awk '{print $3}' "$RA_ARTIFACTS/soak.txt" | sort | uniq -c | awk '{printf "%s=%s ", $2, $1}')"
}
