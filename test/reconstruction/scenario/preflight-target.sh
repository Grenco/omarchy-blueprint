#!/usr/bin/env bash
# Sourced by run.sh: prove Machine B is fresh, then import only the verified
# profile archive and bind it to the target machine locally.

ra_boot_target() {
  local source_id target_id
  ra_guest_create target && ra_guest_start target blueprint-ra-target &&
    ra_guest_enable_session target && ra_guest_freshen_identity target blueprint-ra-target ||
    ra_fail TARGET_PREFLIGHT "Machine B did not boot with a fresh identity"
  source_id=$(<"$RA_ARTIFACTS/source/machine-id.txt")
  target_id=$(<"$RA_ARTIFACTS/target/machine-id.txt")
  [[ $source_id != "$target_id" ]] || ra_fail TARGET_PREFLIGHT "source and target share machine identity"
  ra_guest_stage target || ra_fail TARGET_PREFLIGHT "could not stage test inputs on Machine B"
}

# mode "fresh": nothing from Machine A, not even the profile, has arrived.
# mode "ready": after readiness the profile is imported, but no canonical
# customization may exist before Restore.
ra_assert_target_pristine() {
  local phase=${1:-TARGET_PREFLIGHT} mode=${2:-fresh} reason
  local log="$RA_ARTIFACTS/target/absence-$mode.log"
  ra_guest_exec target "bash -s $mode" > "$log" 2>&1 <<'GUEST' || {
source /tmp/blueprint-ra-fixtures/guest-env.sh
set -uo pipefail
mode=$1
status=0
absent() {
  local name=$1
  shift
  if "$@"; then printf 'PASS absent %s\n' "$name"; else printf 'FAIL source state present: %s\n' "$name"; status=1; fi
}
absent "package:$RA_PACKAGE" eval '! pacman -Q "$RA_PACKAGE" >/dev/null 2>&1'
absent "theme:$RA_THEME" test "$(cat "$HOME/.local/state/omarchy/current/theme.name" 2>/dev/null)" != "$RA_THEME"
absent "default:terminal=$RA_DEFAULT_TERMINAL" test "$(omarchy default terminal)" != "$RA_DEFAULT_TERMINAL"
paths=(".config/omarchy/plugins/$RA_PLUGIN_ID" "$RA_CONFIG_PATH" "$RA_HOOK_PATH" "$RA_HELPER_SOURCE"
       "$RA_HELPER_TARGET" "$RA_GIT_PATH" "$RA_SKIP_PATH")
[[ $mode != fresh ]] || paths+=(omarchy-profile .local/state/omarchy-blueprint)
for path in "${paths[@]}"; do
  absent "~/$path" test ! -e "$HOME/$path"
done
shell_doc="$HOME/.config/omarchy/shell.json"
[[ -f $shell_doc ]] || shell_doc="$OMARCHY_PATH/config/omarchy/shell.json"
absent "shell:idle.lock=600,bar.position=bottom" eval \
  '! jq -e ".idle.lock == 600 and .bar.position == \"bottom\"" "$shell_doc" >/dev/null'
exit "$status"
GUEST
    reason=$(grep '^FAIL ' "$log" | head -1 || true)
    ra_fail "$phase" "${reason:-pristine check failed} (see target/absence-$mode.log)"
  }
  grep '^PASS ' "$log" > "$RA_ARTIFACTS/target/absence-$mode.txt"
}

