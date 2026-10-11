package workflow

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/profilegit"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"os"
	"sort"
)

// SyncProfileView is private-lifetime historical evidence, never JSON output or
// an execution plan. Its Root is an isolated materialization, not the checkout.
type SyncProfileView struct {
	Revision string
	Root     string
	Profile  profile.Data
	Targets  []syncengine.DesiredTarget
	Control  SyncControlState
}

func (s *Session) loadSyncProfileView(ctx context.Context, git profilegit.Service, sha string) (SyncProfileView, func(), error) {
	s.configMu.Lock()
	providers := append([]Provider(nil), s.providers...)
	name := s.machine.Name
	s.configMu.Unlock()
	return loadSyncView(ctx, git, sha, providers, name)
}

func loadSyncView(ctx context.Context, git profilegit.Service, sha string, providers []Provider, machine string) (SyncProfileView, func(), error) {
	if err := ctx.Err(); err != nil {
		return SyncProfileView{}, nil, err
	}
	root, err := os.MkdirTemp("", "blueprint-sync-revision-*")
	if err != nil {
		return SyncProfileView{}, nil, fmt.Errorf("sync: create historical profile view")
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()
	if err := git.MaterializeCommit(ctx, sha, root); err != nil {
		return SyncProfileView{}, nil, err
	}
	data, err := profile.Load(root)
	if err != nil {
		return SyncProfileView{}, nil, fmt.Errorf("sync: historical profile is invalid")
	}
	if err := validateLoadedPolicyTargets(providers, data); err != nil {
		return SyncProfileView{}, nil, fmt.Errorf("sync: historical policy targets are invalid")
	}
	targets, err := projectSyncProfile(data, providers)
	if err != nil {
		return SyncProfileView{}, nil, err
	}
	control, err := syncControl(data, machine)
	if err != nil {
		return SyncProfileView{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return SyncProfileView{}, nil, err
	}
	success = true
	return SyncProfileView{Revision: sha, Root: root, Profile: data, Targets: targets, Control: control}, cleanup, nil
}

func syncCapturedCategories(d profile.Data) map[string]bool {
	c := d.Manifest.Capture
	return map[string]bool{"packages": c.Packages, "themes": c.Themes, "plugins": c.Plugins, "config": c.Config, "defaults": c.Defaults, "shell": c.Shell, "hooks": c.Hooks, "resources": c.Resources, "services": c.Services}
}

func projectSyncProfile(data profile.Data, providers []Provider) ([]syncengine.DesiredTarget, error) {
	registered := map[string]bool{}
	seen := map[syncengine.TargetKey]bool{}
	out := []syncengine.DesiredTarget{}
	for _, p := range providers {
		id := p.ID()
		if registered[id] {
			return nil, fmt.Errorf("sync: duplicate provider")
		}
		registered[id] = true
		v, ok := p.(SyncDesiredProvider)
		if !ok {
			if p.Captured(data) {
				return nil, fmt.Errorf("sync: captured provider %s lacks semantic projection", id)
			}
			continue
		}
		items, err := v.SyncDesired(profile.CloneData(data))
		if err != nil {
			return nil, fmt.Errorf("sync: provider %s desired projection failed", id)
		}
		for _, x := range items {
			if x.Key.Category != id || x.Key.Target == "" || seen[x.Key] {
				return nil, fmt.Errorf("sync: provider %s returned invalid target identity", id)
			}
			if validator, ok := p.(targetValidator); ok {
				key, err := validator.ValidateTarget(x.Key.Target)
				if err != nil || key != x.Key.Target {
					return nil, fmt.Errorf("sync: provider %s returned invalid policy identity", id)
				}
			}
			seen[x.Key] = true
			out = append(out, x)
		}
	}
	for id, captured := range syncCapturedCategories(data) {
		if captured && !registered[id] {
			return nil, fmt.Errorf("sync: captured provider %s is unavailable", id)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key.Category != out[j].Key.Category {
			return out[i].Key.Category < out[j].Key.Category
		}
		return out[i].Key.Target < out[j].Key.Target
	})
	return out, nil
}
