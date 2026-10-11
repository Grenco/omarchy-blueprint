package packages

import (
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"github.com/Grenco/omarchy-blueprint/internal/syncengine"
)

// Catches collapsing tombstones into unmanaged, ordering-sensitive hashes,
// and accidentally fingerprinting detection-only fields.
func TestSyncDesiredPackagesSemanticIdentity(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Packages = true
	d.Packages = profile.Packages{Official: []string{"z", "a"}, Mise: profile.MiseTools{"go": {"version": "1.26"}}, Absent: []profile.PackageAbsence{{Ref: "aur:gone"}}, Preinstalls: profile.Preinstalls{Managed: true, Items: map[string]bool{"firefox": false}}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	states := map[string]syncengine.State{}
	for _, x := range got {
		keys = append(keys, x.Key.Target)
		states[x.Key.Target] = x.State
		if x.Key.Category != "packages" {
			t.Fatal(x.Key)
		}
	}
	if !reflect.DeepEqual(keys, []string{"aur:gone", "mise:go", "official:a", "official:z", "preinstall:firefox", "preinstalls"}) {
		t.Fatal(keys)
	}
	if states["aur:gone"].Kind != syncengine.StateAbsent || states["preinstall:firefox"].Kind != syncengine.StateAbsent {
		t.Fatal(states)
	}
	d.Packages.Official = []string{"a", "z"}
	d.Packages.Installed = []string{"volatile"}
	d.Packages.MiseInstalled = map[string]bool{"go": true}
	same, err := (Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatalf("ordering/scratch changed intent: %v", err)
	}
	d.Packages.Mise["go"]["version"] = "1.27"
	changed, _ := (Provider{}).SyncDesired(d)
	if changed[1].State.Equal(states["mise:go"]) {
		t.Fatal("meaningful tool change invisible")
	}
	d.Manifest.Capture.Packages = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured category became managed")
	}
}

func TestSyncDesiredPackagesExcludedIsUnmanaged(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Packages = true
	d.Packages = profile.Packages{Official: []string{"a"}, Excluded: []string{"official:a"}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil || len(got) != 0 {
		t.Fatalf("excluded desired intent: %v %v", got, err)
	}
}
