package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"syscall"
)

// A Resources capture installs its new generation (resources.toml, files/,
// git-state/) in place before the profile metadata describing it is saved,
// keeping the previous generation beside it until it commits. Its marker
// says which way an interrupted capture resolves: "rollback" until the
// profile save that describes it commits, then "committed". That save
// writes "committed" as part of its own commit (SaveOptions), so the
// Resources generation and the metadata always resolve together.
const (
	resourcesCaptureMarker = "resources/.capture-transaction"
	resourcesCaptureLock   = "resources/.capture.lock"
	resourcesRollback      = "rollback"
	resourcesCommitted     = "committed"
)

// ResourcesCaptureComponents are the parts of a Resources generation.
var ResourcesCaptureComponents = []string{"resources.toml", "files", "git-state"}

// RecoverResourcesCapture resolves an interrupted Resources capture in
// profileDir: a committed one is finalized, an uncommitted one rolled back
// to the previous generation, and leftover staging removed. The caller must
// hold the Resources capture lock.
func RecoverResourcesCapture(profileDir string) error {
	root, err := os.OpenRoot(profileDir)
	if err != nil {
		return err
	}
	defer root.Close()
	return recoverResourcesCapture(root)
}

// recoverResourcesIfIdle runs Resources capture recovery when an earlier
// capture left work behind and no capture is running now (one in progress
// holds the lock and resolves its own generation).
func (s *profileStore) recoverResourcesIfIdle() error {
	if !resourcesCaptureLeftovers(s.root) {
		return nil
	}
	lock, err := s.root.OpenFile(resourcesCaptureLock, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return recoverResourcesCapture(s.root)
}

func resourcesCaptureLeftovers(root *os.Root) bool {
	if _, err := root.Lstat(resourcesCaptureMarker); err == nil {
		return true
	}
	names, err := resourcesDirNames(root)
	if err != nil {
		return false
	}
	for _, name := range names {
		if strings.HasPrefix(name, ".capture-stage-") || strings.HasSuffix(name, "-previous") && strings.HasPrefix(name, ".") {
			return true
		}
	}
	return false
}

func resourcesDirNames(root *os.Root) ([]string, error) {
	f, err := root.Open("resources")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}

func resourcesPath(name string) string { return pathpkg.Join("resources", name) }
func resourcesPreviousPath(name string) string {
	return pathpkg.Join("resources", "."+name+"-previous")
}

func recoverResourcesCapture(root *os.Root) error {
	b, err := root.ReadFile(resourcesCaptureMarker)
	if err == nil {
		switch strings.TrimSpace(string(b)) {
		case resourcesRollback:
			return rollbackResourcesCapture(root)
		case resourcesCommitted:
			return finalizeResourcesCapture(root)
		default:
			return fmt.Errorf("invalid resources capture transaction marker")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, name := range ResourcesCaptureComponents {
		target, previous := resourcesPath(name), resourcesPreviousPath(name)
		if _, err := root.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			if _, err := root.Lstat(previous); err == nil {
				if err := root.Rename(previous, target); err != nil {
					return err
				}
			}
		} else if err == nil {
			if err := root.RemoveAll(previous); err != nil {
				return err
			}
		}
	}
	return removeResourcesStages(root)
}

func rollbackResourcesCapture(root *os.Root) error {
	for _, name := range ResourcesCaptureComponents {
		target, previous := resourcesPath(name), resourcesPreviousPath(name)
		if _, err := root.Lstat(previous); err == nil {
			if err := root.RemoveAll(target); err != nil {
				return err
			}
			if err := root.Rename(previous, target); err != nil {
				return err
			}
		} else if errors.Is(err, fs.ErrNotExist) && !resourcesStageHas(root, name) {
			if err := root.RemoveAll(target); err != nil {
				return err
			}
		}
	}
	if err := root.Remove(resourcesCaptureMarker); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeResourcesStages(root)
}

func finalizeResourcesCapture(root *os.Root) error {
	for _, name := range ResourcesCaptureComponents {
		if err := root.RemoveAll(resourcesPreviousPath(name)); err != nil {
			return err
		}
	}
	if err := root.Remove(resourcesCaptureMarker); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeResourcesStages(root)
}

func resourcesStageHas(root *os.Root, name string) bool {
	names, err := resourcesDirNames(root)
	if err != nil {
		return true
	}
	for _, entry := range names {
		if strings.HasPrefix(entry, ".capture-stage-") {
			if _, err := root.Lstat(pathpkg.Join("resources", entry, name)); err == nil {
				return true
			}
		}
	}
	return false
}

func removeResourcesStages(root *os.Root) error {
	names, err := resourcesDirNames(root)
	if err != nil {
		return err
	}
	for _, name := range names {
		if strings.HasPrefix(name, ".capture-stage-") {
			if err := root.RemoveAll(pathpkg.Join("resources", name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckManagedDir verifies that rel (a managed profile directory such as
// "resources") is a real directory inside profileDir, or absent: never a
// symlink. Providers that work in a managed directory by path check it
// first, so a symlink committed into a profile cannot redirect them.
func CheckManagedDir(profileDir, rel string) error {
	info, err := os.Lstat(filepath.Join(profileDir, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("profile path %s is not a directory inside the profile; refusing to use it", rel)
	}
	return nil
}
