package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type runnerFunc func(context.Context, string, ...string) (string, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}

func TestCaptureGitAndLocalPlugins(t *testing.T) {
	user, profileDir := t.TempDir(), t.TempDir()
	for _, id := range []string{"acme.git", "mine.local"} {
		if err := os.MkdirAll(filepath.Join(user, id), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(user, "acme.git", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "mine.local", "manifest.json"), []byte(`{"id":"mine.local"}`), 0644); err != nil {
		t.Fatal(err)
	}
	catalog := `[{"id":"acme.git","enabled":true,"firstParty":false},{"id":"mine.local","enabled":false,"firstParty":false,"clonedFrom":"omarchy.clock"}]`
	runner := runnerFunc(func(_ context.Context, name string, args ...string) (string, error) {
		joined := name + " " + strings.Join(args, " ")
		switch {
		case joined == "omarchy plugin list --json":
			return catalog, nil
		case strings.HasSuffix(joined, "remote get-url origin"):
			return "https://token@example.test/acme.git\n", nil
		case strings.HasSuffix(joined, "rev-parse HEAD"):
			return strings.Repeat("a", 40) + "\n", nil
		case strings.HasSuffix(joined, "status --porcelain --untracked-files=all"):
			return "", nil
		default:
			return "", fmt.Errorf("unexpected %s", joined)
		}
	})
	got, err := (Provider{Runner: runner, UserDir: user, ProfileDir: profileDir}).Capture(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Source != "git" || got.Items[0].URL != "https://example.test/acme.git" {
		t.Fatalf("git=%#v", got.Items[0])
	}
	if got.Items[1].Source != "local" || got.Items[1].Hash == "" {
		t.Fatalf("local=%#v", got.Items[1])
	}
	if _, err := os.Stat(filepath.Join(profileDir, "plugins", "local", "mine.local", "manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRestorePlanUsesDependentValidationChain(t *testing.T) {
	p := Provider{UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "mine.local", Source: "local", Hash: "abc", Enabled: true}}}
	plan := p.Plan(saved, profile.Plugins{}, 1, "4", "4", Semantics{ManageEnabled: true})
	if len(plan.Operations) != 4 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	if plan.Operations[0].Action != "validate" || plan.Operations[1].DependsOn[0] != plan.Operations[0].ID || plan.Operations[3].Risk != model.RiskHigh {
		t.Fatalf("operations=%#v", plan.Operations)
	}
}

func TestGitRestorePlanRequiresTrustAndRevalidatesPinnedRevision(t *testing.T) {
	p := Provider{UserDir: t.TempDir()}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "git", URL: "https://example.test/acme.git", Revision: strings.Repeat("a", 40), Enabled: true}}}
	plan := p.Plan(saved, profile.Plugins{}, 1, "4", "4", Semantics{ManageEnabled: true})
	if len(plan.Operations) != 5 {
		t.Fatalf("operations=%#v", plan.Operations)
	}
	if plan.Operations[0].Risk != model.RiskHigh || !reflect.DeepEqual(plan.Operations[0].Command, []string{"omarchy", "plugin", "add", "https://example.test/acme.git", "--yes"}) {
		t.Fatalf("install=%#v", plan.Operations[0])
	}
	if plan.Operations[2].Action != "validate" || plan.Operations[2].DependsOn[0] != plan.Operations[1].ID || plan.Operations[4].DependsOn[0] != plan.Operations[3].ID {
		t.Fatalf("dependencies=%#v", plan.Operations)
	}
}

func TestDetectDiffPlanAndVerify(t *testing.T) {
	json := `[{"id":"omarchy.clock","enabled":true,"firstParty":true,"canDisable":true}]`
	p := Provider{Runner: runnerFunc(func(context.Context, string, ...string) (string, error) { return json, nil })}
	current, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.Items, []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}) {
		t.Fatalf("plugins=%#v", current.Items)
	}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Enabled: false}}}
	if len(Diff(saved, current, Semantics{ManageEnabled: true})) != 1 {
		t.Fatal("expected drift")
	}
	plan := p.Plan(saved, current, 1, "4", "4", Semantics{ManageEnabled: true})
	if got := plan.Operations[0].Command; !reflect.DeepEqual(got, []string{"omarchy", "plugin", "disable", "omarchy.clock"}) {
		t.Fatalf("command=%#v", got)
	}
	if Verify(saved, current, Semantics{ManageEnabled: true}).OK {
		t.Fatal("verification unexpectedly passed")
	}
}

func TestShellOwnedSemanticsIgnoreEnabledDrift(t *testing.T) {
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: false}}}
	semantics := Semantics{ManageEnabled: false}

	if got := Diff(saved, current, semantics); len(got) != 0 {
		t.Fatalf("diff = %#v", got)
	}
	if got := Verify(saved, current, semantics); !got.OK {
		t.Fatalf("verify = %#v", got)
	}
}

func TestShellOwnedSemanticsEmitNoEnableDisableOperations(t *testing.T) {
	p := Provider{UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	// Missing local plugin: install/pin/validate/copy/rescan still planned,
	// but no enable/disable even though Enabled is true.
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "acme.weather", Source: "local", Hash: "h", Enabled: true}}}
	current := profile.Plugins{}
	plan := p.Plan(saved, current, 4, "1.0", "1.0", Semantics{ManageEnabled: false})
	for _, operation := range plan.Operations {
		if operation.Action == "enable" || operation.Action == "disable" {
			t.Fatalf("shell-owned enablement must not be planned: %#v", plan.Operations)
		}
	}
	if len(plan.Operations) == 0 {
		t.Fatal("source reconstruction operations must still be planned")
	}
}

func TestLegacySemanticsPreserveEnablementBehavior(t *testing.T) {
	p := Provider{UserDir: t.TempDir(), ProfileDir: t.TempDir()}
	saved := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: true}}}
	current := profile.Plugins{Items: []profile.Plugin{{ID: "omarchy.clock", Source: "builtin", Enabled: false}}}
	semantics := Semantics{ManageEnabled: true}
	changes := Diff(saved, current, semantics)
	if len(changes) != 1 || !strings.Contains(changes[0].Summary, "enabled: false → true") {
		t.Fatalf("legacy diff = %#v", changes)
	}
	plan := p.Plan(saved, current, 3, "1.0", "1.0", semantics)
	foundEnable := false
	for _, operation := range plan.Operations {
		if operation.Action == "enable" {
			foundEnable = true
		}
	}
	if !foundEnable {
		t.Fatalf("legacy plan must emit enable: %#v", plan.Operations)
	}
	if got := Verify(saved, current, semantics); got.OK {
		t.Fatalf("legacy verify must fail on enabled drift: %#v", got)
	}
}
