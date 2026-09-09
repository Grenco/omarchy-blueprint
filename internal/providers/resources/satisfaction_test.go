package resources

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestDetachedRestoredGitResourceSatisfiesSavedBranch(t *testing.T) {
	revision := strings.Repeat("a", 40)
	saved := profile.Resource{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/Grenco/dotfiles", Branch: "main", Revision: revision}
	current := saved
	current.Branch = ""
	if !resourceSatisfied(saved, current) {
		t.Fatal("detached captured revision should satisfy Git resource")
	}
}
