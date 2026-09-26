package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilitySupportedCurrentBaseline(t *testing.T) {
	path := ".config/example/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired", BaselineHash: "base"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: "base", BaselineHash: "base"}}, Surfaces: []SurfaceSummary{{Path: ".config/example", Classification: SurfaceConfigLean}}}
	got, err := RestoreCompatibility(saved, scan, map[string]bool{path: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged || len(got.Evidence) == 0 || len(got.Findings) != 0 {
		t.Fatalf("supported baseline = %+v", got)
	}
}

func TestRestoreCompatibilityUnsupportedApplyBlocks(t *testing.T) {
	path := ".config/example/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired", BaselineHash: "base"}}}
	for _, scan := range []ScanSummary{
		{Candidates: []Candidate{{Path: path, Classification: ConfigUnsupported}}},
		{Candidates: []Candidate{{Path: path, Classification: ConfigModifiedBaseline, BaselineHash: "base"}}, Surfaces: []SurfaceSummary{{Path: ".config/example", Classification: SurfaceStateHeavy}}},
	} {
		got, err := RestoreCompatibility(saved, scan, map[string]bool{path: true}, false)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked || len(got.Findings) != 1 || got.Findings[0].Code != "config.surface.unsupported" || got.Findings[0].Target != path {
			t.Fatalf("unsupported surface = %+v", got)
		}
	}
}

func TestRestoreCompatibilityAmbiguousBaselineReducesExactAuthority(t *testing.T) {
	path := ".config/example/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired", BaselineHash: "old"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigAmbiguousBaseline, UserHash: "local", BaselineHash: "new"}}}
	for _, exact := range []bool{false, true} {
		got, err := RestoreCompatibility(saved, scan, map[string]bool{path: true}, exact)
		if err != nil {
			t.Fatal(err)
		}
		wantAuthority := model.CompatibilityUnchanged
		if exact {
			wantAuthority = model.CompatibilityReduced
		}
		if got.State != model.CompatibilityUnknown || got.Authority != wantAuthority || len(got.Findings) != 1 || got.Findings[0].Code != "config.baseline.ambiguous" {
			t.Fatalf("exact=%v ambiguous baseline = %+v", exact, got)
		}
	}
}

func TestRestoreCompatibilityIncludeNeverOverridesSafety(t *testing.T) {
	path := ".config/example/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired", BaselineHash: "base"}}, Included: []string{path}}
	got, err := RestoreCompatibility(saved, ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnsupported}}}, map[string]bool{path: true}, false)
	if err != nil || got.State != model.CompatibilityIncompatible || got.Authority != model.CompatibilityBlocked {
		t.Fatalf("Included incorrectly granted authority: %+v err=%v", got, err)
	}
}

func TestRestoreCompatibilityExclusionAndRestoreSkipRemoveIntent(t *testing.T) {
	path := ".config/example/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnsupported}}}
	for _, tc := range []struct {
		name  string
		saved profile.Configs
		apply map[string]bool
	}{
		{"policy skip", saved, map[string]bool{path: false}},
		{"excluded", profile.Configs{Files: saved.Files, Excluded: []string{".config/example"}}, map[string]bool{path: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RestoreCompatibility(tc.saved, scan, tc.apply, true)
			if err != nil || got.Applies || got.State != "" || len(got.Findings) != 0 || got.Authority != model.CompatibilityUnchanged {
				t.Fatalf("irrelevant target blocked category: %+v err=%v", got, err)
			}
		})
	}
}

func TestRestoreCompatibilityDelegatedTargetRespectsPolicy(t *testing.T) {
	path := ".config/delegated/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigDelegated}}}
	blocked, err := RestoreCompatibility(saved, scan, map[string]bool{path: true}, false)
	if err != nil || blocked.State != model.CompatibilityIncompatible || blocked.Authority != model.CompatibilityBlocked || blocked.Findings[0].Code != "config.owner.delegated" {
		t.Fatalf("delegated Apply = %+v err=%v", blocked, err)
	}
	skipped, err := RestoreCompatibility(saved, scan, map[string]bool{path: false}, false)
	if err != nil || skipped.Applies || len(skipped.Findings) != 0 {
		t.Fatalf("delegated Skip = %+v err=%v", skipped, err)
	}
}

func TestRestoreCompatibilityOnlyExplicitTombstoneAuthorizesDeletion(t *testing.T) {
	path := ".config/example/default.toml"
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: "base", BaselineHash: "base"}}}
	without, err := RestoreCompatibility(profile.Configs{}, scan, map[string]bool{path: true}, true)
	if err != nil || without.Applies {
		t.Fatalf("unmanaged file invented deletion intent: %+v err=%v", without, err)
	}
	with, err := RestoreCompatibility(profile.Configs{Deletes: []profile.ConfigDelete{{Path: path, BaselineHash: "base"}}}, scan, map[string]bool{path: true}, true)
	if err != nil || with.State != model.CompatibilitySupported || len(with.Evidence) == 0 {
		t.Fatalf("explicit baseline-backed tombstone = %+v err=%v", with, err)
	}
}

func TestRestoreCompatibilityExactTombstoneSkipDoesNotVerifyConverged(t *testing.T) {
	path, profileDir := ".config/example/default.toml", t.TempDir()
	baseline := filepath.Join(profileDir, "config", "baseline", path)
	writeFile(t, baseline, "baseline")
	saved := profile.Configs{Deletes: []profile.ConfigDelete{{Path: path, BaselineHash: hashOf(t, baseline)}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigAmbiguousBaseline, UserHash: "unknown", BaselineHash: "new"}}}
	p := Provider{HomeDir: t.TempDir(), UserRoot: t.TempDir(), BaselineRoot: t.TempDir(), ProfileDir: profileDir}
	plan, err := p.PlanOverlay(saved, scan, 8, "4.0", "5.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range plan.Operations {
		if op.Delete != nil || op.Action == "delete" {
			t.Fatalf("unknown tombstone planned removal: %+v", op)
		}
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "delete disabled") {
		t.Fatalf("uncertainty not visibly skipped: %+v", plan)
	}
	compat, err := RestoreCompatibility(saved, scan, map[string]bool{path: true}, true)
	if err != nil || compat.State != model.CompatibilityUnknown || compat.Authority != model.CompatibilityReduced {
		t.Fatalf("uncertain Exact deletion = %+v err=%v", compat, err)
	}
	verified, err := p.Verify(saved, scan)
	if err != nil || verified.OK {
		t.Fatalf("uncertain deletion falsely converged: %+v err=%v", verified, err)
	}
}

func TestRestoreCompatibilityUsesExistingScanWithoutFilesystemProbe(t *testing.T) {
	path := ".config/example/config.toml"
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: path, Hash: "desired", BaselineHash: "base"}}}
	scan := ScanSummary{Candidates: []Candidate{{Path: path, Classification: ConfigUnchangedBaseline, UserHash: "base", BaselineHash: "base"}}}
	got, err := RestoreCompatibility(saved, scan, map[string]bool{path: true}, false)
	if err != nil || got.State != model.CompatibilitySupported {
		t.Fatalf("assessment probed unavailable filesystem: %+v err=%v", got, err)
	}
}
