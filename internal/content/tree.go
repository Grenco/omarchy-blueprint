package content

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// HashRegularTree returns the canonical hash for a regular file or directory tree.
// Symlinks and special files are rejected so callers can safely copy the result.
func HashRegularTree(root string) (string, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("tree root is a symlink: %s", root)
	}

	hash := sha256.New()
	if rootInfo.Mode().IsRegular() {
		if err := hashTreeFile(hash, root, "root", rootInfo.Mode()); err != nil {
			return "", err
		}
		return fmt.Sprintf("%x", hash.Sum(nil)), nil
	}
	if !rootInfo.IsDir() {
		return "", fmt.Errorf("tree contains unsupported file: %s", root)
	}
	if _, err := fmt.Fprintf(hash, "root-dir\x00%04o\x00", rootInfo.Mode().Perm()); err != nil {
		return "", err
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("tree contains symlink: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if info.IsDir() {
			_, err := fmt.Fprintf(hash, "dir\x00%s\x00%04o\x00", name, info.Mode().Perm())
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("tree contains unsupported file: %s", relative)
		}
		return hashTreeFile(hash, path, name, info.Mode())
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func hashTreeFile(hash io.Writer, path, relative string, mode os.FileMode) error {
	if _, err := fmt.Fprintf(hash, "file\x00%s\x00%04o\x00", filepath.ToSlash(relative), mode.Perm()); err != nil {
		return err
	}
	file, info, err := OpenRegularFile(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("tree file changed while opening: %s", path)
	}
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	_, err = hash.Write([]byte{0})
	return err
}
