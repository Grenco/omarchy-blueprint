package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type CaptureResult struct {
	State   profile.Configs `json:"state"`
	Scan    ScanSummary     `json:"scan"`
	Changes []model.Change  `json:"-"`
}

// beforeStage is used by package tests to model a source changing after scan.
var beforeStage func()

// Capture writes both sparse snapshot trees from one staged payload. Snapshots
// are re-hashed after copying, so metadata always describes persisted bytes.
// enabled resolves the per-path effective Capture decision, already
// ancestor-aware (a child file's own explicit rule outranks its directory's,
// per the hierarchical policy resolver) -- Capture itself does not
// re-implement hierarchy. A disabled path's existing desired state (whether
// a captured file or an existing deletion tombstone) is frozen exactly as
// saved, artifact included, rather than being translated into Config
// Excluded: Excluded prunes a path from management outright, Capture
// Disabled only freezes what is already desired.
func (p Provider) Capture(saved profile.Configs, enabled func(path string) bool) (CaptureResult, error) {
	if p.ProfileDir == "" {
		return CaptureResult{}, fmt.Errorf("profile directory is required to capture config")
	}
	parent := filepath.Join(p.ProfileDir, "config")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return CaptureResult{}, err
	}
	lockPath := filepath.Join(parent, ".capture.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return CaptureResult{}, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return CaptureResult{}, fmt.Errorf("config capture already in progress: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	scan, err := p.ScanForCapture(saved)
	if err != nil {
		return CaptureResult{}, err
	}
	if beforeStage != nil {
		beforeStage()
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return CaptureResult{}, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".capture-stage-") || entry.Name() == ".files-capture-previous" || entry.Name() == ".baseline-capture-previous" {
			if err := os.RemoveAll(filepath.Join(parent, entry.Name())); err != nil {
				return CaptureResult{}, err
			}
		}
	}
	stage, err := os.MkdirTemp(parent, ".capture-stage-*")
	if err != nil {
		return CaptureResult{}, err
	}
	defer os.RemoveAll(stage)
	excluded, err := normalizeExclusions(saved.Excluded)
	if err != nil {
		return CaptureResult{}, err
	}
	included, err := normalizeExclusions(saved.Included)
	if err != nil {
		return CaptureResult{}, err
	}
	savedFiles := make(map[string]profile.ConfigFile, len(saved.Files))
	for _, f := range saved.Files {
		savedFiles[f.Path] = f
	}
	savedDeletes := make(map[string]profile.ConfigDelete, len(saved.Deletes))
	for _, d := range saved.Deletes {
		savedDeletes[d.Path] = d
	}
	state := profile.Configs{Included: included, Excluded: excluded}
	// preserve freezes whatever this path is already desired as (a captured
	// file or an existing deletion tombstone), artifact included, without
	// touching its metadata. A path with no existing desired state at all
	// stays unmanaged: Capture Disabled preserves, it does not adopt.
	preserve := func(path string) error {
		if f, ok := savedFiles[path]; ok {
			if _, _, err := copySnapshot(stage, "files", path, existingSnapshotSource(parent, "files", path)); err != nil {
				return fmt.Errorf("preserve %s: %w", path, err)
			}
			if f.BaselineHash != "" {
				if _, _, err := copySnapshot(stage, "baseline", path, existingSnapshotSource(parent, "baseline", path)); err != nil {
					return fmt.Errorf("preserve baseline %s: %w", path, err)
				}
			}
			state.Files = append(state.Files, f)
			return nil
		}
		if d, ok := savedDeletes[path]; ok {
			if _, _, err := copySnapshot(stage, "baseline", path, existingSnapshotSource(parent, "baseline", path)); err != nil {
				return fmt.Errorf("preserve baseline %s: %w", path, err)
			}
			state.Deletes = append(state.Deletes, d)
		}
		return nil
	}
	// actionable holds only the candidates whose classification can produce a
	// fresh captured value this run.
	actionable := make(map[string]Candidate, len(scan.Candidates))
	// byPath holds every scan candidate, actionable or not, so a previously
	// desired path's classification can still be inspected below.
	byPath := make(map[string]Candidate, len(scan.Candidates))
	for _, c := range scan.Candidates {
		byPath[c.Path] = c
		switch c.Classification {
		case ConfigAdded, ConfigModifiedBaseline, ConfigDeletedBaseline:
			actionable[c.Path] = c
		}
	}
	previouslyDesired := make(map[string]bool, len(savedFiles)+len(savedDeletes))
	for path := range savedFiles {
		previouslyDesired[path] = true
	}
	for path := range savedDeletes {
		previouslyDesired[path] = true
	}
	paths := make(map[string]bool, len(actionable)+len(previouslyDesired))
	for path := range actionable {
		paths[path] = true
	}
	for path := range previouslyDesired {
		paths[path] = true
	}
	orderedPaths := make([]string, 0, len(paths))
	for path := range paths {
		orderedPaths = append(orderedPaths, path)
	}
	sort.Strings(orderedPaths)
	for _, path := range orderedPaths {
		c, hasCandidate := actionable[path]
		if !hasCandidate {
			if !previouslyDesired[path] {
				continue
			}
			if existing, scanned := byPath[path]; scanned {
				switch existing.Classification {
				case ConfigDelegated, ConfigExcluded:
					// An intentional ownership-management transition, not a
					// safety freeze: the path is handed off to a stronger
					// owner (Delegated) or the user explicitly said to
					// leave it alone (Excluded). Capture always drops its
					// desired state for these, regardless of enabled --
					// freezing it here would mean Capture Preserve silently
					// adopting the delegated/excluded meaning, which must
					// never happen.
					continue
				case ConfigUnchangedBaseline:
					// Nothing currently diverges from the baseline to
					// capture. Enabled correctly drops it (there is nothing
					// new to adopt); disabled freezes the stale desired
					// value exactly as before.
					if !enabled(path) {
						if err := preserve(path); err != nil {
							return CaptureResult{}, err
						}
					}
					continue
				default:
					// Every other classification (Sensitive, Volatile,
					// Oversized, Unsupported, ambiguous, an unmanaged
					// symlink, ...) means the current live bytes are unsafe
					// or otherwise unreadable right now. Safety blocks
					// updating from those unsafe bytes; it must never erase
					// already-safe remembered desired state, so this
					// preserves unconditionally, independent of enabled --
					// capturing fresh would be unsafe regardless of policy.
					if err := preserve(path); err != nil {
						return CaptureResult{}, err
					}
					continue
				}
			}
			// No scan candidate at all: genuinely missing, unless the path
			// (or an ancestor) has been handed off to a stronger owner,
			// such as a newly tracked Resource -- the walk itself prunes a
			// delegated directory before its children are ever classified,
			// so the same ownership check the walk uses is consulted
			// directly here.
			abs, err := p.absoluteUserPath(path)
			if err != nil {
				return CaptureResult{}, err
			}
			if p.delegated(abs) {
				continue
			}
			if !enabled(path) {
				if err := preserve(path); err != nil {
					return CaptureResult{}, err
				}
			}
			continue
		}
		if !enabled(path) {
			if err := preserve(path); err != nil {
				return CaptureResult{}, err
			}
			continue
		}
		switch c.Classification {
		case ConfigAdded, ConfigModifiedBaseline:
			user, err := p.absoluteUserPath(c.Path)
			if err != nil {
				return CaptureResult{}, err
			}
			hash, mode, err := copySnapshot(stage, "files", c.Path, user)
			if err != nil {
				return CaptureResult{}, fmt.Errorf("capture %s: %w", c.Path, err)
			}
			if hash != c.UserHash || mode != c.UserMode {
				return CaptureResult{}, fmt.Errorf("capture %s: source changed since scan", c.Path)
			}
			file := profile.ConfigFile{Path: c.Path, Hash: hash, Mode: mode}
			if c.Classification == ConfigModifiedBaseline {
				base, err := p.absoluteBaselinePath(c.Path)
				if err != nil {
					return CaptureResult{}, err
				}
				baseHash, baseMode, err := copySnapshot(stage, "baseline", c.Path, base)
				if err != nil {
					return CaptureResult{}, fmt.Errorf("capture baseline %s: %w", c.Path, err)
				}
				if baseHash != c.BaselineHash || baseMode != c.BaselineMode {
					return CaptureResult{}, fmt.Errorf("capture baseline %s: source changed since scan", c.Path)
				}
				file.BaselineHash, file.BaselineMode = baseHash, baseMode
			}
			state.Files = append(state.Files, file)
		case ConfigDeletedBaseline:
			base, err := p.absoluteBaselinePath(c.Path)
			if err != nil {
				return CaptureResult{}, err
			}
			hash, mode, err := copySnapshot(stage, "baseline", c.Path, base)
			if err != nil {
				return CaptureResult{}, fmt.Errorf("capture baseline %s: %w", c.Path, err)
			}
			if hash != c.BaselineHash || mode != c.BaselineMode {
				return CaptureResult{}, fmt.Errorf("capture baseline %s: source changed since scan", c.Path)
			}
			state.Deletes = append(state.Deletes, profile.ConfigDelete{Path: c.Path, BaselineHash: hash, BaselineMode: mode})
		}
	}
	sort.Slice(state.Files, func(i, j int) bool { return state.Files[i].Path < state.Files[j].Path })
	sort.Slice(state.Deletes, func(i, j int) bool { return state.Deletes[i].Path < state.Deletes[j].Path })
	if err := swapCaptureTrees(stage, parent); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{State: state, Scan: scan, Changes: DiffConfigs(saved, state)}, nil
}

