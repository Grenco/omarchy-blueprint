package app

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func (p restoreProviderAdapter) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	observer, ok := p.stateProvider.(workflow.SyncLiveProvider)
	if !ok {
		return workflow.SyncLiveObservation{}, fmt.Errorf("sync: provider %s lacks live semantic observation", p.ID())
	}
	return observer.SyncObserve(ctx, hints, histories)
}

func syncDerivedLive(category string, hints profile.Data, histories []profile.Data, current profile.Data, projection func(profile.Data) ([]syncengine.DesiredTarget, error), inspect func(profile.Data) ([]workflow.TargetInspection, error)) (workflow.SyncLiveObservation, error) {
	normalized, err := projection(current)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	inspections, err := inspect(hints)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	for _, d := range histories {
		part, err := inspect(d)
		if err != nil {
			return workflow.SyncLiveObservation{}, err
		}
		inspections = append(inspections, part...)
	}
	variants, err := syncHistories(histories, projection)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	return syncLiveFacts(category, normalized, inspections, variants)
}

func (p themesStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	detector, err := p.provider()
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	current, err := detector.Detect(ctx)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	d := profile.CloneData(hints)
	d.Themes = current
	d.Manifest.Capture.Themes = true
	return syncDerivedLive("themes", hints, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromThemes(d, current) })
}

func (p pluginsStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	detector, err := p.provider()
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	current, err := detector.Detect(ctx)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	builtinWanted := map[string]bool{}
	for _, d := range append([]profile.Data{hints}, histories...) {
		for _, x := range d.Plugins.Items {
			if x.Source == "" || x.Source == "builtin" {
				builtinWanted[x.ID] = true
			}
		}
	}
	normalized := profile.Plugins{}
	for _, x := range current.Items {
		if x.Source != "" && x.Source != "builtin" || builtinWanted[x.ID] {
			normalized.Items = append(normalized.Items, x)
		}
	}
	d := profile.CloneData(hints)
	d.Plugins = normalized
	d.Manifest.Capture.Plugins = true
	return syncDerivedLive("plugins", hints, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromPlugins(d, current) })
}

func (p defaultsStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	d := profile.CloneData(hints)
	d.Defaults = current
	d.Manifest.Capture.Defaults = true
	return syncDerivedLive("defaults", hints, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromDefaults(d, current) })
}

func (p shellStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	detector, err := p.provider()
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	current, err := detector.Detect()
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	d := profile.CloneData(hints)
	d.Shell = profile.Shell{Version: current.Version, Hash: current.Hash, BaselineHash: current.BaselineHash}
	d.Manifest.Capture.Shell = true
	return syncDerivedLive("shell", hints, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromShell(d, current) })
}

func (p hooksStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	detector, err := p.provider(hints.Resources)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	current, err := detector.Detect()
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	d := profile.CloneData(hints)
	d.Hooks = profile.Hooks{}
	d.Manifest.Capture.Hooks = true
	for _, x := range current.Items {
		d.Hooks.Items = append(d.Hooks.Items, profile.Hook{Path: x.Path, Hash: x.Hash, Mode: x.Mode})
	}
	return syncDerivedLive("hooks", hints, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromHooks(d, current) })
}

func (p resourcesStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	union := profile.CloneData(hints)
	seen := map[string]bool{}
	for _, r := range union.Resources.Items {
		seen[r.ID] = true
	}
	for _, d := range histories {
		for _, r := range d.Resources.Items {
			if !seen[r.ID] {
				seen[r.ID] = true
				union.Resources.Items = append(union.Resources.Items, r)
			}
		}
	}
	detector, err := p.provider(union)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	current, _, err := detector.Detect(ctx, union.Resources)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	d := profile.CloneData(union)
	d.Resources = current
	d.Manifest.Capture.Resources = true
	return syncDerivedLive("resources", union, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromResources(d, current) })
}

func (p configStateProvider) SyncObserve(ctx context.Context, hints profile.Data, histories []profile.Data) (workflow.SyncLiveObservation, error) {
	union := profile.CloneData(hints)
	seen := map[string]bool{}
	for _, r := range union.Config.Files {
		seen[r.Path] = true
	}
	for _, r := range union.Config.Deletes {
		seen[r.Path] = true
	}
	for _, d := range histories {
		for _, r := range d.Config.Files {
			if !seen[r.Path] {
				seen[r.Path] = true
				union.Config.Files = append(union.Config.Files, r)
			}
		}
		for _, r := range d.Config.Deletes {
			if !seen[r.Path] {
				seen[r.Path] = true
				union.Config.Deletes = append(union.Config.Deletes, r)
			}
		}
	}
	detector, err := p.provider(union)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	scan, err := detector.Scan(union.Config)
	if err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return workflow.SyncLiveObservation{}, err
	}
	d := profile.CloneData(union)
	d.Config.Files = nil
	d.Config.Deletes = nil
	d.Manifest.Capture.Config = true
	for _, x := range scan.Candidates {
		switch x.Classification {
		case configprovider.ConfigModifiedBaseline, configprovider.ConfigAdded:
			d.Config.Files = append(d.Config.Files, profile.ConfigFile{Path: x.Path, Hash: x.UserHash, Mode: x.UserMode, BaselineHash: x.BaselineHash, BaselineMode: x.BaselineMode})
		case configprovider.ConfigDeletedBaseline:
			d.Config.Deletes = append(d.Config.Deletes, profile.ConfigDelete{Path: x.Path, BaselineHash: x.BaselineHash, BaselineMode: x.BaselineMode})
		}
	}
	return syncDerivedLive("config", union, histories, d, p.SyncDesired, func(d profile.Data) ([]workflow.TargetInspection, error) { return targetsFromConfig(d, scan) })
}
