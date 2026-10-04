# ADR 0028: Readable Restore Blockers and Explicit Deferral

## Status

Proposed

## Context

The first public beta was tried on a fresh Omarchy machine. Restore planned
correctly and refused to apply, as ADR 0022 and ADR 0023 require, but the
person using it could not tell why or what to do next:

- The Restore screen listed every blocked target. A fresh machine has no
  pacman sync databases, so every saved package produced its own line
  ("package repository origin cannot be established from local metadata"),
  about 200 of them. The plan's Changes table, the thing Restore is for, was
  squeezed into one line at the bottom.
- The remedy existed. The plan carried a `packages.metadata` requirement
  saying to run `omarchy update`. It was simply buried among those lines and
  never placed next to the blocker it resolves.
- Services was blocked with "Systemd could not validate the proposed effective
  user-service set" on every unit. `systemd-analyze verify` had reported the
  actual reason (typically that a unit's program is not installed yet), but
  Blueprint discarded systemd's output.
- Nothing said how to proceed. ADR 0023 §8 already lets a user narrow the
  selected scope explicitly (`restore <category>`, or Restore Skip policy) and
  plan again, but neither surface offered that, `restore <category>` selects
  only one category, and `--force` (correctly) changes nothing.
- Once a narrowed plan was approved in the TUI, the screen showed only
  "Applying restore..." for as long as the run took. Long steps (a large
  `git clone`, a `mise` tool install) looked identical to a hang. And a step
  that did prompt (Git asking for credentials, SSH for a passphrase or host
  key, `sudo` for a password) wrote its prompt to the terminal the TUI was
  drawing over, then waited forever.

On a fresh machine these blockers are expected, not exceptional. Services that
run programs from saved packages cannot be verified until those packages are
installed, and packages cannot be installed until Omarchy's own update has
created the sync databases. A usable Restore therefore has to let the person
make progress in stages without weakening the safety model.

This ADR changes presentation and scope selection. It does not change any
provider's safety rules, compatibility evidence states, authority effects,
requirements, Force/Exact semantics, or the profile schema.

## Decision

### 1. Restore summarizes blockers; it never enumerates them on the main view

The primary Restore surfaces (TUI Restore screen, human CLI output) present
blocked findings grouped by category and cause, not one line per target:

```text
Restore is blocked by 2 categories
  Packages · 187 packages: pacman's package databases aren't set up yet …
    Fix: run `omarchy update`, then plan again.
  Services · 3 units: systemd rejected waybar-extra.service: Command /usr/bin/foo is not executable …
```

- A group is one category and one finding code. It shows how many targets it
  covers, a few example targets, and one representative explanation.
- The blocker section has a fixed line budget on the TUI, so the Changes and
  Skipped tables always keep their space. Anything beyond the budget is
  summarized as "… n more" and remains available in the compatibility details
  view.
- The same grouping is computed once, in the compatibility domain package,
  and used by both CLI and TUI, preserving ADR 0023 §14's single source.
- JSON output is unchanged and still carries every finding. Grouping is
  presentation only and never enters the plan or approval comparison.

The same principle applies to every Restore section: no section other than the
scrollable Changes/Skipped region may grow with the number of targets.

### 2. Every blocker says what it means and what to do

Each blocker group carries the next step when one is known:

- When findings link a requirement (ADR 0023 §11), the group shows that
  requirement's remediation command next to the blocker.
- Provider summaries use plain language that names the cause. Stable finding
  codes are unchanged for JSON consumers.

Below the groups, both surfaces always state the ways forward:

1. fix the cause outside Blueprint and plan again (TUI `r`);
2. restore everything else now and defer the blocked categories (TUI `d`,
   CLI `--defer`);
3. leave specific targets alone permanently with Restore Skip policy.

`--force` is not offered, because it does not and will not override
compatibility (ADR 0023 §9).

### 3. Providers report the authoritative cause

When an external validator decides a blocker, the finding carries that
validator's reason. In particular, Services keeps `systemd-analyze verify`'s
messages, attributes each message to the unit it names, and drops warnings
systemd itself ignores. A unit systemd did not name is reported as blocked
because the set it belongs to was rejected, naming the unit that was. When
systemd's reason is that a program is not executable, the finding says the
program is not installed yet.

The finding code (`services.unit.invalid`) and its Blocked authority are
unchanged. Only the summary becomes truthful.

### 4. Explicit deferral narrows the selected scope

A Restore run may defer one or more captured categories:

```text
omarchy-blueprint restore --defer services,packages
```

or, in the TUI, by pressing `d` on a blocked plan, which defers exactly the
currently blocked categories and plans again.

Deferral is ADR 0023 §8's explicit narrowing, made usable for more than one
category:

- A deferred category is not selected. It contributes no operations, skips,
  requirements, verification, or compatibility entry, exactly as if the user
  had run `restore` for every other category.
- The plan records the deferred categories (`deferred` in JSON) and both
  surfaces show them before approval. They are part of the approved plan, so
  approval stability (ADR 0023 §12) covers them.
- Deferral is per run. It is never persisted as policy, never inferred, and
  never applied silently: a blocked plan still refuses to apply until the
  person chooses to defer.
- Deferring every captured category, an unknown category, or a category
  without captured state is an error. Deferral and a single selected category
  (`restore <category>`) cannot be combined.
- Deferral grants no authority. Force, Exact, policy, provider safety and
  compatibility apply to the remaining categories exactly as before.

### 5. Deferred categories stay visible until they are restored

After a deferred plan applies, both surfaces say which categories were
deferred and how to plan them again: `restore` with no deferral (or
`restore <category>`) in the CLI, `d` again in the TUI to clear the deferral
and replan. The follow-up is always a newly calculated plan with its own
approval; Blueprint never resumes a deferred category on its own.

This is the intended fresh-machine sequence:

```text
restore --defer packages,services   # everything that can apply now
omarchy update                      # Omarchy's own readiness step
restore --defer services            # packages now install
restore                             # services verify once their programs exist
```

### 6. The TUI can replan on demand

The Restore screen gains `r` to plan again after the person fixes something
outside Blueprint, replacing "open Restore again to replan".

### 7. Applying shows progress

Both surfaces report each operation as it starts, keeps running and
finishes. The CLI already did; the TUI now shows steps done out of the total,
the step running now and how long it has run.

The TUI no longer hands the terminal over for a whole plan that contains a
`sudo` step. The plan applies from the interface, and each interactive step
borrows the terminal (kept across consecutive interactive steps) and prints
its progress there; the interface returns as soon as an ordinary step starts.
While a restore applies, Ctrl+C in the interface no longer quits, which would
abandon the running step: pressing it twice stops the restore after the
current step, and the outcome is shown with the refreshed plan. Progress comes from the executor's
existing events, so it adds no new authority or state.

A step can name itself (`label`) and say what it may wait for from the person
(`awaits_you`), for example an Omarchy recipe that waits for a Tailscale
sign-in. The plan shows that before approval, and progress repeats it while the
step runs, so waiting for the person never looks like a hang.

### 8. Non-interactive commands can never prompt

Operations not marked interactive run detached from the terminal: in their own
session with no controlling terminal, and with `GIT_TERMINAL_PROMPT=0`. Any
attempt to prompt (`sudo`, `ssh`, Git credentials) therefore fails at once with
an error the plan reports, instead of waiting on a prompt nobody can see.
Operations that legitimately prompt keep the existing interactive path, which
hands them the terminal (ADR 0022).

Because detached commands no longer receive the terminal's interrupt, an
interrupt cancels Blueprint's context instead. Cancellation asks a detached
command's whole process group to stop (SIGTERM, then a kill after a grace
period) and sends interactive commands the interrupt they would have received,
so `pacman` can release its database lock.

