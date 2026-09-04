# ADR 0008: portable Omarchy Shell state

Blueprint captures customized Omarchy Shell configuration from
`~/.config/omarchy/shell.json` and restores it conservatively.

## Profile layout (schema 4)

```text
shell/
|- shell.toml
|- shell.json
`- baseline.json
```

`shell.toml` records the supported Shell JSON version and canonical hashes of
the desired and captured baseline documents. The JSON documents are parsed for
semantic comparison but `shell.json` is restored as the exact captured bytes.

## Safety and restore behavior

Customized `shell.json` is authoritative. Baseline content drift alone is not
a conflict, but a Shell JSON version mismatch requires migration. A target
with unknown Shell customization is never overwritten. Empty/default desired
Shell state is additive and does not delete an extra target `shell.json`.

Restore only writes when the target is missing or still semantically equals the
current Omarchy baseline. It then restarts the full Shell with
`omarchy-restart-shell`.

## Plugin ownership

Once `capture.shell` is true, Shell owns plugin enablement, disabled
first-party plugins, layout, instances, and inline settings. The plugins
provider continues to own installed source and provenance. Profiles captured
before Shell state retain the plugins provider's legacy enable/disable behavior.

Third-party Shell references require captured plugin provenance. A targeted
Shell restore does not write references to missing plugins. Aggregate restore
links each missing referenced plugin's reconstruction (including validation and
rescan) ahead of `shell.write`.

## Upstream references

This behavior follows Omarchy's `quattro` source documentation:
`shell/README.md`, `manual/05-the-top-bar.md`, and `manual/31-dotfiles.md`.
