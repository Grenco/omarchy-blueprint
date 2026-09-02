# ADR 0002: built-in theme vertical slice

This initial restriction is superseded by ADR 0003 while its profile format
remains backward compatible.

The first theme milestone records the active built-in Omarchy theme in
`themes/themes.toml`. Detection and activation use the public
`omarchy theme current` and `omarchy theme set` commands. Restore changes only
the active theme and verifies the result.

User-installed themes, local themes, and user overrides of built-in themes are
rejected during capture for now. Recording only their name would create a
blueprint that cannot reproduce their content. A later slice will record Git
provenance and safely copy local theme content before those sources are
accepted.

Category-less `status`, `diff`, and `restore` aggregate packages and every
additional provider captured by the profile. They use one restore plan, one
approval, one journal, and combined verification. An explicit `packages` or
`themes` argument filters the operation to that provider. Profiles without
captured theme state retain their package-only behavior.
