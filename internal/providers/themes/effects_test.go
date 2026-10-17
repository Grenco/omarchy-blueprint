package themes

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// Every descriptor this provider emits is complete and names its own
// category, so Auto Sync never has to guess (capability audit §4).
func TestEffectDescriptorsAreComplete(t *testing.T) {
	for _, e := range []*model.Effect{effectCopyLocal, effectCopyOverlay, effectInstallGit, effectPinGit, effectActivate, effectRemove} {
		if !e.Complete() || !strings.HasPrefix(e.Family, "themes.") {
			t.Fatalf("incomplete descriptor %+v", *e)
		}
	}
}
