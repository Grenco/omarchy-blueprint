# ADR 0002: built-in theme vertical slice

The first theme milestone records the active built-in Omarchy theme in
`themes/themes.toml`. Detection and activation use the public
`omarchy theme current` and `omarchy theme set` commands. Restore changes only
the active theme and verifies the result.

User-installed themes, local themes, and user overrides of built-in themes are
rejected during capture for now. Recording only their name would create a
blueprint that cannot reproduce their content. A later slice will record Git
provenance and safely copy local theme content before those sources are
accepted.

Category-less commands retain their existing package behavior for backward
compatibility. Theme operations are selected explicitly with the `themes`
argument until multi-provider planning and verification are introduced.
