# Observation performance v1 — PR C scheduling spike

## Status

PR B #62 is merged. The human authorized the PR C spike and implementation.
This report completes the spike and presents the measured bound and lifecycle
details for Task 10's explicit review gate. No production scheduler is implemented.

## Method

Base: latest main `7a0c16ec9c937a2545301b9369b710ed44eaba95`.
Machine: Omarchy, Linux `7.1.9-arch1-2 x86_64`, Go `go1.27.1 linux/amd64`,
systemd `261 (261.2-1-arch)`, Ryzen 7 5800HS, 16 logical CPUs.
Profile: `omarchy-profile`, unchanged.

A Go build overlay replaces only read-cycle Status aggregation in a throwaway
binary. Fixed worker pools of 1/2/4 consume provider indexes; each invokes the
unchanged specialized Config/Resources or generic Diff dispatch with a cloned
desired profile. Indexed output preserves order. Classification, target
inspection, preview planning and finalizers remain serial. This overlay handles
successful probes only: it is not a production error/cancellation implementation.
No product source files, host profile, packages or services are changed.

Seven paths × three limits × five retained warm samples = 105 measurements,
with one discarded warm-up per combination, sequential and non-overlapping.
No compilation/tests run during timed suites. A counting Runner records family
timings and peak concurrent calls, not arguments/output. CPU combines self and
child time from Getrusage; RSS is the largest individual child, not aggregate
concurrent memory. Counts exclude grandchildren of shell/Omarchy commands.
The classification case writes an isolated disposable desired-profile fixture,
never the user's profile, then performs only machine observation/preview.

## Results

Median seconds; totals include process startup.

| Path | Serial | Limit 2 | Limit 4 | Commands (all limits) |
| --- | ---: | ---: | ---: | ---: |
| Overview | 1.785 | 1.117 | 0.948 | 79 |
| Full Status | 1.762 | 1.101 | 0.925 | 76 |
| Services Status | 0.855 | 0.841 | 0.845 | 8 |
| Packages Status | 0.652 | 0.660 | 0.657 | 29 |
| Overview → Restore preview | 3.962 | 3.233 | 3.080 | 197 |
| Overview → Force/Safe preview | 4.481 | 3.759 | 3.683 | 276 |
| Conditional classification | 1.561 | 0.940 | 0.942 | 41 |

| Overview resources | Serial | Limit 2 | Limit 4 |
| --- | ---: | ---: | ---: |
| CPU self + children, median | 1.152 s | 1.155 s | 1.206 s |
| Largest child peak RSS across five | 162.38 MiB | 162.41 MiB | 162.38 MiB |
| Blueprint peak RSS across five | 24.98 MiB | 25.22 MiB | 27.25 MiB |
| Peak simultaneous Runner calls | 1 | 2 | 4 |

Every path's normalized result hash is identical across all 15 samples, including
Restore plan and Safe comparison hashes. Every sample exits successfully.
The clean base full uncached Go suite passed. Six additional race-instrumented
live probes (Overview, Overview → Restore, conditional classification at limits
2 and 4) exited successfully with no reported data race. These are not a
substitute for the complete production race/test matrix or error-path tests.
Overview Services/Packages remain one inventory/detection each; independent
Overview → Restore navigation remains two cycles with two observations each.
The conditional case remains one each. No process-count savings are claimed.

Overview command-family cumulative elapsed medians, serial/2/4 respectively:
systemctl 860/846/870 ms; pacman 525/531/544 ms; Git 55/57/57 ms;
Omarchy 152/154/161 ms. There is no observed command failure or major contention.
Limit 4 has modest extra CPU and slower per-family probes; five samples are not
proof of contention-free behavior on every machine. Package semantic checks and
Services can contact the same user manager, so retain a conservative bound.

## Recommendation and implementation details for review

Select **2**, the smallest useful bound. It reduces Overview by 37%, satisfies
the 1.5 s engineering target in this logical probe, and adds only about 0.24 MiB
Blueprint peak RSS. Limit 4 saves a further 169 ms but doubles the child fan-out,
adds about 2 MiB own RSS and about 4% CPU over limit 2. Neither meets the 3 s
combined goal here; single-provider Services cannot benefit from this scheduler.
Do not extend scheduling to Restore planning or classification solely to meet
that number. No progressive TUI change or persisted cache is proposed.

Use the already approved `observeProviders` interface in
`internal/workflow/read_scheduler.go`; integrate only `ReadCycle.Status`.
Keep one-provider/empty requests serial, exact Config/Resources dispatch and
cloned inputs. Reject direct authority providers before launching work.
Preallocate results and errors by provider index. On first meaningful failure,
cancel outstanding work, stop dispatch, join launched workers, and return the
lowest-index independently reported meaningful error instead of secondary
cancellation. No success/partial report is returned after cancellation.

The spike exposed a lifecycle integration requirement, not a race finding:
slot loaders use the cycle context, while canceled projection waiters deliberately
do not cancel those shared loaders. Scheduler cancellation must therefore also
invalidate its owning failed read cycle, rather than leave probes running behind
an error return. Add small explicit cycle work-registration/cancellation methods
in `internal/observation/cycle.go`: registration shares Close's lock/WaitGroup,
and release is idempotent. Register the scheduled Status operation before launch.
On a meaningful failure cancel the cycle, join workers, release that operation,
and Close/join remaining loaders before returning. Preserve the initiating error.
Close must join forwarded providers too, including filesystem-only callbacks
which cannot be interrupted and must finish. No hidden Session lock, Reload,
mutation scheduler, global semaphore or new dependency is introduced.

Tests before implementation:

- Scheduler: literal indexed ordering with delayed A/early B; worker bounds;
  empty/single sets; invalid bounds; direct authority rejection before calls;
  first meaningful error versus sibling cancellation; independent error ties;
  canceled dispatch; all workers joined; no partial successful report.
- Cycle: work registration racing Close; idempotent release; cancellation reaches
  loaders; Close joins delayed forwarded work; canceled waiter remains local
  except terminal scheduled-refresh failure; no late result publication.
- Integration: specialized Config/Resources fields retained; Status/Overview
  ordering and hashes stable; one Services/Packages observation; overlapping
  read operations and Reload with race detector; existing fresh-authority tests.
- Full verification matrix and five-warm logical/actual TUI measurements after
  production implementation; hosted Reconstruction Assurance remains merge gate.

These are production acceptance criteria, not claims made by the success-only
overlay. Raw sanitized evidence and the throwaway overlay live in this worktree's
ignored plan workspace. Production implementation must be written independently
using TDD, not copied from the overlay.
