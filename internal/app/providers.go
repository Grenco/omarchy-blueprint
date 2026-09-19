package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	defaultsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/defaults"
	hooksprovider "github.com/Grenco/omarchy-blueprint/internal/providers/hooks"
	packagesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/packages"
	pluginsprovider "github.com/Grenco/omarchy-blueprint/internal/providers/plugins"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	shellprovider "github.com/Grenco/omarchy-blueprint/internal/providers/shell"
	themesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/themes"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

// stateProvider keeps CLI orchestration independent from each provider's
// typed state and domain-specific operations.
type stateProvider interface {
	ID() string
	Captured(profile.Data) bool
	Capture(context.Context, *profile.Data, workflow.CaptureContext) (any, []model.Change, error)
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

// scanDiffProvider optionally retains discovery metadata alongside semantic
// differences. Providers remain responsible only for domain state, not UI.
type scanDiffProvider interface {
	DiffWithScan(context.Context, profile.Data) ([]model.Change, configprovider.ScanSummary, error)
}

type resourceDiffProvider interface {
	DiffWithGitWorkingState(context.Context, profile.Data) ([]model.Change, map[string]resourcesprovider.GitWorkingSummary, error)
}

func stateProviders(deps Dependencies, opt *options) []stateProvider {
	return []stateProvider{
		packagesStateProvider{deps: deps},
		&themesStateProvider{deps: deps, opt: opt},
		&pluginsStateProvider{deps: deps, opt: opt},
		&resourcesStateProvider{deps: deps, opt: opt},
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

type resourcesStateProvider struct {
	deps     Dependencies
	opt      *options
	prepared *resourcesprovider.PreparedCapture
}

// TrackResource stages resource artifacts before saving metadata, retaining the
// previous generation when the profile save fails.
func (p resourcesStateProvider) TrackResource(ctx context.Context, d profile.Data, request workflow.TrackRequest) (profile.Data, profile.Resource, []model.Change, error) {
	provider, err := p.provider(d)
	if err != nil {
		return d, profile.Resource{}, nil, err
	}
	prepared, err := provider.PrepareTrack(ctx, d.Resources, request.Path, resourcesprovider.TrackOptions{
		ID: request.ID, Strategy: request.Strategy, IncludeUntracked: request.IncludeUntracked, ExcludeUntracked: request.ExcludeUntracked,
	})
	if err != nil {
		return d, profile.Resource{}, nil, err
	}
	if err := prepared.Install(); err != nil {
		return d, profile.Resource{}, nil, err
	}
	changes := trackChanges(d.Resources, prepared.State, prepared.Changes)
	d.Resources = prepared.State
	d.Manifest.Capture.Resources = true
	d.Manifest.Profile.UpdatedAt = p.deps.Now().UTC()
	if err := profile.Save(p.opt.profileDir, d); err != nil {
		_ = prepared.Rollback()
		return d, profile.Resource{}, nil, fmt.Errorf("save profile: %w", err)
	}
	if err := prepared.Commit(); err != nil {
		return d, profile.Resource{}, nil, err
	}
	if err := prepared.Finalize(); err != nil {
		return d, profile.Resource{}, nil, err
	}
	resource := profile.Resource{}
	for _, change := range changes {
		if change.Provider == "resources" && change.Kind == "resource" {
			resource, _ = resourceByID(d.Resources.Items, change.Name)
			break
		}
	}
	if resource.ID == "" && request.ID != "" {
		resource, _ = resourceByID(d.Resources.Items, request.ID)
	}
	return d, resource, changes, nil
}

// UntrackResource removes a saved resource generation while retaining the live files.
func (p resourcesStateProvider) UntrackResource(_ context.Context, d profile.Data, id string) (profile.Data, []string, error) {
	provider, err := p.provider(d)
	if err != nil {
		return d, nil, err
	}
	prepared, removed, err := provider.PrepareUntrack(d.Resources, "resource:"+id)
	if err != nil {
		return d, nil, err
	}
	if err := prepared.Install(); err != nil {
		return d, nil, err
	}
	d.Resources = prepared.State
	d.Manifest.Capture.Resources = true
	d.Manifest.Profile.UpdatedAt = p.deps.Now().UTC()
	if err := profile.Save(p.opt.profileDir, d); err != nil {
		_ = prepared.Rollback()
		return d, nil, fmt.Errorf("save profile: %w", err)
	}
	if err := prepared.Commit(); err != nil {
		return d, nil, err
	}
	if err := prepared.Finalize(); err != nil {
		return d, nil, err
	}
	return d, removed, nil
}

func (resourcesStateProvider) ID() string                     { return "resources" }
func (resourcesStateProvider) CategoryEnabled() bool          { return true }
func (p resourcesStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Resources }
func (resourcesStateProvider) Empty(state any) bool {
	resources, ok := state.(profile.Resources)
	return ok && len(resources.Items) == 0
}
func (p resourcesStateProvider) provider(d profile.Data) (resourcesprovider.Provider, error) {
	home, err := p.deps.HomeDir()
	if err != nil {
		return resourcesprovider.Provider{}, err
	}
	state, err := p.deps.StateHome()
	if err != nil {
		return resourcesprovider.Provider{}, err
	}
	profileDir, err := machine.CanonicalProfileRoot(p.opt.profileDir)
	if err != nil {
		return resourcesprovider.Provider{}, err
	}
	claims := ownership.Index{Claims: []ownership.Claim{{Provider: "profile", Path: profileDir, Recursive: true}, {Provider: "state", Path: state, Recursive: true}}}
	if _, user, err := p.deps.ConfigDirs(); err == nil {
		for _, spec := range configprovider.DefaultSpecs {
			claims.Claims = append(claims.Claims, ownership.Claim{Provider: "config", Path: filepath.Join(user, spec.Path)})
		}
	}
	if _, user, err := p.deps.ShellPaths(); err == nil {
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "shell", Path: user})
	}
	if hooks, err := p.deps.HooksDir(); err == nil {
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "hooks", Path: hooks, Recursive: true, DelegateSymlinks: true})
	}
	if _, themes, err := p.deps.ThemeDirs(); err == nil {
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "themes", Path: themes, Recursive: true})
	}
	if plugins, err := p.deps.PluginDir(); err == nil {
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "plugins", Path: plugins, Recursive: true})
	}
	context, err := resolveMachineContext(p.deps, p.opt, d)
	if err != nil {
		return resourcesprovider.Provider{}, err
	}
	if err := resourcesprovider.ValidateEffectiveOwnership(context.Roots, claims); err != nil {
		return resourcesprovider.Provider{}, err
	}
	overrides := make(map[string]string)
	if context.Selection.Machine != nil {
		for _, mapping := range context.Selection.Machine.ResourcePaths {
			overrides[mapping.Resource] = mapping.Path
		}
	}
	return resourcesprovider.Provider{Runner: p.deps.Runner, HomeDir: home, ProfileDir: profileDir, LinkRoots: p.deps.ResourceLinkRoots(home), Ownership: claims, ResourcePaths: resourcesprovider.ResourcePaths{Home: home, Overrides: overrides}}, nil
}

