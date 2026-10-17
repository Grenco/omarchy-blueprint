// Package syncengine defines semantic reconciliation and machine-local sync state.
package syncengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/localstate"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/pelletier/go-toml/v2"
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
	if err := localstate.InspectDir(s.StateHome, filepath.Dir(path)); errors.Is(err, os.ErrNotExist) {
		return Cursor{}, false, nil
	} else if err != nil {
		return Cursor{}, false, err
	}
	bytes, err := localstate.ReadFile(path, 16384)
	if errors.Is(err, os.ErrNotExist) {
		return Cursor{}, false, nil
	}
	if err != nil {
		return Cursor{}, false, fmt.Errorf("sync cursor: %w", err)
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
	if err := localstate.PrivateDir(s.StateHome, filepath.Dir(path)); err != nil {
		return err
	}
	bytes, err := toml.Marshal(cursor)
	if err != nil {
		return err
	}
	return localstate.WriteFile(path, bytes)
}

func (s CursorStore) Remove() error {
	path, err := s.path()
	if err != nil {
		return err
	}
	return localstate.RemoveFile(s.StateHome, path)
}

// ValidRefIdentity reports whether ref is a safe Git branch/upstream name as
// recorded in Sync and Auto Sync state.
func ValidRefIdentity(ref string) bool { return validRefIdentity(ref) }

// CompletedAt reports when the cursor was last written. The cursor is only
// written by a verified completed Sync (or an explicit bootstrap), so this
// is the last completed Sync, never a check.
func (s CursorStore) CompletedAt() (Cursor, time.Time, bool, error) {
	cursor, ok, err := s.Load()
	if err != nil || !ok {
		return cursor, time.Time{}, ok, err
	}
	path, err := s.path()
	if err != nil {
		return Cursor{}, time.Time{}, false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Cursor{}, time.Time{}, false, err
	}
	return cursor, info.ModTime(), true, nil
}
