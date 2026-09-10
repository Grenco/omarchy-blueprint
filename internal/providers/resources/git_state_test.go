package resources

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

func TestInspectGitWorkingStateParsesPorcelainV2(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " remote get-url origin":                                                   "https://github.com/example/repo.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          revision + "\n",
		"git -C " + root + " symbolic-ref --quiet --short HEAD":                                       "main\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "1 M. N... 100644 100644 100644 a b staged.txt\x002 .M N... 100644 100644 100644 a b R100 renamed.txt\x00original.txt\x00? z file\x00? a file\x00u UU N... 100644 100644 100644 100644 a b c conflict.txt\x00",
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

func TestCaptureGitWorkingStateRejectsSensitiveTrackedContent(t *testing.T) {
	for _, tc := range []struct {
		name, path, content string
		stage               bool
	}{
		{"index blob", "notes.md", "api_token=abcdefghijklmnopqrstuvwxyz", true},
		{"worktree file", "tracked.txt", "api_token=abcdefghijklmnopqrstuvwxyz", false},
		{"path policy", ".env", "safe", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := initGitRepository(t)
			writeGitFile(t, root, tc.path, tc.content)
			if tc.stage {
				runGit(t, root, "add", tc.path)
			}
			_, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil)
			if err == nil || !strings.Contains(err.Error(), "sensitive tracked") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestInspectGitWorkingStateDetectsOperationAtRelativeGitPath(t *testing.T) {
	root := initGitRepository(t)
	if err := os.WriteFile(filepath.Join(root, ".git", "MERGE_HEAD"), []byte(strings.Repeat("a", 40)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, ok, err := InspectGitWorkingState(context.Background(), command.SystemRunner{}, root)
	if err != nil || !ok || state.Operation != "merge" {
		t.Fatalf("state=%#v ok=%t err=%v", state, ok, err)
	}
}

func TestInspectGitWorkingStateParsesRealPorcelainRename(t *testing.T) {
	root := initGitRepository(t)
	runGit(t, root, "mv", "tracked.txt", "renamed.txt")
	state, ok, err := InspectGitWorkingState(context.Background(), command.SystemRunner{}, root)
	if err != nil || !ok || state.StagedTracked != 1 || state.UnstagedTracked != 0 {
		t.Fatalf("state=%#v ok=%t err=%v", state, ok, err)
	}
}

func TestCaptureGitWorkingStateNeutralizesDiffConfig(t *testing.T) {
	root := initGitRepository(t)
	writeGitFile(t, root, "tracked.txt", "changed\n")
	runGit(t, root, "config", "diff.mnemonicPrefix", "true")
	runGit(t, root, "config", "diff.noprefix", "true")
	got, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got.WorktreePatch), "--- a/tracked.txt") || !strings.Contains(string(got.WorktreePatch), "+++ b/tracked.txt") {
		t.Fatalf("patch was affected by local diff config:\n%s", got.WorktreePatch)
	}
}

func initGitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test User")
	writeGitFile(t, root, "tracked.txt", "original\n")
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "commit", "-m", "initial")
	runGit(t, root, "remote", "add", "origin", "https://github.com/example/repo.git")
	return root
}

func writeGitFile(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

func TestCaptureGitWorkingStateCapturesPatchesAndSelectedUntracked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	runner := gitRunner{output: map[string]string{
		"git -C " + root + " rev-parse --show-toplevel":                                               root + "\n",
		"git -C " + root + " remote get-url origin":                                                   "https://github.com/example/repo.git\n",
		"git -C " + root + " rev-parse HEAD":                                                          revision + "\n",
		"git -C " + root + " status --porcelain=v2 -z --untracked-files=all --ignore-submodules=none": "? notes.md\x00",
		"git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --cached HEAD --": "index patch",
		"git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --":               "worktree patch",
	}}
	got, err := CaptureGitWorkingState(context.Background(), runner, root, []string{"notes.md"})
	if err != nil || got.IndexPatchHash == "" || got.WorktreePatchHash == "" || len(got.Untracked) != 1 {
		t.Fatalf("capture=%#v err=%v", got, err)
	}
	if _, err := CaptureGitWorkingState(context.Background(), runner, root, []string{"../notes.md"}); err == nil {
		t.Fatal("traversal accepted")
	}
}
