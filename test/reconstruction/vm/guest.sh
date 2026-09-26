#!/usr/bin/env bash
# Sourced by run.sh. The base is read-only; every guest gets its own overlay.

RA_ACTIVE_GUEST=""
RA_GUEST_PID=""

ra_guest_create() {
  local role=$1
  [[ $role == source || $role == target ]] || { ra_note "unknown guest role: $role"; return 1; }
  [[ -z $RA_ACTIVE_GUEST ]] || { ra_note "guest $RA_ACTIVE_GUEST is still active"; return 1; }
  mkdir -p "$RA_WORK/guests" "$RA_ARTIFACTS/$role"
  qemu-img create -f qcow2 -F qcow2 -b "$RA_WORK/base/omarchy-base.qcow2" \
    "$RA_WORK/guests/$role.qcow2"
  cp "$RA_WORK/base/OVMF_VARS.base.fd" "$RA_WORK/guests/$role.vars.fd"
  chmod 0644 "$RA_WORK/guests/$role.vars.fd"
}

# Keep the serial tail and a screenshot when a guest misses a readiness window.
ra_guest_timeout_diagnostics() {
  local role=$1 stamp
  stamp=$(date -u +%H%M%S)
  tail -c 4000 "$RA_ARTIFACTS/$role/serial.log" > "$RA_ARTIFACTS/$role/boot-timeout-$stamp-serial-tail.txt" 2>/dev/null || true
  ra_screendump "$RA_WORK/guests/$role.monitor.sock" "$RA_ARTIFACTS/$role/boot-timeout-$stamp-screen.ppm"
}

ra_guest_start() {
  local role=$1 hostname=$2
  [[ -z $RA_ACTIVE_GUEST ]] || { ra_note "guest $RA_ACTIVE_GUEST is still active"; return 1; }
  [[ -f $RA_WORK/guests/$role.qcow2 ]] || { ra_note "overlay missing: $role"; return 1; }
  [[ -f $RA_WORK/guests/$role.vars.fd ]] || { ra_note "NVRAM missing: $role"; return 1; }
  qemu-system-x86_64 -enable-kvm -machine q35 -cpu host -smp 4 -m 8192 \
    -display none -vga std -serial "file:$RA_ARTIFACTS/$role/serial.log" -monitor "unix:$RA_WORK/guests/$role.monitor.sock,server,nowait" \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file="$RA_WORK/guests/$role.vars.fd" \
    -drive file="$RA_WORK/guests/$role.qcow2",format=qcow2,if=none,id=disk \
    -device virtio-blk-pci,drive=disk,bootindex=1 \
    -netdev "user,id=n0,hostfwd=tcp:127.0.0.1:${RA_SSH_PORT}-:22" -device virtio-net-pci,netdev=n0 &
  RA_GUEST_PID=$!
  RA_ACTIVE_GUEST=$role
  ra_cleanup_add ra_kill_if_running "$RA_GUEST_PID"
  ra_boot_mark "$role" "qemu started"
  if ! ra_wait_ready "$RA_GUEST_PID" "$RA_ARTIFACTS/$role/guest-checks.log" 120; then
    ra_boot_mark "$role" "readiness timed out"
    ra_guest_timeout_diagnostics "$role"
    return 1
  fi
  ra_boot_mark "$role" "ready"
  ra_note "booted $role overlay for $hostname"
}

ra_guest_exec() {
  local role=$1
  shift
  [[ $role == "$RA_ACTIVE_GUEST" ]] || { ra_note "guest $role is not active"; return 1; }
  ra_ssh "$@"
}

ra_guest_copy_to() {
  local role=$1 local_path=$2 remote_path=$3
  [[ $role == "$RA_ACTIVE_GUEST" ]] || return 1
  scp -q -P "$RA_SSH_PORT" -i "$RA_WORK/control_key" -o BatchMode=yes \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    "$local_path" "$RA_USER@127.0.0.1:$remote_path"
}

ra_guest_copy_from() {
  local role=$1 remote_path=$2 local_path=$3
  [[ $role == "$RA_ACTIVE_GUEST" ]] || return 1
  scp -q -P "$RA_SSH_PORT" -i "$RA_WORK/control_key" -o BatchMode=yes \
    -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    "$RA_USER@127.0.0.1:$remote_path" "$local_path"
}

