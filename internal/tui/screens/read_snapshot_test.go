package screens

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestCaptureReviewAndProviderUseMatchingFreshPolicySnapshot(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	d, err := profile.Load(f.session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Policy.Capture = []policy.Rule{{Category: "packages", Target: "official:firefox", Setting: policy.SettingDisabled}}
	if err := profile.Save(f.session.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	f.screen.chosen = map[string]bool{"packages": true}
	f.screen.Update(f.screen.startReview()())
	if f.screen.review.err != nil {
		t.Fatal(f.screen.review.err)
	}
	if setting := f.screen.review.settings["packages\x00official:firefox"]; setting.Enabled {
		t.Fatal("Capture policy label used stale Session policy")
	}
	p := NewProvider(f.session, "packages")
	p.Update(p.refresh()())
	if p.err != nil {
		t.Fatal(p.err)
	}
	if p.effective["official:firefox"].Capture.Enabled {
		t.Fatal("Provider policy label used stale Session policy")
	}
	if len(f.session.Profile().Policy.Capture) != 0 {
		t.Fatal("read published policy into Session")
	}
}

func TestCaptureReviewPolicyEditFreshlyLoadsDesiredState(t *testing.T) {
	f := newReviewFixture(t, workflow.TargetInspection{Key: "official:firefox", Label: "firefox", CaptureEligible: true, Current: workflow.TargetPresent, Desired: workflow.TargetUnknown})
	d, err := profile.Load(f.session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Manifest.Profile.Name = "external-edit"
	if err := profile.Save(f.session.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	f.openReview(t)
	f.press(t, tea.KeyPressMsg{Code: tea.KeySpace})
	d, err = profile.Load(f.session.ProfileDir())
	if err != nil || d.Manifest.Profile.Name != "external-edit" {
		t.Fatal("policy edit overwrote a freshly previewed profile", err)
	}
}

type snapshotConfigProvider struct{ reviewProvider }

func (snapshotConfigProvider) DiffWithScan(context.Context, profile.Data) ([]model.Change, configprovider.ScanSummary, error) {
	return nil, configprovider.ScanSummary{Candidates: []configprovider.Candidate{{Path: ".config/test", Classification: configprovider.ConfigAdded}}}, nil
}

func TestConfigRefreshUsesMatchingFreshPolicyAndSelectionSnapshot(t *testing.T) {
	f := newReviewFixture(t)
	targets := []workflow.TargetInspection{{Key: ".config/test", Label: ".config/test", CaptureEligible: true, Current: workflow.TargetPresent}}
	p := snapshotConfigProvider{reviewProvider{id: "config", targets: &targets, captured: &f.captured}}
	if err := f.session.SetProviders([]workflow.Provider{p}); err != nil {
		t.Fatal(err)
	}
	d, err := profile.Load(f.session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Config.Excluded = []string{".config/test"}
	d.Policy.Capture = []policy.Rule{{Category: "config", Target: ".config/test", Setting: policy.SettingDisabled}}
	if err := profile.Save(f.session.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	screen := NewConfig(f.session)
	screen.Update(screen.rescan()())
	if screen.err != nil {
		t.Fatal(screen.err)
	}
	if screen.effective[".config/test"].Capture.Enabled {
		t.Fatal("Config policy label used stale Session policy")
	}
	if got := screen.candidatePolicy(".config/test"); got != "Excluded" {
		t.Fatal("Config selection label used stale desired state", got)
	}
	if got := screen.nextPolicy(".config/test"); got != "auto" {
		t.Fatal("Config policy toggle used stale desired state", got)
	}
}

func TestConfigPolicyEditFreshlyLoadsDesiredState(t *testing.T) {
	f := newReviewFixture(t)
	d, err := profile.Load(f.session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Manifest.Profile.Name = "external-edit"
	if err := profile.Save(f.session.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	screen := NewConfig(f.session)
	msg := screen.setPolicy(".config/test", "include")().(configPolicyMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	d, err = profile.Load(f.session.ProfileDir())
	if err != nil || d.Manifest.Profile.Name != "external-edit" {
		t.Fatal("Config policy edit overwrote external desired state", err)
	}
}

func TestResourcesRefreshUsesMatchingFreshPolicySnapshot(t *testing.T) {
	f := newReviewFixture(t)
	targets := []workflow.TargetInspection{{Key: "resource:project", Label: "project", CaptureEligible: true, Current: workflow.TargetPresent}}
	if err := f.session.SetProviders([]workflow.Provider{reviewProvider{id: "resources", targets: &targets, captured: &f.captured}}); err != nil {
		t.Fatal(err)
	}
	d, err := profile.Load(f.session.ProfileDir())
	if err != nil {
		t.Fatal(err)
	}
	d.Resources.Items = []profile.Resource{{ID: "project", Path: "~/project", Kind: "directory", Strategy: "copy"}}
	d.Policy.Capture = []policy.Rule{{Category: "resources", Target: "resource:project", Setting: policy.SettingDisabled}}
	if err := profile.Save(f.session.ProfileDir(), d); err != nil {
		t.Fatal(err)
	}
	screen := NewResources(f.session)
	screen.Update(screen.rescan()())
	if screen.err != nil {
		t.Fatal(screen.err)
	}
	if screen.policies["resource:project"].Capture.Enabled {
		t.Fatal("Resources policy label used stale Session policy")
	}
	if len(screen.items) != 1 {
		t.Fatal("Resources did not load edited items")
	}
}

type snapshotResourceActions struct {
	reviewProvider
	root string
}

func (p snapshotResourceActions) TrackResource(_ context.Context, d profile.Data, _ workflow.TrackRequest) (profile.Data, profile.Resource, []model.Change, error) {
	return d, profile.Resource{}, nil, profile.Save(p.root, d)
}
func (p snapshotResourceActions) UntrackResource(_ context.Context, d profile.Data, _ string) (profile.Data, []string, error) {
	return d, nil, profile.Save(p.root, d)
}

func TestResourcesProfileActionsFreshlyLoadDesiredState(t *testing.T) {
	for _, action := range []string{"track", "untrack"} {
		t.Run(action, func(t *testing.T) {
			f := newReviewFixture(t)
			p := snapshotResourceActions{reviewProvider: reviewProvider{id: "resources", targets: &f.targets, captured: &f.captured}, root: f.session.ProfileDir()}
			if err := f.session.SetProviders([]workflow.Provider{p}); err != nil {
				t.Fatal(err)
			}
			d, err := profile.Load(p.root)
			if err != nil {
				t.Fatal(err)
			}
			d.Manifest.Profile.Name = "external-edit"
			if err := profile.Save(p.root, d); err != nil {
				t.Fatal(err)
			}
			screen := NewResources(f.session)
			screen.items = []profile.Resource{{ID: "project"}}
			var actionErr error
			if action == "track" {
				actionErr = screen.track()().(resourceTrackedMsg).err
			} else {
				actionErr = screen.untrack()().(resourceUntrackedMsg).err
			}
			if actionErr != nil {
				t.Fatal(actionErr)
			}
			d, err = profile.Load(p.root)
			if err != nil || d.Manifest.Profile.Name != "external-edit" {
				t.Fatal("resource action overwrote external desired state", err)
			}
		})
	}
}
