package autosync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRenderUnits(t *testing.T) {
	units, err := RenderUnits(UnitConfig{Executable: "/usr/bin/omarchy-blueprint", IntervalMinutes: 30})
	if err != nil {
		t.Fatal(err)
	}
	service, timer := string(units[ServiceUnit]), string(units[TimerUnit])
	for _, want := range []string{"Type=oneshot", `ExecStart="/usr/bin/omarchy-blueprint" auto-sync scheduled-check`, "TimeoutStartSec=15min", "NoNewPrivileges=yes"} {
		if !strings.Contains(service, want) {
			t.Fatalf("service missing %q:\n%s", want, service)
		}
	}
	for _, want := range []string{"OnActiveSec=5min", "OnUnitInactiveSec=30min", "RandomizedDelaySec=", "Unit=" + ServiceUnit, "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Fatalf("timer missing %q:\n%s", want, timer)
		}
	}
	// Never: catch-up claims, lingering, root, a shell, or the service
	// itself wanted at login.
	// A user unit cannot order itself after the system's network, so it
	// does not pretend to; an offline check reports Offline.
	for _, banned := range []string{"Persistent=", "sh -c", "User=", "sudo", "WantedBy=default.target", "Linger", "network-online"} {
		if strings.Contains(service+timer, banned) {
			t.Fatalf("unit contains %q", banned)
		}
	}
	if strings.Contains(service, "[Install]") {
		t.Fatal("the service is installable on its own; only the timer may be enabled")
	}
}

func TestRenderUnitsEscapesOrRefusesTheExecutablePath(t *testing.T) {
	units, err := RenderUnits(UnitConfig{Executable: "/opt/my apps/100%/$HOME/blueprint", IntervalMinutes: 15})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(units[ServiceUnit]), `ExecStart="/opt/my apps/100%%/$$HOME/blueprint" auto-sync scheduled-check`) {
		t.Fatalf("%s", units[ServiceUnit])
	}
	for _, bad := range []string{"relative/blueprint", "/a\"b", "/a\\b", "/a\nExecStartPost=/bin/rm", "/a\x00b", ""} {
		if _, err := RenderUnits(UnitConfig{Executable: bad, IntervalMinutes: 30}); err == nil {
			t.Fatalf("accepted executable %q", bad)
		}
	}
	for _, interval := range []int{0, 14, 24*60 + 1} {
		if _, err := RenderUnits(UnitConfig{Executable: "/usr/bin/b", IntervalMinutes: interval}); err == nil {
			t.Fatalf("accepted interval %d", interval)
		}
	}
}

func TestReservedUnits(t *testing.T) {
	for _, name := range []string{ServiceUnit, TimerUnit} {
		if !IsReservedUnit(name) {
			t.Fatal(name)
		}
	}
	if IsReservedUnit("omarchy-blueprint-other.service") || IsReservedUnit("syncthing.service") {
		t.Fatal("reserved too much")
	}
}

type fakeManager struct {
	calls     []string
	failOn    string
	enabled   bool
	available error
}

func (m *fakeManager) step(name string) error {
	m.calls = append(m.calls, name)
	if m.failOn == name {
		return errors.New(name + " failed")
	}
	return nil
}
func (m *fakeManager) Available(context.Context) error { return m.available }
func (m *fakeManager) DaemonReload(context.Context) error {
	return m.step("daemon-reload")
}
func (m *fakeManager) Enable(_ context.Context, unit string) error {
	if err := m.step("enable " + unit); err != nil {
		return err
	}
	m.enabled = true
	return nil
}
func (m *fakeManager) Disable(_ context.Context, unit string) error {
	if err := m.step("disable " + unit); err != nil {
		return err
	}
	m.enabled = false
	return nil
}
func (m *fakeManager) Stop(_ context.Context, unit string) error { return m.step("stop " + unit) }
func (m *fakeManager) State(context.Context, string) (TimerState, error) {
	return TimerState{Enabled: m.enabled}, nil
}

