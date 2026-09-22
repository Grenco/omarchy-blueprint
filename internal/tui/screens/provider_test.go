package screens

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestProviderUncapturedHooksExplainsPurposeWithoutClaimingKnownEmpty(t *testing.T) {
	screen := &Provider{
		id:     "hooks",
		width:  80,
		status: workflow.ProviderStatus{ID: "hooks"},
	}

	view := screen.View()
	for _, want := range []string{
		"Hooks are not saved in this profile yet",
		"scripts Omarchy runs automatically",
		"Capture Hooks only when you want Blueprint to remember them.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("uncaptured hooks missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "No hooks to carry") {
		t.Fatalf("uncaptured hooks were presented as known-empty:\n%s", view)
	}
}

func TestProviderCapturedEmptyHooksReassuresUser(t *testing.T) {
	screen := &Provider{
		id:    "hooks",
		width: 80,
		status: workflow.ProviderStatus{
			ID:       "hooks",
			Captured: true,
			Snapshot: profile.Hooks{},
		},
	}

	view := screen.View()
	for _, want := range []string{
		"No hooks to carry",
		"same automation is available",
		"completely normal",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("captured-empty hooks missing %q:\n%s", want, view)
		}
	}
}

func TestProviderUncapturedChangesTabDoesNotClaimNoDifferences(t *testing.T) {
	screen := &Provider{
		id:     "plugins",
		tab:    "Changes",
		width:  80,
		status: workflow.ProviderStatus{ID: "plugins"},
	}

	view := screen.View()
	if !strings.Contains(view, "Plugins are not saved in this profile yet") {
		t.Fatalf("uncaptured Changes tab lost guidance:\n%s", view)
	}
	if strings.Contains(view, "No differences detected") {
		t.Fatalf("uncaptured category falsely claims a completed diff:\n%s", view)
	}
}

func TestProviderCapturedEmptyShellUsesShellZeroState(t *testing.T) {
	screen := &Provider{
		id:    "shell",
		width: 80,
		status: workflow.ProviderStatus{
			ID:       "shell",
			Captured: true,
			Snapshot: profile.Shell{Version: 1},
		},
	}

	view := screen.View()
	if !strings.Contains(view, "No Blueprint-managed Shell customisation") ||
		!strings.Contains(view, "normal Omarchy shell setup") {
		t.Fatalf("shell zero-state missing:\n%s", view)
	}
}

func TestProviderCapturedEmptyDefaultsKeepsConcreteRows(t *testing.T) {
	screen := &Provider{
		id:    "defaults",
		width: 80,
		status: workflow.ProviderStatus{
			ID:       "defaults",
			Captured: true,
			Snapshot: profile.Defaults{},
		},
	}

	view := screen.View()
	if strings.Contains(view, "nothing to do here") {
		t.Fatalf("Defaults received a generic captured-empty tutorial:\n%s", view)
	}
	if !strings.Contains(view, "Applications") || !strings.Contains(view, "Terminal:") {
		t.Fatalf("Defaults concrete rows disappeared:\n%s", view)
	}
}

