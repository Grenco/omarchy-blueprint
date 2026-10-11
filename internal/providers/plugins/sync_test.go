package plugins

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"reflect"
	"testing"
)

func TestSyncDesiredPluginsNormalization(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Plugins = true
	d.Manifest.Capture.Shell = true
	d.Plugins = profile.Plugins{Items: []profile.Plugin{{ID: "custom", Source: "copy", Hash: "a", Enabled: true}, {ID: "builtin", Source: "builtin"}}, Absent: []profile.Plugin{{ID: "gone", Source: "copy", Hash: "b"}}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, x := range got {
		keys = append(keys, x.Key.Target)
		if x.Key.Category != "plugins" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	if !reflect.DeepEqual(keys, []string{"plugin:builtin", "plugin:custom", "plugin:gone"}) {
		t.Fatal(keys)
	}
	d.Plugins.Items[0].Enabled = false
	d.Plugins.Items[0], d.Plugins.Items[1] = d.Plugins.Items[1], d.Plugins.Items[0]
	same, err := (Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("volatile/order changed semantics")
	}
	d.Plugins.Items[1].Hash = "new"
	changed, err := (Provider{}).SyncDesired(d)
	if err != nil || reflect.DeepEqual(got, changed) {
		t.Fatal("meaningful change invisible")
	}
	d.Manifest.Capture.Plugins = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured managed intent")
	}
}
