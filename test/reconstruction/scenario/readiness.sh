#!/usr/bin/env bash
# Sourced by run.sh: a guest becomes package-ready only the supported way,
# Omarchy's own update, and records the runtime Omarchy resolves (ADR 0022).
# Machine A and Machine B must reach the same runtime; nothing is retried.

ra_sudo_password_file() {
  local file="$RA_WORK/sudo-password"
  [[ -f $file ]] || (umask 077 && printf '%s\n' "$RA_USER" > "$file")
  printf '%s' "$file"
}

# A real terminal session on the host, driven by drive_terminal.py.
ra_drive_terminal() {
  python3 "$RA_ROOT/scenario/drive_terminal.py" "$@"
}

ra_omarchy_ready() {
  local role=$1 phase=$2 a="$RA_ARTIFACTS/$1" synced version
  # The fixture user answers sudo's own prompts; -y skips Omarchy's confirmations, never sudo.
  ra_drive_terminal --transcript "$a/omarchy-update.txt" --sudo-password-file "$(ra_sudo_password_file)" \
      --max-sudo 10 --prompt-timeout 900 --exit-timeout 3600 -- \
      ssh -tt "${RA_SSH_OPTS[@]}" "$RA_USER@127.0.0.1" \
      "source /tmp/blueprint-ra-fixtures/guest-env.sh && omarchy update -y" 2> "$a/omarchy-update.err" ||
    ra_fail "$phase" "omarchy update failed on $role; not retried (see $role/omarchy-update.txt)"
  ra_guest_reboot "$role" || ra_fail "$phase" "$role did not come back after omarchy update"
  ra_guest_wait_session "$role" || ra_fail "$phase" "$role desktop session did not settle after omarchy update"
  synced=$(ra_guest_exec "$role" 'for repo in $(pacman-conf --repo-list); do
      [[ -e /var/lib/pacman/sync/$repo.db ]] || printf "%s " "$repo"; done') ||
    ra_fail "$phase" "could not inspect $role pacman sync databases"
  [[ -z $synced ]] || ra_fail "$phase" "$role still has no sync database for: ${synced% } after omarchy update"
  version=$(ra_guest_exec "$role" 'source /usr/share/omarchy/default/bash/env-bootstrap && omarchy version') ||
    ra_fail "$phase" "could not read $role Omarchy version"
  [[ -n $version ]] || ra_fail "$phase" "$role reported no Omarchy version"
  printf '%s\n' "$version" > "$a/ready-omarchy-version.txt"
  ra_guest_exec "$role" 'pacman -Q' > "$a/ready-packages.txt" 2>&1 || true
  ra_record "$role ready Omarchy: $version"
}

# Capture and Restore must run on the same Omarchy (v1 is not migration).
ra_require_same_ready_runtime() {
  local source target
  source=$(<"$RA_ARTIFACTS/source/ready-omarchy-version.txt")
  target=$(<"$RA_ARTIFACTS/target/ready-omarchy-version.txt")
  diff "$RA_ARTIFACTS/source/ready-packages.txt" "$RA_ARTIFACTS/target/ready-packages.txt" \
    > "$RA_ARTIFACTS/target/ready-packages.diff" 2>&1 || true
  [[ $source == "$target" ]] ||
    ra_fail TARGET_READINESS "Machine B reached Omarchy $target but Machine A captured on $source; not retried"
}
