package resources

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"reflect"
	"testing"
)

func TestSyncDesiredResourcesNormalization(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Resources = true
	d.Resources = profile.Resources{Items: []profile.Resource{{ID: "a", Path: "~/a", Strategy: "copy", Kind: "directory", Hash: "a", Untracked: []profile.GitUntrackedFile{{Path: "z", Hash: "z"}, {Path: "a", Hash: "a"}}}}, Links: []profile.ResourceLink{{Origin: "inbound", Source: ".config/demo", TargetResource: "a", Target: "demo"}}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, x := range got {
		keys = append(keys, x.Key.Target)
		if x.Key.Category != "resources" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	if !reflect.DeepEqual(keys, []string{"resource:a"}) {
		t.Fatal(keys)
	}
	d.Resources.Items[0].Dirty = true
	d.Resources.Items[0].Untracked[0], d.Resources.Items[0].Untracked[1] = d.Resources.Items[0].Untracked[1], d.Resources.Items[0].Untracked[0]
	same, err := (Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("volatile/order changed semantics")
	}
	d.Resources.Items[0].Hash = "new"
	changed, err := (Provider{}).SyncDesired(d)
	if err != nil || reflect.DeepEqual(got, changed) {
		t.Fatal("meaningful change invisible")
	}
	d.Manifest.Capture.Resources = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured managed intent")
	}
}
