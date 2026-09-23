package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
)

// firefoxProvider models Firefox saved in the profile but not installed on
// this desktop.
type firefoxProvider struct {
	target  TargetInspection
	changes []model.Change
}

func (firefoxProvider) ID() string                 { return "packages" }
func (firefoxProvider) Captured(profile.Data) bool { return true }
func (p firefoxProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return []TargetInspection{p.target}, nil
}
func (firefoxProvider) Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error) {
	return nil, nil, nil
}
func (p firefoxProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return p.changes, nil
}
func (firefoxProvider) ChangeTargetKey(change model.Change) (string, bool) {
	if change.Kind != "official" {
		return "", false
	}
	return "official:" + change.Name, true
}

var firefoxRemoved = model.Change{Type: model.ChangeRemove, Provider: "packages", Kind: "official", Name: "firefox", Summary: "- official package firefox"}

func firefoxTarget() TargetInspection {
	return TargetInspection{
		Key: "official:firefox", Label: "firefox", Desired: TargetPresent, Current: TargetAbsent,
		CaptureEligible: true, RestoreEligible: true,
		Capabilities: TargetCapabilities{SupportsCapture: true, SupportsRestore: true, SupportsDesiredAbsence: true},
	}
}

func desktopOverviewSession(t *testing.T, provider Provider) *Session {
	t.Helper()
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Machines.Items = []profile.Machine{{Name: "desktop"}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Runner: &overviewRunner{}, Now: func() time.Time { return time.Unix(1, 0) }, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir, ExplicitMachine: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.SetProviders([]Provider{provider}); err != nil {
		t.Fatal(err)
	}
	return session
}

func firefoxItem(t *testing.T, overview Overview) AttentionItem {
	t.Helper()
	for _, item := range overview.Items {
		if item.Provider == "packages" && item.Ref == "firefox" {
			return item
		}
	}
	t.Fatalf("no Firefox difference in %#v", overview.Items)
	return AttentionItem{}
}

func TestOverviewClassifiesFirefoxDifferenceByDesktopPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		capture     policy.Setting
		restore     policy.Setting
		want        AttentionSeverity
		healthy     bool
		wantReasons bool
	}{
		{name: "no policy: Capture would record absence and Restore would reinstall", want: AttentionDrift},
		{name: "Ignore only: Restore would still reinstall", capture: policy.SettingDisabled, want: AttentionDrift},
		{name: "Skip only: Capture would still record absence", restore: policy.SettingDisabled, want: AttentionDrift},
		{name: "Ignore + Skip: desktop deliberately differs", capture: policy.SettingDisabled, restore: policy.SettingDisabled, want: AttentionIntentional, healthy: true, wantReasons: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := desktopOverviewSession(t, firefoxProvider{target: firefoxTarget(), changes: []model.Change{firefoxRemoved}})
			desktop := PolicyScope{Machine: "desktop"}
			if test.capture != "" {
				if err := session.SetPolicy(desktop, policy.AxisCapture, "packages", "official:firefox", test.capture); err != nil {
					t.Fatal(err)
				}
			}
			if test.restore != "" {
				if err := session.SetPolicy(desktop, policy.AxisRestore, "packages", "official:firefox", test.restore); err != nil {
					t.Fatal(err)
				}
			}
			overview, err := session.Overview(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			item := firefoxItem(t, overview)
			if item.Severity != test.want {
				t.Fatalf("severity = %s, want %s", item.Severity, test.want)
			}
			if got := containsString(overview.Healthy, "packages"); got != test.healthy {
				t.Fatalf("packages healthy = %v, want %v (Healthy=%v)", got, test.healthy, overview.Healthy)
			}
			if test.wantReasons && item.Reason == "" {
				t.Fatal("an intentional difference must say which policy makes it intentional")
			}
		})
	}
}

