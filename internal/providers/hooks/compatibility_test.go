package hooks

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilityAdditiveLifecycleUnknownButBounded(t *testing.T) {
	item := profile.Hook{Path: "post-update.d/blueprint-ra", Hash: strings.Repeat("a", 64), Mode: "0755"}
	current := State{Items: []DetectedHook{{Path: item.Path, Hash: item.Hash, Mode: item.Mode, Raw: []byte("token=must-not-leak")}}}
	got, err := RestoreCompatibility(profile.Hooks{Items: []profile.Hook{item}}, current, map[string]bool{item.Path: true}, false)
	if err != nil || got.State != model.CompatibilityUnknown || got.Authority != model.CompatibilityUnchanged || len(got.Evidence) == 0 || len(got.Findings) != 1 || got.Findings[0].Code != "hooks.lifecycle.unestablished" {
		t.Fatalf("safe hook lifecycle unknown = %+v err=%v", got, err)
	}
	for _, e := range got.Evidence {
		if strings.Contains(e.Summary, "token=") || strings.Contains(e.Summary, "/tmp/") {
			t.Fatalf("hook bytes/path leaked: %+v", e)
		}
	}
}

func TestRestoreCompatibilityExactDeletionReducesUnprovenAuthority(t *testing.T) {
	item := profile.Hook{Path: "post-update.d/blueprint-ra", Hash: strings.Repeat("a", 64), Mode: "0755"}
	saved := profile.Hooks{Absent: []profile.Hook{item}}
	current := State{Items: []DetectedHook{{Path: item.Path, Hash: item.Hash, Mode: item.Mode}}}
	additive, err := RestoreCompatibility(saved, current, map[string]bool{item.Path: true}, false)
	if err != nil || additive.Applies || len(additive.Findings) != 0 {
		t.Fatalf("Additive inspected Exact-only tombstone: %+v err=%v", additive, err)
	}
	exact, err := RestoreCompatibility(saved, current, map[string]bool{item.Path: true}, true)
	if err != nil || exact.State != model.CompatibilityUnknown || exact.Authority != model.CompatibilityReduced || len(exact.Findings) != 1 || exact.Findings[0].Code != "hooks.lifecycle.unestablished" {
		t.Fatalf("Exact unproven lifecycle gained deletion authority: %+v err=%v", exact, err)
	}
}

func TestRestoreCompatibilitySkipAndCorruptMetadata(t *testing.T) {
	item := profile.Hook{Path: "post-boot", Hash: strings.Repeat("b", 64), Mode: "0755"}
	skipped, err := RestoreCompatibility(profile.Hooks{Items: []profile.Hook{item}}, State{}, map[string]bool{item.Path: false}, false)
	if err != nil || skipped.Applies || len(skipped.Findings) != 0 {
		t.Fatalf("skipped hook blocked: %+v err=%v", skipped, err)
	}
	item.Hash = "not-a-hash"
	if _, err := RestoreCompatibility(profile.Hooks{Items: []profile.Hook{item}}, State{}, map[string]bool{item.Path: true}, false); err == nil {
		t.Fatal("corrupt trusted hook metadata must remain an error")
	}
}
