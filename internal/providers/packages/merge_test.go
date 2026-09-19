package packages

import (
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func alwaysEnabled(string) bool  { return true }
func alwaysDisabled(string) bool { return false }

// TestMergeOfficialAURTransitionTable covers every row of the Capture merge
// invariant table for the flat official/aur namespaces.
func TestMergeOfficialAURTransitionTable(t *testing.T) {
	cases := []struct {
		name           string
		previous       profile.Packages
		current        profile.Packages
		enabled        func(string) bool
		wantOfficial   []string
		wantAbsentRefs []string
	}{
		{
			name:         "unknown + present + enabled -> present",
			previous:     profile.Packages{},
			current:      profile.Packages{Official: []string{"htop"}},
			enabled:      alwaysEnabled,
			wantOfficial: []string{"htop"},
		},
		{
			name:           "present + absent + enabled -> tombstone",
			previous:       profile.Packages{Official: []string{"htop"}},
			current:        profile.Packages{},
			enabled:        alwaysEnabled,
			wantAbsentRefs: []string{"official:htop"},
		},
		{
			name:         "absent + present + enabled -> present",
			previous:     profile.Packages{Absent: []profile.PackageAbsence{{Ref: "official:htop"}}},
			current:      profile.Packages{Official: []string{"htop"}},
			enabled:      alwaysEnabled,
			wantOfficial: []string{"htop"},
		},
		{
			name:         "present + absent + disabled -> preserve present",
			previous:     profile.Packages{Official: []string{"htop"}},
			current:      profile.Packages{},
			enabled:      alwaysDisabled,
			wantOfficial: []string{"htop"},
		},
		{
			name:           "absent + present + disabled -> preserve absent",
			previous:       profile.Packages{Absent: []profile.PackageAbsence{{Ref: "official:htop"}}},
			current:        profile.Packages{Official: []string{"htop"}},
			enabled:        alwaysDisabled,
			wantAbsentRefs: []string{"official:htop"},
		},
		{
			name:     "unknown + present + disabled -> remain unmanaged",
			previous: profile.Packages{},
			current:  profile.Packages{Official: []string{"htop"}},
			enabled:  alwaysDisabled,
		},
		{
			name:         "unknown + absent + enabled -> remain unmanaged (no tombstone for a never-managed target)",
			previous:     profile.Packages{},
			current:      profile.Packages{},
			enabled:      alwaysEnabled,
			wantOfficial: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge(c.previous, c.current, c.enabled)
			if !reflect.DeepEqual(got.Official, c.wantOfficial) {
				t.Fatalf("official = %#v, want %#v", got.Official, c.wantOfficial)
			}
			var gotAbsent []string
			for _, absence := range got.Absent {
				gotAbsent = append(gotAbsent, absence.Ref)
			}
			if !reflect.DeepEqual(gotAbsent, c.wantAbsentRefs) {
				t.Fatalf("absent = %#v, want %#v", gotAbsent, c.wantAbsentRefs)
			}
		})
	}
}

func TestMergeAURUsesTheSameTransitionTable(t *testing.T) {
	previous := profile.Packages{AUR: []string{"yay"}}
	current := profile.Packages{}
	got := Merge(previous, current, alwaysEnabled)
	if len(got.Absent) != 1 || got.Absent[0].Ref != "aur:yay" {
		t.Fatalf("absent = %#v, want a tombstone for aur:yay", got.Absent)
	}
}

func TestMergeCarriesMachineSpecificAndInstalledFromCurrentUnchanged(t *testing.T) {
	previous := profile.Packages{}
	current := profile.Packages{MachineSpecific: []string{"official:nvidia-utils"}, Installed: []string{"htop"}}
	got := Merge(previous, current, alwaysEnabled)
	if !reflect.DeepEqual(got.MachineSpecific, current.MachineSpecific) {
		t.Fatalf("machine-specific = %#v, want carried unchanged from current: hardware packages are never portable targets", got.MachineSpecific)
	}
	if !reflect.DeepEqual(got.Installed, current.Installed) {
		t.Fatalf("installed = %#v, want carried unchanged from current", got.Installed)
	}
	if len(got.Official) != 0 || len(got.Absent) != 0 {
		t.Fatalf("official=%#v absent=%#v, want machine-specific packages never merged as portable targets", got.Official, got.Absent)
	}
}

func TestMergeMiseUpdatesChangedDeclarationWhenEnabled(t *testing.T) {
	previous := profile.Packages{Mise: profile.MiseTools{"node": {"version": "22"}}}
	current := profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}}}
	got := Merge(previous, current, alwaysEnabled)
	if !EqualMiseTool(got.Mise["node"], profile.MiseTool{"version": "24"}) {
		t.Fatalf("mise[node] = %#v, want updated to the current declaration", got.Mise["node"])
	}
}

func TestMergeMisePreservesChangedLocalDeclarationWhenCaptureDisabled(t *testing.T) {
	previous := profile.Packages{Mise: profile.MiseTools{"node": {"version": "22"}}}
	current := profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}}}
	got := Merge(previous, current, alwaysDisabled)
	if !EqualMiseTool(got.Mise["node"], profile.MiseTool{"version": "22"}) {
		t.Fatalf("mise[node] = %#v, want preserved at the desired declaration despite the changed local value", got.Mise["node"])
	}
}

func TestMergeMiseTombstonePreservesPriorDeclaration(t *testing.T) {
	previous := profile.Packages{Mise: profile.MiseTools{"node": {"version": "22"}}}
	current := profile.Packages{}
	got := Merge(previous, current, alwaysEnabled)
	if len(got.Absent) != 1 || got.Absent[0].Ref != "mise:node" {
		t.Fatalf("absent = %#v, want a tombstone for mise:node", got.Absent)
	}
	if !EqualMiseTool(got.Absent[0].Mise, profile.MiseTool{"version": "22"}) {
		t.Fatalf("absent mise declaration = %#v, want the prior validated declaration preserved", got.Absent[0].Mise)
	}
}

func TestMergeMiseUnTombstonesWithCurrentDeclaration(t *testing.T) {
	previous := profile.Packages{Absent: []profile.PackageAbsence{{Ref: "mise:node", Mise: profile.MiseTool{"version": "22"}}}}
	current := profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}}}
	got := Merge(previous, current, alwaysEnabled)
	if len(got.Absent) != 0 {
		t.Fatalf("absent = %#v, want the tombstone cleared", got.Absent)
	}
	if !EqualMiseTool(got.Mise["node"], profile.MiseTool{"version": "24"}) {
		t.Fatalf("mise[node] = %#v, want the newly captured declaration", got.Mise["node"])
	}
}
