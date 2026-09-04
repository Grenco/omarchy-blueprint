package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	defaultsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/defaults"
	hooksprovider "github.com/Grenco/omarchy-blueprint/internal/providers/hooks"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	pluginsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	shellprovider "github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	themesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/themes"
)

// stateProvider keeps CLI orchestration independent from each provider's
// typed state and domain-specific operations.
type stateProvider interface {
	ID() string
	Captured(profile.Data) bool
	Capture(context.Context, *profile.Data) (any, []model.Change, error)
	Diff(context.Context, profile.Data) ([]model.Change, error)
	Plan(context.Context, profile.Data, omarchy.Info, restorePlanOptions) (model.RestorePlan, error)
	Verify(context.Context, profile.Data) (model.VerificationResult, error)
	Check(context.Context, profile.Data) error
}

type categoryStateProvider interface {
	stateProvider
	CategoryEnabled() bool
}

// stateEmptyer lets a provider report an empty captured state so it can
// remain in the captured-provider list for labeling while its output field is
// omitted from the machine-readable JSON envelope.
type stateEmptyer interface {
	Empty(any) bool
}

func stateProviders(deps Dependencies, opt *options) []stateProvider {
	return []stateProvider{
		packagesStateProvider{deps: deps},
		themesStateProvider{deps: deps, opt: opt},
		pluginsStateProvider{deps: deps, opt: opt},
		configStateProvider{deps: deps, opt: opt},
		defaultsStateProvider{deps: deps, opt: opt},
		shellStateProvider{deps: deps, opt: opt},
		hooksStateProvider{deps: deps, opt: opt},
	}
}

func categoryProviderIDs(providers []stateProvider) []string {
	ids := make([]string, 0, len(providers))
	for _, provider := range providers {
		category, ok := provider.(categoryStateProvider)
		if ok && category.CategoryEnabled() {
			ids = append(ids, provider.ID())
		}
	}
	return ids
}

func categoryProvider(providers []stateProvider, id string) (stateProvider, bool) {
	for _, provider := range providers {
		if provider.ID() == id {
			return provider, true
		}
	}
	return nil, false
}

func captureRequiredError(id string) error {
	verb := "captured"
	switch id {
	case "themes":
		return errors.New("theme state has not been captured; run capture themes first")
	case "plugins":
		return errors.New("plugin state has not been captured; run capture plugins first")
	case "config":
		return errors.New("config state has not been captured; run capture config first")
	case "defaults":
		return errors.New("defaults state has not been captured; run capture defaults first")
	case "shell":
		return errors.New("shell state has not been captured; run capture shell first")
	case "hooks":
		return errors.New("hooks state has not been captured; run capture hooks first")
	}
	return fmt.Errorf("%s state has not been %s", id, verb)
}

func capturedProviders(providers []stateProvider, d profile.Data) []stateProvider {
	selected := make([]stateProvider, 0, len(providers))
	for _, provider := range providers {
		if provider.Captured(d) {
			selected = append(selected, provider)
		}
	}
	return selected
}

func providerStateLabel(ids []string) string {
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		switch id {
		case "packages":
			labels = append(labels, "package")
		case "themes":
			labels = append(labels, "theme")
		case "plugins":
			labels = append(labels, "plugin")
		case "config":
			labels = append(labels, "configuration")
		case "defaults":
			labels = append(labels, "defaults")
		case "shell":
			labels = append(labels, "Shell")
		case "hooks":
			labels = append(labels, "hooks")
		default:
			labels = append(labels, id)
		}
	}
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	case 2:
		return labels[0] + " and " + labels[1]
	default:
		head := strings.Join(labels[:len(labels)-1], ", ")
		return head + ", and " + labels[len(labels)-1]
	}
}

