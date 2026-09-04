package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// Detect captures only the flat and immediate .d hook forms used at runtime.
func (p Provider) Detect() (State, error) {
	info, err := os.Lstat(p.UserDir)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return State{}, fmt.Errorf("hooks root is a symlink: %s", p.UserDir)
	}
	if !info.IsDir() {
		return State{}, fmt.Errorf("hooks root is not a directory: %s", p.UserDir)
	}
	entries, err := os.ReadDir(p.UserDir)
	if err != nil {
		return State{}, err
	}
	state := State{}
	for _, entry := range entries {
		path := filepath.Join(p.UserDir, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			return State{}, fmt.Errorf("runtime hook %s is a symlink", entry.Name())
		}
		if entry.IsDir() {
			if !strings.HasSuffix(entry.Name(), ".d") {
				continue
			}
			children, err := p.detectDirectory(entry.Name(), path)
			if err != nil {
				return State{}, err
			}
			state.Items = append(state.Items, children...)
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return State{}, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		hook, err := detectFile(entry.Name(), path)
		if err != nil {
			return State{}, err
		}
		state.Items = append(state.Items, hook)
	}
	sort.Slice(state.Items, func(i, j int) bool { return state.Items[i].Path < state.Items[j].Path })
	return state, nil
}

func (p Provider) detectDirectory(dir, path string) ([]DetectedHook, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var hooks []DetectedHook
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".sample") {
			continue
		}
		rel := dir + "/" + name
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("runtime hook %s is a symlink", rel)
		}
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		hook, err := detectFile(rel, filepath.Join(path, name))
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, nil
}

func detectFile(rel, path string) (DetectedHook, error) {
	if err := ValidatePath(rel); err != nil {
		return DetectedHook{}, err
	}
	f, info, err := content.OpenRegularFile(path)
	if err != nil {
		return DetectedHook{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		return DetectedHook{}, err
	}
	sum := sha256.Sum256(raw)
	mode, err := FormatMode(info.Mode())
	if err != nil {
		return DetectedHook{}, err
	}
	return DetectedHook{Path: rel, Hash: hex.EncodeToString(sum[:]), Mode: mode, Raw: raw}, nil
}

// Capture writes inert snapshots, keeping executable permission only in metadata.
func (p Provider) Capture(state State) (profile.Hooks, error) {
	if p.ProfileDir == "" {
		return profile.Hooks{}, errors.New("profile directory is required to capture hooks")
	}
	captured := profile.Hooks{Items: make([]profile.Hook, 0, len(state.Items))}
	for _, item := range state.Items {
		captured.Items = append(captured.Items, profile.Hook{Path: item.Path, Hash: item.Hash, Mode: item.Mode})
	}
	if err := ValidateMetadata(captured.Items); err != nil {
		return profile.Hooks{}, err
	}
	parent := filepath.Join(p.ProfileDir, "hooks")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return profile.Hooks{}, err
	}
	staging, err := os.MkdirTemp(parent, ".capture-*")
	if err != nil {
		return profile.Hooks{}, err
	}
	defer os.RemoveAll(staging)
	for _, item := range state.Items {
		target := filepath.Join(staging, "files", filepath.FromSlash(item.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return profile.Hooks{}, err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return profile.Hooks{}, err
		}
		if err := out.Chmod(0o644); err != nil {
			out.Close()
			return profile.Hooks{}, err
		}
		if _, err := out.Write(item.Raw); err != nil {
			out.Close()
			return profile.Hooks{}, err
		}
		if err := out.Close(); err != nil {
			return profile.Hooks{}, err
		}
		sum := sha256.Sum256(item.Raw)
		if hex.EncodeToString(sum[:]) != item.Hash {
			return profile.Hooks{}, fmt.Errorf("hook content hash mismatch for %q", item.Path)
		}
	}
	sort.Slice(captured.Items, func(i, j int) bool { return captured.Items[i].Path < captured.Items[j].Path })
	if err := swapFiles(staging, filepath.Join(parent, "files")); err != nil {
		return profile.Hooks{}, err
	}
	return captured, nil
}

func swapFiles(staging, destination string) error {
	staged := filepath.Join(staging, "files")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		return err
	}
	old := filepath.Join(filepath.Dir(destination), ".files-previous")
	_ = os.RemoveAll(old)
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, old); err != nil {
			return err
		}
	}
	if err := os.Rename(staged, destination); err != nil {
		_ = os.Rename(old, destination)
		return err
	}
	return os.RemoveAll(old)
}

// Check validates metadata and ensures the snapshot tree contains no extras.
func (p Provider) Check(saved profile.Hooks) error {
	if err := ValidateMetadata(saved.Items); err != nil {
		return err
	}
	expected := make(map[string]profile.Hook, len(saved.Items))
	for _, item := range saved.Items {
		expected[item.Path] = item
		snapshot := filepath.Join(p.ProfileDir, "hooks", "files", filepath.FromSlash(item.Path))
		hash, err := content.HashRegularFile(snapshot)
		if err != nil {
			return fmt.Errorf("hook snapshot %q: %w", item.Path, err)
		}
		if hash != item.Hash {
			return fmt.Errorf("hook snapshot hash mismatch for %q (%s != %s)", item.Path, hash, item.Hash)
		}
	}
	root := filepath.Join(p.ProfileDir, "hooks", "files")
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) && len(saved.Items) == 0 {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("hook snapshot is a symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("hook snapshot is not a regular file: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := ValidatePath(rel); err != nil {
			return fmt.Errorf("invalid hook snapshot path %q: %w", rel, err)
		}
		if _, ok := expected[rel]; !ok {
			return fmt.Errorf("orphan hook snapshot %q", rel)
		}
		return nil
	})
}
