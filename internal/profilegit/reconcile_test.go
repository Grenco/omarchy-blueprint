package profilegit

import (
	"context"
	"errors"
	"github.com/Grenco/omarchy-blueprint/internal/command"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests use real disposable repositories. The remote may move without
// changing the reviewed cached ref: execution must never fetch a newer intent.
func TestFastForwardToPinsReviewedCommitWithoutFetching(t *testing.T) {
	_, remote, local := gitRemoteClones(t)
	l := gitOutput(t, local, "rev-parse", "HEAD")
	gitCommit(t, remote, "profile.toml", "reviewed\n")
	git(t, remote, "push")
	git(t, local, "fetch")
	r := gitOutput(t, local, "rev-parse", "@{upstream}")
	gitCommit(t, remote, "profile.toml", "not reviewed\n")
	git(t, remote, "push")
	result, err := profileGitService(t, local).FastForwardTo(context.Background(), l, r)
	if err != nil || !result.Changed {
		t.Fatalf("fast-forward: %#v, %v", result, err)
	}
	if got := gitOutput(t, local, "rev-parse", "HEAD"); got != r {
		t.Fatalf("followed unreviewed revision: %s, want %s", got, r)
	}
	if got := gitOutput(t, local, "rev-parse", "@{upstream}"); got != r {
		t.Fatal("pinned execution fetched the moving remote")
	}
}

func TestSemanticMergeRejectsIgnoredManagedFilesWithoutLosingBytes(t *testing.T) {
	s, req := semanticMergeFixture(t)
	mustWrite(t, filepath.Join(s.Root, ".git/info/exclude"), "hooks/private.sh\n")
	mustWrite(t, filepath.Join(s.Root, "hooks/private.sh"), "private bytes\n")
	if _, err := s.CommitSemanticMerge(context.Background(), req); err == nil {
		t.Fatal("accepted hidden managed content")
	}
	if got := mustRead(t, filepath.Join(s.Root, "hooks/private.sh")); got != "private bytes\n" {
		t.Fatal("ignored content was lost")
	}
}

func TestSemanticMergeCommitsEvenWhenTreeEqualsLocal(t *testing.T) {
	s, req := semanticMergeFixture(t)
	if err := os.Remove(filepath.Join(req.StagingRoot, "hooks/remote.sh")); err != nil {
		t.Fatal(err)
	}
	_, err := s.CommitSemanticMerge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if gitOutput(t, s.Root, "rev-parse", "HEAD^{tree}") != gitOutput(t, s.Root, "rev-parse", req.LocalSHA+"^{tree}") {
		t.Fatal("unchanged semantic result changed tree")
	}
	if gitOutput(t, s.Root, "show", "-s", "--format=%P", "HEAD") != req.LocalSHA+" "+req.RemoteSHA {
		t.Fatal("same-result history lost a parent")
	}
}

type addUnmanagedCommitRunner struct{ command.SystemRunner }

func (r addUnmanagedCommitRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	for _, arg := range args {
		if arg == "commit" {
			for i, v := range args {
				if v == "-C" && i+1 < len(args) {
					root := args[i+1]
					if err := os.WriteFile(filepath.Join(root, "unapproved.txt"), []byte("not approved"), 0600); err != nil {
						return "", err
					}
					if _, err := r.SystemRunner.Run(ctx, "git", "-C", root, "add", "unapproved.txt"); err != nil {
						return "", err
					}
				}
			}
		}
	}
	return r.SystemRunner.Run(ctx, name, args...)
}

func TestSemanticMergeRejectsUnapprovedCommittedTree(t *testing.T) {
	s, req := semanticMergeFixture(t)
	s.Runner = addUnmanagedCommitRunner{}
	if _, err := s.CommitSemanticMerge(context.Background(), req); !errors.Is(err, ErrReconciliationPostCommitVerificationFailed) {
		t.Fatalf("unapproved committed tree lacked repair-required classification: %v", err)
	}
	if gitOutput(t, s.Root, "rev-parse", "HEAD") == req.LocalSHA || gitOutput(t, s.Root, "show", "HEAD:unapproved.txt") != "not approved" {
		t.Fatal("unapproved commit was not retained for manual repair")
	}
}

func TestSemanticMergeCompleteTreeRemovesDeletedManagedArtifacts(t *testing.T) {
	s, req := semanticMergeFixture(t)
	if err := os.Remove(filepath.Join(req.StagingRoot, "hooks/local.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitSemanticMerge(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(s.Root, "hooks/local.sh")); !os.IsNotExist(err) {
		t.Fatal("stale managed artifact survived complete replacement")
	}
	if gitOutput(t, s.Root, "status", "--porcelain") != "" {
		t.Fatal("deleted artifact was not staged")
	}
}

func TestSemanticMergeRejectsInvalidStagingBeforeMutation(t *testing.T) {
	for _, kind := range []string{"invalid-profile", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			s, req := semanticMergeFixture(t)
			if kind == "invalid-profile" {
				mustWrite(t, filepath.Join(req.StagingRoot, "profile.toml"), "not valid TOML [\n")
			} else if err := os.Symlink("/etc/passwd", filepath.Join(req.StagingRoot, "hooks/linked")); err != nil {
				t.Fatal(err)
			}
			if _, err := s.CommitSemanticMerge(context.Background(), req); err == nil {
				t.Fatal("accepted unsafe staging tree")
			}
			if gitOutput(t, s.Root, "rev-parse", "HEAD") != req.LocalSHA || gitOutput(t, s.Root, "status", "--porcelain") != "" {
				t.Fatal("invalid staging changed repository")
			}
		})
	}
}

