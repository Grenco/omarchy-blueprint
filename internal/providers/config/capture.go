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
func (p Provider) Capture(saved profile.Configs) (CaptureResult, error) {
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
	state := profile.Configs{Included: included, Excluded: excluded}
	for _, c := range scan.Candidates {
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