func TestProviderDefaultsToStateTypedDataAndCyclesPolicyTabs(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{ID: "packages", Captured: true, Changes: []model.Change{{Summary: "git differs"}}, Snapshot: profile.Packages{Official: []string{"git"}, AUR: []string{"yay"}, Mise: profile.MiseTools{"node": {}}, Excluded: []string{"linux"}}}}
	view := screen.View()
	for _, want := range []string{"[active] State   Capture   Restore", "Official packages", "git", "AUR packages", "yay", "Mise tools", "node"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "git differs") || strings.Contains(view, "{") {
		t.Fatalf("saved tab exposed drift or JSON: %q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if view = screen.View(); !strings.Contains(view, "[active] Capture") || !strings.Contains(view, "Include") {
		t.Fatalf("capture tab=%q", view)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if view = screen.View(); !strings.Contains(view, "[active] Restore") || !strings.Contains(view, "Apply") {
		t.Fatalf("restore tab=%q", view)
	}
}

func TestProviderPolicyTabsExposeCaptureAndRestoreIntent(t *testing.T) {
	screen := &Provider{id: "packages", tab: "Capture", width: 80, status: workflow.ProviderStatus{ID: "packages", Captured: true}}
	view := screen.View()
	for _, want := range []string{"State", "Capture", "Restore", "Profile defaults"} {
		if !strings.Contains(view, want) {
			t.Fatalf("Capture policy view missing %q:\n%s", want, view)
		}
	}
}

func TestEveryPolicyScreenCanSelectProfileDefaultsWithoutChangingActiveMachine(t *testing.T) {
	screens := []struct {
		name   string
		screen interface {
			Update(tea.Msg) tea.Cmd
			View() string
		}
	}{
		{name: "provider", screen: &Provider{id: "packages", tab: "Capture", policyScope: workflow.PolicyScope{Machine: "desktop"}}},
		{name: "config", screen: &Config{tab: "Capture", policyScope: workflow.PolicyScope{Machine: "desktop"}}},
		{name: "resources", screen: &Resources{tab: "Capture", policyScope: workflow.PolicyScope{Machine: "desktop"}}},
	}
	for _, test := range screens {
		t.Run(test.name, func(t *testing.T) {
			if view := test.screen.View(); !strings.Contains(view, "Machine: desktop") {
				t.Fatalf("initial named scope missing:\n%s", view)
			}
			test.screen.Update(tea.KeyPressMsg{Code: 'p'})
			if view := test.screen.View(); !strings.Contains(view, "Profile defaults") || strings.Contains(view, "Machine: desktop") {
				t.Fatalf("profile-default scope unavailable:\n%s", view)
			}
		})
	}
}

func TestProviderPolicyRowsPreservePackageGroupsAndDesiredAbsence(t *testing.T) {
	screen := &Provider{
		id:    "packages",
		tab:   "State",
		width: 80,
		targets: []workflow.TargetInspection{
			{Key: "official:git", Label: "git", Desired: workflow.TargetPresent, Current: workflow.TargetPresent},
			{Key: "aur:foo", Label: "foo", Desired: workflow.TargetAbsent, Current: workflow.TargetPresent},
			{Key: "mise:node", Label: "node", Desired: workflow.TargetPresent, Current: workflow.TargetAbsent},
			{Key: "preinstall:tailscale", Label: "tailscale", Desired: workflow.TargetAbsent, Current: workflow.TargetPresent},
		},
	}
	view := screen.View()
	for _, want := range []string{"Official packages", "AUR packages", "Mise tools", "Omarchy preinstalls", "foo — desired absent; current present", "tailscale — desired absent; current present"} {
		if !strings.Contains(view, want) {
			t.Fatalf("state policy view missing %q:\n%s", want, view)
		}
	}
}

func TestProviderRestorePolicyShowsSafetyBlockSeparately(t *testing.T) {
	screen := &Provider{
		id: "defaults", tab: "Restore", width: 80,
		targets:   []workflow.TargetInspection{{Key: "agent", Label: "agent", RestoreEligible: false, SafetyReason: "automatic set-only restore is not currently safe"}},
		effective: map[string]policy.Effective{"agent": {Restore: policy.EffectiveSetting{Enabled: false, Explicit: true}}},
	}
	view := screen.View()
	if !strings.Contains(view, "Blocked: automatic set-only restore is not currently safe") || strings.Contains(view, "agent — Skip") {
		t.Fatalf("restore safety block was confused with a policy skip:\n%s", view)
	}
}

func TestProviderPolicyDetailsShowBlockSourceAndCapabilities(t *testing.T) {
	target := workflow.TargetInspection{
		Key: "agent", Label: "agent", Desired: workflow.TargetPresent, Current: workflow.TargetAbsent,
		CaptureEligible: true, RestoreEligible: false, SafetyReason: "automatic restore is unsafe",
		Capabilities: workflow.TargetCapabilities{SupportsCapture: true, SupportsRestore: false, SupportsDesiredAbsence: true, SupportsExactRemoval: true},
	}
	screen := &Provider{
		id: "defaults", tab: "Restore", targets: []workflow.TargetInspection{target},
		effective: map[string]policy.Effective{"agent": {Restore: policy.EffectiveSetting{Enabled: false, Explicit: true, Source: policy.Source{Kind: policy.SourceProfileTarget, Category: "defaults", Target: "agent"}}}},
	}
	screen.list.Selected = 1 // group heading is row zero
	detail := screen.DetailView()
	for _, want := range []string{"Blocked: automatic restore is unsafe", "Source: profile-target", "Supports capture: true", "Supports restore: false", "Supports desired absence: true", "Supports Exact removal: true"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("policy detail missing %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "Skip (explicit)") {
		t.Fatalf("blocked target appears as an intentional skip:\n%s", detail)
	}
}

func TestProviderLongChangesKeepSelectedRowVisible(t *testing.T) {
	changes := make([]model.Change, 6)
	for i := range changes {
		changes[i] = model.Change{Summary: "change " + string(rune('a'+i))}
	}
	screen := &Provider{height: 3, tab: "Changes", status: workflow.ProviderStatus{Captured: true, Changes: changes}}
	for range changes {
		screen.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := screen.View()
	if !strings.Contains(view, "> change f") || strings.Contains(view, "change a") {
		t.Fatalf("viewport wrong: %q", view)
	}
}

func TestProviderSnapshotsRenderTypedSummariesNotJSON(t *testing.T) {
	for _, snapshot := range []any{profile.Packages{Official: []string{"git"}}, profile.Themes{Current: "nord"}, profile.Plugins{Items: []profile.Plugin{{ID: "clock"}}}, profile.Defaults{Terminal: "foot"}, profile.Shell{Version: 1, Hash: "hash"}, profile.Hooks{Items: []profile.Hook{{Path: "hook"}}}} {
		view := (&Provider{tab: "Saved", status: workflow.ProviderStatus{Captured: true, Snapshot: snapshot}}).View()
		if strings.Contains(view, "{") || strings.Contains(view, "\"") {
			t.Fatalf("snapshot rendered as JSON: %q", view)
		}
	}
}

func TestProviderCollapsesSavedGroup(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{Captured: true, Snapshot: profile.Packages{Official: []string{"git", "curl"}}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := screen.View()
	if !strings.Contains(view, "Official packages") || strings.Contains(view, "git") || strings.Contains(view, "curl") {
		t.Fatalf("collapsed view=%q", view)
	}
}

func TestProviderGroupNavigationPreservesCollapsedGroups(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{Captured: true, Snapshot: profile.Packages{Official: []string{"git"}, AUR: []string{"yay"}, Mise: profile.MiseTools{"node": {}}}}}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // Official package child.
	screen.Update(tea.KeyPressMsg{Code: '['})
	if screen.selected != 0 {
		t.Fatalf("[ selected %d, want current group heading", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: ']'})
	if screen.selected != 2 {
		t.Fatalf("] selected %d, want next group heading", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !screen.collapsed["AUR packages"] {
		t.Fatal("enter did not collapse group")
	}
	screen.Update(tea.KeyPressMsg{Code: ']'})
	if screen.selected != 3 || !screen.collapsed["AUR packages"] {
		t.Fatalf("jump changed collapsed state or selected %d", screen.selected)
	}
	screen.Update(tea.KeyPressMsg{Code: '['})
	screen.Update(tea.KeyPressMsg{Code: '['})
	if screen.selected != 0 {
		t.Fatalf("repeated [ selected %d, want no-wrap first group", screen.selected)
	}
}

// TestProviderShowsOnlyIncludedPackages is updated for the cutover away
// from Packages.Excluded: an excluded package/tool has no desired state at
// all now (Session.SetPackageExcluded strips it from Official/AUR/Mise
// immediately), so it simply does not appear in the list -- there is no
// longer a separate "not included" marker row to render.
func TestProviderShowsOnlyIncludedPackages(t *testing.T) {
	screen := &Provider{id: "packages", status: workflow.ProviderStatus{Captured: true, Snapshot: profile.Packages{Official: []string{"git"}}}}
	view := screen.View()
	if !strings.Contains(view, "+ git") {
		t.Fatalf("view missing included package: %q", view)
	}
	if strings.Contains(view, "1password") {
		t.Fatalf("view rendered an excluded package that has no desired state: %q", view)
	}
}

func TestProviderWordingUsesPresentationOnlySourceLabels(t *testing.T) {
	snapshot := profile.Plugins{Items: []profile.Plugin{{ID: "clock", Source: "builtin", Enabled: true}, {ID: "repo", Source: "git", Enabled: true}, {ID: "private", Source: "local"}}}
	view := (&Provider{id: "plugins", status: workflow.ProviderStatus{Captured: true, Snapshot: snapshot}}).View()
	for _, want := range []string{"Built-in plugins", "clock", "Installed plugins", "repo", "Local plugins", "private"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Git plugins") || snapshot.Items[1].Source != "git" {
		t.Fatalf("source presentation changed stored value: view=%q source=%q", view, snapshot.Items[1].Source)
	}

	themes := profile.Themes{Items: []profile.Theme{{ID: "nord", Type: "git"}}}
	view = (&Provider{id: "themes", status: workflow.ProviderStatus{Captured: true, Snapshot: themes}}).View()
	if !strings.Contains(view, "nord (installed)") || strings.Contains(view, "nord (git)") || themes.Items[0].Type != "git" {
		t.Fatalf("theme source presentation changed stored value: view=%q type=%q", view, themes.Items[0].Type)
	}
}

func TestProviderScreenIgnoresStaleStatus(t *testing.T) {
	screen := &Provider{requestID: 2, busy: true}
	screen.Update(providerStatusMsg{requestID: 1, status: workflow.ProviderStatus{ID: "stale"}})
	if !screen.busy || screen.status.ID != "" {
		t.Fatalf("stale status changed state: busy=%v status=%#v", screen.busy, screen.status)
	}
}

func TestProviderScreenSanitizesProfileValues(t *testing.T) {
	screen := &Provider{id: "hooks", status: workflow.ProviderStatus{Captured: true, Snapshot: profile.Hooks{Items: []profile.Hook{{Path: "hook\npath\x1b"}}}}}
	view := screen.View()
	if strings.Contains(view, "\x1b") || !strings.Contains(view, "hook?path?") {
		t.Fatalf("unsafe provider view=%q", view)
	}
}
