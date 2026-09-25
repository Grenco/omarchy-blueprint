package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
	"github.com/Grenco/omarchy-blueprint/internal/ownership"
	"github.com/Grenco/omarchy-blueprint/internal/policy"
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
	Plan(context.Context, profile.Data, omarchy.Info, workflow.RestoreContext) (model.RestorePlan, error)
	Verify(context.Context, profile.Data, workflow.RestoreContext) (model.VerificationResult, error)
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
		&hooksStateProvider{deps: deps, opt: opt},
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
	// Capture also rebuilds each copy resource's internal links.
	linksBySource := map[string][]profile.ResourceLink{}
	for _, link := range current.Links {
		if link.Origin == "resource" {
			linksBySource[link.SourceResource] = append(linksBySource[link.SourceResource], link)
		}
	}
	targets := make([]workflow.TargetInspection, 0, len(d.Resources.Items))
	for _, item := range d.Resources.Items {
		present, fingerprint := false, ""
		if live, ok := currentByID[item.ID]; ok {
			present = !resourceMissing(live)
			// The whole detected entry, as Capture would persist it: content
			// hash and mode, Git remote/branch/revision, patch hashes, and
			// selected untracked files.
			fingerprint = canonicalFingerprint(struct {
				Resource profile.Resource
				Links    []profile.ResourceLink
			}{live, linksBySource[item.ID]})
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             "resource:" + item.ID,
			Label:           item.ID,
			Desired:         workflow.TargetPresent,
			Current:         currentPresence(present),
			Fingerprint:     fingerprint,
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

// canonicalFingerprint digests the detected value a provider's Capture
// consumes for one target, for workflow.TargetInspection.Fingerprint.
// Fingerprinting the whole detection object, rather than chosen fields,
// keeps newly persisted fields inside the approval contract.
func canonicalFingerprint(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		// Never collapse distinct values onto one fingerprint.
		return "unencodable:" + err.Error() + ":" + fmt.Sprintf("%#v", value)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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

// Plan excludes every Restore-Skip "resource:<id>" target's Item from the
// desired state fed to the low-level planner, and any Link referencing a
// Restore-Skip resource on either end (a link that points into a resource
// this run is not restoring should not be planned either, even though
// links have no target Key of their own to report Skip against).
// recordRestoreSkips then guarantees visibility; Resource tags here already
// match InspectTargets' "resource:<id>" Key format exactly, so no
// re-prefixing is needed. filterResourcesForRestoreSkip fails closed
// (returns an error) if any desired target's Restore decision is missing
// or unresolved, rather than letting it through as an implicit Apply.
func (p resourcesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, _, err := provider.Detect(ctx, d.Resources)
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterResourcesForRestoreSkip(d.Resources, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	force := restoreCtx.Options.Conflicts == policy.ConflictForce
	plan, err := provider.Plan(ctx, saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, resourcesprovider.PlanOptions{Force: force})
	if err != nil {
		return model.RestorePlan{}, err
	}
	return recordRestoreSkips(plan, "resources", matched), nil
}
func (p resourcesStateProvider) Verify(ctx context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, _, err := provider.Detect(ctx, d.Resources)
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterResourcesForRestoreSkip(d.Resources, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return resourcesprovider.Verify(saved, current), nil
}

// filterResourcesForRestoreSkip returns a copy of saved with every Item
// whose "resource:<id>" target resolved to Restore Skip removed, and any
// Link referencing a Restore-Skip resource on either end removed alongside
// it (Resources has no desired-absence representation, per the design, so
// there is no tombstone collection to also cover here). The second return
// is the subset that actually matched an Item -- only that subset should
// be recorded as a visible Plan skip. Every Item's target key is resolved
// via resolveRestoreSkip (RestoreContext.Require), so a missing or
// unresolved decision fails the whole call closed.
func filterResourcesForRestoreSkip(saved profile.Resources, restoreCtx workflow.RestoreContext) (profile.Resources, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved
	skippedIDs := map[string]bool{}
	items := make([]profile.Resource, 0, len(saved.Items))
	for _, item := range saved.Items {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "resources", "resource:"+item.ID)
		if err != nil {
			return profile.Resources{}, nil, err
		}
		if skip {
			skippedIDs[item.ID] = true
			matched = append(matched, entry)
			continue
		}
		items = append(items, item)
	}
	filtered.Items = items
	if len(skippedIDs) > 0 && len(saved.Links) > 0 {
		links := make([]profile.ResourceLink, 0, len(saved.Links))
		for _, link := range saved.Links {
			if skippedIDs[link.SourceResource] || skippedIDs[link.TargetResource] {
				continue
			}
			links = append(links, link)
		}
		filtered.Links = links
	}
	return filtered, matched, nil
}

func (p resourcesStateProvider) Check(ctx context.Context, d profile.Data) error {
	provider, err := p.provider(d)
	if err != nil {
		return err
	}
	return provider.Check(ctx, d.Resources)
}

// ValidateTarget accepts only a fully qualified resource:<id> reference,
// canonicalized to itself. It never requires the resource to currently be
// tracked -- StopManaging rejects the category outright regardless, but a
// caller resolving a target for a different purpose (e.g. SetPolicy) must
// still get a validated key shape.
func (resourcesStateProvider) ValidateTarget(target string) (string, error) {
	id, ok := strings.CutPrefix(target, "resource:")
	if !ok || id == "" || strings.ContainsAny(id, " \t\n/") {
		return "", fmt.Errorf("resources: invalid target %q; use resource:<id>", target)
	}
	return target, nil
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
	preinstalls := current.Preinstalls
	desired := packagesprovider.CanonicalizePreinstallOwnership(d.Packages, preinstalls.Items)

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
	portableKeys := make([]string, 0, len(keys))
	for key := range keys {
		portableKeys = append(portableKeys, key)
	}
	sort.Strings(portableKeys)

	targets := make([]workflow.TargetInspection, 0, len(portableKeys)+len(current.MachineSpecific)+len(preinstalls.Items)+1)
	preinstallGroupDesired := workflow.TargetUnknown
	if desired.Preinstalls.Managed {
		preinstallGroupDesired = currentPresence(!desired.Preinstalls.RemovedAll)
	}
	targets = append(targets, workflow.TargetInspection{
		Key:             "preinstalls",
		Label:           "Omarchy preinstalls",
		Desired:         preinstallGroupDesired,
		Current:         currentPresence(!preinstalls.RemovedAll),
		CaptureEligible: true,
		RestoreEligible: true,
		Capabilities: workflow.TargetCapabilities{
			SupportsCapture:        true,
			SupportsRestore:        true,
			SupportsDesiredAbsence: true,
			SupportsExactRemoval:   true,
			Hierarchical:           true,
		},
	})
	preinstallKeys := map[string]bool{}
	for id := range preinstalls.Items {
		preinstallKeys[id] = true
	}
	for id := range desired.Preinstalls.Items {
		preinstallKeys[id] = true
	}
	for _, id := range sortedKeys(preinstallKeys) {
		desiredState := workflow.TargetUnknown
		if present, known := desired.Preinstalls.Items[id]; known {
			desiredState = currentPresence(present)
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             "preinstall:" + id,
			Parent:          "preinstalls",
			Ancestors:       []string{"preinstalls"},
			Label:           id,
			Desired:         desiredState,
			Current:         currentPresence(preinstalls.Items[id]),
			CaptureEligible: true,
			RestoreEligible: true,
			Capabilities: workflow.TargetCapabilities{
				SupportsCapture:        true,
				SupportsRestore:        true,
				SupportsDesiredAbsence: true,
				SupportsExactRemoval:   true,
				Hierarchical:           true,
			},
		})
	}
	for _, key := range portableKeys {
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
		fingerprint := ""
		if id, ok := strings.CutPrefix(key, "mise:"); ok {
			if tool, found := current.Mise[id]; found {
				fingerprint = canonicalFingerprint(tool)
			}
		}
		targets = append(targets, workflow.TargetInspection{
			Key:             key,
			Label:           packageLabel(key),
			Desired:         desiredState,
			Current:         currentState,
			Fingerprint:     fingerprint,
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
// per-target decision. A manually excluded ref has no desired state at all
// by the time Capture runs (Session.SetPackageExcluded already stripped it),
// so Merge's own disabled-preserve rule keeps it unmanaged; there is no
// separate legacy exclusion mechanism to compose with here.
func (p packagesStateProvider) Capture(ctx context.Context, d *profile.Data, capCtx workflow.CaptureContext) (any, []model.Change, error) {
	provider, err := p.provider()
	if err != nil {
		return nil, nil, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return nil, nil, err
	}
	migrateLegacyPreinstallPolicyAliases(&d.Policy, current.Preinstalls.Items)
	for i := range d.Machines.Items {
		migrateLegacyPreinstallPolicyAliases(&d.Machines.Items[i].Policy, current.Preinstalls.Items)
	}
	merged := packagesprovider.Merge(d.Packages, current, func(ref string) bool {
		decision, ok := capCtx.Lookup(ref)
		return ok && decision.Capture
	})
	changes := packagesprovider.Diff(d.Packages, merged)
	d.Packages = merged
	d.Manifest.Capture.Packages = true
	return merged, changes, nil
}

// migrateLegacyPreinstallPolicyAliases persists the same canonicalization
// used by workflow policy resolution once capture has authoritative catalogue
// data. A direct preinstall rule wins; legacy aliases are removed on save.
func migrateLegacyPreinstallPolicyAliases(rules *policy.Rules, catalogue map[string]bool) {
	if len(catalogue) == 0 {
		return
	}
	migrate := func(items []policy.Rule) []policy.Rule {
		out := append([]policy.Rule(nil), items...)
		for id := range catalogue {
			canonical := "preinstall:" + id
			hasCanonical := false
			for _, rule := range out {
				hasCanonical = hasCanonical || (rule.Category == "packages" && rule.Target == canonical)
			}
			for i := range out {
				if out[i].Category != "packages" || (out[i].Target != "official:"+id && out[i].Target != "aur:"+id) {
					continue
				}
				if hasCanonical {
					out[i].Target = ""
					out[i].Category = ""
					continue
				}
				out[i].Target = canonical
				hasCanonical = true
			}
		}
		filtered := out[:0]
		for _, rule := range out {
			if rule.Category != "" {
				filtered = append(filtered, rule)
			}
		}
		return filtered
	}
	rules.Capture = migrate(rules.Capture)
	rules.Restore = migrate(rules.Restore)
}

func (p packagesStateProvider) Diff(ctx context.Context, d profile.Data) ([]model.Change, error) {
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

// Plan excludes every Restore-Skip package/tool from the desired state fed
// to the low-level planner, so a Restore-Skip target can never be batched
// into a bulk official/AUR install or Mise-install operation alongside an
// Apply one (see filterPackagesForRestoreSkip); recordRestoreSkips then
// guarantees each excluded target still appears visibly in Skipped with its
// resolved policy reason, even for a desired-but-not-yet-installed target
// the low-level planner's own skip logic would otherwise never mention.
// filterPackagesForRestoreSkip fails closed (returns an error) if any
// desired target's Restore decision is missing or unresolved, rather than
// letting it through as an implicit Apply.
func (p packagesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	d.Packages = packagesprovider.CanonicalizePreinstallOwnership(d.Packages, current.Preinstalls.Items)
	saved, matched, err := filterPackagesForRestoreSkip(d.Packages, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	plan, err := provider.Plan(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, packagesprovider.PlanOptions{Exact: exact})
	if err != nil {
		return model.RestorePlan{}, err
	}
	return recordRestoreSkips(plan, "packages", matched), nil
}

// Verify excludes every Restore-Skip package/tool from the desired state it
// checks presence against, so a Restore-Skip target's absence (or a
// differing local Mise declaration) never fails verification -- matching
// the design's "desired-present + Restore Skip: verification ignores the
// target" invariant.
func (p packagesStateProvider) Verify(ctx context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	d.Packages = packagesprovider.CanonicalizePreinstallOwnership(d.Packages, current.Preinstalls.Items)
	saved, _, err := filterPackagesForRestoreSkip(d.Packages, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	return provider.Verify(ctx, saved, current, packagesprovider.VerifyOptions{Exact: exact, Schema: d.Manifest.Schema}), nil
}

// filterPackagesForRestoreSkip returns a copy of saved with every
// Official/AUR/Mise/Absent entry whose target key resolved to Restore Skip
// removed (present desired state and desired-absent tombstones alike --
// see the design's Restore Skip invariant, which does not distinguish the
// two), and the subset of skip that actually matched something in saved --
// only that subset should be recorded as a visible Plan skip; a key with
// no matching desired state was never going to produce anything regardless
// of policy, so reporting it would misleadingly imply something was
// skipped. Filtering the input, rather than filtering Plan's output
// Operations, is the only way to reliably exclude a Restore-Skip target
// from a bulk operation the low-level planner could otherwise batch it
// into alongside an Apply target. Every desired target key this provider
// owns is resolved via resolveRestoreSkip (RestoreContext.Require), so a
// missing or unresolved decision fails the whole call closed rather than
// silently letting that target's desired state through as an implicit
// Apply. MachineSpecific is untouched: the low-level planner already skips
// it unconditionally regardless of policy, so it is never restore-actionable
// desired state in the first place.
func filterPackagesForRestoreSkip(saved profile.Packages, restoreCtx workflow.RestoreContext) (profile.Packages, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved

	official, m, err := filterPackageNames(saved.Official, "official:", restoreCtx)
	if err != nil {
		return profile.Packages{}, nil, err
	}
	filtered.Official = official
	matched = append(matched, m...)

	aur, m, err := filterPackageNames(saved.AUR, "aur:", restoreCtx)
	if err != nil {
		return profile.Packages{}, nil, err
	}
	filtered.AUR = aur
	matched = append(matched, m...)

	if len(saved.Mise) > 0 {
		mise := make(profile.MiseTools, len(saved.Mise))
		for id, tool := range saved.Mise {
			skip, entry, err := resolveRestoreSkip(restoreCtx, "packages", "mise:"+id)
			if err != nil {
				return profile.Packages{}, nil, err
			}
			if skip {
				matched = append(matched, entry)
				continue
			}
			mise[id] = tool
		}
		filtered.Mise = mise
	}

	if len(saved.Absent) > 0 {
		absent := make([]profile.PackageAbsence, 0, len(saved.Absent))
		for _, item := range saved.Absent {
			skip, entry, err := resolveRestoreSkip(restoreCtx, "packages", item.Ref)
			if err != nil {
				return profile.Packages{}, nil, err
			}
			if skip {
				matched = append(matched, entry)
				continue
			}
			absent = append(absent, item)
		}
		filtered.Absent = absent
	}

	if saved.Preinstalls.Managed {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "packages", "preinstalls")
		if err != nil {
			return profile.Packages{}, nil, err
		}
		if skip {
			filtered.Preinstalls.Managed = false
			matched = append(matched, entry)
		}
	}
	if len(saved.Preinstalls.Items) > 0 {
		items := make(map[string]bool, len(saved.Preinstalls.Items))
		childSkipped := false
		for id, present := range saved.Preinstalls.Items {
			skip, entry, err := resolveRestoreSkip(restoreCtx, "packages", "preinstall:"+id)
			if err != nil {
				return profile.Packages{}, nil, err
			}
			if skip {
				matched = append(matched, entry)
				childSkipped = true
				continue
			}
			items[id] = present
		}
		filtered.Preinstalls.Items = items
		// Installing or removing the whole Omarchy preinstall set can mutate
		// every child. Defer that group transition when even one child is
		// Restore-Skip; individual Apply children can still converge safely.
		if childSkipped && saved.Preinstalls.Managed {
			filtered.Preinstalls.Managed = false
			groupRecorded := false
			for _, entry := range matched {
				groupRecorded = groupRecorded || entry.Key == "preinstalls"
			}
			if !groupRecorded {
				matched = append(matched, restoreSkip{Key: "preinstalls", Reason: "group transition blocked because it could mutate a Restore-Skip child"})
			}
		}
	}

	return filtered, matched, nil
}

func filterPackageNames(names []string, prefix string, restoreCtx workflow.RestoreContext) ([]string, []restoreSkip, error) {
	var matched []restoreSkip
	kept := make([]string, 0, len(names))
	for _, name := range names {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "packages", prefix+name)
		if err != nil {
			return nil, nil, err
		}
		if skip {
			matched = append(matched, entry)
			continue
		}
		kept = append(kept, name)
	}
	return kept, matched, nil
}

func (p packagesStateProvider) Check(ctx context.Context, d profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	return provider.Check(ctx, d.Packages)
}

// ValidateTarget accepts only a preinstalls group or fully qualified
// official:<name>, aur:<name>, mise:<name>, or preinstall:<name> reference,
// canonicalized to itself. It never requires the
// package to currently be installed or excluded -- a not-yet-captured or
// already-tombstoned reference must validate too.
func (packagesStateProvider) ValidateTarget(target string) (string, error) {
	if target == "preinstalls" {
		return target, nil
	}
	kind, name, ok := strings.Cut(target, ":")
	if !ok || name == "" {
		return "", fmt.Errorf("packages: invalid target %q; use preinstalls, official:<name>, aur:<name>, mise:<name>, or preinstall:<name>", target)
	}
	switch kind {
	case "official", "aur", "mise", "preinstall":
	default:
		return "", fmt.Errorf("packages: invalid target %q; use preinstalls, official:<name>, aur:<name>, mise:<name>, or preinstall:<name>", target)
	}
	if strings.ContainsAny(name, " \t\n") {
		return "", fmt.Errorf("packages: invalid target %q", target)
	}
	return target, nil
}

// StopManaging permanently forgets one package/tool: its desired present or
// desired-absent state. Packages persist no artifact beyond the profile
// metadata itself (unlike Themes/Plugins/Hooks), so there is nothing else on
// disk to remove.
func (packagesStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
	if target == "preinstalls" {
		if !d.Packages.Preinstalls.Managed {
			return profile.Data{}, fmt.Errorf("packages: %q is not managed", target)
		}
		d.Packages.Preinstalls.Managed = false
		return d, nil
	}
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
	case "preinstall":
		if _, ok := d.Packages.Preinstalls.Items[ref]; ok {
			next := make(map[string]bool, len(d.Packages.Preinstalls.Items)-1)
			for id, present := range d.Packages.Preinstalls.Items {
				if id != ref {
					next[id] = present
				}
			}
			d.Packages.Preinstalls.Items, found = next, true
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
		Fingerprint:     current.Current,
		CaptureEligible: true,
		RestoreEligible: true,
		Capabilities:    workflow.TargetCapabilities{SupportsCapture: true, SupportsRestore: true},
	}}

	desiredThemes, currentThemes := map[string]bool{}, map[string]bool{}
	themeFingerprints := map[string]string{}
	for _, theme := range d.Themes.Items {
		if theme.Type != "builtin" {
			desiredThemes[theme.ID] = true
		}
	}
	for _, theme := range current.Items {
		if theme.Type != "builtin" {
			currentThemes[theme.ID] = true
			themeFingerprints[theme.ID] = canonicalFingerprint(theme)
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
			Fingerprint:     themeFingerprints[id],
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

// Plan excludes a Restore-Skip "active" target by clearing the desired
// active theme (the low-level planner only ever attempts activation when
// Current is non-empty, so this alone suppresses it without touching any
// other theme's own install/copy operations) and excludes each Restore-Skip
// "theme:<id>" target from the desired items list, so neither can be
// batched or otherwise entangled with an Apply target (see
// filterThemesForRestoreSkip, which fails closed on any missing/unresolved
// decision).
//
// "active" and "theme:<id>" are independent policy targets (selected active
// theme versus installed-theme availability) even though the low-level
// planner's activation Operation is tagged with the theme's own
// "theme:<id>" Resource (see operation() in internal/providers/themes), so
// the generic Resource-matching recordRestoreSkips does for availability
// skips must never see that operation -- otherwise an availability Skip for
// the active theme would wrongly remove a legitimately Apply-decided
// activation. splitThemeActivationOperation pulls it out first and it is
// reattached afterward, unless themeActivationBlockedReason finds the
// active theme's own availability is Skip AND it is not currently
// installed, in which case activation cannot actually be honored either
// way and becomes its own visible "active" skip instead.
func (p themesStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterThemesForRestoreSkip(d.Themes, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	blockedReason, blocked := themeActivationBlockedReason(saved.Current, current, restoreCtx)
	if blocked {
		saved.Current = ""
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	plan := provider.Plan(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, themesprovider.PlanOptions{Exact: exact})
	activation, rest := splitThemeActivationOperation(plan.Operations)
	plan.Operations = rest
	plan = recordRestoreSkips(plan, "themes", matched)
	if activation != nil {
		plan.Operations = append(plan.Operations, *activation)
	}
	if blocked {
		plan.Skipped = append(plan.Skipped, model.Skipped{Provider: "themes", Resource: "active", Reason: blockedReason})
	}
	return plan, nil
}

func (p themesStateProvider) Verify(ctx context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterThemesForRestoreSkip(d.Themes, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	if _, blocked := themeActivationBlockedReason(saved.Current, current, restoreCtx); blocked {
		saved.Current = ""
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	return provider.Verify(saved, current, themesprovider.VerifyOptions{Exact: exact}), nil
}

// filterThemesForRestoreSkip returns a copy of saved with the desired
// active theme cleared when "active" resolved to Restore Skip, and every
// Item/Absent entry whose "theme:<id>" target resolved to Restore Skip
// removed (present desired state and desired-absent tombstones alike --
// see the design's Restore Skip invariant, which does not distinguish the
// two). A builtin Item is left untouched regardless: InspectTargets never
// creates a "theme:<id>" target for one ("Built-in themes are Omarchy's
// own and carry nothing for Blueprint to track a source for"), so there is
// no decision to require -- unlike Absent, which InspectTargets always
// scopes a target for regardless of Type. The second return is the subset
// that actually matched a desired active theme or Item/Absent entry --
// only that subset should be recorded as a visible Plan skip; a key with
// no matching desired state was never going to produce anything regardless
// of policy. Every other desired target key is resolved via
// resolveRestoreSkip (RestoreContext.Require), so a missing or unresolved
// decision fails the whole call closed rather than silently letting that
// target's desired state through as an implicit Apply.
func filterThemesForRestoreSkip(saved profile.Themes, restoreCtx workflow.RestoreContext) (profile.Themes, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved

	if saved.Current != "" {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "themes", "active")
		if err != nil {
			return profile.Themes{}, nil, err
		}
		if skip {
			filtered.Current = ""
			matched = append(matched, entry)
		}
	}

	items := make([]profile.Theme, 0, len(saved.Items))
	for _, item := range saved.Items {
		if item.Type == "builtin" {
			items = append(items, item)
			continue
		}
		skip, entry, err := resolveRestoreSkip(restoreCtx, "themes", "theme:"+item.ID)
		if err != nil {
			return profile.Themes{}, nil, err
		}
		if skip {
			matched = append(matched, entry)
			continue
		}
		items = append(items, item)
	}
	filtered.Items = items

	if len(saved.Absent) > 0 {
		absent := make([]profile.Theme, 0, len(saved.Absent))
		for _, item := range saved.Absent {
			skip, entry, err := resolveRestoreSkip(restoreCtx, "themes", "theme:"+item.ID)
			if err != nil {
				return profile.Themes{}, nil, err
			}
			if skip {
				matched = append(matched, entry)
				continue
			}
			absent = append(absent, item)
		}
		filtered.Absent = absent
	}

	return filtered, matched, nil
}

// themeActivationBlockedReason reports whether desiredCurrent's own
// "theme:<id>" availability target is Restore Skip AND it is not currently
// available locally, in which case activating it cannot actually be
// honored regardless of "active" itself resolving to Apply. current.Items
// is live detection, not desired state: an already-installed theme is safe
// to activate even while its own availability target is Skip (there is
// nothing left to install for it), so this only blocks when both
// conditions hold together. It uses Lookup rather than Require: a builtin
// active theme has no "theme:<id>" target at all (see
// filterThemesForRestoreSkip), which is not itself an error here -- there
// is simply nothing to block on.
func themeActivationBlockedReason(desiredCurrent string, current profile.Themes, restoreCtx workflow.RestoreContext) (string, bool) {
	if desiredCurrent == "" {
		return "", false
	}
	decision, ok := restoreCtx.Lookup("theme:" + desiredCurrent)
	if !ok || decision.Restore {
		return "", false
	}
	for _, item := range current.Items {
		if item.ID == desiredCurrent {
			return "", false
		}
	}
	return fmt.Sprintf("cannot activate %q: its availability is Restore Skip (%s), and it is not currently installed", desiredCurrent, decision.Reason), true
}

// splitThemeActivationOperation removes the low-level planner's activation
// Operation (Action == "activate", Resource == "theme:<id>" for whichever
// theme is being activated) from operations, if present, returning it
// separately from the rest so it can be recombined after Resource-matching
// availability-skip logic runs on the rest without risking removing it.
// There is at most one: the low-level planner only ever appends a single
// activation Operation, unconditionally last.
func splitThemeActivationOperation(operations []model.Operation) (*model.Operation, []model.Operation) {
	rest := make([]model.Operation, 0, len(operations))
	var activation *model.Operation
	for i := range operations {
		if operations[i].Provider == "themes" && operations[i].Action == "activate" {
			op := operations[i]
			activation = &op
			continue
		}
		rest = append(rest, operations[i])
	}
	return activation, rest
}

func (p themesStateProvider) Check(ctx context.Context, _ profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	_, err = provider.Detect(ctx)
	return err
}

// ValidateTarget accepts only "active" or a fully qualified theme:<id>
// reference, canonicalized to itself. It never requires the theme to
// currently exist -- a not-yet-captured or already-tombstoned id must
// validate too.
func (themesStateProvider) ValidateTarget(target string) (string, error) {
	if target == "active" {
		return target, nil
	}
	id, ok := strings.CutPrefix(target, "theme:")
	if !ok || id == "" || strings.ContainsAny(id, " \t\n/") {
		return "", fmt.Errorf(`themes: invalid target %q; use "active" or theme:<id>`, target)
	}
	return target, nil
}

// StopManaging permanently forgets one theme: its desired present or
// desired-absent state, and its local/overlay artifact directory if it has
// one. Built-in themes carry no Blueprint-owned state to forget, and
// "active" is a separate target (which theme is selected, not a theme's own
// availability), so neither is accepted here. It stages its artifact removal
// rather than deleting immediately: PrepareStopManagingArtifact renames the
// theme's local/overlay directory out of the way, and
// workflow.Session.StopManaging only finalizes (permanently deletes it)
// after the profile save that forgets the theme's metadata has also
// succeeded, or rolls the rename back if it has not -- so a failed save can
// never leave the profile referencing an artifact that is already gone.
func (p *themesStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
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
	provider := themesprovider.Provider{ProfileDir: p.opt.profileDir}
	if err := provider.PrepareStopManagingArtifact(id); err != nil {
		return profile.Data{}, err
	}
	p.captureProvider = &provider
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
	pluginFingerprints := map[string]string{}
	for _, plugin := range current.Items {
		if plugin.Source != "builtin" {
			currentThirdParty[plugin.ID] = true
			pluginFingerprints[plugin.ID] = canonicalFingerprint(plugin)
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
			Fingerprint:     pluginFingerprints[id],
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

// filterPluginsForRestoreSkip fails closed (returns an error) if any
// desired target's Restore decision is missing or unresolved.
func (p pluginsStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterPluginsForRestoreSkip(d.Plugins, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	var shellMatched []restoreSkip
	saved, shellMatched = filterPluginsForEffectiveShellReference(p.deps, p.opt, saved, exact)
	matched = append(matched, shellMatched...)
	plan := provider.Plan(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, pluginSemantics(d), pluginsprovider.PlanOptions{Exact: exact})
	return recordRestoreSkips(plan, "plugins", matched), nil
}

func (p pluginsStateProvider) Verify(ctx context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterPluginsForRestoreSkip(d.Plugins, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	saved, _ = filterPluginsForEffectiveShellReference(p.deps, p.opt, saved, exact)
	return pluginsprovider.Verify(saved, current, pluginSemantics(d), pluginsprovider.VerifyOptions{Exact: exact}), nil
}

// filterPluginsForEffectiveShellReference removes from saved.Absent any
// tombstone the machine's actual, currently effective Shell configuration
// still references -- shared by Plan and Verify (round-2 review blocker)
// so a removal Plan skips for this reason can never turn into a Verify
// failure the way a Plan-only post-processing step could: RequiredThird
// PartyPlugins only reports references a proposed Shell merge would newly
// introduce, never ones the live document already has, so this is
// deliberately a separate, independent check. When Shell's live state
// cannot be established at all (no ShellPaths configured, or detection
// fails), every Absent entry is treated as blocked: "provider safety
// always wins over Exact" means unknown must fail closed, not open. Only
// meaningful under Exact; Additive never reads Absent at all.
func filterPluginsForEffectiveShellReference(deps Dependencies, opt *options, saved profile.Plugins, exact bool) (profile.Plugins, []restoreSkip) {
	if !exact || len(saved.Absent) == 0 {
		return saved, nil
	}
	referenced, ok := effectiveShellPluginReferences(deps, opt)
	var matched []restoreSkip
	absent := make([]profile.Plugin, 0, len(saved.Absent))
	for _, item := range saved.Absent {
		if ok && !referenced[item.ID] {
			absent = append(absent, item)
			continue
		}
		reason := fmt.Sprintf("plugin %q is still referenced by the effective Shell configuration; removal disabled", item.ID)
		if !ok {
			reason = fmt.Sprintf("plugin %q availability against the effective Shell configuration could not be determined; removal disabled", item.ID)
		}
		matched = append(matched, restoreSkip{Key: "plugin:" + item.ID, Reason: reason})
	}
	saved.Absent = absent
	return saved, matched
}

// effectiveShellPluginReferences reports the machine's actual, currently
// effective Shell configuration's plugin references, and whether that
// state could be established at all (false on any detection failure).
func effectiveShellPluginReferences(deps Dependencies, opt *options) (map[string]bool, bool) {
	if deps.ShellPaths == nil {
		return nil, false
	}
	baseline, user, err := deps.ShellPaths()
	if err != nil {
		return nil, false
	}
	current, err := (shellprovider.Provider{BaselinePath: baseline, UserPath: user, ProfileDir: opt.profileDir}).Detect()
	if err != nil {
		return nil, false
	}
	target := current.Baseline
	if current.UserExists {
		target = current.Current
	}
	referenced := make(map[string]bool, len(target.References))
	for _, id := range target.References {
		referenced[id] = true
	}
	return referenced, true
}

// filterPluginsForRestoreSkip returns a copy of saved with every
// Item/Absent entry whose "plugin:<id>" target resolved to Restore Skip
// removed (present desired state and desired-absent tombstones alike --
// see the design's Restore Skip invariant, which does not distinguish the
// two), and the subset that actually matched an Item/Absent entry -- only
// that subset should be recorded as a visible Plan skip; a key with no
// matching desired state was never going to produce anything regardless of
// policy. A builtin Item is left untouched regardless: InspectTargets
// never creates a "plugin:<id>" target for one, unlike Absent, which
// InspectTargets always scopes a target for regardless of Source. Every
// other desired target key is resolved via resolveRestoreSkip
// (RestoreContext.Require), so a missing or unresolved decision fails the
// whole call closed.
func filterPluginsForRestoreSkip(saved profile.Plugins, restoreCtx workflow.RestoreContext) (profile.Plugins, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved

	items := make([]profile.Plugin, 0, len(saved.Items))
	for _, item := range saved.Items {
		if item.Source == "builtin" {
			items = append(items, item)
			continue
		}
		skip, entry, err := resolveRestoreSkip(restoreCtx, "plugins", "plugin:"+item.ID)
		if err != nil {
			return profile.Plugins{}, nil, err
		}
		if skip {
			matched = append(matched, entry)
			continue
		}
		items = append(items, item)
	}
	filtered.Items = items

	if len(saved.Absent) > 0 {
		absent := make([]profile.Plugin, 0, len(saved.Absent))
		for _, item := range saved.Absent {
			skip, entry, err := resolveRestoreSkip(restoreCtx, "plugins", "plugin:"+item.ID)
			if err != nil {
				return profile.Plugins{}, nil, err
			}
			if skip {
				matched = append(matched, entry)
				continue
			}
			absent = append(absent, item)
		}
		filtered.Absent = absent
	}

	return filtered, matched, nil
}

func (p pluginsStateProvider) Check(ctx context.Context, _ profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	_, err = provider.Detect(ctx)
	return err
}

// ValidateTarget accepts only a fully qualified plugin:<id> reference,
// canonicalized to itself. It never requires the plugin to currently exist
// -- a not-yet-captured or already-tombstoned id must validate too.
func (pluginsStateProvider) ValidateTarget(target string) (string, error) {
	id, ok := strings.CutPrefix(target, "plugin:")
	if !ok || id == "" || strings.ContainsAny(id, " \t\n/") {
		return "", fmt.Errorf("plugins: invalid target %q; use plugin:<id>", target)
	}
	return target, nil
}

// StopManaging permanently forgets one third-party plugin: its desired
// present or desired-absent state, and its local clone artifact directory.
// First-party (built-in) plugins carry no Blueprint-owned state to forget.
// It stages its artifact removal rather than deleting immediately:
// PrepareStopManagingArtifact renames the plugin's local clone directory out
// of the way, and workflow.Session.StopManaging only finalizes (permanently
// deletes it) after the profile save that forgets the plugin's metadata has
// also succeeded, or rolls the rename back if it has not -- so a failed save
// can never leave the profile referencing an artifact that is already gone.
func (p *pluginsStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
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
	provider := pluginsprovider.Provider{ProfileDir: p.opt.profileDir}
	if err := provider.PrepareStopManagingArtifact(id); err != nil {
		return profile.Data{}, err
	}
	p.captureProvider = &provider
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
// classifications (volatile, sensitive, unmanaged symlink, unsupported,
// oversized, ambiguous) stay CaptureEligible: false since provider safety
// checks remain authoritative over policy, but Capabilities marks them
// distinctly from delegated/excluded (see configEligibility): the former
// freeze already-safe remembered desired state untouched, the latter
// actively drop it as an ownership-management transition.
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
		eligible, reason, ownershipTransition := configEligibility(candidate.Classification)
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
		target := configTarget(candidate.Path, desired, current, eligible, reason, workflow.TargetCapabilities{
			SupportsCapture:            true,
			SupportsRestore:            true,
			SupportsDesiredAbsence:     true,
			SupportsExactRemoval:       eligible,
			Hierarchical:               true,
			DropsDesiredWhenIneligible: ownershipTransition,
			// A tracked instance of a classification configCaptureInert
			// treats as "nothing to capture" (UnchangedBaseline/
			// HistoricalBaseline) is not skipped like its untracked
			// counterpart above, since Blueprint still has desired state to
			// account for -- but real Capture still never produces a fresh
			// value for it (see capture.go), so the present-present
			// transition must report StopManaging, not the generic Update.
			NoActionableUpdate: configCaptureInert(candidate.Classification),
			RecordsNewAbsence:  configRecordsNewAbsence(candidate.Classification),
		})
		// The whole candidate: Capture persists the Omarchy baseline
		// hash/mode alongside the user's, including in deletion tombstones.
		target.Fingerprint = canonicalFingerprint(candidate)
		targets = append(targets, target)
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
// configRecordsNewAbsence reports classifications Capture turns into a
// desired-absent tombstone even when the path was never tracked: a deleted
// Omarchy default.
func configRecordsNewAbsence(classification configprovider.Classification) bool {
	return classification == configprovider.ConfigDeletedBaseline
}

func configCaptureInert(classification configprovider.Classification) bool {
	switch classification {
	case configprovider.ConfigUnchangedBaseline, configprovider.ConfigHistoricalBaseline:
		return true
	default:
		return false
	}
}

// configEligibility maps a Config scan Classification onto Capture/Restore
// eligibility. ownershipTransition distinguishes why an ineligible
// classification is ineligible, matching what real Capture (capture.go)
// actually does with previously desired state for it: Delegated (handed off
// to a stronger owner) and Excluded (the user explicitly said to leave this
// path alone) are ownership-management transitions -- Capture actively
// drops the target's desired state for them, regardless of policy. Every
// other ineligible classification (Sensitive, Volatile, Oversized,
// Unsupported, ambiguous, an unmanaged symlink) is a safety freeze instead:
// the current live bytes are unsafe or unreadable right now, so Capture
// cannot update from them, but it leaves already-safe remembered desired
// state completely untouched. Reporting both the same way as generic
// "Blocked" would misrepresent the ownership-transition cases, where
// something does happen to desired state; captureOutcome consults
// ownershipTransition (via TargetCapabilities.DropsDesiredWhenIneligible) to
// tell them apart.
func configEligibility(classification configprovider.Classification) (eligible bool, reason string, ownershipTransition bool) {
	switch classification {
	case configprovider.ConfigUnchangedBaseline, configprovider.ConfigModifiedBaseline, configprovider.ConfigHistoricalBaseline,
		configprovider.ConfigAdded, configprovider.ConfigDeletedBaseline:
		return true, "", false
	case configprovider.ConfigExcluded:
		return false, "excluded: you asked Config to leave this path alone", true
	case configprovider.ConfigDelegated:
		return false, "delegated to another provider", true
	case configprovider.ConfigVolatile:
		return false, "excluded as state-heavy/volatile by default", false
	case configprovider.ConfigSensitive:
		return false, "excluded as a likely secret/credential path", false
	case configprovider.ConfigUnmanagedSymlink:
		return false, "existing symlink is not owned by Blueprint", false
	case configprovider.ConfigUnsupported:
		return false, "unsupported file type", false
	case configprovider.ConfigOversized:
		return false, "exceeds the capture size limit", false
	case configprovider.ConfigAmbiguousBaseline, configprovider.ConfigAmbiguousDeletion:
		return false, "baseline provenance is ambiguous", false
	default:
		return false, "unrecognized classification", false
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

// Plan excludes every Restore-Skip ".config/..." path from the desired
// Files/Deletes fed to PlanOverlay, since every operation/skip PlanOverlay
// itself produces is already one-per-path (never batched); recordRestoreSkips
// then guarantees visibility for a path PlanOverlay's own logic would
// otherwise never mention (e.g. a desired-but-not-yet-written file). Its
// own Resource tags are "config:" + path, not the bare path InspectTargets
// uses as the target Key, so skips are re-prefixed before recording.
// filterConfigForRestoreSkip fails closed (returns an error) if any
// desired target's Restore decision is missing or unresolved.
func (p configStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Scan(d.Config)
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterConfigForRestoreSkip(d.Config, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	force := restoreCtx.Options.Conflicts == policy.ConflictForce
	plan, err := provider.PlanOverlay(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, configprovider.PlanOptions{Force: force})
	if err != nil {
		return model.RestorePlan{}, err
	}
	return recordRestoreSkips(plan, "config", prefixConfigSkips(matched)), nil
}

func (p configStateProvider) Verify(_ context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider(d)
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Scan(d.Config)
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterConfigForRestoreSkip(d.Config, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return provider.Verify(saved, current)
}

// filterConfigForRestoreSkip returns a copy of saved with every
// Files/Deletes entry whose path resolved to Restore Skip removed, and the
// subset of skip that actually matched a Files/Deletes entry -- only that
// subset should be recorded as a visible Plan skip; a key with no matching
// desired state was never going to produce anything regardless of policy.
// Every entry's target key is resolved via resolveRestoreSkip
// (RestoreContext.Require), so a missing or unresolved decision fails the
// whole call closed rather than silently letting that path's desired state
// through as an implicit Apply.
func filterConfigForRestoreSkip(saved profile.Configs, restoreCtx workflow.RestoreContext) (profile.Configs, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved
	files := make([]profile.ConfigFile, 0, len(saved.Files))
	for _, file := range saved.Files {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "config", file.Path)
		if err != nil {
			return profile.Configs{}, nil, err
		}
		if skip {
			matched = append(matched, entry)
			continue
		}
		files = append(files, file)
	}
	filtered.Files = files
	deletes := make([]profile.ConfigDelete, 0, len(saved.Deletes))
	for _, del := range saved.Deletes {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "config", del.Path)
		if err != nil {
			return profile.Configs{}, nil, err
		}
		if skip {
			matched = append(matched, entry)
			continue
		}
		deletes = append(deletes, del)
	}
	filtered.Deletes = deletes
	return filtered, matched, nil
}

// prefixConfigSkips re-keys skips with Config's own "config:" Resource
// prefix, so recordRestoreSkips's Resource matching lines up with what
// PlanOverlay actually tags its Operations/Skipped entries with.
func prefixConfigSkips(skips []restoreSkip) []restoreSkip {
	prefixed := make([]restoreSkip, len(skips))
	for i, skip := range skips {
		prefixed[i] = restoreSkip{Key: "config:" + skip.Key, Reason: skip.Reason}
	}
	return prefixed
}

func (p configStateProvider) Check(_ context.Context, d profile.Data) error {
	provider, err := p.provider(d)
	if err != nil {
		return err
	}
	return provider.Check(d.Config)
}

// ValidateTarget canonicalizes a raw or ergonomic (~/.config/-prefixed)
// config path to the canonical HOME-relative form ConfigFile.Path uses,
// reusing the same normalizer the exclude/include CLI already relies on. It
// never requires the path to currently exist or be captured.
func (configStateProvider) ValidateTarget(target string) (string, error) {
	return configprovider.NormalizeConfigPolicyPath(target)
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
			Fingerprint:     field.current,
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

// Plan clears the desired value of every Restore-Skip slot before handing
// it to the low-level planner, which already treats an empty desired value
// as "no desired state, no operation" -- exactly Restore Skip's meaning.
// recordRestoreSkips then guarantees visibility for a skip the low-level
// planner's own logic would not otherwise mention (its Resource tags are
// "default:" + kind, not the bare kind InspectTargets uses as the Key).
// filterDefaultsForRestoreSkip fails closed (returns an error) if any
// slot with an actual desired value has a missing or unresolved Restore
// decision.
func (p defaultsStateProvider) Plan(ctx context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterDefaultsForRestoreSkip(d.Defaults, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	plan := p.provider().Plan(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version)
	return recordRestoreSkips(plan, "defaults", prefixDefaultsSkips(matched)), nil
}

func (p defaultsStateProvider) Verify(ctx context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	current, err := p.provider().Detect(ctx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterDefaultsForRestoreSkip(d.Defaults, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return defaultsprovider.Verify(saved, current), nil
}

func (p defaultsStateProvider) Check(ctx context.Context, _ profile.Data) error {
	_, err := p.provider().Detect(ctx)
	return err
}

// filterDefaultsForRestoreSkip returns a copy of saved with the value of
// every Restore-Skip named slot that actually has a desired value cleared
// to "" -- the low-level planner already treats an empty desired value as
// "no desired state," so this alone prevents a slot's operation from being
// generated. The second return is the subset that actually matched a
// desired value -- only that subset should be recorded as a visible Plan
// skip; an empty slot has no desired state at all, so its Restore decision
// is never even resolved (there is nothing for it to own or protect).
func filterDefaultsForRestoreSkip(saved profile.Defaults, restoreCtx workflow.RestoreContext) (profile.Defaults, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved
	slots := []struct {
		key   string
		value *string
	}{
		{"terminal", &filtered.Terminal},
		{"browser", &filtered.Browser},
		{"editor", &filtered.Editor},
		{"agent", &filtered.Agent},
	}
	for _, slot := range slots {
		if *slot.value == "" {
			continue
		}
		skip, entry, err := resolveRestoreSkip(restoreCtx, "defaults", slot.key)
		if err != nil {
			return profile.Defaults{}, nil, err
		}
		if skip {
			matched = append(matched, entry)
			*slot.value = ""
		}
	}
	return filtered, matched, nil
}

// prefixDefaultsSkips re-keys skips with Defaults' own "default:" Resource
// prefix, so recordRestoreSkips's Resource matching lines up with what the
// low-level planner tags its Operations/Skipped entries with.
func prefixDefaultsSkips(skips []restoreSkip) []restoreSkip {
	prefixed := make([]restoreSkip, len(skips))
	for i, skip := range skips {
		prefixed[i] = restoreSkip{Key: "default:" + skip.Key, Reason: skip.Reason}
	}
	return prefixed
}

// ValidateTarget accepts only one of the four fixed slot names.
func (defaultsStateProvider) ValidateTarget(target string) (string, error) {
	switch target {
	case "terminal", "browser", "editor", "agent":
		return target, nil
	default:
		return "", fmt.Errorf("defaults: invalid target %q; use terminal, browser, editor, or agent", target)
	}
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
		Key:     "state",
		Label:   "state",
		Desired: desiredPresence(d.Shell.Hash != ""),
		Current: currentPresence(current.Status == shellprovider.StatusCustomized),
		// Capture persists the version and Omarchy baseline as well as the
		// user document.
		Fingerprint:     canonicalFingerprint(current),
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

// Plan clears the desired Hash when Shell's single "state" target is
// Restore Skip, before handing it to the low-level planner, which already
// treats an empty Hash as "no desired state" and returns immediately with
// an empty plan (see plan.go); recordRestoreSkips then adds the one visible
// "state" skip entry to that otherwise-empty plan. filterShellForRestoreSkip
// fails closed (returns an error) if Shell has a desired Hash but its
// Restore decision is missing or unresolved.
func (p shellStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider()
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterShellForRestoreSkip(d.Shell, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	force := restoreCtx.Options.Conflicts == policy.ConflictForce
	plan, err := provider.Plan(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, shellprovider.MergeOptions{Force: force})
	if err != nil {
		return model.RestorePlan{}, err
	}
	return recordRestoreSkips(plan, "shell", matched), nil
}

func (p shellStateProvider) Verify(_ context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider()
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterShellForRestoreSkip(d.Shell, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	return provider.Verify(saved, current)
}

// filterShellForRestoreSkip returns a copy of saved with Hash cleared when
// Shell's single "state" target resolved to Restore Skip -- the low-level
// planner/verifier already treats an empty Hash as "no desired state." The
// second return carries the skip forward only when there actually was a
// desired Hash to clear; a saved Hash of "" has no desired state at all, so
// its Restore decision is never even resolved.
func filterShellForRestoreSkip(saved profile.Shell, restoreCtx workflow.RestoreContext) (profile.Shell, []restoreSkip, error) {
	if saved.Hash == "" {
		return saved, nil, nil
	}
	skip, entry, err := resolveRestoreSkip(restoreCtx, "shell", "state")
	if err != nil {
		return profile.Shell{}, nil, err
	}
	if !skip {
		return saved, nil, nil
	}
	saved.Hash = ""
	return saved, []restoreSkip{entry}, nil
}

func (p shellStateProvider) Check(_ context.Context, d profile.Data) error {
	provider, err := p.provider()
	if err != nil {
		return err
	}
	return provider.Check(d.Shell, d.Plugins)
}

// ValidateTarget accepts only Shell's single fixed target.
func (shellStateProvider) ValidateTarget(target string) (string, error) {
	if target != "state" {
		return "", fmt.Errorf(`shell: invalid target %q; use "state"`, target)
	}
	return target, nil
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
	deps            Dependencies
	opt             *options
	captureProvider *hooksprovider.Provider
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
	hookFingerprints := map[string]string{}
	for _, hook := range current.Items {
		currentManaged[hook.Path] = true
		hookFingerprints[hook.Path] = canonicalFingerprint(profile.Hook{Path: hook.Path, Hash: hook.Hash, Mode: hook.Mode})
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
			Fingerprint:     hookFingerprints[path],
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

// Plan excludes every Restore-Skip hook path's Item from the desired state
// fed to the low-level planner, since every operation/skip it produces is
// already one-per-path (never batched); recordRestoreSkips then guarantees
// visibility for a path the low-level planner's own logic would not
// otherwise mention (e.g. a desired-but-not-yet-written hook). Its own
// Resource tags are "hook:" + path, not the bare path InspectTargets uses
// as the target Key, so skips are re-prefixed before recording.
// filterHooksForRestoreSkip fails closed (returns an error) if any desired
// target's Restore decision is missing or unresolved.
func (p hooksStateProvider) Plan(_ context.Context, d profile.Data, info omarchy.Info, restoreCtx workflow.RestoreContext) (model.RestorePlan, error) {
	provider, err := p.provider(d.Resources)
	if err != nil {
		return model.RestorePlan{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.RestorePlan{}, err
	}
	saved, matched, err := filterHooksForRestoreSkip(d.Hooks, restoreCtx)
	if err != nil {
		return model.RestorePlan{}, err
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	plan, err := provider.Plan(saved, current, d.Manifest.Schema, d.Manifest.Omarchy.CapturedVersion, info.Version, hooksprovider.PlanOptions{Exact: exact})
	if err != nil {
		return model.RestorePlan{}, err
	}
	return recordRestoreSkips(plan, "hooks", prefixHooksSkips(matched)), nil
}

func (p hooksStateProvider) Verify(_ context.Context, d profile.Data, restoreCtx workflow.RestoreContext) (model.VerificationResult, error) {
	provider, err := p.provider(d.Resources)
	if err != nil {
		return model.VerificationResult{}, err
	}
	current, err := provider.Detect()
	if err != nil {
		return model.VerificationResult{}, err
	}
	saved, _, err := filterHooksForRestoreSkip(d.Hooks, restoreCtx)
	if err != nil {
		return model.VerificationResult{}, err
	}
	exact := restoreCtx.Options.Convergence == policy.ConvergenceExact
	return provider.Verify(saved, current, hooksprovider.VerifyOptions{Exact: exact})
}

// filterHooksForRestoreSkip returns a copy of saved with every
// Item/Absent entry whose path resolved to Restore Skip removed (present
// desired state and desired-absent tombstones alike -- see the design's
// Restore Skip invariant, which does not distinguish the two), and the
// subset that actually matched an Item/Absent entry -- only that subset
// should be recorded as a visible Plan skip; a path with no matching
// desired state was never going to produce anything regardless of policy.
// Every entry's target key is resolved via resolveRestoreSkip
// (RestoreContext.Require), so a missing or unresolved decision fails the
// whole call closed.
func filterHooksForRestoreSkip(saved profile.Hooks, restoreCtx workflow.RestoreContext) (profile.Hooks, []restoreSkip, error) {
	var matched []restoreSkip
	filtered := saved

	items := make([]profile.Hook, 0, len(saved.Items))
	for _, item := range saved.Items {
		skip, entry, err := resolveRestoreSkip(restoreCtx, "hooks", item.Path)
		if err != nil {
			return profile.Hooks{}, nil, err
		}
		if skip {
			matched = append(matched, entry)
			continue
		}
		items = append(items, item)
	}
	filtered.Items = items

	if len(saved.Absent) > 0 {
		absent := make([]profile.Hook, 0, len(saved.Absent))
		for _, item := range saved.Absent {
			skip, entry, err := resolveRestoreSkip(restoreCtx, "hooks", item.Path)
			if err != nil {
				return profile.Hooks{}, nil, err
			}
			if skip {
				matched = append(matched, entry)
				continue
			}
			absent = append(absent, item)
		}
		filtered.Absent = absent
	}

	return filtered, matched, nil
}

// prefixHooksSkips re-keys skips with Hooks' own "hook:" Resource prefix,
// so recordRestoreSkips's Resource matching lines up with what the
// low-level planner tags its Operations/Skipped entries with.
func prefixHooksSkips(skips []restoreSkip) []restoreSkip {
	prefixed := make([]restoreSkip, len(skips))
	for i, skip := range skips {
		prefixed[i] = restoreSkip{Key: "hook:" + skip.Key, Reason: skip.Reason}
	}
	return prefixed
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
// ValidateTarget reuses the same path shape validation Capture itself
// enforces on every hook path, so a target can never validate here in a
// shape Capture could not otherwise have produced.
func (hooksStateProvider) ValidateTarget(target string) (string, error) {
	if err := hooksprovider.ValidatePath(target); err != nil {
		return "", fmt.Errorf("hooks: %w", err)
	}
	return target, nil
}

// StopManaging stages its artifact removal rather than deleting immediately:
// PrepareStopManagingArtifact renames the hook's captured snapshot file out
// of the way, and workflow.Session.StopManaging only finalizes (permanently
// deletes it) after the profile save that forgets the hook's metadata has
// also succeeded, or rolls the rename back if it has not -- so a failed save
// can never leave the profile referencing an artifact that is already gone.
func (p *hooksStateProvider) StopManaging(_ context.Context, d profile.Data, target string) (profile.Data, error) {
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
	provider := hooksprovider.Provider{ProfileDir: p.opt.profileDir}
	if err := provider.PrepareStopManagingArtifact(target); err != nil {
		return profile.Data{}, err
	}
	p.captureProvider = &provider
	return d, nil
}

func (p *hooksStateProvider) CommitCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	return p.captureProvider.CommitCapture()
}
func (p *hooksStateProvider) FinalizeCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	err := p.captureProvider.FinalizeCapture()
	p.captureProvider = nil
	return err
}
func (p *hooksStateProvider) RollbackCapture() error {
	if p.captureProvider == nil {
		return nil
	}
	err := p.captureProvider.RollbackCapture()
	p.captureProvider = nil
	return err
}
