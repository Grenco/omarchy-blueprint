#!/usr/bin/env bash
# Sourced by run.sh: build the portable profile on Machine A through the public
# CLI, approve one reviewed Capture, and export only the profile archive.

RA_HANDOFF=${RA_HANDOFF:-$RA_WORK/handoff}

# Deterministic tar of a guest's profile; identical bytes mean identical profile.
RA_PROFILE_TAR="tar --sort=name --mtime='UTC 1970-01-01' --owner=0 --group=0 --numeric-owner -C \"\$HOME\""

ra_profile_digest() {
  ra_guest_exec "$1" "$RA_PROFILE_TAR -cf - omarchy-profile | sha256sum | cut -d' ' -f1"
}

ra_contract() {
  python3 "$RA_ROOT/lib/capture_contract.py" "$@"
}

ra_capture_source() {
  local a="$RA_ARTIFACTS/source" p="$RA_GUEST_PROFILE" before after
  local log="$a/blueprint.log"
  ra_blueprint source init "$p" --name reconstruction-assurance >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" machine add source >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" machine add target --no-use >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" --json machine current > "$a/machine-current.json" 2>> "$log" &&
    ra_contract machine-current "$a/machine-current.json" source binding 2>> "$log" ||
    ra_fail CAPTURE "profile and machine overlays were not initialized (see source/blueprint.log)"

  # Explicit Config inclusion avoids depending on unknown-app discovery.
  ra_blueprint source --profile "$p" include "config:$RA_CONFIG_PATH" >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" track "/home/$RA_USER/$RA_HELPER_SOURCE" --id "$RA_HELPER_RESOURCE" --strategy copy >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" track "/home/$RA_USER/$RA_GIT_PATH" --id "$RA_GIT_RESOURCE" --strategy git >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" track "/home/$RA_USER/$RA_SKIP_PATH" --id "$RA_SKIP_RESOURCE" --strategy copy >> "$log" 2>&1 ||
    ra_fail CAPTURE "Config inclusion or Resource tracking failed (see source/blueprint.log)"

  ra_blueprint source --profile "$p" machine use target >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" machine map "resource:$RA_HELPER_RESOURCE" "~/$RA_HELPER_TARGET" >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" machine use source >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" --machine target --json tracked > "$a/target-tracked.json" 2>> "$log" &&
    ra_contract resource-mapping "$a/target-tracked.json" "$RA_HELPER_RESOURCE" \
      "~/$RA_HELPER_SOURCE" "/home/$RA_USER/$RA_HELPER_TARGET" 2>> "$log" ||
    ra_fail CAPTURE "target Resource mapping was not recorded (see source/blueprint.log)"

  ra_blueprint source --profile "$p" policy set restore resources skip "resource:$RA_SKIP_RESOURCE" \
      --scope machine:target >> "$log" 2>&1 &&
    ra_blueprint source --profile "$p" --json policy show resources "resource:$RA_SKIP_RESOURCE" \
      --scope machine:target > "$a/target-policy.json" 2>> "$log" &&
    ra_contract restore-skip "$a/target-policy.json" target resources "resource:$RA_SKIP_RESOURCE" 2>> "$log" ||
    ra_fail CAPTURE "target Restore skip is not an explicit machine rule (see source/blueprint.log)"

  before=$(ra_profile_digest source) &&
    ra_blueprint source --profile "$p" --machine source --json capture --dry-run > "$a/capture-preview.json" 2>> "$log" &&
    after=$(ra_profile_digest source) ||
    ra_fail CAPTURE "Capture preview failed (see source/blueprint.log)"
  [[ $before == "$after" ]] || ra_fail CAPTURE "Capture preview mutated the profile"
  ra_contract capture-preview "$a/capture-preview.json" source 2>> "$log" ||
    ra_fail CAPTURE "Capture preview lacks canonical candidates: $(tail -1 "$log")"

  # The mutating Capture is the normal terminal review, approved once.
  python3 "$RA_ROOT/scenario/approve_capture.py" --transcript "$a/capture-transcript.txt" -- \
    ssh -tt "${RA_SSH_OPTS[@]}" "$RA_USER@127.0.0.1" \
    "source /tmp/blueprint-ra-fixtures/guest-env.sh && omarchy-blueprint --profile $p --machine source capture --review" \
    2>> "$log" || ra_fail CAPTURE "reviewed Capture did not apply; not retried (see source/capture-transcript.txt)"

  ra_blueprint source --profile "$p" --machine source --json check > "$a/check.json" 2>> "$log" &&
    ra_contract check "$a/check.json" source 2>> "$log" ||
    ra_fail CAPTURE "Blueprint check failed after Capture (see source/blueprint.log)"

  ra_guest_exec source "rm -f /tmp/blueprint-ra-profile.tar &&
      $RA_PROFILE_TAR -cf /tmp/blueprint-ra-profile.tar omarchy-profile &&
      cd /tmp && sha256sum blueprint-ra-profile.tar > blueprint-ra-profile.sha256" &&
    rm -rf "$RA_HANDOFF" && mkdir -p "$RA_HANDOFF/inspect" &&
    ra_guest_copy_from source /tmp/blueprint-ra-profile.tar "$RA_HANDOFF/" &&
    ra_guest_copy_from source /tmp/blueprint-ra-profile.sha256 "$RA_HANDOFF/" ||
    ra_fail CAPTURE "profile archive could not be exported"
  cp "$RA_HANDOFF/blueprint-ra-profile.tar" "$RA_HANDOFF/blueprint-ra-profile.sha256" "$RA_ARTIFACTS/profile/"
  tar -C "$RA_HANDOFF/inspect" -xf "$RA_HANDOFF/blueprint-ra-profile.tar" &&
    ra_contract profile "$RA_HANDOFF/inspect/omarchy-profile" 2> "$RA_ARTIFACTS/profile/contract.log" ||
    ra_fail CAPTURE "captured profile lacks canonical state: $(tail -1 "$RA_ARTIFACTS/profile/contract.log")"
}

# Only the verified archive leaves Machine A; then A is destroyed.
ra_handoff_profile() {
  local pid=$RA_GUEST_PID
  (cd "$RA_HANDOFF" && sha256sum -c blueprint-ra-profile.sha256) > "$RA_ARTIFACTS/profile/digest-check.txt" 2>&1 ||
    ra_fail PROFILE_HANDOFF "exported profile archive does not match its digest"
  if tar -tf "$RA_HANDOFF/blueprint-ra-profile.tar" | grep -v '^omarchy-profile/' >&2; then
    ra_fail PROFILE_HANDOFF "profile archive contains paths outside the profile"
  fi
  ra_guest_stop source || ra_fail PROFILE_HANDOFF "Machine A did not shut down"
  ! kill -0 "$pid" 2>/dev/null || ra_fail PROFILE_HANDOFF "Machine A QEMU is still running"
  rm -f "$RA_WORK/guests/source.qcow2" "$RA_WORK/guests/source.vars.fd"
  [[ ! -e $RA_WORK/guests/source.qcow2 ]] || ra_fail PROFILE_HANDOFF "Machine A disk was not destroyed"
  ra_note "Machine A destroyed; handing off $(cut -d' ' -f1 "$RA_HANDOFF/blueprint-ra-profile.sha256")"
}
