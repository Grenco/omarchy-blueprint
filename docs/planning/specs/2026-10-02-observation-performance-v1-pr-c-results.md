# Observation performance v1 — PR C results

## Scope and reproducibility

PR C implements read-cycle Status/Diff scheduling at the human-selected bound
**4**, following merged PR A #61 and PR B #62. Restore planning, targets,
classification and finalizers remain serial. Capture/Restore authority paths are
unchanged. No progressive/stale TUI, persisted cache, scheduling daemon, auto-sync,
new dependency or package-origin rewrite is included.

- Before: latest merged main `7a0c16ec9c937a2545301b9369b710ed44eaba95`.
- Measured production implementation:
  `cc2c2b8ddc77eae105033e50b427252530bb35cd`.
- Omarchy, Linux `7.1.9-arch1-2 x86_64`, Go `go1.27.1 linux/amd64`,
  systemd `261 (261.2-1-arch)`, Ryzen 7 5800HS, 16 logical CPUs.
- `omarchy-profile` is unchanged. Seven logical paths and three real PTY paths,
  five warm samples each per variant, one discarded warm-up. Suites ran
  sequentially without compilation/tests/other benchmark suites overlapping.
- Logical probes use the same temporary counting Runner on both exact source
  revisions. Actual TUI uses normal `go build` binaries. Neither uses the spike
  overlay; debug diagnostics are disabled. CPU/RSS use Getrusage/wait4 because
  `/usr/bin/time` is unavailable. No host Capture/Restore apply was performed.
- Command counts and peaks refer to Blueprint Runner calls, not grandchildren
  inside shell/Omarchy commands. RSS is the largest individual process, not a
  summed concurrent-process tree. The provider limit is per Status operation,
  not a global limit across unrelated simultaneous read cycles.

## Logical results

Median seconds, process startup included. Navigation creates a new read cycle;
Force/Safe projections share one refresh cycle. Classification uses an isolated
disposable desired-profile fixture, never writes the user's profile, and probes
the machine read-only.

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Services-only Status | 0.846 | 0.832 | essentially unchanged; one provider |
| Packages-only Status | 0.648 | 0.639 | essentially unchanged; one provider |
| Full Status | 1.731 | 0.926 | −46.5% |
| Overview | 1.746 | 0.934 | −46.5% |
| Overview → Restore | 3.876 | 3.053 | −21.2% |
| Overview → Force/Safe Restore | 4.457 | 3.610 | −19.0% |
| Conditional classification Overview | 1.580 | 0.953 | −39.7% |
| Commands per Overview | 79 | 79 | unchanged |
| systemctl calls per Overview | 10 | 10 | unchanged |
| Packages detections per Overview | 1 | 1 | unchanged |
| Services inventories per Overview | 1 | 1 | unchanged |
| Peak active Runner calls, Overview | 1 | 4 | bounded provider overlap |
| Largest child RSS, Overview | 162.41 MiB | 162.34 MiB | unchanged |
| Blueprint own peak RSS, Overview | 25.71 MiB | 25.58 MiB | no material increase |

Overview ranges: 1.743–1.778 s before, 0.927–0.962 s after.
Full Status: 1.714–1.774 s and 0.900–0.944 s.
Overview → Restore: 3.861–3.900 s and 2.994–3.079 s.
All 70 logical samples exited successfully and every mode's normalized result
hash matched across all ten samples, including both Restore plan hashes.

| Path | Commands both revisions | Services / Packages observations both | CPU before → after |
| --- | ---: | ---: | ---: |
| Overview | 79 | 1 / 1 | 1.130 → 1.185 s |
| Full Status | 76 | 1 / 1 | 1.101 → 1.158 s |
| Services Status | 8 | 1 / 0 | 0.197 → 0.190 s |
| Packages Status | 29 | 0 / 1 | 0.680 → 0.671 s |
| Overview → Restore | 197 | 2 / 2 | 2.636 → 2.651 s |
| Overview → Force/Safe | 276 | 2 / 2 | 3.234 → 3.284 s |
| Conditional classification | 41 | 1 / 1 | 0.951 → 0.957 s |

Overview self CPU is 0.135 → 0.142 s; child CPU dominates total CPU. Total CPU
increases about 5% while latency falls: subprocess/I/O overlap, not fewer probes
or faster rendering. No CPU profile was warranted. The two-provider conditional
case peaks at two active calls; one-provider Status peaks at one, not four.

## Actual TUI

Five warm PTY samples per variant/path, normal binaries, 150×40 terminal.
Populated detection matches numeric header status, not first section labels.
Navigation sends `screen.restore`, then optionally toggles conflict mode; no
Apply/Capture key is sent. All 30 runs reached populated screens without timeout
or error display. The existing cancel-before-quit exit code 1 occurs on both
revisions and is not a preview failure.