// InspectTargets reports resource:<id> for every explicitly tracked
// resource. Resources have no explicit desired-absence concept and no Exact
// removal (see design non-goals: Resources are never deleted by Exact): a
// resource whose local path is currently missing reports Current: absent,
// which callers must read only as "needs restore," never as deletion intent
// (there is no untracked-by-detection case, since resources only exist here
// once a user explicitly tracks them).
func (p resourcesStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider(d)
	if err != nil {
		return nil, err
	}
	current, _, err := provider.Detect(ctx, d.Resources)
	if err != nil {
		return nil, err
	}
	currentByID := map[string]profile.Resource{}
	for _, item := range current.Items {
		currentByID[item.ID] = item
	}
	targets := make([]workflow.TargetInspection, 0, len(d.Resources.Items))
	for _, item := range d.Resources.Items {
		present := false
		if live, ok := currentByID[item.ID]; ok {
			present = !resourceMissing(live)
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             "resource:" + item.ID,
			Label:           item.ID,
			Desired:         workflow.TargetPresent,
			Current:         currentPresence(present),
			CaptureEligible: true,
			RestoreEligible: true,
			Capabilities: workflow.TargetCapabilities{
				SupportsCapture: true, SupportsRestore: true,
				// A missing local resource is preserved, never dropped:
				// there is no way to tell "gone" apart from "not yet
				// restored," so Capture must not silently forget it.
				PreservesMissingDesired: true,
			},
		})
	}
	return targets, nil
}

// resourceMissing mirrors how Detect itself recognizes a resource whose
// local state could not be read: an empty content hash for a copy strategy,
// or an empty revision for a Git strategy.
func resourceMissing(item profile.Resource) bool {
	if item.Strategy == "copy" {
		return item.Hash == ""
	}
	return item.Revision == ""
}

func (p *resourcesStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	if len(d.Resources.Items) == 0 && !d.Manifest.Capture.Resources {
		return nil, nil, nil
	}
	provider, err := p.provider(*d)
	if err != nil {
		return nil, nil, err
	}
	enabled := func(id string) bool {
		decision, ok := capCtx.Lookup("resource:" + id)
		return ok && decision.Capture
	}
	prepared, err := provider.PrepareCapture(ctx, d.Resources, resourcesprovider.CaptureOptions{}, enabled)
	if err != nil {
		return nil, nil, err
	}
	if err := prepared.Install(); err != nil {
		return nil, nil, err
	}
	p.prepared = prepared
	d.Resources = prepared.State
	d.Manifest.Capture.Resources = true
	return prepared.State, prepared.Changes, nil
}

func (p *resourcesStateProvider) FinalizeCapture() error {
	if p.prepared == nil {
		return nil
	}
	err := p.prepared.Finalize()
	p.prepared = nil
	return err
}
func (p *resourcesStateProvider) CommitCapture() error {
	if p.prepared == nil {
		return nil
	}
	return p.prepared.Commit()
}

func (p *resourcesStateProvider) RollbackCapture() error {
	if p.prepared == nil {
		return nil
	}
	err := p.prepared.Rollback()
	p.prepared = nil
	return err
}
func (p resourcesStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	changes, _, err := p.DiffWithGitWorkingState(ctx, d)
	return changes, err
}
func (p resourcesStateProvider) DiffWithGitWorkingState(ctx context.Context, d profile.Data) ([]model.Change, map[string]resourcesprovider.GitWorkingSummary, error) {
	provider, err := p.provider(d)
	if err != nil {
		return nil, nil, err
	}
	detection, err := provider.DetectDetailed(ctx, d.Resources)
	if err != nil {
		return nil, nil, err
	}
	return resourcesprovider.Diff(d.Resources, detection.Resources), detection.Git, nil
}
func (p resourcesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, options restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, _, err := provider.Detect(ctx, d.Resources)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return provider.Plan(ctx, d.Resources, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, resourcesprovider.PlanOptions{Force: options.Force})
}
func (p resourcesStateProvider) Verify(ctx context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, _, err := provider.Detect(ctx, d.Resources)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return resourcesprovider.Verify(d.Resources, current), nil
}
func (p resourcesStateProvider) Check(ctx context.Context, d profile.Data) error {
	provider, err := p.provider(d)
	if err != nil {
		return err
	}
	return provider.Check(ctx, d.Resources)
}

