package resources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/sensitive"
)

const MaxGitUntrackedFileSize int64 = 16 << 20
const MaxGitStateSize int64 = 128 << 20

type GitStateCapture struct {
	IndexPatch, WorktreePatch         []byte
	IndexPatchHash, WorktreePatchHash string
	Untracked                         []profile.GitUntrackedFile
	TotalBytes                        int64
}

type GitWorkingState struct {
	Remote          string
	Branch          string
	Revision        string
	StagedTracked   int
	UnstagedTracked int
	Untracked       []string
	Conflicted      bool
	Operation       string
	DirtySubmodule  bool
}

func CaptureGitWorkingState(ctx context.Context, runner command.Runner, root string, selected []string) (GitStateCapture, error) {
	state, ok, err := InspectGitWorkingState(ctx, runner, root)
	if err != nil {
		return GitStateCapture{}, err
	}
	if !ok {
		return GitStateCapture{}, fmt.Errorf("not a Git worktree: %s", root)
	}
	if state.Conflicted || state.Operation != "" || state.DirtySubmodule {
		return GitStateCapture{}, fmt.Errorf("Git working state cannot be captured safely")
	}
	index, err := command.RunOutput(ctx, runner, MaxGitStateSize, "git", "-C", root, "diff", "--cached", "HEAD", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--")
	if err != nil {
		return GitStateCapture{}, fmt.Errorf("capture index patch: %w", err)
	}
	worktree, err := command.RunOutput(ctx, runner, MaxGitStateSize-int64(len(index)), "git", "-C", root, "diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--")
	if err != nil {
		return GitStateCapture{}, fmt.Errorf("capture worktree patch: %w", err)
	}
	result := GitStateCapture{IndexPatch: index, WorktreePatch: worktree, TotalBytes: int64(len(index) + len(worktree))}
	if len(index) > 0 {
		result.IndexPatchHash = fmt.Sprintf("%x", sha256.Sum256(index))
	}
	if len(worktree) > 0 {
		result.WorktreePatchHash = fmt.Sprintf("%x", sha256.Sum256(worktree))
	}
	untracked := make(map[string]bool, len(state.Untracked))
	for _, item := range state.Untracked {
		untracked[item] = true
	}
	for _, raw := range selected {
		item, err := validateSelectedUntracked(root, raw, untracked)
		if err != nil {
			return GitStateCapture{}, err
		}
		result.TotalBytes += itemSize(root, item.Path)
		if result.TotalBytes > MaxGitStateSize {
			return GitStateCapture{}, fmt.Errorf("Git state exceeds %d bytes", MaxGitStateSize)
		}
		result.Untracked = append(result.Untracked, item)
	}
	sort.Slice(result.Untracked, func(i, j int) bool { return result.Untracked[i].Path < result.Untracked[j].Path })
	return result, nil
}

func validateSelectedUntracked(root, raw string, untracked map[string]bool) (profile.GitUntrackedFile, error) {
	clean := pathpkg.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || pathpkg.IsAbs(clean) || strings.Contains("/"+clean+"/", "/.git/") {
		return profile.GitUntrackedFile{}, fmt.Errorf("invalid untracked path %q", raw)
	}
	if !untracked[clean] {
		return profile.GitUntrackedFile{}, fmt.Errorf("untracked path is not eligible: %s", clean)
	}
	if sensitiveResourcePath(clean) {
		return profile.GitUntrackedFile{}, fmt.Errorf("sensitive resource path: %s", clean)
	}
	file, info, err := content.OpenRegularFile(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return profile.GitUntrackedFile{}, err
	}
	defer file.Close()
	if info.Size() > MaxGitUntrackedFileSize {
		return profile.GitUntrackedFile{}, fmt.Errorf("untracked file exceeds %d bytes: %s", MaxGitUntrackedFileSize, clean)
	}
	result, err := sensitive.ScanRegularFile(filepath.Join(root, filepath.FromSlash(clean)), MaxGitUntrackedFileSize)
	if err != nil {
		return profile.GitUntrackedFile{}, err
	}
	if result.Sensitive {
		return profile.GitUntrackedFile{}, fmt.Errorf("sensitive untracked content: %s", clean)
	}
	hash, err := content.HashOpenFile(file)
	if err != nil {
		return profile.GitUntrackedFile{}, err
	}
	return profile.GitUntrackedFile{Path: clean, Hash: hash, Mode: fmt.Sprintf("%04o", info.Mode().Perm())}, nil
}

func itemSize(root, path string) int64 {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return MaxGitStateSize + 1
	}
	return info.Size()
}

func InspectGitWorkingState(ctx context.Context, runner command.Runner, root string) (GitWorkingState, bool, error) {
	top, err := runner.Run(ctx, "git", "-C", root, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(strings.TrimSpace(top)) != filepath.Clean(root) {
		return GitWorkingState{}, false, nil
	}
	state := GitWorkingState{}
	remote, err := runner.Run(ctx, "git", "-C", root, "remote", "get-url", "origin")
	if err != nil {
		return state, true, fmt.Errorf("read Git origin: %w", err)
	}
	if state.Remote, err = PortableGitRemote(strings.TrimSpace(remote)); err != nil {
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
	if branch, err := runner.Run(ctx, "git", "-C", root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		state.Branch = strings.TrimSpace(branch)
	}
	status, err := command.RunOutput(ctx, runner, 8<<20, "git", "-C", root, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return state, true, fmt.Errorf("inspect Git status: %w", err)
	}
	for _, record := range strings.Split(string(status), "\x00") {
		if record == "" {
			continue
		}
		fields := strings.Fields(record)
		switch record[0] {
		case '1', '2':
			if len(fields) < 3 {
				return state, true, fmt.Errorf("parse Git status record")
			}
			xy := fields[1]
			if xy[0] != '.' {
				state.StagedTracked++
			}
			if xy[1] != '.' {
				state.UnstagedTracked++
			}
			if len(fields) > 2 && strings.HasPrefix(fields[2], "S") && fields[2] != "S..." {
				state.DirtySubmodule = true
			}
		case '?':
			state.Untracked = append(state.Untracked, strings.TrimPrefix(record, "? "))
		case 'u':
			state.Conflicted = true
		}
	}
	sort.Strings(state.Untracked)
	for _, probe := range []struct{ path, name string }{{"MERGE_HEAD", "merge"}, {"rebase-merge", "rebase"}, {"rebase-apply", "rebase"}, {"CHERRY_PICK_HEAD", "cherry-pick"}, {"REVERT_HEAD", "revert"}} {
		path, err := runner.Run(ctx, "git", "-C", root, "rev-parse", "--git-path", probe.path)
		if err == nil {
			if _, err := os.Stat(strings.TrimSpace(path)); err == nil {
				state.Operation = probe.name
				break
			}
		}
	}
	return state, true, nil
}
