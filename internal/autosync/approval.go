package autosync

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// ExpansionKind names one way a proposed grant authorises more than the
// current one. The UI translates each into plain English.
type ExpansionKind string

const (
	ExpandIdentity          ExpansionKind = "identity-changed"
	ExpandCapabilityVersion ExpansionKind = "capability-version-changed"
	ExpandRestoreLink       ExpansionKind = "restore-link-changed"
	ExpandCategory          ExpansionKind = "category-added"
	ExpandCeiling           ExpansionKind = "automation-level-raised"
	ExpandAllTargets        ExpansionKind = "all-present-and-future-targets"
	ExpandTargets           ExpansionKind = "targets-added"
	ExpandTargetException   ExpansionKind = "target-exception-removed"
	ExpandFamilyException   ExpansionKind = "operation-type-exception-removed"
	ExpandFamilies          ExpansionKind = "operation-types-added"
	ExpandRunCode           ExpansionKind = "may-run-code"
	ExpandPrivilegedInstall ExpansionKind = "privileged-install"
)

// Expansion is one exact widening.
type Expansion struct {
	Kind     ExpansionKind `json:"kind"`
	Category string        `json:"category,omitempty"`
	// Before and After are the category's automation level, for
	// category-added and automation-level-raised.
	Before Class `json:"before,omitempty"`
	After  Class `json:"after,omitempty"`
	// AllTargets: every present and future target in the category is now
	// covered. Targets lists the named targets newly covered, or the target
	// exceptions removed.
	AllTargets bool     `json:"all_targets,omitempty"`
	Targets    []string `json:"targets,omitempty"`
	// AllFamilies: every registered operation type is now covered. Families
	// lists the operation types newly allowed, un-excepted or allowed to run
	// code (resolved from the registry when AllFamilies is set).
	AllFamilies bool     `json:"all_families,omitempty"`
	Families    []string `json:"families,omitempty"`
	// Fields names the identity fields that changed, for identity-changed.
	Fields []string `json:"fields,omitempty"`
	// RunsCode: after this change, operations reached through it can run
	// code on this computer.
	RunsCode bool `json:"runs_code,omitempty"`
}

// Scope is the exact authority of a grant, for before/after display. The
// identity carries only one-way digests for the host and remote, never the
// raw machine ID or remote URL.
type Scope struct {
	Identity          Identity                 `json:"identity"`
	CapabilityVersion string                   `json:"capability_version"`
	Preset            Preset                   `json:"preset"`
	RestoreLink       RestoreLink              `json:"restore_link"`
	Categories        map[string]CategoryScope `json:"categories"`
	RunCode           map[string]RunCodeScope  `json:"run_code"`
	PrivilegedInstall bool                     `json:"privileged_install"`
}

// Change is the confirmation payload: exact before and after scope, the
// lifecycle change, and every expansion. A person must be able to see from
// it everything they are being asked to allow.
type Change struct {
	Enable          bool        `json:"enable"`           // off or paused → on with different authority
	ClearSuspension bool        `json:"clear_suspension"` // lifting the safety guard
	Before          Scope       `json:"before"`
	After           Scope       `json:"after"`
	Expansions      []Expansion `json:"expansions"`
}

func scopeOf(g Grant) Scope {
	a := g.authority()
	return Scope{Identity: a.Identity, CapabilityVersion: a.CapabilityVersion, Preset: a.Preset, RestoreLink: a.RestoreLink, Categories: a.Categories, RunCode: a.RunCode, PrivilegedInstall: a.PrivilegedInstall}
}

// Describe compares a proposal with the current grant. Family lists are
// resolved against the production registry.
func Describe(prev, next Grant) Change {
	c := Change{Before: scopeOf(prev), After: scopeOf(next), Expansions: expansions(prev, next, DefaultRegistry())}
	c.Enable = next.Setting == SettingOn && prev.Setting != SettingOn
	c.ClearSuspension = prev.Suspended != nil && next.Suspended == nil
	return c
}