// StopManaging rejects the generic action: a tracked Resource has no
// "capture disabled, keep the metadata" state distinct from being tracked at
// all, and it needs its own dedicated cleanup (files/git-state, links, other
// resources' overlap checks), which Untrack already performs correctly.
func (resourcesStateProvider) StopManaging(context.Context, profile.Data, string) (profile.Data, error) {
	return profile.Data{}, fmt.Errorf("resources does not support Stop Managing; use Untrack instead")
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
	case "resources":
		return errors.New("resources state has not been captured; track a resource or run capture resources first")
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
		case "resources":
			labels = append(labels, "portable resources")
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
	case "resources":
		return "portable resource state valid"
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

func (p packagesStateProvider) provider() (packagesprovider.Provider, error) {
	miseConfig, err := p.deps.MiseGlobalConfig()
	return packagesprovider.Provider{Runner: p.deps.Runner, MiseGlobalConfig: miseConfig}, err
}

// InspectTargets reports every portable package (official:<name>, aur:<name>,
// mise:<id>) currently desired, currently installed, or explicitly excluded.
// Hardware/machine-specific packages are reported separately and marked
// CaptureEligible: false, since they are protected inspection metadata, not
// normal portable targets (see Task 17's classification rules).
func (p packagesStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, err
	}
	desired := d.Packages

	desiredPortable, currentPortable := map[string]bool{}, map[string]bool{}
	for _, name := range desired.Official {
		desiredPortable["official:"+name] = true
	}
	for _, name := range desired.AUR {
		desiredPortable["aur:"+name] = true
	}
	for id := range desired.Mise {
		desiredPortable["mise:"+id] = true
	}
	for _, name := range current.Official {
		currentPortable["official:"+name] = true
	}
	for _, name := range current.AUR {
		currentPortable["aur:"+name] = true
	}
	for id := range current.Mise {
		currentPortable["mise:"+id] = true
	}
	excluded := map[string]bool{}
	for _, ref := range desired.Excluded {
		excluded[ref] = true
	}
	absent := map[string]bool{}
	for _, absence := range desired.Absent {
		absent[absence.Ref] = true
	}

	keys := map[string]bool{}
	for key := range desiredPortable {
		keys[key] = true
	}
	for key := range currentPortable {
		keys[key] = true
	}
	for key := range excluded {
		keys[key] = true
	}
	for key := range absent {
		keys[key] = true
	}
	sortedKeys := make([]string, 0, len(keys))
	for key := range keys {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	targets := make([]workflow.TargetInspection, 0, len(sortedKeys)+len(current.MachineSpecific))
	for _, key := range sortedKeys {
		currentState := currentPresence(currentPortable[key])
		if excluded[key] {
			// Legacy Excluded is Capture Disabled + Restore Disabled with no
			// desired state, not a deletion tombstone: unmanaged inspection
			// metadata, never eligible for automatic Capture or Restore.
			targets = append(targets, workflow.TargetInspection{
				Key:             key,
				Label:           packageLabel(key),
				Desired:         workflow.TargetUnknown,
				Current:         currentState,
				CaptureEligible: false,
				RestoreEligible: false,
				SafetyReason:    "excluded: legacy Capture Disabled / Restore Disabled",
			})
			continue
		}
		desiredState := workflow.TargetUnknown
		switch {
		case absent[key]:
			desiredState = workflow.TargetAbsent
		case desiredPortable[key]:
			desiredState = workflow.TargetPresent
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             key,
			Label:           packageLabel(key),
			Desired:         desiredState,
			Current:         currentState,
			CaptureEligible: true,
			RestoreEligible: true,
			Capabilities: workflow.TargetCapabilities{
				SupportsCapture:        true,
				SupportsRestore:        true,
				SupportsDesiredAbsence: true,
				SupportsExactRemoval:   true,
			},
		})
	}

	machineSpecific := map[string]bool{}
	for _, ref := range desired.MachineSpecific {
		machineSpecific[ref] = true
	}
	for _, ref := range current.MachineSpecific {
		machineSpecific[ref] = true
	}
	machineKeys := make([]string, 0, len(machineSpecific))
	for ref := range machineSpecific {
		machineKeys = append(machineKeys, ref)
	}
	sort.Strings(machineKeys)
	for _, ref := range machineKeys {
		targets = append(targets, workflow.TargetInspection{
			Key:             ref,
			Label:           packageLabel(ref),
			Desired:         workflow.TargetUnknown,
			Current:         workflow.TargetPresent,
			CaptureEligible: false,
			RestoreEligible: false,
			SafetyReason:    "hardware/machine-specific package is not portable across machines",
		})
	}
	return targets, nil
}

func packageLabel(ref string) string {
	_, name, ok := strings.Cut(ref, ":")
	if !ok {
		return ref
	}
	return name
}

// sortedKeys returns a deterministically ordered slice of a string set, so
// InspectTargets output does not depend on map iteration order.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// desiredPresence reports the Desired TargetState for a target that has no
// explicit desired-absence tombstone: present if tracked, otherwise unknown
// (never captured) rather than absent (explicitly not wanted).
func desiredPresence(tracked bool) workflow.TargetState {
	if tracked {
		return workflow.TargetPresent
	}
	return workflow.TargetUnknown
}

// currentPresence reports the Current TargetState from a live detection
// membership check: present if detected, otherwise genuinely absent.
func currentPresence(detected bool) workflow.TargetState {
	if detected {
		return workflow.TargetPresent
	}
	return workflow.TargetAbsent
}

// Capture merges live detection into desired state per the Capture merge
// transition table (see packagesprovider.Merge), driven by capCtx's resolved
// per-target decision. The legacy Excluded mechanism still applies first
// (still-active for a manually excluded ref with no policy rule): it is
// never translated into the new model, only composed with it.
func (p packagesStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return nil, nil, err
	}
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, nil, err
	}
	current = packagesprovider.ApplyExclusions(current, d.Packages.Excluded)
	current = packagesprovider.PreserveExcludedMise(current, d.Packages)
	merged := packagesprovider.Merge(d.Packages, current, func(ref string) bool {
		decision, ok := capCtx.Lookup(ref)
		return ok && decision.Capture
	})
	changes := packagesprovider.Diff(d.Packages, merged)
	d.Packages = merged
	d.Manifest.Capture.Packages = true
	return merged, changes, nil
}

func (p packagesStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return nil, err
	}
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, err
	}
	return packagesprovider.Diff(d.Packages, current), nil
}

