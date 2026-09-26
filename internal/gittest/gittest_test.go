package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// maintenanceStarts commits in a fresh repository and reports how many
// "git maintenance" processes Git's trace2 event log records.
func maintenanceStarts(t *testing.T, env []string) int {
	t.Helper()
	repo, trace := t.TempDir(), filepath.Join(t.TempDir(), "trace.json")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = repo, append(env, "GIT_TRACE2_EVENT="+trace)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "x")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"event":"start"`) && strings.Contains(line, `"maintenance"`) {
			count++
		}
	}
	return count
}

func TestCommitSpawnsAutomaticMaintenanceByDefault(t *testing.T) {
	if maintenanceStarts(t, os.Environ()) == 0 {
		t.Skip("this Git does not run automatic maintenance after commit")
	}
}

func TestDisableAutomaticMaintenanceStopsBackgroundMaintenance(t *testing.T) {
	env := DisableAutomaticMaintenanceEnv(os.Environ())
	if got := maintenanceStarts(t, env); got != 0 {
		t.Fatalf("automatic maintenance still ran %d time(s)", got)
	}
}

func TestDisableAutomaticMaintenancePreservesExistingConfigEntries(t *testing.T) {
	env := DisableAutomaticMaintenanceEnv([]string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=user.name", "GIT_CONFIG_VALUE_0=Kept"})
	want := map[string]bool{"GIT_CONFIG_COUNT=3": true, "GIT_CONFIG_KEY_0=user.name": true, "GIT_CONFIG_VALUE_0=Kept": true,
		"GIT_CONFIG_KEY_1=maintenance.auto": true, "GIT_CONFIG_VALUE_1=false": true, "GIT_CONFIG_KEY_2=gc.auto": true, "GIT_CONFIG_VALUE_2=0": true}
	for _, entry := range env {
		delete(want, entry)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v in %v", want, env)
	}
}

// pushMaintenanceStarts pushes one commit to a bare remote with the test
// environment in place and counts "git maintenance" processes started.
func pushMaintenanceStarts(t *testing.T, configure bool) int {
	t.Helper()
	root := t.TempDir()
	remote, work, trace := filepath.Join(root, "origin.git"), filepath.Join(root, "work"), filepath.Join(root, "trace.json")
	env := DisableAutomaticMaintenanceEnv(os.Environ())
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = dir, append(env, "GIT_TRACE2_EVENT="+trace)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run(root, "init", "-q", "--bare", remote)
	if configure {
		ConfigureRemote(t, remote)
	}
	run(root, "init", "-q", "-b", "main", work)
	run(work, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "x")
	if err := os.Remove(trace); err != nil {
		t.Fatal(err)
	}
	run(work, "push", "-q", remote, "main")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"event":"start"`) && strings.Contains(line, `"maintenance"`) {
			count++
		}
	}
	return count
}

// Git strips GIT_CONFIG_* from local transports, so receive-pack in a bare
// remote needs the settings in the remote's own configuration.
func TestPushedRemoteNeedsItsOwnConfiguration(t *testing.T) {
	if pushMaintenanceStarts(t, false) == 0 {
		t.Skip("this Git does not run automatic maintenance after receive-pack")
	}
	if got := pushMaintenanceStarts(t, true); got != 0 {
		t.Fatalf("configured remote still ran automatic maintenance %d time(s)", got)
	}
}
