package profilegit

import (
	"archive/tar"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/command"
)

// Profiles contain manifests and captured artifacts, not full machine images.
// 256 MiB / 100,000 entries is a conservative v1 snapshot envelope; larger
// profiles fail explicitly rather than allocating unbounded temporary state.
const maxArchiveBytes int64 = 256 << 20
const maxArchiveEntries = 100000

type archiveBlob struct {
	name, mode, hash string
	size             int64
}

func (s Service) archiveTree(ctx context.Context, sha string) ([]archiveBlob, error) {
	out, err := command.RunOutput(ctx, s.Runner, maxStatusOutput, "git", "-C", s.Root, "ls-tree", "-r", "-l", "-z", sha)
	if err != nil {
		return nil, revisionError(ctx, "inspect snapshot tree", err)
	}
	result := []archiveBlob{}
	var total int64
	records := strings.Split(string(out), "\x00")
	for _, record := range records {
		if record == "" {
			continue
		}
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 4 || fields[1] != "blob" || !exactCommitID.MatchString(fields[2]) {
			return nil, errors.New("unsupported snapshot tree entry")
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("invalid snapshot blob size")
		}
		if _, err := safeArchiveName(name); err != nil {
			return nil, err
		}
		if fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" {
			return nil, errors.New("unsupported snapshot blob mode")
		}
		if size > maxArchiveBytes || total > maxArchiveBytes-size-1024 {
			return nil, errors.New("snapshot exceeds archive size limit")
		}
		total += size + 1024
		result = append(result, archiveBlob{name: name, mode: fields[0], hash: fields[2], size: size})
		if len(result) > maxArchiveEntries {
			return nil, errors.New("snapshot exceeds entry limit")
		}
	}
	return result, nil
}

// MaterializeCommit reads local objects only, into an existing empty private
// destination outside the active repository. Neither HEAD nor index is changed.
func (s Service) MaterializeCommit(ctx context.Context, sha, dest string) (resultErr error) {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !exactCommitID.MatchString(sha) {
		return errors.New("snapshot requires an exact lowercase SHA-1")
	}
	resolved, err := s.ResolveCommit(ctx, sha)
	if err != nil {
		return err
	}
	if resolved != sha {
		return errors.New("snapshot identity changed")
	}
	dest, err = filepath.Abs(dest)
	if err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	if canonical != dest {
		return errors.New("snapshot destination contains a symlink")
	}
	rel, err := filepath.Rel(s.Root, dest)
	if err != nil {
		return err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("snapshot destination overlaps active repository")
	}
	info, err := os.Lstat(dest)
	if err != nil || !info.IsDir() {
		return errors.New("snapshot destination must be a directory")
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("snapshot destination must be empty")
	}
	blobs, err := s.archiveTree(ctx, sha)
	if err != nil {
		return err
	}
	if err := os.Chmod(dest, 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp("", "blueprint-profile-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Close(); err != nil {
		return err
	}
	_, err = s.Runner.Run(ctx, "git", "-c", "tar.umask=0022", "-C", s.Root, "archive", "--format=tar", "--output="+temp.Name(), sha)
	if err != nil {
		return revisionError(ctx, "archive profile revision", err)
	}
	archive, err := os.Open(temp.Name())
	if err != nil {
		return err
	}
	defer archive.Close()
	stat, err := archive.Stat()
	if err != nil {
		return err
	}
	if stat.Size() > maxArchiveBytes {
		return errors.New("profile archive exceeds size limit")
	}
	// Destination was empty and caller-owned; remove only this operation's output on failure.
	defer func() {
		if resultErr != nil {
			items, _ := os.ReadDir(dest)
			for _, item := range items {
				if err := os.RemoveAll(filepath.Join(dest, item.Name())); err != nil {
					resultErr = errors.Join(resultErr, errors.New("snapshot cleanup failed"))
				}
			}
		}
	}()
	if err := extractProfileArchive(ctx, archive, dest); err != nil {
		return err
	}
	if err := verifyArchiveBlobs(ctx, dest, blobs); err != nil {
		return err
	}
	return ctx.Err()
}

func safeArchiveName(name string) (string, error) {
	clean := path.Clean(name)
	if name == "" || path.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsRune(name, 0) {
		return "", errors.New("unsafe profile archive path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.EqualFold(part, ".git") {
			return "", errors.New("unsafe profile archive path")
		}
	}
	return clean, nil
}

func archiveParents(dest, name string) error {
	parent := path.Dir(name)
	if parent == "." {
		return nil
	}
	current := dest
	for _, part := range strings.Split(parent, "/") {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("archive parent is not a directory")
		}
	}
	return nil
}