func (p packagesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, _ restorePlanOptions) (model.RestorePlan, error) {
	if err := packagesprovider.ValidateExclusions(d.Packages); err != nil {
		return model.RestorePlan{}, err
	}
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	return provider.Plan(d.Packages, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
}

func (p packagesStateProvider) Verify(ctx context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return packagesprovider.Verify(d.Packages, current), nil
}

func (p packagesStateProvider) Check(ctx context.Context, d profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	return provider.Check(ctx, d.Packages)
}

// StopManaging permanently forgets one package/tool: its desired present or
// desired-absent state. Packages persist no artifact beyond the profile
// metadata itself (unlike Themes/Plugins/Hooks), so there is nothing else on
// disk to remove.
func (packagesStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
	kind, ref, ok := strings.Cut(target, ":")
	if !ok || ref == "" {
		return profile.Data{}, fmt.Errorf("packages: invalid target %q", target)
	}
	found := false
	switch kind {
	case "official":
		if next, removed := removeStringItem(d.Packages.Official, ref); removed {
			d.Packages.Official, found = next, true
		}
	case "aur":
		if next, removed := removeStringItem(d.Packages.AUR, ref); removed {
			d.Packages.AUR, found = next, true
		}
	case "mise":
		if _, ok := d.Packages.Mise[ref]; ok {
			next := make(profile.MiseTools, len(d.Packages.Mise))
			for id, tool := range d.Packages.Mise {
				if id != ref {
					next[id] = tool
				}
			}
			d.Packages.Mise, found = next, true
		}
	default:
		return profile.Data{}, fmt.Errorf("packages: invalid target %q", target)
	}
	var absent []profile.PackageAbsence
	for _, a := range d.Packages.Absent {
		if a.Ref == target {
			found = true
			continue
		}
		absent = append(absent, a)
	}
	d.Packages.Absent = absent
	if !found {
		return profile.Data{}, fmt.Errorf("packages: %q is not managed", target)
	}
	return d, nil
}

// removeStringItem returns a new slice with target removed, and whether it
// was present. It never mutates items' own backing array, since callers may
// share it with the session's own profile.Data.
func removeStringItem(items []string, target string) ([]string, bool) {
	removed := false
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == target {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if !removed {
		return items, false
	}
	return out, true
}

type themesStateProvider struct {
	deps            Dependencies
	opt             *options
	captureProvider *themesprovider.Provider
}

func (themesStateProvider) ID() string { return "themes" }

func (themesStateProvider) CategoryEnabled() bool { return true }

func (themesStateProvider) Captured(d profile.Data) bool { return d.Manifest.Capture.Themes }

func (p themesStateProvider) provider() (themesprovider.Provider, error) {
	return themeProvider(p.deps, p.opt)
}

// InspectTargets reports "active" (which theme is currently selected) plus
// one theme:<id> target per non-built-in theme available in either the
// desired state or the live detection. Built-in themes are Omarchy's own and
// carry nothing for Blueprint to track a source for, so they are omitted.
func (p themesStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, err
	}
	targets := []workflow.TargetInspection{{
		Key:             "active",
		Label:           "active",
		Desired:         desiredPresence(d.Themes.Current != ""),
		Current:         currentPresence(current.Current != ""),
		CaptureEligible: true,
		RestoreEligible: true,
		Capabilities:    workflow.TargetCapabilities{SupportsCapture: true, SupportsRestore: true},
	}}

	desiredThemes, currentThemes := map[string]bool{}, map[string]bool{}
	for _, theme := range d.Themes.Items {
		if theme.Type != "builtin" {
			desiredThemes[theme.ID] = true
		}
	}
	for _, theme := range current.Items {
		if theme.Type != "builtin" {
			currentThemes[theme.ID] = true
		}
	}
	absentThemes := map[string]bool{}
	for _, absent := range d.Themes.Absent {
		absentThemes[absent.ID] = true
	}
	ids := map[string]bool{}
	for id := range desiredThemes {
		ids[id] = true
	}
	for id := range currentThemes {
		ids[id] = true
	}
	for id := range absentThemes {
		ids[id] = true
	}
	for _, id := range sortedKeys(ids) {
		desiredState := workflow.TargetUnknown
		switch {
		case absentThemes[id]:
			desiredState = workflow.TargetAbsent
		case desiredThemes[id]:
			desiredState = workflow.TargetPresent
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             "theme:" + id,
			Label:           id,
			Desired:         desiredState,
			Current:         currentPresence(currentThemes[id]),
			CaptureEligible: true,
			RestoreEligible: true,
			Capabilities: workflow.TargetCapabilities{
				SupportsCapture: true, SupportsRestore: true,
				SupportsDesiredAbsence: true, SupportsExactRemoval: true,
			},
		})
	}
	return targets, nil
}

func (p *themesStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	p.captureProvider = &provider
	merged, err := p.captureProvider.Capture(ctx, d.Themes, func(ref string) bool {
		decision, ok := capCtx.Lookup(ref)
		return ok && decision.Capture
	})
	if err != nil {
		p.captureProvider = nil
		return nil, nil, err
	}
	changes := themesprovider.Diff(d.Themes, merged)
	d.Themes = merged
	d.Manifest.Capture.Themes = true
	return merged, changes, nil
}

func (p *themesStateProvider) CommitCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	return p.captureProvider.CommitCapture()
}
func (p *themesStateProvider) FinalizeCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	err := p.captureProvider.FinalizeCapture()
	p.captureProvider = nil
	return err
}
func (p *themesStateProvider) RollbackCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	err := p.captureProvider.RollbackCapture()
	p.captureProvider = nil
	return err
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

// StopManaging permanently forgets one theme: its desired present or
// desired-absent state, and its local/overlay artifact directory if it has
// one. Built-in themes carry no Blueprint-owned state to forget, and
// "active" is a separate target (which theme is selected, not a theme's own
// availability), so neither is accepted here.
func (p themesStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
	id, ok := strings.CutPrefix(target, "theme:")
	if !ok || id == "" {
		return profile.Data{}, fmt.Errorf("themes: invalid target %q", target)
	}
	found := false
	var items []profile.Theme
	for _, item := range d.Themes.Items {
		if item.ID == id {
			if item.Type == "builtin" {
				return profile.Data{}, fmt.Errorf("themes: built-in theme %q cannot be Stop Managed", id)
			}
			found = true
			continue
		}
		items = append(items, item)
	}
	d.Themes.Items = items
	var absent []profile.Theme
	for _, item := range d.Themes.Absent {
		if item.ID == id {
			found = true
			continue
		}
		absent = append(absent, item)
	}
	d.Themes.Absent = absent
	if !found {
		return profile.Data{}, fmt.Errorf("themes: %q is not managed", id)
	}
	if err := os.RemoveAll(filepath.Join(p.opt.profileDir, "themes", "local", id)); err != nil {
		return profile.Data{}, err
	}
	return d, nil
}

