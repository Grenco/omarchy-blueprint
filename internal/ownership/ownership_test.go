package ownership

import (
	"path/filepath"
	"testing"
)

func TestOwnershipDelegatesSymlinkLeavesOnly(t *testing.T) {
	root := t.TempDir()
	hooks := filepath.Join(root, ".config", "omarchy", "hooks")
	index := Index{Claims: []Claim{{Provider: "hooks", Path: hooks, Recursive: true, DelegateSymlinks: true}}}
	if got := index.TrackConflict(hooks); len(got) != 1 {
		t.Fatalf("track conflict=%#v", got)
	}
	if got := index.LinkConflict(filepath.Join(hooks, "theme-set")); len(got) != 0 {
		t.Fatalf("delegated link conflict=%#v", got)
	}
}

func TestOwnershipBlocksNonDelegatedConfigLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.lua")
	index := Index{Claims: []Claim{{Provider: "config", Path: path}}}
	if len(index.LinkConflict(path)) != 1 {
		t.Fatal("config-owned link source should conflict")
	}
}
