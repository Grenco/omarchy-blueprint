// Package autosync holds Auto Sync's machine-local authority: the grant, its
// private store, the capability registry and the evaluator that decides
// whether each operation of a fresh Restore plan may run without a person
// present. It never executes anything. See ADR 0029 and the capability audit
// (docs/planning/audits/2026-10-17-auto-sync-restore-capabilities.md).
package autosync

import (
	"sort"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// CapabilityVersion names the registry's meaning. A grant confirmed against a
// different version cannot authorise anything until reconfirmed.
const CapabilityVersion = "restore-capabilities/1"

// Class is the lowest preset whose ceiling admits an operation.
type Class int

const (
	ClassNone         Class = iota // nothing is applied (Notify only)
	ClassConservative              // add only, positively inert
	ClassBalanced                  // routine updates; no user-controlled code
	ClassHandsOff                  // code-running and provenance-checked removal, with local run-code authorisation
)

func (c Class) String() string {
	switch c {
	case ClassConservative:
		return "conservative"
	case ClassBalanced:
		return "balanced"
	case ClassHandsOff:
		return "hands-off"
	}
	return "none"
}

// Categories are the provider categories Auto Sync can reason about.
var Categories = []string{"themes", "defaults", "config", "shell", "hooks", "plugins", "packages", "services", "resources"}

func knownCategory(category string) bool {
	for _, c := range Categories {
		if c == category {
			return true
		}
	}
	return false
}

// Condition names a fact that must be positively proven by fresh evidence
// for an entry to apply. Unproven means review.
type Condition string

const (
	CondSourceHashEnforced   Condition = "source-hash-enforced"    // copy/write carries SourceHash and rejects symlink parents (E5)
	CondNotDesiredCurrent    Condition = "not-desired-current"     // theme is not the desired active theme
	CondAuditedOmarchy       Condition = "audited-omarchy-version" // Omarchy scripts match the audited version
	CondNoThemeHook          Condition = "no-theme-hook"           // no user theme-set hook exists
	CondInertThemeFiles      Condition = "inert-theme-files"       // every theme file is in a supported inert format
	CondInstalledApp         Condition = "installed-app"           // the chosen default app is installed
	CondPositiveEvidence     Condition = "positive-evidence"       // audit §2.4, including transitive includes
	CondInertDestination     Condition = "inert-destination"       // registered inert destination class
	CondPrecondition         Condition = "precondition"            // expected-state precondition on the destination
	CondWholeGraphNoExec     Condition = "whole-graph-no-exec"     // hyprctl reload: no exec anywhere in the sourced graph
	CondFirstPartyPlugins    Condition = "first-party-plugins"     // every referenced/enabled plugin is Omarchy-shipped
	CondUnitInertness        Condition = "unit-inertness"          // audit §4.7 proof
	CondCloneAtRevision      Condition = "clone-at-revision"       // fetched at the exact recorded revision before placement
	CondProvenance           Condition = "provenance"              // Blueprint installed or captured the item being removed
	CondNotActiveTheme       Condition = "not-active-theme"        // removal never targets the active theme
	CondSignedRepoHelper     Condition = "signed-repo-helper"      // administrator-installed helper (E10)
	CondAURMechanism         Condition = "aur-mechanism"           // reviewed AUR build/verify/install (E12)
	CondRecipeSupported      Condition = "recipe-supported"        // per-recipe support (E13)
	CondVersionPins          Condition = "version-pins"            // exact tool versions
	CondBackendVerification  Condition = "backend-verification"    // checksum or signature verification
	CondNoninteractive       Condition = "noninteractive-mechanism"
	CondRepositoryAtRevision Condition = "repository-at-revision"
)

// Entry is one audited way an operation family may run unattended. A family
// may have several entries at different classes (for example a Balanced
// entry needing positive evidence and a Hands-off entry that does not).
type Entry struct {
	Family string
	Class  Class
	// Changes, Payloads and Sources restrict the (evidenced) descriptor
	// shapes the entry covers. Empty means any.
	Changes  []model.EffectChange
	Payloads []model.EffectPayload
	Sources  []model.EffectSource
	// RunCode lists the categories whose "may run code" authorisation is
	// required. Privileged requires the privileged-install authorisation.
	RunCode    []string
	Privileged bool
	Conditions []Condition
	Increment  string // I1..I4
	// Delivered is true only once the entry's engineering, conditions and
	// tests have shipped. Undelivered entries describe "not available yet".
	Delivered bool
}

func (e Entry) Category() string { return categoryOf(e.Family) }

func categoryOf(family string) string {
	for i := 0; i < len(family); i++ {
		if family[i] == '.' {
			return family[:i]
		}
	}
	return ""
}

// Registry is the versioned capability registry.
type Registry struct {
	Version string
	Entries []Entry
}

func (r Registry) entries(family string) []Entry {
	var out []Entry
	for _, e := range r.Entries {
		if e.Family == family {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out
}

// Families returns the registered families of a category.
func (r Registry) Families(category string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range r.Entries {
		if e.Category() == category && !seen[e.Family] {
			seen[e.Family] = true
			out = append(out, e.Family)
		}
	}
	sort.Strings(out)
	return out
}

func (r Registry) known(family string) bool { return len(r.entries(family)) > 0 }

var (
	add, update, remove, none = model.EffectAdd, model.EffectUpdate, model.EffectRemove, model.EffectNoChange
	inert                     = []model.EffectPayload{model.PayloadData, model.PayloadConfig}
	nonePayload               = []model.EffectPayload{model.PayloadNone}
)

func handsOff(family, increment string, runCode []string, conditions ...Condition) Entry {
	return Entry{Family: family, Class: ClassHandsOff, RunCode: runCode, Conditions: conditions, Increment: increment}
}

// DefaultRegistry is the audited v1 registry (capability audit §4). No entry
// is delivered in Phase 11 PR A: each is delivered by the PR that ships its
// engineering, conditions and tests. Operation forms the audit classifies as
// never unattended (T3), such as Force replacement, have no entry.
func DefaultRegistry() Registry {
	themes, hooks, config, plugins := []string{"themes"}, []string{"hooks"}, []string{"config"}, []string{"plugins"}
	services, packages, resources := []string{"services"}, []string{"packages"}, []string{"resources"}
	return Registry{Version: CapabilityVersion, Entries: []Entry{
		// Themes (§4.1)
		{Family: "themes.copy-local", Class: ClassConservative, Changes: []model.EffectChange{add}, Payloads: []model.EffectPayload{model.PayloadData}, Sources: []model.EffectSource{model.SourceArtifact}, Conditions: []Condition{CondSourceHashEnforced, CondNotDesiredCurrent, CondAuditedOmarchy}, Increment: "I1"},
		handsOff("themes.copy-overlay", "I2", themes, CondSourceHashEnforced),
		{Family: "themes.activate", Class: ClassBalanced, Payloads: inert, Conditions: []Condition{CondPositiveEvidence, CondAuditedOmarchy, CondNoThemeHook, CondInertThemeFiles}, Increment: "I2"},
		// A present hook makes activation Hands-off with Themes and Hooks
		// authorisation; a separate hook permission never downgrades it.
		handsOff("themes.activate", "I2", []string{"themes", "hooks"}, CondAuditedOmarchy),
		handsOff("themes.install-git", "I3", themes, CondCloneAtRevision),
		handsOff("themes.pin-git", "I3", themes, CondCloneAtRevision),
		handsOff("themes.remove", "I4", themes, CondProvenance, CondNotActiveTheme),
		// Defaults (§4.2): switching a default is an update.
		{Family: "defaults.set", Class: ClassBalanced, Changes: []model.EffectChange{update}, Payloads: []model.EffectPayload{model.PayloadConfig}, Sources: []model.EffectSource{model.SourceClosedSet}, Conditions: []Condition{CondInstalledApp}, Increment: "I1"},
		// Config (§4.3)
		{Family: "config.write", Class: ClassConservative, Changes: []model.EffectChange{add}, Payloads: inert, Sources: []model.EffectSource{model.SourceArtifact}, Conditions: []Condition{CondPositiveEvidence, CondInertDestination, CondSourceHashEnforced}, Increment: "I1"},
		{Family: "config.write", Class: ClassBalanced, Changes: []model.EffectChange{add, update}, Payloads: inert, Sources: []model.EffectSource{model.SourceArtifact}, Conditions: []Condition{CondPositiveEvidence, CondPrecondition, CondSourceHashEnforced}, Increment: "I2"},
		handsOff("config.write", "I2", config, CondPrecondition, CondSourceHashEnforced),
		{Family: "config.reload", Class: ClassBalanced, Payloads: nonePayload, Conditions: []Condition{CondWholeGraphNoExec}, Increment: "I2"},
		handsOff("config.reload", "I2", config),
		handsOff("config.delete", "I4", config, CondProvenance, CondPrecondition),
		// Shell (§4.4)
		{Family: "shell.write", Class: ClassBalanced, Payloads: inert, Conditions: []Condition{CondFirstPartyPlugins, CondPositiveEvidence}, Increment: "I3"},
		handsOff("shell.write", "I3", []string{"shell", "plugins"}),
		{Family: "shell.restart", Class: ClassBalanced, Payloads: nonePayload, Conditions: []Condition{CondFirstPartyPlugins}, Increment: "I3"},
		handsOff("shell.restart", "I3", []string{"shell", "plugins"}),
		// Hooks (§4.5)
		handsOff("hooks.write", "I2", hooks, CondSourceHashEnforced),
		handsOff("hooks.delete", "I4", hooks, CondProvenance),
		// Plugins (§4.6)
		handsOff("plugins.copy-local", "I3", plugins, CondSourceHashEnforced),
		handsOff("plugins.install-git", "I3", plugins, CondCloneAtRevision),
		handsOff("plugins.pin-git", "I3", plugins, CondCloneAtRevision),
		handsOff("plugins.validate", "I3", plugins),
		handsOff("plugins.rescan", "I3", plugins),
		handsOff("plugins.enable", "I3", plugins),
		{Family: "plugins.disable", Class: ClassBalanced, Changes: []model.EffectChange{update}, Increment: "I3"},
		handsOff("plugins.remove", "I4", plugins, CondProvenance),
		// Services (§4.7): the one named safe-destination exception.
		{Family: "services.write-unit", Class: ClassBalanced, Payloads: inert, Conditions: []Condition{CondUnitInertness, CondPrecondition}, Increment: "I3"},
		handsOff("services.write-unit", "I3", services, CondPrecondition),
		handsOff("services.write-dropin", "I3", services, CondPrecondition),
		{Family: "services.daemon-reload", Class: ClassBalanced, Changes: []model.EffectChange{none}, Payloads: nonePayload, Increment: "I3"},
		handsOff("services.link", "I3", services),
		handsOff("services.enable", "I3", services),
		handsOff("services.start", "I3", services),
		handsOff("services.disable", "I4", services, CondProvenance),
		handsOff("services.mask", "I4", services, CondProvenance),
		handsOff("services.remove-file", "I4", services, CondProvenance),
		// Packages (§4.8): each kind of installation has its own mechanism.
		{Family: "packages.install-official", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondSignedRepoHelper, CondNoninteractive}, Increment: "I4"},
		{Family: "packages.install-aur", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondAURMechanism, CondNoninteractive}, Increment: "I4"},
		{Family: "packages.install-recipe", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondRecipeSupported, CondNoninteractive}, Increment: "I4"},
		{Family: "packages.preinstall-add", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondSignedRepoHelper, CondNoninteractive}, Increment: "I4"},
		handsOff("packages.mise-install", "I4", packages, CondVersionPins, CondBackendVerification),
		handsOff("packages.mise-configure", "I4", packages, CondPrecondition),
		handsOff("packages.mise-guard", "I4", packages),
		handsOff("packages.mise-remove", "I4", packages, CondProvenance),
		{Family: "packages.remove-official", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondProvenance, CondSignedRepoHelper, CondNoninteractive}, Increment: "I4"},
		{Family: "packages.remove-aur", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondProvenance, CondAURMechanism, CondNoninteractive}, Increment: "I4"},
		{Family: "packages.remove-recipe", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondProvenance, CondRecipeSupported, CondNoninteractive}, Increment: "I4"},
		{Family: "packages.preinstall-remove", Class: ClassHandsOff, RunCode: packages, Privileged: true, Conditions: []Condition{CondProvenance, CondSignedRepoHelper, CondNoninteractive}, Increment: "I4"},
		// Resources (§4.9)
		{Family: "resources.file", Class: ClassConservative, Changes: []model.EffectChange{add}, Payloads: inert, Sources: []model.EffectSource{model.SourceArtifact}, Conditions: []Condition{CondPositiveEvidence, CondInertDestination}, Increment: "I1"},
		{Family: "resources.copy", Class: ClassConservative, Changes: []model.EffectChange{add}, Payloads: inert, Sources: []model.EffectSource{model.SourceArtifact}, Conditions: []Condition{CondPositiveEvidence, CondInertDestination}, Increment: "I1"},
		{Family: "resources.link", Class: ClassConservative, Changes: []model.EffectChange{add}, Payloads: inert, Conditions: []Condition{CondPositiveEvidence, CondInertDestination}, Increment: "I1"},
		{Family: "resources.mkdir", Class: ClassConservative, Changes: []model.EffectChange{add}, Payloads: []model.EffectPayload{model.PayloadData}, Conditions: []Condition{CondInertDestination}, Increment: "I1"},
		{Family: "resources.link-replace", Class: ClassBalanced, Changes: []model.EffectChange{update}, Payloads: inert, Conditions: []Condition{CondPositiveEvidence, CondInertDestination, CondPrecondition}, Increment: "I2"},
		handsOff("resources.file", "I2", resources),
		handsOff("resources.copy", "I2", resources),
		handsOff("resources.link", "I2", resources),
		handsOff("resources.link-replace", "I2", resources, CondPrecondition),
		handsOff("resources.git-clone", "I3", resources, CondCloneAtRevision),
		handsOff("resources.git-checkout", "I3", resources, CondCloneAtRevision),
		handsOff("resources.git-apply-index", "I3", resources, CondRepositoryAtRevision),
		handsOff("resources.git-apply-worktree", "I3", resources, CondRepositoryAtRevision),
		handsOff("resources.git-untracked", "I3", resources, CondRepositoryAtRevision),
	}}
}
