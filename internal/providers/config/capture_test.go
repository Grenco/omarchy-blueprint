package config

import (
	"os"
	"path/filepath"
	"strings"
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
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir, History: fakeBaselineHistory(false)}).Capture(profile.Configs{Excluded: []string{"discord"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Files) != 2 || len(result.State.Deletes) != 0 {
		t.Fatalf("state=%#v", result.State)
	}
	if result.State.Excluded[0] != "discord" {
		t.Fatalf("excluded=%v", result.State.Excluded)
	}
	for _, path := range []string{"files/changed.conf", "files/added.conf", "baseline/changed.conf"} {
		if _, err := os.Lstat(filepath.Join(profileDir, "config", path)); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(profileDir, "config", "files", "same.conf")); !os.IsNotExist(err) {
		t.Fatal("unchanged snapshot persisted")
	}
}

func TestCaptureAdvisoryLockAndStaleStageRecovery(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(user, "settings.conf"), "value")
	configDir := filepath.Join(profileDir, "config")
	if err := os.MkdirAll(filepath.Join(configDir, ".capture-stage-crashed"), 0o755); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	beforeStage = func() { close(started); <-release }
	t.Cleanup(func() { beforeStage = nil })
	p := Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}
	done := make(chan error, 1)
	go func() { _, err := p.Capture(profile.Configs{}); done <- err }()
	<-started
	if _, err := p.Capture(profile.Configs{}); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second capture error = %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	beforeStage = nil
	if _, err := os.Stat(filepath.Join(configDir, ".capture-stage-crashed")); !os.IsNotExist(err) {
		t.Fatalf("stale stage remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, ".capture.lock")); err != nil {
		t.Fatalf("persistent lock missing: %v", err)
	}
	if _, err := p.Capture(profile.Configs{}); err != nil {
		t.Fatalf("released lock blocked later capture: %v", err)
	}
}

func TestCaptureDoesNotPersistSensitiveContent(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(user, "harmless.conf"), "api_token = 'abcdefghijklmnopqrstuvwxyz'\n")
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{})
	if err != nil || len(result.State.Files) != 0 || result.Scan.Candidates[0].Classification != ConfigSensitive {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestCaptureAbortsWhenSourceChangesAfterScan(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	path := filepath.Join(user, "settings.conf")
	writeFile(t, path, "before")
	beforeStage = func() { writeFile(t, path, "after") }
	t.Cleanup(func() { beforeStage = nil })
	_, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{})
	if err == nil {
		t.Fatal("changed source captured")
	}
}

func TestCaptureRejectsSymlinkCreatedAfterScan(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	path := filepath.Join(user, "settings.conf")
	writeFile(t, path, "before")
	external := filepath.Join(t.TempDir(), "secret")
	writeFile(t, external, "api_token = 'abcdefghijklmnopqrstuvwxyz'")
	beforeStage = func() {
		_ = os.Remove(path)
		if err := os.Symlink(external, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeStage = nil })
	if _, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{}); err == nil {
		t.Fatal("symlink created after scan was accepted")
	}
}

func TestCaptureDeduplicatesExclusions(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{Excluded: []string{"discord", "discord"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Excluded) != 1 || result.State.Excluded[0] != "discord" {
		t.Fatalf("excluded=%#v", result.State.Excluded)
	}
}

func TestCaptureDoesNotTombstoneBaselineBackup(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "settings.conf.bak.20260909"), "backup")
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Deletes) != 0 {
		t.Fatalf("backup recorded as tombstone=%#v", result.State.Deletes)
	}
}
