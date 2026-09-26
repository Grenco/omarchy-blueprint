#!/usr/bin/env bash
# Runs inside Machine A as the fixture user. Creates the canonical source state
# with native Omarchy commands, then independently proves every customization.
source /tmp/blueprint-ra-fixtures/guest-env.sh
set -euo pipefail
fixtures=/tmp/blueprint-ra-fixtures

fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }
step() { printf '== %s\n' "$*" >&2; }
theme_name() { cat "$HOME/.local/state/omarchy/current/theme.name" 2>/dev/null || true; }

step "pristine preconditions"
pristine() { "$@" || fail "fixture no longer distinguishes pristine Omarchy state: $*"; }
pristine eval '! pacman -Q "$RA_PACKAGE" >/dev/null 2>&1'
pristine test "$(theme_name)" != "$RA_THEME"
pristine test "$(omarchy default terminal)" != "$RA_DEFAULT_TERMINAL"
for path in ".config/omarchy/plugins/$RA_PLUGIN_ID" "$RA_CONFIG_PATH" "$RA_HOOK_PATH" \
            "$RA_HELPER_SOURCE" "$RA_GIT_PATH" "$RA_SKIP_PATH"; do
  pristine test ! -e "$HOME/$path"
done
git ls-remote "$RA_GIT_REMOTE" refs/heads/main | grep -q "^$RA_GIT_REVISION" ||
  fail "guest cannot reach fixture Git remote $RA_GIT_REMOTE"

step "package and default terminal"
# Without a terminal, sudo will not prompt; use omarchy-pkg-add's own root path.
/tmp/blueprint-ra-askpass | sudo -S -p '' omarchy pkg add "$RA_PACKAGE"
omarchy default terminal "$RA_DEFAULT_TERMINAL"

step "theme"
omarchy theme set "$RA_THEME"

step "local plugin"
plugin_dir="$HOME/.config/omarchy/plugins/$RA_PLUGIN_ID"
mkdir -p "$plugin_dir"
cp -a "$fixtures/plugin/." "$plugin_dir/"
omarchy plugin validate "$plugin_dir"
omarchy-shell shell rescanPlugins

step "shell"
shell_doc="$HOME/.config/omarchy/shell.json"
shell_base=$shell_doc
[[ -f $shell_base ]] || shell_base="$OMARCHY_PATH/config/omarchy/shell.json"
jq '.idle.lock = 600 | .bar.position = "bottom"' "$shell_base" > "$shell_doc.new"
mv "$shell_doc.new" "$shell_doc"

step "config and hook"
mkdir -p "$(dirname "$HOME/$RA_CONFIG_PATH")" "$(dirname "$HOME/$RA_HOOK_PATH")"
printf 'mode = "reconstructed"\n' > "$HOME/$RA_CONFIG_PATH"
cp "$fixtures/hooks/blueprint-ra" "$HOME/$RA_HOOK_PATH"
chmod 0755 "$HOME/$RA_HOOK_PATH"

step "resources"
mkdir -p "$(dirname "$HOME/$RA_HELPER_SOURCE")" "$(dirname "$HOME/$RA_SKIP_PATH")" "$(dirname "$HOME/$RA_GIT_PATH")"
cp "$fixtures/scripts/blueprint-ra-helper" "$HOME/$RA_HELPER_SOURCE"
chmod 0755 "$HOME/$RA_HELPER_SOURCE"
cp "$fixtures/skip/skip.txt" "$HOME/$RA_SKIP_PATH"
git clone -q "$RA_GIT_REMOTE" "$HOME/$RA_GIT_PATH"

step "independent assertions"
check() {
  local name=$1
  shift
  "$@" || fail "$name: $*"
  printf 'PASS %s\n' "$name"
}
check "package:$RA_PACKAGE" pacman -Q "$RA_PACKAGE"
check "theme:$RA_THEME" test "$(theme_name)" = "$RA_THEME"
check "default:terminal=$RA_DEFAULT_TERMINAL" test "$(omarchy default terminal)" = "$RA_DEFAULT_TERMINAL"
check "plugin:$RA_PLUGIN_ID" eval 'omarchy plugin list --json |
  jq -e --arg id "$RA_PLUGIN_ID" "any(.[]; .id == \$id and (.firstParty | not))" >/dev/null'
check "shell:idle.lock=600,bar.position=bottom" jq -e \
  '.version == 1 and .idle.lock == 600 and .bar.position == "bottom"' "$shell_doc"
check "config:$RA_CONFIG_PATH" test "$(cat "$HOME/$RA_CONFIG_PATH")" = 'mode = "reconstructed"'
check "hook:$RA_HOOK_PATH" eval 'cmp "$fixtures/hooks/blueprint-ra" "$HOME/$RA_HOOK_PATH" &&
  test "$(stat -c %a "$HOME/$RA_HOOK_PATH")" = 755'
check "resource:$RA_HELPER_RESOURCE" eval 'cmp "$fixtures/scripts/blueprint-ra-helper" "$HOME/$RA_HELPER_SOURCE" &&
  test "$(stat -c %a "$HOME/$RA_HELPER_SOURCE")" = 755'
check "resource:$RA_GIT_RESOURCE" eval 'test "$(git -C "$HOME/$RA_GIT_PATH" rev-parse HEAD)" = "$RA_GIT_REVISION" &&
  test "$(git -C "$HOME/$RA_GIT_PATH" remote get-url origin)" = "$RA_GIT_REMOTE" &&
  test -z "$(git -C "$HOME/$RA_GIT_PATH" status --porcelain)"'
check "resource:$RA_SKIP_RESOURCE" cmp "$fixtures/skip/skip.txt" "$HOME/$RA_SKIP_PATH"