### 9. Steps that wait for the person run last and can be skipped

Some steps legitimately wait for the person: Omarchy's Tailscale recipe runs
`tailscale up`, which waits until the device is signed in. Blueprint does not
skip, time out or reimplement such a step, and never handles the credentials.
Two rules keep the wait from holding the restore hostage:

- **Waiting steps run last.** Steps with `awaits_you`, and every step that
  depends on them, move to the end of the plan in their original order.
  Everything else is done before Restore waits for anyone. The reordering is
  part of the plan, so the preview shows it and approval covers it.
- **Ctrl+C during a step that owns the terminal skips just that step.** The
  terminal delivers the interrupt to the step, which stops; Blueprint ignores
  it itself, records the step as skipped by the person (not failed), holds
  back the steps that depend on it, and continues with the rest. At any other
  moment Ctrl+C still stops the restore, and a second one exits at once.
  The summary names what was skipped and that `restore` will plan it again.
  A skipped step that leaves its target unverified is not reported as a
  verification failure, but anything else still missing is.

Skipping an Omarchy recipe stops the whole recipe, not only its waiting part,
so its remaining setup also happens on the next restore. Recipes are safe to
run again.

## Consequences

### Positive

- A blocked fresh-machine Restore explains itself in a few lines and leaves
  the plan visible.
- The remedy for a requirement appears next to the blocker it resolves.
- Services blockers name the failing unit and systemd's reason.
- People can make progress in stages without any safety rule being relaxed.
- A long step is visibly working, and a step that needs a credential fails
  with an error instead of hanging.
- CLI and TUI stay in parity through one grouping function.

### Negative

- Restoring a fresh machine can take several explicit runs, because Blueprint
  still will not order Services after a same-plan package install (see
  "Not decided here").
- The plan model gains a `deferred` field, which JSON consumers see.
- Grouped output hides individual targets until the details view or JSON is
  consulted.
- A non-interactive step that used to succeed only because someone could type
  into a visible prompt (in the CLI) now fails. Such steps were never safe in
  the TUI; the error says what needs a credential so it can be set up first
  (for example an SSH agent or a Git credential helper).

## Not decided here

**Same-plan dependency ordering.** A Service blocked only because its program
comes from a package installed in the same plan is an ordering problem, not a
safety problem. Validating Services against the post-package state, or
sequencing categories inside one approval, would change ADR 0022/0024's
inspection and authority model and needs its own decision. Until then,
deferral is the supported path.

## Rejected alternatives

- **Let `--force` or a new `--ignore-blockers` apply the rest.** Conflates
  conflict authority with applicability (ADR 0023 §9) and invites applying
  plans nobody reviewed category by category.
- **Silently apply the unblocked categories.** Rejected by ADR 0023 §8: the
  person must choose to narrow the scope.
- **Persist deferral as Restore Skip policy.** Skip means "never restore this
  here"; deferral means "not yet". Conflating them would leave categories
  permanently unrestored by accident.
- **Truncate findings without grouping.** Hides the cause when the first lines
  happen to be the least informative ones.

## References

- ADR 0017: TUI interactive client architecture
- ADR 0022: Fresh-package readiness and elevated Restore
- ADR 0023: Plan-integrated migration compatibility assessment
- ADR 0024: Semantic user-service reconstruction
