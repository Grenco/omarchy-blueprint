package defaults

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type stubRunner struct {
	outputs map[string]string
	fails   map[string]bool
	calls   []string
}

func (r *stubRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	if r.fails[key] {
		return "", fmt.Errorf("command failed: %s", key)
	}
	return r.outputs[key], nil
}

func newRunner(outputs map[string]string) *stubRunner {
	return &stubRunner{outputs: outputs, fails: map[string]bool{}}
}

func testProvider(r command.Runner, profileDir string) Provider {
	return Provider{Runner: r, ProfileDir: profileDir}
}

func allDefaults(terminal, browser, editor, agent string) map[string]string {
	return map[string]string{
		"omarchy default terminal": terminal,
		"omarchy default browser":  browser,
		"omarchy default editor":   editor,
		"omarchy default agent":    agent,
	}
}

func TestDetectTrimsCommandOutput(t *testing.T) {
	r := newRunner(allDefaults("ghostty\n", "  firefox  \n", "zed\n", "codex\n"))
	p := testProvider(r, "")
	got, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := profile.Defaults{Terminal: "ghostty", Browser: "firefox", Editor: "zed", Agent: "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detect = %#v, want %#v", got, want)
	}
}

func TestDetectEmptyValueIsUnmanaged(t *testing.T) {
	r := newRunner(allDefaults("foot\n", "chromium\n", "nvim\n", "\n"))
	p := testProvider(r, "")
	got, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "" {
		t.Fatalf("agent = %q, want empty (unset defaults have no desired state)", got.Agent)
	}
}

func TestDetectCommandFailureIsDescriptive(t *testing.T) {
	r := newRunner(allDefaults("foot\n", "chromium\n", "nvim\n", ""))
	r.fails["omarchy default agent"] = true
	p := testProvider(r, "")
	_, err := p.Detect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "omarchy default agent") {
		t.Fatalf("error = %v, want command context", err)
	}
}

func TestCaptureStoresDetectedDefaults(t *testing.T) {
	r := newRunner(allDefaults("ghostty\n", "firefox\n", "zed\n", "codex\n"))
	dir := t.TempDir()
	p := testProvider(r, dir)
	got, err := p.Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := profile.Defaults{Terminal: "ghostty", Browser: "firefox", Editor: "zed", Agent: "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("captured = %#v, want %#v", got, want)
	}
	b, err := os.ReadFile(filepath.Join(dir, "defaults", "defaults.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "terminal = 'ghostty'") {
		t.Fatalf("defaults.toml = %q", b)
	}
}

func TestDiffSameValueNoDrift(t *testing.T) {
	saved := profile.Defaults{Terminal: "ghostty", Browser: "firefox"}
	current := profile.Defaults{Terminal: "ghostty", Browser: "firefox"}
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes = %#v, want none", changes)
	}
}

func TestDiffDifferentValueIsModify(t *testing.T) {
	changes := Diff(profile.Defaults{Terminal: "foot"}, profile.Defaults{Terminal: "ghostty"})
	if len(changes) != 1 || changes[0].Type != model.ChangeModify || changes[0].Name != "terminal" {
		t.Fatalf("changes = %#v", changes)
	}
	if !strings.Contains(changes[0].Summary, "foot") || !strings.Contains(changes[0].Summary, "ghostty") {
		t.Fatalf("summary = %q, want old → new", changes[0].Summary)
	}
}

func TestDiffNewlySelectedAgentIsAdd(t *testing.T) {
	changes := Diff(profile.Defaults{}, profile.Defaults{Agent: "codex"})
	if len(changes) != 1 || changes[0].Type != model.ChangeAdd || changes[0].Name != "agent" {
		t.Fatalf("changes = %#v", changes)
	}
	if !strings.Contains(changes[0].Summary, "codex") || !strings.Contains(changes[0].Summary, "will not remove") {
		t.Fatalf("summary = %q, want additive machine drift note", changes[0].Summary)
	}
}

func TestPlanSkipsAlreadyCorrectDefaults(t *testing.T) {
	p := testProvider(nil, "")
	saved := profile.Defaults{Terminal: "ghostty", Browser: "firefox"}
	current := profile.Defaults{Terminal: "ghostty", Browser: "firefox"}
	plan := p.Plan(saved, current, 3, "1.0", "1.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v, want none", plan.Operations)
	}
}

