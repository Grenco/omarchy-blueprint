package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/inspection"
	"github.com/Grenco/omarchy-blueprint/internal/machine"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
	configprovider "github.com/Grenco/omarchy-blueprint/internal/providers/config"
	resourcesprovider "github.com/Grenco/omarchy-blueprint/internal/providers/resources"
	"github.com/Grenco/omarchy-blueprint/internal/workflow"
)

func TestInspectPathFixtures(t *testing.T) {
	profileDir, stateHome, home := t.TempDir(), t.TempDir(), t.TempDir()
	configRoot := filepath.Join(home, ".config")
	regular := filepath.Join(home, "notes.txt")
	clean := filepath.Join(home, "clean")
	dirty := filepath.Join(home, "dirty")
	tracked := filepath.Join(home, "mapped-projects")
	configPath := filepath.Join(configRoot, "hypr", "bindings.lua")
	symlink := filepath.Join(home, "linked")
	writeAppFile(t, regular, "notes")
	writeAppFile(t, configPath, "config")
	writeAppFile(t, filepath.Join(tracked, "README"), "tracked")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	initInspectGit(t, clean, false)
	initInspectGit(t, dirty, true)
	d := profile.New("test", time.Now())
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/Projects", Kind: "directory", Strategy: "copy"}}
	d.Machines.Items = []profile.Machine{{Name: "desktop", ResourcePaths: []profile.MachineResourcePath{{Resource: "projects", Path: tracked}}}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	if err := (machine.BindingStore{StateHome: stateHome}).Save(profileDir, "desktop"); err != nil {
		t.Fatal(err)
	}
	deps := inspectDeps(stateHome, home, configRoot)
	deps.BaselineHistory = func() configprovider.BaselineHistory { return fakeBaselineHistory(true) }
	for _, test := range []struct {
		name  string
		path  string
		check func(t *testing.T, value workflow.PathInspection)
	}{
		{"regular", regular, func(t *testing.T, value workflow.PathInspection) {
			if value.Type != "file" || value.SuggestedStrategy != "copy" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
		{"clean git", clean, func(t *testing.T, value workflow.PathInspection) {
			if value.Git == nil || value.SuggestedStrategy != "git" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
		{"dirty git", dirty, func(t *testing.T, value workflow.PathInspection) {
			if value.Git == nil || value.SuggestedStrategy != "git+diff" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
		{"tracked effective", tracked, func(t *testing.T, value workflow.PathInspection) {
			if value.ResourceID != "projects" || value.ResourceEffective != tracked || value.ResourceDefault != "~/Projects" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
		{"config owned", configPath, func(t *testing.T, value workflow.PathInspection) {
			if value.OwnershipProvider != "config" || value.BlockedReason == "" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
		{"symlink", symlink, func(t *testing.T, value workflow.PathInspection) {
			if value.Type != "symlink" || value.SymlinkTarget != regular || value.BlockedReason == "" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := inspectPathValue(t, profileDir, deps, test.path)
			test.check(t, value)
		})
	}
	if value := inspectPathValue(t, profileDir, deps, "/dev/null"); value.Type != "special" || value.BlockedReason == "" {
		t.Fatalf("special inspection = %#v", value)
	}
}

func TestInspectConfigAndResourceJSON(t *testing.T) {
	profileDir, stateHome, home, baseline := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	configRoot := filepath.Join(home, ".config")
	path := filepath.Join(configRoot, "foo", "bar")
	writeAppFile(t, path, "custom")
	writeAppFile(t, filepath.Join(baseline, "foo", "bar"), "default")
	writeAppFile(t, filepath.Join(profileDir, "config", "files", ".config", "foo", "bar"), "saved")
	resourceRoot := filepath.Join(home, "projects")
	plainPath := filepath.Join(home, "notes.txt")
	writeAppFile(t, filepath.Join(resourceRoot, "README"), "resource")
	writeAppFile(t, plainPath, "notes")
	d := profile.New("test", time.Now())
	d.Config.Files = []profile.ConfigFile{{Path: ".config/foo/bar"}}
	d.Resources.Items = []profile.Resource{{ID: "projects", Path: "~/projects", Kind: "directory", Strategy: "copy"}}
	if err := profile.Save(profileDir, d); err != nil {
		t.Fatal(err)
	}
	deps := inspectDeps(stateHome, home, configRoot)
	deps.ConfigDirs = func() (string, string, error) { return baseline, configRoot, nil }
	deps.BaselineHistory = func() configprovider.BaselineHistory { return fakeBaselineHistory(true) }
	for _, test := range []struct {
		args  []string
		check func(t *testing.T, data json.RawMessage)
	}{
		{[]string{"inspect", "path", plainPath}, func(t *testing.T, data json.RawMessage) {
			var value workflow.PathInspection
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if value.Type != "file" || value.SuggestedStrategy != "copy" {
				t.Fatalf("inspection = %#v", value)
			}
		}},
		{[]string{"inspect", "config", ".config/foo/bar"}, func(t *testing.T, data json.RawMessage) {
			var value workflow.ConfigInspection
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if value.Candidate.Classification != configprovider.ConfigHistoricalBaseline || value.Candidate.Reason == "" || !value.Managed || value.BaselineToLive == nil || value.ProfileToLive == nil {
				t.Fatalf("inspection = %#v", value)
			}
			if value.BaselineToLive.Kind != inspection.DiffText || value.BaselineToLive.OldLabel != "baseline/.config/foo/bar" || value.BaselineToLive.NewLabel != "live/.config/foo/bar" || value.ProfileToLive.Kind != inspection.DiffText || value.ProfileToLive.OldLabel != "profile/.config/foo/bar" {
				t.Fatalf("diffs = %#v %#v", value.BaselineToLive, value.ProfileToLive)
			}
		}},
		{[]string{"inspect", "resource", "projects"}, func(t *testing.T, data json.RawMessage) {
			var value workflow.ResourceInspection
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if value.Resource.ID != "projects" || value.DefaultPath != "~/projects" || value.EffectivePath != resourceRoot {
				t.Fatalf("inspection = %#v", value)
			}
		}},
	} {
		var out, stderr bytes.Buffer
		deps.Out, deps.Err = &out, &stderr
		if code := Execute(context.Background(), append([]string{"--profile", profileDir, "--json"}, test.args...), deps); code != 0 {
			t.Fatalf("%v code=%d stderr=%s", test.args, code, stderr.String())
		}
		var envelope struct {
			Data struct {
				Inspection json.RawMessage `json:"inspection"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		test.check(t, envelope.Data.Inspection)
	}
}

func TestConfigDiffSafetyFixtures(t *testing.T) {
	root := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, test := range []struct {
		name       string
		old, new   []byte
		missingOld bool
		kind       inspection.DiffKind
		check      func(*testing.T, *inspection.DiffDocument)
	}{
		{"binary", nil, []byte{'a', 0}, false, inspection.DiffBinary, func(t *testing.T, doc *inspection.DiffDocument) {
			if len(doc.Hunks) != 0 {
				t.Fatalf("binary hunks = %#v", doc.Hunks)
			}
		}},
		{"sensitive", nil, []byte("api_token = tokenvaluewithmorethan16chars\n"), false, inspection.DiffSensitive, func(t *testing.T, doc *inspection.DiffDocument) {
			if len(doc.Hunks) != 0 {
				t.Fatalf("sensitive hunks = %#v", doc.Hunks)
			}
		}},
		{"oversized", nil, []byte(strings.Repeat("x", inspection.MaxPreviewBytes+1)), false, inspection.DiffTooLarge, func(t *testing.T, doc *inspection.DiffDocument) {
			if len(doc.Hunks) != 0 {
				t.Fatalf("oversized hunks = %#v", doc.Hunks)
			}
		}},
		{"missing baseline", nil, []byte("live\n"), true, inspection.DiffText, func(t *testing.T, doc *inspection.DiffDocument) {
			if doc.Hunks[0].Lines[0].Kind != "add" {
				t.Fatalf("missing-side lines = %#v", doc.Hunks[0].Lines)
			}
		}},
		{"control characters", nil, []byte("\x1b]0;unsafe\a\n"), false, inspection.DiffText, func(t *testing.T, doc *inspection.DiffDocument) {
			if strings.ContainsRune(doc.Hunks[0].Lines[0].Text, '\x1b') {
				t.Fatalf("unsafe line = %#v", doc.Hunks[0].Lines[0])
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldPath := filepath.Join(root, test.name+"-old")
			if !test.missingOld {
				oldPath = write(test.name+"-old", test.old)
			}
			doc := configDiff("baseline/.config/"+test.name, oldPath, "live/.config/"+test.name, write(test.name+"-new", test.new))
			if doc.Kind != test.kind || doc.OldLabel != "baseline/.config/"+test.name || doc.NewLabel != "live/.config/"+test.name {
				t.Fatalf("document = %#v", doc)
			}
			test.check(t, doc)
		})
	}
}

func inspectDeps(stateHome, home, configRoot string) Dependencies {
	return Dependencies{
		Runner: command.SystemRunner{}, StateHome: func() (string, error) { return stateHome, nil }, HomeDir: func() (string, error) { return home, nil },
		ConfigDirs: func() (string, string, error) { return "", configRoot, nil }, ShellPaths: func() (string, string, error) { return "", "", nil },
		HooksDir: func() (string, error) { return "", nil }, ThemeDirs: func() (string, string, error) { return "", "", nil }, PluginDir: func() (string, error) { return "", nil },
		ResourceLinkRoots: resourcesprovider.DefaultLinkSearchRoots,
	}
}

func inspectPathValue(t *testing.T, profileDir string, deps Dependencies, path string) workflow.PathInspection {
	t.Helper()
	session, err := openWorkflow(deps, &options{profileDir: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	value, err := session.InspectPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func initInspectGit(t *testing.T, root string, dirty bool) {
	t.Helper()
	for _, args := range [][]string{{"init", root}, {"-C", root, "config", "user.email", "test@example.com"}, {"-C", root, "config", "user.name", "Test"}, {"-C", root, "remote", "add", "origin", "https://github.com/example/repo.git"}} {
		if _, err := (command.SystemRunner{}).Run(context.Background(), "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	writeAppFile(t, filepath.Join(root, "README"), "first")
	for _, args := range [][]string{{"-C", root, "add", "README"}, {"-C", root, "commit", "-m", "initial"}} {
		if _, err := (command.SystemRunner{}).Run(context.Background(), "git", args...); err != nil {
			t.Fatal(err)
		}
	}
	if dirty {
		writeAppFile(t, filepath.Join(root, "README"), "changed")
	}
}
