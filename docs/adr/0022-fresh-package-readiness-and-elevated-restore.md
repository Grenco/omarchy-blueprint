# ADR 0022: Fresh-Package Readiness and Elevated Package Restore

## Status

Accepted

## Context

Reconstruction Assurance (ADR 0021) runs Blueprint against a freshly installed official Omarchy 4.0.4 machine. PR #41 found that Blueprint cannot handle packages on that machine, and pinned the failure as the temporary known gap `packages.fresh-sync-database-readiness`.

### Measured pacman behaviour

A fresh Omarchy 4.0.4 install has a populated local package database (`/var/lib/pacman/local`) but no sync database for any configured repository (`core`, `extra`, `multilib`, `omarchy`). With the local database intact and no sync databases:

| Query | Exit | Result |
| --- | --- | --- |
| `pacman -Qq` | 0 | every installed package; correct |
| `pacman -Qqe` | 0 | every explicitly installed package; correct |
| `pacman -Qqen` | 1 | nothing: native classification needs sync metadata |
| `pacman -Qqem` | **0** | **every** explicitly installed package, reported as foreign |
| `pacman -Qqn` / `-Qqm` | 1 / 0 | same pattern for all packages |

Every query also writes `warning: database file for '<repo>' does not exist (use '-Sy' to download)` to stderr, including the successful ones.

So the local database answers "is this package installed, and was it installed explicitly?". Only sync metadata answers "is it native (official) or foreign (AUR)?". Without that metadata, `-Qqem` does not fail; it silently misclassifies the whole machine as AUR. If only some repositories lack a database, `-Qqen` and `-Qqem` both succeed and silently misclassify that repository's packages. An exit status therefore cannot detect the condition.

Blueprint's detection ran `-Qqen`, then `-Qqem`, then `-Qq`, through the combined-output runner. On the fresh machine this failed `check`, `status`, `diff`, Capture inspection and Restore planning. Had `-Qqen` merely been tolerated, the combined output would have parsed the stderr warnings as package names, and `-Qqem` would have reclassified every package as AUR.

### Measured Omarchy behaviour

- `omarchy pkg add` (`requires-sudo`) runs `pacman -S --noconfirm --needed` as root, or through `sudo` for a non-root user. It never synchronizes metadata, so it cannot install anything on a fresh machine.
- `omarchy pkg aur add` runs `yay -S`, which elevates through `sudo` itself. `omarchy pkg drop` (`requires-sudo`) runs `sudo pacman -Rns`.
- Omarchy has no metadata-only readiness command. Every supported path that creates sync databases also upgrades the system:
  - `omarchy update` is the authoritative full update. It runs a snapshot, a keyring refresh, `pacman -Syu` (`omarchy-update-system-pkgs`), migrations, post-update hooks, AUR updates, mise updates and an orphan review.
  - `omarchy-update-system-pkgs` is `pacman -Syu` alone. Its own comments say Omarchy migrations are written against the packages it installs and must follow it, so running it alone leaves Omarchy half-updated.
  - `omarchy refresh pacman` also rewrites `pacman.conf`.
- Syncing metadata without upgrading (`pacman -Sy`) and then installing is Arch's partial-upgrade hazard.

### Measured execution architecture

- `command.SystemRunner.Run` uses `CombinedOutput`, with no stdin or terminal. `command.RunOutput` separates stdout from stderr.
- `model.Operation.Interactive` and `command.InteractiveRunner` exist. The restore executor already runs interactive operations through `RunInteractive`, attached to the process terminal.
- The CLI refuses, before creating a journal, to run an interactive operation with `--json` or without a terminal (`requireInteractiveTerminal`).
- `workflow.Session.ApplyRestore`, which the TUI uses, refuses any plan containing an interactive operation. The TUI therefore cannot apply interactive operations at all today. That is an existing ADR 0017/0020 parity gap.
- Official and AUR package installs are planned as non-interactive commands. With cold sudo credentials they cannot succeed from either client.

## Decision

### 1. Package origin can be unavailable, and Blueprint says so

Package detection first establishes whether classification metadata exists. It lists the configured repositories with `pacman-conf --repo-list`, reads `DBPath` with `pacman-conf DBPath`, and checks for `<DBPath>/sync/<repo>.db`.

- **All present:** detection is unchanged. It uses `-Qqen`, `-Qqem` and `-Qq` for official, AUR and installed packages.
- **Any missing:** Blueprint does **not** run `-Qqen` or `-Qqem`. It records `OriginUnavailable` with the missing repositories, and detects physical state from the local database only:
  - `-Qq` gives installed packages.
  - `-Qqe` gives explicitly installed packages whose origin is unknown.
  - Official and AUR sets are left empty, meaning unknown rather than absent.