type pluginsStateProvider struct {
	deps            Dependencies
	opt             *options
	captureProvider *pluginsprovider.Provider
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

// InspectTargets reports plugin:<id> for third-party source availability
// only; first-party ("builtin") plugins ship with Omarchy and have no source
// for Blueprint to capture. Plugin enablement itself is Shell's target, not
// Plugins' (see pluginSemantics).
func (p pluginsStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, err
	}
	desiredThirdParty, currentThirdParty := map[string]bool{}, map[string]bool{}
	for _, plugin := range d.Plugins.Items {
		if plugin.Source != "builtin" {
			desiredThirdParty[plugin.ID] = true
		}
	}
	for _, plugin := range current.Items {
		if plugin.Source != "builtin" {
			currentThirdParty[plugin.ID] = true
		}
	}
	absentThirdParty := map[string]bool{}
	for _, absent := range d.Plugins.Absent {
		absentThirdParty[absent.ID] = true
	}
	ids := map[string]bool{}
	for id := range desiredThirdParty {
		ids[id] = true
	}
	for id := range currentThirdParty {
		ids[id] = true
	}
	for id := range absentThirdParty {
		ids[id] = true
	}
	targets := make([]workflow.TargetInspection, 0, len(ids))
	for _, id := range sortedKeys(ids) {
		desiredState := workflow.TargetUnknown
		switch {
		case absentThirdParty[id]:
			desiredState = workflow.TargetAbsent
		case desiredThirdParty[id]:
			desiredState = workflow.TargetPresent
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             "plugin:" + id,
			Label:           id,
			Desired:         desiredState,
			Current:         currentPresence(currentThirdParty[id]),
			CaptureEligible: true,
			RestoreEligible: true,
			Capabilities: workflow.TargetCapabilities{
				SupportsCapture: true, SupportsRestore: true,
				SupportsDesiredAbsence: true, SupportsExactRemoval: true,
			},
		})
	}
	return targets, nil
}

func (p *pluginsStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	p.captureProvider = &provider
	merged, err := p.captureProvider.Capture(ctx, d.Plugins, func(ref string) bool {
		decision, ok := capCtx.Lookup(ref)
		return ok && decision.Capture
	})
	if err != nil {
		p.captureProvider = nil
		return nil, nil, err
	}
	changes := pluginsprovider.Diff(d.Plugins, merged, pluginSemantics(*d))
	d.Plugins = merged
	d.Manifest.Capture.Plugins = true
	return merged, changes, nil
}

func (p *pluginsStateProvider) CommitCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	return p.captureProvider.CommitCapture()
}
func (p *pluginsStateProvider) FinalizeCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	err := p.captureProvider.FinalizeCapture()
	p.captureProvider = nil
	return err
}
func (p *pluginsStateProvider) RollbackCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	err := p.captureProvider.RollbackCapture()
	p.captureProvider = nil
	return err
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

// StopManaging permanently forgets one third-party plugin: its desired
// present or desired-absent state, and its local clone artifact directory.
// First-party (built-in) plugins carry no Blueprint-owned state to forget.
func (p pluginsStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
	id, ok := strings.CutPrefix(target, "plugin:")
	if !ok || id == "" {
		return profile.Data{}, fmt.Errorf("plugins: invalid target %q", target)
	}
	found := false
	var items []profile.Plugin
	for _, item := range d.Plugins.Items {
		if item.ID == id {
			if item.Source == "builtin" {
				return profile.Data{}, fmt.Errorf("plugins: built-in plugin %q cannot be Stop Managed", id)
			}
			found = true
			continue
		}
		items = append(items, item)
	}
	d.Plugins.Items = items
	var absent []profile.Plugin
	for _, item := range d.Plugins.Absent {
		if item.ID == id {
			found = true
			continue
		}
		absent = append(absent, item)
	}
	d.Plugins.Absent = absent
	if !found {
		return profile.Data{}, fmt.Errorf("plugins: %q is not managed", id)
	}
	if err := os.RemoveAll(filepath.Join(p.opt.profileDir, "plugins", "local", id)); err != nil {
		return profile.Data{}, err
	}
	return d, nil
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
	if result, ok := state.(configprovider.CaptureResult); ok {
		s := result.State
		return len(s.Files) == 0 && len(s.Deletes) == 0 && len(s.Included) == 0 && len(s.Excluded) == 0
	}
	return false
}

func (p configStateProvider) provider(d profile.Data) (configprovider.Provider, error) {
	baseline, user, err := p.deps.ConfigDirs()
	if err != nil {
		return configprovider.Provider{}, err
	}
	home, err := p.deps.HomeDir()
	if err != nil {
		return configprovider.Provider{}, err
	}
	claims := ownership.Index{Claims: []ownership.Claim{{Provider: "profile", Path: p.opt.profileDir, Recursive: true}}}
	if p.deps.StateHome != nil {
		if state, err := p.deps.StateHome(); err == nil {
			claims.Claims = append(claims.Claims, ownership.Claim{Provider: "state", Path: state, Recursive: true})
		}
	}
	if _, themes, err := p.deps.ThemeDirs(); err == nil {
		claims = appendConfigOwnershipClaim(claims, "themes", themes, user, true)
	}
	if plugins, err := p.deps.PluginDir(); err == nil {
		claims = appendConfigOwnershipClaim(claims, "plugins", plugins, user, true)
	}
	if hooks, err := p.deps.HooksDir(); err == nil {
		claims = appendConfigOwnershipClaim(claims, "hooks", hooks, user, true)
	}
	if _, shell, err := p.deps.ShellPaths(); err == nil {
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "shell", Path: shell})
	}
	for _, path := range defaultsprovider.GeneratedOutputPaths(home) {
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "defaults", Path: path})
	}
	for _, resource := range d.Resources.Items {
		path, err := resourcesprovider.ExpandHomePath(home, resource.Path)
		if err != nil {
			return configprovider.Provider{}, err
		}
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "resources", Path: path, Recursive: resource.Kind == "directory"})
	}
	for _, link := range d.Resources.Links {
		if link.Origin != "inbound" {
			continue
		}
		path, err := resourcesprovider.ExpandHomePath(home, link.Source)
		if err != nil {
			return configprovider.Provider{}, err
		}
		claims.Claims = append(claims.Claims, ownership.Claim{Provider: "resources", Path: path})
	}
	var history configprovider.BaselineHistory
	if p.deps.BaselineHistory != nil {
		history = p.deps.BaselineHistory()
	}
	return configprovider.Provider{HomeDir: home, UserRoot: user, BaselineRoot: baseline, ProfileDir: p.opt.profileDir, Ownership: claims, History: history}, nil
}

