package resources

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestDiscoverLinksClassifiesInboundAndUntrackedTargets(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "dotfiles", "hypr", "overrides.lua")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(home, ".config", "hypr", "overrides.lua")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../dotfiles/hypr/overrides.lua", source); err != nil {
		t.Fatal(err)
	}
	resources := []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"}}
	got, err := DiscoverLinks(home, DefaultLinkSearchRoots(home), resources, ownership.Index{}, nil)
	if err != nil || len(got) != 1 || got[0].Classification != LinkManagedInbound || got[0].Source != "~/.config/hypr/overrides.lua" || got[0].TargetResource != "dotfiles" || got[0].TargetRelative != "hypr/overrides.lua" {
		t.Fatalf("links=%#v err=%v", got, err)
	}
	got, err = DiscoverLinks(home, DefaultLinkSearchRoots(home), nil, ownership.Index{}, nil)
	if err != nil || len(got) != 1 || got[0].Classification != LinkUntrackedHomeTarget {
		t.Fatalf("untracked=%#v err=%v", got, err)
	}
}

func TestDiscoverLinksDoesNotFollowSymlinkDirectoriesAndRespectsOwnership(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "dotfiles", "target")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(home, "external")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(home, ".config", "external")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(external, "nested")); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(home, ".config", "omarchy", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(hooks, "theme-set")); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, ".config", "owned")
	if err := os.Symlink(target, config); err != nil {
		t.Fatal(err)
	}
	resources := []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"}}
	index := ownership.Index{Claims: []ownership.Claim{{Provider: "hooks", Path: hooks, Recursive: true, DelegateSymlinks: true}, {Provider: "config", Path: config}}}
	got, err := DiscoverLinks(home, DefaultLinkSearchRoots(home), resources, index, nil)
	if err != nil || len(got) != 3 || got[0].Classification != LinkUntrackedHomeTarget || got[1].Classification != LinkManagedInbound || got[2].Classification != LinkOwnershipConflict {
		t.Fatalf("links=%#v err=%v", got, err)
	}
}

func TestClassifyResourceLinksAndRelativeTarget(t *testing.T) {
	home := t.TempDir()
	scripts, dotfiles := filepath.Join(home, "Scripts"), filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(filepath.Join(dotfiles, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotfiles, "bin", "current"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scripts, "current")
	if err := os.Symlink(filepath.Join(dotfiles, "bin", "current"), link); err != nil {
		t.Fatal(err)
	}
	all := []profile.Resource{{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy"}, {ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git"}}
	got, err := ClassifyResourceLinks(home, all[0], []RawLink{{SourceAbsolute: link, RawTarget: filepath.Join(dotfiles, "bin", "current")}}, all)
	want := LinkCandidate{Source: "current", RawTarget: filepath.Join(dotfiles, "bin", "current"), Resolved: filepath.Join(dotfiles, "bin", "current"), Classification: LinkManagedResource, SourceResource: "scripts", TargetResource: "dotfiles", TargetRelative: "bin/current"}
	if err != nil || !reflect.DeepEqual(got, []LinkCandidate{want}) {
		t.Fatalf("links=%#v err=%v", got, err)
	}
	relative, err := RelativeSymlinkTarget(filepath.Join(home, ".config", "nvim"), filepath.Join(dotfiles, "nvim"))
	if err != nil || filepath.IsAbs(relative) {
		t.Fatalf("target=%q err=%v", relative, err)
	}
}
