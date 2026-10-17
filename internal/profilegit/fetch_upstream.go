package profilegit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

// Fetch failures a person can act on. They carry no Git output.
var (
	ErrFetchSignIn      = errors.New("the shared profile needs you to sign in")
	ErrFetchUnreachable = errors.New("the shared profile could not be reached")
)

var (
	signInSignals = []string{
		"permission denied", "authentication failed", "could not read username", "could not read password",
		"terminal prompts disabled", "returned error: 401", "returned error: 403", "host key verification failed",
		"invalid username or password", "askpass",
	}
	unreachableSignals = []string{
		"could not resolve host", "name or service not known", "temporary failure in name resolution",
		"failed to connect", "connection refused", "connection timed out", "operation timed out",
		"no route to host", "network is unreachable", "connection reset",
	}
)

// classifyFetchFailure inspects Git's output only to choose a category;
// the output itself is never returned.
func classifyFetchFailure(err error) error {
	var run *command.RunError
	if !errors.As(err, &run) {
		return nil
	}
	output := strings.ToLower(run.Output)
	for _, signal := range signInSignals {
		if strings.Contains(output, signal) {
			return ErrFetchSignIn
		}
	}
	for _, signal := range unreachableSignals {
		if strings.Contains(output, signal) {
			return ErrFetchUnreachable
		}
	}
	return nil
}

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
		if ctx.Err() == nil {
			if class := classifyFetchFailure(err); class != nil {
				return fmt.Errorf("fetch Sync upstream failed: %w", class)
			}
		}
		return revisionError(ctx, "fetch Sync upstream", err)
	}
	return ctx.Err()
}