func providerCheckLabel(id string) string {
	switch id {
	case "packages":
		return "package discovery available"
	case "themes":
		return "theme discovery available"
	case "plugins":
		return "plugin discovery available"
	case "config":
		return "config state valid"
	case "defaults":
		return "defaults discovery available"
	case "shell":
		return "shell state valid"
	case "hooks":
		return "hooks state valid"
	default:
		return id + " discovery available"
	}
}

type packagesStateProvider struct{ deps Dependencies }

func (packagesStateProvider) ID() string { return "packages" }

func (packagesStateProvider) CategoryEnabled() bool { return true }

// Packages predate capture metadata and remain the aggregate default so
// status, restore, and check retain their behavior for older/new profiles.
func (packagesStateProvider) Captured(profile.Data) bool { return true }

func (p packagesStateProvider) Capture(ctx context.Context, d *profile.Data) (any, []model.Change, error) {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return nil, nil, err
	}
	current, err := (packagesprovider.Provider{Runner: p.deps.Runner}).Detect(ctx)
	if err != nil {
		return nil, nil, err
	}
	current = packagesprovider.ApplyExclusions(current, d.Packages.Excluded)
	changes := packagesprovider.Diff(d.Packages, current)
	d.Packages = current
	d.Manifest.Capture.Packages = true
	return current, changes, nil
}

func (p packagesStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return nil, err
	}
	current, err := (packagesprovider.Provider{Runner: p.deps.Runner}).Detect(ctx)
	if err != nil {
		return nil, err
	}
	return packagesprovider.Diff(d.Packages, current), nil
}

func (p packagesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return model.RestorePlan{}, err
	}
	current, err := (packagesprovider.Provider{Runner: p.deps.Runner}).Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return packagesprovider.Plan(d.Packages, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version), nil
}

func (p packagesStateProvider) Verify(ctx context.Context, d profile.Data) (model.VerificationResult, error) {
	current, err := (packagesprovider.Provider{Runner: p.deps.Runner}).Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return packagesprovider.Verify(d.Packages, current), nil
}

func (p packagesStateProvider) Check(ctx context.Context, d profile.Data) error {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return err
	}
	_, err := (packagesprovider.Provider{Runner: p.deps.Runner}).Detect(ctx)
	return err
}

type themesStateProvider struct {
	deps Dependencies
	opt  *options
}

func (themesStateProvider) ID() string { return "themes" }

func (themesStateProvider) CategoryEnabled() bool { return true }

func (themesStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Themes }

func (p themesStateProvider) provider() (themesprovider.Provider, error) {
	return themeProvider(p.deps, p.opt)
}

func (p themesStateProvider) Capture(ctx context.Context, d *profile.Data) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Capture(ctx)
	if err != nil {
		return nil, nil, err
	}
	changes := themesprovider.Diff(d.Themes, current)
	d.Themes = current
	d.Manifest.Capture.Themes = true
	return current, changes, nil
}

func (p themesStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, err
	}
	return themesprovider.Diff(d.Themes, current), nil
}

func (p themesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return provider.Plan(d.Themes, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version), nil
}

func (p themesStateProvider) Verify(ctx context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return themesprovider.Verify(d.Themes, current), nil
}

func (p themesStateProvider) Check(ctx context.Context, _ profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	_, err = provider.Detect(ctx)
	return err
}

type pluginsStateProvider struct {
	deps Dependencies
	opt  *options
}

// pluginSemantics delegates plugin enablement ownership to the Shell provider
// once Shell state has been captured; legacy profiles keep the current
// enable/disable behavior.
func pluginSemantics(d profile.Data) pluginsprovider.Semantics {
	return pluginsprovider.Semantics{ManageEnabled: !d.Manifest.Capture.Shell}
}

func (pluginsStateProvider) ID() string { return "plugins" }

func (pluginsStateProvider) CategoryEnabled() bool { return true }

func (pluginsStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Plugins }

func (p pluginsStateProvider) provider() (pluginsprovider.Provider, error) {
	return pluginProvider(p.deps, p.opt)
}