ra_guest_freshen_identity() {
  local role=$1 hostname=$2 before after="" prior_id current_id i
  before=$(ra_guest_exec "$role" 'cat /proc/sys/kernel/random/boot_id')
  prior_id=$(ra_guest_exec "$role" 'cat /etc/machine-id')
  ra_boot_mark "$role" "identity reboot requested"
  ra_ssh_sudo "hostnamectl hostname '$hostname' && rm -f /etc/machine-id /var/lib/dbus/machine-id && systemd-machine-id-setup && ln -sf /etc/machine-id /var/lib/dbus/machine-id && systemctl reboot" \
    > "$RA_ARTIFACTS/$role/identity-reboot.log" 2>&1 || true # SSH disconnects when reboot starts.
  for (( i=0; i<120; i+=3 )); do
    if after=$(ra_guest_exec "$role" 'cat /proc/sys/kernel/random/boot_id' 2>/dev/null) &&
       [[ $after != "$before" ]] &&
       ra_guest_health > "$RA_ARTIFACTS/$role/reboot-checks.log" 2>&1; then
      break
    fi
    if ! kill -0 "$RA_GUEST_PID" 2>/dev/null; then
      ra_note "$role QEMU exited during identity reboot"
      return 1
    fi
    sleep 3
  done
  if [[ $after != "$before" ]]; then
    ra_boot_mark "$role" "identity reboot ready"
  else
    ra_boot_mark "$role" "identity reboot timed out"
    ra_guest_timeout_diagnostics "$role"
    ra_note "$role never rebooted"
    return 1
  fi
  ra_guest_health > "$RA_ARTIFACTS/$role/reboot-checks.log" 2>&1 || return 1
  [[ $(ra_guest_exec "$role" hostname) == "$hostname" ]] || { ra_note "$role hostname did not change"; return 1; }
  ra_guest_exec "$role" 'cat /etc/machine-id' > "$RA_ARTIFACTS/$role/machine-id.txt"
  [[ -s $RA_ARTIFACTS/$role/machine-id.txt ]] || { ra_note "$role has no machine ID"; return 1; }
  current_id=$(<"$RA_ARTIFACTS/$role/machine-id.txt")
  [[ $current_id != "$prior_id" ]] || { ra_note "$role retained pristine base machine ID"; return 1; }
  ra_note "$role identity: $current_id (fresh boot $after)"
}

# Reboot the active guest and wait for SSH on a new boot. Readiness of the
# desktop session is the caller's (ra_guest_wait_session).
ra_guest_reboot() {
  local role=$1 before after="" i
  before=$(ra_guest_exec "$role" 'cat /proc/sys/kernel/random/boot_id') || return 1
  ra_boot_mark "$role" "reboot requested"
  ra_ssh_sudo 'systemctl reboot' >> "$RA_ARTIFACTS/$role/reboot.log" 2>&1 || true # SSH drops as it reboots.
  for (( i=0; i<120; i+=3 )); do
    if after=$(ra_guest_exec "$role" 'cat /proc/sys/kernel/random/boot_id' 2>/dev/null) && [[ $after != "$before" ]]; then
      ra_boot_mark "$role" "reboot ready"
      return 0
    fi
    if ! kill -0 "$RA_GUEST_PID" 2>/dev/null; then
      ra_note "$role QEMU exited during reboot"
      return 1
    fi
    sleep 3
  done
  ra_boot_mark "$role" "reboot timed out"
  ra_guest_timeout_diagnostics "$role"
  ra_note "$role did not come back from reboot within 120s"
  return 1
}

ra_guest_stop() {
  local role=$1
  [[ $role == "$RA_ACTIVE_GUEST" ]] || { ra_note "guest $role is not active"; return 1; }
  ra_measure_host
  ra_ssh_sudo 'systemctl poweroff' > "$RA_ARTIFACTS/$role/shutdown.log" 2>&1 || true
  ra_wait_exit "$RA_GUEST_PID" 90 || { ra_note "$role did not power off; see shutdown.log"; return 1; }
  qemu-img info "$RA_WORK/guests/$role.qcow2" > "$RA_ARTIFACTS/$role/disk.txt"
  du -B1 "$RA_WORK/guests/$role.qcow2" >> "$RA_ARTIFACTS/$role/disk.txt"
  free -h >> "$RA_ARTIFACTS/host/runner.txt"
  df -h /mnt >> "$RA_ARTIFACTS/host/runner.txt"
  RA_ACTIVE_GUEST=""
  RA_GUEST_PID=""
}
