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
		ID:          "tailscale",
		Install:     []string{"omarchy-install-service-tailscale"},
		Remove:      []string{"omarchy-remove-service-tailscale"},
		Interactive: true,
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
