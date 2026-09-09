package config

import (
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestVerifyAcceptsTargetOnlyConfigAndRequiresTombstone(t *testing.T) {
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: "ghostty/config", Hash: "want"}}, Deletes: []profile.ConfigDelete{{Path: "example/default.conf", BaselineHash: "base"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: "ghostty/config", Classification: ConfigAdded, UserHash: "want"}, {Path: "newapp/config", Classification: ConfigAdded, UserHash: "other"}, {Path: "example/default.conf", Classification: ConfigDeletedBaseline}}}
	if got := Verify(saved, scan); !got.OK {
		t.Fatalf("verify=%#v", got)
	}
	scan.Candidates[2].Classification = ConfigModifiedBaseline
	if got := Verify(saved, scan); got.OK {
		t.Fatalf("verify=%#v", got)
	}
}

func TestCheckRejectsStrongerOwnership(t *testing.T) {
	root := t.TempDir()
	profileDir := t.TempDir()
	path := "nvim/init.lua"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "x")
	hash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	p := Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: profileDir, Ownership: ownership.Index{Claims: []ownership.Claim{{Provider: "resources", Path: filepath.Join(root, "nvim"), Recursive: true}}}}
	if err := p.Check(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hash}}}); err == nil {
		t.Fatal("ownership conflict accepted")
	}
}
