# Portable Hyprland Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe, file-backed `config` provider that captures and restores customized Omarchy Hyprland configuration while preserving existing package, theme, and plugin behavior.

**Architecture:** Refactor app orchestration behind a stable provider registry and thin adapters, then add schema-v2 config metadata and a dedicated provider for four supported files. Restore uses validated, atomic `FileWrite` operations with backups and dependency-aware reloads; detection, diff, planning, verification, and CLI behavior remain independently testable.

**Tech Stack:** Go, Cobra CLI, TOML profile metadata, SHA-256 content hashes, filesystem staging/atomic rename, existing restore journal and progress reporting.

**Spec:** `/home/graeme/Projects/omarchy-blueprint-config-provider-plan.md`

## Global Constraints

- Supported files in this increment are exactly `hypr/hyprland.lua`, `hypr/bindings.lua`, `hypr/looknfeel.lua`, and `hypr/autostart.lua`; defer monitors, input, shell state, and terminal configuration.
- Preserve existing provider-specific JSON keys and category behavior, including package/theme/plugin output and omission of uncaptured config.
- Never follow or overwrite symlinks, sockets, devices, or directories when capturing or restoring configuration.
- Every mutation must be dry-run safe, journaled, reversible when a backup exists, and independently validated immediately before writing.
- Keep deterministic ordering, actionable errors, continue-on-failure semantics, and clear progress output.
- Add focused unit tests before implementation for each new behavior, then run the complete Go test suite before each commit.

## Task 1: Refactor provider orchestration

**Files:** `internal/app/providers.go` (new), `internal/app/app.go`, existing provider packages, `internal/app/*_test.go`.

- [ ] Define the `stateProvider` interface with `ID`, `Captured`, `Capture`, `Diff`, `Plan`, `Verify`, and `Check` methods using existing model/profile types.
- [ ] Add a deterministic registry in package/theme/plugin/config order (config may initially be a no-op adapter so later tasks can register it without another orchestration rewrite).
- [ ] Implement thin adapters around current package, theme, and plugin providers; retain their state keys and typed behavior.
- [ ] Route category-less capture/status/diff/restore/check through the registry while keeping explicit category commands unchanged.
- [ ] Ensure aggregate capture only sets metadata/state for providers that actually captured data.
- [ ] Add regression tests proving ordering, legacy JSON envelopes, explicit category dispatch, and continue-on-failure behavior.
- [ ] Run `go test ./internal/app/... ./internal/...` and commit: `refactor: centralize state provider orchestration`.

## Task 2: Add safe atomic file restore primitives

**Files:** `internal/content/file.go`, `internal/model/restore.go` (or current model file), `internal/restore/executor.go`, journal/progress tests.

- [ ] Add shared SHA-256 hashing for regular files and helpers that reject symlink/non-regular paths.
- [ ] Add `model.FileWrite{Source, Destination, SourceHash, ExpectedHash, ExpectedMissing, Backup}` and make an operation contain exactly one of Command, Copy, or File.
- [ ] Add `RiskMedium`; classify config writes as medium risk while preserving current low/high behavior.
- [ ] Before mutation, validate source regularity/hash and destination precondition (expected hash or expected missing); reject destination symlinks.
- [ ] Implement temp-file write, flush/fsync, permission preservation where applicable, atomic rename, and backup creation adjacent to the restore journal. Emit `BACKUP_CREATED` and mark the operation reversible when backed up.
- [ ] Preserve dependency blocking and continue independent operations after a failure; dependent operations (including reload) must not run after a failed write.
- [ ] Test backup/restore journal entries, missing-file creation, source corruption, destination drift, destination symlink, atomic replacement, dry-run, and dependent/independent failure behavior.
- [ ] Run `go test ./...` and commit: `feat: add safe atomic file restore operations`.

## Task 3: Add schema-v2 config profile metadata

**Files:** `internal/profile/profile.go`, profile fixtures/tests, `internal/app/app.go` initialization paths.

- [ ] Bump the profile schema constant to 2.
- [ ] Extend `CaptureMeta` with `Config bool`; add `Configs{Files []ConfigFile}`, `ConfigFile{ID, Path, Hash, BaselineHash}`, and `Data.Config` with stable TOML/JSON tags.
- [ ] Store metadata at `config/config.toml`, captured files at `config/files/hypr/...`, and baselines at `config/baseline/hypr/...`.
- [ ] Make `Load` accept schema 1, upgrade in memory to schema 2 with no config state, and make `Save` emit schema 2; new profiles initialize as schema 2.
- [ ] Sort config entries deterministically and round-trip schema-v2 data without losing unknown existing provider state.
- [ ] Add migration, round-trip, schema validation, and old-profile compatibility tests.
- [ ] Run `go test ./internal/profile/... ./...` and commit: `feat: add schema v2 config metadata`.

