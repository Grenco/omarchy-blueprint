package profilegit

import (
	"context"
	"testing"
)

func TestSyncPublicationUsesOnlyConfiguredTrackingDestination(t *testing.T) {
	origin, _, local := gitRemoteClones(t)
	git(t, local, "remote", "rename", "origin", "tracking")
	git(t, local, "config", "push.default", "matching")
	git(t, local, "config", "push.followTags", "true")
	git(t, local, "config", "remote.tracking.push", "refs/heads/side:refs/heads/side")
	git(t, local, "branch", "side")
	git(t, local, "tag", "-a", "unrequested", "-m", "not Sync intent")
	gitCommit(t, local, "profile.toml", "accepted local intent\n")
	s := profileGitService(t, local)
	info, err := s.CurrentRevisionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishSyncRevision(context.Background(), info.SHA, info.Branch, info.Upstream, info.OriginFingerprint); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, origin, "rev-parse", "refs/heads/main"); got != info.SHA {
		t.Fatal("published a different revision")
	}
	if got := gitOutput(t, origin, "for-each-ref", "--format=%(refname)", "refs/heads/side", "refs/tags"); got != "" {
		t.Fatalf("published unrelated refs: %s", got)
	}
}

func TestSyncPublicationRejectsDifferentPushRepository(t *testing.T) {
	origin, _, local := gitRemoteClones(t)
	other, _, _ := gitRemoteClones(t)
	git(t, local, "remote", "set-url", "--push", "origin", other)
	gitCommit(t, local, "profile.toml", "local intent\n")
	s := profileGitService(t, local)
	info, err := s.CurrentRevisionInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	before := gitOutput(t, origin, "rev-parse", "refs/heads/main")
	if err := s.PublishSyncRevision(context.Background(), info.SHA, info.Branch, info.Upstream, info.OriginFingerprint); err == nil {
		t.Fatal("accepted a different push repository")
	}
	if got := gitOutput(t, origin, "rev-parse", "refs/heads/main"); got != before {
		t.Fatal("changed fetch repository")
	}
}
