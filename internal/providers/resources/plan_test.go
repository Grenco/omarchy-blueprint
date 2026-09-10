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
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/restore"
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

func TestPlanDetachedGitResourceStillPlansInboundLink(t *testing.T) {
	home := t.TempDir()
	p := Provider{HomeDir: home, ProfileDir: t.TempDir()}
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/Grenco/dotfiles", Branch: "main", Revision: strings.Repeat("a", 40)}}, Links: []profile.ResourceLink{{Source: "~/.config/nvim", TargetResource: "dotfiles", Target: "nvim", Origin: "inbound"}}}
	current := saved
	current.Items[0].Branch = ""
	current.Links = nil
	plan, err := p.Plan(context.Background(), saved, current, 7, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Action != "symlink" || len(plan.Skipped) != 0 {
		t.Fatalf("plan=%#v", plan)
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

func TestPlanForceReplacesOnlyConflictingResourceLink(t *testing.T) {
	home := t.TempDir()
	p := Provider{HomeDir: home, ProfileDir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "nvim"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	item := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/example/dotfiles", Revision: strings.Repeat("a", 40)}
	saved := profile.Resources{Items: []profile.Resource{item}, Links: []profile.ResourceLink{{Source: "~/.config/nvim", TargetResource: "dotfiles", Target: "nvim", Origin: "inbound"}}}
	plan, err := p.Plan(context.Background(), saved, profile.Resources{Items: []profile.Resource{item}}, 7, "", "", PlanOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 1 || plan.Operations[0].Risk != model.RiskHigh || plan.Operations[0].Symlink == nil || !plan.Operations[0].Symlink.ReplaceExisting || !plan.Operations[0].Symlink.Backup || plan.Operations[0].Symlink.ExpectedExisting.Type != "file" {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestPlanMissingGitDiffReconstructionChainsArtifactsAndLinks(t *testing.T) {
	home, profileDir := t.TempDir(), t.TempDir()
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	index, worktree, note := hashPlanBytes("index"), hashPlanBytes("worktree"), hashPlanBytes("note")
	item := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff", Remote: "https://example.invalid/dotfiles.git", Revision: strings.Repeat("a", 40), IndexPatchHash: index, WorktreePatchHash: worktree, Untracked: []profile.GitUntrackedFile{{Path: "notes.md", Hash: note, Mode: "0600"}}}
	saved := profile.Resources{Items: []profile.Resource{item}, Links: []profile.ResourceLink{{Source: "~/.config/nvim", TargetResource: "dotfiles", Target: "nvim", Origin: "inbound"}}}
	current := profile.Resources{Items: []profile.Resource{{ID: item.ID, Path: item.Path, Kind: item.Kind, Strategy: item.Strategy}}}
	plan, err := p.Plan(context.Background(), saved, current, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"resources.git.clone.dotfiles", "resources.git.checkout.dotfiles", "resources.git.apply-index.dotfiles", "resources.git.apply-worktree.dotfiles", "resources.git.untracked.dotfiles.notes-md.754b6dc3f872", "resources.link.home----config-nvim"}
	got := make([]string, len(plan.Operations))
	for i, op := range plan.Operations {
		got[i] = op.ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operations=%v want=%v", got, want)
	}
	for i := 1; i < len(plan.Operations); i++ {
		if !reflect.DeepEqual(plan.Operations[i].DependsOn, []string{plan.Operations[i-1].ID}) {
			t.Fatalf("%s dependencies=%v", plan.Operations[i].ID, plan.Operations[i].DependsOn)
		}
	}
	if op := plan.Operations[2]; op.GitPatch == nil || !op.GitPatch.ToIndex || op.GitPatch.SourceHash != index {
		t.Fatalf("index operation=%#v", op)
	}
	if op := plan.Operations[4]; op.File == nil || op.File.SourceHash != note || op.File.Mode == nil || *op.File.Mode != 0o600 || !op.File.ExpectedMissing {
		t.Fatalf("untracked operation=%#v", op)
	}
}

func TestPlanGitDiffUntrackedOperationIDsDoNotCollide(t *testing.T) {
	home := t.TempDir()
	p := Provider{HomeDir: home, ProfileDir: t.TempDir()}
	item := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff", Remote: "https://example.invalid/dotfiles.git", Revision: strings.Repeat("a", 40), Untracked: []profile.GitUntrackedFile{{Path: "a/b", Hash: hashPlanBytes("first"), Mode: "0600"}, {Path: "a-b", Hash: hashPlanBytes("second"), Mode: "0600"}}}
	plan, err := p.Plan(context.Background(), profile.Resources{Items: []profile.Resource{item}}, profile.Resources{}, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := restore.ValidatePlan(plan); err != nil {
		t.Fatalf("plan has colliding operation IDs: %v", err)
	}
	if got, want := []string{plan.Operations[2].ID, plan.Operations[3].ID}, []string{"resources.git.untracked.dotfiles.a-b.c14cddc033f6", "resources.git.untracked.dotfiles.a-b.d44362d67d92"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operation IDs=%v want=%v", got, want)
	}
}

func TestPlanGitDiffReconstructsRealGitState(t *testing.T) {
	home, profileDir, source, origin := t.TempDir(), t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "origin.git")
	runGitLifecycle(t, "init", "--bare", origin)
	runGitLifecycle(t, "init", source)
	runGitLifecycle(t, "-C", source, "config", "user.email", "test@example.invalid")
	runGitLifecycle(t, "-C", source, "config", "user.name", "Blueprint Test")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitLifecycle(t, "-C", source, "add", "file.txt")
	runGitLifecycle(t, "-C", source, "commit", "-m", "initial")
	runGitLifecycle(t, "-C", source, "remote", "add", "origin", origin)
	runGitLifecycle(t, "-C", source, "push", "origin", "HEAD")
	// Capture validates portable provenance; the plan uses the offline origin.
	runGitLifecycle(t, "-C", source, "remote", "set-url", "origin", "https://example.invalid/fixture.git")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitLifecycle(t, "-C", source, "add", "file.txt")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("C\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "notes.md"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	capture, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, source, []string{"notes.md"})
	if err != nil {
		t.Fatal(err)
	}
	working, ok, err := InspectGitWorkingState(context.Background(), command.SystemRunner{}, source)
	if err != nil || !ok {
		t.Fatalf("working=%#v ok=%t err=%v", working, ok, err)
	}
	stateRoot := filepath.Join(profileDir, "resources", "git-state", "dotfiles")
	if err := os.MkdirAll(filepath.Join(stateRoot, "untracked"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "index.patch"), capture.IndexPatch, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "worktree.patch"), capture.WorktreePatch, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "untracked", "notes.md"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff", Remote: origin, Revision: working.Revision, IndexPatchHash: capture.IndexPatchHash, WorktreePatchHash: capture.WorktreePatchHash, Untracked: capture.Untracked}}}
	p := Provider{HomeDir: home, ProfileDir: profileDir}
	plan, err := p.Plan(context.Background(), saved, profile.Resources{}, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := restore.NewJournal(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	result, err := restore.Execute(context.Background(), command.SystemRunner{}, plan, journal, time.Now, time.Second, nil)
	if err != nil || len(result.Failed) != 0 || len(result.Blocked) != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	destination := filepath.Join(home, "dotfiles")
	if got := runGitLifecycle(t, "-C", destination, "rev-parse", "HEAD"); got != working.Revision {
		t.Fatalf("HEAD=%q want=%q", got, working.Revision)
	}
	if got := runGitLifecycle(t, "-C", destination, "show", ":file.txt"); got != "B" {
		t.Fatalf("index=%q want=B", got)
	}
	if got := runGitLifecycle(t, "-C", destination, "diff", "--cached"); !strings.Contains(got, "+B") {
		t.Fatalf("cached diff=%q", got)
	}
	if got := runGitLifecycle(t, "-C", destination, "diff"); !strings.Contains(got, "+C") {
		t.Fatalf("worktree diff=%q", got)
	}
	if got, err := os.ReadFile(filepath.Join(destination, "notes.md")); err != nil || string(got) != "notes\n" {
		t.Fatalf("notes=%q err=%v", got, err)
	}
}

func runGitLifecycle(t *testing.T, args ...string) string {
	t.Helper()
	out, err := (command.SystemRunner{}).Run(context.Background(), "git", args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}

func TestPlanGitDiffExistingOverlayMismatchIsSkipped(t *testing.T) {
	home := t.TempDir()
	p := Provider{HomeDir: home, ProfileDir: t.TempDir()}
	item := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git+diff", Remote: "https://example.invalid/dotfiles.git", Revision: strings.Repeat("a", 40), IndexPatchHash: strings.Repeat("b", 64)}
	current := item
	current.IndexPatchHash = ""
	if err := os.Mkdir(filepath.Join(home, "dotfiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := p.Plan(context.Background(), profile.Resources{Items: []profile.Resource{item}}, profile.Resources{Items: []profile.Resource{current}}, 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "existing resource differs") {
		t.Fatalf("plan=%#v", plan)
	}
}

func hashPlanBytes(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
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
