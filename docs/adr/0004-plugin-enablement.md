# ADR 0004: first-party plugin enablement

The first plugin slice records enabled/disabled state for disable-capable
first-party plugins reported by `omarchy plugin list --json`. Restore uses the
native `omarchy plugin enable` and `omarchy plugin disable` commands.

Third-party plugin code is deliberately deferred to a separate slice because
plugins execute unsandboxed inside the long-running shell and Omarchy requires
an explicit trust confirmation when adding them.
