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

ra_assert_target_pristine() {
  local log="$RA_ARTIFACTS/target/preflight.log" reason
  ra_guest_exec target 'bash -s' > "$log" 2>&1 <<'GUEST' || {
source /tmp/blueprint-ra-fixtures/guest-env.sh
set -uo pipefail
status=0
absent() {
  local name=$1
  shift
  if "$@"; then printf 'PASS absent %s\n' "$name"; else printf 'FAIL source state present: %s\n' "$name"; status=1; fi
}
absent "package:$RA_PACKAGE" eval '! pacman -Q "$RA_PACKAGE" >/dev/null 2>&1'
absent "theme:$RA_THEME" test "$(cat "$HOME/.local/state/omarchy/current/theme.name" 2>/dev/null)" != "$RA_THEME"
absent "default:terminal=$RA_DEFAULT_TERMINAL" test "$(omarchy default terminal)" != "$RA_DEFAULT_TERMINAL"
for path in ".config/omarchy/plugins/$RA_PLUGIN_ID" "$RA_CONFIG_PATH" "$RA_HOOK_PATH" "$RA_HELPER_SOURCE" \
            "$RA_HELPER_TARGET" "$RA_GIT_PATH" "$RA_SKIP_PATH" omarchy-profile .local/state/omarchy-blueprint; do
  absent "~/$path" test ! -e "$HOME/$path"
done
shell_doc="$HOME/.config/omarchy/shell.json"
[[ -f $shell_doc ]] || shell_doc="$OMARCHY_PATH/config/omarchy/shell.json"
absent "shell:idle.lock=600,bar.position=bottom" eval \
  '! jq -e ".idle.lock == 600 and .bar.position == \"bottom\"" "$shell_doc" >/dev/null'
exit "$status"
GUEST
    reason=$(grep '^FAIL ' "$log" | head -1 || true)
    ra_fail TARGET_PREFLIGHT "${reason:-pristine check failed} (see target/preflight.log)"
  }
  grep '^PASS ' "$log" > "$RA_ARTIFACTS/target/preflight.txt"
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

  ra_blueprint target --profile "$p" --machine target --json check > "$a/check.json" 2>> "$log" &&
    ra_contract check "$a/check.json" target 2>> "$log" ||
    ra_fail TARGET_PREFLIGHT "Blueprint check failed on Machine B (see target/blueprint.log)"
}