func TestOverviewNeverHidesDifferencesItCannotAttributeOrThatSafetyBlocks(t *testing.T) {
	blocked := firefoxTarget()
	blocked.CaptureEligible, blocked.RestoreEligible, blocked.SafetyReason = false, false, "hardware-specific"
	for _, test := range []struct {
		name     string
		provider firefoxProvider
		policy   bool
	}{
		{name: "unattributable change", provider: firefoxProvider{target: firefoxTarget(), changes: []model.Change{{Kind: "mystery", Name: "firefox", Summary: "something changed"}}}, policy: true},
		{name: "safety blocks both axes without any policy", provider: firefoxProvider{target: blocked, changes: []model.Change{firefoxRemoved}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := desktopOverviewSession(t, test.provider)
			if test.policy {
				for _, axis := range []policy.Axis{policy.AxisCapture, policy.AxisRestore} {
					if err := session.SetPolicy(PolicyScope{Machine: "desktop"}, axis, "packages", "official:firefox", policy.SettingDisabled); err != nil {
						t.Fatal(err)
					}
				}
			}
			overview, err := session.Overview(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range overview.Items {
				if item.Severity == AttentionIntentional {
					t.Fatalf("difference presented as intentional: %#v", item)
				}
			}
			if containsString(overview.Healthy, "packages") {
				t.Fatal("a category with an actionable difference cannot be healthy")
			}
		})
	}
}

func TestOverviewRanksIntentionalDifferencesLast(t *testing.T) {
	if attentionRank(AttentionIntentional) <= attentionRank(AttentionInfo) {
		t.Fatal("intentional differences must sort after every actionable item")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// configOverviewProvider reports Config's baseline scan alongside its
// profile-aware Diff, as the real Config provider does.
type configOverviewProvider struct {
	scan    configprovider.ScanSummary
	changes []model.Change
}

func (configOverviewProvider) ID() string                 { return "config" }
func (configOverviewProvider) Captured(profile.Data) bool { return true }
func (configOverviewProvider) InspectTargets(context.Context, profile.Data) ([]TargetInspection, error) {
	return nil, nil
}
func (configOverviewProvider) Capture(context.Context, *profile.Data, CaptureContext) (any, []model.Change, error) {
	return nil, nil, nil
}
func (p configOverviewProvider) Diff(context.Context, profile.Data) ([]model.Change, error) {
	return p.changes, nil
}
func (p configOverviewProvider) DiffWithScan(context.Context, profile.Data) ([]model.Change, configprovider.ScanSummary, error) {
	return p.changes, p.scan, nil
}

func TestOverviewListsConfigDifferencesOnlyWhenCaptureOrRestoreWouldAct(t *testing.T) {
	profileDir, stateHome := t.TempDir(), t.TempDir()
	data := profile.New("test", time.Now())
	data.Config.Files = []profile.ConfigFile{{Path: ".config/captured-in-sync"}}
	data.Config.Deletes = []profile.ConfigDelete{{Path: ".config/deleted-and-recorded"}}
	if err := profile.Save(profileDir, data); err != nil {
		t.Fatal(err)
	}
	session, err := Open(Dependencies{Runner: &overviewRunner{}, StateHome: func() (string, error) { return stateHome, nil }}, Options{ProfileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	session.SetProviders([]Provider{configOverviewProvider{
		scan: configprovider.ScanSummary{Candidates: []configprovider.Candidate{
			{Path: ".config/captured-in-sync", Classification: configprovider.ConfigAdded},
			{Path: ".config/new-file", Classification: configprovider.ConfigAdded},
			{Path: ".config/deleted-default", Classification: configprovider.ConfigDeletedBaseline},
			{Path: ".config/deleted-and-recorded", Classification: configprovider.ConfigDeletedBaseline},
			{Path: ".config/ambiguous", Classification: configprovider.ConfigAmbiguousBaseline},
		}},
		changes: []model.Change{{Type: model.ChangeAdd, Provider: "config", Kind: "config", Name: ".config/new-file", Summary: "+ config .config/new-file added"}},
	}})
	overview, err := session.Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, item := range overview.Items {
		counts[item.Ref]++
	}
	for ref, want := range map[string]int{
		".config/captured-in-sync":     0,
		".config/new-file":             1,
		".config/deleted-default":      1,
		".config/deleted-and-recorded": 0,
		".config/ambiguous":            1,
	} {
		if counts[ref] != want {
			t.Errorf("%s listed %d times, want %d (items: %#v)", ref, counts[ref], want, overview.Items)
		}
	}
}
