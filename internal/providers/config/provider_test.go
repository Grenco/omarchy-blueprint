package config

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/graeme/omarchy-blueprint/internal/content"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
)

func testProvider(root, baseline, profileDir string) Provider {
	return Provider{
		UserRoot:     root,
		BaselineRoot: baseline,
		ProfileDir:   profileDir,
		Specs: []Spec{
			{ID: "hypr.main", Path: "hypr/hyprland.lua"},
			{ID: "hypr.bindings", Path: "hypr/bindings.lua"},
		},
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectMarksDefaultsNotCaptured(t *testing.T) {
	base, user, _, _ := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/hyprland.lua"), "default")
	writeFile(t, filepath.Join(user, "hypr/hyprland.lua"), "default")
	p := testProvider(user, base, "")
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	want := State{Files: []DetectedFile{
		{
			ID: "hypr.main", Path: "hypr/hyprland.lua", Status: FileDefault,
			Hash: hashOf(t, filepath.Join(base, "hypr/hyprland.lua")), BaselineHash: state.Files[0].BaselineHash,
		},
		{ID: "hypr.bindings", Path: "hypr/bindings.lua", Status: FileUnsupported},
	}}
	if !reflect.DeepEqual(state, want) {
		t.Fatalf("got %+v want %+v", state, want)
	}
}

func TestDetectMarksCustomized(t *testing.T) {
	base, user, _, _ := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "default")
	writeFile(t, filepath.Join(user, "hypr/bindings.lua"), "custom")
	p := testProvider(user, base, "")
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Files[1].Status != FileCustomized {
		t.Fatalf("status = %q", state.Files[1].Status)
	}
	if state.Files[1].Hash != hashOf(t, filepath.Join(user, "hypr/bindings.lua")) {
		t.Fatalf("hash = %q", state.Files[1].Hash)
	}
}

func TestDetectMarksMissingUserFile(t *testing.T) {
	base, user, _, _ := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "default")
	p := testProvider(user, base, "")
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Files[1].Status != FileMissing {
		t.Fatalf("status = %q", state.Files[1].Status)
	}
	if state.Files[1].Hash != "" {
		t.Fatalf("hash = %q", state.Files[1].Hash)
	}
}

func TestDetectMarksMissingBaselineUnsupported(t *testing.T) {
	base, user, _, _ := sandbox(t)
	writeFile(t, filepath.Join(user, "hypr/bindings.lua"), "custom")
	p := testProvider(user, base, "")
	state, err := p.Detect()
	if err != nil {
		t.Fatal(err)
	}
	if state.Files[1].Status != FileUnsupported {
		t.Fatalf("status = %q", state.Files[1].Status)
	}
}

func TestDetectRejectsBaselineSymlinkAndSpecialFile(t *testing.T) {
	base, user, _, _ := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/real.lua"), "real")
	if err := os.Symlink(filepath.Join(base, "hypr/real.lua"), filepath.Join(base, "hypr/hyprland.lua")); err != nil {
		t.Fatal(err)
	}
	if err := mkfifo(filepath.Join(base, "hypr/bindings.lua"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := testProvider(user, base, "")
	if _, err := p.Detect(); err == nil {
		t.Fatal("expected symlink baseline to be rejected")
	}
	p.Specs = []Spec{{ID: "hypr.bindings", Path: "hypr/bindings.lua"}}
	if _, err := p.Detect(); err == nil {
		t.Fatal("expected special-file baseline to be rejected")
	}
}

func TestDetectRejectsUserSymlinkAndSpecialFile(t *testing.T) {
	base, user, _, _ := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/hyprland.lua"), "default")
	writeFile(t, filepath.Join(user, "hypr/real.lua"), "real")
	if err := os.Symlink(filepath.Join(user, "hypr/real.lua"), filepath.Join(user, "hypr/hyprland.lua")); err != nil {
		t.Fatal(err)
	}
	if err := mkfifo(filepath.Join(user, "hypr/bindings.lua"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := testProvider(user, base, "")
	if _, err := p.Detect(); err == nil {
		t.Fatal("expected user symlink to be rejected")
	}
	p.Specs = []Spec{{ID: "hypr.bindings", Path: "hypr/bindings.lua"}}
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "default")
	if _, err := p.Detect(); err == nil {
		t.Fatal("expected user special file to be rejected")
	}
}

func TestCaptureStoresOnlyCustomizedFilesWithBaselineAndMetadata(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/hyprland.lua"), "main default")
	writeFile(t, filepath.Join(user, "hypr/hyprland.lua"), "main default")
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "bindings default")
	writeFile(t, filepath.Join(user, "hypr/bindings.lua"), "bindings custom")
	p := testProvider(user, base, profileDir)
	configs, err := p.Capture()
	if err != nil {
		t.Fatal(err)
	}
	want := profile.Configs{Files: []profile.ConfigFile{{
		ID: "hypr.bindings", Path: "hypr/bindings.lua",
		Hash:         hashOf(t, filepath.Join(user, "hypr/bindings.lua")),
		BaselineHash: hashOf(t, filepath.Join(base, "hypr/bindings.lua")),
	}}}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("configs = %+v want %+v", configs, want)
	}
	assertFile(t, filepath.Join(profileDir, "config/files/hypr/bindings.lua"), "bindings custom")
	assertFile(t, filepath.Join(profileDir, "config/baseline/hypr/bindings.lua"), "bindings default")
	if _, err := os.Stat(filepath.Join(profileDir, "config/files/hypr/hyprland.lua")); !os.IsNotExist(err) {
		t.Fatal("default file must not be captured")
	}
}

func TestCaptureRemovesStaleCapturedFileWhenResetToBaseline(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "bindings default")
	writeFile(t, filepath.Join(user, "hypr/bindings.lua"), "custom")
	p := testProvider(user, base, profileDir)
	if _, err := p.Capture(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(user, "hypr/bindings.lua"), "bindings default")
	configs, err := p.Capture()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs.Files) != 0 {
		t.Fatalf("stale metadata kept: %+v", configs)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "config/files/hypr/bindings.lua")); !os.IsNotExist(err) {
		t.Fatal("stale captured file kept")
	}
	if _, err := os.Stat(filepath.Join(profileDir, "config/baseline/hypr/bindings.lua")); !os.IsNotExist(err) {
		t.Fatal("stale baseline kept")
	}
}

