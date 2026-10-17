# Phase 11 PR A — Execution Notes

**Plan:** [`2026-10-17-auto-sync-phase-11-implementation-plan.md`](2026-10-17-auto-sync-phase-11-implementation-plan.md) §3\
**ADR:** [0029](../../adr/0029-machine-local-auto-sync-authority.md) (accepted, revision 4)\
**Audit:** [`2026-10-17-auto-sync-restore-capabilities.md`](../audits/2026-10-17-auto-sync-restore-capabilities.md)\
**Start SHA:** `aa6f127` (merged `main`), on top of the Phase 11 design commits\
**Scope:** local authority, effect descriptors and registry, permission algebra, read-only preview. **No scheduling, CLI, TUI or unattended mutation.**

## What was built

### Effect descriptors (E1)

`model.Operation` gains `Effect *model.Effect`, supplied by the provider that
plans the operation: family, change, payload, source trust, activation,
privilege, interaction, recovery and verifiability. Every provider operation
in all nine categories carries one; each provider has an `effects` helper.
Descriptors state only what the provider knows: configuration, shell, hook,
resource and service content is `exec-later` until positive-evidence analysis
(PR D) proves otherwise; Git theme/plugin installs and AUR builds are
`unpinned` because they fetch a tip before pinning; recipes that wait for a
person are `interaction: required`. A nil or incomplete descriptor always
means review.

### `internal/localstate`

The Sync cursor's private-state primitives (no-follow regular-file reads with a
size limit, 0700 directory creation that refuses symlink aliases, atomic 0600
writes with fsync) are extracted unchanged and shared by the cursor, the
profile lock and the grant store. The existing cursor and lock tests pass
unchanged (behavioural parity). One hardening: a state directory that
resolves outside the state home is refused.

### `internal/autosync`

- **Registry** (`registry.go`): `restore-capabilities/1`, every audit matrix
  row as an entry (family, class, admitted shapes, run-code categories,
  privileged flag, conditions, increment). **No entry is delivered.** T3
  forms (Force replace/delete, the interactive preinstall group) have no
  entry.
- **Grant** (`authority.go`): setting (off/on/paused) separate from the
  Suspended-for-safety guard; preset; Restore link; interval; identity
  (canonical profile root, one-way host ID, machine, branch, upstream,
  remote fingerprint); per-category scope with target and family
  allow/deny; per-category run-code scope; privileged-install flag;
  capability version; generation; approved authority digest. Validation
  rejects unknown versions, categories and families, run-code or privileged
  scope outside Hands-off/Custom, control characters, oversize scopes and
  raw host IDs.
- **Store** (`store.go`): `auto-sync.toml` beside the Sync cursor, strict
  decoding (unknown fields rejected), fail-closed on malformed, foreign,
  oversized, symlinked or non-regular files. Saves are optimistic
  read-modify-write under a short `auto-sync.lock` that is never held while
  waiting for the profile lock (ordering: profile lock → grant lock).
- **Approval** (`approval.go`): `ApproveLocally` requires an interactive
  `Confirmer`, shows a `Change` summary (new categories, raised ceilings, new
  run-code categories, privileged install) and binds to the exact authority
  digest. Enabling, widening, adding run-code or privileged authority and
  lifting the safety guard need it; pausing, turning off, narrowing and
  resuming the already-approved authority do not.
- **Host identity**: SHA-256 over a domain-separated `/etc/machine-id`
  (fallback `/var/lib/dbus/machine-id`); the raw ID is never stored.
