package profilegit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
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

// fetchFailingRunner lets every Git call through except fetch, which fails
// with the given output.
type fetchFailingRunner struct {
	command.SystemRunner
	output string
}

func (r fetchFailingRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	for _, arg := range args {
		if arg == "fetch" {
			return r.output, &command.RunError{Name: name, Args: args, Output: r.output, ExitCode: 128, Err: errors.New("exit status 128")}
		}
	}
	return r.SystemRunner.Run(ctx, name, args...)
}

// A failed fetch says whether a person needs to sign in or the shared
// profile is unreachable, without carrying any Git output (which can hold
// URLs with credentials).
func TestFetchUpstreamClassifiesFailuresWithoutGitOutput(t *testing.T) {
	_, local, _ := gitRemoteClones(t)
	cases := map[string]struct {
		output string
		want   error
	}{
		"ssh key":       {"git@example.invalid: Permission denied (publickey).\r\nfatal: Could not read from remote repository.", ErrFetchSignIn},
		"https prompt":  {"fatal: could not read Username for 'https://token@example.invalid': terminal prompts disabled", ErrFetchSignIn},
		"https denied":  {"remote: Invalid username or password.\nfatal: Authentication failed for 'https://user:secret@example.invalid/'", ErrFetchSignIn},
		"https 403":     {"fatal: unable to access 'https://example.invalid/': The requested URL returned error: 403", ErrFetchSignIn},
		"host key":      {"Host key verification failed.\nfatal: Could not read from remote repository.", ErrFetchSignIn},
		"dns":           {"ssh: Could not resolve hostname example.invalid: Name or service not known\nfatal: Could not read from remote repository.", ErrFetchUnreachable},
		"https dns":     {"fatal: unable to access 'https://user:secret@example.invalid/': Could not resolve host: example.invalid", ErrFetchUnreachable},
		"refused":       {"fatal: unable to access 'https://127.0.0.1:1/': Failed to connect to 127.0.0.1 port 1: Connection refused", ErrFetchUnreachable},
		"timeout":       {"ssh: connect to host example.invalid port 22: Connection timed out", ErrFetchUnreachable},
		"no route":      {"ssh: connect to host 10.0.0.1 port 22: No route to host", ErrFetchUnreachable},
		"network":       {"ssh: connect to host 10.0.0.1 port 22: Network is unreachable", ErrFetchUnreachable},
		"other failure": {"fatal: bad object refs/remotes/origin/main", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := profileGitService(t, local)
			s.Runner = fetchFailingRunner{output: tc.output}
			err := s.FetchUpstream(context.Background())
			if err == nil {
				t.Fatal("fetch failure not reported")
			}
			for _, known := range []error{ErrFetchSignIn, ErrFetchUnreachable} {
				if errors.Is(err, known) != (known == tc.want) {
					t.Fatalf("classified %v, want %v", err, tc.want)
				}
			}
			if strings.Contains(err.Error(), "example.invalid") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "127.0.0.1") {
				t.Fatalf("Git output leaked: %v", err)
			}
		})
	}
}
