package profilegit

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

var exactCommitID = regexp.MustCompile(`^[0-9a-f]{40}$`)

type RevisionInfo struct {
	SHA      string
	Branch   string
	Upstream string
	// OriginFingerprint identifies the configured tracking remote, which need not be named origin.
	OriginFingerprint string
}

// Exact history reads must interpret stored objects, never local replacement refs.
func (s Service) revisionRun(ctx context.Context, args ...string) (string, error) {
	return s.Runner.Run(ctx, "git", append([]string{"--no-replace-objects", "-C", s.Root}, args...)...)
}
func (s Service) revisionOutput(ctx context.Context, args ...string) ([]byte, error) {
	return command.RunOutput(ctx, s.Runner, maxStatusOutput, "git", append([]string{"--no-replace-objects", "-C", s.Root}, args...)...)
}

func revisionError(ctx context.Context, action string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", action, ctx.Err())
	}
	// Git errors may contain ref names, configured URLs, or profile contents.
	return fmt.Errorf("%s failed", action)
}

func (s Service) requireExactRepository(ctx context.Context) error {
	out, err := s.revisionRun(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return revisionError(ctx, "inspect profile repository", err)
	}
	if filepath.Clean(strings.TrimSpace(out)) != s.Root {
		return errors.New("profile is not an exact-root Git repository")
	}
	return nil
}

// ResolveCommit peels commit-ish refs (including annotated commit tags) to an exact SHA-1.
func (s Service) ResolveCommit(ctx context.Context, ref string) (string, error) {
	if ref == "" || strings.ContainsAny(ref, "\x00\r\n") {
		return "", errors.New("invalid commit reference")
	}
	if err := s.requireExactRepository(ctx); err != nil {
		return "", err
	}
	out, err := s.revisionRun(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", revisionError(ctx, "resolve profile commit", err)
	}
	sha := strings.TrimSpace(out)
	if !exactCommitID.MatchString(sha) {
		return "", errors.New("unsupported Git object identity (expected SHA-1)")
	}
	return sha, nil
}

func (s Service) CurrentRevisionInfo(ctx context.Context) (RevisionInfo, error) {
	sha, err := s.ResolveCommit(ctx, "HEAD")
	if err != nil {
		return RevisionInfo{}, err
	}
	branch, err := s.revisionRun(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return RevisionInfo{}, revisionError(ctx, "resolve current branch (detached HEAD is unsupported)", err)
	}
	// for-each-ref avoids parsing an upstream short name into a guessed remote name.
	out, err := s.revisionRun(ctx, "for-each-ref", "--format=%(upstream:short)%00%(upstream:remotename)", "refs/heads/"+strings.TrimSpace(branch))
	if err != nil {
		return RevisionInfo{}, revisionError(ctx, "resolve tracking identity", err)
	}
	parts := strings.Split(strings.TrimSuffix(out, "\n"), "\x00")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || parts[1] == "." {
		return RevisionInfo{}, errors.New("profile branch requires a configured remote upstream")
	}
	raw, err := s.revisionRun(ctx, "remote", "get-url", "--", parts[1])
	if err != nil {
		return RevisionInfo{}, revisionError(ctx, "resolve tracking remote", err)
	}
	identity, err := remoteIdentity(strings.TrimSpace(raw))
	if err != nil {
		return RevisionInfo{}, err
	}
	return RevisionInfo{SHA: sha, Branch: strings.TrimSpace(branch), Upstream: parts[0], OriginFingerprint: fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))}, nil
}

