package autosync

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// GrantVersion is the stored grant format.
const GrantVersion = 1

// Setting is the user's choice; outcomes of checks never rewrite it.
type Setting string

const (
	SettingOff    Setting = "off"
	SettingOn     Setting = "on"
	SettingPaused Setting = "paused"
)

type Preset string

const (
	PresetNotifyOnly   Preset = "notify-only"
	PresetConservative Preset = "conservative"
	PresetBalanced     Preset = "balanced"
	PresetHandsOff     Preset = "hands-off"
	PresetCustom       Preset = "custom"
)

// Ceiling is the highest class a preset admits. Custom uses per-category
// ceilings, never above Hands-off.
func (p Preset) Ceiling() Class {
	switch p {
	case PresetConservative:
		return ClassConservative
	case PresetBalanced:
		return ClassBalanced
	case PresetHandsOff, PresetCustom:
		return ClassHandsOff
	}
	return ClassNone
}

type RestoreLink string

const (
	// LinkLinked: current Restore rules continuously restrict a locally
	// confirmed ceiling; loosening Restore beyond it needs local review.
	LinkLinked RestoreLink = "linked"
	// LinkCopyOnce: the scope was copied from Restore when confirmed and is
	// managed separately afterwards.
	LinkCopyOnce RestoreLink = "copy-once"
	// LinkSeparate: the scope is configured independently.
	LinkSeparate RestoreLink = "separate"
)