All pacman queries read stdout only (`command.RunOutput`), so warnings on stderr can never become package names. Only this one condition degrades:

- a failing `pacman-conf`;
- a failing `-Qq`/`-Qqe`;
- a failing `-Qqen`/`-Qqem` when the databases exist;
- permission errors, a corrupt database, or a missing `pacman`.

All of these still fail visibly. Pacman exit status 1 is not broadly swallowed; the existing rule accepts exit 1 only with empty stdout (an empty result).

Physical presence remains authoritative for presence. A desired package that `-Qq` reports installed is present, whatever its unknown origin.

### 2. `check` and `status`

- `check` succeeds when metadata is missing. It reports a note that package origin classification is unavailable, naming the missing repositories and the remediation (`omarchy update`). The note appears in human output and as JSON `notes`.
- `status` and `diff` compare desired packages against physical presence. A desired package that is installed is not drift, and one that is not installed is reported missing as before. Explicitly installed packages cannot be classified, so they are never reported as added `official`/`aur` drift. Unknown never becomes present-and-classified, and never becomes absent.

### 3. Capture fails safe

When origin is unavailable:

- Capture inspection marks every official and AUR target not capture-eligible. The safety reason is "package origin unavailable: pacman sync databases missing for …". The Capture preview therefore shows them as Blocked. Current state uses physical presence, so the preview never offers "Remember absent" for an installed package.
- `Merge` keeps the previous desired official and AUR sets and their absence tombstones unchanged, whatever the Capture policy says. Nothing is added from a guess and nothing is tombstoned.
- Mise, preinstalls and semantic recipe state are classified independently and are captured normally.

### 4. Restore planning

- Presence comes from `-Qq`. A desired official or AUR package that is installed is not reinstalled. A desired package that is not installed still produces its install operation, exactly as today.
- "Additional package left installed" skips cannot be attributed to a kind. When unclassified explicit packages exist that are not desired, the plan carries one Skipped entry (`packages:unclassified`) explaining that their origin is unknown and that none are removed.

### 5. Exact never gains authority from unknown origin

With origin unavailable, Exact removes no official or AUR package. Each absence tombstone whose package is physically installed becomes a visible Skipped entry ("package origin unavailable; removal disabled"). Exact verification treats such a package as not converged. Unknown classification can only reduce destructive authority.

### 6. Package metadata readiness is a plan-visible requirement satisfied by Omarchy, not by Restore

Blueprint never synchronizes package metadata itself. `pacman -Sy` is never used. `omarchy update` is not embedded in a Restore plan either, because:

- It is a full system upgrade. It changes the `omarchy` package and its `/usr/share/omarchy` defaults (the baselines Config and Shell planned against), runs migrations and post-update hooks that edit user configuration, and updates mise and AUR tools.
- Running it mid-Restore would invalidate preconditions that other approved operations were planned against, such as expected file hashes and the mise declaration snapshot. That would break the stable-authority rule in the plan → approve → recalculate → apply lifecycle.
- Omarchy's own contract is that a fresh install is updated with `omarchy update` before packages are managed.

So when origin is unavailable and the plan contains an operation that needs repository metadata (official or AUR installs, or semantic installs that install packages):

- The plan includes a **readiness requirement**: `id: packages.metadata`, `kind: package-metadata`, listing the missing repositories, the remediation command `omarchy update`, and the IDs of the operations that need it.
- Those operations stay in the plan, visible and unchanged.
- The plan is **not applicable** while any requirement is unmet. `restore --dry-run`, including `--json`, renders the plan and its requirements normally. Apply fails before approval, before a journal is created and before any mutation, with an actionable error naming the remediation. Nothing is partially applied.
- A scoped Restore of categories that need no package metadata (`restore themes`, …) remains available. That is an explicit user choice, not an implicit partial apply.
- After the user runs `omarchy update`, which owns its own prompts and elevation, re-planning finds metadata present and the requirement disappears.

A plan without package operations that need metadata carries no requirement, even when origin is unavailable.

### 7. Administrator authority is expressed as interactive operations

Package operations whose Omarchy mechanism elevates through `sudo` are planned as `Interactive: true`, with a Notice saying they may ask for administrator authentication in the terminal:

- official install `omarchy pkg add`;
- AUR install `omarchy pkg aur add` (via `yay`);
- Exact removal `omarchy pkg drop`;
- preinstall install `omarchy-install-preinstalls`.

Semantic recipes keep their own interactive flag.

