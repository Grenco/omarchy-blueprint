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

## PR D review remediation — fresh Restore approval context

Reviewed head: `ab5b4e100c41def4e85a5375136f72db1f07420e`.
Remediation implementation: `09a14a700a9e5e939d3362591c115b490561017d`.

Verified the P2: the temporary presenter lacked selected-machine identity and
treated an unmeasured Force-vs-Safe difference as zero. The new regression first
failed with “Profile defaults”, Safe/Additive and zero forced overrides while
reviewing a real Config Force-only high-risk replacement (Safe produces no
operation). After the fix, approval names the selected machine, respects explicit
run settings or its saved machine defaults, and labels the Force override count
“not assessed” in the wide/compact summary and confirmation. Actual operations
and risk details remain fully presented. A real Session test covers named-machine
saved Force/Exact defaults and proves no additional live observation.

The presenter freezes descriptive machine/profile context; it does not replan,
observe live state to compute presentation counts, or change execution authority.
Normal Restore workspace measured override counts are unchanged. No Capture,
Restore, Sync settlement, cursor, or Reconstruction Assurance logic changed.

Verification on the remediation tree passed: full TUI tests, `go test ./...
-count=1`, `go test -race ./... -count=1`, `go vet ./...`, `go build ./...`, all
11 Sync/PTY acceptance tests (22.021s), and `git diff --check`.
Hosted CI/AUR/RA had all passed on the previously reviewed head; the updated head
requires fresh hosted checks. Phase 10 stays incomplete pending review/merge;
no merge or Auto Sync work is authorized or undertaken.

## PR D real-machine remediation — `historical-profile` bootstrap blocker

Reported: a clean, shared, normally loadable profile blocked bootstrap with
`historical-profile: Pinned local profile cannot be loaded safely` (CLI and TUI).

Reproduced read-only against a disposable clone of the reporting profile; the
user's profile, checkout, machine and cursor were never written. Stage tracing
showed snapshot, profile load and policy validation succeed; the **themes
provider artifact check** rejected the revision. Two independent causes:

1. Sync implementation defect (`profilegit/archive.go`). Snapshot extraction
   created every directory `0700`, while files are normalized with
   `tar.umask=0022`. Theme hashes are captured from the live directory and
   include directory permission bits (`themes/provider.go` `hashTree`). Git
   records no directory modes, so any local theme with a subdirectory could
   never verify from history although a fresh clone reproduces its hash.
   Fix: snapshot directories use the canonical checkout mode `0755`, applied
   with an explicit chmod so the process umask cannot vary it. The snapshot
   root remains `0700`; exact-tree blob verification is unchanged.
2. Genuinely non-portable committed data. An empty live theme was captured as
   a local theme with the empty-tree hash; Git cannot store an empty
   directory, so the committed artifact is absent and a fresh-clone Restore
   would also fail. Sync keeps failing closed. Verified repair on the clone:
   `policy stop-managing themes theme:<id>`, then
   `policy set capture themes preserve theme:<id> --scope profile` (stop-managing
   clears overrides, so order matters), commit and publish. Bootstrap then
   inspects without blockers and is convergence-eligible.

Diagnostics: `loadSyncView` now returns a typed stage error (snapshot, profile,
policy, artifacts, projection, control) carrying only category and
provider-owned target identity; providers may return
`syncengine.ArtifactError`. The `historical-profile` code is unchanged; its
message names B/L/R, the stage, and for artifacts the target plus a safe repair.
No wrapped causes, paths or contents are exposed; cancellation stays a context
error. Capture's acceptance of empty theme directories is noted, not changed.

TDD: the snapshot-mode and committed-theme regressions failed before the fix
(`drwx------`; valid committed theme rejected). Added coverage: a mismatched
or missing artifact is still rejected; CLI bootstrap/inspect and the TUI show
the stage diagnostic; a blocked inspection cannot execute or advance/write the
cursor. Verification: gofmt, `go vet ./...`, `go build ./...`,
`go test ./... -count=1`, `go test -race ./... -count=1`, `bash test/sync/run.sh`
(11 tests) and `git diff --check` all passed.

## Themes portable-snapshot contract (Capture hardening)

Follow-up to the `historical-profile` remediation: Capture must never claim a
local theme that Git cannot store and a fresh clone cannot restore.

