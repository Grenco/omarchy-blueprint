#!/usr/bin/env bash
# Sourced by run.sh: plan, approve and apply the canonical Restore on the
# ready Machine B exactly as a user would, from a fresh post-readiness plan.

ra_restore_plan() {
  local a="$RA_ARTIFACTS/target" p="$RA_GUEST_PROFILE" log="$RA_ARTIFACTS/target/blueprint.log"
  # Canonical intent is the machine's persisted Safe + Additive defaults, never a one-run override.
  ra_blueprint target --profile "$p" --json machine restore-defaults target > "$a/restore-defaults-final.json" 2>> "$log" &&
    ra_contract restore-defaults "$a/restore-defaults-final.json" target safe additive 2>> "$log" ||
    ra_fail RESTORE_PLAN "target Restore defaults are not Safe + Additive (see target/restore-defaults-final.json)"
  ra_blueprint target --profile "$p" --machine target --json restore --dry-run > "$a/restore-plan.json" 2>> "$log" ||
    ra_fail RESTORE_PLAN "Restore planning failed on ready Machine B (see target/blueprint.log)"
  python3 "$RA_ROOT/lib/contract.py" assert-canonical-plan "$a/restore-plan.json" 2>> "$log" ||
    ra_fail RESTORE_PLAN "canonical Restore plan rejected: $(tail -1 "$log")"
}

# Mutation-sensitive paths, recorded before approval as diagnostic evidence.
ra_fingerprint_target() {
  ra_guest_exec target 'for path in "$HOME/bin/blueprint-ra-helper" "$HOME/.local/bin/blueprint-ra-helper" \
      "$HOME/.local/share/blueprint-ra/skip.txt" "$HOME/.config/blueprint-ra/config.toml" \
      "$HOME/.config/omarchy/hooks/post-update.d/blueprint-ra"; do
      if [[ -e $path ]]; then stat -c "%n %a %s" "$path"; sha256sum "$path"; else echo "$path absent"; fi
    done' > "$RA_ARTIFACTS/target/pre-restore-fingerprint.txt" 2>&1 || true
}

ra_apply_approved_restore() {
  local a="$RA_ARTIFACTS/target" transcript="$RA_ARTIFACTS/target/restore-transcript.txt" status=0 journal
  ra_phase RESTORE_APPROVAL
  ra_fingerprint_target
  # A real terminal: yes once at Blueprint's prompt, and only sudo's own prompt answered.
  ra_drive_terminal --transcript "$transcript" --approve "Apply this restore? [y/N] " \
      --sudo-password-file "$(ra_sudo_password_file)" --max-sudo 3 --prompt-timeout 900 --exit-timeout 3600 -- \
      ssh -tt "${RA_SSH_OPTS[@]}" "$RA_USER@127.0.0.1" \
      "source /tmp/blueprint-ra-fixtures/guest-env.sh && omarchy-blueprint --profile $RA_GUEST_PROFILE --machine target restore" \
      2> "$a/restore-driver.err" || status=$?
  if ! grep -qF 'Apply this restore? [y/N] ' "$transcript" 2>/dev/null; then
    ra_fail RESTORE_APPROVAL "Restore approval prompt never appeared; not retried (see target/restore-transcript.txt)"
  fi
  if grep -q 'changed after approval' "$transcript"; then
    ra_fail RESTORE_APPROVAL "Blueprint rejected the plan after approval; not retried (see target/restore-transcript.txt)"
  fi
  ra_pass RESTORE_APPROVAL
  ra_phase RESTORE_APPLY
  if (( status != 0 )); then
    if grep -q 'verification failed' "$transcript"; then
      ra_pass RESTORE_APPLY
      ra_phase BLUEPRINT_VERIFY
      ra_fail BLUEPRINT_VERIFY "Restore applied but Blueprint verification failed (see target/restore-transcript.txt)"
    fi
    ra_fail RESTORE_APPLY "Restore failed after approval; not retried (see target/restore-transcript.txt)"
  fi
  ra_pass RESTORE_APPLY
  ra_phase BLUEPRINT_VERIFY
  journal=$(sed -n 's/.*Restore verified\. Journal: \([^[:space:]]*\).*/\1/p' "$transcript" | tail -1)
  [[ $journal == "/home/$RA_USER/.local/state/omarchy-blueprint/restores/"* ]] ||
    ra_fail BLUEPRINT_VERIFY "Restore did not report a journal in Blueprint's state directory: '${journal}'"
  ra_guest_copy_from target "$journal" "$a/restore-journal.jsonl" ||
    ra_fail BLUEPRINT_VERIFY "could not copy the Restore journal $journal"
  grep -q '"VERIFY_COMPLETED".*ok=true' "$a/restore-journal.jsonl" ||
    ra_fail BLUEPRINT_VERIFY "Restore journal has no successful VERIFY_COMPLETED event"
  ra_pass BLUEPRINT_VERIFY
}

ra_verify_target() {
  local a="$RA_ARTIFACTS/target" p="$RA_GUEST_PROFILE" log="$RA_ARTIFACTS/target/blueprint.log" code=0 reason
  if ! ra_guest_exec target 'bash /tmp/blueprint-ra-fixtures/verify-target.sh' > "$a/independent.txt" 2>&1; then
    reason=$(grep '^FAIL ' "$a/independent.txt" | head -1 || true)
    ra_fail INDEPENDENT_VERIFY "native assertion failed: ${reason:-verify-target.sh failed} (see target/independent.txt)"
  fi
  # Evidence only: the intentional Restore Skip may legitimately count as a difference.
  ra_blueprint target --profile "$p" --machine target --json status > "$a/final-status.json" 2>> "$log" || code=$?
  (( code == 0 || code == 2 )) || ra_fail INDEPENDENT_VERIFY "Blueprint status failed with exit $code"
  ra_blueprint target --profile "$p" --machine target --json restore --dry-run > "$a/final-plan.json" 2>> "$log" &&
    python3 "$RA_ROOT/lib/contract.py" assert-final-plan "$a/final-plan.json" 2>> "$log" ||
    ra_fail INDEPENDENT_VERIFY "actionable Restore work remains after Restore: $(tail -1 "$log")"
}
