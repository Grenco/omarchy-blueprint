#!/usr/bin/env bash
# Sourced inside a guest: Omarchy PATH plus the running graphical session's
# environment, so native Omarchy commands behave as they would from a desktop
# terminal. Applied identically to source customization and Blueprint.

source /usr/share/omarchy/default/bash/env-bootstrap
source /tmp/blueprint-ra-fixtures/expected.env
while IFS= read -r assignment; do
  export "$assignment"
done < <(systemctl --user show-environment 2>/dev/null |
  grep -E '^(WAYLAND_DISPLAY|DISPLAY|HYPRLAND_INSTANCE_SIGNATURE|XDG_CURRENT_DESKTOP|XDG_SESSION_TYPE)=' || true)
