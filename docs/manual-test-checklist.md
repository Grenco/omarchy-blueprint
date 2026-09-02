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

## First-party plugin enablement

- Capture a profile with at least one optional first-party plugin disabled.
- Change one captured plugin from enabled to disabled and another from disabled to enabled.
- Confirm `status plugins` reports both changes with the correct desired state.
- Confirm `restore plugins --dry-run` uses only native `omarchy plugin enable/disable` operations.
- Confirm interactive restore asks once before changing plugin state.
- Confirm `restore plugins --yes` works without waiting for input.
- Confirm a first-party plugin missing in a different Omarchy version is skipped with a clear explanation.
- Re-run `status plugins` after restore and confirm it is clean.

## Third-party plugins (next slice)

- Capture a clean Git-installed plugin and verify its public URL, exact revision, and enabled state.
- Confirm credentials embedded in an HTTPS Git remote are never written to the profile.
- Modify a Git plugin and confirm it is captured as local content rather than losing edits.
- Capture a custom local plugin and a plugin cloned from a first-party plugin.
- Restore a missing Git plugin interactively and confirm the executable-code trust warning is visible.
- Confirm JSON/non-interactive restore refuses third-party installation without explicit consent.
- Confirm explicit consent can add the plugin, validate its manifest, and pin the captured revision.
- Confirm a failed manifest validation leaves no partial plugin directory.
- Restore a missing local plugin, rescan the shell catalog, and restore its enabled state.
- Confirm an existing plugin with different content or provenance is reported and never overwritten.
- Confirm additional target-machine plugins remain installed.
- Confirm failures for one plugin do not prevent independent plugins from restoring.

## Aggregate cross-machine workflow

- Clone the profile into an empty directory on the second machine.
- Run aggregate `check`, `status`, `diff`, and `restore --dry-run` before changing anything.
- Confirm the aggregate plan groups packages, themes, and plugins clearly.
- Perform aggregate restore and confirm one approval and one journal cover every provider.
- Confirm provider failures are summarized together and independent operations continue.
- Re-run aggregate `status`; only intentionally skipped conflicts or machine-specific differences should remain.
- Capture again, review the Git diff for secrets and unexpected machine-local data, then push the profile.
