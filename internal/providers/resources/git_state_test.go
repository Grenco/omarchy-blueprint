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
	writeGitFile(t, root, "cafe.txt", strings.Repeat("unchanged\n", 12))
	runGit(t, root, "add", "cafe.txt")
	runGit(t, root, "commit", "-m", "add cafe")
	if err := os.Rename(filepath.Join(root, "cafe.txt"), filepath.Join(root, "caf\u00e9.txt")); err != nil {
		t.Fatal(err)
	}
	writeGitFile(t, root, "caf\u00e9.txt", "changed first\n"+strings.Repeat("unchanged\n", 10)+"changed last\n")
	runGit(t, root, "add", "-A")
	baseline, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"diff.mnemonicPrefix", "true"}, {"diff.noprefix", "true"}, {"diff.context", "0"}, {"diff.interHunkContext", "100"}, {"diff.compactionHeuristic", "true"}, {"core.quotePath", "false"}} {
		runGit(t, root, "config", pair[0], pair[1])
	}
	got, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.IndexPatchHash != baseline.IndexPatchHash || string(got.IndexPatch) != string(baseline.IndexPatch) {
		t.Fatalf("patch was affected by local diff config:\n%s", got.IndexPatch)
	}
	if !strings.Contains(string(got.IndexPatch), "@@") || !strings.Contains(string(got.IndexPatch), "caf\\303\\251.txt") {
		t.Fatalf("patch did not use pinned context or quoting:\n%s", got.IndexPatch)
	}
}

func TestCaptureGitWorkingStateScansOnlyChangedDesiredContent(t *testing.T) {
	root := initGitRepository(t)
	writeGitFile(t, root, "unchanged-secret.txt", "api_token=abcdefghijklmnopqrstuvwxyz\n")
	runGit(t, root, "add", "unchanged-secret.txt")
	runGit(t, root, "commit", "-m", "unchanged secret")
	if _, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil); err != nil {
		t.Fatalf("unchanged committed content was scanned: %v", err)
	}

	writeGitFile(t, root, "unchanged-secret.txt", "removed\n")
	if _, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil); err != nil {
		t.Fatalf("removing a committed token was rejected: %v", err)
	}
}

func TestCaptureGitWorkingStateIgnoresRenameSourceAndGitlinks(t *testing.T) {
	root := initGitRepository(t)
	original := "api_token=abcdefghijklmnopqrstuvwxyz\n" + strings.Repeat("unchanged\n", 20)
	writeGitFile(t, root, "under", original)
	runGit(t, root, "add", "under")
	runGit(t, root, "commit", "-m", "add old path")
	runGit(t, root, "mv", "under", "renamed.txt")
	writeGitFile(t, root, "renamed.txt", "removed\n"+strings.Repeat("unchanged\n", 20))
	runGit(t, root, "add", "renamed.txt")
	if status := gitOutput(t, root, "status", "--porcelain=v2"); !strings.HasPrefix(status, "2 R.") {
		t.Fatalf("expected porcelain rename, got %q", status)
	}
	if _, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil); err != nil {
		t.Fatalf("rename source was treated as a desired path: %v", err)
	}

	revision := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
	runGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+revision+",submodule")
	if _, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil); err != nil {
		t.Fatalf("staged gitlink was treated as a blob or regular file: %v", err)
	}
	runGit(t, root, "commit", "-m", "add submodule")
	if _, err := CaptureGitWorkingState(context.Background(), command.SystemRunner{}, root, nil); err != nil {
		t.Fatalf("clean gitlink was rejected: %v", err)
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

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(output)
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
		"git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -c diff.compactionHeuristic=false -c diff.context=3 -c diff.interHunkContext=0 -c core.quotePath=true -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --cached HEAD --": "index patch",
		"git -c diff.external= -c diff.mnemonicPrefix=false -c diff.noprefix=false -c diff.srcPrefix=a/ -c diff.dstPrefix=b/ -c diff.algorithm=myers -c diff.indentHeuristic=false -c diff.compactionHeuristic=false -c diff.context=3 -c diff.interHunkContext=0 -c core.quotePath=true -C " + root + " diff --binary --full-index --no-color --no-ext-diff --no-textconv --no-renames --":               "worktree patch",
	}}
	got, err := CaptureGitWorkingState(context.Background(), runner, root, []string{"notes.md"})
	if err != nil || got.IndexPatchHash == "" || got.WorktreePatchHash == "" || len(got.Untracked) != 1 {
		t.Fatalf("capture=%#v err=%v", got, err)
	}
	if _, err := CaptureGitWorkingState(context.Background(), runner, root, []string{"../notes.md"}); err == nil {
		t.Fatal("traversal accepted")
	}
}
