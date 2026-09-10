package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestSurfaceClassificationStructuralBrowserSignatures(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "arbitrary", "Local State"), "{}")
	writeFile(t, filepath.Join(root, "arbitrary", "Default", "Preferences"), "{}")
	writeFile(t, filepath.Join(root, "arbitrary", "Default", "History"), "")
	probe, err := ProbeSurface(filepath.Join(root, "arbitrary"))
	if err != nil {
		t.Fatal(err)
	}
	got, reasons := ClassifySurface(probe)
	if got != SurfaceStateHeavy || len(reasons) != 1 || reasons[0] != "browser-profile-chromium" {
		t.Fatalf("classification=%s reasons=%v", got, reasons)
	}

	writeFile(t, filepath.Join(root, "gecko-anything", "profiles.ini"), "")
	writeFile(t, filepath.Join(root, "gecko-anything", "profile", "prefs.js"), "")
	writeFile(t, filepath.Join(root, "gecko-anything", "profile", "cookies.sqlite"), "")
	probe, err = ProbeSurface(filepath.Join(root, "gecko-anything"))
	if err != nil {
		t.Fatal(err)
	}
	got, reasons = ClassifySurface(probe)
	if got != SurfaceStateHeavy || reasons[0] != "browser-profile-gecko" {
		t.Fatalf("classification=%s reasons=%v", got, reasons)
	}

	writeFile(t, filepath.Join(root, "outer-vendor", "engine", "Local State"), "{}")
	writeFile(t, filepath.Join(root, "outer-vendor", "engine", "Default", "Preferences"), "{}")
	writeFile(t, filepath.Join(root, "outer-vendor", "engine", "Default", "History"), "{}")
	probe, err = ProbeSurface(filepath.Join(root, "outer-vendor"))
	if err != nil {
		t.Fatal(err)
	}
	got, reasons = ClassifySurface(probe)
	if got != SurfaceStateHeavy || reasons[0] != "browser-profile-chromium" {
		t.Fatalf("wrapper classification=%s reasons=%v", got, reasons)
	}
}

