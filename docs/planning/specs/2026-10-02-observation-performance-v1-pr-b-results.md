# Observation performance v1 — PR B results

PR B implements the approved request-local read cycle, Services/Packages reuse,
and fresh-mutation authority tests. PR A's identity-aware batching is unchanged.
No provider scheduler, progressive TUI, persisted cache, auto-sync, or machine
mutation is included. PR C remains a separate human/measurement gate.

## Reproducibility

- Before: merged PR A/main `bf6b816b75cffc79a30cdde7a7272ea902432745`.
- After measured implementation: `a28167d9a3e561f0452e1b9048e0c134450d1105`.
  A subsequent diagnostics-only correction carries the request collector into
  forwarded caller contexts; diagnostics were disabled for these measurements.
- Omarchy, Linux `7.1.9-arch1-2 x86_64`, Go `go1.27.1 linux/amd64`,
  systemd `261 (261.2-1-arch)`, Ryzen 7 5800HS, 16 logical CPUs.
- Profile: `omarchy-profile`, unchanged. Five warm samples per variant/path,
  one discarded warm-up, sequential non-overlapping suites. No Go compilation
  or tests during timed suites. Normal `go build` binaries for PTY measurements;
  temporary counting Runner for logical probes, no command outputs/arguments
  in evidence. `/usr/bin/time` is unavailable: `wait4`/`Getrusage` provide CPU/RSS.
- Command counts are Blueprint Runner invocations, not grandchildren spawned
  internally by shell/Omarchy tools. Maximum simultaneous Blueprint-launched
  commands was **one**, before and after. RSS is the largest individual process,
  not a sum of a process tree. Child pacman memory dominates it.

## Logical read results

Times are medians, seconds. Overview → Restore is two independent read cycles
because navigation must refresh observations, not carry an old Overview into a
new screen. The Force/Safe row is Overview followed by one Force refresh that
also calculates the Safe comparison, as the TUI does.

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Services-only Status | 0.804 | 0.811 | +0.9%, already observed once |
| Packages-only Status | 0.626 | 0.630 | +0.6%, already detected once |
| Full Status | 1.686 | 1.687 | effectively unchanged |
| Overview | 2.339 | 1.747 | −25.3% |
| Overview → Restore | 5.769 | 3.773 | −34.6% |
| Overview → Force/Safe Restore | 9.324 | 4.284 | −54.0% |
| Commands per Overview | 108 | 79 | −29 |
| systemctl calls per Overview | 12 | 10 | −2 package semantic probes |
| Packages detections per Overview | 2 | 1 | −50% |
| Services inventories per Overview | 1 | 1 | already one on this profile |
| Largest child RSS, Overview | 162.31 MiB | 162.32 MiB | unchanged |
| Blueprint's own peak RSS, Overview | 23.13 MiB | 25.36 MiB | +2.23 MiB |

Overview ranges: 2.319–2.409 s before, 1.732–1.760 s after (startup included).
Overview → Restore ranges: 5.756–5.841 s and 3.756–3.811 s.
Force/Safe ranges: 9.250–9.383 s and 4.238–4.339 s.

| Path | Commands before → after | Services before → after | Packages before → after | CPU before → after |
| --- | ---: | ---: | ---: | ---: |
| Overview | 108 → 79 | 1 → 1 | 2 → 1 | 1.745 → 1.124 s |
| Full Status | 76 → 76 | 1 → 1 | 1 → 1 | 1.083 → 1.079 s |
| Services Status | 8 → 8 | 1 → 1 | 0 → 0 | 0.182 → 0.183 s |
| Packages Status | 29 → 29 | 0 → 0 | 1 → 1 | 0.659 → 0.663 s |
| Overview → Restore | 263 → 197 | 3 → 2 | 4 → 2 | 3.933 → 2.557 s |
| Overview → Force/Safe | 418 → 276 | 5 → 2 | 6 → 2 | 6.186 → 3.106 s |

CPU combines Blueprint and child user/system time; it is not evidence of Go CPU
as the bottleneck. Overview Blueprint self CPU is 0.136 → 0.131 s; child CPU is
1.608 → 0.994 s. Status needs only
one live observation already, so reuse does not speed it up.

## Actual TUI measurements

Five warm PTY samples per variant/path, normal uninstrumented binaries, 150×40
terminal. Ready detection matches the **numeric header status**, not the initial
section labels. Restore navigation uses the exact `screen.restore` action; `f`
only toggles conflict mode. No Apply/Capture action was sent.

| Metric | Before | After |
| --- | ---: | ---: |
| Process start → first frame | 45.0 ms | 44.8 ms |
| Process start → populated Overview | 2.360 s | 1.743 s |
| Process start → completed default Restore preview | 5.908 s | 3.927 s |
| Force/Safe refresh after default preview | 7.016 s | 2.584 s |
| Overview-only process CPU (self + children) | 1.769 s | 1.140 s |
| Default navigation process CPU (self + children) | 4.105 s | 2.729 s |
| Default + Force navigation process CPU | 8.677 s | 4.768 s |
| Largest process RSS, default navigation | 162.46 MiB | 162.42 MiB |

