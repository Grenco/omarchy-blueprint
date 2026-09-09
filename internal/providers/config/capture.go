package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type CaptureResult struct {
	State   profile.Configs
	Scan    ScanSummary
	Changes []model.Change
}

// Capture writes both sparse snapshot trees from one staged payload. Snapshots
// are re-hashed after copying, so metadata always describes persisted bytes.
func (p Provider) Capture(saved profile.Configs) (CaptureResult, error) {
	if p.ProfileDir == "" {
		return CaptureResult{}, fmt.Errorf("profile directory is required to capture config")
	}
	scan, err := p.Scan(saved)
	if err != nil {
		return CaptureResult{}, err
	}
	parent := filepath.Join(p.ProfileDir, "config")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return CaptureResult{}, err
	}
	stage, err := os.MkdirTemp(parent, ".capture-*")
	if err != nil {
		return CaptureResult{}, err
	}
	defer os.RemoveAll(stage)
	state := profile.Configs{Excluded: append([]string(nil), saved.Excluded...)}
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
	hash, err := content.HashRegularFile(target)
	if err != nil {
		return "", "", err
	}
	return hash, fmt.Sprintf("%04o", info.Mode().Perm()), nil
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
