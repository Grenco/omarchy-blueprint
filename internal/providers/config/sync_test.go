package config

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"reflect"
	"testing"
)

func TestSyncDesiredConfigSemantics(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Config = true
	d.Config = profile.Configs{Files: []profile.ConfigFile{{Path: ".config/a", Hash: "a"}, {Path: ".config/z", Hash: "z"}}, Deletes: []profile.ConfigDelete{{Path: ".config/gone", BaselineHash: "baseline"}}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %v", got, err)
	}

	d.Config.Files[0], d.Config.Files[1] = d.Config.Files[1], d.Config.Files[0]
	d.Config.Files[0].ID = "legacy-layout-id"
	reordered, _ := (Provider{}).SyncDesired(d)
	if !reflect.DeepEqual(got, reordered) {
		t.Fatal("serialization order/layout ID changed semantics")
	}
	d.Config.Files[1].Hash = "new"
	changed, _ := (Provider{}).SyncDesired(d)
	if changed[0].State.Equal(got[0].State) {
		t.Fatal("config content change invisible")
	}
	got = changed
	for _, x := range got {
		if x.Key.Category != "config" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	same, err := (Provider{}).SyncDesired(profile.CloneData(d))
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("unstable or mutating projection")
	}
	d.Manifest.Capture.Config = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured intent")
	}
}

func TestSyncDesiredConfigAbsenceProvenance(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Config = true
	d.Config = profile.Configs{Files: []profile.ConfigFile{{Path: ".config/a", Hash: "a"}, {Path: ".config/z", Hash: "z"}}, Deletes: []profile.ConfigDelete{{Path: ".config/gone", BaselineHash: "baseline"}}}
	got, _ := (Provider{}).SyncDesired(d)
	found := false
	for _, x := range got {
		if x.State.Kind == syncengine.StateAbsent {
			found = true
		}
	}
	if !found {
		t.Fatal("explicit absence collapsed")
	}
}
