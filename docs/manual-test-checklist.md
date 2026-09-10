# Manual cross-machine test checklist

## Portable themes

- Restore a missing clean Git theme and confirm the captured revision is checked out.

## Portable resources

- Track a regular file and confirm `resources/resources.toml` plus its copied snapshot are created.
- Track a clean Git worktree and confirm only portable remote/revision metadata is saved.
- Confirm an inbound `.config` symlink into a tracked resource is adopted and restored with relative link text.
- Confirm a direct symlink root is refused and its target is not automatically tracked.
- Confirm a differing existing resource or link is skipped rather than overwritten.
- Confirm copied resources with a private key, `.env`, special file, or external internal symlink are rejected.
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

## Third-party plugins

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

## Baseline-aware Config overlay

- Run `capture config`, `status config`, `diff config`, `restore config --dry-run`, and `restore config` for an explicitly included baseline customization and an ordinary added file in a config-lean surface.
- Confirm no-history baseline mismatches/deletions are review-only, not captured or ordinary drift; include a safe path to supply intent.
- Run `diff config` and confirm review candidates say `differs` for ambiguous baselines and `absent` for ambiguous deletions, while exit status remains success without actual drift.
- Verify `include config:.zshrc` and `include config:~/.zshrc` both persist canonical `.zshrc`; an exact exclusion becomes Included, while an excluded ancestor rejects a child include.
- Verify `.config/xdg-terminals.list`, `.config/brave-flags.conf`, and `.config/environment.d/omarchy-firefox-wayland.conf` remain delegated to Defaults, while an unrelated safe Config file remains eligible.
- Confirm `status config` and capture output aggregate Config changes by surface, while `diff config` retains detailed portable file differences and one summary per skipped surface.
- On a real desktop with Chromium/Gecko/WebKit-style browsers and mixed applications installed, time `status config`; confirm it remains comfortably interactive, emits one surface-level line per browser/profile store, and never enumerates skipped descendants.
- Confirm structurally detected browser/profile surfaces are pruned regardless of application directory name, and that mixed surfaces such as Typora, LibreOffice, or Obsidian are reported but not recursively auto-captured.
- Confirm a baseline-backed path and a previously saved path inside a now-skipped surface are still inspected exactly and reported when they drift.
- Confirm `restore config --force --dry-run` reports a recoverable backup before replacing a merge-conflicted or otherwise unknown target.
- Use `include config:.config/Typora/themes` on a mixed surface, then capture; confirm only the requested safe subtree is discovered, sibling recovery/runtime state remains skipped, and the inclusion persists in schema-9 Config policy.
- Confirm explicit inclusion still refuses sensitive paths/content, symlinks, special files, oversized files, backup artifacts, and paths owned by stronger providers. Confirm an include below an excluded ancestor is rejected.
- Confirm generic backup files (`*.bak`, `*.bak.*`, `*.backup`, `*.backup-*`, `*.orig`, `*~`) and backup directories (`backup`, `backups`, `*.bak`, `*.backup`, `*-backup`, `*-backups`, `*_backup`, `*_backups`) appear in neither `config/files/` nor `config/baseline/`, produce no tombstones, and are pruned before child inspection.
- On an upgraded baseline, confirm independent source and upstream edits merge cleanly; confirm overlapping edits preserve the target unless `--force` is approved.
- Delete a shipped baseline file, capture, restore onto a target where it exists, and confirm it is backed up then removed.
- Track an intentionally authoritative large or opaque config tree as a Resource, capture again, and confirm Config no longer duplicates its bytes or restore destination.

### Same-machine capture and status

- With all four files at their Omarchy defaults, run `capture config` and confirm nothing is stored under `config/files/` and `config/config.toml` has no entries.
- Customize one file and re-run `capture config`; confirm `config/files/<path>` matches and `config/config.toml` records desired and baseline hashes.
- After capture, `status config` and `diff config` report no drift.
- Change a captured file on the machine and confirm `status config` reports the modify with the file path.

### Reset and restore

- Reset a captured file to its Omarchy default and run `capture config`; confirm the stale snapshot under `config/files/` and its metadata entry are removed.
- Delete a captured target file so it is missing, then run `restore config --dry-run` and confirm a medium-risk write plus a final `hyprctl reload`.
- Approve the restore and confirm the target is rewritten, the reload runs, the restore journal is written, and a backup exists beside the journal.
- Re-run `status config` and confirm it is clean.
- Run `status config`, `restore config --yes` again and confirm "no changes".

### Baseline drift

- Edit the Omarchy baseline for a captured file on the target (simulate an Omarchy upgrade) so its hash no longer matches `config/config.toml`.
- Run `restore config --dry-run` and confirm `Omarchy baseline changed; migration required` with no write.
- Confirm nothing on the machine was modified.

### User drift

- Put content that matches neither the baseline nor the desired file in a captured target.
- Run `status config` and confirm drift is reported.
- Run `restore config --dry-run` and confirm `existing user configuration differs; overwrite disabled`.
- Confirm restore with `--yes` still refuses to touch the drifted file.

### Symlink and special-file rejection

- Replace a baseline or user config file with a symlink and confirm `capture config`/`detect` rejects it without partial profile files.
- Confirm a FIFO or other special file is likewise rejected.

### Backup recovery

- After a config restore, confirm the replaced file exists in the `<journal>.backup/` directory.
- Manually restore the backup file and confirm the machine returns to its prior state.

### Aggregate flows

