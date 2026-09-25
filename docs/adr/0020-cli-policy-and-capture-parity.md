# ADR 0020: CLI access to machine-aware policy and capture review

## Status

Accepted

## Context

ADR 0017 requires every Blueprint semantic capability and derived state used by the TUI to be available to CLI-only users, including JSON. The machine-aware policy design made `workflow.Session` the shared authority, but the CLI still exposes only the older package exclusion and Config management controls. `include`/`exclude` are deliberately different from Capture Preserve / Restore Skip: package exclusion removes desired state; Config Excluded changes discovery. Neither can express sparse profile or machine overrides.

An audit against the TUI screens and workflow APIs finds these gaps:

| TUI capability | Existing CLI | Required CLI surface |
| --- | --- | --- |
| Category and target Capture Update/Preserve, Restore Apply/Skip, profile or named machine scope, reset to inherit | No | Policy show/set/clear with effective source and explicit rules |
| Machine Safe/Force and Additive/Exact persisted defaults | No | Machine restore-defaults show/set |
| Stop managing a provider-owned target, distinct from Preserve and desired absence | Only package exclude, Config exclude and Resource untrack | Stop-managing through workflow, with provider-specific rejection |
| First-capture review with outcomes, safety blocks, and approved reinspection | Capture immediately writes | Read-only capture preview and reviewed capture |
| One-run Restore conflict/convergence overrides and plan | Yes (`--conflicts`, `--convergence`, `--force`, `--exact`, `--dry-run`, JSON) | Document, retain |
| Machine lifecycle, Resource mappings, Config management, Sync, path inspection | Yes | Retain |

Navigation, editing handoffs, and presentation-only affordances do not need matching commands. TUI composition of multiple actions does not require a dedicated CLI verb.

## Decision

1. Add `policy show [category [target]]`, `policy set capture <category> <update|preserve> [target]`, `policy set restore <category> <apply|skip> [target]`, and `policy clear <capture|restore> <category> [target]`. All accept `--scope profile|machine:<name>`. The default scope is the active machine when selected, otherwise profile defaults. `--scope profile` explicitly selects portable defaults; `--scope machine:<name>` selects a named machine without changing the local binding, even when its name is `profile`. `show` reports explicit sparse rules and effective semantic values, inheritance source, and target inspection where available. Category-level rules remain addressable when no target is supplied. A target not currently present can still be set or cleared through provider validation. CLI values translate to internal Enabled/Disabled only at the app boundary; the other axis's values and internal names are invalid CLI input.
2. Add `machine restore-defaults [name]` and `machine restore-defaults set <name> --conflicts safe|force --convergence additive|exact`. Partial edits inherit the other axis's current effective value. Names are explicit for mutation; reads default to the active machine. Use the existing workflow setter.
3. Add `policy stop-managing <category> <target>` through `Session.StopManaging`. No generic deletion logic belongs in CLI. Config's management controls and Resource untracking remain separate operations.
4. Add `capture --dry-run` for a read-only, grouped inspection and `capture --review` for an interactive review before invoking `Session.CaptureApproved`. With `--json --review`, return the preview without applying (JSON must never wait for a prompt). Existing plain `capture` remains the script-friendly immediate capture path. A changed approved review fails closed with fresh differences and requires another invocation; never auto-approve it.
5. Human and JSON output must contain stable target keys, semantic policy values for each axis (Update/Preserve and Apply/Skip), and policy sources, not just labels or raw booleans/enums. CLI parsing and presentation live in `internal/app`; validation, resolution, reinspection, safety, and persistence remain in workflow/providers.

## Consequences

- CLI-only and automation users can inspect and edit the same policy and machine defaults used by TUI, including targets on other machines.
- Preview and approved Capture reuse the transaction's reinspection and rollback rather than introducing a weaker CLI-only check.
- Explicit scope and separate `policy` verbs avoid conflating policy with legacy package exclusion or Config discovery rules.
- CLI output and tests must track workflow target identities and inheritance semantics; provider-specific safety remains authoritative.

## References

- ADR 0017: TUI interactive client architecture and capability parity
- Machine-aware Capture and Restore policy design (`docs/planning/specs/2026-09-18-machine-aware-capture-restore-policy-design.md`)
