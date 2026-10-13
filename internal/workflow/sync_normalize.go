package workflow

import (
	"context"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"sort"
)

// Every revision uses one frozen provider context, never a different live
// catalogue per historical revision. Returned views retain exact SHA/root
// evidence while their detached metadata is semantically canonicalized.
func (r *ReadCycle) normalizeSyncViews(ctx context.Context, views map[string]SyncProfileView, providers []Provider) error {
	release, err := r.observations.BeginWork()
	if err != nil {
		return err
	}
	defer release()
	ctx, stop := r.withContext(ctx)
	defer stop()
	keys := make([]string, 0, len(views))
	for sha := range views {
		keys = append(keys, sha)
	}
	sort.Strings(keys)
	for _, sha := range keys {
		v := views[sha]
		d := profile.CloneData(v.Profile)
		for _, p := range r.providers {
			if n, ok := readCapabilitySource(p).(SyncNormalizeProvider); ok {
				d, err = n.NormalizeSyncProfile(ctx, d)
				if err != nil {
					return err
				}
			}
		}
		if err := r.check(ctx); err != nil {
			return err
		}
		v.Profile = d
		v.Targets, err = projectSyncProfile(d, providers)
		if err != nil {
			return err
		}
		v.Control, err = syncControl(d, r.machine.Name)
		if err != nil {
			return err
		}
		views[sha] = v
	}
	return r.check(ctx)
}