func TestSemanticMergeRejectsUnsupportedRollbackTreeBeforeMutation(t *testing.T) {
	s, req := semanticMergeFixture(t)
	if err := os.Symlink("local.sh", filepath.Join(s.Root, "hooks/linked")); err != nil {
		t.Fatal(err)
	}
	git(t, s.Root, "add", "hooks/linked")
	git(t, s.Root, "commit", "-m", "link")
	req.LocalSHA = gitOutput(t, s.Root, "rev-parse", "HEAD")
	if _, err := s.CommitSemanticMerge(context.Background(), req); err == nil {
		t.Fatal("accepted rollback tree that cannot be restored")
	}
	if gitOutput(t, s.Root, "rev-parse", "HEAD") != req.LocalSHA || gitOutput(t, s.Root, "status", "--porcelain") != "" {
		t.Fatal("unsupported rollback tree was mutated")
	}
}

func semanticMergeFixture(t *testing.T) (Service, SemanticMergeRequest) {
	t.Helper()
	_, remote, local := gitRemoteClones(t)
	writeProfile(t, remote)
	git(t, remote, "add", "-A")
	git(t, remote, "commit", "-m", "valid profile")
	git(t, remote, "push")
	git(t, local, "pull", "--ff-only")
	b := gitOutput(t, local, "rev-parse", "HEAD")
	gitCommit(t, remote, "hooks/remote.sh", "remote\n")
	git(t, remote, "push")
	gitCommit(t, local, "hooks/local.sh", "local\n")
	git(t, local, "fetch")
	l := gitOutput(t, local, "rev-parse", "HEAD")
	r := gitOutput(t, local, "rev-parse", "@{upstream}")
	stage := t.TempDir()
	s := profileGitService(t, local)
	if err := s.MaterializeCommit(context.Background(), l, stage); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(stage, "hooks/remote.sh"), "chosen remote\n")
	return s, SemanticMergeRequest{BaseSHA: b, LocalSHA: l, RemoteSHA: r, StagingRoot: stage, Message: "semantic reconciliation"}
}

