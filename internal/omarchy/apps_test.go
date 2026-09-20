package omarchy

import (
	"reflect"
	"testing"
)

func TestSemanticRecipeSelectsTailscaleServiceFlow(t *testing.T) {
	got, ok := SemanticRecipe("tailscale")
	if !ok {
		t.Fatal("tailscale has no semantic recipe")
	}
	want := AppRecipe{
		ID:            "tailscale",
		Install:       []string{"omarchy-install-service-tailscale"},
		Remove:        []string{"omarchy-remove-service-tailscale"},
		Interactive:   true,
		InstallNotice: "Tailscale setup requires interactive device authentication; credentials are not stored by Blueprint.",
		RemoveNotice:  "Tailscale removal uses Omarchy's interactive service teardown.",
		Verify: [][]string{
			{"systemctl", "is-enabled", "tailscaled"},
			{"tailscale", "status"},
			{"systemctl", "--user", "is-enabled", "omarchy-tailscale-receive.service"},
			{"sh", "-c", `omarchy plugin list --json | jq -e '.[] | select(.id == "omarchy.tailscale" and .enabled)' >/dev/null`},
			{"sh", "-c", `test -f "$HOME/.local/share/applications/Tailscale.desktop"`},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recipe = %#v, want %#v", got, want)
	}
}

func TestSemanticRecipeLeavesOrdinaryPackageOnFallbackPath(t *testing.T) {
	if recipe, ok := SemanticRecipe("firefox"); ok {
		t.Fatalf("firefox unexpectedly selected semantic recipe %#v", recipe)
	}
}
