package profilegit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestInitCreatesRootRepositoryAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root)
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Init(context.Background())
	if err != nil || !result.Changed || !result.Status.Repository {
		t.Fatalf("init=%#v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatal(err)
	}
	result, err = service.Init(context.Background())
	if err != nil || result.Changed {
		t.Fatalf("second init=%#v err=%v", result, err)
	}
}

func TestInitRefusesParentRepositoryAndInvalidGitMarker(t *testing.T) {
	t.Run("parent repository", func(t *testing.T) {
		parent := t.TempDir()
		root := filepath.Join(parent, "profile")
		writeProfile(t, root)
		git(t, parent, "init", "-b", "main")
		if _, err := profileGitService(t, root).Init(context.Background()); err == nil || !strings.Contains(err.Error(), "parent Git repository") {
			t.Fatalf("init error=%v", err)
		}
		if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
			t.Fatalf("nested repository created: %v", err)
		}
	})
	t.Run("invalid marker", func(t *testing.T) {
		root := t.TempDir()
		writeProfile(t, root)
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("invalid"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := profileGitService(t, root).Init(context.Background()); err == nil || !strings.Contains(err.Error(), "not a valid repository") {
			t.Fatalf("init error=%v", err)
		}
	})
}

func TestRemoteLifecyclePreservesOtherRemotes(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	if remote, err := service.Remote(context.Background()); err != nil || remote != "" {
		t.Fatalf("remote=%q err=%v", remote, err)
	}
	if _, err := service.SetRemote(context.Background(), "https://user:secret@example.test/profile.git"); err != nil {
		t.Fatal(err)
	}
	if remote, err := service.Remote(context.Background()); err != nil || remote != "https://example.test/profile.git" {
		t.Fatalf("sanitized remote err=%v", err)
	}
	git(t, root, "remote", "add", "backup", "https://example.test/backup.git")
	if _, err := service.SetRemote(context.Background(), "git@example.test:second.git"); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, root, "remote", "get-url", "backup"); got != "https://example.test/backup.git" {
		t.Fatalf("backup=%q", got)
	}
	removed, err := service.RemoveRemote(context.Background())
	if err != nil || !removed.Changed || removed.Status.Origin != "" {
		t.Fatalf("remove=%#v err=%v", removed, err)
	}
	removed, err = service.RemoveRemote(context.Background())
	if err != nil || removed.Changed {
		t.Fatalf("second remove=%#v err=%v", removed, err)
	}
}

func TestBrowserURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/acme/profile.git": "https://github.com/acme/profile",
		"git@github.com:acme/profile.git":     "https://github.com/acme/profile",
		"ssh://git@gitlab.com/acme/profile":   "https://gitlab.com/acme/profile",
		"file:///tmp/repo":                    "",
		"custom::repo":                        "",
		"https://user:secret@host/repo.git":   "https://host/repo",
	} {
		got, ok := BrowserURL(raw)
		if (want != "") != ok || got != want {
			t.Fatalf("unexpected BrowserURL result")
		}
	}
}

func TestFetchUpdatesCachedRemoteStatusOnlyWhenExplicit(t *testing.T) {
	origin, first, second := gitRemoteClones(t)
	service := profileGitService(t, second)

	mustWrite(t, filepath.Join(first, "profile.toml"), "second\n")
	git(t, first, "add", "profile.toml")
	git(t, first, "commit", "-m", "second")
	git(t, first, "push")

	before := gitOutput(t, second, "rev-parse", "refs/remotes/origin/main")
	status, err := service.Status(context.Background())
	if err != nil || status.Behind != 0 {
		t.Fatalf("status before fetch=%#v err=%v", status, err)
	}
	if after := gitOutput(t, second, "rev-parse", "refs/remotes/origin/main"); after != before {
		t.Fatalf("status updated remote ref: before=%s after=%s", before, after)
	}

	result, err := service.Fetch(context.Background())
	if err != nil || !result.Changed || result.Status.Behind != 1 {
		t.Fatalf("fetch=%#v err=%v", result, err)
	}
	if after := gitOutput(t, second, "rev-parse", "refs/remotes/origin/main"); after == before {
		t.Fatal("fetch did not update remote ref")
	}
	if got := gitOutput(t, origin, "rev-parse", "refs/heads/main"); got != result.Status.Head && got == before {
		t.Fatal("origin fixture is invalid")
	}
}

func TestPullFastForwardsCleanRepository(t *testing.T) {
	_, first, second := gitRemoteClones(t)
	service := profileGitService(t, second)
	mustWrite(t, filepath.Join(first, "profile.toml"), "second\n")
	git(t, first, "add", "profile.toml")
	git(t, first, "commit", "-m", "second")
	git(t, first, "push")

	result, err := service.Pull(context.Background())
	if err != nil || !result.Changed || result.Status.Behind != 0 || result.Status.Head != gitOutput(t, first, "rev-parse", "HEAD") {
		t.Fatalf("pull=%#v err=%v", result, err)
	}
}