func (p pluginsStateProvider) Capture(ctx context.Context, d *profile.Data) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Capture(ctx)
	if err != nil {
		return nil, nil, err
	}
	changes := pluginsprovider.Diff(d.Plugins, current, pluginSemantics(*d))
	d.Plugins = current
	d.Manifest.Capture.Plugins = true
	return current, changes, nil
}

func (p pluginsStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, err
	}
	return pluginsprovider.Diff(d.Plugins, current, pluginSemantics(d)), nil
}

func (p pluginsStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return provider.Plan(d.Plugins, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, pluginSemantics(d)), nil
}

func (p pluginsStateProvider) Verify(ctx context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return pluginsprovider.Verify(d.Plugins, current, pluginSemantics(d)), nil
}

func (p pluginsStateProvider) Check(ctx context.Context, _ profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	_, err = provider.Detect(ctx)
	return err
}

// configStateProvider captures customized Hyprland configuration files.
type configStateProvider struct {
	deps Dependencies
	opt  *options
}

func (configStateProvider) ID() string { return "config" }

func (configStateProvider) CategoryEnabled() bool { return true }

func (configStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Config }

func (configStateProvider) Empty(state any) bool {
	if s, ok := state.(profile.Configs); ok {
		return len(s.Files) == 0
	}
	return false
}

func (p configStateProvider) provider() (configprovider.Provider, error) {
	baseline, user, err := p.deps.ConfigDirs()
	return configprovider.Provider{UserRoot: user, BaselineRoot: baseline, ProfileDir: p.opt.profileDir}, err
}

func (p configStateProvider) Capture(_ context.Context, d *profile.Data) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Capture()
	if err != nil {
		return nil, nil, err
	}
	changes := configprovider.DiffConfigs(d.Config, current)
	d.Config = current
	d.Manifest.Capture.Config = true
	return current, changes, nil
}

func (p configStateProvider) Diff(_ context.Context, d profile.Data) ([]model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, err
	}
	return configprovider.Diff(d.Config, current), nil
}

func (p configStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.RestorePlan{}, err
	}
	plan, err := provider.Plan(d.Config, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return plan, nil
}

func (p configStateProvider) Verify(_ context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.VerificationResult{}, err
	}
	return configprovider.Verify(d.Config, current), nil
}

func (p configStateProvider) Check(_ context.Context, d profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	return provider.Check(d.Config)
}

// defaultsStateProvider captures Omarchy's semantic default applications.
type defaultsStateProvider struct {
	deps Dependencies
	opt  *options
}

func (defaultsStateProvider) ID() string { return "defaults" }

func (defaultsStateProvider) CategoryEnabled() bool { return true }

func (defaultsStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Defaults }

func (defaultsStateProvider) Empty(state any) bool {
	if s, ok := state.(profile.Defaults); ok {
		return (s == profile.Defaults{})
	}
	return false
}

func (p defaultsStateProvider) provider() defaultsprovider.Provider {
	return defaultsprovider.Provider{Runner: p.deps.Runner, ProfileDir: p.opt.profileDir}
}

func (p defaultsStateProvider) Capture(ctx context.Context, d *profile.Data) (any, []model.Change, error) {
	current, err := p.provider().Capture(ctx)
	if err != nil {
		return nil, nil, err
	}
	changes := defaultsprovider.Diff(d.Defaults, current)
	changes = append(changes, defaultsprovider.Warn(current)...)
	d.Defaults = current
	d.Manifest.Capture.Defaults = true
	return current, changes, nil
}

func (p defaultsStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return nil, err
	}
	return defaultsprovider.Diff(d.Defaults, current), nil
}

func (p defaultsStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return p.provider().Plan(d.Defaults, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version), nil
}

func (p defaultsStateProvider) Verify(ctx context.Context, d profile.Data) (model.VerificationResult, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return defaultsprovider.Verify(d.Defaults, current), nil
}

