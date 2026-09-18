package tui

import (
	"context"

	"github.com/Grenco/omarchy-blueprint/internal/tui/screens"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// newScreens is the single construction point for the fixed set of shipped
// screens. It installs placeholders when no session is available and
// concrete screens, wrapped in their root-facing adapters, otherwise.
func newScreens(ctx context.Context, session *workflow.Session) map[ScreenID]screen {
	screenMap := make(map[ScreenID]screen, len(screenOrder))
	for _, id := range screenOrder {
		screenMap[id] = &placeholderScreen{id: id}
	}
	if session == nil {
		return screenMap
	}

	screenMap[ScreenOverview] = &overviewScreen{Overview: screens.NewOverviewContext(ctx, session)}
	screenMap[ScreenCapture] = &captureScreen{Capture: screens.NewCaptureContext(ctx, session)}
	screenMap[ScreenConfig] = &configScreen{Config: screens.NewConfigContext(ctx, session)}
	screenMap[ScreenResources] = &resourcesScreen{Resources: screens.NewResourcesContext(ctx, session)}
	screenMap[ScreenMachines] = &machinesScreen{Machines: screens.NewMachinesContext(ctx, session)}
	screenMap[ScreenRestore] = &restoreScreen{Restore: screens.NewRestoreContext(ctx, session)}
	screenMap[ScreenSync] = &syncScreen{Sync: screens.NewSyncContext(ctx, session)}

	for _, id := range []ScreenID{
		ScreenPackages,
		ScreenThemes,
		ScreenPlugins,
		ScreenShell,
		ScreenHooks,
		ScreenDefaults,
	} {
		screenMap[id] = &providerScreen{
			Provider: screens.NewProviderContext(ctx, session, string(id)),
			id:       id,
		}
	}
	return screenMap
}
