package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
)

type overviewRunner struct{ calls []string }

func (r *overviewRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name)
	return "", errors.New("not a repository")
}

type overviewProvider struct{}

func (overviewProvider) ID() string                 { return "config" }
func (overviewProvider) Captured(profile.Data) bool { return true }
func (overviewProvider) Capture(context.Context, *profile.Data) (any, []model.Change, error) {
	return nil, nil, nil
}
func (overviewProvider) Diff(context.Context, profile.Data) ([]model.Change, error) { return nil, nil }
func (overviewProvider) DiffWithScan(context.Context, profile.Data) ([]model.Change, configprovider.ScanSummary, error) {
	return nil, configprovider.ScanSummary{Candidates: []configprovider.Candidate{{Path: ".config/z", Classification: configprovider.ConfigModifiedBaseline}, {Path: ".config/a", Classification: configprovider.ConfigAmbiguousBaseline}}}, nil
}

type overviewResources struct{}

func (overviewResources) ID() string                 { return "resources" }
func (overviewResources) Captured(profile.Data) bool { return true }
func (overviewResources) Capture(context.Context, *profile.Data) (any, []model.Change, error) {
	return nil, nil, nil
}
func (overviewResources) Diff(context.Context, profile.Data) ([]model.Change, error) { return nil, nil }
func (overviewResources) DiffWithGitWorkingState(context.Context, profile.Data) ([]model.Change, map[string]resourcesprovider.GitWorkingSummary, error) {
	return nil, map[string]resourcesprovider.GitWorkingSummary{"repo": {UnstagedTracked: 1}}, nil
}

func TestOverviewOrdersTypedAttentionWithoutNetworkFetch(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "removed", Path: "/mnt/removed"}}}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	runner := &overviewRunner{}
	session, err := Open(Dependencies{Runner: runner, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{overviewResources{}, overviewProvider{}})
	overview, err := session.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(overview.Items), 4; got != want {
		t.Fatalf("items=%d: %#v", got, overview.Items)
	}
	for i, want := range []AttentionSeverity{AttentionDecision, AttentionWarning, AttentionDrift, AttentionInfo} {
		if overview.Items[i].Severity != want {
			t.Fatalf("item %d severity=%s, want %s", i, overview.Items[i].Severity, want)
		}
	}
	if overview.Items[0].Ref != ".config/a" || overview.Items[3].Target != "resources" {
		t.Fatalf("items=%#v", overview.Items)
	}
	for _, call := range runner.calls {
		if call != "git" {
			t.Fatalf("unexpected network command %q", call)
		}
	}
}
