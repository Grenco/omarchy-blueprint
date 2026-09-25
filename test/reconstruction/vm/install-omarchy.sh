#!/usr/bin/env bash
# Sourced by run.sh: owns only pristine Omarchy installation and base validation.

ra_base_screen() {
  python3 - "$RA_WORK/base/monitor.sock" "$RA_ARTIFACTS/base/guest-screen.ppm" <<'PY' || true
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

ra_install_base() {
  local work=$1 pid start free_now
  mkdir -p "$work/base" "$work/cidata" "$RA_ARTIFACTS/base"
  start=$(date +%s)
  printf '%s\n%s\n' "$OMARCHY_ISO_URL" "$OMARCHY_ISO_SHA256" > "$RA_ARTIFACTS/base/iso.txt"
  curl -fL --retry 3 --retry-all-errors -o "$work/omarchy.iso" "$OMARCHY_ISO_URL"
  printf '%s  %s\n' "$OMARCHY_ISO_SHA256" "$work/omarchy.iso" | sha256sum -c -

  ssh-keygen -q -t ed25519 -N '' -f "$work/control_key"
  python3 "$RA_ROOT/vm/make-cidata.py" --output-dir "$work/cidata" \
    --ssh-public-key "$work/control_key.pub"
  genisoimage -quiet -output "$work/cidata.iso" -volid cidata -joliet -rock \
    "$work/cidata/user_configuration.json" "$work/cidata/user_credentials.json" \
    "$work/cidata/user_encrypt_installation.txt" "$work/cidata/authorized_keys"

  qemu-img create -f qcow2 "$work/base/omarchy-base.qcow2" 40G
  cp /usr/share/OVMF/OVMF_VARS_4M.fd "$work/base/vars.fd"
  qemu-system-x86_64 -enable-kvm -machine q35 -cpu host -smp 4 -m 8192 \
    -display none -vga std -serial "file:$RA_ARTIFACTS/base/omarchy-serial.log" \
    -monitor "unix:$work/base/monitor.sock,server,nowait" \
    -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
    -drive if=pflash,format=raw,file="$work/base/vars.fd" \
    -drive file="$work/base/omarchy-base.qcow2",format=qcow2,if=none,id=disk \
    -device virtio-blk-pci,drive=disk,bootindex=1 \
    -drive file="$work/omarchy.iso",format=raw,media=cdrom,if=none,id=installer \
    -device ide-cd,drive=installer,bootindex=2 \
    -drive file="$work/cidata.iso",format=raw,media=cdrom,if=none,id=seed \
    -device ide-cd,drive=seed,bus=ide.1 \
    -netdev "user,id=n0,hostfwd=tcp::${RA_SSH_PORT}-:22" -device virtio-net-pci,netdev=n0 &
  pid=$!
  ra_cleanup_add ra_kill_if_running "$pid"
  if ! ra_wait_ready "$pid" "$RA_ARTIFACTS/base/guest-checks.log" 480; then
    ra_base_screen
    ra_fail OMARCHY_INSTALL "official Omarchy installer did not boot a healthy disk guest"
  fi
  ra_ssh 'source /usr/share/omarchy/default/bash/env-bootstrap; findmnt -no SOURCE /; uname -r; kernel=$(cat "/usr/lib/modules/$(uname -r)/pkgbase"); pacman -Q "$kernel" "$kernel-headers"; systemd-analyze; omarchy theme current; command -v omarchy-shell' \
    >> "$RA_ARTIFACTS/base/guest-checks.log" 2>&1
  ra_note "Omarchy installed and ready in $(($(date +%s)-start))s"
  ra_ssh_sudo 'systemctl poweroff' > "$RA_ARTIFACTS/base/shutdown.log" 2>&1 || true
  ra_wait_exit "$pid" 90 || ra_fail OMARCHY_INSTALL "installed base did not power off; see shutdown.log"
  cp "$work/base/vars.fd" "$work/base/OVMF_VARS.base.fd"
  chmod 0444 "$work/base/omarchy-base.qcow2" "$work/base/OVMF_VARS.base.fd"
  qemu-img info "$work/base/omarchy-base.qcow2" > "$RA_ARTIFACTS/base/disk.txt"
  du -B1 "$work/base/omarchy-base.qcow2" >> "$RA_ARTIFACTS/base/disk.txt"
  free_now=$(df -B1 --output=avail /mnt | tail -1 | tr -d ' ')
  printf 'install_and_boot_seconds=%s\nhost_free_after_base_bytes=%s\n' \
    "$(($(date +%s)-start))" "$free_now" >> "$RA_ARTIFACTS/base/disk.txt"
}
