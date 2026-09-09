package content

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// HashFilesystemObject hashes a tree without following symlinks.
func HashFilesystemObject(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintf(h, "%s\x00%s\x00%o\x00", rel, objectType(info), info.Mode().Perm())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(h, "%s\x00", target)
		} else if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return "", err
			}
			if closeErr != nil {
				return "", closeErr
			}
		} else if !info.IsDir() {
			return "", fmt.Errorf("unsupported filesystem object: %s", path)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func objectType(info os.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "directory"
	}
	if info.Mode().IsRegular() {
		return "file"
	}
	return "other"
}
