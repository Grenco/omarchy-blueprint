#!/usr/bin/env bash
# Sourced by run.sh: identical test inputs for either guest. Nothing here is
# source state; the target receives fixtures from the repository, not Machine A.

# The ISO only configures autologin for encrypted targets; this unattended
# install is unencrypted, so write the same drop-in before the identity reboot.
ra_guest_enable_session() {
  ra_ssh_sudo "printf '[Autologin]\\nUser=%s\\nSession=omarchy.desktop\\n' '$RA_USER' > /etc/sddm.conf.d/autologin.conf" \
    > "$RA_ARTIFACTS/$1/session.log" 2>&1
}

ra_guest_wait_session() {
  local role=$1 i
  for (( i=0; i<180; i+=3 )); do
    if ra_guest_exec "$role" 'source /usr/share/omarchy/default/bash/env-bootstrap
        while IFS= read -r a; do export "$a"; done < <(systemctl --user show-environment)
        omarchy-shell shell ping' >> "$RA_ARTIFACTS/$role/session.log" 2>&1; then
      return 0
    fi
    sleep 3
  done
  ra_note "$role Omarchy desktop session did not become ready; see $role/session.log"
  return 1
}

ra_guest_stage() {
  local role=$1
  ra_guest_wait_session "$role" || return 1
  tar -C "$RA_ROOT" --transform 's,^fixtures/,,;s,^scenario/,,' -cf - \
    fixtures scenario/guest-env.sh scenario/customize-source.sh |
    ra_guest_exec "$role" 'rm -rf /tmp/blueprint-ra-fixtures && mkdir /tmp/blueprint-ra-fixtures &&
      tar -C /tmp/blueprint-ra-fixtures -xf -' || return 1
  # Trust the per-run fixture Git certificate for that remote only, outside $HOME.
  ra_guest_copy_to "$role" "$RA_WORK/git-tls/cert.pem" /tmp/blueprint-ra-git.pem || return 1
  ra_guest_copy_to "$role" "$RA_BLUEPRINT_BIN" /tmp/omarchy-blueprint || return 1
  ra_ssh_sudo "install -D -m 0644 /tmp/blueprint-ra-git.pem /etc/blueprint-ra/git-fixture.pem &&
    git config --system http.https://10.0.2.2:9443/.sslCAInfo /etc/blueprint-ra/git-fixture.pem &&
    install -m 0755 /tmp/omarchy-blueprint /usr/local/bin/omarchy-blueprint" \
    > "$RA_ARTIFACTS/$role/stage.log" 2>&1 || return 1
  ra_guest_exec "$role" 'omarchy-blueprint --help >/dev/null' || return 1
  ra_guest_refresh_package_databases "$role"
}

# The installed base ships without pacman sync databases. Refresh them the same
# way on both guests (network transfer, so retryable); packages are untouched.
ra_guest_refresh_package_databases() {
  local role=$1 attempt
  for attempt in 1 2 3; do
    ra_ssh_sudo 'pacman -Sy --noconfirm' >> "$RA_ARTIFACTS/$role/pacman-sync.log" 2>&1 && return 0
    ra_note "$role pacman database refresh attempt $attempt failed"
    sleep 10
  done
  return 1
}

# Runs the exact PR build of Blueprint inside the guest's session environment.
ra_blueprint() {
  local role=$1 quoted
  shift
  printf -v quoted '%q ' "$@"
  ra_guest_exec "$role" "source /tmp/blueprint-ra-fixtures/guest-env.sh && omarchy-blueprint $quoted"
}

ra_customize_source() {
  local log="$RA_ARTIFACTS/source/customize.log" status=0 reason
  # Prints the fixture account password for `sudo -S omarchy pkg add`; removed afterwards.
  printf '#!/bin/sh\nprintf "%%s\\n" %q\n' "$RA_USER" |
    ra_guest_exec source 'umask 077 && cat > /tmp/blueprint-ra-askpass && chmod 0700 /tmp/blueprint-ra-askpass'
  ra_guest_exec source 'bash /tmp/blueprint-ra-fixtures/customize-source.sh' > "$log" 2>&1 || status=$?
  ra_guest_exec source 'rm -f /tmp/blueprint-ra-askpass' || status=1
  grep '^PASS ' "$log" > "$RA_ARTIFACTS/source/pre-capture.txt" || true
  if (( status != 0 )); then
    reason=$(grep '^FAIL ' "$log" | tail -1 || true)
    ra_fail SOURCE_CUSTOMIZATION "${reason:-customization exited $status} (see source/customize.log)"
  fi
}
