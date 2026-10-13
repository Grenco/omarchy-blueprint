package defaults

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
)

// OverlaySyncTarget modifies only disposable staging, never the active profile.
func (Provider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, view syncprofile.View, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !view.Profile.Manifest.Capture.Defaults {
		view.Profile.Defaults = profile.Defaults{}
	}
	switch target {
	case "browser":
		b.Profile.Defaults.Browser = view.Profile.Defaults.Browser
	case "terminal":
		b.Profile.Defaults.Terminal = view.Profile.Defaults.Terminal
	case "editor":
		b.Profile.Defaults.Editor = view.Profile.Defaults.Editor
	case "agent":
		b.Profile.Defaults.Agent = view.Profile.Defaults.Agent
	default:
		return fmt.Errorf("sync: invalid default target")
	}
	b.Profile.Manifest.Capture.Defaults = b.Profile.Manifest.Capture.Defaults || view.Profile.Manifest.Capture.Defaults
	return nil
}
