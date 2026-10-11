package defaults

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"reflect"
	"testing"
)

func TestSyncDesiredDefaultsNormalization(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Defaults = true
	d.Defaults = profile.Defaults{Terminal: "kitty", Browser: "firefox"}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, x := range got {
		keys = append(keys, x.Key.Target)
		if x.Key.Category != "defaults" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	if !reflect.DeepEqual(keys, []string{"browser", "terminal"}) {
		t.Fatal(keys)
	}

	same, err := (Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("volatile/order changed semantics")
	}
	d.Defaults.Browser = "chromium"
	changed, err := (Provider{}).SyncDesired(d)
	if err != nil || reflect.DeepEqual(got, changed) {
		t.Fatal("meaningful change invisible")
	}
	d.Manifest.Capture.Defaults = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured managed intent")
	}
}