Contract (`internal/providers/themes/portable.go`), shared by Detect/drift,
Capture, Restore's Verify and Sync artifact validation: a theme's portable
snapshot is its regular files (bytes plus executable bit) and the directories
holding them, always `0755`; directory modes, other permission bits and empty
directories are not part of it because Git stores none of them. A top-level
`.git` is excluded. Symlinks, special files, nested Git metadata, or no
storable files make a theme unportable. The fingerprint keeps the previous
serialization evaluated over this canonical tree, so recorded hashes of
ordinarily-permissioned themes still verify (confirmed on the reporting
profile's real themes) and no migration is needed.

Behaviour: Detect reports an unportable theme as a detection-only fact
(`profile.Theme.Unportable`, never persisted) instead of failing every theme.
`targetsFromThemes` turns it into a Capture safety freeze (CaptureEligible
false, target-specific SafetyReason), so a never-managed theme is not recorded
and a managed one keeps its last good desired state and artifact; it is never
tombstoned. The provider backstops this, re-fingerprints what it wrote, and
copies only the canonical snapshot with explicit modes. Preserve leaves a
missing/empty legacy artifact absent rather than failing Capture; Sync still
reports it. The live theme directory is never changed. Plain Capture now lists
safety-blocked live targets ("Not captured (left unchanged for safety)", JSON
`blocked`) unless policy already keeps them out of Capture.

Tests: contract unit tests (permission/umask/empty-folder invariance,
executable bit and empty files significant, legacy-hash compatibility, every
unportable shape, canonical copy, lenient preserve); CLI end-to-end tests for
capture → commit → clone → Sync snapshot validation → Restore → identical
fingerprint, each unportable shape blocked with sibling capture intact and the
live tree byte/mode identical, a managed theme that empties, and preserve
policy silencing the warning. All new end-to-end tests failed on the previous
head and pass now. Full Go/race/vet/build, 11 Sync acceptance tests and
`git diff --check` passed.

Separate finding, not changed here: local Plugins duplicate the original
defect (`plugins/provider.go` `hashTree` hashes permission bits; `copyTree`
copies live directory modes; no empty-snapshot guard), so a local plugin with
non-canonical modes or no files has the same Capture/Sync/clone mismatch.
Recommended follow-up: adopt the same contract for plugins.

## Plain-language TUI help and Sync first-run redesign

Presentation, navigation and read-only preflight only; Sync execution and
safety rules are unchanged. One workflow change: `ResolveSyncBootstrapPlan`
returns a typed `SyncBootstrapUnavailableError` naming the cause and every
affected target (same rules, order and error text), so the TUI preflights both
first-time choices through the exact function execution uses.

User journey: Sync opens with a verdict and "Here's what Blueprint found"
counts drawn from the reviewed plan, then collapsible groups by what a run would
do (headings with counts; Already matches/Protected/large groups closed). `Enter`
and `Space` only expand/collapse; `[`/`]` jump groups; `s` (Review Sync changes)
opens the run review, whose `Enter` continues to approvals. Details explain each
setting in plain language with technical values in a separate section.
First-time setup explains both starting choices, previews only, and shows an
unavailable choice with its actual cause (protected settings listed, `o`/`c`
to show them or open their Capture settings). Help leads with the supplied
Sync questions and per-screen "What is this page for? / What can I do here? /
Will this change my computer…?" text, then shortcuts split into available and
not-available-with-reason; search narrows both.

Deviations from the supplied copy, kept truthful:
- Plan blockers form an extra, openable "Needs attention before Sync can run"
  group so "Open the items below…" is literally true.
- The summary adds "N settings are left for later" when non-zero.
- "Will save changes from this computer" also holds committed-but-unshared
  profile changes (publish only); their Details say so.
- "Everything … already in sync" appears only with no proposed changes, no
  protected differences, outside first-time setup, and with the last completed
  Sync equal to both saved and shared revisions; otherwise the limited wording.
- "Combine approved changes to the saved profile" is shown when the saved
  profile is behind or has diverged (fast-forward or reconciliation commit).
- "Cancel — do not run Sync" also clears this review's decisions, so it is
  distinct from "Back to review".
- "Can't start from this computer yet" also covers differing settings kept out
  of both Capture and Restore, because the workflow refuses those too.
- A mismatched or unreadable last-completed-Sync record has no reset command;
  the TUI explains the problem and the guide documents the manual reset
  ("Resetting Sync on a computer"). A dedicated command is a recommended gap.
- Short terminals use graduated setup/confirmation layouts; full text stays in
  Details.

Tests: groups collapse independently with accurate totals; overview starts
collapsed and selects the first decision; navigation never selects hidden rows,
never starts a run, and never changes decisions or the plan; Details match the
selected row; `s`/Back/Cancel semantics; setup copy and preview-only choices;
protected-settings preflight, listing and Capture navigation; cause-specific
uncommitted/diverged/unreachable guidance; supplied help copy before keys;
footer/help/palette agree; long lists fit 140/100/80, narrow and short
workspaces with colour. Full Go/race/vet/build, 11 Sync acceptance tests and
`git diff --check` passed.
