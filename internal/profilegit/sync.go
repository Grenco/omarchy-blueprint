package profilegit

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Result reports a profile repository mutation and refreshed local status.
type Result struct {
	Changed bool   `json:"changed"`
	Commit  string `json:"commit,omitempty"`
	Status  Status `json:"status"`
}

// Init initializes a Git repository at exactly the profile root.
func (s Service) Init(ctx context.Context) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if status.Repository {
		return Result{Status: status}, nil
	}
	if _, err := s.Runner.Run(ctx, "git", "init", "-b", "main", s.Root); err != nil {
		return Result{}, fmt.Errorf("initialize profile repository: %w", err)
	}
	status, err = s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Status: status}, nil
}

// Remote returns the sanitized origin URL, or an empty string when absent.
func (s Service) Remote(ctx context.Context) (string, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	if !status.Repository {
		return "", nil
	}
	return status.Origin, nil
}

// SetRemote adds or replaces the origin URL without changing other remotes.
func (s Service) SetRemote(ctx context.Context, rawURL string) (Result, error) {
	if strings.TrimSpace(rawURL) == "" {
		return Result{}, fmt.Errorf("origin URL is empty")
	}
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Repository {
		return Result{}, fmt.Errorf("profile is not a Git repository")
	}
	command := []string{"-C", s.Root, "remote", "add", "origin", rawURL}
	if status.Origin != "" {
		command = []string{"-C", s.Root, "remote", "set-url", "origin", rawURL}
	}
	if _, err := s.Runner.Run(ctx, "git", command...); err != nil {
		return Result{}, fmt.Errorf("set profile origin: %w", err)
	}
	status, err = s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Status: status}, nil
}

// RemoveRemote removes origin and leaves all other remotes unchanged.
func (s Service) RemoveRemote(ctx context.Context) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Repository || status.Origin == "" {
		return Result{Status: status}, nil
	}
	if _, err := s.Runner.Run(ctx, "git", "-C", s.Root, "remote", "remove", "origin"); err != nil {
		return Result{}, fmt.Errorf("remove profile origin: %w", err)
	}
	status, err = s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Status: status}, nil
}

// Fetch updates locally cached refs from origin. It never runs implicitly from Status.
func (s Service) Fetch(ctx context.Context) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Repository {
		return Result{}, fmt.Errorf("profile is not a Git repository")
	}
	if status.Origin == "" {
		return Result{}, fmt.Errorf("profile origin is not configured; set origin before fetching")
	}
	if _, err := s.Runner.Run(ctx, "git", "-C", s.Root, "fetch", "origin"); err != nil {
		return Result{}, fmt.Errorf("fetch profile origin: %w", err)
	}
	return s.syncResult(ctx)
}

// Pull fast-forwards the current branch only when the entire repository is clean.
func (s Service) Pull(ctx context.Context) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Repository {
		return Result{}, fmt.Errorf("profile is not a Git repository")
	}
	if status.Head == "" {
		return Result{}, fmt.Errorf("profile repository has no HEAD commit")
	}
	if status.Upstream == "" {
		return Result{}, fmt.Errorf("profile branch has no upstream; configure one before pulling")
	}
	if len(status.Changes) != 0 {
		return Result{}, fmt.Errorf("profile repository has uncommitted changes; commit or discard them before pulling")
	}
	if _, err := s.Runner.Run(ctx, "git", "-C", s.Root, "pull", "--ff-only"); err != nil {
		return Result{}, fmt.Errorf("pull profile with --ff-only (use Git or LazyGit to resolve divergent history): %w", err)
	}
	return s.syncResult(ctx)
}

// Push publishes committed history without modifying the working tree or index.
func (s Service) Push(ctx context.Context) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Repository {
		return Result{}, fmt.Errorf("profile is not a Git repository")
	}
	if status.Head == "" {
		return Result{}, fmt.Errorf("profile repository has no HEAD commit")
	}
	if status.Origin == "" {
		return Result{}, fmt.Errorf("profile origin is not configured; set origin before pushing")
	}
	if status.Branch == "" {
		return Result{}, fmt.Errorf("cannot push a detached HEAD")
	}
	command := []string{"-C", s.Root, "push"}
	if status.Upstream == "" {
		command = append(command, "--set-upstream", "origin", status.Branch)
	}
	if _, err := s.Runner.Run(ctx, "git", command...); err != nil {
		return Result{}, fmt.Errorf("push profile: %w", err)
	}
	return s.syncResult(ctx)
}

func (s Service) syncResult(ctx context.Context) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Status: status}, nil
}

// BrowserURL converts conventional Git remotes to a safe HTTPS browser URL.
func BrowserURL(rawRemote string) (string, bool) {
	if parsed, err := url.Parse(rawRemote); err == nil && (parsed.Scheme == "https" || parsed.Scheme == "ssh") && parsed.Host != "" {
		path := strings.TrimSuffix(parsed.EscapedPath(), ".git")
		if path == "" || path == "/" {
			return "", false
		}
		return "https://" + parsed.Host + path, true
	}
	if at := strings.Index(rawRemote, "@"); at > 0 {
		if colon := strings.Index(rawRemote[at+1:], ":"); colon >= 0 {
			host := rawRemote[at+1 : at+1+colon]
			path := strings.TrimSuffix(rawRemote[at+2+colon:], ".git")
			if host != "" && path != "" && !strings.ContainsAny(host+path, " \t\n") {
				return "https://" + host + "/" + path, true
			}
		}
	}
	return "", false
}
