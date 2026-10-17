package autosync

import (
	"errors"
	"sort"
)

// Change summarises what a proposed grant would newly authorise, for the
// local confirmation screen. It names categories, classes and families only.
type Change struct {
	Enable            bool     // off or paused → on with a different authority
	ClearSuspension   bool     // lifting the safety guard
	Preset            Preset   // proposed preset
	NewCategories     []string // categories not covered before
	RaisedCategories  []string // covered categories whose ceiling or scope grows
	NewRunCode        []string // categories newly allowed to run code
	PrivilegedInstall bool     // newly allowed privileged installs
	IdentityChanged   bool
	VersionChanged    bool
}

// Describe compares a proposal with the current grant.
func Describe(prev, next Grant) Change {
	c := Change{Preset: next.Preset, IdentityChanged: prev.Identity != next.Identity, VersionChanged: prev.CapabilityVersion != next.CapabilityVersion}
	c.Enable = next.Setting == SettingOn && prev.Setting != SettingOn
	c.ClearSuspension = prev.Suspended != nil && next.Suspended == nil
	c.PrivilegedInstall = next.PrivilegedInstall && !prev.PrivilegedInstall
	for category, scope := range next.Categories {
		old, ok := prev.Categories[category]
		if !ok {
			c.NewCategories = append(c.NewCategories, category)
			continue
		}
		single := Grant{Preset: prev.Preset, RestoreLink: prev.RestoreLink, Identity: prev.Identity, CapabilityVersion: prev.CapabilityVersion, Categories: map[string]CategoryScope{category: old}}
		proposed := Grant{Preset: next.Preset, RestoreLink: prev.RestoreLink, Identity: prev.Identity, CapabilityVersion: prev.CapabilityVersion, Categories: map[string]CategoryScope{category: scope}}
		if !Narrows(single, proposed) {
			c.RaisedCategories = append(c.RaisedCategories, category)
		}
	}
	for category := range next.RunCode {
		if _, ok := prev.RunCode[category]; !ok {
			c.NewRunCode = append(c.NewRunCode, category)
		}
	}
	sort.Strings(c.NewCategories)
	sort.Strings(c.RaisedCategories)
	sort.Strings(c.NewRunCode)
	return c
}

// Confirmer is a local, interactive confirmation surface (the TUI or a
// terminal prompt). A JSON, --yes or non-terminal path must report
// Interactive() == false.
type Confirmer interface {
	Interactive() bool
	ConfirmAutoSync(Change) (bool, error)
}

// LocalApproval proves a person confirmed this exact authority on this
// computer. It can only be created by ApproveLocally, and binds to the
// proposed grant's authority digest.
type LocalApproval struct {
	digest string
}

var (
	ErrNotInteractive = errors.New("Auto Sync permissions can only be granted or widened by a person at this computer")
	ErrNotConfirmed   = errors.New("Auto Sync permissions were not confirmed; nothing changed")
)

// ApproveLocally asks a person to confirm the proposed grant.
func ApproveLocally(prev, next Grant, c Confirmer) (LocalApproval, error) {
	if c == nil || !c.Interactive() {
		return LocalApproval{}, ErrNotInteractive
	}
	ok, err := c.ConfirmAutoSync(Describe(prev, next))
	if err != nil {
		return LocalApproval{}, err
	}
	if !ok {
		return LocalApproval{}, ErrNotConfirmed
	}
	return LocalApproval{digest: next.AuthorityDigest()}, nil
}

// needsApproval decides whether saving next over prev needs a local
// approval. Pausing, turning off, narrowing and resuming an already
// approved authority do not; enabling or widening anything, and lifting
// the safety guard, do.
func needsApproval(prev, next Grant) bool {
	if prev.Suspended != nil && next.Suspended == nil {
		return true
	}
	if next.Setting != SettingOn {
		return false
	}
	digest := next.AuthorityDigest()
	if prev.Approved != "" && digest == prev.Approved {
		return false
	}
	return !(prev.Setting == SettingOn && prev.Approved == prev.AuthorityDigest() && Narrows(prev, next))
}