- With packages, themes, plugins, and config captured, run aggregate `status`, `diff`, and `restore --dry-run` and confirm config appears alongside the other providers.
- Run aggregate `restore --yes` and confirm one approval, one journal, and one summary cover every provider.
- Force a reload failure and confirm the failed op is reported and the summary lists failures while independent operations completed.

### Cross-machine validation

- Clone the profile to a second Omarchy machine (same version) and confirm `capture`-time baseline hashes match, then restore missing targets and verify `status config` is clean.
- On a second machine with a different Omarchy baseline, confirm restore skips with the migration-required message rather than overwriting.

## Portable Omarchy defaults

### Same-machine capture and status

- With defaults selected (e.g. ghostty/firefox/zed/codex), run `capture defaults` and confirm `defaults/defaults.toml` records the values and `capture.defaults` is set.
- Leave one default unselected (fresh Omarchy state) and confirm it is omitted from `defaults/defaults.toml` — unset means unmanaged.
- After capture, `status defaults` and `diff defaults` report no drift.
- Change the machine's terminal default and confirm `status defaults` exits 2 with `~ default terminal: <captured> → <machine>`.

### Restore

- Change the machine's browser default, run `restore defaults --dry-run`, and confirm exactly one `set default:browser` low-risk operation.
- Approve with `--yes` and confirm `omarchy default browser <captured>` ran, verification passed, and the journal records the operation.
- Confirm a machine-selected default that was never captured (e.g. a new agent) is untouched by restore and appears as additive drift (`restore will not remove it`).
- Re-run `status defaults`; only unmanaged extras should remain.

### Unset semantics

- Capture with no agent selected (no agent field in defaults.toml), then select an agent on the machine.
- Confirm `restore defaults --dry-run` produces no agent operation.
- Confirm aggregate `capture`, `status`, `diff`, and `restore --dry-run` include defaults alongside the other providers.

### Agent safety

- Capture a profile with a default agent selected, then run `restore defaults --dry-run` on a machine where the agent default differs or is unset.
- Confirm restore produces NO agent operation and instead shows:
  `- skip default:agent (Omarchy's agent setter launches the selected agent; automatic set-only restore is not currently safe)`.
- Confirm `status defaults`/`diff defaults` still report the agent drift, and `restore defaults --yes` succeeds (verification ignores the agent because restore cannot set it).

### Non-portable values

- Manually set the system browser to an application Omarchy does not manage (raw `.desktop` ID) before capture.
- Confirm capture records the value with a `may not be portable` warning.
- Confirm `restore defaults --dry-run` skips the kind with the portability reason instead of replaying a value Omarchy would reject.

### Profile integrity

- Mark `capture.defaults = true` in profile.toml but delete `defaults/defaults.toml`; confirm any command loading the profile fails with `defaults state marked captured but defaults/defaults.toml is missing`.

### Cross-machine validation

- Clone the profile to a second Omarchy machine and confirm the captured defaults are applied and `status defaults` is clean.
- On a machine where a captured default value is not offered by this Omarchy version, confirm the operation fails with Omarchy's own message and the journal records the failure without touching other defaults.

## Portable Omarchy Shell

- Customize the bar placement/layout and idle settings, then run `capture shell`.
- Include one third-party widget after capturing its plugin provenance.
- Remove the plugin and target `shell.json`, then confirm aggregate
  `restore --dry-run` reconstructs the plugin before `shell.write` and
  `shell.restart`.
- Restore in a disposable or fresh session and confirm bar placement, widget
  settings, and idle values after the restart.
- Confirm `omarchy-shell shell ping` responds after restore; do not rely only
  on the restart command's success because Shell readiness can lag upstream.
- Independently change a scalar and bar layout on the target, then confirm
  normal restore preserves target-only values while applying unrelated captured
  intent.
- Change the same scalar differently on source and target; confirm normal
  restore reports a conflict and `restore shell --force --yes` selects captured
  intent without replacing unrelated target state.
- Change an Omarchy baseline field untouched by the source and confirm restore
  retains the current baseline value.
- Place a captured widget in center while the target has the same widget in
  right beside a desktop-only widget. Confirm normal restore leaves one widget
  in center and preserves the desktop-only widget without `--force`.
- Create duplicate target widget IDs and confirm normal restore reports a
  conflict rather than silently deleting either instance.
- Reset the target to the Omarchy baseline, restore a captured customization,
  and confirm a backup exists beside the restore journal.

## Aggregate cross-machine workflow

- Clone the profile into an empty directory on the second machine.
- Run aggregate `check`, `status`, `diff`, and `restore --dry-run` before changing anything.
- Confirm the aggregate plan groups packages, themes, and plugins clearly.
- Perform aggregate restore and confirm one approval and one journal cover every provider.
- Confirm provider failures are summarized together and independent operations continue.
- Re-run aggregate `status`; only intentionally skipped conflicts or machine-specific differences should remain.
- Capture again, review the Git diff for secrets and unexpected machine-local data, then push the profile.

## Portable Omarchy Hooks

- Capture a flat `post-boot` hook and an immediate `post-update.d` hook; confirm both source bytes and recorded modes are present in the profile.
- Confirm `.d/*.sample`, hidden `.d` children, nested files, and an unknown event's valid hook follow the documented capture rules.
- Capture hooks with modes `0644` and `0755`; confirm profile snapshots are `0644` while restore returns each recorded mode.
- Capture an empty Hooks state, add a hook later, and confirm `status hooks` reports drift.
- Remove a captured hook and confirm `restore hooks --yes` recreates it without executing it.
- Change target hook content and confirm restore skips it; retain an extra target hook and confirm it is not removed.
- Confirm restoring an existing mode-only drift creates a backup and that hook source is never run during capture, restore, or check.
