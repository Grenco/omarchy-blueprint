package config

import (
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
	scan, err := (Provider{UserRoot: user, BaselineRoot: baseline}).Scan(profile.Configs{})
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
	scan, err := (Provider{HomeDir: home, UserRoot: user, BaselineRoot: baseline}).Scan(profile.Configs{})
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
