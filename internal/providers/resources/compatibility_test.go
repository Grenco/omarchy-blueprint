package resources

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

func TestRestoreCompatibilityPortableResourceStrategies(t *testing.T) {
	revision, hash := strings.Repeat("a", 40), strings.Repeat("b", 64)
	cases := []struct {
		name string
		item profile.Resource
		ops  []model.Operation
	}{
		{"copy", profile.Resource{ID: "helper", Path: "~/bin/helper", Kind: "file", Strategy: "copy", Hash: hash, Mode: "0755"},
			[]model.Operation{{Provider: "resources", Resource: "resource:helper", Action: "file", File: &model.FileWrite{SourceHash: hash, ExpectedMissing: true, RejectSymlinkParents: true}}}},
		{"git", profile.Resource{ID: "repo", Path: "~/Projects/repo", Kind: "directory", Strategy: "git", Remote: "https://example.invalid/repo", Revision: revision},
			[]model.Operation{{Provider: "resources", Resource: "resource:repo", Action: "git clone", Command: []string{"git", "clone", "--no-checkout", "https://example.invalid/repo", "/home/user/Projects/repo"}}, {Provider: "resources", Resource: "resource:repo", Action: "git checkout", Command: []string{"git", "-C", "/home/user/Projects/repo", "checkout", "--detach", revision}}}},
		{"git+diff", profile.Resource{ID: "dirty", Path: "~/Projects/dirty", Kind: "directory", Strategy: "git+diff", Remote: "https://example.invalid/dirty", Revision: revision, IndexPatchHash: hash, Untracked: []profile.GitUntrackedFile{{Path: "notes.txt", Hash: hash, Mode: "0600"}}},
			[]model.Operation{{Provider: "resources", Resource: "resource:dirty", Action: "git clone", Command: []string{"git", "clone", "--no-checkout", "https://example.invalid/dirty", "/home/user/Projects/dirty"}}, {Provider: "resources", Resource: "resource:dirty", Action: "git checkout", Command: []string{"git", "-C", "/home/user/Projects/dirty", "checkout", "--detach", revision}}, {Provider: "resources", Resource: "resource:dirty", Action: "apply staged Git state", GitPatch: &model.GitPatchApply{SourceHash: hash, ToIndex: true}}, {Provider: "resources", Resource: "resource:dirty", Action: "restore untracked Git file", File: &model.FileWrite{SourceHash: hash, ExpectedMissing: true, RejectSymlinkParents: true}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RestoreCompatibility([]profile.Resource{tc.item}, map[string]bool{"resource:" + tc.item.ID: true}, model.RestorePlan{Operations: tc.ops, OmarchyFrom: "4.0", OmarchyTo: "5.0"})
			if err != nil {
				t.Fatal(err)
			}
			if got.State != model.CompatibilitySupported || got.Authority != model.CompatibilityUnchanged || len(got.Findings) != 0 || len(got.Evidence) == 0 || got.Evidence[0].Kind != "portable-resource-representation" {
				t.Fatalf("portable %s = %+v", tc.name, got)
			}
			for _, evidence := range got.Evidence {
				if strings.Contains(evidence.Summary, "example.invalid") || strings.Contains(evidence.Summary, "/home/") {
					t.Fatalf("evidence leaked repository/path details: %+v", evidence)
				}
			}
		})
	}
}

func TestRestoreCompatibilityRespectsPolicySkipAndUnknownStrategy(t *testing.T) {
	item := profile.Resource{ID: "unknown", Path: "~/data", Kind: "directory", Strategy: "reference"}
	skipped, err := RestoreCompatibility([]profile.Resource{item}, map[string]bool{"resource:unknown": false}, model.RestorePlan{})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Applies || skipped.State != "" || len(skipped.Findings) != 0 || skipped.Authority != model.CompatibilityUnchanged {
		t.Fatalf("Restore Skip still contributed compatibility: %+v", skipped)
	}
	applying, err := RestoreCompatibility([]profile.Resource{item}, map[string]bool{"resource:unknown": true}, model.RestorePlan{})
	if err != nil {
		t.Fatal(err)
	}
	if applying.State != model.CompatibilityIncompatible || applying.Authority != model.CompatibilityBlocked {
		t.Fatalf("unknown strategy was treated as portable: %+v", applying)
	}
}

func TestRestoreCompatibilityRequiresSafePlannedEffect(t *testing.T) {
	item := profile.Resource{ID: "helper", Path: "~/helper", Kind: "file", Strategy: "copy", Hash: "hash", Mode: "0755"}
	for _, plan := range []model.RestorePlan{
		{Skipped: []model.Skipped{{Provider: "resources", Resource: "resource:helper", Reason: "existing resource differs; overwrite disabled"}}},
		{Operations: []model.Operation{{Provider: "resources", Resource: "resource:helper", Action: "delete", Delete: &model.FileDelete{Destination: "/home/user/helper"}}}},
		{Operations: []model.Operation{{Provider: "resources", Resource: "resource:helper", Action: "file", File: &model.FileWrite{SourceHash: "hash", ReplaceExisting: true}}}},
	} {
		got, err := RestoreCompatibility([]profile.Resource{item}, map[string]bool{"resource:helper": true}, plan)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == model.CompatibilitySupported || len(got.Findings) == 0 {
			t.Fatalf("untrusted or skipped effect became Supported: %+v for %+v", got, plan)
		}
	}
}

func TestRestoreCompatibilityDoesNotInspectFilesystem(t *testing.T) {
	item := profile.Resource{ID: "helper", Path: "~/does-not-exist", Kind: "file", Strategy: "copy", Hash: "hash", Mode: "0755"}
	plan := model.RestorePlan{Operations: []model.Operation{{Provider: "resources", Resource: "resource:helper", Action: "file", File: &model.FileWrite{Source: "/not-a-real-profile/snapshot", SourceHash: "hash", ExpectedMissing: true, RejectSymlinkParents: true}}}}
	got, err := RestoreCompatibility([]profile.Resource{item}, nil, plan)
	if err != nil || got.State != model.CompatibilitySupported {
		t.Fatalf("pure plan-backed assessment = %+v, %v", got, err)
	}
}

func TestRestoreCompatibilityRejectsUnexpectedOperationAlongsideSafeFile(t *testing.T) {
	item := profile.Resource{ID: "helper", Path: "~/helper", Kind: "file", Strategy: "copy", Hash: "hash", Mode: "0755"}
	plan := model.RestorePlan{Operations: []model.Operation{
		{Provider: "resources", Resource: "resource:helper", Action: "file", File: &model.FileWrite{SourceHash: "hash", ExpectedMissing: true, RejectSymlinkParents: true}},
		{Provider: "resources", Resource: "resource:helper", Action: "run privileged helper", Command: []string{"sudo", "unexpected"}},
	}}
	got, err := RestoreCompatibility([]profile.Resource{item}, nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got.State == model.CompatibilitySupported || got.Authority != model.CompatibilityBlocked {
		t.Fatalf("unexpected operation acquired portable authority: %+v", got)
	}
}
