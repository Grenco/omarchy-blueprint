package themes

import (
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	"reflect"
	"testing"
)

func TestSyncDesiredThemesNormalization(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Themes = true
	d.Themes = profile.Themes{Current: "builtin", Items: []profile.Theme{{ID: "custom", Type: "local", Hash: "a", Enabled: true}}, Absent: []profile.Theme{{ID: "gone", Type: "local", Hash: "b"}}}
	got, err := (Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, x := range got {
		keys = append(keys, x.Key.Target)
		if x.Key.Category != "themes" || x.State.Fingerprint == "" {
			t.Fatal(x)
		}
	}
	if !reflect.DeepEqual(keys, []string{"active", "theme:custom", "theme:gone"}) {
		t.Fatal(keys)
	}
	d.Themes.Items[0].Enabled = false
	same, err := (Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(got, same) {
		t.Fatal("volatile/order changed semantics")
	}
	d.Themes.Items[0].Hash = "new"
	changed, err := (Provider{}).SyncDesired(d)
	if err != nil || reflect.DeepEqual(got, changed) {
		t.Fatal("meaningful change invisible")
	}
	d.Manifest.Capture.Themes = false
	empty, err := (Provider{}).SyncDesired(d)
	if err != nil || len(empty) != 0 {
		t.Fatal("uncaptured managed intent")
	}
}

func TestSyncGitThemeUsesProviderEquivalence(t *testing.T) {
	d := profile.Data{}
	d.Manifest.Capture.Themes = true
	d.Themes.Items = []profile.Theme{{ID: "demo", Type: "git", URL: "https://example.test/theme", Revision: "abc", Hash: "first"}}
	first, err := (Provider{}).SyncDesired(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Themes.Items[0].Hash = "second"
	second, err := (Provider{}).SyncDesired(d)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("non-semantic Git snapshot hash changed intent")
	}
}