func TestCaptureSortsMetadataDeterministically(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/hyprland.lua"), "d1")
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "d2")
	writeFile(t, filepath.Join(base, "hypr/autostart.lua"), "d3")
	writeFile(t, filepath.Join(user, "hypr/bindings.lua"), "c2")
	writeFile(t, filepath.Join(user, "hypr/autostart.lua"), "c3")
	writeFile(t, filepath.Join(user, "hypr/hyprland.lua"), "c1")
	p := testProvider(user, base, profileDir)
	p.Specs = []Spec{
		{ID: "hypr.looknfeel", Path: "hypr/autostart.lua"},
		{ID: "hypr.autostart", Path: "hypr/hyprland.lua"},
		{ID: "hypr.bindings", Path: "hypr/bindings.lua"},
	}
	configs, err := p.Capture()
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(configs.Files))
	for _, f := range configs.Files {
		got = append(got, f.ID)
	}
	want := []string{"hypr.autostart", "hypr.bindings", "hypr.looknfeel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v want %v", got, want)
	}
}

func TestCaptureRejectsUserSymlink(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	writeFile(t, filepath.Join(base, "hypr/bindings.lua"), "default")
	writeFile(t, filepath.Join(user, "hypr/real.lua"), "custom")
	if err := os.Symlink(filepath.Join(user, "hypr/real.lua"), filepath.Join(user, "hypr/bindings.lua")); err != nil {
		t.Fatal(err)
	}
	p := testProvider(user, base, profileDir)
	if _, err := p.Capture(); err == nil {
		t.Fatal("expected capture to reject symlink")
	}
	if _, err := os.Stat(filepath.Join(profileDir, "config/files")); !os.IsNotExist(err) {
		t.Fatal("failed capture must not leave partial profile files")
	}
}

func sandbox(t *testing.T) (base, user, plugin, profileDir string) {
	t.Helper()
	dir := t.TempDir()
	base, user, profileDir = filepath.Join(dir, "base"), filepath.Join(dir, "user"), filepath.Join(dir, "profile")
	for _, d := range []string{base, user, profileDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {})
	return base, user, "", profileDir
}

