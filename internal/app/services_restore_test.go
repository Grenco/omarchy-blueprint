package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	servicesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/services"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

type servicesExecutionRunner struct {
	base  *machineRunner
	calls []string
}

func (r *servicesExecutionRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if name == "systemctl" {
		command := name + " " + strings.Join(args, " ")
		r.calls = append(r.calls, command)
		if len(args) == 2 && args[0] == "--user" && args[1] == "daemon-reload" {
			return "", nil
		}
		return "", errors.New("unexpected service mutation: " + command)
	}
	return r.base.Run(ctx, name, args...)
}

func TestServicesPublicRestoreRecreatesPersistentDefinitionWithoutActivation(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	want := []byte("[Service]\n# retain this comment\nExecStart=/usr/bin/true\n")
	if err := os.WriteFile(live, want, 0o644); err != nil {
		t.Fatal(err)
	}
	deps.ServicesSystemd = appServicesSystemd{units: []servicesprovider.ObservedUnit{appService("backup.service", live)}}
	deps.In = strings.NewReader("yes\nyes\n")
	if code, output := configRun(t, deps, profileDir, "capture", "services", "--review"); code != 0 {
		t.Fatal(output)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	runner := &servicesExecutionRunner{base: deps.Runner.(*machineRunner)}
	deps.Runner = runner
	if code, output := configRun(t, deps, profileDir, "restore", "services", "--yes"); code != 0 || !strings.Contains(output, "Restore verified") {
		t.Fatalf("public persistent Restore failed: code=%d output=%s", code, output)
	}
	bytes, err := os.ReadFile(live)
	if err != nil || string(bytes) != string(want) {
		t.Fatalf("restored definition bytes differ: %q err=%v", bytes, err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "systemctl --user daemon-reload" {
		t.Fatalf("persistent Restore gained activation or used wrong ordering: %v", runner.calls)
	}
}

func TestServicesApprovedRestoreRejectsChangedOwnershipBeforeMutation(t *testing.T) {
	profileDir, deps, roots := servicesCLIFixture(t)
	mise := filepath.Join(t.TempDir(), "mise.toml")
	deps.MiseGlobalConfig = func() (string, error) { return mise, nil }
	live := filepath.Join(roots.UserConfigDir, "backup.service")
	if err := os.WriteFile(live, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := &appServicesSystemd{units: []servicesprovider.ObservedUnit{appService("backup.service", live)}}
	deps.ServicesSystemd = manager
	deps.In = strings.NewReader("yes\nyes\n")
	if code, output := configRun(t, deps, profileDir, "capture", "services", "--review"); code != 0 {
		t.Fatal(output)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	session, err := openWorkflow(deps, &options{profileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := session.PlanRestore(context.Background(), "services", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(approved.Operations) == 0 {
		t.Fatal("fixture has no persistent work")
	}
	external := filepath.Join(t.TempDir(), "backup.service")
	if err := os.WriteFile(external, []byte("[Service]\nExecStart=/usr/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager.units[0].FragmentPath = external
	_, err = session.ApplyApprovedRestore(context.Background(), "services", nil, approved, false)
	if !errors.Is(err, workflow.ErrRestorePlanChanged) {
		t.Fatalf("changed ownership retained approval: %v", err)
	}
	if _, err := os.Stat(live); !os.IsNotExist(err) {
		t.Fatalf("rejected approval mutated Services: %v", err)
	}
}

func TestServicesUserManagerRequirementReachesAuthoritativeWorkflowPlan(t *testing.T) {
	profileDir, deps, _ := servicesCLIFixture(t)
	mise := filepath.Join(t.TempDir(), "mise.toml")
	deps.MiseGlobalConfig = func() (string, error) { return mise, nil }
	data, err := profile.Load(profileDir)
	if err != nil {
		t.Fatal(err)
	}
	data.Manifest.Capture.Services = true
	data.Services.Units = []profile.ServiceUnit{{Name: "backup.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, Definition: "units/backup.service", StartIntent: profile.ServiceStartNotManaged}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	deps.ServicesSystemd = unavailableAppServices{appServicesSystemd{}}
	session, err := openWorkflow(deps, &options{profileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := session.PlanRestore(context.Background(), "services", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Requirements) != 1 || len(plan.Compatibility.Categories) != 1 || plan.Compatibility.Categories[0].Authority != model.CompatibilityBlocked {
		t.Fatalf("inspection swallowed authoritative readiness: %+v", plan)
	}
	var blocked *workflow.BlockedCompatibilityError
	if err := workflow.CheckRestoreApplicable(plan, false); !errors.As(err, &blocked) {
		t.Fatalf("readiness failed to block before mutation: %v", err)
	}
}

type unavailableAppServices struct{ appServicesSystemd }

func (unavailableAppServices) InspectUserUnits(context.Context) ([]servicesprovider.ObservedUnit, error) {
	return nil, errors.New("user manager unavailable")
}