ra_import_target_profile() {
  local a="$RA_ARTIFACTS/target" p="$RA_GUEST_PROFILE" handoff imported bound
  local log="$a/blueprint.log"
  handoff=$(cut -d' ' -f1 "$RA_HANDOFF/blueprint-ra-profile.sha256")
  ra_guest_copy_to target "$RA_HANDOFF/blueprint-ra-profile.tar" /tmp/ &&
    ra_guest_copy_to target "$RA_HANDOFF/blueprint-ra-profile.sha256" /tmp/ ||
    ra_fail TARGET_PREFLIGHT "profile archive could not be copied to Machine B"
  ra_guest_exec target 'cd /tmp && sha256sum -c blueprint-ra-profile.sha256 && tar -C "$HOME" -xf blueprint-ra-profile.tar' \
    > "$a/import.log" 2>&1 || ra_fail TARGET_PREFLIGHT "profile archive failed digest verification on Machine B"

  imported=$(ra_profile_digest target) &&
    ra_blueprint target --profile "$p" machine use target >> "$log" 2>&1 &&
    bound=$(ra_profile_digest target) ||
    ra_fail TARGET_PREFLIGHT "could not bind the target machine (see target/blueprint.log)"
  printf 'handoff=%s\nimported=%s\nbound=%s\n' "$handoff" "$imported" "$bound" > "$a/profile-digests.txt"
  [[ $imported == "$handoff" && $bound == "$handoff" ]] ||
    ra_fail TARGET_PREFLIGHT "profile bytes differ from the handoff archive (see target/profile-digests.txt)"
  ra_blueprint target --profile "$p" --json machine current > "$a/machine-current.json" 2>> "$log" &&
    ra_contract machine-current "$a/machine-current.json" target binding 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "target is not the locally bound machine (see target/blueprint.log)"

  # Canonical Restore intent is persisted machine policy, never a one-run override.
  ra_blueprint target --profile "$p" machine restore-defaults set target --conflicts safe --convergence additive >> "$log" 2>&1 &&
    ra_blueprint target --profile "$p" --json machine restore-defaults target > "$a/restore-defaults.json" 2>> "$log" &&
    ra_contract restore-defaults "$a/restore-defaults.json" target safe additive 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "target Safe + Additive Restore defaults were not persisted (see target/blueprint.log)"

  ra_target_fresh_check
}

# Machine B keeps the package state the official install left: no sync
# database for any configured repository. Blueprint must still check it,
# report the unavailable package origin, and plan a Restore (ADR 0022).
ra_target_fresh_check() {
  local a="$RA_ARTIFACTS/target" synced log="$RA_ARTIFACTS/target/blueprint.log"
  ra_guest_exec target 'pacman-conf --repo-list; ls -la /var/lib/pacman/sync/' > "$a/pacman-sync-state.txt" 2>&1 || true
  # Only configured repositories matter; the sync directory may hold other files.
  synced=$(ra_guest_exec target 'for repo in $(pacman-conf --repo-list); do
      [[ ! -e /var/lib/pacman/sync/$repo.db ]] || printf "%s " "$repo"; done') ||
    ra_fail TARGET_PREFLIGHT "could not inspect Machine B pacman sync databases"
  if [[ -n $synced ]]; then
    ra_fail TARGET_PREFLIGHT "Machine B has pacman sync databases for: ${synced% }; its fresh-install package state was altered"
  fi
  # Read-only evidence of what pacman itself can answer on the fresh machine.
  ra_guest_exec target 'for q in -Qq -Qqe -Qqen -Qqem; do out=$(pacman "$q" 2>/dev/null); status=$?
      printf "pacman %s exit=%s lines=%s\n" "$q" "$status" "$(printf "%s" "$out" | grep -c .)"; done' \
    > "$a/pacman-queries.txt" 2>&1 || true
  ra_blueprint target --profile "$RA_GUEST_PROFILE" --machine target --json check > "$a/check.json" 2>> "$log" &&
    ra_contract check "$a/check.json" target 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "Blueprint check failed on fresh Machine B (see target/check.json, target/blueprint.log)"
  ra_contract fresh-check "$a/check.json" target 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "Blueprint check did not report the fresh machine's unavailable package origin"
  ra_blueprint target --profile "$RA_GUEST_PROFILE" --machine target --json restore --dry-run \
    > "$a/restore-plan.json" 2>> "$log" &&
    ra_contract restore-plan "$a/restore-plan.json" 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "Restore planning failed on fresh Machine B (see target/restore-plan.json)"
  ra_contract readiness-plan "$a/restore-plan.json" 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "Restore plan on fresh Machine B does not demand package readiness (see target/restore-plan.json)"
  ra_target_refuses_unready_restore
}

# The real apply path must refuse before any mutation while readiness is
# unmet. It runs without a terminal, so nothing could prompt either.
ra_target_refuses_unready_restore() {
  local a="$RA_ARTIFACTS/target" status=0
  ra_blueprint target --profile "$RA_GUEST_PROFILE" --machine target restore packages --yes \
    > "$a/restore-refusal.txt" 2>&1 || status=$?
  if (( status == 0 )) || ! grep -q 'omarchy update' "$a/restore-refusal.txt"; then
    ra_fail TARGET_PREFLIGHT "Restore on fresh Machine B did not refuse with the readiness remediation (see target/restore-refusal.txt)"
  fi
  if ra_guest_exec target "pacman -Q $RA_PACKAGE" >/dev/null 2>&1 ||
     ra_guest_exec target 'test -e ~/.local/state/omarchy-blueprint/restores' >/dev/null 2>&1; then
    ra_fail TARGET_PREFLIGHT "refused Restore still mutated Machine B"
  fi
}
