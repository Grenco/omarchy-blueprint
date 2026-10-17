// Package portabletree defines the portable snapshot of a local directory
// artifact (a local theme or plugin): what Git can store and a fresh clone
// can restore. Capture writes it, Sync verifies it from history, and Detect
// fingerprints it on the live machine, so all three agree.
package portabletree

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The snapshot is:
//
//   - regular files below the directory, with their bytes and their
//     executable bit (owner execute, as Git's core.fileMode reads it);
//   - the directories needed to hold those files, always 0755;
//   - nothing else. Git stores no directory modes, no empty directories and
//     no permission bits beyond executable, so none of those are part of it.
//
// A top-level .git directory is VCS metadata, not content, and is left out.
// Symlinks, special files and nested Git metadata cannot be stored
// faithfully, and a directory with no regular files has nothing to store;
// such an artifact is unportable and must never be recorded as captured.
//
// The fingerprint keeps the original serialization the themes and plugins
// providers used, evaluated over this canonical tree, so an artifact
// captured with ordinary permissions and no empty directories keeps the
// fingerprint it already has.

const (
	portableDirMode  os.FileMode = 0o755
	portableFileMode os.FileMode = 0o644
	portableExecMode os.FileMode = 0o755
)

// unportableError explains why an artifact has no faithful Git representation.
// Its reason is safe to show: it names no paths or contents.
type unportableError struct {
	reason string
	empty  bool // no storable files at all
}

func (e *unportableError) Error() string { return e.reason }

// UnportableReason reports the user-facing reason when err is an
// unportable-snapshot error.
func UnportableReason(err error) (string, bool) {
	var unportable *unportableError
	if errors.As(err, &unportable) {
		return unportable.reason, true
	}
	return "", false
}

type portableEntry struct {
	rel, path string
	dir, exec bool
	size      int64
}

// scan lists the canonical snapshot of root in walk order. It
// returns an *unportableError when the tree cannot be stored faithfully.
func scan(root string) ([]portableEntry, error) {
	var all []portableEntry
	files := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		for _, part := range strings.Split(rel, "/") {
			if strings.EqualFold(part, ".git") {
				return &unportableError{reason: "it contains nested Git metadata, which Git cannot commit"}
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return &unportableError{reason: "it contains a symlink, which snapshots do not support"}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			all = append(all, portableEntry{rel: rel, path: path, dir: true})
		case info.Mode().IsRegular():
			files++
			all = append(all, portableEntry{rel: rel, path: path, exec: info.Mode().Perm()&0o100 != 0, size: info.Size()})
		default:
			return &unportableError{reason: "it contains a special file, which Git cannot store"}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if files == 0 {
		return nil, &unportableError{reason: "it has no files Git can store (it is empty, or holds only empty folders or Git metadata)", empty: true}
	}
	// Keep only directories that hold a file: Git stores no empty ones.
	needed := map[string]bool{}
	for _, entry := range all {
		if entry.dir {
			continue
		}
		for parent := filepath.ToSlash(filepath.Dir(entry.rel)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			needed[parent] = true
		}
	}
	out := all[:0]
	for _, entry := range all {
		if !entry.dir || needed[entry.rel] {
			out = append(out, entry)
		}
	}
	return out, nil
}

func (e portableEntry) mode() os.FileMode {
	switch {
	case e.dir:
		return portableDirMode
	case e.exec:
		return portableExecMode
	default:
		return portableFileMode
	}
}

// Fingerprint is the artifact's recorded Hash: a digest of its
// canonical snapshot, independent of live permissions and empty folders.
func Fingerprint(root string) (string, error) {
	entries, err := scan(root)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, entry := range entries {
		fmt.Fprintf(h, "%s\x00%o\x00", entry.rel, entry.mode().Perm())
		if entry.dir {
			continue
		}
		file, err := os.Open(entry.path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Copy writes source's canonical snapshot to destination, with
// explicit modes so neither live permissions nor the process umask leak
// into what Git will commit.
func Copy(source, destination string) error {
	entries, err := scan(source)
	if err != nil {
		return err
	}
	if err := os.Mkdir(destination, portableDirMode); err != nil {
		return err
	}
	if err := os.Chmod(destination, portableDirMode); err != nil {
		return err
	}
	for _, entry := range entries {
		target := filepath.Join(destination, filepath.FromSlash(entry.rel))
		if entry.dir {
			if err := os.Mkdir(target, portableDirMode); err != nil {
				return err
			}
			if err := os.Chmod(target, portableDirMode); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(entry, target); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(entry portableEntry, target string) error {
	in, err := os.Open(entry.path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.mode())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	modeErr := out.Chmod(entry.mode())
	closeErr := out.Close()
	return errors.Join(copyErr, modeErr, closeErr)
}

// Preserve carries a preserved target's existing profile artifact
// forward unchanged in meaning. Preserve leaves desired state alone, so an
// artifact that is missing or has no storable files (Git keeps nothing for
// it either way) stays as it is rather than failing every other target;
// Sync's historical validation still reports it.
func Preserve(source, destination string) error {
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	err := Copy(source, destination)
	var unportable *unportableError
	if errors.As(err, &unportable) && unportable.empty {
		return os.RemoveAll(destination)
	}
	return err
}
