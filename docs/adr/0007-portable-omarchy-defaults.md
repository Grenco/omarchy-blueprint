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

Plan emits one `omarchy default <kind> --install <value>` operation per drifted
managed default, classified low risk. The `--install` form performs a direct,
non-interactive installation instead of Omarchy's floating presentation UI, so
aggregate restore stays properly CLI-driven. An empty saved value produces no
operation. Omarchy validates values and handles installation; Blueprint
detects and replays and never maintains its own allowlist of valid choices,
because Omarchy's supported set evolves.

Two kinds are deliberately excluded from automatic restore:

- **agent** — Omarchy's agent setter ultimately launches the selected agent
  (`exec omarchy-agent`), so an automatic restore must never invoke it. The
  desired agent is still captured, diffed, and verified, but planning skips it
  with `Omarchy's agent setter launches the selected agent; automatic set-only
  restore is not currently safe`. If Omarchy later gains a set-only mode, this
  skip can become an operation.

- **raw `.desktop` values** — Omarchy's getters fall back to the raw desktop
  ID for applications they do not manage (e.g. a manually configured Vivaldi),
  while the setters reject those values. They are captured and shown with a
  `may not be portable` warning, and restore skips them instead of replaying a
  value Omarchy would refuse.

Restore remains additive: the provider never unsets a default the user
selected on the target machine. Verification is asymmetric like packages and
enforces only what restore can actually set: agent and non-portable values are
excluded from verification (they remain visible as status/diff drift), every
other managed default must match its desired value, and an extra
machine-selected default never fails restore verification.

## CLI

`capture|status|diff|restore|check defaults` behave like every other category,
and aggregate flows include defaults through the provider registry.
