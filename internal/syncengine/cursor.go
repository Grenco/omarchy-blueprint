// Package syncengine defines semantic reconciliation and machine-local sync state.
package syncengine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

const CursorFormatVersion = 1

var commitID = regexp.MustCompile(`^[0-9a-f]{40}$`)
var remoteID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Cursor records verified settlement, not the latest inspected revision.
type Cursor struct {
	Version           int    `toml:"version"`
	Machine           string `toml:"machine"`
	Revision          string `toml:"revision"`
	Branch            string `toml:"branch"`
	Upstream          string `toml:"upstream"`
	RemoteFingerprint string `toml:"remote_fingerprint"`
}

func (c Cursor) validate() error {
	if c.Version != CursorFormatVersion {
		return errors.New("unsupported sync cursor format")
	}
	if machine.ValidateName(c.Machine) != nil {
		return errors.New("invalid sync cursor machine")
	}
	if !commitID.MatchString(c.Revision) {
		return errors.New("invalid sync cursor revision (expected lowercase SHA-1)")
	}
	if !validRefIdentity(c.Branch) || !validRefIdentity(c.Upstream) {
		return errors.New("invalid sync cursor branch/upstream")
	}
	if !remoteID.MatchString(c.RemoteFingerprint) {
		return errors.New("invalid sync cursor remote fingerprint")
	}
	return nil
}

func validRefIdentity(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.ContainsAny(ref, " ~^:?*[\\") || ref == "@" {
		return false
	}
	for _, c := range ref {
		if c < 33 || c == 127 {
			return false
		}
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

type CursorStore struct{ StateHome, ProfileRoot string }

func (s CursorStore) path() (string, error) {
	dir, err := machine.ProfileStateDir(s.StateHome, s.ProfileRoot)
	return filepath.Join(dir, "sync.toml"), err
}

// Load never creates state. Invalid or unsupported state is not treated as missing.
func (s CursorStore) Load() (Cursor, bool, error) {
	path, err := s.path()
	if err != nil {
		return Cursor{}, false, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return Cursor{}, false, nil
	}
	if err != nil {
		return Cursor{}, false, errors.New("open sync cursor failed")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Cursor{}, false, errors.New("sync cursor is not a regular file")
	}
	bytes, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return Cursor{}, false, errors.New("read sync cursor failed")
	}
	if len(bytes) > 16384 {
		return Cursor{}, false, errors.New("sync cursor exceeds size limit")
	}
	var cursor Cursor
	// TOML diagnostics can quote input; do not expose a malformed file's content.
	if err := toml.Unmarshal(bytes, &cursor); err != nil {
		return Cursor{}, false, errors.New("parse sync cursor failed")
	}
	if err := cursor.validate(); err != nil {
		return Cursor{}, false, err
	}
	return cursor, true, nil
}

// Save is a storage primitive only. Workflow must prove settlement before calling it.
func (s CursorStore) Save(cursor Cursor) error {
	if err := cursor.validate(); err != nil {
		return err
	}
	path, err := s.path()
	if err != nil {
		return err
	}
	if err := privateStateDir(s.StateHome, filepath.Dir(path)); err != nil {
		return err
	}
	bytes, err := toml.Marshal(cursor)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".sync-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(bytes); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	return syncStateDir(filepath.Dir(path))
}

func (s CursorStore) Remove() error {
	path, err := s.path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return syncStateDir(filepath.Dir(path))
}

func syncStateDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// The caller-provided state home is trusted; Blueprint-created descendants must
// be private directories, never symlink aliases to another profile's state.
func privateStateDir(home, dir string) error {
	root, err := filepath.Abs(home)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("sync state directory is not a directory")
		}
		if err := os.Chmod(current, 0700); err != nil {
			return err
		}
	}
	return nil
}
