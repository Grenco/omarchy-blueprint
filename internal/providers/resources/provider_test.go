package resources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestTrackFileCapturesAndAdoptsInboundLink(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	source := filepath.Join(home, "dotfiles", "deploy")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("echo deploy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dotfiles/deploy", link); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	got, changes, err := p.Track(context.Background(), profile.Resources{}, source, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "deploy" || got.Items[0].Strategy != "copy" || got.Items[0].Hash == "" {
		t.Fatalf("resources=%#v", got)
	}
	if len(got.Links) != 1 || got.Links[0].Source != "~/.config/example" || got.Links[0].TargetResource != "deploy" {
		t.Fatalf("links=%#v", got.Links)
	}
	if len(changes) != 2 {
		t.Fatalf("changes=%#v", changes)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "resources", "files", "deploy", "content")); err != nil {
		t.Fatal(err)
	}
}

func TestTrackRejectsSymlinksAndLeavesSnapshotsUntouched(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	target := filepath.Join(home, "target")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(profileDir, "resources", "files", "old", "content")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	if _, _, err := p.Track(context.Background(), profile.Resources{}, link, ""); err == nil {
		t.Fatal("symlink tracked")
	}
	if body, err := os.ReadFile(old); err != nil || string(body) != "old" {
		t.Fatalf("snapshot=%q err=%v", body, err)
	}
}

func TestCaptureFailureLeavesOldSnapshotsAndMetadataUntouched(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "Scripts")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(profileDir, "resources", "files", "scripts", "content")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/outside", filepath.Join(root, "bad")); err != nil {
		t.Fatal(err)
	}
	saved := profile.Resources{Items: []profile.Resource{{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "old", Mode: "0755"}}}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	got, _, err := p.Capture(context.Background(), saved)
	if err == nil || !strings.Contains(err.Error(), "contains") {
		t.Fatalf("err=%v", err)
	}
	if !reflect.DeepEqual(got, saved) {
		t.Fatalf("metadata changed: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(old, "old")); err != nil {
		t.Fatalf("old snapshot removed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(profileDir, "resources"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".capture-") {
			t.Fatalf("staging retained: %s", entry.Name())
		}
	}
}

func TestUntrackAndEnableLinkKeepLivePaths(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	target := filepath.Join(home, "dotfiles", "value")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(home, ".config", "example")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dotfiles/value", source); err != nil {
		t.Fatal(err)
	}
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy", Hash: "x", Mode: "0755"}}, Links: []profile.ResourceLink{{Source: "~/.config/example", TargetResource: "dotfiles", Target: "value", Origin: "inbound"}}}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	ignored, _, err := p.Untrack(saved, "link:~/.config/example")
	if err != nil {
		t.Fatal(err)
	}
	if len(ignored.Links) != 0 || len(ignored.IgnoredLinks) != 1 {
		t.Fatalf("state=%#v", ignored)
	}
	enabled, err := p.EnableLink(ignored, "~/.config/example")
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled.IgnoredLinks) != 0 {
		t.Fatalf("state=%#v", enabled)
	}
	updated, _, err := p.Untrack(saved, "resource:dotfiles")
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Items) != 0 || len(updated.Links) != 0 {
		t.Fatalf("state=%#v", updated)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(source); err != nil {
		t.Fatal(err)
	}
}

func TestCheckOnlyRequiresGitWhenGitResourceExists(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	file := filepath.Join(home, "deploy")
	if err := os.WriteFile(file, []byte("ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	scan, err := StageCopyResource(file, filepath.Join(profileDir, "resources", "files", "deploy", "content"))
	if err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: gitRunner{err: map[string]error{"git --version": errors.New("should not run")}}}
	copy := profile.Resources{Items: []profile.Resource{{ID: "deploy", Path: "~/deploy", Kind: "file", Strategy: "copy", Hash: scan.Hash, Mode: "0755"}}}
	if err := p.Check(context.Background(), copy); err != nil {
		t.Fatal(err)
	}
	git := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "https://github.com/example/dotfiles.git", Revision: strings.Repeat("a", 40)}}}
	if err := p.Check(context.Background(), git); err == nil {
		t.Fatal("missing git accepted")
	}
}

func TestDetectRebuildsInternalResourceLinks(t *testing.T) {
	home := t.TempDir()
	scripts, dotfiles := filepath.Join(home, "Scripts"), filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(filepath.Join(dotfiles, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dotfiles, "bin", "tool"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dotfiles, "bin", "tool"), filepath.Join(scripts, "tool")); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home}
	saved := profile.Resources{Items: []profile.Resource{
		{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy"},
		{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"},
	}}
	current, _, err := p.Detect(context.Background(), saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Links) != 1 || current.Links[0].SourceResource != "scripts" || current.Links[0].TargetResource != "dotfiles" || current.Links[0].Target != "bin/tool" {
		t.Fatalf("links=%#v", current.Links)
	}
}
