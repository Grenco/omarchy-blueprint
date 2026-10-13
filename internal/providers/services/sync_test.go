package services

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"reflect"
	"testing"
)

func TestSyncDesiredServicesSemanticIdentity(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Services = true
	u := profile.ServiceUnit{Name: "demo@.service", Kind: "service", Management: profile.ServiceManagementDefinition, Presence: profile.ServicePresent, StartIntent: profile.ServiceStartDisabled, Definition: "units/demo@.service", DefinitionHash: "content", DropIns: []profile.ServiceArtifact{{Path: "units/demo@.service.d/z.conf", Presence: profile.ServicePresent, Hash: "z"}, {Path: "units/demo@.service.d/a.conf", Presence: profile.ServicePresent, Hash: "a"}}, Instances: []profile.ServiceInstance{{Name: "demo@one.service", Presence: profile.ServicePresent, StartIntent: profile.ServiceStartEnabled}}}
	d.Services.Units = []profile.ServiceUnit{u}
	got, err := (&Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	// Existing policy identity is the template, not synthetic artifact targets.
	if len(got) != 1 || got[0].Key.Target != "demo@.service" || got[0].Key.Category != "services" {
		t.Fatal(got)
	}
	d.Services.Units[0].ObservedActive = true
	d.Services.Units[0].Instances[0].ObservedActive = true
	d.Services.Units[0].DropIns[0], d.Services.Units[0].DropIns[1] = d.Services.Units[0].DropIns[1], d.Services.Units[0].DropIns[0]
	same, err := (&Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("volatile runtime or order changed persistent intent")
	}
	d.Services.Units[0].StartIntent = profile.ServiceStartEnabled
	changed, _ := (&Provider{}).SyncDesired(d)
	if changed[0].State.Equal(got[0].State) {
		t.Fatal("start intent ignored")
	}
	d.Services.Units[0].Presence = profile.ServiceAbsent
	absent, _ := (&Provider{}).SyncDesired(d)
	if absent[0].State.Kind != syncengine.StateAbsent {
		t.Fatal(absent)
	}
	d.Manifest.Capture.Services = false
	empty, err := (&Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured category became managed")
	}
}

func TestSyncDesiredServicesWorkingStateIsIntent(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Services = true
	d.Services.Units = []profile.ServiceUnit{{Name: "a.service", Presence: profile.ServicePresent, ActivationPreference: profile.ServiceActivationRestoreWorkingState}}
	a, _ := (&Provider{}).SyncDesired(d)
	d.Services.Units[0].ObservedActive = true
	b, _ := (&Provider{}).SyncDesired(d)
	if a[0].State.Equal(b[0].State) {
		t.Fatal("working-state activation intent lost")
	}
}
