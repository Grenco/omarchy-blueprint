# ADR 0013: Baseline-Aware Home Configuration Overlay

## Status

Accepted

## Context

Omarchy Blueprint's Config v1 captures four known Hyprland files. That is safe but incomplete for users who configure applications normally and do not maintain a dotfiles repository.

A wholesale snapshot of `~/.config` would capture unchanged Omarchy defaults, runtime/cache state, provider-owned semantic state, update backups, and potentially secrets.

## Decision

Expand Config to a sparse baseline-aware home-configuration overlay in schema 8. The recursive surface is `~/.config/**`; important non-XDG configuration is covered by a curated registry of exact `$HOME`-relative paths such as `.bashrc`, `.zshrc`, `.tmux.conf`, `.XCompose`, and `.gitconfig`. Blueprint never recursively scans all of `$HOME`.

Config captures baseline modifications and explicit tombstones only when modification/deletion intent is established; ADR 0014 defines the conservative provenance and explicit-Include rules. User-added regular config files are captured by default, and unchanged baseline files are omitted.

Paths owned by Resources, Themes, Plugins, Hooks, or Shell are delegated.

Omarchy update backup names matching `xxx.bak.xxx` and shell-extension backups matching `*.backup-*` are built-in ignored noise and are filtered before capture and tombstone classification.

Across Omarchy baseline changes, mergeable text files use a three-way merge from captured baseline → captured desired, applied to the current baseline. Conflicts preserve the target by default. `--force` may resolve supported conflicts only after a recoverable backup.

## Consequences

Profiles remain sparse and Git-friendly while covering ordinary application configuration.

The Config provider becomes more sophisticated: it needs deterministic recursive scanning, policy classification, merge logic, guarded deletion, and stronger provider ownership integration.

Dirty Git preservation remains a separate Resources v2 feature.


## Shell extension boundary

The current Omarchy Zsh and Fish setup helpers implement their interactive-shell preference through user configuration files, primarily `.bashrc`, rather than changing the account login shell.

Config v2 may use installed Zsh/Fish package templates as explicit baselines for the files those setup helpers seed.

Actual login-shell state (`chsh` / `/etc/passwd`) is deferred to a separate semantic feature and is never mutated by Config.
