package profilegit

import (
	"context"
	"errors"
	"strings"
)

// FetchUpstream is an explicit Sync inspection request. Existing Profile Git
// origin transport/authentication behavior remains unchanged. No pull or HEAD
// update is involved, and failures never include credential-bearing Git output.
func (s Service) FetchUpstream(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := s.CurrentRevisionInfo(ctx)
	if err != nil {
		return err
	}
	out, err := s.revisionRun(ctx, "for-each-ref", "--format=%(upstream:remotename)", "refs/heads/"+info.Branch)
	if err != nil {
		return revisionError(ctx, "resolve Sync tracking remote", err)
	}
	remote := strings.TrimSpace(out)
	if remote == "" || remote == "." || strings.ContainsAny(remote, "\x00\r\n") {
		return errors.New("invalid Sync tracking remote")
	}
	_, err = s.revisionRun(ctx, "fetch", "--", remote)
	if err != nil {
		return revisionError(ctx, "fetch Sync upstream", err)
	}
	return ctx.Err()
}
