package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/autosync"
	"github.com/Grenco/omarchy-blueprint/internal/command"
)

type systemctlFake struct {
	calls   []string
	replies map[string]string
	fail    map[string]bool
}

func (f *systemctlFake) Run(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	for prefix, reply := range f.replies {
		if strings.HasPrefix(call, prefix) {
			if f.fail[prefix] {
				return reply, &command.RunError{Name: name, Args: args, Output: reply, ExitCode: 1, Err: errors.New("exit status 1")}
			}
			return reply, nil
		}
	}
	return "", nil
}

func TestSystemctlUserManagerNeverUsesRootAndReadsState(t *testing.T) {
	next := time.Date(2026, 10, 17, 10, 30, 0, 0, time.UTC)
	fake := &systemctlFake{replies: map[string]string{
		"systemctl --user is-system-running": "degraded\n",
		"systemctl --user show":              "LoadState=loaded\nUnitFileState=enabled\nActiveState=active\n",
		"systemctl --user list-timers":       `[{"next":` + strconv.FormatInt(next.UnixMicro(), 10) + `,"unit":"omarchy-blueprint-auto-sync.timer"}]`,
	}, fail: map[string]bool{"systemctl --user is-system-running": true}}
	m := systemctlUserManager{Runner: fake}
	if err := m.Available(context.Background()); err != nil {
		t.Fatal("a degraded user manager is still available:", err)
	}
	state, err := m.State(context.Background(), autosync.TimerUnit)
	if err != nil || !state.Installed || !state.Enabled || !state.Active || state.Next == nil || !state.Next.Equal(next) {
		t.Fatalf("%+v %v", state, err)
	}
	m.Enable(context.Background(), autosync.TimerUnit)
	m.Disable(context.Background(), autosync.TimerUnit)
	m.Stop(context.Background(), autosync.TimerUnit)
	m.DaemonReload(context.Background())
	for _, call := range fake.calls {
		if !strings.HasPrefix(call, "systemctl --user ") || strings.Contains(call, "sudo") || strings.Contains(call, "linger") {
			t.Fatalf("unexpected call %q", call)
		}
	}
	joined := strings.Join(fake.calls, "\n")
	for _, want := range []string{"enable --now -- omarchy-blueprint-auto-sync.timer", "disable --now -- omarchy-blueprint-auto-sync.timer", "stop -- omarchy-blueprint-auto-sync.timer", "daemon-reload"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	absent := systemctlUserManager{Runner: &systemctlFake{replies: map[string]string{"systemctl --user is-system-running": "Failed to connect to bus: No medium found"}, fail: map[string]bool{"systemctl --user is-system-running": true}}}
	if err := absent.Available(context.Background()); !errors.Is(err, autosync.ErrNoUserManager) {
		t.Fatal(err)
	}
}
