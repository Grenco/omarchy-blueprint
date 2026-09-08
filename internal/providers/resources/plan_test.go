package resources

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestPlanMissingCopiedFileUsesContentSnapshot(t *testing.T) {
	p, saved := plannedCopyFile(t)
	plan, err := p.Plan(context.Background(), saved, profile.Resources{}, 7, "old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	op := plan.Operations[0]
	if op.File == nil || op.File.Source != filepath.Join(p.ProfileDir, "resources", "files", "deploy", "content") || !op.File.ExpectedMissing || op.File.SourceHash != saved.Items[0].Hash || op.File.Mode == nil || *op.File.Mode != 0o755 || !op.File.RejectSymlinkParents {
		t.Fatalf("operation=%#v", op)
	}
}

func TestPlanExistingCopyMismatchIsSkipped(t *testing.T) {
	p, saved := plannedCopyFile(t)
	destination, _ := ExpandHomePath(p.HomeDir, saved.Items[0].Path)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("different"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(context.Background(), saved, profile.Resources{}, 7, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "existing resource differs") {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestPlanMissingCopyDirectoryAndGitDependencies(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	scripts := profile.Resource{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "hash", Mode: "0755"}
	if err := os.MkdirAll(filepath.Join(profileDir, "resources", "files", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	git := profile.Resource{ID: "dotfiles", Path: "~/Projects/dotfiles", Kind: "directory", Strategy: "git", Remote: "https://github.com/example/dotfiles.git", Revision: strings.Repeat("a", 40)}
	saved := profile.Resources{Items: []profile.Resource{scripts, git}}
	plan, err := p.Plan(context.Background(), saved, profile.Resources{}, 7, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 4 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	var copy, mkdir, clone, checkout model.Operation
	for _, op := range plan.Operations {
		switch op.ID {
		case "resources.copy.scripts":
			copy = op
		case "resources.mkdir.dotfiles":
			mkdir = op
		case "resources.git.clone.dotfiles":
			clone = op
		case "resources.git.checkout.dotfiles":
			checkout = op
		}
	}
	if copy.Copy == nil || copy.Copy.Source != filepath.Join(profileDir, "resources", "files", "scripts") || copy.Copy.SourceHash != "hash" {
		t.Fatalf("copy=%#v", copy)
	}
	if mkdir.Directory == nil || clone.Command[0] != "gh" || !reflect.DeepEqual(clone.Command, []string{"gh", "repo", "clone", "example/dotfiles", filepath.Join(home, "Projects", "dotfiles"), "--", "--no-checkout"}) || len(clone.DependsOn) != 1 || clone.DependsOn[0] != mkdir.ID || len(checkout.DependsOn) != 1 || checkout.DependsOn[0] != clone.ID {
		t.Fatalf("git operations=%#v %#v %#v", mkdir, clone, checkout)
	}
}

func TestPlanLinksAreSemanticAndDependOnRestores(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(profileDir, "resources", "files", "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	scripts := profile.Resource{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "hash", Mode: "0755"}
	dotfiles := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "https://github.com/example/dotfiles.git", Revision: strings.Repeat("a", 40)}
	saved := profile.Resources{Items: []profile.Resource{scripts, dotfiles}, Links: []profile.ResourceLink{{SourceResource: "scripts", Source: "current", TargetResource: "dotfiles", Target: "bin/current", Origin: "resource"}, {Source: "~/.config/nvim", TargetResource: "dotfiles", Target: "nvim", Origin: "inbound"}}}
	plan, err := p.Plan(context.Background(), saved, profile.Resources{}, 7, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var links []model.Operation
	for _, op := range plan.Operations {
		if op.Symlink != nil {
			links = append(links, op)
		}
	}
	if len(links) != 2 {
		t.Fatalf("plan=%#v", plan)
	}
	for _, op := range links {
		if strings.Contains(op.Symlink.Destination, "Scripts") && len(op.DependsOn) != 2 {
			t.Fatalf("cross-resource dependencies=%#v", op)
		}
		if filepath.IsAbs(op.Symlink.Target) {
			t.Fatalf("absolute link target=%q", op.Symlink.Target)
		}
	}

	// An absolute target is semantically equivalent to the planned relative target.
	existing := filepath.Join(home, ".config", "nvim")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "dotfiles", "nvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles", "nvim"), existing); err != nil {
		t.Fatal(err)
	}
	current := profile.Resources{Items: []profile.Resource{dotfiles}}
	plan, err = p.Plan(context.Background(), profile.Resources{Items: []profile.Resource{dotfiles}, Links: saved.Links[1:]}, current, 7, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Skipped) != 0 {
		t.Fatalf("equivalent link plan=%#v", plan)
	}
}

func TestPlanSuppressesLinkForConflictingTargetAndRejectsMode(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	item := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "https://github.com/example/dotfiles.git", Revision: strings.Repeat("a", 40)}
	if err := os.MkdirAll(filepath.Join(home, "dotfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := profile.Resources{Items: []profile.Resource{item}, Links: []profile.ResourceLink{{Source: "~/.config/nvim", TargetResource: "dotfiles", Target: "nvim", Origin: "inbound"}}}
	plan, err := p.Plan(context.Background(), saved, profile.Resources{}, 7, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Skipped) != 2 || !strings.Contains(plan.Skipped[1].Reason, "not satisfied") {
		t.Fatalf("plan=%#v", plan)
	}
	bad := profile.Resources{Items: []profile.Resource{{ID: "bad", Path: "~/bad", Kind: "file", Strategy: "copy", Hash: "x", Mode: "999"}}}
	if _, err := p.Plan(context.Background(), bad, profile.Resources{}, 7, "", ""); err == nil || !strings.Contains(err.Error(), "invalid resource mode") {
		t.Fatalf("err=%v", err)
	}
}

func plannedCopyFile(t *testing.T) (Provider, profile.Resources) {
	t.Helper()
	home, profileDir := t.TempDir(), t.TempDir()
	source := filepath.Join(profileDir, "resources", "files", "deploy", "content")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("echo deploy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scan, err := ScanCopyResource(source)
	if err != nil {
		t.Fatal(err)
	}
	return Provider{HomeDir: home, ProfileDir: profileDir}, profile.Resources{Items: []profile.Resource{{ID: "deploy", Path: "~/.local/bin/deploy", Kind: "file", Strategy: "copy", Hash: scan.Hash, Mode: "0755"}}}
}
