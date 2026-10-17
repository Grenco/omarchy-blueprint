package plugins

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// Every descriptor this provider emits is complete and names its own
// category, so Auto Sync never has to guess (capability audit §4).
func TestEffectDescriptorsAreComplete(t *testing.T) {
	for _, e := range []*model.Effect{effectInstallGit, effectPinGit, effectValidate, effectCopyLocal, effectRescan, effectEnable, effectDisable, effectRemove} {
		if !e.Complete() || !strings.HasPrefix(e.Family, "plugins.") {
			t.Fatalf("incomplete descriptor %+v", *e)
		}
	}
}
