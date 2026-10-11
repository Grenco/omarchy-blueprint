package workflow

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
	"os"
	"path/filepath"
	"sort"
)

func (s *Session) composeSyncProfileTree(ctx context.Context, base, local, remote SyncProfileView, resolutions map[syncengine.TargetKey]syncengine.Resolution) (string, func(), error) {
	s.configMu.Lock()
	providers := append([]Provider(nil), s.providers...)
	name := s.machine.Name
	s.configMu.Unlock()
	return composeSyncTree(ctx, base, local, remote, resolutions, providers, name)
}

func syncTargetMap(targets []syncengine.DesiredTarget) map[syncengine.TargetKey]syncengine.State {
	out := map[syncengine.TargetKey]syncengine.State{}
	for _, x := range targets {
		out[x.Key] = x.State
	}
	return out
}
func syncState(states map[syncengine.TargetKey]syncengine.State, key syncengine.TargetKey) syncengine.State {
	if x, ok := states[key]; ok {
		return x
	}
	return syncengine.State{Kind: syncengine.StateUnmanaged}
}
func syncKeys(maps ...map[syncengine.TargetKey]syncengine.State) []syncengine.TargetKey {
	set := map[syncengine.TargetKey]bool{}
	for _, m := range maps {
		for k := range m {
			set[k] = true
		}
	}
	keys := make([]syncengine.TargetKey, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Category != keys[j].Category {
			return keys[i].Category < keys[j].Category
		}
		return keys[i].Target < keys[j].Target
	})
	return keys
}

func composeSyncTree(ctx context.Context, base, local, remote SyncProfileView, resolutions map[syncengine.TargetKey]syncengine.Resolution, providers []Provider, selected string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	useLocalControl, blocked := chooseSyncControl(base.Control, local.Control, remote.Control)
	if blocked {
		return "", nil, fmt.Errorf("sync: two-sided control-plane disagreement requires external resolution")
	}
	b, err := syncprofile.NewBuilder(remote.Profile)
	if err != nil {
		return "", nil, fmt.Errorf("sync: create disposable composition")
	}
	success := false
	defer func() {
		if !success {
			b.Close()
		}
	}()
	// Only the managed tree is composed; unrelated repository files are never
	// silently selected for a later semantic merge transaction.
	for _, rel := range profile.ManagedTopLevelPaths {
		if _, err := os.Lstat(filepath.Join(remote.Root, rel)); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", nil, fmt.Errorf("sync: remote managed tree cannot be read")
		}
		if err := b.ReplaceArtifact(ctx, remote.Root, rel); err != nil {
			return "", nil, err
		}
	}
	bm, lm, rm := syncTargetMap(base.Targets), syncTargetMap(local.Targets), syncTargetMap(remote.Targets)
	expected := map[syncengine.TargetKey]syncengine.State{}
	seen := map[syncengine.TargetKey]bool{}
	for _, key := range syncKeys(bm, lm, rm) {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		seen[key] = true
		bs, ls, rs := syncState(bm, key), syncState(lm, key), syncState(rm, key)
		class, err := syncengine.ClassifyHistory(bs, ls, rs)
		if err != nil {
			return "", nil, err
		}
		useLocal := class == syncengine.HistoryLocal
		resolution := resolutions[key]
		if class == syncengine.HistoryConflict {
			switch resolution {
			case syncengine.ResolutionKeepLocal:
				useLocal = true
			case syncengine.ResolutionAcceptRemote:
				useLocal = false
			default:
				return "", nil, fmt.Errorf("sync: unresolved semantic conflict prevents composition")
			}
		} else if resolution != "" && resolution != syncengine.ResolutionNone {
			return "", nil, fmt.Errorf("sync: resolution does not address a semantic conflict")
		}
		expected[key] = rs
		if useLocal {
			p, ok := ProviderByID(providers, key.Category)
			if !ok {
				return "", nil, fmt.Errorf("sync: composition provider unavailable")
			}
			composer, ok := p.(SyncComposeProvider)
			if !ok {
				return "", nil, fmt.Errorf("sync: provider %s lacks semantic composition", key.Category)
			}
			if err := composer.OverlaySyncTarget(ctx, b, syncprofile.View{Root: local.Root, Profile: profile.CloneData(local.Profile)}, key.Target); err != nil {
				return "", nil, fmt.Errorf("sync: provider %s target composition failed", key.Category)
			}
			expected[key] = ls
		}
	}
	for key := range resolutions {
		if !seen[key] {
			return "", nil, fmt.Errorf("sync: resolution references unknown target")
		}
	}
	controlSource := remote.Profile
	if useLocalControl {
		controlSource = local.Profile
	}
	overlaySyncControl(&b.Profile, controlSource)
	if err := b.RemoveArtifact("machines"); err != nil {
		return "", nil, err
	}
	if err := profile.Save(b.Root(), b.Profile); err != nil {
		return "", nil, fmt.Errorf("sync: composed profile cannot be saved")
	}
	loaded, err := profile.Load(b.Root())
	if err != nil {
		return "", nil, fmt.Errorf("sync: composed profile is invalid")
	}
	actual, err := projectSyncProfile(loaded, providers)
	if err != nil {
		return "", nil, err
	}
	am := syncTargetMap(actual)
	for _, key := range syncKeys(expected, am) {
		if !syncState(expected, key).Equal(syncState(am, key)) {
			return "", nil, fmt.Errorf("sync: composition semantic round-trip mismatch")
		}
	}
	actualControl, err := syncControl(loaded, selected)
	if err != nil {
		return "", nil, err
	}
	wantControl := remote.Control
	if useLocalControl {
		wantControl = local.Control
	}
	if actualControl.Fingerprint != wantControl.Fingerprint {
		return "", nil, fmt.Errorf("sync: composition control-plane round-trip mismatch")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	success = true
	return b.Root(), b.Close, nil
}
