package profile

import (
	"reflect"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/policy"
)

// mutateCollections touches every nested slice/map/pointer without relying on
// CloneData itself to construct the expected value. Scalar TOML/time values
// are compared exactly, including their concrete types.
func mutateCollections(v reflect.Value) {
	switch v.Kind() {
	case reflect.Interface:
		if !v.IsNil() {
			mutateCollections(v.Elem())
		}
	case reflect.Pointer:
		if !v.IsNil() {
			mutateCollections(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				mutateCollections(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			mutateCollections(v.Index(i))
		}
	case reflect.Map:
		for _, key := range v.MapKeys() {
			mutateCollections(v.MapIndex(key))
			v.SetMapIndex(key, reflect.Value{})
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString("changed")
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	}
}

func packageCloneFixture() Packages {
	return Packages{
		Official: []string{"official"}, AUR: []string{}, Installed: []string{"installed"},
		Mise:          MiseTools{"tool": {"version": []any{"1", "2"}, "strings": []string{"s"}, "nested": map[string]any{"flag": true, "number": int64(7), "float": float64(2.5), "time": time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)}}},
		MiseInstalled: map[string]bool{"tool": true}, SemanticInstalled: map[string]bool{"recipe": true}, SemanticRemoved: map[string]bool{"removed": true},
		Preinstalls:     Preinstalls{Managed: true, RemovedAll: true, Items: map[string]bool{"preinstall": true}},
		Absent:          []PackageAbsence{{Ref: "mise:absent", Mise: MiseTool{"version": []any{"3"}}}},
		MachineSpecific: []string{"hardware"}, Excluded: []string{"excluded"}, OriginUnavailable: true,
		MissingSyncDatabases: []string{"repository"}, UnclassifiedExplicit: []string{"unknown"},
	}
}

func TestClonePackagesPreservesLiveOnlyFacts(t *testing.T) {
	source := packageCloneFixture()
	expected := packageCloneFixture()
	got := ClonePackages(source)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("copy changed facts/types: %#v", got)
	}
	mutateCollections(reflect.ValueOf(&got).Elem())
	if !reflect.DeepEqual(source, expected) {
		t.Fatal("mutating copy changed original")
	}
	for _, source := range []Packages{{}, {Official: []string{}, Mise: MiseTools{}, MiseInstalled: map[string]bool{}, Absent: []PackageAbsence{}}} {
		if got := ClonePackages(source); !reflect.DeepEqual(got, source) {
			t.Fatal("nil/empty distinction lost")
		}
	}
}

func dataCloneFixture() Data {
	return Data{Packages: packageCloneFixture(),
		Themes:    Themes{Items: []Theme{{ID: "theme"}}, Absent: []Theme{{ID: "absent"}}},
		Plugins:   Plugins{Items: []Plugin{{ID: "plugin"}}, Absent: []Plugin{{ID: "absent"}}},
		Resources: Resources{Items: []Resource{{ID: "resource", Untracked: []GitUntrackedFile{{Path: "file"}}}}, Links: []ResourceLink{{Source: "link"}}, IgnoredLinks: []string{"ignored"}},
		Machines:  Machines{Items: []Machine{{Name: "machine", ResourcePaths: []MachineResourcePath{{Resource: "resource", Path: "root"}}, Policy: policy.Rules{Capture: []policy.Rule{{Category: "services"}}, Restore: []policy.Rule{{Category: "packages"}}}}}},
		Config:    Configs{Files: []ConfigFile{{Path: "config"}}, Deletes: []ConfigDelete{{Path: "delete"}}, Included: []string{"include"}, Excluded: []string{"exclude"}},
		Hooks:     Hooks{Items: []Hook{{Path: "hook"}}, Absent: []Hook{{Path: "absent"}}},
		Services:  Services{Units: []ServiceUnit{{Name: "unit.service", Mask: &ServiceMask{Presence: ServicePresent}, DropIns: []ServiceArtifact{{Path: "drop-in"}}, Instances: []ServiceInstance{{Name: "unit@one.service", Mask: &ServiceMask{Presence: ServicePresent}, DropIns: []ServiceArtifact{{Path: "instance-drop-in"}}}}}}},
		Policy:    policy.Rules{Capture: []policy.Rule{{Category: "services"}}, Restore: []policy.Rule{{Category: "packages"}}},
	}
}

func TestCloneDataIsolatesProfilePolicyAndMachine(t *testing.T) {
	source := dataCloneFixture()
	expected := dataCloneFixture()
	got := CloneData(source)
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("copy changed data")
	}
	mutateCollections(reflect.ValueOf(&got).Elem())
	if !reflect.DeepEqual(source, expected) {
		t.Fatal("mutating copy changed source")
	}
	if got := CloneData(Data{}); !reflect.DeepEqual(got, Data{}) {
		t.Fatal("nil collections changed")
	}
}
