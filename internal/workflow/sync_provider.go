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

// SyncLiveProvider is available on read-bound providers only. Desired histories
// are normalization hints, never permission to capture or restore. Providers
// own physical absence/provenance matching as well as present-value semantics.
type SyncLiveProvider interface {
	SyncObserve(context.Context, profile.Data, []profile.Data) (SyncLiveObservation, error)
}

// SyncNormalizeProvider freezes provider-specific semantic identity using the
// already shared observation. It returns detached historical metadata only;
// no active profile files or authority are changed.
type SyncNormalizeProvider interface {
	NormalizeSyncProfile(context.Context, profile.Data) (profile.Data, error)
}
type SyncLiveObservation struct {
	Targets     []syncengine.DesiredTarget
	Inspections []TargetInspection
	Blockers    []syncengine.Blocker
}

func cloneSyncHistories(ds []profile.Data) []profile.Data {
	out := make([]profile.Data, len(ds))
	for i, d := range ds {
		out[i] = profile.CloneData(d)
	}
	return out
}
