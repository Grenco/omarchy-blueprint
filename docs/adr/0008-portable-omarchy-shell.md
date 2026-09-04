# ADR 0008: portable Omarchy Shell state

Blueprint captures customized Omarchy Shell configuration from
`~/.config/omarchy/shell.json` and restores it conservatively.

ADR 0009 supersedes this ADR's whole-document restore conflict rule. The
capture format, Shell ownership, provenance, backup, restart, and schema-4
decisions below remain in force.

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

Shell JSON version mismatches require migration. Empty/default desired Shell
state is additive and does not delete an extra target `shell.json`. See ADR
0009 for baseline-aware semantic merge and conflict resolution; a successful
write restarts the full Shell with `omarchy-restart-shell`.

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
