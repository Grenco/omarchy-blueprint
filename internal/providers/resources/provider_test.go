package resources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/content"
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
	got, changes, err := p.Track(context.Background(), profile.Resources{}, source, TrackOptions{})
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
	if _, _, err := p.Track(context.Background(), profile.Resources{}, link, TrackOptions{}); err == nil {
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

func TestPrepareUntrackRemovesGitStateInGeneration(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	for _, path := range []string{"files/dotfiles/content", "git-state/dotfiles/untracked/notes.md", "files/other/content"} {
		full := filepath.Join(profileDir, "resources", path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(path), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff"}, {ID: "other", Path: "~/other", Kind: "file", Strategy: "copy"}}}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	prepared, _, err := p.PrepareUntrack(saved, "resource:dotfiles")
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Install(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(profileDir, "resources", "git-state", "dotfiles")); !os.IsNotExist(err) {
		t.Fatalf("Git state retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "resources", "files", "other", "content")); err != nil {
		t.Fatalf("other snapshot removed: %v", err)
	}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, "resources", "git-state", "dotfiles", "untracked", "notes.md")); err != nil {
		t.Fatalf("Git state not restored: %v", err)
	}
}

func TestStageUntrackedFileMetadataComesFromStagedBytes(t *testing.T) {
	root := t.TempDir()
	source, destination := filepath.Join(root, "notes.md"), filepath.Join(root, "staged", "notes.md")
	if err := os.WriteFile(source, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scan, err := stageUntrackedFile(source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := content.HashRegularFile(destination)
	if err != nil || scan.Hash != hash || scan.Mode != 0o600 {
		t.Fatalf("metadata=%#v hash=%s err=%v", scan, hash, err)
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

func TestDetectCopyGitWorktreeExcludesGitAdministration(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitLifecycle(t, "init", root)
	if err := os.WriteFile(filepath.Join(root, "tracked"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	expected, err := ScanCopyResourceWithOptions(root, SnapshotOptions{ExcludeGitAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	includingAdmin, err := ScanCopyResource(root)
	if err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, Runner: command.SystemRunner{}}
	current, _, err := p.Detect(context.Background(), profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "copy"}}})
	if err != nil {
		t.Fatal(err)
	}
	if current.Items[0].Hash != expected.Hash || current.Items[0].Hash == includingAdmin.Hash {
		t.Fatalf("detected=%s excluded=%s included=%s", current.Items[0].Hash, expected.Hash, includingAdmin.Hash)
	}
}

func TestTrackAndCaptureDirtyGitStateIsInformational(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "1 .M N... 100644 100644 100644 a a init.lua\x00? experiment.sh\x00",
		"git -C " + root + " remote get-url origin":                                                   "https://github.com/example/dotfiles.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          revision + "\n",
	}}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: runner}
	captured, changes, err := p.Track(context.Background(), profile.Resources{}, root, TrackOptions{})
	if err != nil || len(captured.Items) != 1 || captured.Items[0].Revision != revision || !captured.Items[0].Dirty || len(changes) != 0 {
		t.Fatalf("resources=%#v changes=%#v err=%v", captured, changes, err)
	}
	current, _, err := p.Detect(context.Background(), profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "https://github.com/example/dotfiles.git", Revision: revision}}})
	if err != nil || current.Items[0].Revision != revision || !current.Items[0].Dirty || !Verify(captured, current).OK {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	detection, err := p.DetectDetailed(context.Background(), captured)
	if err != nil || detection.Git["dotfiles"].UnstagedTracked != 1 || len(detection.Git["dotfiles"].Untracked) != 1 {
		t.Fatalf("detection=%#v err=%v", detection, err)
	}
}

func TestPrepareTrackUsesStrategiesAndUpdatesExactPath(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	gitRoot := filepath.Join(home, "dotfiles")
	plain := filepath.Join(home, "plain")
	for _, root := range []string{gitRoot, plain} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + gitRoot + " rev-parse --show-toplevel":                                               gitRoot + "\n",
		"git -C " + gitRoot + " remote get-url origin":                                                   "https://github.com/example/dotfiles.git\n",
		"git -C " + gitRoot + " rev-parse HEAD":                                                          revision + "\n",
		"git -C " + gitRoot + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "",
	}}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: runner}
	prepared, err := p.PrepareTrack(context.Background(), profile.Resources{}, gitRoot, TrackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared.State.Items[0]; got.Strategy != "git" || got.Revision != revision {
		t.Fatalf("resource=%#v", got)
	}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PrepareTrack(context.Background(), profile.Resources{}, plain, TrackOptions{Strategy: "git"}); err == nil {
		t.Fatal("non-Git directory accepted as Git")
	}
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/example/dotfiles", Revision: revision}}}
	prepared, err = p.PrepareTrack(context.Background(), saved, gitRoot, TrackOptions{Strategy: "copy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.State.Items) != 1 || prepared.State.Items[0].ID != "dotfiles" || prepared.State.Items[0].Strategy != "copy" {
		t.Fatalf("resource=%#v", prepared.State.Items)
	}
	if err := prepared.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PrepareTrack(context.Background(), saved, gitRoot, TrackOptions{ID: "other"}); err == nil {
		t.Fatal("mismatched ID accepted for exact path")
	}
}

func TestPrepareTrackGitDiffStagesArtifactsTransactionally(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("b", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " remote get-url origin":                                                   "https://github.com/example/dotfiles.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          revision + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "? notes.md\x00",
		"git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -c diff.compactionHeuristic=false -c diff.context=3 -c diff.interHunkContext=0 -c core.quotePath=true -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --cached HEAD --": "index patch",
		"git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -c diff.compactionHeuristic=false -c diff.context=3 -c diff.interHunkContext=0 -c core.quotePath=true -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --":               "worktree patch",
	}}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: runner}
	prepared, err := p.PrepareTrack(context.Background(), profile.Resources{}, root, TrackOptions{Strategy: "git+diff", IncludeUntracked: []string{"notes.md"}})
	if err != nil {
		t.Fatal(err)
	}
	item := prepared.State.Items[0]
	if item.Strategy != "git+diff" || item.IndexPatchHash == "" || item.WorktreePatchHash == "" || len(item.Untracked) != 1 {
		t.Fatalf("resource=%#v", item)
	}
	if err := prepared.Install(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.patch", "worktree.patch", filepath.Join("untracked", "notes.md")} {
		if _, err := os.Stat(filepath.Join(profileDir, "resources", "git-state", item.ID, path)); err != nil {
			t.Fatalf("artifact %s: %v", path, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(root, "notes.md")); err != nil || string(body) != "notes\n" {
		t.Fatalf("live file=%q err=%v", body, err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureGitDiffRecapturesSelectedUntrackedAndIsIdempotent(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(root, "notes.md")
	if err := os.WriteFile(note, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := gitDiffTestRunner(root, "? notes.md\x00")
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: runner}
	saved, _, err := p.Track(context.Background(), profile.Resources{}, root, TrackOptions{Strategy: "git+diff", IncludeUntracked: []string{"notes.md"}})
	if err != nil {
		t.Fatal(err)
	}
	resourcesPath := filepath.Join(profileDir, "resources", "resources.toml")
	artifact := filepath.Join(profileDir, "resources", "git-state", "dotfiles", "untracked", "notes.md")
	beforeTOML, err := os.ReadFile(resourcesPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeArtifact, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	again, changes, err := p.Capture(context.Background(), saved)
	if err != nil || len(changes) != 0 || !reflect.DeepEqual(again, saved) {
		t.Fatalf("recapture=%#v changes=%#v err=%v", again, changes, err)
	}
	if got, _ := os.ReadFile(resourcesPath); !reflect.DeepEqual(got, beforeTOML) {
		t.Fatal("resources.toml changed on idempotent capture")
	}
	if got, _ := os.ReadFile(artifact); !reflect.DeepEqual(got, beforeArtifact) {
		t.Fatal("untracked artifact changed on idempotent capture")
	}
	if err := os.WriteFile(note, []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(note, 0o644); err != nil {
		t.Fatal(err)
	}
	updated, _, err := p.Capture(context.Background(), saved)
	if err != nil || updated.Items[0].Untracked[0].Hash == saved.Items[0].Untracked[0].Hash || updated.Items[0].Untracked[0].Mode != "0644" {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if got, _ := os.ReadFile(artifact); string(got) != "two\n" {
		t.Fatalf("artifact=%q", got)
	}
}

func TestCaptureGitDiffDropsMissingSelectedUntrackedAndPreservesUnsafeState(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(root, "notes.md")
	if err := os.WriteFile(note, []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: gitDiffTestRunner(root, "? notes.md\x00")}
	saved, _, err := p.Track(context.Background(), profile.Resources{}, root, TrackOptions{Strategy: "git+diff", IncludeUntracked: []string{"notes.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	missingRunner := gitDiffTestRunner(root, "")
	missingRunner.err["git -C "+root+" check-ignore -q -- notes.md"] = errors.New("not ignored")
	p.Runner = missingRunner
	dropped, _, err := p.Capture(context.Background(), saved)
	if err != nil || len(dropped.Items[0].Untracked) != 0 {
		t.Fatalf("dropped=%#v err=%v", dropped, err)
	}
	if _, err := os.Lstat(filepath.Join(profileDir, "resources", "git-state", "dotfiles", "untracked", "notes.md")); !os.IsNotExist(err) {
		t.Fatalf("removed selection artifact retained: %v", err)
	}

	// An ignored former selection must fail before the staged generation is installed.
	before, err := os.ReadFile(filepath.Join(profileDir, "resources", "resources.toml"))
	if err != nil {
		t.Fatal(err)
	}
	p.Runner = gitDiffTestRunner(root, "")
	if _, _, err := p.Capture(context.Background(), saved); err == nil || !strings.Contains(err.Error(), "now ignored") {
		t.Fatalf("ignored selection err=%v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(profileDir, "resources", "resources.toml")); !reflect.DeepEqual(got, before) {
		t.Fatal("resources metadata changed after rejected capture")
	}
}

func TestCaptureGitDiffUnsafeStateLeavesArtifactsUntouched(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: gitDiffTestRunner(root, "")}
	saved, _, err := p.Track(context.Background(), profile.Resources{}, root, TrackOptions{Strategy: "git+diff"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(profileDir, "resources", "resources.toml")
	before, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	for name, status := range map[string]string{"conflict": "u UU N... 100644 100644 100644 100644 a b c file.txt\x00", "submodule": "1 M. S.M... 160000 160000 160000 a b module\x00"} {
		t.Run(name, func(t *testing.T) {
			p.Runner = gitDiffTestRunner(root, status)
			if _, _, err := p.Capture(context.Background(), saved); err == nil {
				t.Fatal("unsafe Git state captured")
			}
			if got, _ := os.ReadFile(artifact); !reflect.DeepEqual(got, before) {
				t.Fatal("resources metadata changed after rejected capture")
			}
		})
	}
}

func TestDetectGitDiffWithSensitiveChangesSucceedsWhileCapturePreservesState(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	root := filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "remote", "add", "origin", "https://github.com/example/dotfiles.git")
	writeGitFile(t, root, "tracked.txt", "safe\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	writeGitFile(t, root, "notes.md", "safe\n")
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: command.SystemRunner{}}
	saved, _, err := p.Track(context.Background(), profile.Resources{}, root, TrackOptions{Strategy: "git+diff", IncludeUntracked: []string{"notes.md"}})
	if err != nil {
		t.Fatal(err)
	}
	resourcesPath := filepath.Join(profileDir, "resources", "resources.toml")
	artifactPath := filepath.Join(profileDir, "resources", "git-state", "dotfiles", "untracked", "notes.md")
	beforeResources, err := os.ReadFile(resourcesPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeArtifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	writeGitFile(t, root, "tracked.txt", "api_token=abcdefghijklmnopqrstuvwxyz\n")
	writeGitFile(t, root, "notes.md", "api_token=abcdefghijklmnopqrstuvwxyz\n")

	detection, err := p.DetectDetailed(context.Background(), saved)
	if err != nil || detection.Git["dotfiles"].UnstagedTracked != 1 || len(detection.Git["dotfiles"].Untracked) != 1 || len(Diff(saved, detection.Resources)) == 0 {
		t.Fatalf("detection=%#v diff=%#v err=%v", detection, Diff(saved, detection.Resources), err)
	}
	if _, _, err := p.Capture(context.Background(), saved); err == nil || !strings.Contains(err.Error(), "sensitive tracked content") {
		t.Fatalf("capture err=%v", err)
	}
	if got, err := os.ReadFile(resourcesPath); err != nil || !reflect.DeepEqual(got, beforeResources) {
		t.Fatalf("resources metadata changed after rejected capture: %v", err)
	}
	if got, err := os.ReadFile(artifactPath); err != nil || !reflect.DeepEqual(got, beforeArtifact) {
		t.Fatalf("untracked artifact changed after rejected capture: %v", err)
	}
	writeGitFile(t, root, "tracked.txt", "safe\n")
	if _, _, err := p.Capture(context.Background(), saved); err == nil || !strings.Contains(err.Error(), "sensitive untracked content") {
		t.Fatalf("capture err=%v", err)
	}
	if got, err := os.ReadFile(resourcesPath); err != nil || !reflect.DeepEqual(got, beforeResources) {
		t.Fatalf("resources metadata changed after rejected untracked capture: %v", err)
	}
	if got, err := os.ReadFile(artifactPath); err != nil || !reflect.DeepEqual(got, beforeArtifact) {
		t.Fatalf("untracked artifact changed after rejected capture: %v", err)
	}
}

func gitDiffTestRunner(root, status string) gitRunner {
	revision := strings.Repeat("a", 40)
	return gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                                                       root + "\n",
		"git -C " + root + " remote get-url origin":                                                                           "https://example.invalid/dotfiles.git\n",
		"git -C " + root + " rev-parse HEAD":                                                                                  revision + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none":                         status,
		"git -C " + root + " diff --cached HEAD --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --": "index patch",
		"git -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --":               "worktree patch",
	}, err: map[string]error{}}
}

func TestMappedRootsCaptureAndDetectAllStrategiesWithoutRewritingPortablePaths(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	copyRoot := filepath.Join(home, "Code", "copy")
	gitRoot := filepath.Join(home, "Code", "git")
	diffRoot := filepath.Join(home, "Code", "diff")
	for _, root := range []string{copyRoot, gitRoot, diffRoot} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(copyRoot, "current"), []byte("mapped"), 0o644); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{}, err: map[string]error{}}
	for _, root := range []string{gitRoot, diffRoot} {
		runner.output["git -C "+root+" rev-parse --show-toplevel"] = root + "\n"
		runner.output["git -C "+root+" remote get-url origin"] = "https://example.invalid/repo.git\n"
		runner.output["git -C "+root+" rev-parse HEAD"] = revision + "\n"
		runner.output["git -C "+root+" status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none"] = ""
	}
	diffPrefix := "git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -c diff.compactionHeuristic=false -c diff.context=3 -c diff.interHunkContext=0 -c core.quotePath=true -C " + diffRoot + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames"
	runner.output[diffPrefix+" --cached HEAD --"] = "index patch"
	runner.output[diffPrefix+" --"] = "worktree patch"
	saved := profile.Resources{Items: []profile.Resource{
		{ID: "copy", Path: "~/Projects/copy", Kind: "directory", Strategy: "copy"},
		{ID: "git", Path: "~/Projects/git", Kind: "directory", Strategy: "git", Remote: "https://example.invalid/repo.git", Revision: revision},
		{ID: "diff", Path: "~/Projects/diff", Kind: "directory", Strategy: "git+diff", Remote: "https://example.invalid/repo.git", Revision: revision},
	}}
	p := Provider{HomeDir: home, ProfileDir: profileDir, Runner: runner, ResourcePaths: ResourcePaths{Home: home, Overrides: map[string]string{"copy": "~/Code/copy", "git": "~/Code/git", "diff": "~/Code/diff"}}}
	captured, _, err := p.Capture(context.Background(), saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range captured.Items {
		if item.Path != "~/Projects/"+item.ID {
			t.Fatalf("resource %s path = %q", item.ID, item.Path)
		}
	}
	items := resourceMap(captured.Items)
	if items["copy"].Hash == "" || items["git"].Revision != revision || items["diff"].IndexPatchHash == "" {
		t.Fatalf("captured=%#v", captured.Items)
	}
	current, _, err := p.Detect(context.Background(), captured)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(captured, current).OK {
		t.Fatalf("mapped resources not satisfied: captured=%#v current=%#v", captured, current)
	}
}

func TestPrepareTrackUpdatesMappedExistingResourceWithoutRewritingPortablePath(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	mapped := filepath.Join(home, "Code", "repo")
	if err := os.MkdirAll(mapped, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mapped, "current"), []byte("mapped"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := profile.Resources{Items: []profile.Resource{{ID: "repo", Path: "~/Projects/repo", Kind: "directory", Strategy: "copy"}}}
	p := Provider{HomeDir: home, ProfileDir: profileDir, ResourcePaths: ResourcePaths{Home: home, Overrides: map[string]string{"repo": "~/Code/repo"}}}
	prepared, err := p.PrepareTrack(context.Background(), saved, mapped, TrackOptions{Strategy: "copy"})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Rollback()
	if len(prepared.State.Items) != 1 || prepared.State.Items[0].ID != "repo" || prepared.State.Items[0].Path != "~/Projects/repo" {
		t.Fatalf("tracked=%#v", prepared.State.Items)
	}
}

func TestCheckValidatesEffectiveMappedRoots(t *testing.T) {
	home := t.TempDir()
	p := Provider{HomeDir: home, ResourcePaths: ResourcePaths{Home: home, Overrides: map[string]string{"one": "~/Code", "two": "~/Code/nested"}}}
	saved := profile.Resources{Items: []profile.Resource{
		{ID: "one", Path: "~/Projects/one", Kind: "directory", Strategy: "copy", Hash: strings.Repeat("a", 64), Mode: "0755"},
		{ID: "two", Path: "~/Projects/two", Kind: "directory", Strategy: "copy", Hash: strings.Repeat("b", 64), Mode: "0755"},
	}}
	if err := p.Check(context.Background(), saved); err == nil || !strings.Contains(err.Error(), "resource roots overlap") {
		t.Fatalf("check err=%v", err)
	}
}
