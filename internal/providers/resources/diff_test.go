package resources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestDiffAndVerifyResourcesAndLinks(t *testing.T) {
	saved := profile.Resources{Items: []profile.Resource{{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "old", Mode: "0755"}}, Links: []profile.ResourceLink{{Source: "~/.config/tool", TargetResource: "scripts", Target: "tool", Origin: "inbound"}}}
	current := profile.Resources{Items: []profile.Resource{{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "new", Mode: "0755"}}, Links: []profile.ResourceLink{{Source: "~/.config/tool", TargetResource: "scripts", Target: "changed", Origin: "inbound"}, {Source: "~/.config/extra", TargetResource: "scripts", Target: "extra", Origin: "inbound"}}}
	changes := Diff(saved, current)
	if len(changes) != 3 || changes[0].Kind != "link" || changes[2].Kind != "resource" || changes[2].Type != model.ChangeModify {
		t.Fatalf("changes=%#v", changes)
	}
	result := Verify(saved, current)
	if result.OK || !reflect.DeepEqual(result.Missing, []string{"link:~/.config/tool", "resource:scripts"}) {
		t.Fatalf("result=%#v", result)
	}
	current.Links[0].Target = "tool"
	current.Items[0].Hash = "old"
	if !Verify(saved, current).OK {
		t.Fatal("extra inbound link failed verification")
	}
}

func TestDiffAndVerifyDetachedGitResourceAreClean(t *testing.T) {
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/Grenco/dotfiles", Branch: "main", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	current := profile.Resources{Items: append([]profile.Resource(nil), saved.Items...)}
	current.Items[0].Branch = ""
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes=%#v", changes)
	}
	if result := Verify(saved, current); !result.OK {
		t.Fatalf("result=%#v", result)
	}
}

func TestDirtyGitResourceIsSatisfiedByRemoteAndRevision(t *testing.T) {
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/example/dotfiles", Revision: strings.Repeat("a", 40)}}}
	current := profile.Resources{Items: append([]profile.Resource(nil), saved.Items...)}
	current.Items[0].Untracked = append([]profile.GitUntrackedFile(nil), saved.Items[0].Untracked...)
	current.Items[0].Dirty = true
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes=%#v", changes)
	}
	if result := Verify(saved, current); !result.OK {
		t.Fatalf("result=%#v", result)
	}
	current.Items[0].Revision = strings.Repeat("b", 40)
	if changes := Diff(saved, current); len(changes) != 1 {
		t.Fatalf("changes=%#v", changes)
	}
}

func TestGitDiffReportsOnlyManagedOverlayDrift(t *testing.T) {
	hash := strings.Repeat("a", 64)
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff", Remote: "github.com/example/dotfiles", Revision: strings.Repeat("b", 40), IndexPatchHash: hash, WorktreePatchHash: hash, Untracked: []profile.GitUntrackedFile{{Path: "notes.md", Hash: hash, Mode: "0644"}}}}}
	current := profile.Resources{Items: append([]profile.Resource(nil), saved.Items...)}
	current.Items[0].Untracked = append([]profile.GitUntrackedFile(nil), saved.Items[0].Untracked...)
	current.Items[0].Dirty = true // Extra unselected state is informational only.
	if changes := Diff(saved, current); len(changes) != 0 || !Verify(saved, current).OK {
		t.Fatalf("changes=%#v verify=%#v", changes, Verify(saved, current))
	}
	current.Items[0].IndexPatchHash = strings.Repeat("c", 64)
	current.Items[0].WorktreePatchHash = strings.Repeat("d", 64)
	current.Items[0].Untracked = nil
	changes := Diff(saved, current)
	if len(changes) != 3 || !strings.Contains(changes[0].Summary, "staged") || !strings.Contains(changes[1].Summary, "unstaged") || !strings.Contains(changes[2].Summary, "untracked:notes.md missing") {
		t.Fatalf("changes=%#v", changes)
	}
}

func TestCheckValidatesGitDiffArtifacts(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	stateRoot := filepath.Join(profileDir, "resources", "git-state", "dotfiles")
	if err := os.MkdirAll(filepath.Join(stateRoot, "untracked"), 0o755); err != nil {
		t.Fatal(err)
	}
	index, note := []byte("index"), []byte("note")
	if err := os.WriteFile(filepath.Join(stateRoot, "index.patch"), index, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "untracked", "notes.md"), note, 0o644); err != nil {
		t.Fatal(err)
	}
	indexHash, noteHash := fmtHash(index), fmtHash(note)
	resources := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff", Remote: "github.com/example/dotfiles", Revision: strings.Repeat("a", 40), IndexPatchHash: indexHash, Untracked: []profile.GitUntrackedFile{{Path: "notes.md", Hash: noteHash, Mode: "0644"}}}}}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: gitRunner{output: map[string]string{"git --version": "git version"}}}
	if err := p.Check(context.Background(), resources); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "index.patch"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Check(context.Background(), resources); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("err=%v", err)
	}
}

func fmtHash(value []byte) string { return fmt.Sprintf("%x", sha256.Sum256(value)) }
