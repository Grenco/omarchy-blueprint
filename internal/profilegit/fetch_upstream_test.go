package profilegit

import (
	"context"
	"testing"
)

func TestFetchUpstreamUsesConfiguredRemoteWithoutCheckout(t *testing.T) {
	_, local, peer := gitRemoteClones(t)
	git(t, local, "remote", "rename", "origin", "shared")
	before, index := gitOutput(t, local, "rev-parse", "HEAD"), gitOutput(t, local, "write-tree")
	gitCommit(t, peer, "packages/new.txt", "new")
	git(t, peer, "push")
	remote := gitOutput(t, peer, "rev-parse", "HEAD")
	s := profileGitService(t, local)
	if err := s.FetchUpstream(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpstreamCommit(context.Background())
	if err != nil || got != remote {
		t.Fatal(got, err)
	}
	if gitOutput(t, local, "rev-parse", "HEAD") != before || gitOutput(t, local, "write-tree") != index || gitOutput(t, local, "status", "--porcelain") != "" {
		t.Fatal("fetch changed checkout/index")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.FetchUpstream(ctx); err != context.Canceled {
		t.Fatal(err)
	}
}