- Blueprint never asks for, stores, pipes, logs or journals a password. It does not use `sudo -S`, `SUDO_ASKPASS`, `sudo -v` pre-warming, `NOPASSWD`, or re-executing itself as root. The operation runs through `InteractiveRunner` with the real terminal attached, so `sudo` (or `yay`) owns its own prompt. Blueprint does not probe credentials with `sudo -n` during planning.
- The interactive flag and notice are part of the plan, so approval covers them. Recalculation compares them like every other field, so the authority requirement cannot appear after approval.
- An interactive operation runs through exactly one runner. Its failure is journalled like any other failed operation. Cancellation, via context and executor semantics, is unchanged.

### 8. CLI

- The existing rules apply unchanged:
  - `--dry-run` shows interactive operations, their notices and requirements.
  - `--json` with interactive operations refuses to apply.
  - No terminal refuses before mutation.
  - A normal terminal apply prompts, recalculates, then executes interactive operations attached to the terminal.
- An unmet readiness requirement refuses apply with the remediation. It does this after rendering the plan and before the approval prompt.

### 9. TUI

The TUI uses the same `workflow.Session` plan and apply path. `ApplyRestore` gains an interactive runner supplied by the client. For each interactive operation, the TUI releases the terminal, including its alternate screen. It then runs the command attached to the real terminal through the same `RunInteractive` call, and restores the terminal. Readiness requirements are shown in the plan review, and apply is refused with the same remediation text. The TUI never runs a separate execution stack.

### 10. Headless and non-interactive use

- A plan containing interactive operations is refused before any mutation when there is no terminal or when `--json` is set. This includes `restore --yes` without a terminal. Blueprint never hangs on an invisible prompt and never gains authority silently.
- An unmet readiness requirement is refused the same way.
- In v1 there is no "privilege already available" exemption. Blueprint does not probe sudo, and running Blueprint itself as root is not a supported way to reconstruct a user's machine. Automation must provide a terminal for elevated operations.

### 11. Dry-run and JSON

- `RestorePlan` gains `requirements` (omitted when empty).
- Operations keep their existing `interactive` and `notice` fields.
- Package detection exposes `origin_unavailable` and `missing_sync_databases` wherever package state is emitted.
- Capture previews carry the Blocked safety reason.
- `check` JSON gains `notes`.

### 12. Read-only commands cannot mutate package state

Detection runs only read-only queries: `pacman-conf`, `pacman -Q*`, `stat`. No read path contains `-S`, `-Sy`, `omarchy update`, `sudo` or an interactive runner. `check`, `status`, `diff`, Capture inspection, `--dry-run` and JSON inspection can therefore no more mutate package state than before. Readiness is only ever performed by the user through Omarchy. Tests assert that the detection runner sees no mutating command.

## Consequences

- Blueprint inspects, checks and plans on a fresh supported Omarchy machine without lying about package origin or mutating it.
- Desired package state cannot be silently destroyed by Capture on a machine without metadata. Exact cannot remove packages whose origin it does not know.
- Restoring packages onto a fresh machine becomes an explicit two-step flow: Blueprint shows the readiness requirement, the user runs `omarchy update`, then Restore proceeds. Blueprint never upgrades the system on the user's behalf.
- Package Restore becomes possible with cold sudo credentials from both the CLI and the TUI, through one execution path.
- Headless automation cannot restore elevated package operations. This is deliberate.
- Reconstruction Assurance must model the readiness step explicitly, as amended in the implementation plan. The harness only performs the product-named remediation after asserting that Blueprint demanded it. It never prepares Machine B silently.

## Rejected alternatives

- **`pacman -Sy` inside Blueprint:** partial-upgrade hazard, and it bypasses Omarchy.
- **`omarchy update` or `omarchy-update-system-pkgs` as a Restore operation:** a full upgrade in the middle of an approved plan invalidates other operations' approved preconditions. The narrower command alone skips Omarchy migrations.
- **Treating installed as official, or unknown as absent:** both lie. The first mis-captures AUR packages; the second destroys desired state and misleads Exact.
- **Trusting `-Qqem` when `-Qqen` fails:** it reports every package as foreign.
- **Skipping package operations and applying the rest:** a silent partial reconstruction whose dependents (defaults, themes) may fail later. A scoped Restore already gives users an explicit choice.
- **Handling sudo inside Blueprint** (`sudo -S`, askpass, pre-warming, `NOPASSWD`, running as root): Blueprint must never own administrator credentials or silently hold authority.
- **Raw `pacman -S` instead of `omarchy pkg add`:** it bypasses Omarchy as the package authority and does not remove the elevation or metadata requirements.

## References

- ADR 0017: TUI interactive client architecture and capability parity
- ADR 0020: CLI access to machine-aware policy and capture review
- ADR 0021: Real-Omarchy Reconstruction Assurance in required CI
- `docs/planning/plans/2026-09-25-reconstruction-assurance-v1-implementation-plan.md`