Default-then-Force total startup latency is 12.975 → 6.493 s, including both
preview refreshes. It is not the direct Force/Safe logical row above. All runs
reached populated screens, with no timeout/error display. Quitting the TUI
returns the existing context-canceled exit code 1 on both versions; this is not
a preview failure. The <100 ms initial-frame target remains met.

## Conditional classification

A disposable **local profile copy**, not the user's profile, forces both
Services and Packages drift into Restore candidate classification rather than
the Capture short-circuit. Only read-only machine probes/preview run against it;
the temporary profile is removed after the probe.

| Metric | Before | After |
| --- | ---: | ---: |
| Overview wall median | 5.889 s | 1.540 s |
| Commands | 154 | 41 |
| Services inventories | 4 | 1 |
| Packages detections | 4 | 1 |

Every one of the five before/after runs produced the same normalized Overview
hash. The ordinary profile's Overview, Status, provider Status and both Restore
plan hashes also matched across all before/after samples. This validates the
conditional repeated-observation diagnosis, without treating four observations
as an unconditional property of every Overview.

## Command costs for ordinary Overview

Cumulative and median-call columns are medians across five runs; slowest is
the maximum single call across the five. Fixed representative families only;
private arguments/output are deliberately omitted.

| Family | Calls before → after | Cumulative before → after | Median call after | Slowest after |
| --- | ---: | ---: | ---: | ---: |
| systemctl | 12 → 10 | 820 → 838 ms | 113 ms | 151 ms |
| pacman | 32 → 16 | 1,028 → 517 ms | 10.7 ms | 188 ms |
| pacman-conf | 4 → 2 | 24.3 → 12.1 ms | 6.1 ms | 7.4 ms |
| mise | 2 → 1 | 22.6 → 11.9 ms | 11.9 ms | 13.1 ms |
| git | 36 → 36 | 53.6 → 52.7 ms | 1.3 ms | 2.9 ms |
| omarchy | 6 → 6 | 148 → 152 ms | 14.1 ms | 65.0 ms |
| sh | 12 → 6 | 161 → 80 ms | 3.2 ms | 65.4 ms |
| tailscale | 2 → 1 | 13.0 → 6.4 ms | 6.4 ms | 7.5 ms |
| cat | 2 → 1 | 1.7 → 0.8 ms | 0.8 ms | 1.1 ms |
| systemd-analyze | 0 → 0 | 0 | — | — |

Services inventory is two catalogues plus six `show` batches (8 commands).
Remaining systemctl calls here come from Packages semantic checks. Packages
probes fall with Detect reuse; profile Git scanning remains cheap and unchanged.
Other providers remain serial and use their existing probes. No pacman-origin
semantics or local database parsing was rewritten.

## Safety, verification and remaining work

The cycle privately freezes profile/machine/policy and owns lazy typed slots.
Concurrent projections share one in-flight observation; failed observations are
retained only for that cycle. Caller cancellation does not cancel another
waiter. Cycle cancellation/Close cancel and join loaders and prevent publication
of late facts. Results are defensive copies, including Packages fields omitted
from JSON. Services root resolution is local and never mutates shared discovery
state. Read adapters expose no Capture/Verify/transaction authority.

Capture approval comparison and post-stage recheck, authoritative Restore
replanning/comparison, and Verify remain freshly inspected. Tests explicitly
change live A to B while a preview cycle remains alive, and prove mutation is
refused with no effects; staging A to C also refuses/rolls back. CLI changed
no-op plans are revalidated before verification rather than trusted.

Verification: `go test ./...`, `go vet ./...`, `go build ./...`, full
`go test -race ./...`, release helpers (61/61), reconstruction helpers (103/103),
and shell syntax checks passed. No pre-existing races were observed. Hosted
same-runtime Reconstruction Assurance remains a required merge gate; no
reconstruction/apply was run on the developer machine.

Batch size remains **64** from PR A: nearly the same measured speed as 128,
with bounded arguments/output/failure scope and exact serial-inventory equality.
The original N+1 inventory was 371 processes; it is now 8 on this machine.

Remaining ranked costs are Services/systemd response latency (~0.81 s), Packages
detection (~0.63 s), then other Omarchy/shell probes; Git is still low priority.
The 0.5 s Services, 1.5 s Overview and 3 s combined engineering targets remain
unmet here. This justifies a separately approved coarse serial/2/4 concurrency
experiment after B review/merge, not concurrency in this branch. Initial frame
latency is already small; progressive UI and persisted caching are not needed
to establish this observation architecture. No CPU profile was warranted.
