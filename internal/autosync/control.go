package autosync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Grenco/omarchy-blueprint/internal/localstate"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
)

const selectionFile = "auto-sync-profile.json"

// SelectionStore records the one profile the user timer checks. It lives in
// private state, never in the unit, so the unit carries no profile path or
// remote.
type SelectionStore struct {
	StateHome string
}

type selection struct {
	Version     int    `json:"version"`
	ProfileRoot string `json:"profile_root"`
}

func (s SelectionStore) dir() string { return filepath.Join(s.StateHome, "omarchy-blueprint") }

// Load returns the selected canonical profile root. An untrusted or
// malformed selection is an error: the scheduled check then does nothing.
func (s SelectionStore) Load() (string, bool, error) {
	if err := localstate.InspectPrivateDir(s.StateHome, s.dir()); errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	} else if err != nil {
		return "", false, ErrInvalidGrant
	}
	data, err := localstate.ReadPrivateFile(filepath.Join(s.dir(), selectionFile), 4<<10)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	var sel selection
	if err != nil || json.Unmarshal(data, &sel) != nil || sel.Version != 1 || !filepath.IsAbs(sel.ProfileRoot) || filepath.Clean(sel.ProfileRoot) != sel.ProfileRoot {
		return "", false, ErrInvalidGrant
	}
	return sel.ProfileRoot, true, nil
}

// Save selects root.
func (s SelectionStore) Save(root string) error {
	if err := localstate.PrivateDir(s.StateHome, s.dir()); err != nil {
		return err
	}
	data, err := json.Marshal(selection{Version: 1, ProfileRoot: root})
	if err != nil {
		return err
	}
	return localstate.WriteFile(filepath.Join(s.dir(), selectionFile), data)
}

// Clear removes the selection.
func (s SelectionStore) Clear() error {
	return localstate.RemoveFile(s.StateHome, filepath.Join(s.dir(), selectionFile))
}

// Control turns Auto Sync on, pauses, resumes and turns it off for one
// profile, keeping the grant, the selected profile and the timer in step.
// The grant always changes first when permission shrinks, and last when it
// grows, so a failure never leaves more running than was confirmed.
type Control struct {
	Store     Store
	Selection SelectionStore
	Scheduler Scheduler
	Units     UnitConfig
}

func (c Control) root() (string, error) {
	return machine.CanonicalProfileRoot(c.Store.ProfileRoot)
}

// Enable saves a confirmed grant, selects this profile and schedules
// checks. If scheduling fails, the grant is turned off again and the
// previous selection restored.
func (c Control) Enable(ctx context.Context, base, next Grant, approval *LocalApproval) (Grant, error) {
	root, err := c.root()
	if err != nil {
		return Grant{}, err
	}
	saved, err := c.Store.Save(base, next, approval)
	if err != nil {
		return Grant{}, err
	}
	previous, hadPrevious, _ := c.Selection.Load()
	undo := func(cause error) (Grant, error) {
		// Back to paused if resuming, otherwise off: never more than before.
		off := saved
		off.Setting = SettingOff
		if base.Setting == SettingPaused {
			off.Setting = SettingPaused
		}
		_, _ = c.Store.Save(saved, off, nil)
		if hadPrevious {
			_ = c.Selection.Save(previous)
		} else {
			_ = c.Selection.Clear()
		}
		return Grant{}, cause
	}
	if err := c.Selection.Save(root); err != nil {
		return undo(err)
	}
	if err := c.Scheduler.Install(ctx, c.Units); err != nil {
		return undo(err)
	}
	return saved, nil
}

// Pause stops automatic work at once in the grant (no confirmation), then
// stops the timer. A timer error never undoes the pause: scheduled checks
// read the grant and skip.
func (c Control) Pause(ctx context.Context, base Grant) (Grant, error) {
	next := base
	next.Setting = SettingPaused
	saved, err := c.Store.Save(base, next, nil)
	if err != nil {
		return Grant{}, err
	}
	if c.selected() {
		_ = c.Scheduler.Stop(ctx)
	}
	return saved, nil
}

// Resume turns the same authority back on.
func (c Control) Resume(ctx context.Context, base Grant, approval *LocalApproval) (Grant, error) {
	next := base
	next.Setting = SettingOn
	return c.ResumeWith(ctx, base, next, approval)
}

// ResumeWith turns Auto Sync back on with next; any widening needs a
// confirmation, as for Enable.
func (c Control) ResumeWith(ctx context.Context, base, next Grant, approval *LocalApproval) (Grant, error) {
	next.Setting = SettingOn
	return c.Enable(ctx, base, next, approval)
}

// Disable revokes permission first, then removes the timer if it checks
// this profile. Disable keeps status and history.
func (c Control) Disable(ctx context.Context, base Grant) (Grant, error) {
	next := base
	next.Setting = SettingOff
	saved, err := c.Store.Save(base, next, nil)
	if err != nil {
		return Grant{}, err
	}
	if !c.selected() {
		return saved, nil
	}
	if err := c.Scheduler.Remove(ctx); err != nil {
		return saved, fmt.Errorf("Auto Sync is off, but its timer could not be removed: %w", err)
	}
	if err := c.Selection.Clear(); err != nil {
		return saved, err
	}
	return saved, nil
}

func (c Control) selected() bool {
	root, err := c.root()
	if err != nil {
		return false
	}
	selected, ok, err := c.Selection.Load()
	return err == nil && ok && selected == root
}
