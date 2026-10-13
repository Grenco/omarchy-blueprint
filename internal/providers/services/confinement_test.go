package services

import (
	"os"
	"path/filepath"
	"strings"
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

// Two Blueprint processes on one profile (two independent providers): one
// must never remove or swap the other's staged generation.
func TestServicesCaptureIsSerialisedAcrossProviders(t *testing.T) {
	profileDir := t.TempDir()
	first, second := &Provider{ProfileDir: profileDir}, &Provider{ProfileDir: profileDir}
	prepared, err := first.stageServiceFiles(profile.Services{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first.prepared = prepared
	if _, err := second.stageServiceFiles(profile.Services{}, nil); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("a second capture staged during the first: %v", err)
	}
	if _, err := os.Stat(prepared.stage); err != nil {
		t.Fatalf("the first capture's staging was removed: %v", err)
	}
	if err := first.CommitCapture(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.stageServiceFiles(profile.Services{}, nil); err == nil {
		t.Fatal("a second capture staged before the first was finalized")
	}
	if err := first.FinalizeCapture(); err != nil {
		t.Fatal(err)
	}
	next, err := second.stageServiceFiles(profile.Services{}, nil)
	if err != nil {
		t.Fatalf("capture stayed locked after the first finished: %v", err)
	}
	second.prepared = next
	if err := second.RollbackCapture(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.stageServiceFiles(profile.Services{}, nil); err != nil {
		t.Fatalf("a rollback left Services locked: %v", err)
	}
}

// Staging left by a process that is gone (its lock released) is abandoned
// and removed by the next capture.
func TestAbandonedServicesStagingIsRemoved(t *testing.T) {
	profileDir := t.TempDir()
	abandoned := filepath.Join(profileDir, "services", ".capture-abandoned")
	if err := os.MkdirAll(abandoned, 0o755); err != nil {
		t.Fatal(err)
	}
	p := &Provider{ProfileDir: profileDir}
	prepared, err := p.stageServiceFiles(profile.Services{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.prepared = prepared
	defer p.RollbackCapture()
	if _, err := os.Lstat(abandoned); !os.IsNotExist(err) {
		t.Fatal("abandoned staging was kept")
	}
}

// A capture that fails while staging releases the lock.
func TestFailedServicesStagingReleasesTheLock(t *testing.T) {
	profileDir := t.TempDir()
	p := &Provider{ProfileDir: profileDir}
	bad := profile.Services{Units: []profile.ServiceUnit{{Name: "a.service", Presence: profile.ServicePresent, Definition: "units/a.service"}}}
	if _, err := p.stageServiceFiles(bad, nil); err == nil {
		t.Fatal("staging an artifact without a hash succeeded")
	}
	prepared, err := p.stageServiceFiles(profile.Services{}, nil)
	if err != nil {
		t.Fatalf("a failed staging left Services locked: %v", err)
	}
	p.prepared = prepared
	_ = p.RollbackCapture()
}
