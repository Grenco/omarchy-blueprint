package profilegit

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/content"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type DiffFile struct {
	Path     string                  `json:"path"`
	Document inspection.DiffDocument `json:"document"`
	Deleted  bool                    `json:"deleted,omitempty"`
	New      bool                    `json:"new,omitempty"`
}

type Diff struct {
	Files []DiffFile `json:"files"`
}

// Diff inspects changed, Blueprint-managed profile files without modifying Git
// state or checking out historical content.
func (s Service) Diff(ctx context.Context, onlyPath string) (Diff, error) {
	if onlyPath != "" && !profile.IsManagedRepositoryPath(onlyPath) {
		return Diff{}, fmt.Errorf("profile diff path is not a managed canonical profile path: %s", onlyPath)
	}
	status, err := s.Status(ctx)
	if err != nil {
		return Diff{}, err
	}
	if !status.Repository {
		return Diff{}, nil
	}
	files := make([]DiffFile, 0)
	for _, change := range status.Changes {
		path := change.Path
		if !change.Managed && change.OriginalManaged {
			path = change.OriginalPath
		}
		if !profile.IsManagedRepositoryPath(path) || (onlyPath != "" && path != onlyPath) {
			continue
		}
		file, err := s.diffFile(ctx, path, status.Head == "")
		if err != nil {
			return Diff{}, err
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Diff{Files: files}, nil
}

func (s Service) diffFile(ctx context.Context, path string, unborn bool) (DiffFile, error) {
	worktree, exists, regular, mode, err := readWorktree(filepath.Join(s.Root, filepath.FromSlash(path)))
	if err != nil {
		return DiffFile{}, fmt.Errorf("read profile diff %s: %w", path, err)
	}
	file := DiffFile{Path: path, Deleted: !exists, New: unborn || !s.headPathExists(ctx, path)}
	if !regular && exists {
		file.Document = inspection.DiffDocument{Kind: inspection.DiffMetadata, OldLabel: "HEAD/" + path, NewLabel: "worktree/" + path, Metadata: []inspection.DiffFact{{Key: "worktree-kind", Value: mode}}}
		return file, nil
	}
	old := []byte(nil)
	headMode := ""
	if !unborn && !file.New {
		headMode, err = s.headMode(ctx, path)
		if err != nil {
			file.Document = inspection.DiffDocument{Kind: inspection.DiffUnavailable, OldLabel: "HEAD/" + path, NewLabel: "worktree/" + path, Metadata: []inspection.DiffFact{{Key: "reason", Value: "historical object metadata is unavailable"}}}
			return file, nil
		}
		if !regularGitMode(headMode) {
			file.Document = inspection.DiffDocument{Kind: inspection.DiffMetadata, OldLabel: "HEAD/" + path, NewLabel: "worktree/" + path, Metadata: []inspection.DiffFact{{Key: "head-kind", Value: gitObjectKind(headMode)}}}
			return file, nil
		}
		old, err = command.RunOutput(ctx, s.Runner, maxDiffOutput, "git", "-C", s.Root, "show", "HEAD:"+path)
		if err != nil {
			kind, fact := inspection.DiffUnavailable, inspection.DiffFact{Key: "reason", Value: "historical content is unavailable"}
			if strings.Contains(err.Error(), "output exceeds") {
				kind, fact = inspection.DiffTooLarge, inspection.DiffFact{Key: "limit", Value: "historical content exceeds preview limit"}
			}
			file.Document = inspection.DiffDocument{Kind: kind, OldLabel: "HEAD/" + path, NewLabel: "worktree/" + path, Metadata: []inspection.DiffFact{fact}}
			return file, nil
		}
	}
	file.Document = inspection.BuildTextDiff("HEAD/"+path, old, "worktree/"+path, worktree)
	if file.Document.Kind == inspection.DiffMetadata && regular && !unborn && !file.New {
		if headMode != mode {
			file.Document.Metadata = []inspection.DiffFact{{Key: "old-mode", Value: headMode}, {Key: "new-mode", Value: mode}}
		}
	}
	return file, nil
}

func regularGitMode(mode string) bool { return mode == "100644" || mode == "100755" }

func gitObjectKind(mode string) string {
	if mode == "120000" {
		return "symlink"
	}
	return "special"
}

func (s Service) headPathExists(ctx context.Context, path string) bool {
	_, err := s.Runner.Run(ctx, "git", "-C", s.Root, "cat-file", "-e", "HEAD:"+path)
	return err == nil
}

func (s Service) headMode(ctx context.Context, path string) (string, error) {
	out, err := s.Runner.Run(ctx, "git", "-C", s.Root, "ls-tree", "HEAD", "--", path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("HEAD path is missing")
	}
	mode, err := strconv.ParseUint(fields[0], 8, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04o", mode), nil
}

func readWorktree(path string) ([]byte, bool, bool, string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, false, "missing", nil
	}
	if err != nil {
		return nil, false, false, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, true, false, "symlink", nil
	}
	if !info.Mode().IsRegular() {
		return nil, true, false, info.Mode().Type().String(), nil
	}
	f, _, err := content.OpenRegularFile(path)
	if err != nil {
		return nil, true, true, "regular", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, inspection.MaxPreviewBytes+1))
	return data, true, true, fmt.Sprintf("%04o", info.Mode().Perm()), err
}