- **Evaluator** (`evaluate.go`): grant gates (off, paused, suspended,
  identity, capability version, unconfirmed authority, Notify only), then
  descriptor hard denials (undescribed, provider mismatch, interaction
  required, unpinned source, removal without recovery), then registry
  entries lowest class first (delivered, shape, proven conditions,
  no-prompt), then authority (preset or Custom ceiling, category scope,
  family and target denies, Restore Skip veto, linked-mode
  "Restore widened beyond confirmed", run-code for every named category
  and the operation's own, privileged install). Dependencies propagate:
  a missing, cyclic or review-needing dependency blocks its dependents.
  `Evidence` is an interface that can only refine a descriptor with proof;
  `NoEvidence` is the default.

### `workflow.PreviewAutoSync`

Read-only: runs `InspectSync` (fetching only when asked, labelled
`Fetched`), turns blockers, missing completed Sync, divergence, unpublished
local commits, Capture/Publish needs, decisions, deferrals and remote control
changes into whole-plan reasons, and — when nothing is left to fast-forward —
plans Restore from the current checkout and evaluates every operation. It
reports per-preset "available now / not available yet" from the registry.

## Deviations and decisions

1. **Behind plans are not classified yet.** When the remote is ahead, the
   executable Restore plan for the pinned remote revision needs snapshot-root
   planning (E2), which the plan assigns to PR C (Task C0). The preview reports
   `operation-plan-unavailable` instead of guessing. Plan §3 Task A3's
   "precise requirement" paragraph predates that assignment; C0 governs.
2. **Fetch locking is PR B.** `PreviewAutoSync{Fetch: true}` performs the same
   unlocked fetch as `InspectSync`; Task B1 adds the non-blocking profile lock
   and review lease. No scheduled caller exists yet.
3. **No CLI or TUI.** Task B4/E1 register `auto-sync`; PR A exposes Go APIs
   only, so no user can create a grant yet.
4. **Theme activation is stricter than the audit's hook-free case.** The
   Hands-off entry always requires Themes and Hooks run-code; the Balanced
   entry needs `no-theme-hook` evidence. Refining Hands-off to Themes-only when
   no hook exists is PR D work with the evidence provider.
5. **Unattributed category steps** (no target keys) need a category-wide
   grant. Scoped Restore planning already refuses unattributed operations, so
   this is defensive.
6. **Same-UID tampering.** The approval digest and file permissions protect
   against other users and accidental import, not a process already running as
   this user; this is documented in the plan and not claimed otherwise.

## Tests

- `internal/autosync`: production registry authorises nothing; grant gates;
  hard denials beat delivered entries; presets strictly ordered; run-code
  separate and family-scoped; theme activation with a hook never Balanced
  (hook permission does not downgrade it; proven-inert path does); privileged
  install needs its own authorisation and a noninteractive mechanism;
  Restore Skip veto, target/family denies, linked vs copy-once/separate
  widening reasons; dependency missing/cycle/blocked propagation; evidence
  cannot rename a family; a 2,000-case randomised monotonicity test
  (tightening preset, scope, run-code or Restore never turns review into
  eligible); availability strictly broader per preset with nothing delivered;
  store round trip (0600/0700, no temp files, no URL/secret text), fail-closed
  on malformed/unknown-field/unknown-version/unknown-category/oversize/
  symlink/directory/FIFO/other-profile grants and symlinked state dirs; never
  reads a grant from the profile checkout; approval required for enable,
  preset, category, run-code, privileged and un-deny widening, refused when
  non-interactive, declined or bound to another authority; pause/resume/off
  and narrowing without approval; resume-with-wider and lifting the guard
  need approval; stale writer refused; copied grant on another host
  authorises nothing; one-way host ID; validation table; order-independent
  digest; registry matches audit rules (Conservative additive and code-free,
  Balanced code-free, Hands-off needs run-code, packages privileged in I4 with
  separate AUR/recipe mechanisms, removals need provenance in I4, unit
  inertness, theme-hook rules, T3 forms unregistered); every provider family
  registered and every registry family emitted; source-level check that every
  provider `model.Operation` literal carries a descriptor (red-checked by
  deleting one).
- Providers: descriptor completeness per provider; Defaults `Plan` emits
  closed-set updates.
- `internal/localstate`: no creation on inspect, 0700 tightening, symlink
  alias refusal, outside-home refusal, bounded regular-file reads (symlink,
  FIFO, oversize), atomic 0600 writes, idempotent removal.
- `internal/workflow`: preview is read-only (profile tree, HEAD, cursor bytes,
  state directory, live state, no push/capture/apply; only the tracking ref
  moves, and only with `Fetch`); behind plans report
  `operation-plan-unavailable`; up to date vs manual Sync required;
  unpublished local work refused; fresh Restore plan classified (undescribed
  → review, delivered fixture entry → eligible, production registry →
  nothing, other host → identity mismatch, target deny, paused).

Verification: see the PR description for the exact commands and results.
