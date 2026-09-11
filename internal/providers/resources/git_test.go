package resources

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type gitRunner struct {
	output map[string]string
	err    map[string]error
}

func (r gitRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	return r.output[key], r.err[key]
}

func TestDetectCleanGitResource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dotfiles")
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "",
		"git -C " + root + " remote get-url origin":                                                   "git@github.com:example/dotfiles.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          revision + "\n",
		"git -C " + root + " symbolic-ref --quiet --short HEAD":                                       "main\n",
	}}
	state, isGit, err := DetectGitResource(context.Background(), runner, root)
	if err != nil || !isGit || state != (GitState{Remote: "github.com/example/dotfiles", Branch: "main", Revision: revision}) {
		t.Fatalf("state=%#v isGit=%t err=%v", state, isGit, err)
	}
	if !EqualGitResource(profile.Resource{Strategy: "git", Remote: state.Remote, Revision: state.Revision}, state) {
		t.Fatal("matching Git resource was not equal")
	}
}

func TestDetectGitResourceMarksDirtyAndRejectsNestedWorktree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dotfiles")
	base := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "1 .M N... 100644 100644 100644 a a config\x00",
		"git -C " + root + " remote get-url origin":                                                   "https://github.com/example/dotfiles.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          strings.Repeat("b", 40),
	}}
	state, isGit, err := DetectGitResource(context.Background(), base, root)
	if err != nil || !isGit || !state.Dirty {
		t.Fatalf("state=%#v isGit=%t err=%v", state, isGit, err)
	}
	nested := gitRunner{output: map[string]string{"git -C " + root + " rev-parse --show-toplevel": filepath.Dir(root) + "\n"}}
	if _, isGit, err := DetectGitResource(context.Background(), nested, root); err != nil || isGit {
		t.Fatalf("nested isGit=%t err=%v", isGit, err)
	}
	notGit := gitRunner{err: map[string]error{"git -C " + root + " rev-parse --show-toplevel": errors.New("not a repository")}}
	if _, isGit, err := DetectGitResource(context.Background(), notGit, root); err != nil || isGit {
		t.Fatalf("notGit isGit=%t err=%v", isGit, err)
	}
}

func TestPortableGitRemote(t *testing.T) {
	for _, raw := range []string{"https://github.com/Grenco/dotfiles.git", "ssh://git@github.com/Grenco/dotfiles.git", "git@github.com:Grenco/dotfiles.git"} {
		if got, err := PortableGitRemote(raw); err != nil || got != "github.com/Grenco/dotfiles" {
			t.Fatalf("remote=%q got=%q err=%v", raw, got, err)
		}
	}
	if got, err := PortableGitRemote("https://token@example.com/example/dotfiles.git"); err != nil || got != "https://example.com/example/dotfiles.git" {
		t.Fatalf("sanitized=%q err=%v", got, err)
	}
	for _, raw := range []string{"/path/to/local/repo", "../relative/repo", "file:///home/user/repo"} {
		if _, err := PortableGitRemote(raw); err == nil {
			t.Fatalf("accepted local remote %q", raw)
		}
	}
}
