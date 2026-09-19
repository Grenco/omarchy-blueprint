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
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
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
			managed, err := p.managedInboundLink(path)
			if err != nil {
				return State{}, err
			}
			if managed {
				continue
			}
			unmanaged, err := unmanagedHook(entry.Name(), path)
			if err != nil {
				return State{}, err
			}
			state.Unmanaged = append(state.Unmanaged, unmanaged)
			continue
		}
		if entry.IsDir() {
			if !strings.HasSuffix(entry.Name(), ".d") {
				continue
			}
			children, unmanaged, err := p.detectDirectory(entry.Name(), path)
			if err != nil {
				return State{}, err
			}
			state.Items = append(state.Items, children...)
			state.Unmanaged = append(state.Unmanaged, unmanaged...)
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
	sort.Slice(state.Unmanaged, func(i, j int) bool { return state.Unmanaged[i].Path < state.Unmanaged[j].Path })
	return state, nil
}

func (p Provider) detectDirectory(dir, path string) ([]DetectedHook, []UnmanagedHook, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, nil, err
	}
	var hooks []DetectedHook
	var unmanagedHooks []UnmanagedHook
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".sample") {
			continue
		}
		rel := dir + "/" + name
		if entry.Type()&os.ModeSymlink != 0 {
			managed, err := p.managedInboundLink(filepath.Join(path, name))
			if err != nil {
				return nil, nil, err
			}
			if managed {
				continue
			}
			unmanaged, err := unmanagedHook(rel, filepath.Join(path, name))
			if err != nil {
				return nil, nil, err
			}
			unmanagedHooks = append(unmanagedHooks, unmanaged)
			continue
		}
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		hook, err := detectFile(rel, filepath.Join(path, name))
		if err != nil {
			return nil, nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, unmanagedHooks, nil
}

func (p Provider) managedInboundLink(path string) (bool, error) {
	if p.HomeDir == "" {
		return false, nil
	}
	return resourcesprovider.IsTrackedInboundLink(p.HomeDir, path, p.Resources)
}

func (p Provider) reservesInboundLink(path string) (bool, error) {
	if p.HomeDir == "" {
		return false, nil
	}
	return resourcesprovider.IsTrackedInboundLinkPath(p.HomeDir, path, p.Resources)
}

func unmanagedHook(rel, path string) (UnmanagedHook, error) {
	if err := ValidatePath(rel); err != nil {
		return UnmanagedHook{}, err
	}
	target, err := os.Readlink(path)
	if err != nil {
		return UnmanagedHook{}, err
	}
	_, err = os.Stat(path)
	return UnmanagedHook{Path: rel, Target: target, Broken: errors.Is(err, os.ErrNotExist)}, nil
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

// Capture writes inert snapshots, keeping executable permission only in
// metadata. It merges live detection into saved's desired state per the
// Capture merge transition table, driven by enabled's resolved per-path
// decision: a freshly captured (enabled+present) path is staged from the
// live raw bytes just read by Detect; a preserved (disabled, or
// enabled-but-no-longer-present) path keeps its existing snapshot exactly as
// already captured, copied from the prior generation rather than re-read
// live, so metadata and the on-disk snapshot never drift; a path enabled but
// no longer present, with prior desired state, becomes a desired-absence
// tombstone rather than silently vanishing.
func (p Provider) Capture(state State, saved profile.Hooks, enabled func(path string) bool) (profile.Hooks, error) {
	if p.ProfileDir == "" {
		return profile.Hooks{}, errors.New("profile directory is required to capture hooks")
	}
	savedByPath := savedMap(saved.Items)
	currentByPath := currentMap(state.Items)
	savedAbsentByPath := savedMap(saved.Absent)

	paths := map[string]bool{}
	for path := range savedByPath {
		paths[path] = true
	}
	for path := range currentByPath {
		paths[path] = true
	}
	for path := range savedAbsentByPath {
		paths[path] = true
	}

	type stagedItem struct {
		path   string
		fresh  *DetectedHook
		source string
	}
	captured := profile.Hooks{}
	var toStage []stagedItem
	for path := range paths {
		savedItem, wasPresent := savedByPath[path]
		currentItem, isPresent := currentByPath[path]
		_, wasAbsent := savedAbsentByPath[path]
		isEnabled := enabled(path)
		switch transition(wasPresent, wasAbsent, isPresent, isEnabled) {
		case transitionPresent:
			if isEnabled && isPresent {
				captured.Items = append(captured.Items, profile.Hook{Path: currentItem.Path, Hash: currentItem.Hash, Mode: currentItem.Mode})
				item := currentItem
				toStage = append(toStage, stagedItem{path: path, fresh: &item})
			} else {
				captured.Items = append(captured.Items, savedItem)
				toStage = append(toStage, stagedItem{path: path, source: filepath.Join(p.ProfileDir, "hooks", "files", filepath.FromSlash(path))})
			}
		case transitionAbsent:
			if wasAbsent {
				captured.Absent = append(captured.Absent, savedAbsentByPath[path])
			} else {
				captured.Absent = append(captured.Absent, savedItem)
			}
		}
	}
	if err := ValidateMetadata(captured.Items); err != nil {
		return profile.Hooks{}, err
	}
	sort.Slice(captured.Items, func(i, j int) bool { return captured.Items[i].Path < captured.Items[j].Path })
	sort.Slice(captured.Absent, func(i, j int) bool { return captured.Absent[i].Path < captured.Absent[j].Path })

	parent := filepath.Join(p.ProfileDir, "hooks")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return profile.Hooks{}, err
	}
	staging, err := os.MkdirTemp(parent, ".capture-*")
	if err != nil {
		return profile.Hooks{}, err
	}
	defer os.RemoveAll(staging)
	for _, s := range toStage {
		target := filepath.Join(staging, "files", filepath.FromSlash(s.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return profile.Hooks{}, err
		}
		if s.fresh != nil {
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				return profile.Hooks{}, err
			}
			if err := out.Chmod(0o644); err != nil {
				out.Close()
				return profile.Hooks{}, err
			}
			if _, err := out.Write(s.fresh.Raw); err != nil {
				out.Close()
				return profile.Hooks{}, err
			}
			if err := out.Close(); err != nil {
				return profile.Hooks{}, err
			}
			sum := sha256.Sum256(s.fresh.Raw)
			if hex.EncodeToString(sum[:]) != s.fresh.Hash {
				return profile.Hooks{}, fmt.Errorf("hook content hash mismatch for %q", s.path)
			}
			continue
		}
		hash, err := copyHookSnapshot(s.source, target)
		if err != nil {
			return profile.Hooks{}, fmt.Errorf("preserve hook %q: %w", s.path, err)
		}
		if hash != savedByPath[s.path].Hash {
			return profile.Hooks{}, fmt.Errorf("preserve hook %q: snapshot hash mismatch", s.path)
		}
	}
	if err := swapFiles(staging, filepath.Join(parent, "files")); err != nil {
		return profile.Hooks{}, err
	}
	return captured, nil
}

