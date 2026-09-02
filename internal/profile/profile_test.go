package profile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSaveLoadRoundTripNormalizesPackages(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	d := New("main", now)
	d.Manifest.Capture.Packages = true
	d.Packages = Packages{Official: []string{"zoxide", "git", "git", ""}, AUR: []string{"visual-studio-code-bin"}}
	if err := Save(dir, d); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Packages.Official, []string{"git", "zoxide"}) {
		t.Fatalf("official = %#v", got.Packages.Official)
	}
	b, err := os.ReadFile(filepath.Join(dir, "packages", "official.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "git\nzoxide\n" {
		t.Fatalf("unexpected file: %q", b)
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.toml"), []byte("schema = 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected schema error")
	}
}