func extractProfileArchive(ctx context.Context, reader io.Reader, dest string) error {
	tr := tar.NewReader(io.LimitReader(reader, maxArchiveBytes+1))
	seen := map[string]bool{}
	links := []string{}
	var total int64
	count := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h, err := tr.Next()
		if err == io.EOF {
			for _, name := range links {
				if err := validateSnapshotLink(ctx, dest, name); err != nil {
					return err
				}
			}
			return nil
		}
		if err != nil {
			return errors.New("invalid profile archive")
		}
		count++
		if count > maxArchiveEntries {
			return errors.New("archive exceeds entry limit")
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			if len(h.PAXRecords) != 1 || !exactCommitID.MatchString(h.PAXRecords["comment"]) {
				return errors.New("unsupported archive global metadata")
			}
			continue
		}
		name, err := safeArchiveName(h.Name)
		if err != nil {
			return err
		}
		if seen[name] {
			return errors.New("duplicate profile archive entry")
		}
		seen[name] = true
		if h.Size < 0 || h.Size > maxArchiveBytes || total > maxArchiveBytes-h.Size {
			return errors.New("archive exceeds extracted size limit")
		}
		total += h.Size
		if err := archiveParents(dest, name); err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(target, 0700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err := os.Lstat(target)
			if err != nil || !info.IsDir() {
				return errors.New("archive directory conflict")
			}
		case tar.TypeReg, tar.TypeRegA:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(f, contextArchiveReader{ctx: ctx, reader: tr}, h.Size)
			modeErr := f.Chmod(os.FileMode(h.Mode) & 0777)
			closeErr := f.Close()
			if copyErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return errors.New("incomplete profile archive file")
			}
			if modeErr != nil {
				return modeErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if h.Linkname == "" || path.IsAbs(h.Linkname) || strings.ContainsRune(h.Linkname, 0) {
				return errors.New("unsafe archive symlink")
			}
			if _, err := safeArchiveName(path.Join(path.Dir(name), h.Linkname)); err != nil {
				return errors.New("archive symlink escapes snapshot")
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
			links = append(links, name)
		default:
			return errors.New("unsupported profile archive entry type")
		}
	}
}

// Resolve components as the kernel does: lexical cleaning alone is unsafe when
// a preceding symlink changes what a later '..' means. Dangling in-root links
// are retained, but escapes and cycles fail before any profile loader runs.
func validateSnapshotLink(ctx context.Context, root, name string) error {
	pending := strings.Split(name, "/")
	stack := []string{}
	follows := 0
	for len(pending) > 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		part := pending[0]
		pending = pending[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(stack) == 0 {
				return errors.New("archive symlink chain escapes snapshot")
			}
			stack = stack[:len(stack)-1]
			continue
		}
		candidate := filepath.Join(append([]string{root}, append(stack, part)...)...)
		info, err := os.Lstat(candidate)
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrInvalid) {
			return errors.New("inspect snapshot symlink failed")
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			follows++
			if follows > 40 {
				return errors.New("archive symlink cycle or excessive chain")
			}
			target, err := os.Readlink(candidate)
			if err != nil || path.IsAbs(target) {
				return errors.New("unsafe snapshot symlink")
			}
			pending = append(strings.Split(target, "/"), pending...)
		} else {
			stack = append(stack, part)
		}
	}
	return nil
}

type contextArchiveReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextArchiveReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	return r.reader.Read(p)
}

// Git archive attributes may omit/substitute content. Verify the materialized
// blobs against the exact tree and fail closed instead of loading altered facts.
func verifyArchiveBlobs(ctx context.Context, dest string, blobs []archiveBlob) error {
	for _, blob := range blobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		target := filepath.Join(dest, filepath.FromSlash(blob.name))
		info, err := os.Lstat(target)
		if err != nil {
			return errors.New("archive did not preserve the exact commit tree")
		}
		hasher := sha1.New()
		fmt.Fprintf(hasher, "blob %d\x00", blob.size)
		if blob.mode == "120000" {
			if info.Mode()&os.ModeSymlink == 0 {
				return errors.New("snapshot symlink mode changed")
			}
			link, err := os.Readlink(target)
			if err != nil {
				return err
			}
			if int64(len(link)) != blob.size {
				return errors.New("snapshot symlink size changed")
			}
			io.WriteString(hasher, link)
		} else {
			perm := os.FileMode(0644)
			if blob.mode == "100755" {
				perm = 0755
			}
			if !info.Mode().IsRegular() || info.Size() != blob.size || info.Mode().Perm() != perm {
				return errors.New("snapshot file metadata changed")
			}
			f, err := os.Open(target)
			if err != nil {
				return err
			}
			_, err = io.Copy(hasher, contextArchiveReader{ctx: ctx, reader: f})
			f.Close()
			if err != nil {
				return err
			}
		}
		if fmt.Sprintf("%x", hasher.Sum(nil)) != blob.hash {
			return errors.New("archive did not preserve exact commit content")
		}
	}
	return nil
}
