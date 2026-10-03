package profilegit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Endpoint-only comparison, unpinned refs, or treating missing objects as non-ancestors loses lineage evidence.
func TestRevisionExactObjectsAndAncestorTopology(t *testing.T) {
	_, local, remote := gitRemoteClones(t)
	ctx := context.Background()
	s := profileGitService(t, local)
	base := gitOutput(t, local, "rev-parse", "HEAD")
	for _, ref := range []string{"HEAD", base, "refs/heads/main"} {
		sha, err := s.ResolveCommit(ctx, ref)
		if err != nil || sha != base {
			t.Fatal(sha, err)
		}
	}
	git(t, local, "tag", "-a", "approved", "-m", "tag")
	if sha, err := s.ResolveCommit(ctx, "approved"); err != nil || sha != base {
		t.Fatal(sha, err)
	}
	blob := gitOutput(t, local, "rev-parse", "HEAD:profile.toml")
	for _, ref := range []string{blob, strings.Repeat("a", 40), "--help", ""} {
		if _, err := s.ResolveCommit(ctx, ref); err == nil {
			t.Fatal("accepted noncommit/missing/option", ref)
		}
	}
	gitCommit(t, local, "packages/local.txt", "local")
	left := gitOutput(t, local, "rev-parse", "HEAD")
	gitCommit(t, remote, "packages/remote.txt", "remote")
	git(t, remote, "push")
	git(t, local, "fetch", "origin")
	right := gitOutput(t, local, "rev-parse", "origin/main")
	for _, tc := range []struct {
		base, tip string
		want      bool
	}{{base, left, true}, {base, right, true}, {left, right, false}, {right, left, false}, {base, base, true}} {
		got, err := s.IsAncestor(ctx, tc.base, tc.tip)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := s.IsAncestor(ctx, strings.Repeat("a", 40), left); err == nil {
		t.Fatal("missing base treated as false")
	}
	if sha, err := s.UpstreamCommit(ctx); err != nil || sha != right {
		t.Fatal(sha, err)
	}
	info, err := s.CurrentRevisionInfo(ctx)
	if err != nil || info.SHA != left || info.Branch != "main" || info.Upstream != "origin/main" || len(info.OriginFingerprint) != 64 {
		t.Fatal(info, err)
	}
	git(t, local, "checkout", "--detach")
	if _, err := s.CurrentRevisionInfo(ctx); err == nil {
		t.Fatal("detached HEAD accepted")
	}
	git(t, local, "checkout", "main")
	git(t, local, "branch", "--unset-upstream")
	if _, err := s.CurrentRevisionInfo(ctx); err == nil {
		t.Fatal("missing upstream accepted")
	}
	if _, err := s.UpstreamCommit(ctx); err == nil {
		t.Fatal("missing upstream resolved")
	}
}

func TestRevisionRemoteIdentityIsSanitizedAndTracksConfiguredRemote(t *testing.T) {
	_, local, _ := gitRemoteClones(t)
	s := profileGitService(t, local)
	ctx := context.Background()
	git(t, local, "remote", "set-url", "origin", "https://user:token@example.invalid/team/profile.git?access_token=secret#secret")
	first, err := s.CurrentRevisionInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.OriginFingerprint, "token") || strings.Contains(first.OriginFingerprint, "secret") {
		t.Fatal("sensitive fingerprint")
	}
	git(t, local, "remote", "set-url", "origin", "https://other:password@example.invalid/team/profile.git")
	second, err := s.CurrentRevisionInfo(ctx)
	if err != nil || second.OriginFingerprint != first.OriginFingerprint {
		t.Fatal("credentials altered repository identity", err)
	}
	git(t, local, "remote", "set-url", "origin", "https://example.invalid/team/other.git")
	third, err := s.CurrentRevisionInfo(ctx)
	if err != nil || third.OriginFingerprint == first.OriginFingerprint {
		t.Fatal("repository change ignored", err)
	}
	git(t, local, "remote", "add", "backup", "https://example.invalid/team/profile.git")
	git(t, local, "config", "branch.main.remote", "backup")
	git(t, local, "update-ref", "refs/remotes/backup/main", third.SHA)
	fourth, err := s.CurrentRevisionInfo(ctx)
	if err != nil || fourth.Upstream != "backup/main" || fourth.OriginFingerprint != first.OriginFingerprint {
		t.Fatal("wrong tracking remote", fourth, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ResolveCommit(cancelled, "HEAD"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
}

func TestCommittedPathsIncludesRevertedChangesRenameCopyAndMergeHistory(t *testing.T) {
	_, root, _ := gitRemoteClones(t)
	s := profileGitService(t, root)
	ctx := context.Background()
	gitCommit(t, root, "notes/source.txt", "original\n")
	base := gitOutput(t, root, "rev-parse", "HEAD")
	gitCommit(t, root, "notes/ephemeral.txt", "temporary\n")
	git(t, root, "rm", "notes/ephemeral.txt")
	git(t, root, "commit", "-m", "revert")
	mustMkdir(t, filepath.Join(root, "config"))
	git(t, root, "mv", "notes/source.txt", "config/renamed.txt")
	git(t, root, "commit", "-am", "rename")
	mustWrite(t, filepath.Join(root, "config/copied.txt"), "original\n")
	git(t, root, "add", "config/copied.txt")
	git(t, root, "commit", "-m", "copy")
	// A merged side branch must be audited too, even if its change is later reverted.
	git(t, root, "checkout", "-b", "side")
	gitCommit(t, root, "notes/side.txt", "side\n")
	git(t, root, "checkout", "main")
	gitCommit(t, root, "config/main.txt", "main\n")
	git(t, root, "merge", "--no-ff", "side", "-m", "merge")
	tip := gitOutput(t, root, "rev-parse", "HEAD")
	got, err := s.CommittedPaths(ctx, base, tip)
	want := []string{"config/copied.txt", "config/main.txt", "config/renamed.txt", "notes/ephemeral.txt", "notes/side.txt", "notes/source.txt"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	if got, err := s.CommittedPaths(ctx, base, base); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	if _, err := s.CommittedPaths(ctx, tip, base); err == nil {
		t.Fatal("reversed history accepted")
	}
}

func TestRevisionRefusesParentRepository(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	gitCommit(t, root, "profile.toml", "x")
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := profileGitService(t, child).ResolveCommit(context.Background(), "HEAD"); err == nil {
		t.Fatal("parent repository accepted")
	}
}
