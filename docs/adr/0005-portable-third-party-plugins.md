# ADR 0005: portable third-party plugins

Clean Git plugins are stored by sanitized remote URL and exact revision.
Modified Git plugins, custom plugins, symlinked development plugins, and clones
of first-party plugins are stored as content-addressed local snapshots under
`plugins/local/<id>`; Git internals and credentials are excluded.

Third-party plugins execute unsandboxed code. Their install, copy, and enable
operations are therefore high risk and are called out in every restore plan.
Blueprint's restore approval is the trust decision, after which native
`omarchy plugin add --yes`, validation, enable, and disable commands are used.

Restore is additive and never overwrites an existing plugin. Local restore is
an ordered validation, copy, shell-rescan, and optional-enable chain. Git
restore is an ordered add, revision-pin, revalidation, shell-rescan, and
optional-enable chain. Failed
dependencies block their downstream operations while independent plugins keep
restoring.
