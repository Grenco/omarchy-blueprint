package config

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/graeme/omarchy-blueprint/internal/content"
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