// SSH usernames select repository namespaces and are part of identity. Passwords,
// HTTPS authentication userinfo, queries and fragments are not repository identity.
func remoteIdentity(raw string) (string, error) {
	if raw == "" || strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("invalid tracking remote identity")
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || (u.Host == "" && u.Scheme != "file") {
			return "", errors.New("invalid tracking remote identity")
		}
		if strings.EqualFold(u.Scheme, "ssh") && u.User != nil {
			u.User = url.User(u.User.Username())
		} else {
			u.User = nil
		}
		u.RawQuery = ""
		u.Fragment = ""
		u.Host = strings.ToLower(u.Host)
		u.Scheme = strings.ToLower(u.Scheme)
		return u.String(), nil
	}
	// SCP syntax has a host/path colon before any slash (outside IPv6
	// brackets). '@' in a repository or local filesystem path is not userinfo.
	bracket := false
	for i, c := range raw {
		if c == '/' {
			break
		}
		if c == '[' {
			bracket = true
		}
		if c == ']' {
			bracket = false
		}
		if c == ':' && !bracket {
			authority, repository := raw[:i], raw[i+1:]
			user := ""
			if at := strings.LastIndex(authority, "@"); at >= 0 {
				user = authority[:at+1]
				authority = authority[at+1:]
			}
			if authority == "" || repository == "" {
				return "", errors.New("invalid tracking remote identity")
			}
			return user + strings.ToLower(authority) + ":" + repository, nil
		}
	}
	return raw, nil
}

func (s Service) UpstreamCommit(ctx context.Context) (string, error) {
	if _, err := s.CurrentRevisionInfo(ctx); err != nil {
		return "", err
	}
	return s.ResolveCommit(ctx, "@{upstream}")
}

func (s Service) IsAncestor(ctx context.Context, base, tip string) (bool, error) {
	b, err := s.ResolveCommit(ctx, base)
	if err != nil {
		return false, err
	}
	t, err := s.ResolveCommit(ctx, tip)
	if err != nil {
		return false, err
	}
	_, err = s.revisionRun(ctx, "merge-base", "--is-ancestor", b, t)
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var run *command.RunError
	if errors.As(err, &run) && run.ExitCode == 1 {
		return false, nil
	}
	return false, revisionError(ctx, "inspect commit ancestry", err)
}

// CommittedPaths audits every commit in the ancestry range, not the endpoint diff.
// Rename/copy sources and destinations both remain visible to the managed-path guard.
func (s Service) CommittedPaths(ctx context.Context, baseExclusive, tipInclusive string) ([]string, error) {
	b, err := s.ResolveCommit(ctx, baseExclusive)
	if err != nil {
		return nil, err
	}
	t, err := s.ResolveCommit(ctx, tipInclusive)
	if err != nil {
		return nil, err
	}
	ancestor, err := s.IsAncestor(ctx, b, t)
	if err != nil {
		return nil, err
	}
	if !ancestor {
		return nil, errors.New("history audit base is not an ancestor")
	}
	commits, err := s.revisionOutput(ctx, "rev-list", b+".."+t)
	if err != nil {
		return nil, revisionError(ctx, "enumerate committed history", err)
	}
	paths := map[string]bool{}
	total := 0
	for _, sha := range strings.Fields(string(commits)) {
		if !exactCommitID.MatchString(sha) {
			return nil, errors.New("invalid history commit identity")
		}
		bytes, err := s.revisionOutput(ctx, "diff-tree", "--root", "-r", "-m", "--no-commit-id", "--name-status", "-z", "-M", "-C", "--find-copies-harder", sha)
		if err != nil {
			return nil, revisionError(ctx, "inspect committed paths", err)
		}
		total += len(bytes)
		if total > maxStatusOutput {
			return nil, errors.New("committed path audit exceeds size limit")
		}
		records := strings.Split(string(bytes), "\x00")
		for i := 0; i < len(records)-1; {
			status := records[i]
			i++
			count := 1
			if status == "" {
				return nil, errors.New("invalid committed path record")
			}
			if status[0] == 'R' || status[0] == 'C' {
				count = 2
			}
			if i+count > len(records)-1 {
				return nil, errors.New("incomplete committed path record")
			}
			for j := 0; j < count; j++ {
				if records[i] == "" {
					return nil, errors.New("empty committed path")
				}
				paths[records[i]] = true
				i++
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}
