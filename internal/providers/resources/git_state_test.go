package resources

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectGitWorkingStateParsesPorcelainV2(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " remote get-url origin":                                                   "https://github.com/example/repo.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          revision + "\n",
		"git -C " + root + " symbolic-ref --quiet --short HEAD":                                       "main\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "1 M. N... 100644 100644 100644 a b staged.txt\x001 .M N... 100644 100644 100644 a b unstaged.txt\x00? z file\x00? a file\x00u UU N... 100644 100644 100644 100644 a b c conflict.txt\x00",
	}}
	got, ok, err := InspectGitWorkingState(context.Background(), runner, root)
	if err != nil || !ok {
		t.Fatalf("state=%#v ok=%t err=%v", got, ok, err)
	}
	if got.StagedTracked != 1 || got.UnstagedTracked != 1 {
		t.Fatalf("state=%#v", got)
	}
	if !got.Conflicted || !strings.Contains(strings.Join(got.Untracked, ","), "a file,z file") {
		t.Fatalf("state=%#v", got)
	}
}

func TestCaptureGitWorkingStateCapturesPatchesAndSelectedUntracked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                                                       root + "\n",
		"git -C " + root + " remote get-url origin":                                                                           "https://github.com/example/repo.git\n",
		"git -C " + root + " rev-parse HEAD":                                                                                  revision + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none":                         "? notes.md\x00",
		"git -C " + root + " diff --cached HEAD --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --": "index patch",
		"git -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --":               "worktree patch",
	}}
	got, err := CaptureGitWorkingState(context.Background(), runner, root, []string{"notes.md"})
	if err != nil || got.IndexPatchHash == "" || got.WorktreePatchHash == "" || len(got.Untracked) != 1 {
		t.Fatalf("capture=%#v err=%v", got, err)
	}
	if _, err := CaptureGitWorkingState(context.Background(), runner, root, []string{"../notes.md"}); err == nil {
		t.Fatal("traversal accepted")
	}
}
