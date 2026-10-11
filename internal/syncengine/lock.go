package syncengine

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"golang.org/x/sys/unix"
)

var ErrProfileBusy = errors.New("another sync execution holds the profile lock")

// ProfileLock is a Linux advisory flock. The stable lock file is never unlinked;
// process death releases its kernel lock. Coordination does not replace ref checks.
type ProfileLock struct {
	file *os.File
	once sync.Once
	err  error
}

func AcquireProfileLock(stateHome, profileRoot string) (*ProfileLock, error) {
	dir, err := machine.ProfileStateDir(stateHome, profileRoot)
	if err != nil {
		return nil, err
	}
	if err := privateStateDir(stateHome, dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "sync.lock")
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("open profile lock failed")
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("profile lock is not a regular file")
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrProfileBusy
		}
		return nil, err
	}
	return &ProfileLock{file: file}, nil
}

func (l *ProfileLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { l.err = l.file.Close() })
	return l.err
}
