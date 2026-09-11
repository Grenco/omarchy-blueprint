package resources

import (
	"bytes"
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
	if err := validateChangedGitContent(ctx, runner, root); err != nil {
		return GitStateCapture{}, err
	}
	selectedPaths := make(map[string]bool, len(state.Untracked))
	for _, item := range state.Untracked {
		selectedPaths[item] = true
	}
	for _, raw := range selected {
		if _, err := validateSelectedUntracked(root, raw, selectedPaths); err != nil {
			return GitStateCapture{}, err
		}
	}
	return calculateGitWorkingStateOverlay(ctx, runner, root, state, selected)
}

// calculateGitWorkingStateOverlay derives non-persisted Git state without inspecting content for secrets.
func calculateGitWorkingStateOverlay(ctx context.Context, runner command.Runner, root string, state GitWorkingState, selected []string) (GitStateCapture, error) {
	diffConfig := []string{"-c", "diff.external=", "-c", "diff.mnemonicPrefix=false", "-c", "diff.noprefix=false", "-c", "diff.srcPrefix=a/", "-c", "diff.dstPrefix=b/", "-c", "diff.algorithm=myers", "-c", "diff.indentHeuristic=false", "-c", "diff.compactionHeuristic=false", "-c", "diff.context=3", "-c", "diff.interHunkContext=0", "-c", "core.quotePath=true", "-C", root, "diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames"}
	indexArgs := append(append([]string{}, diffConfig...), "--cached", "HEAD", "--")
	index, err := command.RunOutput(ctx, runner, MaxGitStateSize, "git", indexArgs...)
	if err != nil {
		return GitStateCapture{}, fmt.Errorf("capture index patch: %w", err)
	}
	worktreeArgs := append(append([]string{}, diffConfig...), "--")
	worktree, err := command.RunOutput(ctx, runner, MaxGitStateSize-int64(len(index)), "git", worktreeArgs...)
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
		item, err := overlaySelectedUntracked(root, raw, untracked)
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

func validateChangedGitContent(ctx context.Context, runner command.Runner, root string) error {
	status, err := command.RunOutput(ctx, runner, 8<<20, "git", "-C", root, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return fmt.Errorf("inspect Git status for safety scan: %w", err)
	}
	records := strings.Split(string(status), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" || (record[0] != '1' && record[0] != '2') {
			continue
		}
		fields := strings.SplitN(record, " ", 9)
		if len(fields) != 9 || len(fields[1]) != 2 {
			return fmt.Errorf("parse Git status record for safety scan")
		}
		xy, modes, blob, path := fields[1], fields[3:6], fields[7], fields[8]
		if record[0] == '2' {
			if i+1 >= len(records) {
				return fmt.Errorf("parse Git rename status record for safety scan")
			}
			i++ // The following NUL record is the old path, not a desired path.
		}

		if xy[0] != '.' && (modes[0] == "160000" || modes[1] == "160000") {
			return fmt.Errorf("unsupported submodule change: %s", path)
		}
		if xy[1] != '.' && (modes[1] == "160000" || modes[2] == "160000") {
			return fmt.Errorf("unsupported submodule change: %s", path)
		}

		if xy[0] != '.' && xy[0] != 'D' {
			if sensitiveResourcePath(path) {
				return fmt.Errorf("sensitive tracked path: %s", path)
			}
			if modes[1] != "160000" {
				content, err := command.RunOutput(ctx, runner, MaxGitStateSize, "git", "-C", root, "cat-file", "blob", blob)
				if err != nil {
					return fmt.Errorf("read Git index blob %s: %w", path, err)
				}
				if result, err := sensitive.ScanReader(bytes.NewReader(content), MaxGitStateSize); err != nil {
					return fmt.Errorf("scan Git index blob %s: %w", path, err)
				} else if result.Sensitive {
					return fmt.Errorf("sensitive tracked content: %s", path)
				}
			}
		}

		if xy[1] != '.' && xy[1] != 'D' {
			if sensitiveResourcePath(path) {
				return fmt.Errorf("sensitive tracked path: %s", path)
			}
			worktreePath := filepath.Join(root, filepath.FromSlash(path))
			info, err := os.Lstat(worktreePath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("stat Git worktree file %s: %w", path, err)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			result, err := sensitive.ScanRegularFile(worktreePath, MaxGitStateSize)
			if err != nil {
				return fmt.Errorf("scan Git worktree file %s: %w", path, err)
			}
			if result.Sensitive {
				return fmt.Errorf("sensitive tracked content: %s", path)
			}
		}
	}
	return nil
}

func validateSelectedUntracked(root, raw string, untracked map[string]bool) (profile.GitUntrackedFile, error) {
	clean, err := validateSelectedUntrackedPath(raw, untracked)
	if err != nil {
		return profile.GitUntrackedFile{}, err
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

func overlaySelectedUntracked(root, raw string, untracked map[string]bool) (profile.GitUntrackedFile, error) {
	clean, err := validateSelectedUntrackedPath(raw, untracked)
	if err != nil {
		return profile.GitUntrackedFile{}, err
	}
	file, info, err := content.OpenRegularFile(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return profile.GitUntrackedFile{}, err
	}
	defer file.Close()
	if info.Size() > MaxGitUntrackedFileSize {
		return profile.GitUntrackedFile{}, fmt.Errorf("untracked file exceeds %d bytes: %s", MaxGitUntrackedFileSize, clean)
	}
	hash, err := content.HashOpenFile(file)
	if err != nil {
		return profile.GitUntrackedFile{}, err
	}
	return profile.GitUntrackedFile{Path: clean, Hash: hash, Mode: fmt.Sprintf("%04o", info.Mode().Perm())}, nil
}

func validateSelectedUntrackedPath(raw string, untracked map[string]bool) (string, error) {
	clean := pathpkg.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || pathpkg.IsAbs(clean) || strings.Contains("/"+clean+"/", "/.git/") {
		return "", fmt.Errorf("invalid untracked path %q", raw)
	}
	if !untracked[clean] {
		return "", fmt.Errorf("untracked path is not eligible: %s", clean)
	}
	return clean, nil
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
	records := strings.Split(string(status), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		switch record[0] {
		case '1', '2':
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 || len(fields[1]) != 2 {
				return state, true, fmt.Errorf("parse Git status record")
			}
			xy := fields[1]
			if xy[0] != '.' {
				state.StagedTracked++
			}
			if xy[1] != '.' {
				state.UnstagedTracked++
			}
			if strings.HasPrefix(fields[2], "S") && fields[2] != "S..." {
				state.DirtySubmodule = true
			}
			if record[0] == '2' {
				if i+1 >= len(records) {
					return state, true, fmt.Errorf("parse Git rename status record")
				}
				i++ // Porcelain v2 -z emits the rename source as the next NUL record.
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
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			if _, err := os.Stat(path); err == nil {
				state.Operation = probe.name
				break
			}
		}
	}
	return state, true, nil
}
