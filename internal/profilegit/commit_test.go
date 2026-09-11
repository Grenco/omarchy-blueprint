package profilegit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

func TestCommitManagedPathsOnly(t *testing.T) {
	root := commitRepository(t)
	mustWrite(t, filepath.Join(root, "config", "settings.conf"), "changed\n")
	mustWrite(t, filepath.Join(root, "resources", "resources.toml"), "changed\n")
	mustWrite(t, filepath.Join(root, "README.md"), "unmanaged change\n")
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Commit(context.Background(), "managed update")
	if err != nil || !result.Changed {
		t.Fatalf("commit=%#v err=%v", result, err)
	}
	if got := gitOutput(t, root, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD"); got != "config/settings.conf\nresources/resources.toml" {
		t.Fatalf("committed paths = %q", got)
	}
	if got := gitOutput(t, root, "status", "--porcelain", "README.md"); got != " M README.md" {
		t.Fatalf("unmanaged status = %q", got)
	}
}

func TestCommitRefusesPreexistingStaging(t *testing.T) {
	for _, path := range []string{"config/settings.conf", "README.md"} {
		t.Run(path, func(t *testing.T) {
			root := commitRepository(t)
			mustWrite(t, filepath.Join(root, path), "staged\n")
			git(t, root, "add", "--", path)
			service, err := New(command.SystemRunner{}, root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Commit(context.Background(), "ignored"); !errors.Is(err, ErrStagedChanges) {
				t.Fatalf("commit error = %v", err)
			}
			if got := gitOutput(t, root, "diff", "--cached", "--name-only"); got != path {
				t.Fatalf("staged paths = %q", got)
			}
			if got, err := os.ReadFile(filepath.Join(root, path)); err != nil || string(got) != "staged\n" {
				t.Fatalf("worktree bytes = %q err=%v", got, err)
			}
		})
	}
}

func TestCommitNoManagedChangesAndDefaultMessage(t *testing.T) {
	root := commitRepository(t)
	mustWrite(t, filepath.Join(root, "README.md"), "unmanaged change\n")
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Commit(context.Background(), "")
	if err != nil || result.Changed {
		t.Fatalf("no-op commit=%#v err=%v", result, err)
	}
	mustWrite(t, filepath.Join(root, "resources", "resources.toml"), "changed\n")
	mustWrite(t, filepath.Join(root, "config", "settings.conf"), "changed\n")
	result, err = service.Commit(context.Background(), "")
	if err != nil || !result.Changed {
		t.Fatalf("default commit=%#v err=%v", result, err)
	}
	if got := gitOutput(t, root, "log", "-1", "--format=%B"); got != "Update Blueprint profile: config, resources" {
		t.Fatalf("commit message = %q", got)
	}
}

func TestCommitFailureUnstagesManagedPaths(t *testing.T) {
	root := commitRepository(t)
	path := filepath.Join(root, "config", "settings.conf")
	mustWrite(t, path, "changed\n")
	hook := filepath.Join(root, ".git", "hooks", "pre-commit")
	mustWrite(t, hook, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Commit(context.Background(), "will fail"); err == nil {
		t.Fatal("Commit succeeded despite failing hook")
	}
	if got := gitOutput(t, root, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("staged paths after failure = %q", got)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "changed\n" {
		t.Fatalf("worktree bytes = %q err=%v", got, err)
	}
}

func TestCommitNonRepository(t *testing.T) {
	service, err := New(command.SystemRunner{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Commit(context.Background(), "message"); err == nil || err.Error() != "profile is not a Git repository" {
		t.Fatalf("commit error = %v", err)
	}
}

func commitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.name", "Blueprint Test")
	git(t, root, "config", "user.email", "blueprint@example.test")
	mustWrite(t, filepath.Join(root, "profile.toml"), "initial\n")
	mustWrite(t, filepath.Join(root, "README.md"), "initial\n")
	git(t, root, "add", "--", "profile.toml", "README.md")
	git(t, root, "commit", "-m", "initial")
	return root
}