func hashOf(t *testing.T, path string) string {
	t.Helper()
	h, err := content.HashRegularFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func assertFile(t *testing.T, path, body string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != body {
		t.Fatalf("%s = %q want %q", path, b, body)
	}
}

func mkfifo(path string, mode os.FileMode) error {
	return syscall.Mkfifo(path, uint32(mode))
}

func planProvider(user, base, profileDir string) Provider {
	p := testProvider(user, base, profileDir)
	p.Specs = []Spec{{ID: "hypr.bindings", Path: "hypr/bindings.lua"}}
	return p
}

func detected(status FileStatus, hash, baselineHash string) State {
	return State{Files: []DetectedFile{{
		ID: "hypr.bindings", Path: "hypr/bindings.lua", Status: status, Hash: hash, BaselineHash: baselineHash,
	}}}
}

func savedConfig(hash, baselineHash string) profile.Configs {
	return profile.Configs{Files: []profile.ConfigFile{{
		ID: "hypr.bindings", Path: "hypr/bindings.lua", Hash: hash, BaselineHash: baselineHash,
	}}}
}

func TestPlanNoOpsWhenTargetMatchesDesired(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	writeFile(t, filepath.Join(profileDir, "config/files/hypr/bindings.lua"), "custom")
	plan := p.Plan(savedConfig("customhash", "basehash"), detected(FileCustomized, "customhash", "basehash"), 2, "1.0", "1.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanWritesMissingTargetWhenBaselineMatches(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	writeFile(t, filepath.Join(profileDir, "config/files/hypr/bindings.lua"), "custom")
	plan := p.Plan(savedConfig("customhash", "basehash"), detected(FileMissing, "", "basehash"), 2, "1.0", "1.0")
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	write := plan.Operations[0]
	if !write.File.ExpectedMissing || write.File.ExpectedHash != "" || write.File.Backup {
		t.Fatalf("missing-target write = %#v", write.File)
	}
	if write.Risk != model.RiskMedium || !write.Reversible || write.ID != "config.write.hypr.bindings" {
		t.Fatalf("write op = %#v", write)
	}
	if plan.Operations[1].ID != "config.reload" || !reflect.DeepEqual(plan.Operations[1].DependsOn, []string{"config.write.hypr.bindings"}) {
		t.Fatalf("reload op = %#v", plan.Operations[1])
	}
}

func TestPlanReplacesDefaultTargetWhenBaselineUnchanged(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	writeFile(t, filepath.Join(profileDir, "config/files/hypr/bindings.lua"), "custom")
	plan := p.Plan(savedConfig("customhash", "basehash"), detected(FileDefault, "basehash", "basehash"), 2, "1.0", "1.0")
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	write := plan.Operations[0].File
	if write.ExpectedMissing || write.ExpectedHash != "basehash" || !write.Backup {
		t.Fatalf("replace write = %#v", write)
	}
}

func TestPlanSkipsWhenBaselineChanged(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	plan := p.Plan(savedConfig("customhash", "oldbase"), detected(FileMissing, "", "newbase"), 2, "1.0", "1.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "Omarchy baseline changed; migration required" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanSkipsUserDrift(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	plan := p.Plan(savedConfig("savedhash", "basehash"), detected(FileCustomized, "userhash", "basehash"), 2, "1.0", "1.0")
	if len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "existing user configuration differs; overwrite disabled" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanSkipsUnsupportedConfig(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	plan := p.Plan(savedConfig("savedhash", "basehash"), detected(FileUnsupported, "", ""), 2, "1.0", "1.0")
	if len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "config is no longer shipped by this Omarchy version" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanSkipsMissingCapturedSource(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	plan := p.Plan(savedConfig("customhash", "basehash"), detected(FileMissing, "", "basehash"), 2, "1.0", "1.0")
	if len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "captured config file is missing from the profile" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlanOmitsReloadWithoutWrites(t *testing.T) {
	base, user, _, profileDir := sandbox(t)
	p := planProvider(user, base, profileDir)
	plan := p.Plan(savedConfig("savedhash", "basehash"), detected(FileCustomized, "savedhash", "basehash"), 2, "1.0", "1.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
}

func TestVerifyAsymmetricOnExtras(t *testing.T) {
	_, user, _, _ := sandbox(t)
	_ = user
	result := Verify(savedConfig("savedhash", "basehash"), detected(FileCustomized, "savedhash", "basehash"))
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
	result = Verify(savedConfig("savedhash", "basehash"), detected(FileCustomized, "other", "basehash"))
	if result.OK || !reflect.DeepEqual(result.Missing, []string{"config:hypr/bindings.lua"}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestDiffDetectsAddModifyRemove(t *testing.T) {
	saved := profile.Configs{Files: []profile.ConfigFile{
		{ID: "hypr.bindings", Path: "hypr/bindings.lua", Hash: "saved"},
		{ID: "hypr.main", Path: "hypr/hyprland.lua", Hash: "saved"},
	}}
	current := State{Files: []DetectedFile{
		{ID: "hypr.bindings", Path: "hypr/bindings.lua", Status: FileCustomized, Hash: "other"},
		{ID: "hypr.looknfeel", Path: "hypr/looknfeel.lua", Status: FileCustomized, Hash: "new"},
		{ID: "hypr.autostart", Path: "hypr/autostart.lua", Status: FileDefault},
		{ID: "hypr.main", Path: "hypr/hyprland.lua", Status: FileMissing},
	}}
	changes := Diff(saved, current)
	got := make([]string, 0, len(changes))
	for _, c := range changes {
		got = append(got, string(c.Type)+" "+c.Name)
	}
	want := []string{
		"modify hypr.bindings",
		"add hypr.looknfeel",
		"remove hypr.main",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changes = %v want %v", got, want)
	}
}

func TestDiffBaselineOnlyChangeIsNotDrift(t *testing.T) {
	saved := profile.Configs{Files: []profile.ConfigFile{{ID: "hypr.bindings", Path: "hypr/bindings.lua", Hash: "saved"}}}
	current := State{Files: []DetectedFile{{ID: "hypr.bindings", Path: "hypr/bindings.lua", Status: FileCustomized, Hash: "saved", BaselineHash: "newbase"}}}
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}