func copyHookSnapshot(source, target string) (string, error) {
	f, _, err := content.OpenRegularFile(source)
	if err != nil {
		return "", err
	}
	defer f.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(target, 0o644); err != nil {
		return "", err
	}
	return content.HashRegularFile(target)
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

// transitionResult is the Capture merge outcome for one target: whether it
// belongs in the new desired-present set, the new desired-absent (tombstone)
// set, or neither (still unmanaged). See internal/providers/packages and
// internal/providers/themes' identical helper: each provider owns its own
// merge/preserve behavior, so this small, stable, pure table is duplicated
// rather than shared.
type transitionResult int

const (
	transitionNone transitionResult = iota
	transitionPresent
	transitionAbsent
)

// transition implements the Capture merge invariant table generically.
// wasPresent/wasAbsent describe the previous desired state (both false means
// "unknown": never captured); isPresent is the current live state; enabled
// is the resolved Capture decision for this target.
func transition(wasPresent, wasAbsent, isPresent, enabled bool) transitionResult {
	if !enabled {
		switch {
		case wasPresent:
			return transitionPresent
		case wasAbsent:
			return transitionAbsent
		default:
			return transitionNone
		}
	}
	if isPresent {
		return transitionPresent
	}
	if wasPresent || wasAbsent {
		return transitionAbsent
	}
	return transitionNone
}

// PrepareStopManagingArtifact stages the removal of one hook's captured
// snapshot file without deleting it yet: the file is renamed to a sibling
// backup, and the caller defers the actual deletion until it knows the
// profile save that forgets the hook's metadata has also succeeded
// (FinalizeCapture), or undoes the rename if it has not (RollbackCapture).
// A hook with no snapshot file (already removed) is a no-op.
func (p *Provider) PrepareStopManagingArtifact(path string) error {
	source := filepath.Join(p.ProfileDir, "hooks", "files", filepath.FromSlash(path))
	if _, err := os.Lstat(source); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	backup := source + ".stop-managing-backup"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(source, backup); err != nil {
		return err
	}
	p.stopManagingDestination, p.stopManagingBackup, p.stopManagingPending = source, backup, true
	return nil
}

func (p *Provider) CommitCapture() error { return nil }
func (p *Provider) FinalizeCapture() error {
	if !p.stopManagingPending {
		return nil
	}
	err := os.Remove(p.stopManagingBackup)
	p.stopManagingDestination, p.stopManagingBackup, p.stopManagingPending = "", "", false
	return err
}
func (p *Provider) RollbackCapture() error {
	if !p.stopManagingPending {
		return nil
	}
	if err := os.Remove(p.stopManagingDestination); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(p.stopManagingBackup); err == nil {
		if err := os.Rename(p.stopManagingBackup, p.stopManagingDestination); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	p.stopManagingDestination, p.stopManagingBackup, p.stopManagingPending = "", "", false
	return nil
}
