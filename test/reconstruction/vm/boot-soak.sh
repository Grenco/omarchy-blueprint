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

# Identity and initramfs contents of the unified kernel images, so boot
# image variants can be compared: hash, lsinitcpio analysis, module list.
ra_base_record_boot_image() {
  ra_ssh_sudo 'echo "== sources of console=uart"; grep -rIl "console=uart" /boot /efi /etc 2>/dev/null
    echo "== /boot files"; find /boot -xdev -type f 2>/dev/null | sort
    echo "== EFI loader and stub variables"
    for v in /sys/firmware/efi/efivars/Loader* /sys/firmware/efi/efivars/Stub*; do
      [ -e "$v" ] && printf "%s: %s\n" "${v##*/}" "$(tail -c +5 "$v" | tr -d "\0" | tr -c "[:print:]" " ")"
    done
    echo "== /boot/limine.conf"; cat /boot/limine.conf
    for efi in /boot/EFI/Linux/*.efi; do
      echo "== $efi"; sha256sum "$efi"
      objcopy -O binary --only-section=.cmdline "$efi" /tmp/ra-cmdline && echo "cmdline: $(tr -d "\0" < /tmp/ra-cmdline)"
      objcopy -O binary --only-section=.initrd "$efi" /tmp/ra-initrd || continue
      lsinitcpio -a /tmp/ra-initrd; echo "-- modules"; lsinitcpio -l /tmp/ra-initrd | grep -E "\.ko" | sort
    done' > "$RA_ARTIFACTS/base/boot-image-$1.txt" 2>&1 || true
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
  ra_record "boot soak boot image: $2"
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

# Stops a stuck guest without restarting it, keeping the diagnostics first.
ra_soak_discard() {
  local role=$1
  ra_guest_timeout_diagnostics "$role"
  ra_kill_if_running "$RA_GUEST_PID"
  wait "$RA_GUEST_PID" 2>/dev/null || true
  RA_ACTIVE_GUEST="" RA_GUEST_PID=""
}

# Reproduces the conditions of every observed stall: the first boots of a
# fresh overlay. Each trial creates a new overlay, cold-boots it, enables the
# autologin session and then reboots, alternating two arms:
#   identity: the production identity reboot (hostname + new machine ID)
#   plain:    a plain systemctl reboot, isolating the identity change
ra_fresh_overlay_trials() {
  local role=source trials=$1 i arm started result
  : > "$RA_ARTIFACTS/soak.txt"
  ra_record "boot soak experiment: fresh-overlay ($trials trials, boot image: $2)"
  for (( i=1; i<=trials; i++ )); do
    arm=identity
    (( i % 2 == 0 )) && arm=plain
    rm -f "$RA_WORK/guests/$role.qcow2" "$RA_WORK/guests/$role.vars.fd"
    ra_guest_create "$role" || ra_fail INFRASTRUCTURE "could not create trial overlay"
    ra_boot_mark "$role" "trial $i ($arm)"
    started=$SECONDS
    if ! ra_guest_start "$role" "blueprint-ra-$role"; then
      ra_soak_record "$i" "first-boot-hung" "$((SECONDS - started))"
      ra_soak_discard "$role"
      continue
    fi
    ra_soak_record "$i" "first-boot-ok" "$((SECONDS - started))"
    if [[ ! -s $RA_ARTIFACTS/soak-cmdline.txt ]]; then
      # Evidence of the boot configuration measured; a debug soak must really have it.
      ra_guest_exec "$role" 'cat /proc/cmdline' > "$RA_ARTIFACTS/soak-cmdline.txt" 2>&1 || true
      # Later parameters win, so the image's own quiet/loglevel=0 would silence debug output.
      if [[ $2 == debug ]] && { ! grep -q 'console=ttyS0' "$RA_ARTIFACTS/soak-cmdline.txt" ||
           grep -qE '(^| )(quiet|loglevel=0)( |$)' "$RA_ARTIFACTS/soak-cmdline.txt"; }; then
        ra_fail INFRASTRUCTURE "debug console output is not effective on the kernel command line (see soak-cmdline.txt)"
      fi
      # A console variant must really have changed what the kernel booted with.
      if [[ $2 == no-uart-console || $2 == ttys0-console ]] && grep -q 'console=uart' "$RA_ARTIFACTS/soak-cmdline.txt"; then
        ra_fail INFRASTRUCTURE "console=uart is still on the kernel command line (see soak-cmdline.txt)"
      fi
      if [[ $2 == ttys0-console ]] && ! grep -q 'console=ttyS0,115200' "$RA_ARTIFACTS/soak-cmdline.txt"; then
        ra_fail INFRASTRUCTURE "console=ttyS0 did not reach the kernel command line (see soak-cmdline.txt)"
      fi
    fi
    ra_guest_enable_session "$role" || ra_fail INFRASTRUCTURE "could not enable the trial session"
    started=$SECONDS
    result=ok
    if [[ $arm == identity ]]; then
      if ! ra_guest_freshen_identity "$role" "blueprint-ra-$role-$i"; then
        result=hung
        tail -1 "$RA_ARTIFACTS/$role/boot-timings.txt" | grep -q 'ssh answered: yes' && result=slow
      fi
    elif ! ra_guest_reboot "$role"; then
      result=hung
    else
      ra_soak_health "$role" || result=slow
    fi
    ra_soak_record "$i" "$arm-reboot-$result" "$((SECONDS - started))"
    if [[ $result == ok ]]; then
      ra_guest_stop "$role" || ra_soak_discard "$role"
    else
      ra_soak_discard "$role"
    fi
  done
  ra_record "boot soak: $(awk '{print $3}' "$RA_ARTIFACTS/soak.txt" | sort | uniq -c | awk '{printf "%s=%s ", $2, $1}')"
}
