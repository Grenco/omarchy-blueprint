#!/usr/bin/env bash
# Sourced by run.sh: owns only pristine Omarchy installation and base validation.

ra_base_screen() {
  ra_screendump "$RA_WORK/base/monitor.sock" "$RA_ARTIFACTS/base/guest-screen.ppm"
}

# The installed base's first disk boot: records its kernel command line and
# the installer custom command's log; an image with console arguments must
# already have booted with them (canonical: the normalized CI console).
ra_base_after_first_boot() {
  local boot_image=$1 consoles cmdline reason
  consoles=$(ra_boot_console_args "$boot_image")
  cmdline=$(ra_ssh 'cat /proc/cmdline') || ra_fail INFRASTRUCTURE "could not read the base first-boot kernel command line"
  printf '%s\n' "$cmdline" > "$RA_ARTIFACTS/base/first-boot-cmdline.txt"
  ra_record "base first disk boot cmdline: $cmdline"
  if declare -F ra_base_record_boot_image >/dev/null; then
    ra_base_record_boot_image first-boot
  fi
  [[ -n $consoles ]] || return 0
  ra_ssh_sudo "cat $RA_BOOT_CONSOLE_LOG" > "$RA_ARTIFACTS/base/boot-console.log" 2>&1 ||
    ra_fail INFRASTRUCTURE "the installer console custom command left no log (see base/boot-console.log)"
  if [[ $boot_image == canonical ]]; then
    reason=$(ra_check_canonical_cmdline "$cmdline") ||
      ra_fail INFRASTRUCTURE "base first disk boot outside the normalized CI console: $reason (see base/first-boot-cmdline.txt)"
  elif [[ " $cmdline " != *" $consoles "* || " $cmdline " == *" console=uart"* ]]; then
    ra_fail INFRASTRUCTURE "base first disk boot lacks $consoles or has console=uart (see base/first-boot-cmdline.txt)"
  fi
}

# boot_image: "canonical" (default; the installer's own post-install custom
# command ends every Limine entry with console=tty0, so the first disk boot is
# already normalized). Diagnostic soaks only: "installer" (untouched control,
# no custom command), "ttys0-console" (same install-time rule, explicit serial
# kernel console), "rebuilt" (limine-update) or "debug" (serial boot output).
ra_install_base() {
  local work=$1 boot_image pid start free_now
  boot_image=$(ra_resolve_boot_image "${2:-}") || ra_fail OMARCHY_INSTALL "unknown boot image: ${2:-}"
  mkdir -p "$work/base" "$work/cidata" "$RA_ARTIFACTS/base"
  start=$(date +%s)
  printf '%s\n%s\n' "$OMARCHY_ISO_URL" "$OMARCHY_ISO_SHA256" > "$RA_ARTIFACTS/base/iso.txt"
  curl -fL --retry 3 --retry-all-errors -o "$work/omarchy.iso" "$OMARCHY_ISO_URL"
  printf '%s  %s\n' "$OMARCHY_ISO_SHA256" "$work/omarchy.iso" | sha256sum -c -

  ssh-keygen -q -t ed25519 -N '' -f "$work/control_key"
  ra_write_cidata "$work/cidata" "$work/control_key.pub" "$boot_image" ||
    ra_fail OMARCHY_INSTALL "could not write the installer inputs"
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
    -netdev "user,id=n0,hostfwd=tcp:127.0.0.1:${RA_SSH_PORT}-:22" -device virtio-net-pci,netdev=n0 &
  pid=$!
  ra_cleanup_add ra_kill_if_running "$pid"
  if ! ra_wait_ready "$pid" "$RA_ARTIFACTS/base/guest-checks.log" 480; then
    ra_base_screen
    ra_fail OMARCHY_INSTALL "official Omarchy installer did not boot a healthy disk guest"
  fi
  ra_ssh 'source /usr/share/omarchy/default/bash/env-bootstrap; findmnt -no SOURCE /; uname -r; kernel=$(cat "/usr/lib/modules/$(uname -r)/pkgbase"); pacman -Q "$kernel" "$kernel-headers"; systemd-analyze; omarchy theme current; command -v omarchy-shell' \
    >> "$RA_ARTIFACTS/base/guest-checks.log" 2>&1
  ra_note "Omarchy installed and ready in $(($(date +%s)-start))s"
  ra_base_after_first_boot "$boot_image"
  if [[ $boot_image == debug ]]; then
    ra_base_enable_boot_debug || ra_fail OMARCHY_INSTALL "could not enable boot debugging (see base/boot-debug.log)"
  elif [[ $boot_image == rebuilt ]]; then
    ra_ssh_sudo 'limine-update' > "$RA_ARTIFACTS/base/boot-rebuild.log" 2>&1 ||
      ra_fail OMARCHY_INSTALL "could not rebuild the boot image (see base/boot-rebuild.log)"
  fi
  if [[ $boot_image == debug || $boot_image == rebuilt ]] && declare -F ra_base_record_boot_image >/dev/null; then
    ra_base_record_boot_image "$boot_image"
  fi
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
