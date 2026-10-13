package shell

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"reflect"
	"testing"
)

func TestSyncDesiredShellSemantics(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Shell = true
	d.Shell = profile.Shell{Version: 1, Hash: "desired", BaselineHash: "baseline"}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %v", got, err)
	}
	d.Shell.BaselineHash = "changed-baseline"
	changed, _ := (Provider{}).SyncDesired(d)
	if changed[0].State.Equal(got[0].State) {
		t.Fatal("shell baseline intent ignored")
	}
	got = changed
	for _, x := range got {
		if x.Key.Category != "shell" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	same, err := (Provider{}).SyncDesired(profile.CloneData(d))
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("unstable or mutating projection")
	}
	d.Manifest.Capture.Shell = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured intent")
	}
}

func TestSyncDesiredShellEmptyIsUnmanaged(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Shell = true
	got, err := (Provider{}).SyncDesired(d)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
