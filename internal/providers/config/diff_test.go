package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
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

func TestPlanOverlayMergesUpstreamBaselineModeTransition(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "base\n")
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "user\n")
	writeFile(t, filepath.Join(base, path), "base\n")
	if err := os.Chmod(filepath.Join(base, path), 0o600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, path), "base\n")
	if err := os.Chmod(filepath.Join(root, path), 0o600); err != nil {
		t.Fatal(err)
	}
	a := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	b := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	updated, err := p.Scan(profile.Configs{})
	if err != nil || len(updated.Candidates) != 1 || updated.Candidates[0].Classification != ConfigUnchangedBaseline {
		t.Fatalf("scan=%#v err=%v", updated, err)
	}
	plan, err := p.PlanOverlay(profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: b, Mode: "0644", BaselineHash: a, BaselineMode: "0644"}}}, updated, 8, "old", "new")
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File == nil || !plan.Operations[0].File.Generated || string(plan.Operations[0].File.Content) != "user\n" {
		t.Fatalf("plan=%#v err=%v", plan, err)
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
	plan, err := p.PlanOverlay(saved, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: currentHash, UserMode: "0644", BaselineHash: currentHash, BaselineMode: "0644"}}}, 8, "old", "new")
	if err != nil || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "Omarchy baseline changed; merge conflict requires review" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "\x00")
	saved.Files[0].Hash = hashOf(t, filepath.Join(profileDir, "config", "files", path))
	plan, err = p.PlanOverlay(saved, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: currentHash, UserMode: "0644", BaselineHash: currentHash, BaselineMode: "0644"}}}, 8, "old", "new")
	if err != nil || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "Omarchy baseline changed; configuration is not mergeable" {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	plan, err = p.PlanOverlay(saved, ScanSummary{}, 8, "old", "new")
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File == nil || plan.Operations[0].File.SourceHash != saved.Files[0].Hash {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestDesiredCandidateUsesCleanMergeBeforeClassifyingTarget(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "base\nold\n")
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "base\nuser\n")
	writeFile(t, filepath.Join(base, path), "upstream\nold\n")
	a := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	b := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	c := hashOf(t, filepath.Join(base, path))
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	f := profile.ConfigFile{Path: path, Hash: b, Mode: "0644", BaselineHash: a, BaselineMode: "0644"}
	merged, err := p.resolveDesiredFile(f, Candidate{Path: path, UserHash: c, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}, true)
	if err != nil || merged.state != desiredMerge || string(merged.content) != "upstream\nuser\n" {
		t.Fatalf("merged=%#v err=%v", merged, err)
	}
	m := hashContent(merged.content)
	for _, test := range []struct {
		name      string
		candidate Candidate
		exists    bool
		want      desiredState
	}{
		{"missing", Candidate{BaselineHash: c, BaselineMode: "0644"}, true, desiredMissing},
		{"A", Candidate{UserHash: a, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}, true, desiredMerge},
		{"B", Candidate{UserHash: b, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}, true, desiredMerge},
		{"C", Candidate{UserHash: c, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}, true, desiredMerge},
		{"M", Candidate{UserHash: m, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}, true, desiredSatisfied},
		{"unknown", Candidate{UserHash: "unknown", UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}, true, desiredUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := p.resolveDesiredFile(f, test.candidate, test.exists)
			if err != nil || got.state != test.want {
				t.Fatalf("got=%#v err=%v", got, err)
			}
		})
	}
}

