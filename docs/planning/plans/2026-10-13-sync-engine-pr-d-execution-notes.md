# Sync Engine PR D execution notes

Starting main: `5def92f434336ed1d0f89dfe7f1ed15c22d2af22` (merged PR C #73).
Branch: `feat/sync-engine-tui-acceptance`; isolated worktree: `.worktrees/sync-engine-pr-d`.

Scope: approved implementation plan tasks 14–16. No auto-sync, background fetch,
RA redesign, release, or user-profile/machine mutation during development.
Baseline full Go tests passed. Independent review is owned by the human's reviewer;
no review agent will be dispatched.

Pre-flight: task 14 consumes PR B inspection and PR C execution interfaces;
task 15 exercises those same CLI interfaces with disposable host roots and Git;
task 16 documents/tests the completed slice. Current main contains the required
interfaces and retains the four-worker shared read cycle and fresh mutation gates.

Task 14: implemented — separate Profile Git/Sync destinations; inert construction,
asynchronous explicit inspection, grouped read models, B/L/R/M details, separate
control-plane evidence, in-memory history/machine choices and baseless bootstrap.
Execution lends the terminal to `ExecuteSync` with independent fresh Capture,
Restore compatibility/risk and Services activation reviews. Every outcome runs a
new cached inspection and refreshes initialized related views without fetching.

Task 14: Ruling: preserve the existing transport screen's internal `ScreenSync`
identifier for compatibility with transport actions/tests; its user-facing name
is Profile Git, and the new `ScreenReconcile` is user-facing Sync. Cost if wrong:
internal IDs may require a later mechanical rename; user navigation remains clear.

Task 14: Ruling: lend the terminal for the explicitly approved Sync transaction,
using native fresh Capture/Restore approval prompts (and the existing Restore
presentation), rather than coupling Sync to Restore's per-operation terminal
leases/progress. This keeps Git authentication semantics unchanged and retains
all workflow authority callbacks. Cost: Sync execution temporarily replaces the
workspace instead of showing per-step progress inside it.

TDD evidence: distinct navigation, concrete registration, execution confirmation,
explicit bootstrap, no-fetch unrelated refresh, grouped presentation order,
control-only/blocker details, bounded confirmation rendering, persistent repair
warnings, and broken-terminal review rejection each failed before its fix.
Cancellation joins inspection work and discards cancelled/obsolete evidence.
Failure tests cover stale review, lock, publication, Restore and cursor failures;
safe repair markers never expose raw subprocess errors. Canonical reviewed plan
ordering is unchanged by presentation grouping.

Task 15: implemented — real binary/Git, private bare remote, two isolated HOME/XDG
hosts, explicit bootstraps, all ten planned semantic transaction scenarios plus
a Linux PTY inspection smoke test. Inspection snapshots HEAD, index entries,
profile bytes/modes and cursor before/after. The harness is added to the existing
CI job only after repeated local passes; RA sources/workflows are untouched.

Fixture ruling: portable Defaults stand in for the conflicting value target and
real Hook additions for publication/disjoint intent. Existing differing Hooks
cannot be overwritten under reduced lifecycle compatibility, even with Force;
the early acceptance experiment stopped at that existing safety gate. No provider
or compatibility behavior was changed to make acceptance pass. Pacman/Omarchy
probes are strictly controlled, mutation only targets private fixture homes,
unneeded OS commands fail closed, and update checking is disabled for PTY runs.

Bootstrap implementation note: normal inspection returns missing-cursor evidence
before machine observation. Only then is baseless inspection requested (no second
fetch), owning the single live observation cycle. Execution remains separately
fresh; no manufactured B or cursor write is introduced by the screen.

Task 16: documentation/authority audit and final committed-tree automated
verification complete. README, CLI/TUI/Guide and the manual checklist separate Profile Git,
semantic Sync and future Auto Sync. Phase 10 stays pending review/merge of PR D;
Phase 11 remains not started.

Safety audit: new production code is confined to TUI presentation/orchestration.
No changes to CLI transaction authority, providers, Git transport, approval
fingerprints, lock, Verify or cursor-save placement. No shell Git, force push,
rebase/reset, persisted choices, telemetry or background fetch was added.
Unrelated refreshes explicitly use Fetch=false. No real user profile or machine
was changed by development/acceptance.

Verification before final commits: full Go tests/race, vet/build; 66 release
helper tests; 103 reconstruction helper tests; all repository helper shell syntax;
11 Sync/PTY acceptance tests (repeated green). Local runtime is Go 1.27.1,
linux/amd64; module remains Go 1.25 and hosted CI covers 1.25/1.26.

Outstanding human/hosted validation: physical-terminal fresh-approval journeys
and real two-machine Omarchy acceptance are not claimed by automated fixtures.
Local full container package validation could not run because Docker socket
access is denied. No privileged workaround or host package installation was
attempted; the existing hosted AUR validation and RA checks remain required.

Final implementation SHA: `d76d2f04992d1400601517809a1645a1a4ccec82`.
TUI implementation commit: `3258259`.
After both implementation commits, the worktree was clean and the complete Go
test/race suites, vet/build, all 11 Sync/PTY acceptance tests (23.103s), 66 release
helper tests and 103 reconstruction helper tests passed again. All shell helpers
passed `bash -n`; `git diff --check` passed. The final follow-up commit changes
execution notes/manual wording only, not the verified implementation.
Latest origin/main was re-fetched and remained the exact starting SHA above.
PR D is ready for independent code review with the explicit human/hosted gaps
listed above. No merge, release, PR E or Auto Sync work was begun.