// InspectTargets reports every scanned Config candidate as a target keyed by
// its canonical managed path, with a meaningful parent chain. Classification
// drives both the descriptive Desired/Current state and whether the path is
// currently safe for Blueprint to manage automatically; safety-blocked
// classifications (delegated, volatile, sensitive, unmanaged symlink,
// unsupported, oversized, ambiguous) stay CaptureEligible: false, since
// provider safety checks remain authoritative over policy.
func (p configStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider(d)
	if err != nil {
		return nil, err
	}
	scan, err := provider.Scan(d.Config)
	if err != nil {
		return nil, err
	}
	desiredFiles := map[string]bool{}
	for _, file := range d.Config.Files {
		desiredFiles[file.Path] = true
	}
	desiredDeletes := map[string]bool{}
	for _, del := range d.Config.Deletes {
		desiredDeletes[del.Path] = true
	}
	seen := map[string]bool{}
	targets := make([]workflow.TargetInspection, 0, len(scan.Candidates))
	for _, candidate := range scan.Candidates {
		seen[candidate.Path] = true
		tracked := desiredFiles[candidate.Path] || desiredDeletes[candidate.Path]
		if !tracked && configCaptureInert(candidate.Classification) {
			// Matches Omarchy's default exactly and was never captured:
			// real Capture persists nothing for Added/ModifiedBaseline/
			// DeletedBaseline only, so this path is not yet a managed
			// target at all, not an implicit "will be updated" one.
			continue
		}
		eligible, reason := configEligibility(candidate.Classification)
		desired := workflow.TargetUnknown
		switch {
		case desiredDeletes[candidate.Path]:
			desired = workflow.TargetAbsent
		case desiredFiles[candidate.Path]:
			desired = workflow.TargetPresent
		}
		current := workflow.TargetPresent
		if candidate.Classification == configprovider.ConfigDeletedBaseline {
			current = workflow.TargetAbsent
		}
		targets = append(targets, configTarget(candidate.Path, desired, current, eligible, reason, workflow.TargetCapabilities{
			SupportsCapture:        true,
			SupportsRestore:        true,
			SupportsDesiredAbsence: true,
			SupportsExactRemoval:   eligible,
			Hierarchical:           true,
		}))
	}

	// Scan omits a saved path entirely once it has no baseline counterpart
	// and is no longer present locally (ScanForCapture never synthesizes it
	// either, since a profile upgrade must not keep legacy discoveries alive
	// merely by recapture -- see ScanForCapture's doc comment). Without this,
	// a previously captured, now locally missing user-added file would
	// vanish from policy inspection even though Blueprint still has saved
	// desired state for it, breaking Capture Preserve (nothing to preserve)
	// and Restore (nothing to recreate from).
	for _, file := range d.Config.Files {
		if seen[file.Path] {
			continue
		}
		// No candidate and no baseline counterpart: the same real Capture
		// this target would go through on next Update silently drops it
		// from Files (see capture.go), so it stop-manages rather than
		// tombstones, exactly like Defaults/Shell.
		targets = append(targets, configTarget(file.Path, workflow.TargetPresent, workflow.TargetAbsent, true, "", workflow.TargetCapabilities{
			SupportsCapture: true,
			SupportsRestore: true,
			Hierarchical:    true,
		}))
	}
	for _, del := range d.Config.Deletes {
		if seen[del.Path] {
			continue
		}
		targets = append(targets, configTarget(del.Path, workflow.TargetAbsent, workflow.TargetAbsent, true, "", workflow.TargetCapabilities{
			SupportsCapture:        true,
			SupportsRestore:        true,
			SupportsDesiredAbsence: true,
			Hierarchical:           true,
		}))
	}
	return targets, nil
}

func configTarget(logicalPath string, desired, current workflow.TargetState, eligible bool, reason string, capabilities workflow.TargetCapabilities) workflow.TargetInspection {
	ancestors := configAncestors(logicalPath)
	parent := ""
	if len(ancestors) > 0 {
		parent = ancestors[0]
	}
	return workflow.TargetInspection{
		Key:             logicalPath,
		Parent:          parent,
		Ancestors:       ancestors,
		Label:           logicalPath,
		Desired:         desired,
		Current:         current,
		CaptureEligible: eligible,
		RestoreEligible: eligible,
		Capabilities:    capabilities,
		SafetyReason:    reason,
	}
}

// configCaptureInert reports classifications real Capture never persists
// (see capture.go: only Added, ModifiedBaseline, and DeletedBaseline ever
// produce a Files or Deletes entry). An inert, untracked path matches
// Omarchy's default exactly and carries nothing for Blueprint to manage yet.
func configCaptureInert(classification configprovider.Classification) bool {
	switch classification {
	case configprovider.ConfigUnchangedBaseline, configprovider.ConfigHistoricalBaseline:
		return true
	default:
		return false
	}
}

// configEligibility maps a Config scan Classification onto Capture/Restore
// eligibility. Excluded is the provider-owned "leave this path alone" state
// -- explicit, permanent, unmanaged metadata, never a deletion tombstone --
// so it stays ineligible exactly like the other safety-blocked
// classifications, not merely "desired absent."
func configEligibility(classification configprovider.Classification) (eligible bool, reason string) {
	switch classification {
	case configprovider.ConfigUnchangedBaseline, configprovider.ConfigModifiedBaseline, configprovider.ConfigHistoricalBaseline,
		configprovider.ConfigAdded, configprovider.ConfigDeletedBaseline:
		return true, ""
	case configprovider.ConfigExcluded:
		return false, "excluded: you asked Config to leave this path alone"
	case configprovider.ConfigDelegated:
		return false, "delegated to another provider"
	case configprovider.ConfigVolatile:
		return false, "excluded as state-heavy/volatile by default"
	case configprovider.ConfigSensitive:
		return false, "excluded as a likely secret/credential path"
	case configprovider.ConfigUnmanagedSymlink:
		return false, "existing symlink is not owned by Blueprint"
	case configprovider.ConfigUnsupported:
		return false, "unsupported file type"
	case configprovider.ConfigOversized:
		return false, "exceeds the capture size limit"
	case configprovider.ConfigAmbiguousBaseline, configprovider.ConfigAmbiguousDeletion:
		return false, "baseline provenance is ambiguous"
	default:
		return false, "unrecognized classification"
	}
}