func difference(a, b []string) []string {
	var out []string
	for _, v := range a {
		if !contains(b, v) {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// identityChanges names the identity fields that differ, by their payload
// keys, in sorted order.
func identityChanges(a, b Identity) []string {
	var out []string
	for _, f := range []struct {
		name       string
		prev, next string
	}{
		{"branch", a.Branch, b.Branch},
		{"host_id", a.HostID, b.HostID},
		{"machine", a.Machine, b.Machine},
		{"profile_root", a.ProfileRoot, b.ProfileRoot},
		{"remote_fingerprint", a.RemoteFingerprint, b.RemoteFingerprint},
		{"upstream", a.Upstream, b.Upstream},
	} {
		if f.prev != f.next {
			out = append(out, f.name)
		}
	}
	return out
}

// codeFamilies lists a category's registered families that need its
// run-code authorisation.
func codeFamilies(registry Registry, category string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range registry.Entries {
		if contains(e.RunCode, category) && !seen[e.Family] {
			seen[e.Family] = true
			out = append(out, e.Family)
		}
	}
	sort.Strings(out)
	return out
}

// expansions is the single source of truth for widening: Narrows is true
// exactly when it returns nothing, so the confirmation data can never omit
// a widening the approval rule detects.
func expansions(prev, next Grant, registry Registry) []Expansion {
	a, b := prev.authority(), next.authority()
	var out []Expansion
	if fields := identityChanges(a.Identity, b.Identity); len(fields) > 0 {
		out = append(out, Expansion{Kind: ExpandIdentity, Fields: fields})
	}
	if a.CapabilityVersion != b.CapabilityVersion {
		out = append(out, Expansion{Kind: ExpandCapabilityVersion})
	}
	if a.RestoreLink != b.RestoreLink {
		out = append(out, Expansion{Kind: ExpandRestoreLink})
	}
	categories := make([]string, 0, len(b.Categories))
	for category := range b.Categories {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		nextScope := b.Categories[category]
		_, runsCode := b.RunCode[category]
		prevScope, covered := a.Categories[category]
		if !covered {
			e := Expansion{Kind: ExpandCategory, Category: category, After: next.ceiling(category), AllTargets: nextScope.AllTargets, Targets: sortedCopy(nextScope.Targets), RunsCode: runsCode}
			if len(nextScope.AllowFamilies) == 0 {
				e.AllFamilies, e.Families = true, registry.Families(category)
			} else {
				e.Families = sortedCopy(nextScope.AllowFamilies)
			}
			out = append(out, e)
			continue
		}
		if next.ceiling(category) > prev.ceiling(category) {
			out = append(out, Expansion{Kind: ExpandCeiling, Category: category, Before: prev.ceiling(category), After: next.ceiling(category), RunsCode: runsCode})
		}
		if nextScope.AllTargets && !prevScope.AllTargets {
			out = append(out, Expansion{Kind: ExpandAllTargets, Category: category, AllTargets: true, RunsCode: runsCode})
		} else if !nextScope.AllTargets && !prevScope.AllTargets {
			if added := difference(nextScope.Targets, prevScope.Targets); len(added) > 0 {
				out = append(out, Expansion{Kind: ExpandTargets, Category: category, Targets: added, RunsCode: runsCode})
			}
		}
		if removed := difference(prevScope.DenyTargets, nextScope.DenyTargets); len(removed) > 0 {
			out = append(out, Expansion{Kind: ExpandTargetException, Category: category, Targets: removed, RunsCode: runsCode})
		}
		if removed := difference(prevScope.DenyFamilies, nextScope.DenyFamilies); len(removed) > 0 {
			out = append(out, Expansion{Kind: ExpandFamilyException, Category: category, Families: removed, RunsCode: runsCode})
		}
		if len(prevScope.AllowFamilies) > 0 {
			if len(nextScope.AllowFamilies) == 0 {
				out = append(out, Expansion{Kind: ExpandFamilies, Category: category, AllFamilies: true, Families: difference(registry.Families(category), prevScope.AllowFamilies), RunsCode: runsCode})
			} else if added := difference(nextScope.AllowFamilies, prevScope.AllowFamilies); len(added) > 0 {
				out = append(out, Expansion{Kind: ExpandFamilies, Category: category, Families: added, RunsCode: runsCode})
			}
		}
	}
	runCategories := make([]string, 0, len(b.RunCode))
	for category := range b.RunCode {
		runCategories = append(runCategories, category)
	}
	sort.Strings(runCategories)
	for _, category := range runCategories {
		nextRun := b.RunCode[category]
		prevRun, had := a.RunCode[category]
		switch {
		case !had && len(nextRun.Families) == 0:
			out = append(out, Expansion{Kind: ExpandRunCode, Category: category, AllFamilies: true, Families: codeFamilies(registry, category), RunsCode: true})
		case !had:
			out = append(out, Expansion{Kind: ExpandRunCode, Category: category, Families: sortedCopy(nextRun.Families), RunsCode: true})
		case len(prevRun.Families) > 0 && len(nextRun.Families) == 0:
			out = append(out, Expansion{Kind: ExpandRunCode, Category: category, AllFamilies: true, Families: difference(codeFamilies(registry, category), prevRun.Families), RunsCode: true})
		case len(prevRun.Families) > 0:
			if added := difference(nextRun.Families, prevRun.Families); len(added) > 0 {
				out = append(out, Expansion{Kind: ExpandRunCode, Category: category, Families: added, RunsCode: true})
			}
		}
	}
	if b.PrivilegedInstall && !a.PrivilegedInstall {
		out = append(out, Expansion{Kind: ExpandPrivilegedInstall, RunsCode: true})
	}
	return out
}

// Confirmer is a local, interactive confirmation surface (the TUI or a
// terminal prompt). A JSON, --yes or non-terminal path must report
// Interactive() == false.
type Confirmer interface {
	Interactive() bool
	ConfirmAutoSync(Change) (bool, error)
}

// LocalApproval proves a person confirmed this exact change on this
// computer. It can only be created by ApproveLocally, and binds to both the
// proposed grant's authority digest and the grant it was described against,
// so a confirmation shown against any other starting point is refused.
type LocalApproval struct {
	digest string
	base   string
}

// baseDigest identifies the grant a confirmation was described against:
// everything Describe compares, plus the generation and the confirmed
// authority, so a since-replaced grant does not match.
func baseDigest(g Grant) string {
	data, _ := json.Marshal(struct {
		Generation uint64  `json:"generation"`
		Setting    Setting `json:"setting"`
		Suspended  bool    `json:"suspended"`
		Approved   string  `json:"approved"`
		Authority  string  `json:"authority"`
	}{g.Generation, g.Setting, g.Suspended != nil, g.Approved, g.AuthorityDigest()})
	return fmt.Sprintf("%x", sha256.Sum256(data))
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
	return LocalApproval{digest: next.AuthorityDigest(), base: baseDigest(prev)}, nil
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
