package tui

import (
	"context"
	"testing"
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