func TestScanSkipsMixedSurfaceButWalksExplicitIncludeAndBaseline(t *testing.T) {
	home, baseline := t.TempDir(), t.TempDir()
	user := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(user, "writer", "themes", "dark.css"), "body {}")
	writeFile(t, filepath.Join(user, "writer", "Cache", "state"), "not captured")
	writeFile(t, filepath.Join(user, "writer", "baseline.conf"), "custom")
	writeFile(t, filepath.Join(baseline, "writer", "baseline.conf"), "base")
	p := Provider{HomeDir: home, UserRoot: user, BaselineRoot: baseline, History: fakeBaselineHistory(false)}
	scan, err := p.Scan(profile.Configs{Included: []string{".config/writer/themes"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Classification{}
	for _, candidate := range scan.Candidates {
		got[candidate.Path] = candidate.Classification
	}
	if got[".config/writer/themes/dark.css"] != ConfigAdded || got[".config/writer/baseline.conf"] != ConfigModifiedBaseline {
		t.Fatalf("candidates=%#v", scan.Candidates)
	}
	if _, ok := got[".config/writer/Cache/state"]; ok {
		t.Fatalf("mixed sibling was walked: %#v", scan.Candidates)
	}
	if len(scan.Surfaces) != 1 || scan.Surfaces[0].Classification != SurfaceMixed || !scan.Surfaces[0].Explicit {
		t.Fatalf("surfaces=%#v", scan.Surfaces)
	}
}

func TestBackupArtifactsArePrunedBeforeScan(t *testing.T) {
	home := t.TempDir()
	user := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(user, "nvim.backup", "secret.conf"), "api_token = abcdefghijklmnopqrstuvwxyz")
	writeFile(t, filepath.Join(user, "nvim", "init.lua.bak"), "ignored")
	writeFile(t, filepath.Join(user, "nvim", "init.lua"), "captured")
	scan, err := (Provider{HomeDir: home, UserRoot: user}).Scan(profile.Configs{})
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range scan.Candidates {
		if candidate.Path == ".config/nvim/init.lua" && candidate.Classification == ConfigAdded {
			return
		}
		if candidate.Path == ".config/nvim.backup/secret.conf" || candidate.Path == ".config/nvim/init.lua.bak" {
			t.Fatalf("backup artifact was scanned: %#v", scan)
		}
	}
	t.Fatalf("config file absent from scan=%#v", scan)
}

func TestSurfaceClassificationRequiresConfigLikeMajorityForLargeSurface(t *testing.T) {
	root := t.TempDir()
	for i := range 100 {
		writeFile(t, filepath.Join(root, "mixed", "generated", fmt.Sprintf("%03d.data", i)), "generated")
	}
	writeFile(t, filepath.Join(root, "mixed", "settings.toml"), "theme = dark")
	probe, err := ProbeSurface(filepath.Join(root, "mixed"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ClassifySurface(probe); got != SurfaceMixed {
		t.Fatalf("classification=%s, want mixed", got)
	}

	for i := range 100 {
		writeFile(t, filepath.Join(root, "lean", fmt.Sprintf("%03d.lua", i)), "return {}")
	}
	probe, err = ProbeSurface(filepath.Join(root, "lean"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := ClassifySurface(probe); got != SurfaceConfigLean {
		t.Fatalf("classification=%s, want config-lean", got)
	}
}

func TestRealHomeNamespacePoliciesExcludeAndInclude(t *testing.T) {
	home := t.TempDir()
	user := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(user, "ghostty", "config"), "font-size = 12")
	writeFile(t, filepath.Join(user, "Typora", "themes", "dark.css"), "body {}")
	writeFile(t, filepath.Join(user, "Typora", "Cache", "state"), "state")
	saved, _, err := AddExclusion(profile.Configs{}, "ghostty")
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Excluded[0]; got != ".config/ghostty" {
		t.Fatalf("excluded=%q", got)
	}
	saved, _, err = AddInclusion(saved, ".config/Typora/themes")
	if err != nil {
		t.Fatal(err)
	}
	scan, err := (Provider{HomeDir: home, UserRoot: user}).ScanForCapture(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range scan.Candidates {
		if candidate.Path == ".config/ghostty/config" || candidate.Path == ".config/Typora/Cache/state" {
			t.Fatalf("policy failed: %#v", scan.Candidates)
		}
		if candidate.Path == ".config/Typora/themes/dark.css" {
			return
		}
	}
	t.Fatalf("included path absent: %#v", scan.Candidates)
}

func TestCapturePrunesLegacySavedBrowserState(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	user := filepath.Join(home, ".config")
	browserPath := filepath.Join(user, "arbitrary-browser", "Default", "Preferences")
	writeFile(t, filepath.Join(user, "arbitrary-browser", "Local State"), "{}")
	writeFile(t, browserPath, "{}")
	writeFile(t, filepath.Join(user, "arbitrary-browser", "Default", "History"), "")
	saved := profile.Configs{Files: []profile.ConfigFile{{Path: ".config/arbitrary-browser/Default/Preferences", Hash: "old"}}}
	result, err := (Provider{HomeDir: home, UserRoot: user, BaselineRoot: t.TempDir(), ProfileDir: profileDir}).Capture(saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.Files) != 0 {
		t.Fatalf("state=%#v", result.State)
	}
	for _, candidate := range result.Scan.Candidates {
		if candidate.Path == saved.Files[0].Path {
			t.Fatalf("legacy browser file was reconsidered for capture: %#v", result.Scan)
		}
	}
	if _, err := os.Lstat(browserPath); err != nil {
		t.Fatalf("live browser file changed: %v", err)
	}
}

func TestExplicitIncludeCannotBypassBackupOrAuthenticationSafety(t *testing.T) {
	home := t.TempDir()
	user := filepath.Join(home, ".config")
	writeFile(t, filepath.Join(user, "browser", "Login Data"), "opaque")
	writeFile(t, filepath.Join(user, "browser", "Cookies"), "opaque")
	scan, err := (Provider{HomeDir: home, UserRoot: user}).ScanForCapture(profile.Configs{Included: []string{".config/browser"}})
	if err != nil {
		t.Fatal(err)
	}
	sensitive := false
	for _, candidate := range scan.Candidates {
		if candidate.Path == ".config/browser/Login Data" && candidate.Classification == ConfigSensitive {
			sensitive = true
		}
	}
	if !sensitive {
		t.Fatalf("authentication database was not sensitive: %#v", scan.Candidates)
	}
	writeFile(t, filepath.Join(user, "nvim.backup", "config"), "ignored")
	if _, err := (Provider{HomeDir: home, UserRoot: user}).ScanForCapture(profile.Configs{Included: []string{".config/nvim.backup"}}); err == nil {
		t.Fatal("backup root accepted as explicit include")
	}
}
