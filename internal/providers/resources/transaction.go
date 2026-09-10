package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type PreparedCapture struct {
	State   profile.Resources
	Changes []model.Change

	parent string
	stage  string
	lock   *os.File
}

var captureComponents = []string{"resources.toml", "files", "git-state"}
var renameCapturePath = os.Rename

func prepareCapture(parent string, state profile.Resources, changes []model.Change) (*PreparedCapture, error) {
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(parent, ".capture.lock"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("resources capture already in progress: %w", err)
	}
	c := &PreparedCapture{State: state, Changes: changes, parent: parent, lock: lock}
	if err := c.recover(); err != nil {
		c.close()
		return nil, err
	}
	stage, err := os.MkdirTemp(parent, ".capture-stage-*")
	if err != nil {
		c.close()
		return nil, err
	}
	c.stage = stage
	if err := os.WriteFile(filepath.Join(stage, "resources.toml"), nil, 0o644); err != nil {
		_ = os.RemoveAll(stage)
		c.close()
		return nil, err
	}
	for _, name := range captureComponents[1:] {
		if err := os.MkdirAll(filepath.Join(stage, name), 0o755); err != nil {
			_ = os.RemoveAll(stage)
			c.close()
			return nil, err
		}
	}
	return c, nil
}

func (c *PreparedCapture) stagePath(name string) string { return filepath.Join(c.stage, name) }

func (c *PreparedCapture) Install() error {
	if c.stage == "" {
		return fmt.Errorf("resources capture is not prepared")
	}
	if err := c.writeMarker("rollback"); err != nil {
		_ = os.RemoveAll(c.stage)
		c.close()
		return err
	}
	for _, name := range captureComponents {
		target := filepath.Join(c.parent, name)
		previous := filepath.Join(c.parent, "."+name+"-previous")
		if err := os.RemoveAll(previous); err != nil {
			_ = c.Rollback()
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			if err := renameCapturePath(target, previous); err != nil {
				_ = c.Rollback()
				return err
			}
		} else if !os.IsNotExist(err) {
			_ = c.Rollback()
			return err
		}
		if err := renameCapturePath(c.stagePath(name), target); err != nil {
			_ = c.Rollback()
			return err
		}
	}
	return nil
}

// Commit marks an installed generation durable after its profile metadata was saved.
func (c *PreparedCapture) Commit() error {
	if c.stage == "" {
		return fmt.Errorf("resources capture is not prepared")
	}
	return c.writeMarker("committed")
}

func (c *PreparedCapture) Rollback() error {
	if c.parent == "" {
		return nil
	}
	var first error
	for _, name := range captureComponents {
		target := filepath.Join(c.parent, name)
		previous := filepath.Join(c.parent, "."+name+"-previous")
		staged := c.stagePath(name)
		if _, err := os.Lstat(previous); err == nil {
			if err := os.RemoveAll(target); err != nil && first == nil {
				first = err
			}
			if err := renameCapturePath(previous, target); err != nil && first == nil {
				first = err
			}
		} else if os.IsNotExist(err) {
			// A missing staged component was installed over a previously absent one.
			if _, stageErr := os.Lstat(staged); os.IsNotExist(stageErr) {
				if err := os.RemoveAll(target); err != nil && first == nil {
					first = err
				}
			}
		} else if first == nil {
			first = err
		}
	}
	if err := os.RemoveAll(c.stage); err != nil && first == nil {
		first = err
	}
	if err := os.Remove(filepath.Join(c.parent, ".capture-transaction")); err != nil && !os.IsNotExist(err) && first == nil {
		first = err
	}
	c.close()
	return first
}

func (c *PreparedCapture) Finalize() error {
	var first error
	for _, name := range captureComponents {
		if err := os.RemoveAll(filepath.Join(c.parent, "."+name+"-previous")); err != nil && first == nil {
			first = err
		}
	}
	if err := os.RemoveAll(c.stage); err != nil && first == nil {
		first = err
	}
	if err := os.Remove(filepath.Join(c.parent, ".capture-transaction")); err != nil && !os.IsNotExist(err) && first == nil {
		first = err
	}
	c.close()
	return first
}

func (c *PreparedCapture) recover() error {
	marker := filepath.Join(c.parent, ".capture-transaction")
	b, err := os.ReadFile(marker)
	if err == nil {
		switch strings.TrimSpace(string(b)) {
		case "rollback":
			return c.rollbackStale()
		case "committed":
			return c.finalizeStale()
		default:
			return fmt.Errorf("invalid resources capture transaction marker")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, name := range captureComponents {
		target, previous := filepath.Join(c.parent, name), filepath.Join(c.parent, "."+name+"-previous")
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			if _, err := os.Lstat(previous); err == nil {
				if err := renameCapturePath(previous, target); err != nil {
					return err
				}
			}
		} else if err == nil {
			if err := os.RemoveAll(previous); err != nil {
				return err
			}
		}
	}
	return c.removeStaleStages()
}

func (c *PreparedCapture) rollbackStale() error {
	for _, name := range captureComponents {
		target, previous := filepath.Join(c.parent, name), filepath.Join(c.parent, "."+name+"-previous")
		if _, err := os.Lstat(previous); err == nil {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			if err := renameCapturePath(previous, target); err != nil {
				return err
			}
		} else if os.IsNotExist(err) && !c.staleStageHas(name) {
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(filepath.Join(c.parent, ".capture-transaction")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return c.removeStaleStages()
}

func (c *PreparedCapture) staleStageHas(name string) bool {
	entries, err := os.ReadDir(c.parent)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".capture-stage-") {
			if _, err := os.Lstat(filepath.Join(c.parent, entry.Name(), name)); err == nil {
				return true
			}
		}
	}
	return false
}

func (c *PreparedCapture) finalizeStale() error {
	for _, name := range captureComponents {
		if err := os.RemoveAll(filepath.Join(c.parent, "."+name+"-previous")); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(c.parent, ".capture-transaction")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return c.removeStaleStages()
}

func (c *PreparedCapture) removeStaleStages() error {
	entries, err := os.ReadDir(c.parent)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".capture-stage-") {
			if err := os.RemoveAll(filepath.Join(c.parent, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *PreparedCapture) writeMarker(phase string) error {
	temp, err := os.CreateTemp(c.parent, ".capture-transaction-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.WriteString(phase + "\n"); err == nil {
		err = temp.Close()
	} else {
		_ = temp.Close()
	}
	if err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, filepath.Join(c.parent, ".capture-transaction"))
}

func (c *PreparedCapture) close() {
	if c.lock != nil {
		_ = syscall.Flock(int(c.lock.Fd()), syscall.LOCK_UN)
		_ = c.lock.Close()
		c.lock = nil
	}
}
