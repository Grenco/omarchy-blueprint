# Phase 11 PR B — Execution Notes

**Plan:** [`2026-10-17-auto-sync-phase-11-implementation-plan.md`](2026-10-17-auto-sync-phase-11-implementation-plan.md) §4\
**ADR:** [0029](../../adr/0029-machine-local-auto-sync-authority.md) (accepted, revision 4)\
**Start SHA:** `ec58dcc` (merged `main`, PR #75)\
**Scope:** locked read-only background checker, status/history/notifications, review lease, systemd user timer plumbing, read-only CLI. **No unattended mutation; no way for a user to turn Auto Sync on yet; no timer installed anywhere.**

## What was built

### B1 — Background cycle (`internal/autosync/cycle.go`, `internal/workflow/auto_check.go`)

`autosync.Cycle` loads the grant (never creating one). A scheduled cycle
stops at once when Auto Sync is Off or Paused (no fetch, nothing recorded) and
records **Busy** without fetching while a manual review lease is live. It then
runs `Session.CheckAutoSync`, which takes the profile lock **without waiting**
(a held lock is a Busy skip), fetches once with a bounded timeout (2 minutes;
the whole check is bounded at 10 minutes), inspects and classifies through the
PR A preview, and returns a sanitized `Report`. A fetching `PreviewAutoSync`
now holds the same lock around fetch and inspection. Cancellation is
returned, never recorded. There is no apply step: even a fully eligible plan
reports Needs review with `automatic-apply-not-available-yet`.

Fetch failures are classified in `profilegit.FetchUpstream` from Git's output
(which is never returned or stored) into `ErrFetchSignIn` and
`ErrFetchUnreachable`; a timeout is Offline (`fetch-timed-out`). Background
Git runs through `command.SystemRunner{Noninteractive: true}`: its own session
with no controlling terminal, `GIT_TERMINAL_PROMPT=0`, `GIT_ASKPASS=false`,
`SSH_ASKPASS_REQUIRE=never`, whole-process-group kill on cancellation, and
`RunInteractive` refused. Credential helpers and agents that need no prompt
keep working; anything that would prompt reports **Sign-in needed**.

### B2 — Status, history, notifications (`status.go`, `notify.go`)

`auto-sync-status.json` sits beside the grant in the profile-keyed private
state directory: the last check (time, outcome, typed reason codes, safe
counts, fetched, scheduled), the last notification key, and history bounded
to 50 entries, 30 days and 64 KiB, newest first. Reason codes must match
`^[a-z][a-z0-9-]{0,63}$` (at most 16), so no URL, path or Git output can be
stored. Untrusted, symlinked, oversized or malformed status is set aside
(`Recovered`) and never blocks a check; writes are atomic 0600 under a short
status lock. The setting is never derived from an outcome, and the last
completed Sync is read from the Sync cursor (written only by a verified Sync
or an explicit bootstrap), never from a check.

`Notifier` is injectable; `DesktopNotifier` runs `notify-send` with
arguments (never a shell) and escaped markup. Notices use fixed text and
counts only. Identical states coalesce until a 24-hour reminder (this covers
recurring offline and sign-in warnings); a changed state notifies again; Up
to date resets coalescing. A notification failure never changes the outcome.
Only scheduled checks notify.

### B2a — Manual-review coordination (`lease.go`, TUI Sync review)

Each open review holds its own lease file under `review-leases/` (90-second
TTL, 30-second heartbeat, holder PID). A lease is live only if unexpired, not
implausibly far in the future, trusted (owned by this user, private, not a
symlink) and its process is alive, so an unclean exit, a dead process or a
planted file cannot stop checks for long. Holding prunes expired leases.

The TUI review holds the lease while it shows something to decide or run
(actions, decisions, deferrals, blockers or first-time setup) and releases it
when nothing is pending and when the TUI exits. Before confirmation it
re-reads the tracking revision (no fetch); if it moved, it shows “New changes
arrived from your shared profile while you were reviewing. Your decisions are
kept where they still apply.”, disables confirmation and relabels `r` as
**Refresh**. Refresh re-inspects and keeps a decision only when every input of
that setting is unchanged; an ordinary check for updates still starts a fresh
review. Execution's “Sync plan changed” refusal remains the final guard.

### B3 — systemd user timer (`systemd.go`, `control.go`, `internal/app/auto_sync_systemd.go`)

`omarchy-blueprint-auto-sync.service` is a oneshot (`ExecStart="<exe>"
auto-sync scheduled-check`, `TimeoutStartSec=15min`, `NoNewPrivileges`, idle
I/O, no `[Install]`). `omarchy-blueprint-auto-sync.timer` uses
`OnActiveSec=5min`, `OnUnitInactiveSec=<interval>`,
`RandomizedDelaySec=interval/6`, `AccuracySec=1min` and
`WantedBy=timers.target`, with no `Persistent=` and so no catch-up claim. A
user unit cannot order itself after the system's network, so it does not
pretend to; an offline check reports Offline. The executable path is quoted
with `%`/`$` escaped, and refused if it is relative or contains quotes,
backslashes or control characters. No profile path or remote is in the unit:
the scheduled check reads the one selected profile from
`auto-sync-profile.json` in private state. Both units pass
`systemd-analyze --user verify` (systemd 261). A unit file without
Blueprint's marker, or a symlink, is never overwritten or removed.

`Scheduler` installs (write, `daemon-reload`, `enable --now`) with rollback to
the previous units and timer state on any failure, removes, stops, and
reports state (`list-timers --output=json` for the next check). Without a
user manager it says so and never falls back to root or lingering. `Control`
keeps grant, selection and timer in step:

- Enable saves the confirmed grant, selects the profile and schedules; if
  scheduling fails it turns the grant back off and restores the selection.
- Pause writes the grant first, then stops the timer; a timer error never
  undoes the pause.
- Resume uses the same path as Enable (approval for any widening); a failed
  resume stays paused.
- Disable revokes the grant before touching the timer and leaves another
  profile's timer alone. One profile per timer.

Blueprint's units are reserved: `profile` rejects them as service names (so
no profile can carry them) and Services discovery never offers them.

