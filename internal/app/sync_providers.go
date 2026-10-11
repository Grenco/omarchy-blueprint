package app

import (
	"context"
	"fmt"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	defaultsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/defaults"
	hooksprovider "github.com/Grenco/omarchy-blueprint/internal/providers/hooks"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	pluginsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	shellprovider "github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	themesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/themes"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"github.com/Grenco/omarchy-blueprint/internal/syncprofile"
)

func (p restoreProviderAdapter) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	v, ok := p.stateProvider.(interface {
		SyncDesired(profile.Data) ([]syncengine.DesiredTarget, error)
	})
	if !ok {
		return nil, fmt.Errorf("sync: provider %s lacks semantic projection", p.ID())
	}
	return v.SyncDesired(profile.CloneData(d))
}

func (p restoreProviderAdapter) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	v, ok := p.stateProvider.(interface {
		OverlaySyncTarget(context.Context, *syncprofile.Builder, syncprofile.View, string) error
	})
	if !ok {
		return fmt.Errorf("sync: provider %s lacks semantic composition", p.ID())
	}
	return v.OverlaySyncTarget(ctx, b, src, target)
}

func (packagesStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (packagesprovider.Provider{}).SyncDesired(d)
}
func (packagesStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (packagesprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (themesStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (themesprovider.Provider{}).SyncDesired(d)
}
func (themesStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (themesprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (pluginsStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (pluginsprovider.Provider{}).SyncDesired(d)
}
func (pluginsStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (pluginsprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (configStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (configprovider.Provider{}).SyncDesired(d)
}
func (configStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (configprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (defaultsStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (defaultsprovider.Provider{}).SyncDesired(d)
}
func (defaultsStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (defaultsprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (shellStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (shellprovider.Provider{}).SyncDesired(d)
}
func (shellStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (shellprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (hooksStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (hooksprovider.Provider{}).SyncDesired(d)
}
func (hooksStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (hooksprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}

func (resourcesStateProvider) SyncDesired(d profile.Data) ([]syncengine.DesiredTarget, error) {
	return (resourcesprovider.Provider{}).SyncDesired(d)
}
func (resourcesStateProvider) OverlaySyncTarget(ctx context.Context, b *syncprofile.Builder, src syncprofile.View, target string) error {
	return (resourcesprovider.Provider{}).OverlaySyncTarget(ctx, b, src, target)
}
