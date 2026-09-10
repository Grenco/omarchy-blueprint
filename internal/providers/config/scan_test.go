package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestScanClassifiesBaselineOverlay(t *testing.T) {
	user, baseline := t.TempDir(), t.TempDir()
	for root, files := range map[string]map[string]string{baseline: {"same.conf": "same", "changed.conf": "base", "deleted.conf": "base"}, user: {"same.conf": "same", "changed.conf": "user", "added.conf": "new"}} {
		for path, content := range files {
			if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	scan, err := (Provider{UserRoot: user, BaselineRoot: baseline, History: fakeBaselineHistory(false)}).Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Classification{"added.conf": ConfigAdded, "changed.conf": ConfigModifiedBaseline, "deleted.conf": ConfigDeletedBaseline, "same.conf": ConfigUnchangedBaseline}
	if len(scan.Candidates) != len(want) {
		t.Fatalf("scan=%#v", scan)
	}
	for _, candidate := range scan.Candidates {
		if want[candidate.Path] != candidate.Classification {
			t.Fatalf("candidate=%#v", candidate)
		}
	}
}

func TestScanUsesOneConfigNamespaceForHomeAndOmarchyRoots(t *testing.T) {
	home := t.TempDir()
	user := filepath.Join(home, ".config")
	baseline := filepath.Join(t.TempDir(), "omarchy", "config")
	writeFile(t, filepath.Join(user, "ghostty", "config"), "user")
	writeFile(t, filepath.Join(baseline, "ghostty", "config"), "base")
	scan, err := (Provider{HomeDir: home, UserRoot: user, BaselineRoot: baseline, History: fakeBaselineHistory(false)}).Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range scan.Candidates {
		if candidate.Path == ".config/ghostty/config" && candidate.Classification == ConfigModifiedBaseline {
			return
		}
	}
	t.Fatalf("config roots did not resolve to one identity: %#v", scan)
}

func TestScanIncludesExactBashrcHomeSurface(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".bashrc"), "export EDITOR=vim\n")
	scan, err := (Provider{HomeDir: home, UserRoot: filepath.Join(home, ".config"), BaselineRoot: t.TempDir()}).Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range scan.Candidates {
		if candidate.Path == ".bashrc" && candidate.UserHash != "" {
			return
		}
	}
	t.Fatalf("bashrc absent from scan=%#v", scan)
}

func TestScanExcludesBothUserAndBaselineEntries(t *testing.T) {
	user, baseline := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(user, "discord", "settings.json"), "user")
	writeFile(t, filepath.Join(baseline, "discord", "settings.json"), "base")
	scan, err := (Provider{UserRoot: user, BaselineRoot: baseline}).Scan(profile.Configs{Excluded: []string{"discord"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Candidates) != 0 {
		t.Fatalf("excluded candidates=%#v", scan.Candidates)
	}
}

func TestScanIgnoresBlueprintBackupFilesAndDirectories(t *testing.T) {
	user, baseline := t.TempDir(), t.TempDir()
	for _, root := range []string{user, baseline} {
		writeFile(t, filepath.Join(root, ".config.omarchy-blueprint-backup-1"), "backup")
		writeFile(t, filepath.Join(root, ".anything.omarchy-blueprint-backup-2", "nested"), "backup")
	}
	scan, err := (Provider{UserRoot: user, BaselineRoot: baseline}).Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Candidates) != 0 {
		t.Fatalf("backup candidates=%#v", scan.Candidates)
	}
}

func TestScanSkipsRuntimeSubtreeBeforeSensitiveInspection(t *testing.T) {
	user := t.TempDir()
	for i := range 10000 {
		writeFile(t, filepath.Join(user, "browser", "IndexedDB", "entries", fmt.Sprintf("%05d", i)), "api_token = abcdefghijklmnopqrstuvwxyz\n")
	}
	writeFile(t, filepath.Join(user, "normal", "config"), "setting = captured\n")
	inspections := 0
	sensitiveContentInspection = func() { inspections++ }
	t.Cleanup(func() { sensitiveContentInspection = nil })
	scan, err := (Provider{UserRoot: user}).Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	if inspections != 1 {
		t.Fatalf("sensitive inspections=%d", inspections)
	}
	if len(scan.Candidates) != 1 || scan.Candidates[0].Path != "normal/config" || scan.Candidates[0].Classification != ConfigAdded {
		t.Fatalf("scan=%#v", scan)
	}
}

func TestScanForCaptureRequiresBaselineProvenance(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "settings.conf"), "default")
	writeFile(t, filepath.Join(user, "settings.conf"), "custom")

	t.Run("nil history is ambiguous and not drift", func(t *testing.T) {
		p := Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir}
		scan, err := p.ScanForCapture(profile.Configs{})
		if err != nil || scan.Candidates[0].Classification != ConfigAmbiguousBaseline {
			t.Fatalf("scan=%#v err=%v", scan, err)
		}
		result, err := p.Capture(profile.Configs{})
		if err != nil || len(result.State.Files) != 0 || len(result.Changes) != 0 {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("trusted historical baseline is not captured", func(t *testing.T) {
		p := Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir, History: fakeBaselineHistory(true)}
		scan, err := p.ScanForCapture(profile.Configs{})
		if err != nil || scan.Candidates[0].Classification != ConfigHistoricalBaseline {
			t.Fatalf("scan=%#v err=%v", scan, err)
		}
		result, err := p.Capture(profile.Configs{})
		if err != nil || len(result.State.Files) != 0 {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})

	t.Run("known non-historical baseline is capturable", func(t *testing.T) {
		p := Provider{UserRoot: user, BaselineRoot: base, ProfileDir: profileDir, History: fakeBaselineHistory(false)}
		scan, err := p.ScanForCapture(profile.Configs{})
		if err != nil || scan.Candidates[0].Classification != ConfigModifiedBaseline {
			t.Fatalf("scan=%#v err=%v", scan, err)
		}
		result, err := p.Capture(profile.Configs{})
		if err != nil || len(result.State.Files) != 1 || len(result.Changes) != 1 {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	})
}
