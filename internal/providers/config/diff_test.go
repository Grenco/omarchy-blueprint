package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestPlanOverlayForceReplacesUnknownTargets(t *testing.T) {
	profileDir := t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "wanted")
	hash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	for _, target := range []struct {
		name  string
		setup func(string)
		kind  string
	}{
		{"file", func(target string) { writeFile(t, target, "unknown") }, "file"},
		{"directory", func(target string) {
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
		}, "directory"},
		{"symlink", func(target string) {
			if err := os.Symlink("elsewhere", target); err != nil {
				t.Fatal(err)
			}
		}, "symlink"},
	} {
		t.Run(target.name, func(t *testing.T) {
			caseRoot := t.TempDir()
			p := Provider{UserRoot: caseRoot, BaselineRoot: t.TempDir(), ProfileDir: profileDir}
			destination := filepath.Join(caseRoot, path)
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				t.Fatal(err)
			}
			target.setup(destination)
			plan, err := p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hash}}}, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigAdded, UserHash: "unknown"}}}, 8, "old", "new", PlanOptions{Force: true})
			if err != nil || len(plan.Operations) != 1 {
				t.Fatalf("plan=%#v err=%v", plan, err)
			}
			op := plan.Operations[0]
			if op.Risk != model.RiskHigh || !op.Reversible || op.File == nil || !op.File.ReplaceExisting || !op.File.Backup || op.File.ExpectedExisting.Type != target.kind {
				t.Fatalf("operation=%#v", op)
			}
		})
	}
}

func TestPlanOverlayForceUsesCapturedVersionForMergeConflictAndUnknownTombstone(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "value=user\n")
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "value=old\n")
	writeFile(t, filepath.Join(base, path), "value=upstream\n")
	writeFile(t, filepath.Join(root, path), "value=upstream\n")
	baseHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	desiredHash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	currentHash := hashOf(t, filepath.Join(base, path))
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	plan, err := p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: desiredHash, BaselineHash: baseHash}}}, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: currentHash, BaselineHash: currentHash}}}, 8, "old", "new", PlanOptions{Force: true})
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File == nil || plan.Operations[0].File.SourceHash != desiredHash || !strings.Contains(plan.Operations[0].Action, "captured user version will win") {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}

	deletePath := "app/removed"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", deletePath), "baseline")
	writeFile(t, filepath.Join(root, deletePath), "unknown")
	deleteHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", deletePath))
	plan, err = p.PlanOverlay(profile.Configs{Deletes: []profile.ConfigDelete{{Path: deletePath, BaselineHash: deleteHash}}}, ScanSummary{Candidates: []Candidate{{Path: deletePath, Classification: ConfigAdded, UserHash: "unknown"}}}, 8, "old", "new", PlanOptions{Force: true})
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].Risk != model.RiskHigh || !plan.Operations[0].Reversible || plan.Operations[0].Delete == nil || !plan.Operations[0].Delete.Backup || plan.Operations[0].Delete.ExpectedExisting.Type != "file" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

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

func TestPlanOverlayRestoresAddedFileWithoutOverwritingTarget(t *testing.T) {
	root, profileDir := t.TempDir(), t.TempDir()
	path := ".config/ghostty/config"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "wanted")
	hash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	p := Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	plan, err := p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hash}}}, ScanSummary{}, 8, "old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || !plan.Operations[0].File.ExpectedMissing {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	plan, err = p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hash}}}, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigAdded, UserHash: "unknown"}}}, 8, "old", "new")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestPlanOverlayWritesUnchangedBaselineAndMergesNewBaseline(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := ".config/hypr/bindings.conf"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "base\nuser\n")
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "base\nold\n")
	writeFile(t, filepath.Join(base, "hypr", "bindings.conf"), "base\nold\n")
	baseHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	desiredHash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	p := Provider{HomeDir: root, UserRoot: filepath.Join(root, ".config"), BaselineRoot: base, ProfileDir: profileDir}
	plan, err := p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: desiredHash, BaselineHash: baseHash}}}, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: baseHash, BaselineHash: baseHash}}}, 8, "old", "new")
	if err != nil || len(plan.Operations) != 2 || plan.Operations[0].File.Generated {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	writeFile(t, filepath.Join(base, "hypr", "bindings.conf"), "upstream\nold\n")
	newHash := hashOf(t, filepath.Join(base, "hypr", "bindings.conf"))
	plan, err = p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: desiredHash, BaselineHash: baseHash}}}, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: newHash, BaselineHash: newHash}}}, 8, "old", "new")
	if err != nil || len(plan.Operations) != 2 || !plan.Operations[0].File.Generated || string(plan.Operations[0].File.Content) != "upstream\nuser\n" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if !reflect.DeepEqual(plan.Operations[1].DependsOn, []string{plan.Operations[0].ID}) {
		t.Fatalf("reload=%#v", plan.Operations[1])
	}
}

func TestPlanOverlaySkipsConflictingNonmergeableAndDisappearedBaselines(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "value=user\n")
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "value=old\n")
	writeFile(t, filepath.Join(base, path), "value=new\n")
	baseHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	desiredHash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: desiredHash, BaselineHash: baseHash}}}
	currentHash := hashOf(t, filepath.Join(base, path))
	plan, err := p.PlanOverlay(saved, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: currentHash, BaselineHash: currentHash}}}, 8, "old", "new")
	if err != nil || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "Omarchy baseline changed; merge conflict requires review" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "\x00")
	saved.Files[0].Hash = hashOf(t, filepath.Join(profileDir, "config", "files", path))
	plan, err = p.PlanOverlay(saved, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: currentHash, BaselineHash: currentHash}}}, 8, "old", "new")
	if err != nil || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "Omarchy baseline changed; configuration is not mergeable" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	plan, err = p.PlanOverlay(saved, ScanSummary{}, 8, "old", "new")
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File == nil || plan.Operations[0].File.SourceHash != baseHash {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestPlanOverlayRestoresTombstoneAndReloadsOnlyHypr(t *testing.T) {
	root, profileDir := t.TempDir(), t.TempDir()
	hypr, other := ".config/hypr/removed.conf", ".config/app/removed.conf"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", hypr), "hypr")
	writeFile(t, filepath.Join(profileDir, "config", "baseline", other), "other")
	hyprHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", hypr))
	otherHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", other))
	p := Provider{HomeDir: root, UserRoot: filepath.Join(root, ".config"), BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	saved := profile.Configs{Deletes: []profile.ConfigDelete{{Path: hypr, BaselineHash: hyprHash}, {Path: other, BaselineHash: otherHash}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: hypr, Classification: ConfigUnchangedBaseline, UserHash: hyprHash, BaselineHash: hyprHash}, {Path: other, Classification: ConfigUnchangedBaseline, UserHash: otherHash, BaselineHash: otherHash}}}
	plan, err := p.PlanOverlay(saved, scan, 8, "old", "new")
	if err != nil || len(plan.Operations) != 3 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	if plan.Operations[0].Delete == nil || plan.Operations[0].Delete.ExpectedExisting.Type != "file" || plan.Operations[0].Delete.ExpectedExisting.Hash != hyprHash || plan.Operations[1].Delete == nil {
		t.Fatalf("deletes=%#v", plan.Operations)
	}
	if !reflect.DeepEqual(plan.Operations[2].DependsOn, []string{"config.delete.hypr.removed.conf"}) || plan.Operations[2].Risk != model.RiskLow {
		t.Fatalf("reload=%#v", plan.Operations[2])
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
