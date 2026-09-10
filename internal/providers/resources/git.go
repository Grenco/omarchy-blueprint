package resources

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type GitState struct {
	Remote   string
	Branch   string
	Revision string
	Dirty    bool
}

func DetectGitResource(ctx context.Context, runner command.Runner, root string) (GitState, bool, error) {
	working, isGit, err := InspectGitWorkingState(ctx, runner, root)
	if err != nil || !isGit {
		return GitState{}, isGit, err
	}
	dirty := working.StagedTracked > 0 || working.UnstagedTracked > 0 || len(working.Untracked) > 0 || working.Conflicted || working.DirtySubmodule
	return GitState{Remote: working.Remote, Branch: working.Branch, Revision: working.Revision, Dirty: dirty}, true, nil
}

func PortableGitRemote(raw string) (string, error) {
	if repo, ok := githubRepo(raw); ok {
		return repo, nil
	}
	if raw == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return "", fmt.Errorf("Git remote is not portable: %s", raw)
	}
	if isSCPLikeRemote(raw) {
		if repo, ok := githubRepo(raw); ok {
			return repo, nil
		}
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "ssh") {
		return "", fmt.Errorf("Git remote is not portable: %s", raw)
	}
	if parsed.Scheme == "https" {
		parsed.User = nil
	}
	if repo, ok := githubRepo(parsed.String()); ok {
		return repo, nil
	}
	return parsed.String(), nil
}

func githubRepo(remote string) (string, bool) {
	if strings.HasPrefix(remote, "github.com/") {
		parts := strings.Split(strings.TrimPrefix(remote, "github.com/"), "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			return "github.com/" + parts[0] + "/" + strings.TrimSuffix(parts[1], ".git"), true
		}
	}
	if isSCPLikeRemote(remote) {
		at, colon := strings.IndexByte(remote, '@'), strings.IndexByte(remote, ':')
		if strings.EqualFold(remote[at+1:colon], "github.com") {
			return githubRepo("github.com/" + remote[colon+1:])
		}
		return "", false
	}
	parsed, err := url.Parse(remote)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", false
	}
	return githubRepo("github.com/" + strings.TrimPrefix(parsed.Path, "/"))
}

func isGitHubRepo(remote string) bool { _, ok := githubRepo(remote); return ok }

func EqualGitResource(saved profile.Resource, current GitState) bool {
	return saved.Strategy == "git" && !current.Dirty && saved.Remote == current.Remote && saved.Revision == current.Revision
}

func validGitRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func isSCPLikeRemote(value string) bool {
	at, colon := strings.IndexByte(value, '@'), strings.IndexByte(value, ':')
	return at > 0 && colon > at+1 && !strings.ContainsAny(value[:colon], "/ ") && colon < len(value)-1
}
