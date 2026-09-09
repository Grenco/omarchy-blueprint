package resources

import (
	"reflect"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestDiffAndVerifyResourcesAndLinks(t *testing.T) {
	saved := profile.Resources{Items: []profile.Resource{{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "old", Mode: "0755"}}, Links: []profile.ResourceLink{{Source: "~/.config/tool", TargetResource: "scripts", Target: "tool", Origin: "inbound"}}}
	current := profile.Resources{Items: []profile.Resource{{ID: "scripts", Path: "~/Scripts", Kind: "directory", Strategy: "copy", Hash: "new", Mode: "0755"}}, Links: []profile.ResourceLink{{Source: "~/.config/tool", TargetResource: "scripts", Target: "changed", Origin: "inbound"}, {Source: "~/.config/extra", TargetResource: "scripts", Target: "extra", Origin: "inbound"}}}
	changes := Diff(saved, current)
	if len(changes) != 3 || changes[0].Kind != "link" || changes[2].Kind != "resource" || changes[2].Type != model.ChangeModify {
		t.Fatalf("changes=%#v", changes)
	}
	result := Verify(saved, current)
	if result.OK || !reflect.DeepEqual(result.Missing, []string{"link:~/.config/tool", "resource:scripts"}) {
		t.Fatalf("result=%#v", result)
	}
	current.Links[0].Target = "tool"
	current.Items[0].Hash = "old"
	if !Verify(saved, current).OK {
		t.Fatal("extra inbound link failed verification")
	}
}

func TestDiffAndVerifyDetachedGitResourceAreClean(t *testing.T) {
	saved := profile.Resources{Items: []profile.Resource{{ID: "dotfiles", Path: "~/dotfiles", Kind: "directory", Strategy: "git", Remote: "github.com/Grenco/dotfiles", Branch: "main", Revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	current := saved
	current.Items[0].Branch = ""
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes=%#v", changes)
	}
	if result := Verify(saved, current); !result.OK {
		t.Fatalf("result=%#v", result)
	}
}
