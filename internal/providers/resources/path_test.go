package resources

import (
	"path/filepath"
	"testing"
)

func TestHomePathRoundTrip(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	absolute := filepath.Join(home, ".config", "nvim")
	logical, err := LogicalHomePath(home, absolute)
	if err != nil || logical != "~/.config/nvim" {
		t.Fatalf("logical=%q err=%v", logical, err)
	}
	expanded, err := ExpandHomePath(home, logical)
	if err != nil || expanded != absolute {
		t.Fatalf("expanded=%q err=%v", expanded, err)
	}
	for _, path := range []string{"/etc/example", "~/../outside", "relative/no-tilde", "~"} {
		if _, err := ExpandHomePath(home, path); err == nil {
			t.Fatalf("accepted invalid logical path %q", path)
		}
	}
}

func TestResourcePathAndIDValidation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "home")
	a, b, c := filepath.Join(root, "dotfiles"), filepath.Join(root, "dotfiles", "nvim"), filepath.Join(root, "Scripts")
	if !PathsOverlap(a, b) || PathsOverlap(a, c) {
		t.Fatalf("overlap a=%q b=%q c=%q", a, b, c)
	}
	for _, path := range []string{".", "nvim", "config/hypr.lua"} {
		if !SafeRelativeResourcePath(path) {
			t.Fatalf("%q should be safe", path)
		}
	}
	for _, path := range []string{"", "../outside", "/abs", "a/../../outside"} {
		if SafeRelativeResourcePath(path) {
			t.Fatalf("%q should be unsafe", path)
		}
	}
	if ResourceIDForPath(filepath.Join(root, "dotfiles")) != "dotfiles" {
		t.Fatal("unexpected derived resource ID")
	}
	for _, id := range []string{"", ".", "..", "bad/id"} {
		if ValidateResourceID(id) == nil {
			t.Fatalf("accepted invalid ID %q", id)
		}
	}
}
