#!/usr/bin/env bash
# Sourced by run.sh: owns only pristine Omarchy installation and base validation.

# The base builder missed its readiness window: screenshot, VM and vCPU
# state, QEMU activity and, when the image has a serial device, its tail.
# The canonical base builder deliberately has none; that is recorded, not
# treated as another failure.
ra_base_timeout_diagnostics() {
  local boot_image=$1 pid=$2 a="$RA_ARTIFACTS/base"
  ra_screendump "$RA_WORK/base/monitor.sock" "$a/guest-screen.ppm"
  ra_monitor "$RA_WORK/base/monitor.sock" "$a/boot-timeout-monitor.txt" "info status" "info cpus" "info blockstats"
  ra_qemu_activity "$pid" 10 > "$a/boot-timeout-qemu.txt" 2>&1 || true
  if ra_base_serial_args "$boot_image" x | grep -qx none; then
    echo "no serial device: the canonical base builder runs without one" > "$a/boot-timeout-serial-tail.txt"
  else
    tail -c 4000 "$a/omarchy-serial.log" > "$a/boot-timeout-serial-tail.txt" 2>/dev/null || true
  fi
}

# The installed base's first disk boot (the installer reboots into it inside
# the base builder): records its kernel command line; the canonical base
# must have no serial kernel console. Explicit console=tty0 is not expected
# yet: the durable Limine setting is installed right after this boot.
ra_base_first_disk_boot() {
  local boot_image=$1 cmdline reason
  cmdline=$(ra_ssh 'cat /proc/cmdline') || ra_fail INFRASTRUCTURE "could not read the base first-disk-boot kernel command line"
  printf '%s\n' "$cmdline" > "$RA_ARTIFACTS/base/first-disk-boot-cmdline.txt"
  ra_record "base first disk boot ($boot_image, $(ra_base_serial_args "$boot_image" omarchy-serial.log | paste -sd' ')): $cmdline"
  [[ $boot_image == canonical ]] || return 0
  reason=$(ra_check_no_serial_console "$cmdline") ||
    ra_fail INFRASTRUCTURE "base first disk boot has a serial kernel console: $reason (see base/first-disk-boot-cmdline.txt)"
}

# boot_image: "canonical" (default; built without a serial device, then
# console=tty0 through Omarchy's limine-entry-tool, see
# RA_CANONICAL_BOOT_IMAGE). Diagnostic soaks only, all built with the
# original serial device: "installer" (untouched control), "ttys0-console"
# (explicit serial kernel console), "rebuilt" (limine-update) or "debug"
# (serial boot output).
ra_install_base() {
  local work=$1 boot_image pid start free_now consoles serial
  boot_image=$(ra_resolve_boot_image "${2:-}") || ra_fail OMARCHY_INSTALL "unknown boot image: ${2:-}"
  consoles=$(ra_boot_console_args "$boot_image")
  mapfile -t serial < <(ra_base_serial_args "$boot_image" "$RA_ARTIFACTS/base/omarchy-serial.log")
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
    -display none -vga std "${serial[@]}" \
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
    ra_base_timeout_diagnostics "$boot_image" "$pid"
    ra_fail OMARCHY_INSTALL "official Omarchy installer did not boot a healthy disk guest"
  fi
  ra_ssh 'source /usr/share/omarchy/default/bash/env-bootstrap; findmnt -no SOURCE /; uname -r; kernel=$(cat "/usr/lib/modules/$(uname -r)/pkgbase"); pacman -Q "$kernel" "$kernel-headers"; systemd-analyze; omarchy theme current; command -v omarchy-shell' \
    >> "$RA_ARTIFACTS/base/guest-checks.log" 2>&1
  ra_note "Omarchy installed and ready in $(($(date +%s)-start))s"
  ra_base_first_disk_boot "$boot_image"
  if declare -F ra_base_record_boot_image >/dev/null; then
    ra_base_record_boot_image installer
  fi
  if [[ $boot_image != installer ]]; then
    if [[ -n $consoles ]]; then
      ra_ssh_sudo "$(ra_boot_console_normalize_command "$consoles")" > "$RA_ARTIFACTS/base/boot-console.log" 2>&1 ||
        ra_fail OMARCHY_INSTALL "could not install the durable boot console (see base/boot-console.log)"
    elif [[ $boot_image == debug ]]; then
      ra_base_enable_boot_debug || ra_fail OMARCHY_INSTALL "could not enable boot debugging (see base/boot-debug.log)"
    else
      ra_ssh_sudo 'limine-update' > "$RA_ARTIFACTS/base/boot-rebuild.log" 2>&1 ||
        ra_fail OMARCHY_INSTALL "could not rebuild the boot image (see base/boot-rebuild.log)"
    fi
    if declare -F ra_base_record_boot_image >/dev/null; then
      ra_base_record_boot_image "$boot_image"
    fi
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
