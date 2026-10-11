package workflow

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
)

// SyncDesiredProvider projects historical desired state without live I/O.
type SyncDesiredProvider interface {
	ID() string
	SyncDesired(profile.Data) ([]syncengine.DesiredTarget, error)
}

// SyncComposeProvider owns one target and its referenced artifacts. Its
// destination is always an owned disposable staging tree, never a checkout.
type SyncComposeProvider interface {
	SyncDesiredProvider
	OverlaySyncTarget(context.Context, *syncprofile.Builder, syncprofile.View, string) error
}
