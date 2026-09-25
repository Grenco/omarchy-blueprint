package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type policyCLI struct {
	dir  string
	deps Dependencies
	out  bytes.Buffer
	err  bytes.Buffer
}

func newPolicyCLI(t *testing.T) *policyCLI {
	t.Helper()
	dir, deps := configSandbox(t)
	deps.Runner.(*machineRunner).official = map[string]bool{"firefox": true}
	deps.Runner.(*machineRunner).aur = map[string]bool{}
	c := &policyCLI{dir: dir, deps: deps}
	c.deps.Out, c.deps.Err = &c.out, &c.err
	return c
}

func (c *policyCLI) run(t *testing.T, args ...string) string {
	t.Helper()
	c.out.Reset()
	c.err.Reset()
	argv := append([]string{"--profile", c.dir}, args...)
	if code := Execute(context.Background(), argv, c.deps); code != 0 {
		t.Fatalf("%v: exit %d: %s", argv, code, c.err.String())
	}
	return c.out.String()
}

func (c *policyCLI) fail(t *testing.T, args ...string) string {
	t.Helper()
	c.out.Reset()
	c.err.Reset()
	if code := Execute(context.Background(), append([]string{"--profile", c.dir}, args...), c.deps); code == 0 {
		t.Fatalf("%v unexpectedly succeeded: %s", args, c.out.String())
	}
	return c.err.String()
}

func TestCLIPolicyScopesInheritanceAndReset(t *testing.T) {
	c := newPolicyCLI(t)
	c.run(t, "machine", "add", "laptop", "--no-use")
	c.run(t, "policy", "set", "capture", "packages", "disabled", "--scope", "profile")
	c.run(t, "policy", "set", "capture", "packages", "enabled", "official:firefox", "--scope", "laptop")
	c.run(t, "policy", "set", "restore", "packages", "disabled", "official:firefox", "--scope", "laptop")
	var result struct {
		Data struct {
			Scope   string `json:"scope"`
			Targets []struct {
				Target    string           `json:"target"`
				Effective policy.Effective `json:"effective"`
			} `json:"targets"`
		} `json:"data"`
	}
	decode := func() {
		t.Helper()
		if err := json.Unmarshal([]byte(c.run(t, "--json", "policy", "show", "packages", "official:firefox", "--scope", "laptop")), &result); err != nil {
			t.Fatal(err)
		}
	}
	decode()
	if result.Data.Scope != "laptop" || len(result.Data.Targets) != 1 || !result.Data.Targets[0].Effective.Capture.Enabled || result.Data.Targets[0].Effective.Restore.Enabled || !result.Data.Targets[0].Effective.Capture.Explicit {
		t.Fatalf("effective policy = %+v", result.Data)
	}
	c.run(t, "policy", "clear", "capture", "packages", "official:firefox", "--scope", "laptop")
	decode()
	if result.Data.Targets[0].Effective.Capture.Enabled || result.Data.Targets[0].Effective.Capture.Explicit {
		t.Fatalf("clear failed to inherit profile category: %+v", result.Data.Targets[0].Effective.Capture)
	}
	if got := c.run(t, "--json", "policy", "show", "packages", "official:firefox", "--scope", "profile"); !strings.Contains(got, `"scope": "profile"`) {
		t.Fatalf("portable scope unavailable: %s", got)
	}
	if got := c.fail(t, "policy", "set", "capture", "packages", "disabled", "bad target"); !strings.Contains(got, "invalid") {
		t.Fatalf("invalid target error = %s", got)
	}
	if got := c.fail(t, "policy", "set", "capture", "packages", "disabled", "--scope", "missing"); !strings.Contains(got, "does not exist") {
		t.Fatalf("unknown machine error = %s", got)
	}
}