func TestSemanticMergeUsesCompleteChosenTreeAndExactParents(t *testing.T) {
	s, req := semanticMergeFixture(t)
	result, err := s.CommitSemanticMerge(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || gitOutput(t, s.Root, "show", "-s", "--format=%P", "HEAD") != req.LocalSHA+" "+req.RemoteSHA {
		t.Fatal("wrong reconciliation ancestry")
	}
	if mustRead(t, filepath.Join(s.Root, "hooks/remote.sh")) != "chosen remote\n" {
		t.Fatal("Git chose remote bytes rather than staged intent")
	}
	if len(result.Status.Changes) != 0 {
		t.Fatal("transaction left dirty checkout")
	}
}

func TestSemanticMergeRejectsRevertedUnmanagedHistory(t *testing.T) {
	s, req := semanticMergeFixture(t)
	gitCommit(t, s.Root, "notes.txt", "unmanaged\n")
	git(t, s.Root, "revert", "--no-edit", "HEAD")
	req.LocalSHA = gitOutput(t, s.Root, "rev-parse", "HEAD")
	_, err := s.CommitSemanticMerge(context.Background(), req)
	var blocked *UnmanagedHistoryError
	if !errors.As(err, &blocked) {
		t.Fatalf("missing typed history blocker: %v", err)
	}
	if gitOutput(t, s.Root, "rev-parse", "HEAD") != req.LocalSHA {
		t.Fatal("blocked merge changed HEAD")
	}
}

type failMergeCommitRunner struct{ command.SystemRunner }

func (r failMergeCommitRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	for _, arg := range args {
		if arg == "commit" {
			return "sensitive output", errors.New("injected commit failure")
		}
	}
	return r.SystemRunner.Run(ctx, name, args...)
}

type failRollbackRunner struct{ failMergeCommitRunner }

func (r failRollbackRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	for _, arg := range args {
		if arg == "--abort" {
			return "", errors.New("injected rollback failure")
		}
	}
	return r.failMergeCommitRunner.Run(ctx, name, args...)
}

func TestSemanticMergeRollbackFailureRequiresRepair(t *testing.T) {
	s, req := semanticMergeFixture(t)
	s.Runner = failRollbackRunner{}
	if _, err := s.CommitSemanticMerge(context.Background(), req); err == nil || !strings.Contains(err.Error(), "HIGH SEVERITY") {
		t.Fatalf("rollback failure was not escalated: %v", err)
	}
}

type cancelAfterMergeRunner struct {
	command.SystemRunner
	cancel context.CancelFunc
}

func (r cancelAfterMergeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := r.SystemRunner.Run(ctx, name, args...)
	for _, arg := range args {
		if arg == "--no-commit" {
			r.cancel()
		}
	}
	return out, err
}

func TestSemanticMergeCancellationAfterStartStillRollsBack(t *testing.T) {
	s, req := semanticMergeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Runner = cancelAfterMergeRunner{cancel: cancel}
	if _, err := s.CommitSemanticMerge(ctx, req); err == nil {
		t.Fatal("cancelled transaction reported success")
	}
	if gitOutput(t, s.Root, "rev-parse", "HEAD") != req.LocalSHA || gitOutput(t, s.Root, "status", "--porcelain") != "" {
		t.Fatal("cancellation left partial checkout mutation")
	}
	if _, err := os.Lstat(filepath.Join(s.Root, ".git/MERGE_HEAD")); !os.IsNotExist(err) {
		t.Fatal("cancelled transaction left active merge")
	}
}

func TestSemanticMergeRollbackRestoresHeadIndexAndWorktree(t *testing.T) {
	s, req := semanticMergeFixture(t)
	index := gitOutput(t, s.Root, "write-tree")
	s.Runner = failMergeCommitRunner{}
	_, err := s.CommitSemanticMerge(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "commit") || strings.Contains(err.Error(), "sensitive output") {
		t.Fatalf("unsafe failure: %v", err)
	}
	if gitOutput(t, s.Root, "rev-parse", "HEAD") != req.LocalSHA || gitOutput(t, s.Root, "write-tree") != index || gitOutput(t, s.Root, "status", "--porcelain") != "" {
		t.Fatal("rollback did not restore exact state")
	}
}

func TestFastForwardToRejectsStaleAndDirtyInputsWithoutMutation(t *testing.T) {
	for _, name := range []string{"head", "upstream", "managed", "unmanaged", "staged", "untracked", "merge", "rebase", "cherry-pick", "cancelled", "not-exact"} {
		t.Run(name, func(t *testing.T) {
			_, remote, local := gitRemoteClones(t)
			l := gitOutput(t, local, "rev-parse", "HEAD")
			gitCommit(t, remote, "profile.toml", "reviewed\n")
			git(t, remote, "push")
			git(t, local, "fetch")
			r := gitOutput(t, local, "rev-parse", "@{upstream}")
			ctx := context.Background()
			switch name {
			case "head":
				gitCommit(t, local, "profile.toml", "local\n")
			case "upstream":
				gitCommit(t, remote, "profile.toml", "later\n")
				git(t, remote, "push")
				git(t, local, "fetch")
			case "managed", "staged":
				mustWrite(t, filepath.Join(local, "profile.toml"), "dirty\n")
				if name == "staged" {
					git(t, local, "add", "profile.toml")
				}
			case "unmanaged", "untracked":
				mustWrite(t, filepath.Join(local, "notes.txt"), "do not lose\n")
			case "merge":
				mustWrite(t, filepath.Join(local, ".git", "MERGE_HEAD"), r+"\n")
			case "rebase":
				mustWrite(t, filepath.Join(local, ".git", "rebase-merge", "head-name"), "refs/heads/main\n")
			case "cherry-pick":
				mustWrite(t, filepath.Join(local, ".git", "CHERRY_PICK_HEAD"), r+"\n")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "not-exact":
				r = "origin/main"
			}
			beforeHead := gitOutput(t, local, "rev-parse", "HEAD")
			beforeStatus := gitOutput(t, local, "--no-optional-locks", "status", "--porcelain=v2")
			if _, err := profileGitService(t, local).FastForwardTo(ctx, l, r); err == nil {
				t.Fatal("accepted invalid execution preconditions")
			}
			if gitOutput(t, local, "rev-parse", "HEAD") != beforeHead || gitOutput(t, local, "--no-optional-locks", "status", "--porcelain=v2") != beforeStatus {
				t.Fatal("rejected execution mutated checkout/index")
			}
		})
	}
}
