package resources

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
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

func TestValidateEffectiveOwnership(t *testing.T) {
	roots := map[string]string{"projects": "/home/test/.config/omarchy"}
	claims := ownership.Index{Claims: []ownership.Claim{{Provider: "config", Path: "/home/test/.config/omarchy", Recursive: true}}}
	err := ValidateEffectiveOwnership(roots, claims)
	for _, want := range []string{"projects", "/home/test/.config/omarchy", "config"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	if err := ValidateEffectiveOwnership(map[string]string{"projects": "/mnt/projects"}, claims); err != nil {
		t.Fatalf("unexpected ownership conflict: %v", err)
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

func TestResourcePathsResolve(t *testing.T) {
	item := profile.Resource{ID: "projects", Path: "~/Projects"}
	for _, test := range []struct {
		name      string
		overrides map[string]string
		want      string
	}{
		{"default", nil, "/home/test/Projects"},
		{"home override", map[string]string{"projects": "~/Code"}, "/home/test/Code"},
		{"absolute override", map[string]string{"projects": "/mnt/projects"}, "/mnt/projects"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveResourcePath(ResourcePaths{Home: "/home/test", Overrides: test.overrides}, item)
			if err != nil || got != test.want {
				t.Fatalf("ResolveResourcePath() = %q, %v; want %q, nil", got, err, test.want)
			}
			if item.Path != "~/Projects" {
				t.Fatalf("item.Path = %q, want unchanged", item.Path)
			}
		})
	}
}
