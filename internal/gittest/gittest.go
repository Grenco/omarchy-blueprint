// Package gittest keeps Git-backed tests deterministic.
//
// Git commands such as commit, fetch and receive-pack finish by starting
// "git maintenance run --auto", which by default detaches and keeps running
// after the command returns. In a test that races the cleanup of its
// temporary repository ("unlinkat .../objects: directory not empty").
// Tests turn automatic maintenance off for their own process and the Git
// commands it starts. Git strips GIT_CONFIG_* when it runs receive-pack for
// a local push, so bare remotes also get the same settings in their own
// configuration. User and global configuration are never touched.
package gittest

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

var settings = [][2]string{{"maintenance.auto", "false"}, {"gc.auto", "0"}}

// DisableAutomaticMaintenanceEnv returns env with automatic maintenance and
// automatic gc disabled through GIT_CONFIG_COUNT entries, keeping any
// entries already present.
func DisableAutomaticMaintenanceEnv(env []string) []string {
	count := 0
	out := make([]string, 0, len(env)+1+2*len(settings))
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "GIT_CONFIG_COUNT="); ok {
			count, _ = strconv.Atoi(value)
			continue
		}
		out = append(out, entry)
	}
	for _, setting := range settings {
		out = append(out, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", count, setting[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count, setting[1]))
		count++
	}
	return append(out, fmt.Sprintf("GIT_CONFIG_COUNT=%d", count))
}

// DisableAutomaticMaintenance applies DisableAutomaticMaintenanceEnv to the
// current test process, so every Git command it starts inherits it. Call it
// from TestMain before any test runs.
func DisableAutomaticMaintenance() {
	for _, entry := range DisableAutomaticMaintenanceEnv(os.Environ()) {
		if strings.HasPrefix(entry, "GIT_CONFIG_") {
			key, value, _ := strings.Cut(entry, "=")
			if err := os.Setenv(key, value); err != nil {
				panic(err)
			}
		}
	}
}

// ConfigureRemote disables automatic maintenance in a test repository that
// receives pushes, such as a bare origin. Call it right after creating it.
func ConfigureRemote(t testing.TB, gitDir string) {
	t.Helper()
	for _, setting := range settings {
		if out, err := exec.Command("git", "--git-dir="+gitDir, "config", setting[0], setting[1]).CombinedOutput(); err != nil {
			t.Fatalf("configure test remote %s: %v: %s", gitDir, err, out)
		}
	}
}
