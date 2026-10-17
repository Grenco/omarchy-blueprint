package command

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Background Auto Sync checks must never wait for a password, passphrase or
// host-key answer: no controlling terminal, no Git prompt, no askpass helper.
func TestNoninteractiveRunnerCannotPrompt(t *testing.T) {
	t.Setenv("GIT_TERMINAL_PROMPT", "")
	t.Setenv("SSH_ASKPASS_REQUIRE", "force")
	probe := sessionProbe + `; echo "ssh=$SSH_ASKPASS_REQUIRE git=$GIT_ASKPASS"`
	runner := SystemRunner{Noninteractive: true}
	for name, run := range map[string]func() (string, error){
		"Run": func() (string, error) { return runner.Run(context.Background(), "sh", "-c", probe) },
		"RunOutput": func() (string, error) {
			out, err := runner.RunOutput(context.Background(), 1024, "sh", "-c", probe)
			return string(out), err
		},
	} {
		out, err := run()
		if err != nil {
			t.Fatal(name, err)
		}
		if !strings.Contains(out, "prompt=0") || !strings.Contains(out, "leader") || !strings.Contains(out, "ssh=never") || strings.Contains(out, "git= ") || !strings.Contains(out, "git=") {
			t.Fatalf("%s could still prompt: %q", name, out)
		}
	}
}

// Cancelling a background check stops the whole process group, so an ssh
// helper started by git does not outlive it.
func TestNoninteractiveRunnerCancelStopsTheGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := SystemRunner{Noninteractive: true}.Run(ctx, "sh", "-c", `sleep 30 & echo $! > `+pidFile+`; wait`)
		done <- err
	}()
	var child int
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if data, err := os.ReadFile(pidFile); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			child, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			break
		}
	}
	if child == 0 {
		t.Fatal("child never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop the command")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if !running(child) {
			return
		}
	}
	t.Fatal("helper process outlived the cancelled command")
}

func TestNoninteractiveRunnerRefusesTheTerminal(t *testing.T) {
	err := SystemRunner{Noninteractive: true}.RunInteractive(context.Background(), "true")
	if err == nil {
		t.Fatal("a background runner lent the terminal")
	}
}

// running reports a live process. A killed process whose parent has gone
// stays a zombie until init reaps it, and some containers' init never does;
// a zombie has stopped running.
func running(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) == 0 || fields[0] != "Z"
}
