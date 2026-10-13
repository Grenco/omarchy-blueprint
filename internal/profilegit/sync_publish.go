package profilegit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

// PublishSyncRevision names exactly one commit and tracking branch. It does
// not inherit push.default, matching refspecs, mirror or implicit tag pushes.
// Fetch afterwards proves reachability only; the newer tip is not checkout or
// machine execution authority. Existing Profile Git transport is unchanged.
func (s Service) PublishSyncRevision(ctx context.Context, final, branch, upstream, fingerprint string) error {
	if !exactCommitID.MatchString(final) {
		return errors.New("Sync publication requires exact revision")
	}
	info, err := s.CurrentRevisionInfo(ctx)
	if err != nil {
		return err
	}
	if info.SHA != final || info.Branch != branch || info.Upstream != upstream || info.OriginFingerprint != fingerprint {
		return errors.New("Sync publication identity changed")
	}
	status, err := s.ReadOnlyStatus(ctx)
	if err != nil {
		return err
	}
	if len(status.Changes) > 0 {
		return errors.New("Sync publication requires clean checkout/index")
	}
	out, err := s.revisionRun(ctx, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", "refs/heads/"+branch)
	if err != nil {
		return revisionError(ctx, "resolve Sync publication destination", err)
	}
	parts := strings.Split(strings.TrimSpace(out), "\x00")
	if len(parts) != 2 || parts[0] == "" || parts[0] == "." || !strings.HasPrefix(parts[1], "refs/heads/") {
		return errors.New("invalid Sync publication destination")
	}
	urls, err := s.revisionRun(ctx, "remote", "get-url", "--push", "--all", "--", parts[0])
	if err != nil {
		return revisionError(ctx, "inspect Sync publication identity", err)
	}
	identities := strings.Split(strings.TrimSpace(urls), "\n")
	if len(identities) != 1 {
		return errors.New("Sync requires one tracking push destination")
	}
	identity, err := remoteIdentity(identities[0])
	if err != nil {
		return err
	}
	if fmtFingerprint(identity) != fingerprint {
		return errors.New("tracking fetch and push destinations differ; configure one Sync upstream")
	}
	_, err = s.revisionRun(ctx, "-c", "remote."+parts[0]+".mirror=false", "push", "--no-follow-tags", "--recurse-submodules=no", "--", parts[0], final+":"+parts[1])
	if err != nil {
		return revisionError(ctx, "publish reviewed Sync revision (do not force; reinspect)", err)
	}
	if err := s.FetchUpstream(ctx); err != nil {
		return err
	}
	r, err := s.UpstreamCommit(ctx)
	if err != nil {
		return err
	}
	ok, err := s.IsAncestor(ctx, final, r)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("published Sync revision is not reachable on tracking branch")
	}
	return nil
}
func fmtFingerprint(identity string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}
