package resources

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
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
	topLevel, err := runner.Run(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	if err != nil {
		return GitState{}, false, nil
	}
	if filepath.Clean(strings.TrimSpace(topLevel)) != filepath.Clean(root) {
		return GitState{}, false, nil
	}
	status, err := runner.Run(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return GitState{}, true, fmt.Errorf("inspect Git status: %w", err)
	}
	state := GitState{Dirty: strings.TrimSpace(status) != ""}
	remote, err := runner.Run(ctx, "git", "-C", root, "remote", "get-url", "origin")
	if err != nil {
		return state, true, fmt.Errorf("read Git origin: %w", err)
	}
	state.Remote, err = PortableGitRemote(strings.TrimSpace(remote))
	if err != nil {
		return state, true, err
	}
	revision, err := runner.Run(ctx, "git", "-C", root, "rev-parse", "HEAD")
	if err != nil {
		return state, true, fmt.Errorf("read Git revision: %w", err)
	}
	state.Revision = strings.TrimSpace(revision)
	if !validGitRevision(state.Revision) {
		return state, true, fmt.Errorf("invalid Git revision %q", state.Revision)
	}
	branch, err := runner.Run(ctx, "git", "-C", root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		state.Branch = strings.TrimSpace(branch)
	}
	return state, true, nil
}

func PortableGitRemote(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return "", fmt.Errorf("Git remote is not portable: %s", raw)
	}
	if isSCPLikeRemote(raw) {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "ssh") {
		return "", fmt.Errorf("Git remote is not portable: %s", raw)
	}
	if parsed.Scheme == "https" {
		parsed.User = nil
	}
	return parsed.String(), nil
}

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
