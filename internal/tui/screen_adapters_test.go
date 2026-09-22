package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
)

func TestNewScreensBuildsPlaceholdersWithoutSession(t *testing.T) {
	got := newScreens(context.Background(), nil)
	for _, id := range screenOrder {
		screen, ok := got[id]
		if !ok {
			t.Fatalf("missing screen %s", id)
		}
		if _, ok := screen.(*placeholderScreen); !ok {
			t.Fatalf("%s = %T, want *placeholderScreen", id, screen)
		}
	}
}

func TestConfigAdapterOwnsPolicyTabNavigation(t *testing.T) {
	screen := &configScreen{Config: screens.NewConfig(nil)}
	if result := screen.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab}); !result.Consumed {
		t.Fatal("Config did not consume Tab for State/Capture/Restore navigation")
	}
	found := false
	for _, action := range screen.Actions() {
		found = found || action.ID == "config.tab"
	}
	if !found {
		t.Fatal("Config actions omitted policy tab navigation")
	}
	foundSet, foundLegacyExclude := false, false
	for _, action := range screen.Actions() {
		foundSet = foundSet || action.ID == "config.policy-set"
		foundLegacyExclude = foundLegacyExclude || action.ID == "config.exclude"
	}
	if !foundSet || foundLegacyExclude {
		t.Fatalf("Capture tab actions did not switch to policy engine controls: %#v", screen.Actions())
	}
}

func TestNewScreensBuildsExpectedAdaptersWithSession(t *testing.T) {
	got := newScreens(context.Background(), integrationSession(t))

	if _, ok := got[ScreenOverview].(*overviewScreen); !ok {
		t.Fatalf("%s = %T, want *overviewScreen", ScreenOverview, got[ScreenOverview])
	}
	if _, ok := got[ScreenCapture].(*captureScreen); !ok {
		t.Fatalf("%s = %T, want *captureScreen", ScreenCapture, got[ScreenCapture])
	}
	if _, ok := got[ScreenConfig].(*configScreen); !ok {
		t.Fatalf("%s = %T, want *configScreen", ScreenConfig, got[ScreenConfig])
	}
	if _, ok := got[ScreenResources].(*resourcesScreen); !ok {
		t.Fatalf("%s = %T, want *resourcesScreen", ScreenResources, got[ScreenResources])
	}
	if _, ok := got[ScreenMachines].(*machinesScreen); !ok {
		t.Fatalf("%s = %T, want *machinesScreen", ScreenMachines, got[ScreenMachines])
	}
	if _, ok := got[ScreenRestore].(*restoreScreen); !ok {
		t.Fatalf("%s = %T, want *restoreScreen", ScreenRestore, got[ScreenRestore])
	}
	if _, ok := got[ScreenSync].(*syncScreen); !ok {
		t.Fatalf("%s = %T, want *syncScreen", ScreenSync, got[ScreenSync])
	}

	for _, id := range []ScreenID{
		ScreenPackages, ScreenThemes, ScreenPlugins,
		ScreenShell, ScreenHooks, ScreenDefaults,
	} {
		if got[id].ID() != id {
			t.Fatalf("%s adapter returned ID %s", id, got[id].ID())
		}
		if _, ok := got[id].(*providerScreen); !ok {
			t.Fatalf("%s = %T, want *providerScreen", id, got[id])
		}
	}
}