func TestPlanEmitsOneNativeOperationPerDriftedDefault(t *testing.T) {
	p := testProvider(nil, "")
	saved := profile.Defaults{Terminal: "ghostty", Browser: "firefox", Editor: "zed"}
	current := profile.Defaults{Terminal: "foot", Browser: "firefox", Editor: "nvim"}
	plan := p.Plan(saved, current, 3, "1.0", "1.0")
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %#v, want terminal+editor", plan.Operations)
	}
	byKind := map[string]model.Operation{}
	for _, op := range plan.Operations {
		byKind[op.Items[0]] = op
	}
	if cmd := byKind["terminal"].Command; !reflect.DeepEqual(cmd, []string{"omarchy", "default", "terminal", "--install", "ghostty"}) {
		t.Fatalf("terminal command = %v, want non-interactive install path", cmd)
	}
	if cmd := byKind["editor"].Command; !reflect.DeepEqual(cmd, []string{"omarchy", "default", "editor", "--install", "zed"}) {
		t.Fatalf("editor command = %v, want non-interactive install path", cmd)
	}
	for _, op := range plan.Operations {
		if op.Risk != model.RiskLow {
			t.Fatalf("%s risk = %s, want low", op.ID, op.Risk)
		}
		if op.Provider != "defaults" {
			t.Fatalf("provider = %s", op.Provider)
		}
	}
}

func TestPlanDoesNotAutomaticallyLaunchAgent(t *testing.T) {
	// omarchy default agent <name> ultimately launches the selected agent,
	// so an automatic restore must never invoke it; the default is skipped
	// until a set-only path exists.
	p := testProvider(nil, "")
	saved := profile.Defaults{Agent: "codex"}
	current := profile.Defaults{}
	plan := p.Plan(saved, current, 3, "1.0", "1.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v, agent setter must never be planned", plan.Operations)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Resource != "default:agent" {
		t.Fatalf("skipped = %#v, want explicit agent skip", plan.Skipped)
	}
	if !strings.Contains(plan.Skipped[0].Reason, "launches the selected agent") {
		t.Fatalf("reason = %q", plan.Skipped[0].Reason)
	}
}

func TestPlanSkipsNonPortableDesktopValues(t *testing.T) {
	p := testProvider(nil, "")
	saved := profile.Defaults{Browser: "vivaldi-stable.desktop"}
	current := profile.Defaults{Browser: "chromium"}
	plan := p.Plan(saved, current, 3, "1.0", "1.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v, raw desktop IDs must not be replayed", plan.Operations)
	}
	if len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "may not be portable") {
		t.Fatalf("skipped = %#v", plan.Skipped)
	}
}

func TestWarnFlagsNonPortableDesktopValues(t *testing.T) {
	warnings := Warn(profile.Defaults{Terminal: "foot", Browser: "vivaldi-stable.desktop"})
	if len(warnings) != 1 || warnings[0].Name != "browser" {
		t.Fatalf("warnings = %#v", warnings)
	}
	if warnings[0].Type != model.ChangeWarn || !strings.Contains(warnings[0].Summary, "may not be portable") {
		t.Fatalf("warning = %#v", warnings[0])
	}
	if warnings := Warn(profile.Defaults{Terminal: "foot", Browser: "firefox"}); len(warnings) != 0 {
		t.Fatalf("warnings = %#v, managed values must not warn", warnings)
	}
}

func TestPlanEmptySavedValueProducesNoOperation(t *testing.T) {
	p := testProvider(nil, "")
	saved := profile.Defaults{Terminal: "foot"}
	current := profile.Defaults{Terminal: "foot", Agent: "codex"}
	plan := p.Plan(saved, current, 3, "1.0", "1.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v; captured empty means no desired state", plan.Operations)
	}
}

func TestVerifyAllMatchOK(t *testing.T) {
	saved := profile.Defaults{Terminal: "ghostty", Browser: "firefox", Editor: "zed", Agent: "codex"}
	result := Verify(saved, saved)
	if !result.OK {
		t.Fatalf("result = %#v", result)
	}
}

func TestVerifyReportsDriftedDefault(t *testing.T) {
	result := Verify(profile.Defaults{Browser: "firefox"}, profile.Defaults{Browser: "chromium"})
	if result.OK {
		t.Fatal("expected verification failure")
	}
	if !reflect.DeepEqual(result.Missing, []string{"default:browser"}) {
		t.Fatalf("missing = %v", result.Missing)
	}
}

func TestVerifyUnmanagedKindNeverFails(t *testing.T) {
	saved := profile.Defaults{Terminal: "foot"}
	current := profile.Defaults{Terminal: "foot", Agent: "codex"}
	result := Verify(saved, current)
	if !result.OK {
		t.Fatalf("extra current default must not fail verification: %#v", result)
	}
}

func TestVerifyIgnoresValuesRestoreCannotSet(t *testing.T) {
	// Agent and non-portable values are skipped by Plan; verification must
	// not demand them either, or every restore would fail permanently.
	saved := profile.Defaults{Terminal: "foot", Agent: "codex", Browser: "vivaldi-stable.desktop"}
	current := profile.Defaults{Terminal: "foot"}
	if result := Verify(saved, current); !result.OK {
		t.Fatalf("result = %#v, agent and non-portable values must not fail verification", result)
	}
}