func TestPlanOverlaySkipsAllConflictedTargetsUnlessForced(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "value=old\n")
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "value=user\n")
	writeFile(t, filepath.Join(base, path), "value=upstream\n")
	a := hashOf(t, filepath.Join(profileDir, "config", "baseline", path))
	b := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	c := hashOf(t, filepath.Join(base, path))
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: b, Mode: "0644", BaselineHash: a, BaselineMode: "0644"}}}
	for _, test := range []struct {
		name string
		hash string
	}{
		{"missing", ""}, {"A", a}, {"B", b}, {"C", c}, {"unknown", "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := Candidate{Path: path, BaselineHash: c, BaselineMode: "0644"}
			if test.hash != "" {
				candidate.UserHash, candidate.UserMode = test.hash, "0644"
			}
			plan, err := p.PlanOverlay(saved, ScanSummary{Candidates: []Candidate{candidate}}, 8, "old", "new")
			if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
				t.Fatalf("plan=%#v err=%v", plan, err)
			}
		})
	}
	writeFile(t, filepath.Join(root, path), "value=user\n")
	plan, err := p.PlanOverlay(saved, ScanSummary{Candidates: []Candidate{{Path: path, UserHash: b, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}}}, 8, "old", "new", PlanOptions{Force: true})
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File == nil || plan.Operations[0].File.SourceHash != b {
		t.Fatalf("forced plan=%#v err=%v", plan, err)
	}
}

func TestBaselineIdentityRequiresModeExceptForLegacyProfiles(t *testing.T) {
	if baselineIdentity("hash", "0600", "hash", "0644") {
		t.Fatal("schema-8 baseline mode drift accepted")
	}
	if !baselineIdentity("hash", "0600", "hash", "") {
		t.Fatal("legacy hash-only baseline identity rejected")
	}
}

func TestTombstoneRequiresMatchingCurrentBaselineIdentity(t *testing.T) {
	deletion := profile.ConfigDelete{Path: "app/default", BaselineHash: "base", BaselineMode: "0644"}
	modeChanged := Candidate{Path: deletion.Path, UserHash: "base", UserMode: "0600", BaselineHash: "base", BaselineMode: "0600"}
	if got := resolveDesiredDelete(deletion, modeChanged, true); got != desiredDirect {
		t.Fatalf("current baseline mode transition delete=%v, want direct", got)
	}
	legacy := deletion
	legacy.BaselineMode = ""
	if got := resolveDesiredDelete(legacy, modeChanged, true); got != desiredDirect {
		t.Fatalf("legacy delete=%v, want direct", got)
	}
}

func TestTombstoneAcceptsSavedOrCurrentBaselineWithRealScan(t *testing.T) {
	for _, target := range []struct {
		name, content string
		mode          os.FileMode
	}{
		{"saved baseline", "saved", 0o644},
		{"current baseline", "current", 0o600},
	} {
		t.Run(target.name, func(t *testing.T) {
			root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
			path := "app/removed"
			writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "saved")
			writeFile(t, filepath.Join(base, path), "current")
			writeFile(t, filepath.Join(root, path), target.content)
			if err := os.Chmod(filepath.Join(root, path), target.mode); err != nil {
				t.Fatal(err)
			}
			if target.name == "current baseline" {
				if err := os.Chmod(filepath.Join(base, path), target.mode); err != nil {
					t.Fatal(err)
				}
			}
			saved := profile.Configs{Deletes: []profile.ConfigDelete{{Path: path, BaselineHash: hashOf(t, filepath.Join(profileDir, "config", "baseline", path)), BaselineMode: "0644"}}}
			p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
			scan, err := p.Scan(saved)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.PlanOverlay(saved, scan, 8, "old", "new")
			if err != nil || len(plan.Operations) != 1 || plan.Operations[0].Delete == nil || len(plan.Skipped) != 0 {
				t.Fatalf("plan=%#v err=%v", plan, err)
			}
			verified, err := p.Verify(saved, scan)
			if err != nil || verified.OK {
				t.Fatalf("verify=%#v err=%v", verified, err)
			}
			changes, err := p.Diff(saved, scan)
			if err != nil || len(changes) != 1 {
				t.Fatalf("changes=%#v err=%v", changes, err)
			}
		})
	}
}

