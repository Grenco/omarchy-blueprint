package screens

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/tui/components"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestProfileHasCapturedState(t *testing.T) {
	fresh := profile.New("fresh", time.Unix(0, 0))
	if profileHasCapturedState(fresh) {
		t.Fatal("fresh profile reported captured state")
	}

	fresh.Manifest.Capture.Hooks = true
	if !profileHasCapturedState(fresh) {
		t.Fatal("captured hooks were not detected")
	}
}

func TestProviderEmptyStateDistinguishesUncapturedFromKnownEmptyHooks(t *testing.T) {
	uncaptured, ok := providerEmptyState("hooks", workflow.ProviderStatus{ID: "hooks"}, "Saved")
	if !ok || uncaptured.Heading != "Hooks are not saved in this profile yet" {
		t.Fatalf("uncaptured hooks copy=%#v ok=%v", uncaptured, ok)
	}
	if strings.Contains(strings.ToLower(uncaptured.Explanation+" "+uncaptured.Guidance), "no hooks found") {
		t.Fatalf("uncaptured copy claims live inspection: %#v", uncaptured)
	}

	empty, ok := providerEmptyState("hooks", workflow.ProviderStatus{
		ID:       "hooks",
		Captured: true,
		Snapshot: profile.Hooks{},
	}, "Saved")
	if !ok || empty.Heading != "No hooks to carry" {
		t.Fatalf("captured-empty hooks copy=%#v ok=%v", empty, ok)
	}
	if !strings.Contains(empty.Guidance, "completely normal") {
		t.Fatalf("known-empty hooks do not reassure: %#v", empty)
	}
}

func TestProviderEmptyStateDoesNotReplaceCapturedChangesTab(t *testing.T) {
	_, ok := providerEmptyState("hooks", workflow.ProviderStatus{
		ID:       "hooks",
		Captured: true,
		Snapshot: profile.Hooks{},
	}, "Changes")
	if ok {
		t.Fatal("captured Changes tab must keep normal difference presentation")
	}
}

func TestRenderEmptyStateWrapsToWorkspaceWidth(t *testing.T) {
	view := renderEmptyState(
		components.NewStyles(components.ThemePalette{}),
		32,
		emptyStateCopy{
			Heading:     "No hooks to carry",
			Explanation: "Hooks are scripts Omarchy runs automatically at supported events.",
			Guidance:    "Having no hooks is completely normal. There is nothing to set up here.",
		},
	)
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 32 {
			t.Fatalf("line width=%d > 32: %q", lipgloss.Width(line), line)
		}
	}
}

func TestUncapturedProviderCopyCoversGenericCategoryScreens(t *testing.T) {
	tests := map[string]string{
		"packages": "Packages are not saved in this profile yet",
		"themes":   "Themes are not saved in this profile yet",
		"plugins":  "Plugins are not saved in this profile yet",
		"defaults": "Defaults are not saved in this profile yet",
		"shell":    "Shell state is not saved in this profile yet",
		"hooks":    "Hooks are not saved in this profile yet",
	}
	for id, heading := range tests {
		copy, ok := uncapturedProviderCopy(id)
		if !ok || copy.Heading != heading {
			t.Errorf("%s copy=%#v ok=%v", id, copy, ok)
		}
	}
}

func TestProviderEmptyStateCapturedEmptySemanticsAndPopulatedSuppression(t *testing.T) {
	tests := []struct {
		id       string
		snapshot any
		wantOK   bool
	}{
		{"packages", profile.Packages{}, true},
		{"packages", profile.Packages{Official: []string{"git"}}, false},
		{"themes", profile.Themes{}, true},
		{"themes", profile.Themes{Current: "tokyo-night"}, false},
		{"plugins", profile.Plugins{}, true},
		{"plugins", profile.Plugins{Items: []profile.Plugin{{ID: "example"}}}, false},
		{"hooks", profile.Hooks{}, true},
		{"hooks", profile.Hooks{Items: []profile.Hook{{Path: "pre-restore"}}}, false},
		{"shell", profile.Shell{}, true},
		{"shell", profile.Shell{Hash: "abc"}, false},
	}
	for _, test := range tests {
		_, ok := providerEmptyState(test.id, workflow.ProviderStatus{
			ID:       test.id,
			Captured: true,
			Snapshot: test.snapshot,
		}, "Saved")
		if ok != test.wantOK {
			t.Errorf("%s snapshot=%#v ok=%v, want %v", test.id, test.snapshot, ok, test.wantOK)
		}
	}
}

