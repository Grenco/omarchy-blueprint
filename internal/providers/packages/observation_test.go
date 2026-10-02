package packages

import (
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestPackagesObservationPreservesAndCopiesLiveFacts(t *testing.T) {
	fixture := func() profile.Packages {
		return profile.Packages{Installed: []string{"dependency"}, MiseInstalled: map[string]bool{"node": true}, SemanticInstalled: map[string]bool{"tailscale": true}, SemanticRemoved: map[string]bool{"other": true}, Mise: profile.MiseTools{"node": {"version": []any{"24"}}}, OriginUnavailable: true, MissingSyncDatabases: []string{"core"}, UnclassifiedExplicit: []string{"unknown"}}
	}
	original := fixture()
	o := Observation{packages: original}
	copy := o.Clone().Packages()
	if !reflect.DeepEqual(copy, fixture()) {
		t.Fatal("lost live facts")
	}
	copy.Installed[0] = "changed"
	copy.MiseInstalled["node"] = false
	copy.SemanticInstalled["tailscale"] = false
	copy.SemanticRemoved["other"] = false
	copy.Mise["node"]["version"].([]any)[0] = "changed"
	copy.MissingSyncDatabases[0] = "changed"
	copy.UnclassifiedExplicit[0] = "changed"
	if !reflect.DeepEqual(o.Packages(), fixture()) || !reflect.DeepEqual(original, fixture()) {
		t.Fatal("facts shared mutable state")
	}
}
