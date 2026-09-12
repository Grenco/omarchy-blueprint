package profilegit

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

func TestStatusNonRepositoryAndParentRepository(t *testing.T) {
	root := t.TempDir()
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Repository {
		t.Fatal("non-repository profile reported as repository")
	}

	profileRoot := filepath.Join(root, "profile")
	mustMkdir(t, profileRoot)
	git(t, root, "init", "-b", "main")
	service, err = New(command.SystemRunner{}, profileRoot)
	if err != nil {
		t.Fatal(err)
	}
	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Repository {
		t.Fatal("profile inside parent repository reported as repository")
	}
}

func TestStatusPorcelainWithManagedPathsAndRename(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Repository || status.Head != "" {
		t.Fatalf("uninitialized repository status = %#v", status)
	}
	git(t, root, "config", "user.name", "Blueprint Test")
	git(t, root, "config", "user.email", "blueprint@example.test")
	mustWrite(t, filepath.Join(root, "profile.toml"), "initial\n")
	mustWrite(t, filepath.Join(root, "README.md"), "initial\n")
	mustWrite(t, filepath.Join(root, "config", "old name.conf"), "initial\n")
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "initial")

	mustWrite(t, filepath.Join(root, "profile.toml"), "modified\n")
	mustWrite(t, filepath.Join(root, "README.md"), "modified\n")
	mustWrite(t, filepath.Join(root, "packages", "new file.txt"), "new\n")
	mustWrite(t, filepath.Join(root, "themes", "untracked theme.txt"), "new\n")
	mustMkdir(t, filepath.Join(root, "notes"))
	if err := os.Rename(filepath.Join(root, "config", "old name.conf"), filepath.Join(root, "notes", "renamed file.conf")); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "packages/new file.txt", "config/old name.conf", "notes/renamed file.conf")

	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Repository || status.Branch != "main" || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(status.Head) {
		t.Fatalf("unexpected repository identity: %#v", status)
	}
	changes := changesByPath(status.Changes)
	if change := changes["profile.toml"]; !change.Managed || change.Worktree != "M" {
		t.Fatalf("managed unstaged change = %#v", change)
	}
	if change := changes["README.md"]; change.Managed || change.Worktree != "M" {
		t.Fatalf("unmanaged unstaged change = %#v", change)
	}
	if change := changes["packages/new file.txt"]; !change.Managed || change.Index == "" {
		t.Fatalf("managed staged spaced path = %#v", change)
	}
	if change := changes["themes/untracked theme.txt"]; !change.Managed || change.Worktree != "?" {
		t.Fatalf("managed untracked path = %#v", change)
	}
	if change := changes["notes/renamed file.conf"]; change.Managed || change.OriginalPath != "config/old name.conf" || !change.OriginalManaged || change.Index == "" {
		t.Fatalf("managed-to-unmanaged rename = %#v", change)
	}
}

func TestStatusAheadBehindAndSanitizedOrigin(t *testing.T) {
	parent := t.TempDir()
	origin := filepath.Join(parent, "origin.git")
	git(t, parent, "init", "--bare", origin)
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	git(t, parent, "clone", origin, first)
	git(t, first, "checkout", "-b", "main")
	git(t, first, "config", "user.name", "Blueprint Test")
	git(t, first, "config", "user.email", "blueprint@example.test")
	mustWrite(t, filepath.Join(first, "profile.toml"), "initial\n")
	git(t, first, "add", ".")
	git(t, first, "commit", "-m", "initial")
	git(t, first, "push", "-u", "origin", "main")
	git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, parent, "clone", origin, second)
	git(t, second, "config", "user.name", "Blueprint Test")
	git(t, second, "config", "user.email", "blueprint@example.test")
	mustWrite(t, filepath.Join(first, "packages", "ahead"), "ahead\n")
	git(t, first, "add", ".")
	git(t, first, "commit", "-m", "ahead")
	mustWrite(t, filepath.Join(second, "README.md"), "behind\n")
	git(t, second, "add", ".")
	git(t, second, "commit", "-m", "behind")
	git(t, second, "push")
	git(t, first, "fetch", "origin")
	git(t, first, "remote", "set-url", "origin", "https://user:secret@example.test/profile.git")

	service, err := New(command.SystemRunner{}, first)
	if err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Upstream != "origin/main" || status.Ahead != 1 || status.Behind != 1 {
		t.Fatalf("upstream status = %#v", status)
	}
	if status.Origin != "https://example.test/profile.git" {
		t.Fatalf("sanitized origin = %q", status.Origin)
	}
}

func TestSanitizeRemote(t *testing.T) {
	if got := SanitizeRemote("git@github.com:owner/repo.git"); got != "git@github.com:owner/repo.git" {
		t.Fatalf("scp remote = %q", got)
	}
}

func changesByPath(changes []Change) map[string]Change {
	result := make(map[string]Change, len(changes))
	for _, change := range changes {
		result[change.Path] = change
	}
	return result
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := (command.SystemRunner{}).Run(context.Background(), "git", append([]string{"-C", dir}, args...)...); err != nil {
		t.Fatal(err)
	}
	if len(args) == 0 || args[0] != "init" {
		return
	}
	for _, arg := range args {
		if arg == "--bare" {
			return
		}
	}
	git(t, dir, "config", "user.name", "Blueprint Test")
	git(t, dir, "config", "user.email", "blueprint@example.test")
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, name, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(name))
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
