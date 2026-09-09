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
