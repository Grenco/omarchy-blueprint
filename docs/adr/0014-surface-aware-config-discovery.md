# ADR 0014: Surface-Aware Config Discovery

## Status

Accepted

## Context

ADR 0013 expanded Config to a sparse baseline-aware overlay, but its recursive
`~/.config` discovery treated application roots too uniformly. Real-machine
testing showed that browser, Electron-style, credential, database, recovery,
and generated application state can dominate `status` and `capture`, producing
large, non-portable profiles and thousands of irrelevant file differences.

## Decision

This is an accepted correction to ADR 0013 for Config discovery, filtering,
output, and include-policy behavior. Config moves to schema 9 so persistent
positive inclusion cannot be silently discarded by a schema-8 binary.

- The unit of automatic recursive discovery is one top-level `.config` surface.
- A bounded, deterministic, metadata-only probe classifies directory surfaces
  as `config-lean`, `state-heavy`, `mixed`, or `sensitive`.
- Only `config-lean` surfaces are recursively auto-discovered. Uncertain,
  mixed, state-heavy, and sensitive surfaces are skipped and reported once.
- Browser/profile stores are detected from Chromium, Gecko, WebKit, and generic
  structural evidence, not a product-name blacklist.
- Baseline-backed paths and saved desired paths remain exact-inspected regardless
  of later surface classification.
- `Configs.Included` persists safe, HOME-relative explicit inclusion roots.
  Inclusion recursively inspects only the requested subtree and does not bypass
  ownership, sensitive-path/content, symlink, special-file, backup, or size
  policy. Exclusion outranks inclusion.
- Generic backup files and backup directories are filtered before probing,
  baseline comparison, tombstones, sensitive inspection, and hashing.
- Human `status` and capture output are surface-oriented. `diff` retains
  detailed portable file differences and emits one summary per skipped surface.
- Resources, not Config, is the explicit bulk-tree option for authoritative
  opaque or state-heavy trees.

## Consequences

Automatic Config capture favors precision over recall and stays bounded on
normal desktop installations. Users can opt into a narrow safe subtree when a
mixed application contains portable settings, without turning Config into an
application-state backup provider. Existing schema-8 profiles migrate
non-destructively with an empty inclusion policy; their saved paths remain
authoritative until a later capture prunes paths that are no longer eligible.