func TestCLIPolicyConfigCanonicalTargetAndAncestor(t *testing.T) {
	c := newPolicyCLI(t)
	c.run(t, "policy", "set", "restore", "config", "disabled", "~/.config/editor", "--scope", "profile")
	var result struct {
		Data struct {
			Targets []struct {
				Target    string           `json:"target"`
				Effective policy.Effective `json:"effective"`
			} `json:"targets"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(c.run(t, "--json", "policy", "show", "config", "~/.config/editor/settings", "--scope", "profile")), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.Targets) != 1 || result.Data.Targets[0].Target != ".config/editor/settings" || result.Data.Targets[0].Effective.Restore.Enabled {
		t.Fatalf("Config ancestor rule was not resolved against canonical path: %+v", result.Data.Targets)
	}
	if got := c.run(t, "--json", "policy", "clear", "restore", "config", "~/.config/editor", "--scope", "profile"); !strings.Contains(got, `"target": ".config/editor"`) {
		t.Fatalf("clear reported noncanonical target: %s", got)
	}
}

func TestCLIMachineRestoreDefaultsAndStopManaging(t *testing.T) {
	c := newPolicyCLI(t)
	c.run(t, "machine", "add", "laptop", "--no-use")
	c.run(t, "machine", "restore-defaults", "set", "laptop", "--convergence", "exact")
	if got := c.run(t, "machine", "restore-defaults", "laptop"); !strings.Contains(got, "safe / exact") {
		t.Fatalf("defaults = %s", got)
	}
	c.run(t, "machine", "restore-defaults", "set", "laptop", "--conflicts", "force")
	d, err := profile.Load(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Machines.Items[0].EffectiveRestoreDefaults(); got.Conflicts != policy.ConflictForce || got.Convergence != policy.ConvergenceExact {
		t.Fatalf("partial edit lost the other axis: %+v", got)
	}
	if got := c.fail(t, "machine", "restore-defaults", "set", "laptop", "--conflicts", "invalid"); !strings.Contains(got, "invalid conflict") {
		t.Fatalf("invalid default = %s", got)
	}
	c.run(t, "capture", "packages")
	c.run(t, "policy", "set", "capture", "packages", "disabled", "official:firefox", "--scope", "laptop")
	c.run(t, "policy", "stop-managing", "packages", "official:firefox")
	d, err = profile.Load(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Packages.Official) != 0 || len(d.Machines.Items[0].Policy.Capture) != 0 {
		t.Fatalf("stop-managing left desired state or override: %+v", d)
	}
	if got := c.fail(t, "policy", "stop-managing", "shell", "state"); !strings.Contains(got, "does not support") {
		t.Fatalf("provider refusal = %s", got)
	}
}

func TestCLICapturePreviewAndJSONReviewNeverWrite(t *testing.T) {
	c := newPolicyCLI(t)
	for _, args := range [][]string{{"--json", "capture", "packages", "--dry-run"}, {"--json", "capture", "packages", "--review"}} {
		var result struct {
			Data struct {
				Sections []struct {
					Targets []struct {
						Key     string `json:"key"`
						Outcome string `json:"outcome"`
					} `json:"targets"`
				} `json:"sections"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(c.run(t, args...)), &result); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, section := range result.Data.Sections {
			for _, target := range section.Targets {
				found = found || (target.Key == "official:firefox" && target.Outcome == "add")
			}
		}
		if !found {
			t.Fatalf("preview omitted first-capture candidate: %+v", result.Data)
		}
		d, err := profile.Load(c.dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Packages.Official) != 0 {
			t.Fatalf("preview modified profile: %+v", d.Packages)
		}
	}
	if got := c.fail(t, "capture", "packages", "--review"); !strings.Contains(got, "interactive terminal") {
		t.Fatalf("non-interactive review error = %s", got)
	}
}

func TestCLICaptureReviewAppliesOnlyAfterApproval(t *testing.T) {
	c := newPolicyCLI(t)
	c.deps.IsTTY = func() bool { return true }
	c.deps.In = strings.NewReader("n\n")
	c.fail(t, "capture", "packages", "--review")
	d, err := profile.Load(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Packages.Official) != 0 {
		t.Fatalf("declined review captured packages: %+v", d.Packages)
	}
	c.deps.In = strings.NewReader("yes\n")
	if got := c.run(t, "capture", "packages", "--review"); !strings.Contains(got, "Captured state") {
		t.Fatalf("approved capture = %s", got)
	}
	d, err = profile.Load(c.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Packages.Official) != 1 || d.Packages.Official[0] != "firefox" {
		t.Fatalf("approved review did not capture candidate: %+v", d.Packages)
	}
}
