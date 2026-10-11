package syncengine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/pelletier/go-toml/v2"
)

func validCursor() Cursor {
	return Cursor{Version: 1, Machine: "laptop", Revision: strings.Repeat("a", 40), Branch: "main", Upstream: "origin/main", RemoteFingerprint: strings.Repeat("b", 64)}
}

// Missing validation or in-place truncation would accept broken authority lineage or corrupt a prior cursor.
func TestCursorRoundTripIsolationAtomicPermissions(t *testing.T) {
	store := CursorStore{StateHome: t.TempDir(), ProfileRoot: t.TempDir()}
	if _, ok, err := store.Load(); ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	a := validCursor()
	if err := store.Save(a); err != nil {
		t.Fatal(err)
	}
	dir, _ := machine.ProfileStateDir(store.StateHome, store.ProfileRoot)
	path := filepath.Join(dir, "sync.toml")
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	b := a
	b.Revision = strings.Repeat("c", 40)
	if err := store.Save(b); err != nil {
		t.Fatal(err)
	}
	var before Cursor
	contents := make([]byte, 4096)
	n, err := old.Read(contents)
	if err != nil {
		t.Fatal(err)
	}
	if err := toml.Unmarshal(contents[:n], &before); err != nil || before != a {
		t.Fatal("replacement was not atomic", before, err)
	}
	got, ok, err := store.Load()
	if err != nil || !ok || got != b {
		t.Fatal(got, ok, err)
	}
	for p, perm := range map[string]os.FileMode{path: 0600, dir: 0700} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != perm {
			t.Fatal("insecure permissions", p, err)
		}
	}
	other := store
	other.ProfileRoot = t.TempDir()
	if _, ok, err := other.Load(); ok || err != nil {
		t.Fatal("profile isolation failed", ok, err)
	}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
}

func TestCursorRejectsInvalidStateWithoutReplacingPrior(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Cursor)
	}{
		{"version", func(c *Cursor) { c.Version = 2 }}, {"missing sha", func(c *Cursor) { c.Revision = "" }},
		{"uppercase sha", func(c *Cursor) { c.Revision = strings.Repeat("A", 40) }}, {"invalid sha", func(c *Cursor) { c.Revision = strings.Repeat("z", 40) }},
		{"sha256", func(c *Cursor) { c.Revision = strings.Repeat("a", 64) }}, {"machine", func(c *Cursor) { c.Machine = "" }},
		{"invalid machine", func(c *Cursor) { c.Machine = "bad name" }}, {"branch", func(c *Cursor) { c.Branch = "" }},
		{"upstream", func(c *Cursor) { c.Upstream = "" }}, {"fingerprint", func(c *Cursor) { c.RemoteFingerprint = "" }},
		{"raw remote", func(c *Cursor) { c.RemoteFingerprint = "https://user:secret@example.invalid/repo" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := CursorStore{StateHome: t.TempDir(), ProfileRoot: t.TempDir()}
			good := validCursor()
			if err := store.Save(good); err != nil {
				t.Fatal(err)
			}
			bad := good
			tc.change(&bad)
			if err := store.Save(bad); err == nil {
				t.Fatal("invalid cursor saved")
			}
			got, ok, err := store.Load()
			if err != nil || !ok || got != good {
				t.Fatal("valid cursor lost", got, ok, err)
			}
			dir, _ := machine.ProfileStateDir(store.StateHome, store.ProfileRoot)
			bytes, _ := toml.Marshal(bad)
			if err := os.WriteFile(filepath.Join(dir, "sync.toml"), bytes, 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := store.Load(); err == nil || ok || strings.Contains(err.Error(), "secret") {
				t.Fatal("invalid or sensitive load result", ok, err)
			}
		})
	}
}

func TestCursorMalformedAndSymlinkRejected(t *testing.T) {
	store := CursorStore{StateHome: t.TempDir(), ProfileRoot: t.TempDir()}
	if err := store.Save(validCursor()); err != nil {
		t.Fatal(err)
	}
	dir, _ := machine.ProfileStateDir(store.StateHome, store.ProfileRoot)
	path := filepath.Join(dir, "sync.toml")
	if err := os.WriteFile(path, []byte("revision = [\"sensitive\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); ok || err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal(ok, err)
	}
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Load(); ok || err == nil {
		t.Fatal("followed symlink", ok, err)
	}
}

func TestCursorRejectsCredentialOrMalformedRefIdentity(t *testing.T) {
	for _, bad := range []string{"https://user:secret@example.invalid/repo", "../main", "main\nother", "main..other", "main@{1}", "-main"} {
		for _, field := range []string{"branch", "upstream"} {
			cursor := validCursor()
			if field == "branch" {
				cursor.Branch = bad
			} else {
				cursor.Upstream = bad
			}
			if err := (CursorStore{StateHome: t.TempDir(), ProfileRoot: t.TempDir()}).Save(cursor); err == nil {
				t.Fatal("accepted invalid ref", field)
			}
		}
	}
}

func TestCursorAndLockRejectSymlinkedStateDirectory(t *testing.T) {
	home, root, outside := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "omarchy-blueprint")); err != nil {
		t.Fatal(err)
	}
	if err := (CursorStore{StateHome: home, ProfileRoot: root}).Save(validCursor()); err == nil {
		t.Fatal("save followed directory symlink")
	}
	if _, err := AcquireProfileLock(home, root); err == nil {
		t.Fatal("lock followed directory symlink")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside directory changed", err)
	}
}

func TestCursorLoadRemoveRejectAliasedParentAndPreserveOtherProfile(t *testing.T) {
	home, firstRoot, secondRoot := t.TempDir(), t.TempDir(), t.TempDir()
	first := CursorStore{StateHome: home, ProfileRoot: firstRoot}
	second := CursorStore{StateHome: home, ProfileRoot: secondRoot}
	if err := first.Save(validCursor()); err != nil {
		t.Fatal(err)
	}
	firstDir, _ := machine.ProfileStateDir(home, firstRoot)
	secondDir, _ := machine.ProfileStateDir(home, secondRoot)
	if err := os.Symlink(firstDir, secondDir); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := second.Load(); ok || err == nil {
		t.Error("Load adopted another profile through a parent symlink")
	}
	if err := second.Remove(); err == nil {
		t.Error("Remove accepted another profile alias")
	}
	if got, ok, err := first.Load(); err != nil || !ok || got != validCursor() {
		t.Fatal("other profile cursor damaged", ok, err)
	}
}
