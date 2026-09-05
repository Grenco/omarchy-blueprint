package hooks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func hook(path, hash, mode string) profile.Hook {
	return profile.Hook{Path: path, Hash: hash, Mode: mode}
}

func TestDiffCapturesSemanticChanges(t *testing.T) {
	previous := profile.Hooks{Items: []profile.Hook{hook("post-boot", strings.Repeat("a", 64), "0644")}}
	next := profile.Hooks{Items: []profile.Hook{hook("post-boot", strings.Repeat("b", 64), "0755"), hook("post-update", strings.Repeat("c", 64), "0644")}}
	changes := DiffCaptures(previous, next)
	if len(changes) != 2 || changes[0].Summary != "~ hook post-boot content and mode changed (0644 → 0755)" || changes[1].Summary != "+ hook post-update" {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestDiffAndVerifyRespectModesAndExtras(t *testing.T) {
	saved := profile.Hooks{Items: []profile.Hook{hook("post-boot", strings.Repeat("a", 64), "0644")}}
	current := State{Items: []DetectedHook{{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0755"}, {Path: "extra", Hash: strings.Repeat("b", 64), Mode: "0644"}}}
	changes := Diff(saved, current)
	if !reflect.DeepEqual([]model.ChangeType{changes[0].Type, changes[1].Type}, []model.ChangeType{model.ChangeAdd, model.ChangeModify}) || changes[1].Summary != "~ hook post-boot mode 0644 → 0755" {
		t.Fatalf("changes = %#v", changes)
	}
	result := Verify(saved, current)
	if result.OK || !reflect.DeepEqual(result.Missing, []string{"hook:post-boot"}) {
		t.Fatalf("verification = %#v", result)
	}
	if !Verify(saved, State{Items: []DetectedHook{{Path: "post-boot", Hash: strings.Repeat("a", 64), Mode: "0644"}, {Path: "extra", Hash: "x", Mode: "0644"}}}).OK {
		t.Fatal("extra hooks must not fail verification")
	}
}

func TestDiffWarnsForUnmanagedSymlink(t *testing.T) {
	changes := Diff(profile.Hooks{}, State{Unmanaged: []UnmanagedHook{{Path: "theme-set", Target: "/external/hook"}}})
	if len(changes) != 1 || changes[0].Type != model.ChangeWarn || !strings.Contains(changes[0].Summary, "left unmanaged") {
		t.Fatalf("changes=%#v", changes)
	}
}
