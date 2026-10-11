// Package syncprofile defines disposable composition inputs shared by providers
// and workflow, without importing provider packages or mutation orchestration.
package syncprofile

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type View struct {
	Root    string
	Profile profile.Data
}

// Builder owns its private temporary root. It cannot be constructed around an
// existing checkout; provider overlays can only write into this owned tree.
type Builder struct {
	root    string
	Profile profile.Data
}

func NewBuilder(data profile.Data) (*Builder, error) {
	root, err := os.MkdirTemp("", "blueprint-sync-compose-*")
	if err != nil {
		return nil, err
	}
	return &Builder{root: root, Profile: profile.CloneData(data)}, nil
}
func (b *Builder) Root() string { return b.root }
func (b *Builder) Close() {
	if b.root != "" {
		_ = os.RemoveAll(b.root)
		b.root = ""
	}
}

func safePath(root, rel string) (string, error) {
	if root == "" || !profile.IsManagedRepositoryPath(rel) {
		return "", fmt.Errorf("sync: invalid managed artifact path")
	}
	path := root
	for _, part := range strings.Split(rel, "/") {
		path = filepath.Join(path, part)
		st, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("sync: artifact path crosses a symbolic link")
		}
	}
	return path, nil
}

func (b *Builder) RemoveArtifact(rel string) error {
	path, err := safePath(b.root, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// ReplaceArtifact refuses links and special files, retains regular mode bits,
// and checks cancellation while copying. Any failure discards the whole stage.
func (b *Builder) ReplaceArtifact(ctx context.Context, sourceRoot, rel string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := safePath(sourceRoot, rel)
	if err != nil {
		return err
	}
	dest, err := safePath(b.root, rel)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(source); err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		sub, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := dest
		if sub != "." {
			target = filepath.Join(dest, sub)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("sync: unsupported artifact file type")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, &cancelReader{ctx: ctx, r: in})
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
