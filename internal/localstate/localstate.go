// Package localstate holds the private, profile-keyed machine-local state
// primitives shared by the Sync cursor, the profile lock and Auto Sync. The
// caller-provided state home is trusted; Blueprint-created descendants must be
// private directories, never symlink aliases to another profile's state.
package localstate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// ErrNotDirectory reports a state path component that is not a real
// directory (for example a symlink alias).
var ErrNotDirectory = errors.New("local state directory is not a directory")

// ErrUntrusted reports state that is not owned by this user or that other
// users could write (or, for a private file, read).
var ErrUntrusted = errors.New("local state is not private to this user")

// currentUID is replaceable in tests to simulate foreign ownership.
var currentUID = os.Geteuid

func owner(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

// trustedDir requires a directory owned by this user that no other user can
// write.
func trustedDir(info os.FileInfo) bool {
	uid, ok := owner(info)
	return ok && int(uid) == currentUID() && info.Mode().Perm()&0o022 == 0
}

// InspectPrivateDir is InspectDir plus trust: every component below home
// must be owned by this user and not group- or world-writable. It never
// creates or repairs anything. Authority-bearing state (the Auto Sync
// grant) uses it; the Sync cursor keeps InspectDir for compatibility.
func InspectPrivateDir(home, dir string) error {
	root, rel, err := relative(home, dir)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrNotDirectory
		}
		if !trustedDir(info) {
			return ErrUntrusted
		}
	}
	return nil
}

// ReadPrivateFile is ReadFile for authority-bearing state: the file must
// also be owned by this user with no group or world permissions.
func ReadPrivateFile(path string, limit int64) ([]byte, error) {
	return readFile(path, limit, true)
}

// InspectDir checks dir below home without creating missing state or
// changing permissions. A missing component returns os.ErrNotExist.
func InspectDir(home, dir string) error {
	root, rel, err := relative(home, dir)
	if err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrNotDirectory
		}
	}
	return nil
}

// PrivateDir creates dir below home as 0700 directories, refusing any
// component that is not a real directory.
func PrivateDir(home, dir string) error {
	root, rel, err := relative(home, dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrNotDirectory
		}
		if err := os.Chmod(current, 0700); err != nil {
			return err
		}
	}
	return nil
}

// SyncDir flushes a directory entry change to disk.
func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// ReadFile reads a private state file without following a symlink at the
// file itself, accepting only a regular file of at most limit bytes. Error
// messages never contain file contents. A missing file returns
// os.ErrNotExist.
func ReadFile(path string, limit int64) ([]byte, error) {
	return readFile(path, limit, false)
}

func readFile(path string, limit int64, private bool) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, errors.New("open local state failed")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("local state is not a regular file")
	}
	if private {
		uid, ok := owner(info)
		if !ok || int(uid) != currentUID() || info.Mode().Perm()&0o077 != 0 {
			return nil, ErrUntrusted
		}
	}
	bytes, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errors.New("read local state failed")
	}
	if int64(len(bytes)) > limit {
		return nil, errors.New("local state exceeds size limit")
	}
	return bytes, nil
}

// WriteFile atomically replaces path with a private 0600 file whose bytes
// are flushed before the rename and whose directory entry is flushed after.
// The parent directory must already be private (PrivateDir).
func WriteFile(path string, bytes []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0600); err != nil {
		return err
	}
	if _, err := temp.Write(bytes); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	return SyncDir(dir)
}

// RemoveFile removes path if present, inspecting (never creating) its state
// directory below home first.
func RemoveFile(home, path string) error {
	if err := InspectDir(home, filepath.Dir(path)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return SyncDir(filepath.Dir(path))
}

func relative(home, dir string) (root, rel string, err error) {
	root, err = filepath.Abs(home)
	if err != nil {
		return "", "", err
	}
	target, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	rel, err = filepath.Rel(root, target)
	if err != nil {
		return "", "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("local state directory is outside the state home")
	}
	return root, rel, nil
}
