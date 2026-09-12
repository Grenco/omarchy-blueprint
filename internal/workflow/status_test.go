package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type statusTestProvider struct {
	id      string
	changes []model.Change
}

func (p statusTestProvider) ID() string               { return p.id }
func (statusTestProvider) Captured(profile.Data) bool { return true }
func (p statusTestProvider) Capture(context.Context, *profile.Data) (any, []model.Change, error) {
	return nil, nil, nil
}
func (p statusTestProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return p.changes, nil
}
func (statusTestProvider) CategoryEnabled() bool { return true }

type uncapturedStatusTestProvider struct{ statusTestProvider }

func (uncapturedStatusTestProvider) Captured(profile.Data) bool { return false }

func TestWorkflowStatusPreservesProviderOrder(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{statusTestProvider{id: "packages"}, statusTestProvider{id: "config"}, statusTestProvider{id: "resources"}})
	report, err := session.Status(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Providers) != 3 || report.Providers[0].ID != "packages" || report.Providers[1].ID != "config" || report.Providers[2].ID != "resources" {
		t.Fatalf("providers = %#v", report.Providers)
	}
}

func TestWorkflowStatusIncludesProviderSnapshot(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Packages.Official = []string{"zoxide"}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{statusTestProvider{id: "packages"}})
	report, err := session.Status(context.Background(), "packages")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := report.Providers[0].Snapshot.(profile.Packages)
	if !ok || len(snapshot.Official) != 1 || snapshot.Official[0] != "zoxide" {
		t.Fatalf("snapshot = %#v", report.Providers[0].Snapshot)
	}
}

func TestCaptureStatusIncludesUncapturedProvidersInProviderOrder(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	if err := profile.Save(profileDir, profile.New("test", time.Now())); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{uncapturedStatusTestProvider{statusTestProvider{id: "packages"}}, statusTestProvider{id: "config"}})
	report, err := session.CaptureStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Providers) != 2 || report.Providers[0].ID != "packages" || report.Providers[0].Captured || report.Providers[1].ID != "config" || !report.Providers[1].Captured {
		t.Fatalf("providers = %#v", report.Providers)
	}
}