func (p defaultsStateProvider) Check(ctx context.Context, _ profile.Data) error {
	_, err := p.provider().Detect(ctx)
	return err
}

func captureProvider(ctx context.Context, provider stateProvider, d *profile.Data) (any, []model.Change, error) {
	state, changes, err := provider.Capture(ctx, d)
	if err != nil {
		return nil, nil, fmt.Errorf("capture %s: %w", provider.ID(), err)
	}
	return state, changes, nil
}

// shellStateProvider captures Omarchy Shell state; after capture it owns
// plugin enablement/layout semantics while the plugins provider keeps source
// provenance.
type shellStateProvider struct {
	deps Dependencies
	opt  *options
}

func (shellStateProvider) ID() string { return "shell" }

func (shellStateProvider) CategoryEnabled() bool { return true }

func (shellStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Shell }

func (shellStateProvider) Empty(state any) bool {
	if s, ok := state.(profile.Shell); ok {
		return s.Hash == ""
	}
	return false
}

func (p shellStateProvider) provider() (shellprovider.Provider, error) {
	baseline, user, err := p.deps.ShellPaths()
	return shellprovider.Provider{BaselinePath: baseline, UserPath: user, ProfileDir: p.opt.profileDir}, err
}

func (p shellStateProvider) Capture(ctx context.Context, d *profile.Data) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, nil, err
	}
	changes, err := provider.CaptureChanges(d.Shell, current)
	if err != nil {
		return nil, nil, err
	}
	if current.Status == shellprovider.StatusCustomized {
		if err := shellprovider.ValidatePluginReferences(current.References, d.Plugins); err != nil {
			return nil, nil, err
		}
	}
	captured, err := provider.Capture(current)
	if err != nil {
		return nil, nil, err
	}
	d.Shell = captured
	d.Manifest.Capture.Shell = true
	return captured, changes, nil
}

func (p shellStateProvider) Diff(_ context.Context, d profile.Data) ([]model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, err
	}
	return provider.Diff(d.Shell, current)
}

func (p shellStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, options restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.RestorePlan{}, err
	}
	return provider.Plan(d.Shell, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, shellprovider.MergeOptions{Force: options.Force})
}

func (p shellStateProvider) Verify(_ context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.VerificationResult{}, err
	}
	return provider.Verify(d.Shell, current)
}

func (p shellStateProvider) Check(_ context.Context, d profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	return provider.Check(d.Shell, d.Plugins)
}

type hooksStateProvider struct {
	deps Dependencies
	opt  *options
}

func (hooksStateProvider) ID() string { return "hooks" }

func (hooksStateProvider) CategoryEnabled() bool { return true }

func (hooksStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Hooks }

func (hooksStateProvider) Empty(state any) bool {
	s, ok := state.(profile.Hooks)
	return ok && len(s.Items) == 0
}

func (p hooksStateProvider) provider() (hooksprovider.Provider, error) {
	dir, err := p.deps.HooksDir()
	return hooksprovider.Provider{UserDir: dir, ProfileDir: p.opt.profileDir}, err
}

func (p hooksStateProvider) Capture(_ context.Context, d *profile.Data) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, nil, err
	}
	captured, err := provider.Capture(current)
	if err != nil {
		return nil, nil, err
	}
	changes := hooksprovider.DiffCaptures(d.Hooks, captured)
	d.Hooks = captured
	d.Manifest.Capture.Hooks = true
	return captured, changes, nil
}

func (p hooksStateProvider) Diff(_ context.Context, d profile.Data) ([]model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, err
	}
	return hooksprovider.Diff(d.Hooks, current), nil
}

func (p hooksStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.RestorePlan{}, err
	}
	return provider.Plan(d.Hooks, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
}

func (p hooksStateProvider) Verify(_ context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.VerificationResult{}, err
	}
	return hooksprovider.Verify(d.Hooks, current), nil
}

func (p hooksStateProvider) Check(_ context.Context, d profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	return provider.Check(d.Hooks)
}
