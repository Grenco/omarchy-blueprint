#!/usr/bin/env bash
# Runs inside the restored Machine B. Grades it with native system state
# only, independent of Blueprint's own verification.
source /tmp/blueprint-ra-fixtures/guest-env.sh
set -uo pipefail
fixtures=/tmp/blueprint-ra-fixtures
status=0

check() {
  local name=$1
  shift
  if "$@" >/dev/null 2>&1; then printf 'PASS %s\n' "$name"; else printf 'FAIL %s\n' "$name"; status=1; fi
}

check "package:$RA_PACKAGE" pacman -Q "$RA_PACKAGE"
check "theme:$RA_THEME" test "$(cat "$HOME/.local/state/omarchy/current/theme.name" 2>/dev/null)" = "$RA_THEME"
check "plugin:$RA_PLUGIN_ID" eval 'omarchy plugin validate "$HOME/.config/omarchy/plugins/$RA_PLUGIN_ID" &&
  omarchy plugin list --json | jq -e --arg id "$RA_PLUGIN_ID" "any(.[]; .id == \$id and (.firstParty | not))"'
check "shell:idle-lock-and-bar" jq -e '.version == 1 and .idle.lock == 600 and .bar.position == "bottom"' \
  "$HOME/.config/omarchy/shell.json"
check "config:$RA_CONFIG_PATH" eval 'printf "mode = \"reconstructed\"\n" | cmp - "$HOME/$RA_CONFIG_PATH"'
check "hook:post-update.d/blueprint-ra" eval 'cmp "$fixtures/hooks/blueprint-ra" "$HOME/$RA_HOOK_PATH" &&
  test "$(stat -c %a "$HOME/$RA_HOOK_PATH")" = 755'
check "default:terminal=$RA_DEFAULT_TERMINAL" test "$(omarchy default terminal)" = "$RA_DEFAULT_TERMINAL"
check "resource:$RA_HELPER_RESOURCE:mapped" eval 'cmp "$fixtures/scripts/blueprint-ra-helper" "$HOME/$RA_HELPER_TARGET" &&
  test "$(stat -c %a "$HOME/$RA_HELPER_TARGET")" = 755 && test ! -e "$HOME/$RA_HELPER_SOURCE"'
check "resource:$RA_GIT_RESOURCE" eval 'test "$(git -C "$HOME/$RA_GIT_PATH" remote get-url origin)" = "$RA_GIT_REMOTE" &&
  test "$(git -C "$HOME/$RA_GIT_PATH" rev-parse HEAD)" = "$RA_GIT_REVISION" &&
  test -z "$(git -C "$HOME/$RA_GIT_PATH" status --porcelain)"'
check "resource:$RA_SKIP_RESOURCE:untouched" test ! -e "$HOME/$RA_SKIP_PATH"
exit "$status"