func TestDisappearedBaselineRecognizesDesiredFileAcrossLifecycle(t *testing.T) {
	root, profileDir := t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "saved baseline\n")
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "wanted\n")
	writeFile(t, filepath.Join(root, path), "wanted\n")
	saved := profile.Configs{Files: []profile.ConfigFile{{
		Path: path, Hash: hashOf(t, filepath.Join(profileDir, "config", "files", path)), Mode: "0644",
		BaselineHash: hashOf(t, filepath.Join(profileDir, "config", "baseline", path)), BaselineMode: "0644",
	}}}
	p := Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	scan, err := p.Scan(saved)
	if err != nil || len(scan.Candidates) != 1 || scan.Candidates[0].Classification != ConfigAdded {
		t.Fatalf("scan=%#v err=%v", scan, err)
	}
	plan, err := p.PlanOverlay(saved, scan, 8, "old", "new")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	verified, err := p.Verify(saved, scan)
	if err != nil || !verified.OK {
		t.Fatalf("verify=%#v err=%v", verified, err)
	}
	changes, err := p.Diff(saved, scan)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestForceMergeConflictAtMissingTargetWritesExpectedMissing(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "value=old\n")
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "value=user\n")
	writeFile(t, filepath.Join(base, path), "value=upstream\n")
	saved := profile.Configs{Files: []profile.ConfigFile{{
		Path: path, Hash: hashOf(t, filepath.Join(profileDir, "config", "files", path)), Mode: "0644",
		BaselineHash: hashOf(t, filepath.Join(profileDir, "config", "baseline", path)), BaselineMode: "0644",
	}}}
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	scan, err := p.Scan(saved)
	if err != nil || len(scan.Candidates) != 1 || scan.Candidates[0].Classification != ConfigDeletedBaseline {
		t.Fatalf("scan=%#v err=%v", scan, err)
	}
	plan, err := p.PlanOverlay(saved, scan, 8, "old", "new", PlanOptions{Force: true})
	if err != nil || len(plan.Operations) != 1 || plan.Operations[0].File == nil || !plan.Operations[0].File.ExpectedMissing || plan.Operations[0].File.Backup || plan.Operations[0].File.ExpectedExisting != nil {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestScanSavedPathDirectoriesConflictAndForceReplaceOrDelete(t *testing.T) {
	for _, test := range []struct {
		name  string
		saved func(string) profile.Configs
		setup func(string, string)
		check func(t *testing.T, op model.Operation)
	}{
		{
			name: "replace file",
			saved: func(profileDir string) profile.Configs {
				path := "app/config"
				writeFile(t, filepath.Join(profileDir, "config", "files", path), "wanted")
				return profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hashOf(t, filepath.Join(profileDir, "config", "files", path)), Mode: "0644"}}}
			},
			setup: func(root, _ string) {
				if err := os.MkdirAll(filepath.Join(root, "app", "config"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, op model.Operation) {
				if op.File == nil || !op.File.ReplaceExisting || !op.File.Backup || op.File.ExpectedExisting.Type != "directory" {
					t.Fatalf("operation=%#v", op)
				}
			},
		},
		{
			name: "delete tombstone",
			saved: func(profileDir string) profile.Configs {
				path := "app/config"
				writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "baseline")
				return profile.Configs{Deletes: []profile.ConfigDelete{{Path: path, BaselineHash: hashOf(t, filepath.Join(profileDir, "config", "baseline", path)), BaselineMode: "0644"}}}
			},
			setup: func(root, base string) {
				writeFile(t, filepath.Join(base, "app", "config"), "baseline")
				if err := os.MkdirAll(filepath.Join(root, "app", "config"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			check: func(t *testing.T, op model.Operation) {
				if op.Delete == nil || !op.Delete.Backup || op.Delete.ExpectedExisting.Type != "directory" {
					t.Fatalf("operation=%#v", op)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
			saved := test.saved(profileDir)
			test.setup(root, base)
			p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
			scan, err := p.Scan(saved)
			if err != nil || len(scan.Candidates) != 1 || scan.Candidates[0].Classification != ConfigUnsupported {
				t.Fatalf("scan=%#v err=%v", scan, err)
			}
			plan, err := p.PlanOverlay(saved, scan, 8, "old", "new")
			if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
				t.Fatalf("plan=%#v err=%v", plan, err)
			}
			verified, err := p.Verify(saved, scan)
			if err != nil || verified.OK {
				t.Fatalf("verify=%#v err=%v", verified, err)
			}
			changes, err := p.Diff(saved, scan)
			if err != nil || len(changes) != 1 {
				t.Fatalf("changes=%#v err=%v", changes, err)
			}
			plan, err = p.PlanOverlay(saved, scan, 8, "old", "new", PlanOptions{Force: true})
			if err != nil || len(plan.Operations) != 1 {
				t.Fatalf("forced plan=%#v err=%v", plan, err)
			}
			test.check(t, plan.Operations[0])
			journal, err := restore.NewJournal(t.TempDir(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if _, err := restore.Execute(context.Background(), noopRunner{}, plan, journal, time.Now, time.Second, nil); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(filepath.Join(root, "app", ".config.omarchy-blueprint-backup-*"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups=%v err=%v", backups, err)
			}
			if test.name == "replace file" {
				assertFile(t, filepath.Join(root, "app", "config"), "wanted")
			} else if _, err := os.Lstat(filepath.Join(root, "app", "config")); !os.IsNotExist(err) {
				t.Fatalf("deleted target err=%v", err)
			}
			updated, err := p.Scan(saved)
			if err != nil {
				t.Fatal(err)
			}
			verified, err = p.Verify(saved, updated)
			if err != nil || !verified.OK {
				t.Fatalf("post-force verify=%#v err=%v", verified, err)
			}
		})
	}
}

func TestOverlayMergedExecutionSatisfiesVerifyAndDiff(t *testing.T) {
	root, base, profileDir := t.TempDir(), t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "baseline", path), "base\nold\n")
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "base\nuser\n")
	writeFile(t, filepath.Join(base, path), "upstream\nold\n")
	writeFile(t, filepath.Join(root, path), "upstream\nold\n")
	a, b, c := hashOf(t, filepath.Join(profileDir, "config", "baseline", path)), hashOf(t, filepath.Join(profileDir, "config", "files", path)), hashOf(t, filepath.Join(base, path))
	p := Provider{UserRoot: root, BaselineRoot: base, ProfileDir: profileDir}
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: b, Mode: "0644", BaselineHash: a, BaselineMode: "0644"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: c, UserMode: "0644", BaselineHash: c, BaselineMode: "0644"}}}
	plan, err := p.PlanOverlay(saved, scan, 8, "old", "new")
	if err != nil || len(plan.Operations) != 1 || !plan.Operations[0].File.Generated {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if _, err := restore.Execute(context.Background(), noopRunner{}, plan, journal, time.Now, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	updated, err := p.Scan(saved)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := p.Verify(saved, updated)
	if err != nil || !verified.OK {
		t.Fatalf("verify=%#v err=%v", verified, err)
	}
	changes, err := p.Diff(saved, updated)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

func TestPlanOverlayTreatsModeDriftAsUnknownAndAbsentTombstoneAsSatisfied(t *testing.T) {
	root, profileDir := t.TempDir(), t.TempDir()
	path := "app/config"
	writeFile(t, filepath.Join(profileDir, "config", "files", path), "wanted")
	writeFile(t, filepath.Join(profileDir, "config", "baseline", "gone"), "base")
	writeFile(t, filepath.Join(root, path), "wanted")
	hash := hashOf(t, filepath.Join(profileDir, "config", "files", path))
	goneHash := hashOf(t, filepath.Join(profileDir, "config", "baseline", "gone"))
	p := Provider{UserRoot: root, BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: hash, Mode: "0600"}}, Deletes: []profile.ConfigDelete{{Path: "gone", BaselineHash: goneHash}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigAdded, UserHash: hash, UserMode: "0644"}}}
	plan, err := p.PlanOverlay(saved, scan, 8, "old", "new")
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	verified, err := p.Verify(saved, scan)
	if err != nil || verified.OK {
		t.Fatalf("verify=%#v err=%v", verified, err)
	}
	changes, err := p.Diff(saved, scan)
	if err != nil || len(changes) != 1 {
		t.Fatalf("changes=%#v err=%v", changes, err)
	}
}

type noopRunner struct{}

func (noopRunner) Run(context.Context, string, ...string) (string, error) { return "", nil }

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