// Identity binds a grant to this computer, profile and shared repository.
// HostID is a one-way digest (HostIdentity), never the raw machine ID.
type Identity struct {
	ProfileRoot       string `toml:"profile_root" json:"profile_root"`
	HostID            string `toml:"host_id" json:"host_id"`
	Machine           string `toml:"machine" json:"machine"`
	Branch            string `toml:"branch" json:"branch"`
	Upstream          string `toml:"upstream" json:"upstream"`
	RemoteFingerprint string `toml:"remote_fingerprint" json:"remote_fingerprint"`
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (i Identity) validate() error {
	if i.ProfileRoot == "" || !filepath.IsAbs(i.ProfileRoot) || filepath.Clean(i.ProfileRoot) != i.ProfileRoot {
		return errors.New("auto sync grant: invalid profile root")
	}
	if !hex64.MatchString(i.HostID) {
		return errors.New("auto sync grant: invalid host identity")
	}
	if machine.ValidateName(i.Machine) != nil {
		return errors.New("auto sync grant: invalid machine")
	}
	if !syncengine.ValidRefIdentity(i.Branch) || !syncengine.ValidRefIdentity(i.Upstream) {
		return errors.New("auto sync grant: invalid branch or upstream")
	}
	if !hex64.MatchString(i.RemoteFingerprint) {
		return errors.New("auto sync grant: invalid remote identity")
	}
	return nil
}

// CategoryScope is the locally confirmed scope within one category.
type CategoryScope struct {
	// AllTargets covers every current and future target in the category.
	AllTargets bool     `toml:"all_targets" json:"all_targets"`
	Targets    []string `toml:"targets,omitempty" json:"targets,omitempty"`
	// DenyTargets always win over AllTargets and Targets.
	DenyTargets []string `toml:"deny_targets,omitempty" json:"deny_targets,omitempty"`
	// AllowFamilies limits operation families (empty: all registered);
	// DenyFamilies always win.
	AllowFamilies []string `toml:"allow_families,omitempty" json:"allow_families,omitempty"`
	DenyFamilies  []string `toml:"deny_families,omitempty" json:"deny_families,omitempty"`
	// MaxClass is used by Custom only (presets use their own ceiling).
	MaxClass Class `toml:"max_class,omitempty" json:"max_class,omitempty"`
}

// RunCodeScope is a category's "may run code" authorisation. Empty
// Families covers every code-running family in the category.
type RunCodeScope struct {
	Families []string `toml:"families,omitempty" json:"families,omitempty"`
}

// Suspension is the safety guard set after a failed or uncertain run.
type Suspension struct {
	Reason string    `toml:"reason" json:"reason"`
	Since  time.Time `toml:"since" json:"since"`
}

// Grant is the machine-local Auto Sync authority record. It never lives in
// the Git profile.
type Grant struct {
	Version         int         `toml:"version"`
	Setting         Setting     `toml:"setting"`
	Suspended       *Suspension `toml:"suspended,omitempty"`
	Preset          Preset      `toml:"preset"`
	RestoreLink     RestoreLink `toml:"restore_link"`
	IntervalMinutes int         `toml:"interval_minutes"`
	Identity        Identity    `toml:"identity"`
	// Categories is the confirmed scope; absent categories are not covered.
	Categories        map[string]CategoryScope `toml:"categories,omitempty"`
	RunCode           map[string]RunCodeScope  `toml:"run_code,omitempty"`
	PrivilegedInstall bool                     `toml:"privileged_install"`
	CapabilityVersion string                   `toml:"capability_version"`
	// Generation increases on every save; a stale writer is refused.
	Generation uint64 `toml:"generation"`
	// Approved is the authority digest last confirmed locally. Resuming or
	// narrowing never needs a new confirmation; anything else does.
	Approved string `toml:"approved"`
}

// DefaultGrant is the state when nothing is stored: off, no scope.
func DefaultGrant() Grant {
	return Grant{Version: GrantVersion, Setting: SettingOff, Preset: PresetNotifyOnly, RestoreLink: LinkLinked, IntervalMinutes: 30, CapabilityVersion: CapabilityVersion}
}

// Interval bounds (spec §5.1).
const (
	MinIntervalMinutes = 15
	MaxIntervalMinutes = 24 * 60
	maxTargets         = 4096
	maxTargetLength    = 512
)

func validTarget(target string) bool {
	if target == "" || len(target) > maxTargetLength {
		return false
	}
	for _, r := range target {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

// Validate checks a grant's structure; it does not check identity against
// this computer (see CheckIdentity) or the registry version.
func (g Grant) Validate(registry Registry) error {
	if g.Version != GrantVersion {
		return errors.New("auto sync grant: unsupported version")
	}
	switch g.Setting {
	case SettingOff, SettingOn, SettingPaused:
	default:
		return errors.New("auto sync grant: invalid setting")
	}
	switch g.Preset {
	case PresetNotifyOnly, PresetConservative, PresetBalanced, PresetHandsOff, PresetCustom:
	default:
		return errors.New("auto sync grant: invalid preset")
	}
	switch g.RestoreLink {
	case LinkLinked, LinkCopyOnce, LinkSeparate:
	default:
		return errors.New("auto sync grant: invalid Restore link")
	}
	if g.IntervalMinutes < MinIntervalMinutes || g.IntervalMinutes > MaxIntervalMinutes {
		return errors.New("auto sync grant: interval out of range")
	}
	if g.CapabilityVersion == "" {
		return errors.New("auto sync grant: missing capability version")
	}
	if g.Suspended != nil && g.Suspended.Reason == "" {
		return errors.New("auto sync grant: suspension without reason")
	}
	if g.Setting != SettingOff || len(g.Categories) > 0 {
		if err := g.Identity.validate(); err != nil {
			return err
		}
	}
	targets := 0
	for category, scope := range g.Categories {
		if !knownCategory(category) {
			return fmt.Errorf("auto sync grant: unknown category %q", category)
		}
		for _, list := range [][]string{scope.Targets, scope.DenyTargets} {
			for _, target := range list {
				if !validTarget(target) {
					return errors.New("auto sync grant: invalid target")
				}
			}
			targets += len(list)
		}
		for _, list := range [][]string{scope.AllowFamilies, scope.DenyFamilies} {
			for _, family := range list {
				if categoryOf(family) != category || !registry.known(family) {
					return errors.New("auto sync grant: unknown operation family")
				}
			}
		}
		if scope.MaxClass < ClassNone || scope.MaxClass > ClassHandsOff {
			return errors.New("auto sync grant: invalid class")
		}
		if g.Preset != PresetCustom && scope.MaxClass != ClassNone {
			return errors.New("auto sync grant: per-category class is Custom only")
		}
	}
	if targets > maxTargets {
		return errors.New("auto sync grant: too many targets")
	}
	for category, scope := range g.RunCode {
		if !knownCategory(category) {
			return fmt.Errorf("auto sync grant: unknown run-code category %q", category)
		}
		for _, family := range scope.Families {
			if categoryOf(family) != category || !registry.known(family) {
				return errors.New("auto sync grant: unknown run-code family")
			}
		}
	}
	if (len(g.RunCode) > 0 || g.PrivilegedInstall) && g.Preset != PresetHandsOff && g.Preset != PresetCustom {
		return errors.New("auto sync grant: run-code and privileged-install authorisations need Hands-off or Custom")
	}
	return nil
}

// ErrIdentityMismatch means the grant belongs to another computer, profile,
// machine, branch, upstream or remote. It authorises nothing.
var ErrIdentityMismatch = errors.New("auto sync grant belongs to a different computer, profile or shared repository; confirm Auto Sync again on this computer")

// CheckIdentity requires an exact match of every bound identity field.
func (g Grant) CheckIdentity(current Identity) error {
	if g.Identity != current {
		return ErrIdentityMismatch
	}
	return nil
}

// authority is the part of a grant that decides what may run.
type authority struct {
	Preset            Preset                   `json:"preset"`
	RestoreLink       RestoreLink              `json:"restore_link"`
	Identity          Identity                 `json:"identity"`
	Categories        map[string]CategoryScope `json:"categories"`
	RunCode           map[string]RunCodeScope  `json:"run_code"`
	PrivilegedInstall bool                     `json:"privileged_install"`
	CapabilityVersion string                   `json:"capability_version"`
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func (g Grant) authority() authority {
	a := authority{Preset: g.Preset, RestoreLink: g.RestoreLink, Identity: g.Identity, Categories: map[string]CategoryScope{}, RunCode: map[string]RunCodeScope{}, PrivilegedInstall: g.PrivilegedInstall, CapabilityVersion: g.CapabilityVersion}
	for k, v := range g.Categories {
		v.Targets, v.DenyTargets, v.AllowFamilies, v.DenyFamilies = sortedCopy(v.Targets), sortedCopy(v.DenyTargets), sortedCopy(v.AllowFamilies), sortedCopy(v.DenyFamilies)
		a.Categories[k] = v
	}
	for k, v := range g.RunCode {
		a.RunCode[k] = RunCodeScope{Families: sortedCopy(v.Families)}
	}
	return a
}

// AuthorityDigest is a deterministic, non-secret digest of what the grant
// authorises. It is a consistency check, not protection against a process
// already running as this user.
func (g Grant) AuthorityDigest() string {
	data, _ := json.Marshal(g.authority())
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// Active reports whether the grant may authorise application at all.
func (g Grant) Active() bool {
	return g.Setting == SettingOn && g.Suspended == nil && g.Preset != PresetNotifyOnly
}

func (g Grant) ceiling(category string) Class {
	if g.Preset == PresetCustom {
		scope, ok := g.Categories[category]
		if !ok {
			return ClassNone
		}
		if scope.MaxClass > ClassHandsOff {
			return ClassHandsOff
		}
		return scope.MaxClass
	}
	return g.Preset.Ceiling()
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func subset(small, large []string) bool {
	for _, v := range small {
		if !contains(large, v) {
			return false
		}
	}
	return true
}

// Narrows reports whether next authorises nothing that prev does not. It
// is defined by expansions, so the confirmation data names every widening
// this rule detects.
func Narrows(prev, next Grant) bool {
	return len(expansions(prev, next, Registry{})) == 0
}