| Metric | Before | After |
| --- | ---: | ---: |
| Process start → first frame, Overview-only | 44.1 ms | 45.0 ms |
| Process start → populated Overview | 1.808 s | 0.977 s |
| Process start → completed default Restore preview | 4.024 s | 3.194 s |
| Force/Safe refresh after default preview | 2.650 s | 2.667 s |
| Default-then-Force total startup latency | 6.708 s | 5.897 s |
| Overview-only CPU self + children | 1.182 s | 1.202 s |
| Default navigation CPU self + children | 2.799 s | 2.863 s |
| Default + Force navigation CPU | 4.909 s | 4.996 s |
| Largest process RSS, default navigation | 162.57 MiB | 162.48 MiB |

The default-then-Force row includes both preview refreshes; it is not the direct
logical Force/Safe path. Preview-only refresh remains serial and is essentially
unchanged. First frame and Overview engineering targets are met. Services-only
and the 3 s combined target remain unmet here; do not broaden scheduling or
weaken inspection semantics just to cross those thresholds.

## Command costs and interference

Ordinary Overview; family counts are unchanged. Cumulative elapsed and median
call are medians across five runs, slowest is the maximum across five.
Arguments, environment and output bodies are never retained in the report.

| Family | Calls | Cumulative before → after | Median call after | Slowest after |
| --- | ---: | ---: | ---: | ---: |
| systemctl | 10 | 841 → 852 ms | 115 ms | 153 ms |
| pacman | 16 | 518 → 543 ms | 11.3 ms | 199 ms |
| pacman-conf | 2 | 13.1 → 13.0 ms | 6.5 ms | 8.6 ms |
| mise | 1 | 11.4 → 11.3 ms | 11.3 ms | 12.1 ms |
| git | 36 | 53.6 → 55.6 ms | 1.4 ms | 3.2 ms |
| omarchy | 6 | 148 → 156 ms | 14.6 ms | 67.8 ms |
| sh | 6 | 79.8 → 83.2 ms | 3.1 ms | 70.0 ms |
| tailscale | 1 | 6.4 → 6.6 ms | 6.6 ms | 6.7 ms |
| cat | 1 | 1.0 → 0.8 ms | 0.8 ms | 0.9 ms |
| systemd-analyze | 0 | 0 | — | — |

Cumulative command time overlaps, so it must not be summed as independent wall
time. Modest per-family slowdown/extra CPU is consistent with some resource
contention; no observed failure or result divergence. This is evidence on this
machine, not a promise of no interference everywhere. Services batching remains
64 (two catalogues plus six show batches here); two additional systemctl calls
come from Packages semantic probes. Git remains low priority.

## Correctness and lifecycle

`observeProviders` accepts narrow read views, rejects Capture/Verify authority
before launch, assigns fixed result indexes, stops dispatch on failure/cancel,
and joins launched workers. Among independently reported meaningful errors,
lowest provider index wins; secondary cancellation does not hide the initiating
error. A signal-exit RunError under canceled scheduler context is cancellation;
missing-executable errors remain meaningful. A simultaneous independent
signal-killed probe is indistinguishable from cancellation and may be classified
as secondary; regular exit/launch/output-limit failures retain their errors.

`Cycle.BeginWork` registers scheduled Status lifetime under the same lock used
by Close/loader registration; release is idempotent. Close cancels and joins
forwarded workers as well as loaders. Meaningful Status failure joins workers,
releases its own registration, then closes/joins its failed cycle before return,
with no partial report. A canceled individual waiter remains local while its
cycle is still alive; parent cancellation/Close reaches all probes. Filesystem
providers which do not support interruption must finish before Close returns.

No Session.Reload or authority operation runs in this scheduler. Existing
overlapping-cycle/private-snapshot and fresh A→B authority/post-stage rollback
tests remain green. New tests exercise four-worker bounds, reordered completion,
error precedence/ties, canceled dispatch/late success, Close registration races,
shared loader joining, Config/Resources specialized fields, and Services/Packages
single observations across six overlapping Status requests while Reload runs.

Verification: full uncached Go tests, full race suite, vet/build, release helpers
61/61, reconstruction helpers 103/103 and required shell syntax passed.
The first race run exposed a pre-existing unsynchronized fake machineRunner
command-log append newly reachable under provider concurrency; the test fixture
was mutex-protected and the full race rerun passed. No production race was found.
Hosted same-runtime Reconstruction Assurance remains a required merge gate;
no reconstruction/apply runs on the developer machine.

Independent whole-branch review completed: no Critical, Important or Minor
findings; targeted observation/workflow/profile/Services/Packages/application
read-and-authority race checks passed independently. No fix pass was required.
Hosted same-runtime Reconstruction Assurance still gates merge.
The serial/2/4 selection evidence and original limit-2 recommendation are in
[the spike report](2026-10-02-observation-performance-v1-pr-c-spike.md); the human
explicitly selected 4 for the additional latency gain. There is no runtime tuning
flag/cache schema. Reducing the internal bound later remains a small change with
the same ordering, cancellation and authority tests.
