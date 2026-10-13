package workflow

import (
	"context"
	"fmt"
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
	Targets            []syncengine.DesiredTarget
	Inspections        []TargetInspection
	DesiredInspections []SyncDesiredInspection
	Blockers           []syncengine.Blocker
}

// SyncDesiredInspection binds safety to semantic intent, not merely a target
// identity. Providers derive it from the same frozen live facts. It is read
// evidence only, never permission to execute Restore.
type SyncDesiredInspection struct {
	Key        syncengine.TargetKey
	Desired    syncengine.State
	Inspection TargetInspection
}

func BindSyncDesiredInspections(targets []syncengine.DesiredTarget, inspections []TargetInspection) ([]SyncDesiredInspection, error) {
	byKey := map[string]TargetInspection{}
	for _, t := range cloneTargets(inspections) {
		byKey[t.Key] = t
	}
	var out []SyncDesiredInspection
	for _, target := range targets {
		t, ok := byKey[target.Key.Target]
		if !ok {
			return nil, fmt.Errorf("sync: desired target lacks provider safety inspection")
		}
		out = append(out, SyncDesiredInspection{Key: target.Key, Desired: target.State, Inspection: t})
	}
	return out, nil
}

func cloneSyncHistories(ds []profile.Data) []profile.Data {
	out := make([]profile.Data, len(ds))
	for i, d := range ds {
		out[i] = profile.CloneData(d)
	}
	return out
}
