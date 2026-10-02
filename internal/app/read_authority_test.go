package app

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type approvalChangeReader struct {
	change func()
	done   bool
}

func (r *approvalChangeReader) Read(out []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	r.change()
	return copy(out, "y\n"), nil
}

type authorityChangeRunner struct {
	runner   *machineRunner
	loads    int
	changeAt int
	change   func()
}

func (r *authorityChangeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "pacman" && len(args) == 1 && args[0] == "-Qq" {
		r.loads++
		if r.loads == r.changeAt {
			r.change()
		}
	}
	return r.runner.Run(ctx, name, args...)
}

func TestCLIApprovalRecheckBypassesReadCycle(t *testing.T) {
	for _, yes := range []bool{false, true} {
		t.Run(map[bool]string{false: "interactive", true: "yes"}[yes], func(t *testing.T) {
			dir, deps := configSandbox(t)
			d, e := profile.Load(dir)
			if e != nil {
				t.Fatal(e)
			}
			d.Packages.Official = []string{"git"}
			if e := profile.Save(dir, d); e != nil {
				t.Fatal(e)
			}
			runner := deps.Runner.(*machineRunner)
			runner.official = map[string]bool{}
			change := func() { runner.official["git"] = true }
			wrapper := &authorityChangeRunner{runner: runner, change: change}
			deps.Runner = wrapper
			deps.IsTTY = func() bool { return true }
			deps.MiseGlobalConfig = func() (string, error) { return "", nil }
			args := []string{"restore", "packages"}
			if yes {
				wrapper.changeAt = 2
				args = append(args, "--yes")
			} else {
				deps.In = &approvalChangeReader{change: change}
			}
			code, out := configRun(t, deps, dir, args...)
			if code == 0 || !strings.Contains(out, "plan changed after approval") {
				t.Fatalf("stale approval code=%d loads=%d out=%s", code, wrapper.loads, out)
			}
			for _, cmd := range runner.allCommands {
				if len(cmd) > 1 && (cmd[0] == "sudo" || cmd[0] == "pacman" && strings.HasPrefix(cmd[1], "-S")) {
					t.Fatal("executor ran after stale approval")
				}
			}
		})
	}
}

func TestCLINoopPreviewRevalidatesFreshAuthorityBeforeVerify(t *testing.T) {
	dir, deps := configSandbox(t)
	d, e := profile.Load(dir)
	if e != nil {
		t.Fatal(e)
	}
	d.Packages.Official = []string{"git"}
	if e := profile.Save(dir, d); e != nil {
		t.Fatal(e)
	}
	runner := deps.Runner.(*machineRunner)
	runner.official = map[string]bool{"git": true}
	wrapper := &authorityChangeRunner{runner: runner, changeAt: 2, change: func() { delete(runner.official, "git") }}
	deps.Runner = wrapper
	deps.IsTTY = func() bool { return true }
	deps.MiseGlobalConfig = func() (string, error) { return "", nil }
	code, out := configRun(t, deps, dir, "restore", "packages", "--yes")
	if code == 0 || !strings.Contains(out, "plan changed") {
		t.Fatalf("stale no-op code=%d loads=%d out=%s", code, wrapper.loads, out)
	}
	if runner.official["git"] {
		t.Fatal("stale no-op allowed newly appeared install authority")
	}
	after, e := profile.Load(dir)
	if e != nil || !reflect.DeepEqual(after, d) {
		t.Fatal("refusal changed profile", e)
	}
}