func copySnapshot(stage, tree, logical, source string) (string, string, error) {
	sensitive, err := hasSensitiveContent(source)
	if err != nil {
		return "", "", err
	}
	if sensitive {
		return "", "", fmt.Errorf("sensitive content")
	}
	f, info, err := content.OpenRegularFile(source)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	target := filepath.Join(stage, tree, filepath.FromSlash(logical))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", "", err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return "", "", err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		return "", "", err
	}
	if err := out.Close(); err != nil {
		return "", "", err
	}
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return "", "", err
	}
	sensitive, err = hasSensitiveContent(target)
	if err != nil {
		return "", "", err
	}
	if sensitive {
		return "", "", fmt.Errorf("sensitive content")
	}
	hash, err := content.HashRegularFile(target)
	if err != nil {
		return "", "", err
	}
	return hash, fmt.Sprintf("%04o", info.Mode().Perm()), nil
}

func normalizeExclusions(exclusions []string) ([]string, error) {
	if len(exclusions) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(exclusions))
	for _, exclusion := range exclusions {
		path, err := profile.NormalizeConfigPath(exclusion)
		if err != nil {
			return nil, err
		}
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}

func swapCaptureTrees(stage, parent string) error {
	// Both trees are swapped as a unit with rollback if the second rename fails.
	// The profile metadata is written later by profile.Save, so it is not moved.
	old := map[string]string{}
	installed := []string{}
	for _, name := range []string{"files", "baseline"} {
		staged := filepath.Join(stage, name)
		if _, err := os.Lstat(staged); os.IsNotExist(err) {
			if err := os.Mkdir(staged, 0o700); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		target := filepath.Join(parent, name)
		backup := filepath.Join(parent, "."+name+"-capture-previous")
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			if err := os.Rename(target, backup); err != nil {
				return err
			}
			old[name] = backup
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(staged, target); err != nil {
			for i := len(installed) - 1; i >= 0; i-- {
				_ = os.RemoveAll(filepath.Join(parent, installed[i]))
				_ = os.Rename(old[installed[i]], filepath.Join(parent, installed[i]))
			}
			if backup, ok := old[name]; ok {
				_ = os.Rename(backup, target)
			}
			return err
		}
		installed = append(installed, name)
	}
	for _, backup := range old {
		if err := os.RemoveAll(backup); err != nil {
			return err
		}
	}
	return nil
}

// existingSnapshotSource resolves the absolute path preserve should read an
// existing snapshot from. A path saved before the schema-8 HOME-relative
// rewrite (see profile.migrateLegacyConfigPaths) has its logical metadata
// path already rewritten to the ".config/"-prefixed form on load, but the
// on-disk snapshot tree itself is never relocated to match -- loading must
// never rewrite anything on disk, only the in-memory metadata -- so a
// legacy snapshot can still be found at its pre-rewrite path. Preserving a
// path whose current-style snapshot is missing falls back to that legacy
// location rather than failing outright.
func existingSnapshotSource(parent, tree, path string) string {
	current := filepath.Join(parent, tree, filepath.FromSlash(path))
	if _, err := os.Lstat(current); err == nil {
		return current
	}
	if legacy, ok := strings.CutPrefix(path, ".config/"); ok {
		if legacySource := filepath.Join(parent, tree, filepath.FromSlash(legacy)); snapshotExists(legacySource) {
			return legacySource
		}
	}
	return current
}

func snapshotExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
