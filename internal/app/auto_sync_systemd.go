package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/command"
)

func defaultConfigHome() (string, error) {
	if path := os.Getenv("XDG_CONFIG_HOME"); path != "" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func autoSyncUnitDir(configHome string) string {
	return filepath.Join(configHome, "systemd", "user")
}

// systemctlUserManager drives the systemd user manager with systemctl
// --user. It never uses root, sudo or lingering.
type systemctlUserManager struct {
	Runner command.Runner
}

func (m systemctlUserManager) systemctl(ctx context.Context, args ...string) (string, error) {
	return m.Runner.Run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// Available reports a reachable user manager. is-system-running exits
// non-zero when degraded, so its output decides, not its exit status.
func (m systemctlUserManager) Available(ctx context.Context) error {
	out, _ := m.systemctl(ctx, "is-system-running")
	switch strings.TrimSpace(out) {
	case "running", "degraded", "starting", "initializing", "maintenance":
		return nil
	}
	return autosync.ErrNoUserManager
}

func (m systemctlUserManager) DaemonReload(ctx context.Context) error {
	_, err := m.systemctl(ctx, "daemon-reload")
	return err
}

func (m systemctlUserManager) Enable(ctx context.Context, unit string) error {
	_, err := m.systemctl(ctx, "enable", "--now", "--", unit)
	return err
}

func (m systemctlUserManager) Disable(ctx context.Context, unit string) error {
	_, err := m.systemctl(ctx, "disable", "--now", "--", unit)
	return err
}

func (m systemctlUserManager) Stop(ctx context.Context, unit string) error {
	_, err := m.systemctl(ctx, "stop", "--", unit)
	return err
}

func (m systemctlUserManager) State(ctx context.Context, unit string) (autosync.TimerState, error) {
	out, err := m.systemctl(ctx, "show", "--property=LoadState,UnitFileState,ActiveState", "--", unit)
	if err != nil {
		return autosync.TimerState{}, err
	}
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[key] = value
		}
	}
	state := autosync.TimerState{
		Installed: props["LoadState"] == "loaded",
		Enabled:   props["UnitFileState"] == "enabled",
		Active:    props["ActiveState"] == "active",
	}
	if !state.Active {
		return state, nil
	}
	out, err = m.systemctl(ctx, "list-timers", "--all", "--output=json", "--", unit)
	if err != nil {
		return state, nil
	}
	var timers []struct {
		Next int64  `json:"next"`
		Unit string `json:"unit"`
	}
	if json.Unmarshal([]byte(out), &timers) == nil {
		for _, timer := range timers {
			if timer.Unit == unit && timer.Next > 0 {
				next := time.UnixMicro(timer.Next)
				state.Next = &next
			}
		}
	}
	return state, nil
}
