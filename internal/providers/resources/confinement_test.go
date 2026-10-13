package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// SEC-01: Resources works in its profile directory by path, so it refuses a
// resources/ that is a symlink committed into the profile.
func TestCaptureRefusesASymlinkedResourcesDirectory(t *testing.T) {
	profileDir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "resources.toml"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(profileDir, "resources")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareCapture(filepath.Join(profileDir, "resources"), profile.Resources{}, nil); err == nil {
		t.Fatal("a Resources capture prepared inside a symlinked resources/")
	}
	entries, _ := os.ReadDir(outside)
	if got, _ := os.ReadFile(filepath.Join(outside, "resources.toml")); len(entries) != 1 || string(got) != "keep" {
		t.Fatal("the capture touched the directory outside the profile")
	}
}
