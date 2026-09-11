package profilegit

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

const maxStatusOutput = 8 << 20

// Change describes one path reported by Git's porcelain status output.
type Change struct {
	Path            string `json:"path"`
	OriginalPath    string `json:"original_path,omitempty"`
	Index           string `json:"index,omitempty"`
	Worktree        string `json:"worktree,omitempty"`
	Managed         bool   `json:"managed"`
	OriginalManaged bool   `json:"original_managed,omitempty"`
}

// Status is the local state of the repository rooted at a profile.
type Status struct {
	Repository bool     `json:"repository"`
	Branch     string   `json:"branch,omitempty"`
	Head       string   `json:"head,omitempty"`
	Upstream   string   `json:"upstream,omitempty"`
	Ahead      int      `json:"ahead"`
	Behind     int      `json:"behind"`
	Origin     string   `json:"origin,omitempty"`
	Changes    []Change `json:"changes,omitempty"`
}

// Status inspects only the local repository rooted exactly at the profile.
func (s Service) Status(ctx context.Context) (Status, error) {
	top, err := s.Runner.Run(ctx, "git", "-C", s.Root, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(strings.TrimSpace(top)) != filepath.Clean(s.Root) {
		return Status{}, nil
	}

	result := Status{Repository: true}
	out, err := command.RunOutput(ctx, s.Runner, maxStatusOutput, "git", "-C", s.Root, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return Status{}, fmt.Errorf("inspect Git status: %w", err)
	}
	if err := parseStatus(out, &result); err != nil {
		return Status{}, err
	}

	if origin, err := s.Runner.Run(ctx, "git", "-C", s.Root, "remote", "get-url", "origin"); err == nil {
		result.Origin = SanitizeRemote(strings.TrimSpace(origin))
	}
	sort.Slice(result.Changes, func(i, j int) bool { return result.Changes[i].Path < result.Changes[j].Path })
	return result, nil
}

func parseStatus(out []byte, status *Status) error {
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "# ") {
			if err := parseBranchHeader(record, status); err != nil {
				return err
			}
			continue
		}

		var change Change
		switch record[0] {
		case '1':
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 {
				return fmt.Errorf("parse Git status record")
			}
			change = statusChange(fields[1], fields[8])
		case '2':
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || i+1 >= len(records) {
				return fmt.Errorf("parse Git rename status record")
			}
			change = statusChange(fields[1], fields[9])
			change.OriginalPath = records[i+1]
			change.OriginalManaged = profile.IsManagedRepositoryPath(change.OriginalPath)
			i++
		case '?':
			if !strings.HasPrefix(record, "? ") {
				return fmt.Errorf("parse Git untracked status record")
			}
			change = Change{Path: record[2:], Worktree: "?", Managed: profile.IsManagedRepositoryPath(record[2:])}
		case 'u':
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 {
				return fmt.Errorf("parse Git unmerged status record")
			}
			change = statusChange(fields[1], fields[10])
		default:
			continue
		}
		status.Changes = append(status.Changes, change)
	}
	return nil
}

func parseBranchHeader(record string, status *Status) error {
	fields := strings.SplitN(record[2:], " ", 2)
	if len(fields) != 2 {
		return fmt.Errorf("parse Git branch status")
	}
	switch fields[0] {
	case "branch.oid":
		if fields[1] != "(initial)" {
			status.Head = fields[1]
		}
	case "branch.head":
		if fields[1] != "(detached)" {
			status.Branch = fields[1]
		}
	case "branch.upstream":
		status.Upstream = fields[1]
	case "branch.ab":
		counts := strings.Fields(fields[1])
		if len(counts) != 2 {
			return fmt.Errorf("parse Git branch ahead/behind")
		}
		ahead, err := strconv.Atoi(strings.TrimPrefix(counts[0], "+"))
		if err != nil {
			return fmt.Errorf("parse Git branch ahead: %w", err)
		}
		behind, err := strconv.Atoi(strings.TrimPrefix(counts[1], "-"))
		if err != nil {
			return fmt.Errorf("parse Git branch behind: %w", err)
		}
		status.Ahead, status.Behind = ahead, behind
	}
	return nil
}

func statusChange(xy, path string) Change {
	change := Change{Path: path, Managed: profile.IsManagedRepositoryPath(path)}
	if len(xy) == 2 {
		if xy[0] != '.' {
			change.Index = string(xy[0])
		}
		if xy[1] != '.' {
			change.Worktree = string(xy[1])
		}
	}
	return change
}

// SanitizeRemote removes URL-form remote userinfo before display or logging.
func SanitizeRemote(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}
