# Manual cross-machine test checklist

## Portable themes

- Restore a missing clean Git theme and confirm the captured revision is checked out.
- Restore a missing local/custom theme and compare its files and permissions.
- Restore a missing built-in overlay and confirm packaged files remain unchanged.
- Confirm the captured active theme is applied after all theme installs.
- Confirm an existing conflicting theme is reported and never overwritten.
- Confirm additional target-machine themes remain installed.
- Confirm aggregate `status`, `diff`, `restore --dry-run`, and `restore` include themes.
- Re-run `status` after restore and confirm it is clean.
