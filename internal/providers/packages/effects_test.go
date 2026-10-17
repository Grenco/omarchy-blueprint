package packages

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/omarchy"
)

// Every descriptor this provider emits is complete and names its own
// category, so Auto Sync never has to guess (capability audit §4).
func TestEffectDescriptorsAreComplete(t *testing.T) {
	for _, e := range []*model.Effect{installEffect("official"), installEffect("aur"), recipeEffect(omarchy.AppRecipe{}), recipeEffect(omarchy.AppRecipe{AwaitsYou: "sign in"}), preinstallEffect("install"), preinstallEffect("remove"), removeEffect("official", false), removeEffect("aur", false), removeEffect("official", true), miseInstallEffect(), miseConfigureEffect(true), miseConfigureEffect(false)} {
		if !e.Complete() || !strings.HasPrefix(e.Family, "packages.") {
			t.Fatalf("incomplete descriptor %+v", *e)
		}
	}
}