func schedulerFixture(t *testing.T) (Scheduler, *fakeManager, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config", "systemd", "user")
	m := &fakeManager{}
	return Scheduler{UnitDir: dir, Manager: m}, m, dir
}

var unitConfig = UnitConfig{Executable: "/usr/bin/omarchy-blueprint", IntervalMinutes: 30}

func TestSchedulerInstallAndRemove(t *testing.T) {
	s, m, dir := schedulerFixture(t)
	if err := s.Install(context.Background(), unitConfig); err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.calls, ",") != "daemon-reload,enable "+TimerUnit || !m.enabled {
		t.Fatalf("%v", m.calls)
	}
	for _, unit := range []string{ServiceUnit, TimerUnit} {
		info, err := os.Stat(filepath.Join(dir, unit))
		if err != nil || info.Mode().Perm() != 0o644 {
			t.Fatalf("%s: %v %v", unit, info, err)
		}
	}
	m.calls = nil
	if err := s.Remove(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(m.calls, ",") != "disable "+TimerUnit+",daemon-reload" || m.enabled {
		t.Fatalf("%v", m.calls)
	}
	for _, unit := range []string{ServiceUnit, TimerUnit} {
		if _, err := os.Stat(filepath.Join(dir, unit)); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", unit)
		}
	}
}

// Any failed step puts the units and the timer back as they were.
func TestSchedulerInstallRollsBack(t *testing.T) {
	for _, step := range []string{"daemon-reload", "enable " + TimerUnit} {
		t.Run(step, func(t *testing.T) {
			s, m, dir := schedulerFixture(t)
			m.failOn = step
			if err := s.Install(context.Background(), unitConfig); err == nil {
				t.Fatal("install succeeded")
			}
			for _, unit := range []string{ServiceUnit, TimerUnit} {
				if _, err := os.Stat(filepath.Join(dir, unit)); !os.IsNotExist(err) {
					t.Fatalf("%s left behind after a failed install", unit)
				}
			}
			if m.enabled {
				t.Fatal("timer left enabled")
			}
		})
	}
	// An upgrade that fails restores the previous units.
	s, m, dir := schedulerFixture(t)
	if err := s.Install(context.Background(), unitConfig); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, TimerUnit))
	m.failOn = "enable " + TimerUnit
	changed := unitConfig
	changed.IntervalMinutes = 60
	if err := s.Install(context.Background(), changed); err == nil {
		t.Fatal("install succeeded")
	}
	if after, _ := os.ReadFile(filepath.Join(dir, TimerUnit)); string(after) != string(before) {
		t.Fatal("previous timer not restored")
	}
}

func TestSchedulerWithoutAUserManager(t *testing.T) {
	s, m, dir := schedulerFixture(t)
	m.available = errors.New("no user manager")
	if err := s.Install(context.Background(), unitConfig); !errors.Is(err, ErrNoUserManager) {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("wrote units without a user manager")
	}
	if len(m.calls) != 0 {
		t.Fatalf("%v", m.calls)
	}
}

// Units that are not Blueprint's are never overwritten or removed.
func TestSchedulerRefusesForeignUnits(t *testing.T) {
	s, _, dir := schedulerFixture(t)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, TimerUnit), []byte("[Timer]\nOnCalendar=daily\n"), 0o644)
	if err := s.Install(context.Background(), unitConfig); !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("%v", err)
	}
	if err := s.Remove(context.Background()); !errors.Is(err, ErrForeignUnit) {
		t.Fatalf("%v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, TimerUnit)); !strings.Contains(string(data), "daily") {
		t.Fatal("foreign unit changed")
	}
}

func TestReservedUnitsMatchTheProfileRule(t *testing.T) {
	for _, name := range []string{ServiceUnit, TimerUnit} {
		if !profile.IsReservedServiceUnit(name) {
			t.Fatalf("profile would accept %s", name)
		}
	}
}
