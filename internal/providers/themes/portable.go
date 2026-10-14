package themes

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

// A local theme's portable snapshot is what Git can store and a fresh clone
// can restore. Capture writes it, Sync verifies it from history, and
// Detect fingerprints it on the live machine, so all three agree:
//
//   - regular files below the theme directory, with their bytes and their
//     executable bit (owner execute, as Git's core.fileMode reads it);
//   - the directories needed to hold those files, always 0755;
//   - nothing else. Git stores no directory modes, no empty directories and
//     no permission bits beyond executable, so none of those are part of it.
//
// A top-level .git directory is VCS metadata, not theme content, and is
// left out. Symlinks, special files and nested Git metadata cannot be
// stored faithfully, and a theme with no regular files has nothing to
// store; such a theme is unportable and must never be recorded as captured.
//
// The fingerprint keeps the original serialization, evaluated over this
// canonical tree, so a theme captured with ordinary permissions and no
// empty directories keeps the fingerprint it already has.

const (
	portableDirMode  os.FileMode = 0o755
	portableFileMode os.FileMode = 0o644
	portableExecMode os.FileMode = 0o755
)

// unportableError explains why a theme has no faithful Git representation.
// Its reason is safe to show: it names no paths or contents.
type unportableError struct{ reason string }

func (e *unportableError) Error() string { return e.reason }

// unportableReason reports the user-facing reason when err is an
// unportable-snapshot error.
func unportableReason(err error) (string, bool) {
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

// scanPortable lists the canonical snapshot of root in walk order. It
// returns an *unportableError when the tree cannot be stored faithfully.
func scanPortable(root string) ([]portableEntry, error) {
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
				return &unportableError{"it contains nested Git metadata, which Git cannot commit"}
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return &unportableError{"it contains a symlink, which theme snapshots do not support"}
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
			return &unportableError{"it contains a special file, which Git cannot store"}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if files == 0 {
		return nil, &unportableError{"it has no files Git can store (it is empty, or holds only empty folders or Git metadata)"}
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

// portableFingerprint is the theme's recorded Hash: a digest of its
// canonical snapshot, independent of live permissions and empty folders.
func portableFingerprint(root string) (string, error) {
	entries, err := scanPortable(root)
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

// copyPortable writes source's canonical snapshot to destination, with
// explicit modes so neither live permissions nor the process umask leak
// into what Git will commit.
func copyPortable(source, destination string) error {
	entries, err := scanPortable(source)
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
		if err := copyPortableFile(entry, target); err != nil {
			return err
		}
	}
	return nil
}

func copyPortableFile(entry portableEntry, target string) error {
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

// preservePortable carries a preserved theme's existing profile artifact
// forward unchanged in meaning. Preserve leaves desired state alone, so an
// artifact that is missing or has no storable files (Git keeps nothing for
// it either way) stays as it is rather than failing every other theme;
// Sync's historical validation still reports it.
func preservePortable(source, destination string) error {
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	err := copyPortable(source, destination)
	if reason, unportable := unportableReason(err); unportable && strings.HasPrefix(reason, "it has no files") {
		return os.RemoveAll(destination)
	}
	return err
}
