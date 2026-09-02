# ADR 0001: first vertical slice

Omarchy State begins with a complete packages-only reconstruction loop rather
than broad capture coverage. The first schema records all explicitly installed
native and foreign packages. Restore only installs missing packages; it never
removes extras.

The core consumes structured provider results and owns serialization,
operation execution, approval, and journaling. External commands are invoked
as argument arrays without a shell. Capability checks are preferred to version
branches, while the captured Omarchy version remains profile metadata.

This deliberately postpones the TUI, Git automation, themes, plugins, shell
configuration, directories, migrations, and AI integration. A later ADR will
define how an Omarchy-version baseline can distinguish user additions from the
full explicit package set without invalidating schema 1 profiles.