// configAncestors returns logical's full ancestor chain, nearest parent
// first, down to (and including) the Config root. Policy resolution needs
// the whole chain -- not just the immediate parent -- to find the nearest
// matching ancestor rule when closer directories have none.
func configAncestors(logical string) []string {
	var ancestors []string
	dir := path.Dir(logical)
	for dir != "." && dir != "/" && dir != "" {
		ancestors = append(ancestors, dir)
		parent := path.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ancestors
}

func appendConfigOwnershipClaim(index ownership.Index, provider, path, configRoot string, recursive bool) ownership.Index {
	relative, err := filepath.Rel(configRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return index
	}
	index.Claims = append(index.Claims, ownership.Claim{Provider: provider, Path: path, Recursive: recursive})
	return index
}

func (p configStateProvider) Capture(_ context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	provider, err := p.provider(*d)
	if err != nil {
		return nil, nil, err
	}
	result, err := provider.Capture(d.Config, func(path string) bool {
		decision, ok := capCtx.Lookup(path)
		return ok && decision.Capture
	})
	if err != nil {
		return nil, nil, err
	}
	d.Config = result.State
	d.Manifest.Capture.Config = true
	return result, result.Changes, nil
}

func (p configStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
	changes, _, err := p.DiffWithScan(ctx, d)
	return changes, err
}

func (p configStateProvider) DiffWithScan(_ context.Context, d profile.Data) ([]model.Change, configprovider.ScanSummary, error) {
	provider, err := p.provider(d)
	if err != nil {
		return nil, configprovider.ScanSummary{}, err
	}
	current, err := provider.Scan(d.Config)
	if err != nil {
		return nil, configprovider.ScanSummary{}, err
	}
	changes, err := provider.Diff(d.Config, current)
	return changes, current, err
}

func (p configStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, options restorePlanOptions) (model.RestorePlan, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Scan(d.Config)
	if err != nil {
		return model.RestorePlan{}, err
	}
	plan, err := provider.PlanOverlay(d.Config, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, configprovider.PlanOptions{Force: options.Force})
	if err != nil {
		return model.RestorePlan{}, err
	}
	return plan, nil
}

func (p configStateProvider) Verify(_ context.Context, d profile.Data) (model.VerificationResult, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Scan(d.Config)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return provider.Verify(d.Config, current)
}

func (p configStateProvider) Check(_ context.Context, d profile.Data) error {
	provider, err := p.provider(d)
	if err != nil {
		return err
	}
	return provider.Check(d.Config)
}

// StopManaging rejects the generic action: Config already has its own
// per-path policy controls (see SetConfigPolicy and the Excluded mechanism),
// which are more precise than a single flat target key here.
func (configStateProvider) StopManaging(context.Context, profile.Data, string) (profile.Data, error) {
	return profile.Data{}, fmt.Errorf("config does not support Stop Managing; use its own path-level policy controls instead")
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

// InspectTargets reports the four fixed Defaults targets. There is no
// explicit desired-absence concept for a default application choice: an
// empty stored value means "never captured," not "explicitly cleared."
func (p defaultsStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return nil, err
	}
	fields := []struct{ key, desired, current string }{
		{"terminal", d.Defaults.Terminal, current.Terminal},
		{"browser", d.Defaults.Browser, current.Browser},
		{"editor", d.Defaults.Editor, current.Editor},
		{"agent", d.Defaults.Agent, current.Agent},
	}
	targets := make([]workflow.TargetInspection, 0, len(fields))
	for _, field := range fields {
		// Mirrors Plan/Verify's restore exclusions exactly: agent is never
		// automatically restored (Omarchy's setter launches it), and a
		// desired value Omarchy cannot replay (a raw .desktop fallback) is
		// visible drift but not restorable.
		restoreEligible, reason := true, ""
		switch {
		case field.key == "agent":
			restoreEligible, reason = false, "Omarchy's agent setter launches the selected agent; automatic set-only restore is not currently safe"
		case field.desired != "" && !defaultsprovider.Portable(field.desired):
			restoreEligible, reason = false, fmt.Sprintf("%q is not an Omarchy-managed default and may not be portable", field.desired)
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             field.key,
			Label:           field.key,
			Desired:         desiredPresence(field.desired != ""),
			Current:         currentPresence(field.current != ""),
			CaptureEligible: true,
			RestoreEligible: restoreEligible,
			Capabilities:    workflow.TargetCapabilities{SupportsCapture: true, SupportsRestore: restoreEligible},
			SafetyReason:    reason,
		})
	}
	return targets, nil
}

