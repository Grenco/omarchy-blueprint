package profilegit

import (
	"os"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/gittest"
)

// Git-backed tests must not leave detached automatic maintenance running
// against temporary repositories while they are being removed.
func TestMain(m *testing.M) {
	gittest.DisableAutomaticMaintenance()
	os.Exit(m.Run())
}