## Task 4: Implement config detection and capture

**Files:** `internal/providers/config/provider.go`, `internal/providers/config/provider_test.go`, dependency wiring in `internal/app`.

- [ ] Define `Provider{UserRoot, BaselineRoot, ProfileDir, Specs}` and `Spec{ID, Path}` plus the four exact default specs.
- [ ] Resolve defaults from `~/.config` and `${OMARCHY_PATH:-/usr/share/omarchy}/config`; expose `Dependency.ConfigDirs` for testable/app-specific roots.
- [ ] Detect each file as default, customized, missing, or unsupported using baseline/user hashes; reject baseline symlinks and non-regular files.
- [ ] Capture only customized files, copying both user content and baseline bytes through staging/atomic profile replacement; reject user symlinks/special files.
- [ ] Remove stale captured files and metadata when a file is reset to baseline; leave clean defaults uncaptured.
- [ ] Test defaults, customized, missing, unsupported, symlink/special-file rejection, stale removal, deterministic metadata, and profile layout.
- [ ] Run `go test ./internal/providers/config/... ./...` and commit: `feat: capture customized hyprland config`.

## Task 5: Implement config diff, safe plan, and verification

**Files:** `internal/providers/config/provider.go`, config tests, restore-plan/model tests.

- [ ] Emit add/modify/remove changes using `ChangeModify`; include baseline and desired hashes in meaningful change details.
- [ ] Build `FileWrite` plans using these safety rules: no-op when desired matches; write a missing target only when current baseline still matches captured baseline; replace a current-baseline target only when baseline is unchanged; otherwise skip with migration-required; never overwrite user drift; mark absent current config unsupported.
- [ ] Set medium risk and attach stable operation IDs/dependencies; append one final `hyprctl reload` operation when writes exist, dependent on all writes.
- [ ] Verify desired captured files exist and hash-match; report extras as drift without failing verification.
- [ ] Test unchanged, add/modify/remove, baseline changes, target drift, unsupported state, extras, operation dependencies, reload gating, and verification asymmetry.
- [ ] Run `go test ./...` and commit: `feat: plan and verify safe hyprland config restore`.

## Task 6: Integrate CLI commands and aggregate flows

**Files:** `internal/app/app.go`, command wiring, CLI/integration tests, help/snapshot fixtures.

- [ ] Add explicit `capture config`, `status config`, `diff config`, `restore config`, and `check config` commands with the same profile/approval/dry-run conventions as other providers.
- [ ] Include config in category-less capture/status/diff/restore/check through the registry; preserve “nothing to restore” and exit-code semantics.
- [ ] Show progress for each config operation, medium-risk approval text, skipped safety reasons, backup paths, and final success/failure summary.
- [ ] Add integration coverage for capture, reset, status drift, dry-run, restore with backup/journal, aggregate restore, and reload failure blocking.
- [ ] Run `go test ./...` plus representative CLI smoke commands and commit: `feat: integrate config provider into cli`.

## Task 7: Documentation, ADR, and acceptance checklist

**Files:** `docs/adr/0006-portable-hyprland-config.md`, `README.md`, `ROADMAP.md`, existing manual test/checklist docs, stale plugin documentation.

- [ ] Document scope, profile layout, baseline safety model, backups/journal recovery, migration-required skips, and deferred config files in ADR 0006.
- [ ] Update README command/reference sections and ROADMAP status; remove or correct stale plugin-only aggregate-flow notes.
- [ ] Add a manual acceptance checklist covering same-machine capture/status, reset-and-restore, baseline drift, user drift, symlink rejection, backup recovery, aggregate restore, and cross-machine validation.
- [ ] Run documentation link/format checks and `go test ./...`; commit: `docs: document portable hyprland config provider`.

## Final verification and handoff

- [ ] Review `git diff --check`, `go vet ./...`, and the full test suite.
- [ ] Inspect the final diff for unintended profile/API changes and confirm all new files are tracked.
- [ ] Push `feature/portable-config-provider`, open a PR against `main`, and include test output plus the manual acceptance checklist in the description.