func (p defaultsStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	current, err := p.provider().Capture(ctx, d.Defaults, func(kind string) bool {
		decision, ok := capCtx.Lookup(kind)
		return ok && decision.Capture
	})
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

// StopManaging clears one default slot back to unmanaged. There is no
// desired-absence concept or on-disk artifact for a default application
// choice, so clearing the saved value is the entire mechanism.
func (defaultsStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
	var current *string
	switch target {
	case "terminal":
		current = &d.Defaults.Terminal
	case "browser":
		current = &d.Defaults.Browser
	case "editor":
		current = &d.Defaults.Editor
	case "agent":
		current = &d.Defaults.Agent
	default:
		return profile.Data{}, fmt.Errorf("defaults: invalid target %q", target)
	}
	if *current == "" {
		return profile.Data{}, fmt.Errorf("defaults: %q is not managed", target)
	}
	*current = ""
	return d, nil
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

// InspectTargets reports exactly one target, "state", for the whole opaque
// Shell customization blob. Shell has no explicit desired-absence concept
// and no Exact cleanup (see design non-goals): an empty captured Hash means
// "no Blueprint-managed Shell customization," never "explicitly removed."
func (p shellStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, err
	}
	eligible, reason := true, ""
	if current.Status == shellprovider.StatusUnsupported {
		eligible, reason = false, "Shell version is unsupported"
	}
	return []workflow.TargetInspection{{
		Key:             "state",
		Label:           "state",
		Desired:         desiredPresence(d.Shell.Hash != ""),
		Current:         currentPresence(current.Status == shellprovider.StatusCustomized),
		CaptureEligible: eligible,
		RestoreEligible: eligible,
		Capabilities:    workflow.TargetCapabilities{SupportsCapture: true, SupportsRestore: true},
		SafetyReason:    reason,
	}}, nil
}

func (p shellStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, nil, err
	}
	decision, ok := capCtx.Lookup("state")
	enabled := ok && decision.Capture
	var changes []model.Change
	if enabled {
		changes, err = provider.CaptureChanges(d.Shell, current)
		if err != nil {
			return nil, nil, err
		}
		if current.Status == shellprovider.StatusCustomized {
			if err := shellprovider.ValidatePluginReferences(current.References, d.Plugins); err != nil {
				return nil, nil, err
			}
		}
	}
	captured, err := provider.Capture(current, d.Shell, enabled)
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

// StopManaging rejects the generic action: Shell is one merge unit spanning
// the whole customization document, and there is currently no existing safe
// operation that clears just its management state without discarding
// captured intent Restore would need. Recapturing with the Omarchy default
// active is the supported way to reset it.
func (shellStateProvider) StopManaging(context.Context, profile.Data, string) (profile.Data, error) {
	return profile.Data{}, fmt.Errorf("shell does not support Stop Managing; capture again with the Omarchy default active instead")
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

func (p hooksStateProvider) provider(resources profile.Resources) (hooksprovider.Provider, error) {
	dir, err := p.deps.HooksDir()
	if err != nil {
		return hooksprovider.Provider{}, err
	}
	home, err := p.deps.HomeDir()
	if err != nil {
		return hooksprovider.Provider{}, err
	}
	return hooksprovider.Provider{UserDir: dir, ProfileDir: p.opt.profileDir, HomeDir: home, Resources: resources}, nil
}

// InspectTargets reports one target per managed hook path (tracked in the
// desired state, live-detected, or both). A live unmanaged symlink stays
// CaptureEligible: false safety state: Blueprint records no source from it
// and never follows, replaces, or verifies it as portable state.
func (p hooksStateProvider) InspectTargets(ctx context.Context, d profile.Data) ([]workflow.TargetInspection, error) {
	provider, err := p.provider(d.Resources)
	if err != nil {
		return nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, err
	}
	desired, currentManaged, absent := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, hook := range d.Hooks.Items {
		desired[hook.Path] = true
	}
	for _, hook := range current.Items {
		currentManaged[hook.Path] = true
	}
	for _, hook := range d.Hooks.Absent {
		absent[hook.Path] = true
	}
	paths := map[string]bool{}
	for path := range desired {
		paths[path] = true
	}
	for path := range currentManaged {
		paths[path] = true
	}
	for path := range absent {
		paths[path] = true
	}
	targets := make([]workflow.TargetInspection, 0, len(paths)+len(current.Unmanaged))
	for _, path := range sortedKeys(paths) {
		desiredState := workflow.TargetUnknown
		switch {
		case absent[path]:
			desiredState = workflow.TargetAbsent
		case desired[path]:
			desiredState = workflow.TargetPresent
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             path,
			Label:           path,
			Desired:         desiredState,
			Current:         currentPresence(currentManaged[path]),
			CaptureEligible: true,
			RestoreEligible: true,
			Capabilities: workflow.TargetCapabilities{
				SupportsCapture: true, SupportsRestore: true,
				SupportsDesiredAbsence: true, SupportsExactRemoval: true,
			},
		})
	}
	for _, unmanaged := range current.Unmanaged {
		reason := "unmanaged symlink is not owned by Blueprint"
		if unmanaged.Broken {
			reason = "unmanaged symlink is broken"
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             unmanaged.Path,
			Label:           unmanaged.Path,
			Desired:         workflow.TargetUnknown,
			Current:         workflow.TargetPresent,
			CaptureEligible: false,
			RestoreEligible: false,
			SafetyReason:    reason,
		})
	}
	return targets, nil
}

func (p hooksStateProvider) Capture(_ context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	provider, err := p.provider(d.Resources)
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Detect()
	if err != nil {
		return nil, nil, err
	}
	captured, err := provider.Capture(current, d.Hooks, func(path string) bool {
		decision, ok := capCtx.Lookup(path)
		return ok && decision.Capture
	})
	if err != nil {
		return nil, nil, err
	}
	changes := hooksprovider.DiffCaptures(d.Hooks, captured)
	changes = append(changes, hooksprovider.UnmanagedWarnings(current.Unmanaged)...)
	d.Hooks = captured
	d.Manifest.Capture.Hooks = true
	return captured, changes, nil
}

func (p hooksStateProvider) Diff(_ context.Context, d profile.Data) ([]model.Change, error) {
	provider, err := p.provider(d.Resources)
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
	provider, err := p.provider(d.Resources)
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
	provider, err := p.provider(d.Resources)
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
	provider, err := p.provider(d.Resources)
	if err != nil {
		return err
	}
	return provider.Check(d.Hooks)
}

// StopManaging permanently forgets one hook: its desired present or
// desired-absent state, and its captured snapshot file.
func (p hooksStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
	found := false
	var items []profile.Hook
	for _, item := range d.Hooks.Items {
		if item.Path == target {
			found = true
			continue
		}
		items = append(items, item)
	}
	d.Hooks.Items = items
	var absent []profile.Hook
	for _, item := range d.Hooks.Absent {
		if item.Path == target {
			found = true
			continue
		}
		absent = append(absent, item)
	}
	d.Hooks.Absent = absent
	if !found {
		return profile.Data{}, fmt.Errorf("hooks: %q is not managed", target)
	}
	if err := os.RemoveAll(filepath.Join(p.opt.profileDir, "hooks", "files", filepath.FromSlash(target))); err != nil {
		return profile.Data{}, err
	}
	return d, nil
}
