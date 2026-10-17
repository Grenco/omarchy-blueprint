package hooks

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// Every descriptor this provider emits is complete and names its own
// category, so Auto Sync never has to guess (capability audit §4).
func TestEffectDescriptorsAreComplete(t *testing.T) {
	for _, e := range []*model.Effect{hookWriteEffect(true), hookWriteEffect(false), hookDeleteEffect()} {
		if !e.Complete() || !strings.HasPrefix(e.Family, "hooks.") {
			t.Fatalf("incomplete descriptor %+v", *e)
		}
	}
}
