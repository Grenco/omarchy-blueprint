package hooks

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
	"reflect"
	"testing"
)

func TestSyncDesiredHooksSemantics(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Hooks = true
	d.Hooks = profile.Hooks{Items: []profile.Hook{{Path: "post-update.d/a", Hash: "a", Mode: "0755"}, {Path: "post-update.d/z", Hash: "z", Mode: "0755"}}, Absent: []profile.Hook{{Path: "post-update.d/gone", Hash: "old", Mode: "0755"}}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %v", got, err)
	}
	d.Hooks.Items[0], d.Hooks.Items[1] = d.Hooks.Items[1], d.Hooks.Items[0]
	reordered, _ := (Provider{}).SyncDesired(d)
	if !reflect.DeepEqual(got, reordered) {
		t.Fatal("hook order changed intent")
	}
	d.Hooks.Items[1].Mode = "0644"
	changed, _ := (Provider{}).SyncDesired(d)
	if changed[0].State.Equal(got[0].State) {
		t.Fatal("hook executable-mode change invisible")
	}
	got = changed
	for _, x := range got {
		if x.Key.Category != "hooks" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	same, err := (Provider{}).SyncDesired(profile.CloneData(d))
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("unstable or mutating projection")
	}
	d.Manifest.Capture.Hooks = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured intent")
	}
}

func TestSyncDesiredHooksAbsenceProvenance(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Hooks = true
	d.Hooks = profile.Hooks{Items: []profile.Hook{{Path: "post-update.d/a", Hash: "a", Mode: "0755"}, {Path: "post-update.d/z", Hash: "z", Mode: "0755"}}, Absent: []profile.Hook{{Path: "post-update.d/gone", Hash: "old", Mode: "0755"}}}
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
