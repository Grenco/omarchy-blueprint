# ADR 0003: portable user themes

Theme capture inventories the active built-in plus every user theme. Clean Git
themes are represented by repository URL and exact revision. Modified Git
themes, local themes, and development-directory symlinks are captured as local
content; a user directory sharing a built-in slug is recorded as an overlay.
Git metadata is not copied, credentials are removed from HTTP remote URLs, and
internal symlinks or special files are rejected.

Local content is stored under `themes/local/<id>`. Capture replaces that
managed snapshot atomically so removed themes do not linger in the blueprint.

Restore is additive. A missing Git theme is installed with
`omarchy theme install`, pinned to the captured revision, and activated only
after all theme material is present. A missing local theme or overlay is copied
without overwriting. Existing themes whose source, revision, or content hash
differs are reported as conflicts and left untouched. Omarchy's packaged theme
directory is always read-only.
