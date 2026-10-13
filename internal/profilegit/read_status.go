package profilegit

import (
	"context"
	"sort"
)

// ReadOnlyStatus suppresses Git's optional index-stat refresh. Sync must not
// write even this index metadata while inspecting committed intent.
func (s Service) ReadOnlyStatus(ctx context.Context) (Status, error) {
	if err := s.requireExactRepository(ctx); err != nil {
		return Status{}, err
	}
	out, err := s.revisionOutput(ctx, "--no-optional-locks", "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return Status{}, revisionError(ctx, "inspect read-only Git status", err)
	}
	result := Status{Repository: true}
	if err := parseStatus(out, &result); err != nil {
		return Status{}, revisionError(ctx, "parse read-only Git status", err)
	}
	sort.Slice(result.Changes, func(i, j int) bool { return result.Changes[i].Path < result.Changes[j].Path })
	return result, nil
}
