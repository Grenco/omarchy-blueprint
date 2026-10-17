package autosync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/localstate"
)

// Blueprint's own user units. Systemd only schedules; the Go workflow
// decides everything.
const (
	unitBase    = "omarchy-blueprint-auto-sync"
	ServiceUnit = unitBase + ".service"
	TimerUnit   = unitBase + ".timer"
	unitMarker  = "# Managed by Omarchy Blueprint Auto Sync. Change it with omarchy-blueprint auto-sync."
)

var (
	ErrNoUserManager = errors.New("the systemd user manager is not running, so Auto Sync cannot schedule checks; Blueprint never uses root or lingering instead")
	ErrForeignUnit   = errors.New("an Auto Sync unit file exists that Blueprint did not write; it was left alone")
)

// IsReservedUnit reports Blueprint's own units, which the Services provider
// must never capture or restore as portable user services (the profile
// package enforces the same names).
func IsReservedUnit(name string) bool {
	return name == ServiceUnit || name == TimerUnit
}

// UnitConfig is everything that varies in the units. The profile is not in
// it: the scheduled check reads the selected profile from private state.
type UnitConfig struct {
	Executable      string
	IntervalMinutes int
}

// execPath quotes an absolute path for ExecStart, escaping systemd
// specifiers and variables, and refuses anything it cannot quote exactly.
func execPath(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\"\\") {
		return "", errors.New("Auto Sync needs an absolute executable path without quotes or backslashes")
	}
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("Auto Sync executable path contains control characters")
		}
	}
	path = strings.ReplaceAll(path, "%", "%%")
	path = strings.ReplaceAll(path, "$", "$$")
	return `"` + path + `"`, nil
}

// RenderUnits returns the service and timer. The timer is monotonic
// (relative to activation and to the last check), so it makes no catch-up
// claim for time asleep or logged out.
func RenderUnits(cfg UnitConfig) (map[string][]byte, error) {
	exe, err := execPath(cfg.Executable)
	if err != nil {
		return nil, err
	}
	if cfg.IntervalMinutes < MinIntervalMinutes || cfg.IntervalMinutes > MaxIntervalMinutes {
		return nil, fmt.Errorf("Auto Sync interval must be between %d minutes and %d hours", MinIntervalMinutes, MaxIntervalMinutes/60)
	}
	jitter := cfg.IntervalMinutes / 6
	service := unitMarker + `
[Unit]
Description=Omarchy Blueprint Auto Sync check

[Service]
Type=oneshot
ExecStart=` + exe + ` auto-sync scheduled-check
TimeoutStartSec=15min
NoNewPrivileges=yes
Nice=10
IOSchedulingClass=idle
`
	timer := unitMarker + `
[Unit]
Description=Check the shared Omarchy Blueprint profile

[Timer]
OnActiveSec=5min
OnUnitInactiveSec=` + fmt.Sprintf("%dmin", cfg.IntervalMinutes) + `
RandomizedDelaySec=` + fmt.Sprintf("%dmin", jitter) + `
AccuracySec=1min
Unit=` + ServiceUnit + `

[Install]
WantedBy=timers.target
`
	return map[string][]byte{ServiceUnit: []byte(service), TimerUnit: []byte(timer)}, nil
}

// TimerState is what the user manager reports about the timer.
type TimerState struct {
	Installed bool       `json:"installed"`
	Enabled   bool       `json:"enabled"`
	Active    bool       `json:"active"`
	Next      *time.Time `json:"next,omitempty"`
}

// UserManager is the systemd --user manager. Tests use a fake; the real
// adapter runs systemctl --user and never root.
type UserManager interface {
	Available(context.Context) error
	DaemonReload(context.Context) error
	Enable(context.Context, string) error  // enable --now
	Disable(context.Context, string) error // disable --now
	Stop(context.Context, string) error
	State(context.Context, string) (TimerState, error)
}

// Scheduler installs and removes Blueprint's timer.
type Scheduler struct {
	UnitDir string
	Manager UserManager
}

// readOwned returns a unit's current content, or nil when absent. A unit
// Blueprint did not write (or a symlink) is ErrForeignUnit.
func (s Scheduler) readOwned(name string) ([]byte, error) {
	path := filepath.Join(s.UnitDir, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, ErrForeignUnit
	}
	data, err := localstate.ReadFile(path, 64<<10)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(data), unitMarker+"\n") {
		return nil, ErrForeignUnit
	}
	return data, nil
}

func (s Scheduler) put(name string, data []byte) error {
	path := filepath.Join(s.UnitDir, name)
	if data == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := localstate.WriteFile(path, data); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}

// Install writes the units, reloads the user manager and enables the
// timer. If any step fails, the previous units and timer state come back.
func (s Scheduler) Install(ctx context.Context, cfg UnitConfig) error {
	units, err := RenderUnits(cfg)
	if err != nil {
		return err
	}
	if s.Manager.Available(ctx) != nil {
		return ErrNoUserManager
	}
	previous := map[string][]byte{}
	for _, name := range []string{ServiceUnit, TimerUnit} {
		data, err := s.readOwned(name)
		if err != nil {
			return err
		}
		previous[name] = data
	}
	wasEnabled := false
	if state, err := s.Manager.State(ctx, TimerUnit); err == nil {
		wasEnabled = state.Enabled
	}
	if err := os.MkdirAll(s.UnitDir, 0o755); err != nil {
		return err
	}
	rollback := func(cause error) error {
		for name, data := range previous {
			_ = s.put(name, data)
		}
		_ = s.Manager.DaemonReload(ctx)
		if !wasEnabled {
			_ = s.Manager.Disable(ctx, TimerUnit)
		}
		return fmt.Errorf("Auto Sync could not schedule checks; nothing was changed: %w", cause)
	}
	for _, name := range []string{ServiceUnit, TimerUnit} {
		if err := s.put(name, units[name]); err != nil {
			return rollback(err)
		}
	}
	if err := s.Manager.DaemonReload(ctx); err != nil {
		return rollback(err)
	}
	if err := s.Manager.Enable(ctx, TimerUnit); err != nil {
		return rollback(err)
	}
	return nil
}

// Remove disables the timer and deletes Blueprint's units.
func (s Scheduler) Remove(ctx context.Context) error {
	present := false
	for _, name := range []string{ServiceUnit, TimerUnit} {
		data, err := s.readOwned(name)
		if err != nil {
			return err
		}
		present = present || data != nil
	}
	if !present {
		return nil
	}
	if s.Manager.Available(ctx) == nil {
		if err := s.Manager.Disable(ctx, TimerUnit); err != nil {
			return err
		}
	}
	for _, name := range []string{TimerUnit, ServiceUnit} {
		if err := s.put(name, nil); err != nil {
			return err
		}
	}
	if s.Manager.Available(ctx) == nil {
		return s.Manager.DaemonReload(ctx)
	}
	return nil
}

// Stop stops the timer until the next login without uninstalling it.
// Stopping a timer does not cancel a check already running; a paused
// setting makes that check skip on its own.
func (s Scheduler) Stop(ctx context.Context) error {
	if s.Manager.Available(ctx) != nil {
		return nil
	}
	return s.Manager.Stop(ctx, TimerUnit)
}

// State reports the timer, or an empty state without a user manager.
func (s Scheduler) State(ctx context.Context) (TimerState, error) {
	if s.Manager.Available(ctx) != nil {
		return TimerState{}, ErrNoUserManager
	}
	return s.Manager.State(ctx, TimerUnit)
}
