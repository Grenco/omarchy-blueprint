package services

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestServicesRefusesSymlinkedArtifactDirectories(t *testing.T) {
	for _, linked := range []string{"services", "services/units"} {
		t.Run(linked, func(t *testing.T) {
			profileDir, outside := t.TempDir(), t.TempDir()
			if err := os.MkdirAll(filepath.Join(profileDir, "services"), 0o755); err != nil {
				t.Fatal(err)
			}
			_ = os.Remove(filepath.Join(profileDir, filepath.FromSlash(linked)))
			if err := os.Symlink(outside, filepath.Join(profileDir, filepath.FromSlash(linked))); err != nil {
				t.Fatal(err)
			}
			p := &Provider{ProfileDir: profileDir}
			if _, err := p.stageServiceFiles(profile.Services{}, nil); err == nil {
				t.Fatalf("Services staged artifacts with a symlinked %s/", linked)
			}
			if _, err := p.desiredServiceFile("units/a.service", "hash", ""); err == nil {
				t.Fatalf("Services read an artifact through a symlinked %s/", linked)
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Fatal("Services wrote outside the profile")
			}
		})
	}
}

// An interrupted unit-generation swap must not block the next capture.
func TestInterruptedServicesSwapRecovers(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "services")
	if err := os.MkdirAll(filepath.Join(parent, ".units-previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".units-previous", "a.service"), []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, ".capture-123"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Stopped between moving units/ aside and moving the new one in.
	if err := recoverServiceGeneration(parent); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(parent, "units", "a.service")); string(got) != "previous" {
		t.Fatal("the previous unit generation was not restored")
	}
	for _, leftover := range []string{".units-previous", ".capture-123"} {
		if _, err := os.Lstat(filepath.Join(parent, leftover)); !os.IsNotExist(err) {
			t.Fatalf("%s was left behind", leftover)
		}
	}
	// Stopped after the swap: the previous generation is stale.
	if err := os.MkdirAll(filepath.Join(parent, ".units-previous"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := recoverServiceGeneration(parent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(parent, ".units-previous")); !os.IsNotExist(err) {
		t.Fatal("a stale previous generation was kept")
	}
	if _, err := os.Lstat(filepath.Join(parent, "units", "a.service")); err != nil {
		t.Fatal("the current units were lost")
	}
}
