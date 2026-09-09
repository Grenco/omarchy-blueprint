package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestCaptureStoresAddedModifiedAndTombstoneSparsely(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "same.conf"), "same")
	writeFile(t, filepath.Join(user, "same.conf"), "same")
	writeFile(t, filepath.Join(base, "changed.conf"), "base")
	writeFile(t, filepath.Join(user, "changed.conf"), "user")
	writeFile(t, filepath.Join(base, "deleted.conf"), "base")
	writeFile(t, filepath.Join(user, "added.conf"), "added")
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{Excluded: []string{"discord"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Files) != 2 || len(result.State.Deletes) != 1 || result.State.Deletes[0].Path != "deleted.conf" {
		t.Fatalf("state=%#v", result.State)
	}
	if result.State.Excluded[0] != "discord" {
		t.Fatalf("excluded=%v", result.State.Excluded)
	}
	for _, path := range []string{"files/changed.conf", "files/added.conf", "baseline/changed.conf", "baseline/deleted.conf"} {
		if _, err := os.Lstat(filepath.Join(profileDir, "config", path)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(profileDir, "config", "files", "same.conf")); !os.IsNotExist(err) {
		t.Fatal("unchanged snapshot persisted")
	}
}
