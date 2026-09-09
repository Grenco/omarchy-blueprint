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

func TestPlanOverlayDefersTombstoneRestore(t *testing.T) {
	root, profileDir := t.TempDir(), t.TempDir()
	path := ".config/example/default.conf"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "base")
	baseHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	p := Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	plan, err := p.PlanOverlay(profile.Configs{Deletes: []profile.ConfigDelete{{Path: path, BaselineHash: baseHash}}}, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: baseHash, BaselineHash: baseHash}}}, 8, "old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("tombstone created operations=%#v", plan.Operations)
	}
}

func TestCheckRejectsSensitiveDesiredSnapshot(t *testing.T) {
	root, profileDir := t.TempDir(), t.TempDir()
	path := "safe.conf"
	snapshot := filepath.Join(profileDir, "config", "files", path)
	writeFile(t, snapshot, "api_token: abcdefghijklmnopqrstuvwxyz\n")
	p := Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	if err := p.Check(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hashOf(t, snapshot)}}}); err == nil {
		t.Fatal("sensitive desired snapshot accepted")
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
