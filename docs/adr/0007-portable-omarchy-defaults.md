# ADR 0007: portable Omarchy defaults

Blueprint captures Omarchy's semantic default applications — terminal, browser,
editor, and agent — as plain values in `defaults/defaults.toml` and restores
them through Omarchy's own native commands. No file copying, hashing, merging,
or filesystem ownership is involved.

## Profile layout (schema 3)

```text
profile.toml
defaults/defaults.toml
```

```toml
terminal = 'ghostty'
browser = 'firefox'
editor = 'zed'
agent = 'codex'
```

Unset values are omitted. An empty value means the user never picked that
default, so Blueprint holds no desired state for it and never produces an
operation to unset it — absence does not mean delete/reset.

Schema 2 profiles load with empty defaults state and save as schema 3; the
migration is intentionally boring and carries no data.

## Detection and capture

Detection runs `omarchy default <kind>` for terminal, browser, editor, and
agent and trims the output. A command failure is a descriptive provider error.
Capture records the detected values to `defaults/defaults.toml` and marks
`capture.defaults`.

## Diff semantics

Diff compares the saved desired values against the live machine state:

- same value → no drift;
- profile wants a different value → `~ default terminal: foot → ghostty`
  (profile → machine, matching the theme drift convention);
- machine selects a default the profile does not manage → additive drift
  (`+ default agent: codex (not captured; restore will not remove it)`),
  mirroring the "additional theme left installed" philosophy.

## Restore

Plan emits one `omarchy default <kind> <value>` operation per drifted managed
default, classified low risk. An empty saved value produces no operation.
Omarchy validates values and handles install-if-necessary (notably for the
default agent through its mise integration); Blueprint detects and replays and
never maintains its own allowlist of valid choices, because Omarchy's supported
set evolves.

Restore is additive: the provider never unsets a default the user selected on
the target machine. Verification is asymmetric like packages — every managed
default must match its desired value, but an extra machine-selected default
shows up as status drift without failing restore verification.

## CLI

`capture|status|diff|restore|check defaults` behave like every other category,
and aggregate flows include defaults through the provider registry.
