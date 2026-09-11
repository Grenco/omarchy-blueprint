package machine

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestValidateName(t *testing.T) {
	for _, test := range []struct {
		name  string
		valid bool
	}{
		{"framework", true},
		{"desktop-2", true},
		{"work.vm_1", true},
		{"", false},
		{".", false},
		{"..", false},
		{"work laptop", false},
		{"../../x", false},
	} {
		if got := ValidateName(test.name) == nil; got != test.valid {
			t.Errorf("ValidateName(%q) valid = %t, want %t", test.name, got, test.valid)
		}
	}
}

func TestResolveEffectiveRoots(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	profileDir := filepath.Join(t.TempDir(), "profile")
	state := filepath.Join(t.TempDir(), "omarchy-blueprint")
	resources := profile.Resources{Items: []profile.Resource{
		{ID: "projects", Path: "~/Projects"},
		{ID: "other", Path: "~/Other"},
	}}
	t.Run("valid and dormant", func(t *testing.T) {
		roots, dormant, err := ResolveEffectiveRoots(home, profileDir, state, resources, &profile.Machine{ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: "/mnt/projects"}, {Resource: "removed", Path: "/mnt/removed"}}})
		if err != nil {
			t.Fatal(err)
		}
		if want := map[string]string{"projects": "/mnt/projects", "other": filepath.Join(home, "Other")}; !reflect.DeepEqual(roots, want) {
			t.Fatalf("roots = %#v, want %#v", roots, want)
		}
		if want := []string{"removed"}; !reflect.DeepEqual(dormant, want) {
			t.Fatalf("dormant = %#v, want %#v", dormant, want)
		}
	})
	for _, test := range []struct {
		name    string
		machine profile.Machine
		want    string
	}{
		{"resource overlap", profile.Machine{ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: "~/Code"}, {Resource: "other", Path: "~/Code/other"}}}, "overlaps resource"},
		{"profile overlap", profile.Machine{ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: filepath.Join(profileDir, "nested")}}}, "profile directory"},
		{"state overlap", profile.Machine{ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: filepath.Join(state, "nested")}}}, "Blueprint state"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := ResolveEffectiveRoots(home, profileDir, state, resources, &test.machine)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNormalizeAndExpandMappingPath(t *testing.T) {
	const home = "/home/test"
	for _, test := range []struct {
		raw      string
		stored   string
		expanded string
	}{
		{"~/Code/site", "~/Code/site", "/home/test/Code/site"},
		{"/home/test/Code/site", "~/Code/site", "/home/test/Code/site"},
		{"/mnt/fast/site", "/mnt/fast/site", "/mnt/fast/site"},
		{"/home/test/../test/Code", "~/Code", "/home/test/Code"},
	} {
		got, err := NormalizeMappingPath(home, test.raw)
		if err != nil || got != test.stored {
			t.Errorf("NormalizeMappingPath(%q) = %q, %v; want %q, nil", test.raw, got, err, test.stored)
		}
		expanded, err := ExpandMappingPath(home, got)
		if err != nil || expanded != test.expanded {
			t.Errorf("ExpandMappingPath(%q) = %q, %v; want %q, nil", got, expanded, err, test.expanded)
		}
	}
	for _, raw := range []string{"~", "/", "/home/test", "Code/site", "../site", "~other/site", "$HOME/site", "${HOME}/site", "~/../site"} {
		if _, err := NormalizeMappingPath(home, raw); err == nil {
			t.Errorf("NormalizeMappingPath accepted %q", raw)
		}
		if _, err := ExpandMappingPath(home, raw); err == nil {
			t.Errorf("ExpandMappingPath accepted %q", raw)
		}
	}
}