func TestPullRejectsDirtyRepositoryAndDivergence(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, root string)
	}{
		{"managed unstaged", func(t *testing.T, root string) { mustWrite(t, filepath.Join(root, "profile.toml"), "dirty\n") }},
		{"unmanaged unstaged", func(t *testing.T, root string) { mustWrite(t, filepath.Join(root, "README.md"), "dirty\n") }},
		{"staged", func(t *testing.T, root string) {
			mustWrite(t, filepath.Join(root, "profile.toml"), "dirty\n")
			git(t, root, "add", "profile.toml")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, second := gitRemoteClones(t)
			service := profileGitService(t, second)
			test.prepare(t, second)
			before := mustRead(t, filepath.Join(second, "profile.toml"))
			if _, err := service.Pull(context.Background()); err == nil {
				t.Fatal("pull accepted dirty repository")
			}
			if after := mustRead(t, filepath.Join(second, "profile.toml")); after != before {
				t.Fatal("pull changed dirty worktree bytes")
			}
		})
	}

	t.Run("diverged", func(t *testing.T) {
		_, first, second := gitRemoteClones(t)
		service := profileGitService(t, second)
		mustWrite(t, filepath.Join(first, "profile.toml"), "remote\n")
		git(t, first, "add", "profile.toml")
		git(t, first, "commit", "-m", "remote")
		git(t, first, "push")
		mustWrite(t, filepath.Join(second, "README.md"), "local\n")
		git(t, second, "config", "user.name", "Blueprint Test")
		git(t, second, "config", "user.email", "blueprint@example.test")
		git(t, second, "add", "README.md")
		git(t, second, "commit", "-m", "local")
		before := gitOutput(t, second, "rev-parse", "HEAD")
		if _, err := service.Pull(context.Background()); err == nil {
			t.Fatal("pull merged divergent history")
		}
		if after := gitOutput(t, second, "rev-parse", "HEAD"); after != before {
			t.Fatal("pull changed divergent HEAD")
		}
	})
}

func TestPullRejectsMissingUpstream(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	gitCommit(t, root, "profile.toml", "initial\n")
	service := profileGitService(t, root)
	if _, err := service.Pull(context.Background()); err == nil {
		t.Fatal("pull accepted missing upstream")
	}
}

func TestPushEstablishesAndUsesUpstream(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	git(t, filepath.Dir(origin), "init", "--bare", origin)
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	git(t, root, "remote", "add", "origin", origin)
	gitCommit(t, root, "profile.toml", "initial\n")
	service := profileGitService(t, root)

	result, err := service.Push(context.Background())
	if err != nil || !result.Changed || result.Status.Upstream != "origin/main" {
		t.Fatalf("initial push=%#v err=%v", result, err)
	}
	gitCommit(t, root, "profile.toml", "second\n")
	result, err = service.Push(context.Background())
	if err != nil || result.Status.Ahead != 0 || gitOutput(t, origin, "rev-parse", "refs/heads/main") != result.Status.Head {
		t.Fatalf("upstream push=%#v err=%v", result, err)
	}
}

func TestPushRejectsDetachedHeadAndMissingOriginButAllowsDirtyWorktree(t *testing.T) {
	t.Run("detached", func(t *testing.T) {
		_, _, root := gitRemoteClones(t)
		git(t, root, "checkout", "--detach")
		if _, err := profileGitService(t, root).Push(context.Background()); err == nil {
			t.Fatal("push accepted detached HEAD")
		}
	})
	t.Run("missing origin", func(t *testing.T) {
		root := t.TempDir()
		git(t, root, "init", "-b", "main")
		gitCommit(t, root, "profile.toml", "initial\n")
		if _, err := profileGitService(t, root).Push(context.Background()); err == nil {
			t.Fatal("push accepted missing origin")
		}
	})
	t.Run("dirty worktree", func(t *testing.T) {
		origin, _, root := gitRemoteClones(t)
		mustWrite(t, filepath.Join(root, "profile.toml"), "dirty\n")
		result, err := profileGitService(t, root).Push(context.Background())
		if err != nil || len(result.Status.Changes) == 0 || gitOutput(t, origin, "rev-parse", "refs/heads/main") != result.Status.Head {
			t.Fatalf("dirty push=%#v err=%v", result, err)
		}
	})
}

func TestNetworkFailuresLeaveProfileBytesUntouched(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root)
	service := profileGitService(t, root)
	if _, err := service.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	git(t, root, "config", "user.name", "Blueprint Test")
	git(t, root, "config", "user.email", "blueprint@example.test")
	if _, err := service.Commit(context.Background(), "initial"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetRemote(context.Background(), filepath.Join(t.TempDir(), "missing-origin.git")); err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(root, "profile.toml"))
	if _, err := service.Fetch(context.Background()); err == nil {
		t.Fatal("fetch succeeded against missing remote")
	}
	if after := mustRead(t, filepath.Join(root, "profile.toml")); after != before {
		t.Fatal("fetch changed profile bytes")
	}
	if _, err := service.Push(context.Background()); err == nil {
		t.Fatal("push succeeded against missing remote")
	}
	if after := mustRead(t, filepath.Join(root, "profile.toml")); after != before {
		t.Fatal("push changed profile bytes")
	}
}

func gitRemoteClones(t *testing.T) (origin, first, second string) {
	t.Helper()
	parent := t.TempDir()
	origin = filepath.Join(parent, "origin.git")
	first = filepath.Join(parent, "first")
	second = filepath.Join(parent, "second")
	git(t, parent, "init", "--bare", origin)
	git(t, parent, "clone", origin, first)
	git(t, first, "checkout", "-b", "main")
	gitCommit(t, first, "profile.toml", "initial\n")
	git(t, first, "push", "-u", "origin", "main")
	git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, parent, "clone", origin, second)
	return origin, first, second
}

func profileGitService(t *testing.T, root string) Service {
	t.Helper()
	service, err := New(command.SystemRunner{}, root)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func writeProfile(t *testing.T, root string) {
	t.Helper()
	if err := profile.Save(root, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
}

func gitCommit(t *testing.T, root, path, content string) {
	t.Helper()
	git(t, root, "config", "user.name", "Blueprint Test")
	git(t, root, "config", "user.email", "blueprint@example.test")
	mustWrite(t, filepath.Join(root, path), content)
	git(t, root, "add", path)
	git(t, root, "commit", "-m", "commit")
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := (command.SystemRunner{}).Run(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return stringTrim(out)
}

func stringTrim(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}
