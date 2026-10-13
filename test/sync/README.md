# Sync acceptance: two disposable hosts

```sh
bash test/sync/run.sh
```

Requires Go, Git and Python 3 (standard library only). The runner builds the real
Blueprint binary, creates a private bare upstream and two cloned profiles, and
uses isolated HOME/XDG roots for two machine overlays. Nothing is installed.
No public remote, user profile, user Git configuration or real machine mutation
is used. Build and fixture directories are removed on exit.

The CLI, semantic providers, Git transactions, cursor files and verification are
real. Machine probes are controlled: Pacman returns an empty fixture inventory;
an isolated Omarchy Defaults getter/setter writes only inside fixture homes.
Services, privilege escalation and other unsupported machine commands fail
closed. Hooks are actual private files, never executed.

Scenarios:

1. Both hosts explicitly bootstrap to the same verified revision (every setup).
2. A captures/publishes a new Hook; B follows and verifies it.
3. Conflicting portable Defaults resolve Keep Local into an exact two-parent
   merge, then the peer follows it.
4. Disjoint Hook additions preserve both intents and the peer follows.
5. Defer preserves checkout and cursor without claiming settlement.
6. Dirty checkout blocks without mutation.
7. Upstream moves after review: no unreviewed Restore or cursor advance.
8. Divergent non-managed committed history blocks.
9. Restore fails after publication: published history remains, cursor stays
   behind, and the next inspection exposes unresolved machine drift.
10. Machine, upstream branch and remote repository changes invalidate cursors.

An additional Linux PTY smoke test opens Overview, navigates to semantic Sync,
checks the populated screen, and exits through the palette without changing
checkout/cursor. It is not a substitute for physical-terminal approval journeys.

Read-only inspection snapshots HEAD/index/worktree and proves no checkout
mutation. Successful cases assert live fixture state and exact verified cursors;
failure cases assert non-settlement rather than just an exit status.

Existing Hook content is intentionally not overwritten under reduced lifecycle
compatibility, even with Force. Consequently conflict tests use portable
Defaults, while addition/disjoint tests exercise real Hook Capture/Restore.
This harness does not weaken that safety boundary.

These tests run in the existing CI job and remain separate from Reconstruction
Assurance. They are not evidence of real two-machine Omarchy acceptance,
physical-terminal usability, or same-runtime reconstruction. Follow the Sync
section of `docs/manual-test-checklist.md` for those human checks.