### B4 — CLI (`internal/app/auto_sync.go`)

`auto-sync status | history` (never fetch), `check` (fetch, record, no
notification), `preview [--no-fetch]`, `run` (refuses: not available yet) and
the hidden `scheduled-check` used by the unit. JSON parity through the usual
envelope. `docs/cli.md` documents the read-only commands.

## Deviations and decisions

1. **No enable or configure command.** Task B4 lists only status, check,
   preview and history. The Go `Control` API is complete and tested, but no
   user path creates a grant or installs the timer until the PR E consent
   surfaces, so PR B changes nothing on any computer.
2. **Explicit checks.** A person's `auto-sync check` runs whatever the
   setting (it is read-only), ignores review leases and never notifies;
   scheduled checks honour Off/Paused and leases, and notify.
3. **Unreadable settings** in a scheduled check record Error
   (`settings-unreadable`) and notify once, since a timer may still be
   installed.
4. **Profiles not set up for Sync** are not fetched; the inspection's own
   reason (for example `git-identity` or `machine-required`) is reported.
5. **Backoff** is the timer interval plus its randomised delay; a check never
   retries within itself, and repeated identical failures do not notify again
   until the reminder interval.
6. **Busy profile for manual Sync.** If a check that started before a review
   was opened still holds the lock, manual Sync now says it did not start and
   nothing was changed (previously the generic “may already have been sent”
   text); `ErrProfileBusy`'s message names Auto Sync checks.
7. **Preview read-only snapshot.** PR A's test now ignores the stable, empty
   `sync.lock` file a fetching preview creates, exactly as manual Sync does.
8. **One read cycle.** A behind plan (the normal case when something changed
   remotely) inspects once. When local live state alone needs Restore, the PR
   A preview also plans Restore (a second read); snapshot planning in PR C
   (Task C0) unifies this.
9. **Login, logout and suspend behaviour** of the real timer is for physical
   acceptance (Task E4). Here the units are syntax-verified and the user
   manager is faked. No real timer was installed.

## Tests

- `internal/autosync`:
  - **Cycle:** Off, Paused and no grant never fetch or record; a live lease
    gives Busy without a check; recording; coalescing and its reset;
    notification failure; explicit check; unreadable settings; check errors;
    cancellation not recorded.
  - **Status:** empty without creating state; check kept separate from
    completed Sync; bounded by count, age and bytes; only safe reason codes;
    corrupt, foreign and symlinked status recovered without following links.
  - **Notices:** plain-English counts only; coalescing; markup escaped and
    passed as arguments.
  - **Leases:** hold, heartbeat and release; independent leases; dead,
    far-future, readable, malformed and symlinked leases ignored; pruning.
  - **Units:** content; banned directives; path escaping and refusal;
    interval bounds; reserved names match `profile`.
  - **Scheduler:** install and remove; rollback at each step and on upgrade;
    no user manager; foreign units untouched.
  - **Control:** enable; rollback; disable ordering; pause and resume; a
    failed resume stays paused; widening on resume needs approval; one
    profile per timer; private selection that fails closed.
- `internal/workflow`: a lock held by manual Sync gives Busy with no fetch
  and nothing changed, and a fetching preview refuses; concurrent checks
  fetch once; a check fetches once and writes nothing but the tracking ref;
  offline, sign-in and unknown failures, repeated three times, never run a
  Git write; fetch timeout gives Offline and the lock is released;
  cancellation returned; up to date; profile without upstream; review lease
  and tracking revision through the session.
- `internal/command`: the background runner cannot prompt (session leader,
  prompts and askpass disabled), cancellation stops the process group, the
  terminal is refused.
- `internal/profilegit`: fetch failures classified without Git output.
- `internal/profile`, `internal/providers/services`: reserved units.
- `internal/tui/screens`: lease held, heartbeated and released; confirmation
  when unchanged; stale review explained with Refresh and still-valid
  decisions kept; changed decisions dropped; ordinary refresh starts fresh;
  busy profile says nothing changed.
- `internal/app`: status and history read-only (no fetch, no state); check
  records, and status and JSON report it separately from the completed Sync;
  scheduled check uses the selection, honours Paused and an open review, and
  notifies; `run` unavailable; preview with and without fetch; no remote URL
  or credential in any output; a profile not set up for Sync explained; the
  systemctl adapter never uses root and reads state.

Verification: see the PR description for the exact commands and results.
