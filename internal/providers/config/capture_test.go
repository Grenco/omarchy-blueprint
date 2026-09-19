package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir, History: fakeBaselineHistory(false)}).Capture(profile.Configs{Excluded: []string{"discord"}}, func(string) bool { return true })
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
	go func() { _, err := p.Capture(profile.Configs{}, func(string) bool { return true }); done <- err }()
	<-started
	if _, err := p.Capture(profile.Configs{}, func(string) bool { return true }); err == nil || !strings.Contains(err.Error(), "already in progress") {
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
	if _, err := p.Capture(profile.Configs{}, func(string) bool { return true }); err != nil {
		t.Fatalf("released lock blocked later capture: %v", err)
	}
}

func TestCaptureDoesNotPersistSensitiveContent(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(user, "harmless.conf"), "api_token = 'abcdefghijklmnopqrstuvwxyz'\n")
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{}, func(string) bool { return true })
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
	_, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{}, func(string) bool { return true })
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
	if _, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{}, func(string) bool { return true }); err == nil {
		t.Fatal("symlink created after scan was accepted")
	}
}

func TestCaptureDeduplicatesExclusions(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{Excluded: []string{"discord", "discord"}}, func(string) bool { return true })
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
	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{}, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Deletes) != 0 {
		t.Fatalf("backup recorded as tombstone=%#v", result.State.Deletes)
	}
}

// TestCaptureRespectsPerPathDecisionIndependentOfSiblingPaths is Task 19's
// "directory disabled + child enabled exception": Capture itself has no
// hierarchy/ancestor logic at all -- the caller (the shared, ancestor-aware
// policy resolver) has already resolved one decision per exact path before
// calling in, and Capture must respect it path by path, not by directory.
// Here only nvim/init.lua resolves enabled; its sibling does not, even
// though both live under the same directory.
func TestCaptureRespectsPerPathDecisionIndependentOfSiblingPaths(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(user, "nvim", "init.lua"), "child enabled")
	writeFile(t, filepath.Join(user, "nvim", "other.lua"), "sibling disabled")
	enabled := func(path string) bool { return path == "nvim/init.lua" }

	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}).Capture(profile.Configs{}, enabled)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range result.State.Files {
		paths = append(paths, f.Path)
	}
	if !reflect.DeepEqual(paths, []string{"nvim/init.lua"}) {
		t.Fatalf("captured files = %#v, want only the specifically-enabled child", paths)
	}
}

// TestCaptureDisabledPreservesWhileExcludedPrunes is Task 19's regression
// distinguishing the two mechanisms: Config Excluded prunes a path from
// management outright (it is never captured, tombstoned, or preserved, even
// if a caller mistakenly enables it), while a generic Capture Disabled
// decision freezes whatever is already desired for that path exactly as
// saved -- Capture Disabled must never be implemented by translating it
// into Excluded.
func TestCaptureDisabledPreservesWhileExcludedPrunes(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "frozen.conf"), "base")
	writeFile(t, filepath.Join(user, "frozen.conf"), "changed locally")
	writeFile(t, filepath.Join(user, "excluded.conf"), "should never be captured")
	// Simulate a prior successful capture of frozen.conf: its metadata is
	// already saved and its artifact already exists in the captured tree.
	writeFile(t, filepath.Join(profileDir, "config", "files", "frozen.conf"), "captured before local changed")
	saved := profile.Configs{
		Files:    []profile.ConfigFile{{Path: "frozen.conf", Hash: hashOf(t, filepath.Join(profileDir, "config", "files", "frozen.conf")), Mode: "0644"}},
		Excluded: []string{"excluded.conf"},
	}
	enabled := func(string) bool { return false }

	result, err := (Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir, History: fakeBaselineHistory(false)}).Capture(saved, enabled)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range result.State.Files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)
	if !reflect.DeepEqual(paths, []string{"frozen.conf"}) {
		t.Fatalf("captured files = %#v, want only frozen.conf preserved: Excluded must prune excluded.conf, not preserve it", paths)
	}
	if result.State.Files[0].Hash != saved.Files[0].Hash {
		t.Fatalf("frozen.conf hash = %q, want the frozen saved hash %q despite the changed local content", result.State.Files[0].Hash, saved.Files[0].Hash)
	}
	if _, err := os.Lstat(filepath.Join(profileDir, "config", "files", "frozen.conf")); err != nil {
		t.Fatalf("preserved artifact missing: %v", err)
	}
}
