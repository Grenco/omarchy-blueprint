package content

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// HashRegularFile returns the SHA-256 digest of a regular, non-symlink file.
func HashRegularFile(path string) (string, error) {
	f, _, err := OpenRegularFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return HashOpenFile(f)
}

// OpenRegularFile opens path only when it is a regular, non-symlink file.
func OpenRegularFile(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("path is a symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		f.Close()
		return nil, nil, fmt.Errorf("path changed while opening: %s", path)
	}
	return f, opened, nil
}

// HashOpenFile returns the SHA-256 digest for an already-open regular file.
func HashOpenFile(f *os.File) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
