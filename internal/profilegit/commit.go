package profilegit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

// ErrStagedChanges means Blueprint cannot safely preserve an existing index
// selection while preparing a managed-only commit.
var ErrStagedChanges = errors.New("profile repository already has staged changes")

// SuggestedCommitMessage returns a deterministic message for managed changes.
func SuggestedCommitMessage(status Status) string {
	areas := make(map[string]struct{})
	for _, change := range status.Changes {
		path := change.Path
		if !change.Managed && change.OriginalManaged {
			path = change.OriginalPath
		}
		if !profile.IsManagedRepositoryPath(path) {
			continue
		}
		areas[strings.SplitN(path, "/", 2)[0]] = struct{}{}
	}
	if len(areas) == 0 {
		return "Update Blueprint profile"
	}
	ordered := make([]string, 0, len(areas))
	for area := range areas {
		ordered = append(ordered, area)
	}
	sort.Strings(ordered)
	return "Update Blueprint profile: " + strings.Join(ordered, ", ")
}

// Commit stages and commits Blueprint-managed profile paths only.
func (s Service) Commit(ctx context.Context, message string) (Result, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	if !status.Repository {
		return Result{}, fmt.Errorf("profile is not a Git repository")
	}
	for _, change := range status.Changes {
		if change.Index != "" {
			return Result{Status: status}, ErrStagedChanges
		}
	}

	pathspecs := managedChangedRoots(status)
	if len(pathspecs) == 0 {
		return Result{Status: status}, nil
	}
	addArgs := append([]string{"-C", s.Root, "add", "-A", "--"}, pathspecs...)
	if _, err := s.Runner.Run(ctx, "git", addArgs...); err != nil {
		return Result{}, fmt.Errorf("stage managed profile changes: %w", err)
	}

	staged, err := s.Status(ctx)
	if err != nil {
		s.unstageManaged(ctx, pathspecs)
		return Result{}, err
	}
	for _, change := range staged.Changes {
		if change.Index != "" && !change.Managed {
			s.unstageManaged(ctx, pathspecs)
			return Result{}, fmt.Errorf("managed staging included unmanaged path: %s", change.Path)
		}
	}
	if !hasStagedManagedChange(staged) {
		return Result{Status: staged}, nil
	}

	if strings.TrimSpace(message) == "" {
		message = SuggestedCommitMessage(staged)
	}
	if _, err := s.Runner.Run(ctx, "git", "-C", s.Root, "commit", "-m", message); err != nil {
		s.unstageManaged(ctx, pathspecs)
		return Result{}, fmt.Errorf("commit managed profile changes: %w", err)
	}
	status, err = s.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Changed: true, Commit: status.Head, Status: status}, nil
}

func hasStagedManagedChange(status Status) bool {
	for _, change := range status.Changes {
		if change.Index != "" && change.Managed {
			return true
		}
	}
	return false
}

func managedChangedRoots(status Status) []string {
	roots := make(map[string]struct{})
	for _, change := range status.Changes {
		path := change.Path
		if !change.Managed && change.OriginalManaged {
			path = change.OriginalPath
		}
		if profile.IsManagedRepositoryPath(path) {
			roots[strings.SplitN(path, "/", 2)[0]] = struct{}{}
		}
	}
	pathspecs := make([]string, 0, len(roots))
	for root := range roots {
		pathspecs = append(pathspecs, root)
	}
	sort.Strings(pathspecs)
	return pathspecs
}

func (s Service) unstageManaged(ctx context.Context, pathspecs []string) {
	args := append([]string{"-C", s.Root, "reset", "--"}, pathspecs...)
	_, _ = s.Runner.Run(ctx, "git", args...)
}
