package packages

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/command"
	"github.com/Grenco/omarchy-blueprint/internal/model"
	"github.com/Grenco/omarchy-blueprint/internal/profile"
)

type queryRunner struct {
	output string
	err    error
}

func (r queryRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	switch key {
	case "sh -c command -v omarchy-remove-preinstalls":
		return "/usr/bin/omarchy-remove-preinstalls\n", nil
	case "cat /usr/bin/omarchy-remove-preinstalls":
		return "omarchy-pkg-drop \\\n  aether\n", nil
	case `sh -c [ -f "$HOME/.local/state/omarchy/preinstalls-removed" ]`, "pacman -Q aether":
		return "", &command.RunError{Name: name, Args: args, ExitCode: 1, Err: errors.New("exit status 1")}
	}
	return r.output, r.err
}

func TestDiffIsSemanticAndStable(t *testing.T) {
	saved := profile.Packages{Official: []string{"git", "old"}, AUR: []string{"aur-old"}}
	current := profile.Packages{Official: []string{"git", "new"}, AUR: []string{"aur-new"}}
	got := Diff(saved, current)
	want := []string{"aur-new", "aur-old", "new", "old"}
	var names []string
	for _, change := range got {
		names = append(names, change.Name)
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %#v", names)
	}
}

func TestPackageNameSatisfiesProfileAcrossRepositorySources(t *testing.T) {
	tests := []struct {
		name    string
		saved   profile.Packages
		current profile.Packages
	}{
		{"AUR package moved to official repository", profile.Packages{AUR: []string{"example"}}, profile.Packages{Official: []string{"example"}}},
		{"official package installed as foreign", profile.Packages{Official: []string{"example"}}, profile.Packages{AUR: []string{"example"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if changes := Diff(tt.saved, tt.current); len(changes) != 0 {
				t.Fatalf("changes = %#v", changes)
			}
			if plan := Plan(tt.saved, tt.current, 1, "4.0.0", "4.1.0"); len(plan.Operations) != 0 {
				t.Fatalf("operations = %#v", plan.Operations)
			}
			if verification := Verify(tt.saved, tt.current); !verification.OK || len(verification.Missing) != 0 {
				t.Fatalf("verification = %#v", verification)
			}
		})
	}
}

func TestInstalledDependencySatisfiesExplicitProfileIntent(t *testing.T) {
	saved := profile.Packages{Official: []string{"neovim", "ninja", "unzip"}}
	current := profile.Packages{Installed: []string{"neovim", "ninja", "unzip"}}
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
	if plan := Plan(saved, current, 1, "4.0.0", "4.0.0"); len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if verification := Verify(saved, current); !verification.OK {
		t.Fatalf("verification = %#v", verification)
	}
}

func TestPlanExplainsAdditionalPackagesWithoutRemovingThem(t *testing.T) {
	saved := profile.Packages{Official: []string{"git"}}
	current := profile.Packages{Official: []string{"git", "linux-headers", "mkinitcpio", "sudo"}, Installed: []string{"git", "linux-headers", "mkinitcpio", "sudo"}}
	plan := Plan(saved, current, 1, "4.0.0", "4.0.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	want := []string{"official:linux-headers", "official:mkinitcpio", "official:sudo"}
	var got []string
	for _, skipped := range plan.Skipped {
		if skipped.Reason != "additional package left installed; removal disabled" {
			t.Fatalf("reason = %q", skipped.Reason)
		}
		got = append(got, skipped.Resource)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped = %#v", got)
	}
}

func TestPlanInstallsNativeBeforeAURAndNeverRemoves(t *testing.T) {
	saved := profile.Packages{Official: []string{"git", "ripgrep", "zoxide"}, AUR: []string{"another-bin", "tool-bin"}}
	current := profile.Packages{Official: []string{"git", "extra"}}
	plan := Plan(saved, current, 1, "4.0.0", "4.1.0")
	if len(plan.Operations) != 3 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if plan.Operations[0].Resource != "official:ripgrep,zoxide" || plan.Operations[1].Resource != "aur:another-bin" || plan.Operations[2].Resource != "aur:tool-bin" {
		t.Fatalf("wrong order: %#v", plan.Operations)
	}
	if !reflect.DeepEqual(plan.Operations[0].Command, []string{"omarchy", "pkg", "add", "ripgrep", "zoxide"}) {
		t.Fatalf("native command = %#v", plan.Operations[0].Command)
	}
	for _, op := range plan.Operations {
		if op.Action != "install" || op.Risk != model.RiskLow || op.Reversible {
			t.Fatalf("unsafe operation: %#v", op)
		}
	}
}

func TestPlanUsesSemanticTailscaleInstallerAndOrdinaryPackageFallback(t *testing.T) {
	saved := profile.Packages{Official: []string{"firefox", "tailscale"}}
	plan := Plan(saved, profile.Packages{}, 13, "4.0.0", "4.1.0")
	if len(plan.Operations) != 2 {
		t.Fatalf("Operations = %#v", plan.Operations)
	}
	var semantic, ordinary *model.Operation
	for i := range plan.Operations {
		op := &plan.Operations[i]
		switch op.Resource {
		case "official:tailscale":
			semantic = op
		case "official:firefox":
			ordinary = op
		}
	}
	if semantic == nil || !reflect.DeepEqual(semantic.Command, []string{"omarchy-install-service-tailscale"}) || !semantic.Interactive || semantic.Notice == "" {
		t.Fatalf("semantic operation = %#v, want interactive Omarchy service install with notice", semantic)
	}
	if ordinary == nil || !reflect.DeepEqual(ordinary.Command, []string{"omarchy", "pkg", "add", "firefox"}) || ordinary.Interactive {
		t.Fatalf("ordinary operation = %#v, want raw package fallback", ordinary)
	}
}

func TestMachineSpecificPackagesAreSkippedEvenFromLegacyProfileLists(t *testing.T) {
	saved := profile.Packages{Official: []string{"git", "nvidia-open", "amd-ucode", "fprintd"}, AUR: []string{"nvidia-580xx-dkms", "libfprint-goodix-521d"}}
	plan := Plan(saved, profile.Packages{Official: []string{"git"}}, 1, "4.0.0", "4.0.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	want := []string{"aur:libfprint-goodix-521d", "aur:nvidia-580xx-dkms", "official:amd-ucode", "official:fprintd", "official:nvidia-open"}
	var got []string
	for _, skipped := range plan.Skipped {
		got = append(got, skipped.Resource)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped = %#v", got)
	}
}

func TestClassifyKeepsHardwareOutOfPortableDiff(t *testing.T) {
	saved := profile.Packages{Official: []string{"git", "nvidia-open", "fprintd"}, AUR: []string{"libfprint-goodix-521d"}}
	current := profile.Packages{Official: []string{"git", "nvidia-settings", "libfprint"}, AUR: []string{"nvidia-580xx-dkms"}}
	if changes := Diff(saved, current); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestLinesNormalizesCommandOutput(t *testing.T) {
	got := lines(" zoxide\ngit\ngit\n\n")
	if !reflect.DeepEqual(got, []string{"git", "zoxide"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestPacmanOutputFixtures(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{"testdata/pacman-native.txt", []string{"base", "git", "zoxide"}},
		{"testdata/pacman-foreign.txt", []string{"visual-studio-code-bin", "yay-bin"}},
	}
	for _, tt := range tests {
		b, err := os.ReadFile(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := lines(string(b)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %#v", tt.path, got)
		}
	}
}

func TestQueryAcceptsPacmanEmptyResultExitCode(t *testing.T) {
	runErr := &command.RunError{Name: "pacman", Args: []string{"-Qqem"}, ExitCode: 1, Err: errors.New("exit status 1")}
	got, err := (Provider{Runner: queryRunner{err: runErr}}).query(context.Background(), "-Qqem")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestQueryPreservesRealPacmanFailures(t *testing.T) {
	tests := []queryRunner{
		{err: &command.RunError{Name: "pacman", ExitCode: 2, Err: errors.New("exit status 2")}},
		{output: "database unavailable", err: &command.RunError{Name: "pacman", ExitCode: 1, Output: "database unavailable", Err: errors.New("exit status 1")}},
		{err: errors.New("command not found")},
	}
	for _, runner := range tests {
		if _, err := (Provider{Runner: runner}).query(context.Background(), "-Qqem"); err == nil {
			t.Fatalf("expected failure for %#v", runner)
		}
	}
}

func TestDetectDiffAndVerifyMisePackages(t *testing.T) {
	config := filepath.Join(t.TempDir(), "mise.toml")
	if err := os.WriteFile(config, []byte("[tools]\nnode = \"24\"\n\"npm:@anthropic-ai/claude-code\" = \"latest\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := (Provider{Runner: queryRunner{}, MiseGlobalConfig: config}).Detect(context.Background())
	if err != nil || current.Mise["node"]["version"] != "24" {
		t.Fatalf("current=%#v err=%v", current, err)
	}
	saved := profile.Packages{Official: []string{"node"}, Mise: profile.MiseTools{"node": {"version": "22"}, "python": {"version": "3.13"}}}
	changes := Diff(saved, current)
	containsMise := false
	for _, change := range changes {
		containsMise = containsMise || change.Kind == "mise"
	}
	if len(changes) == 0 || !containsMise {
		t.Fatalf("changes=%#v", changes)
	}
	if Verify(profile.Packages{Official: []string{"node"}}, current).OK {
		t.Fatal("mise:node must not satisfy official:node")
	}
	if Verify(profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}}}, current).OK != true {
		t.Fatal("equal Mise declaration must verify")
	}
}

func TestPlanAppendsMissingMiseToolsAndPreservesConflicts(t *testing.T) {
	config := filepath.Join(t.TempDir(), "mise", "config.toml")
	existing := []byte("# target comment\n[tools]\nnode = \"22\"\n\n[env]\nKEEP = \"yes\"\n")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, existing, 0o600); err != nil {
		t.Fatal(err)
	}
	saved := profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}, "python": {"version": "3.13", "postinstall": "echo setup"}}}
	current := profile.Packages{Mise: profile.MiseTools{"node": {"version": "22"}}}
	plan, err := (Provider{MiseGlobalConfig: config}).Plan(saved, current, 6, "4.0", "4.1")
	if err != nil || len(plan.Operations) != 2 || len(plan.Skipped) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	configure, install := plan.Operations[0], plan.Operations[1]
	if configure.ID != "packages.mise.configure" || !configure.File.Generated || !configure.File.Backup || configure.Risk != model.RiskMedium || !bytes.HasPrefix(configure.File.Content, existing) {
		t.Fatalf("configure=%#v", configure)
	}
	if !reflect.DeepEqual(install.Command, []string{"mise", "-C", "/", "install", "python"}) || install.Risk != model.RiskHigh || !reflect.DeepEqual(install.DependsOn, []string{"packages.mise.configure"}) {
		t.Fatalf("install=%#v", install)
	}
	if !strings.Contains(plan.Skipped[0].Resource, "mise:node") {
		t.Fatalf("skipped=%#v", plan.Skipped)
	}
}

// TestPlanPreservesUnmanagedPhysicalMiseToolWhenAddingManagedTool is a
// regression for a review finding on PR 3: excluding a mise tool now strips
// it from saved.Mise entirely (Session.SetPackageExcluded), rather than
// keeping it declared alongside a separate Excluded marker, so this
// exercises that an unmanaged tool still physically present in the mise
// config is left untouched while a genuinely new tool is appended.
func TestPlanPreservesUnmanagedPhysicalMiseToolWhenAddingManagedTool(t *testing.T) {
	config := filepath.Join(t.TempDir(), "mise", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("[tools]\nfoo = \"local\"\n")
	if err := os.WriteFile(config, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	saved := profile.Packages{Mise: profile.MiseTools{"bar": {"version": "1"}}}
	current := profile.Packages{Mise: profile.MiseTools{"foo": {"version": "local"}}}
	plan, err := (Provider{MiseGlobalConfig: config}).Plan(saved, current, 6, "4.0", "4.1")
	if err != nil || len(plan.Operations) != 2 || !bytes.HasPrefix(plan.Operations[0].File.Content, existing) || !strings.Contains(string(plan.Operations[0].File.Content), "[tools.bar]") {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestPlanPreinstallRemoveAllThenRestoresIndividuallyDesiredItem(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{
		Managed:    true,
		RemovedAll: true,
		Items:      map[string]bool{"aether": true, "libreoffice-fresh": false},
	}}
	current := profile.Packages{Preinstalls: profile.Preinstalls{
		Managed: false,
		Items:   map[string]bool{"aether": false, "libreoffice-fresh": true},
	}}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("Operations = %#v, want native remove-all then aether reinstall", plan.Operations)
	}
	if !reflect.DeepEqual(plan.Operations[0].Command, []string{"omarchy-remove-preinstalls"}) {
		t.Fatalf("remove-all command = %#v", plan.Operations[0].Command)
	}
	if !reflect.DeepEqual(plan.Operations[1].Command, []string{"omarchy-pkg-add", "aether"}) || !reflect.DeepEqual(plan.Operations[1].DependsOn, []string{"packages.preinstalls.remove"}) {
		t.Fatalf("individual reinstall = %#v", plan.Operations[1])
	}

	after := profile.Packages{Preinstalls: profile.Preinstalls{
		Managed:    true,
		RemovedAll: true,
		Items:      map[string]bool{"aether": true, "libreoffice-fresh": false},
	}}
	if verification := Verify(saved, after); !verification.OK {
		t.Fatalf("verification = %#v", verification)
	}
}

func TestPlanPreinstallRestoreAllThenRemovesIndividuallyAbsentItem(t *testing.T) {
	saved := profile.Packages{Preinstalls: profile.Preinstalls{
		Managed: true,
		Items:   map[string]bool{"aether": true, "libreoffice-fresh": false},
	}}
	current := profile.Packages{Preinstalls: profile.Preinstalls{
		Managed:    true,
		RemovedAll: true,
		Items:      map[string]bool{"aether": false, "libreoffice-fresh": false},
	}}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 || !reflect.DeepEqual(plan.Operations[0].Command, []string{"omarchy-install-preinstalls"}) || !reflect.DeepEqual(plan.Operations[1].Command, []string{"omarchy-pkg-drop", "libreoffice-fresh"}) {
		t.Fatalf("Operations = %#v, want native restore-all then supported individual absence", plan.Operations)
	}
}

func TestExactPackageRemovalPlansOfficialAURAndSemanticCommands(t *testing.T) {
	saved := profile.Packages{Absent: []profile.PackageAbsence{
		{Ref: "official:htop"},
		{Ref: "official:tailscale"},
		{Ref: "aur:tool-bin"},
	}}
	current := profile.Packages{Official: []string{"htop", "tailscale"}, AUR: []string{"tool-bin"}, Installed: []string{"htop", "tailscale", "tool-bin"}}
	plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.1", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"official:htop":      {"omarchy", "pkg", "drop", "htop"},
		"official:tailscale": {"omarchy-remove-service-tailscale"},
		"aur:tool-bin":       {"omarchy", "pkg", "drop", "tool-bin"},
	}
	for _, op := range plan.Operations {
		if command, ok := want[op.Resource]; ok {
			if !reflect.DeepEqual(op.Command, command) || op.Risk != model.RiskHigh {
				t.Fatalf("operation %s = %#v", op.Resource, op)
			}
			delete(want, op.Resource)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing removal operations: %#v; plan=%#v", want, plan)
	}
}

func TestPackageRemovalNeedsExactExplicitSafeTombstone(t *testing.T) {
	saved := profile.Packages{Absent: []profile.PackageAbsence{
		{Ref: "official:dependency"},
		{Ref: "official:nvidia-utils"},
	}}
	current := profile.Packages{
		Official:  []string{"nvidia-utils", "unknown-extra"},
		Installed: []string{"dependency", "nvidia-utils", "unknown-extra"},
	}
	for _, options := range []PlanOptions{{Exact: false}, {Exact: true}} {
		plan, err := (Provider{}).Plan(saved, current, 13, "4.0", "4.1", options)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range plan.Operations {
			if op.Action == "remove" {
				t.Fatalf("unsafe removal under options %+v: %#v", options, op)
			}
		}
	}
}

func TestExactMiseRemovalRequiresMatchingDeclarationAndPreconditionedRewrite(t *testing.T) {
	config := filepath.Join(t.TempDir(), "mise", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("# keep\n[tools]\nnode = '24'\npython = '3.13'\n")
	if err := os.WriteFile(config, existing, 0o600); err != nil {
		t.Fatal(err)
	}
	tombstone := profile.PackageAbsence{Ref: "mise:node", Mise: profile.MiseTool{"version": "24"}}
	saved := profile.Packages{Absent: []profile.PackageAbsence{tombstone}}
	current := profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}, "python": {"version": "3.13"}}}
	plan, err := (Provider{MiseGlobalConfig: config}).Plan(saved, current, 13, "4.0", "4.1", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 2 {
		t.Fatalf("Operations = %#v, want uninstall plus config rewrite", plan.Operations)
	}
	uninstall, configure := plan.Operations[0], plan.Operations[1]
	if !reflect.DeepEqual(uninstall.Command, []string{"mise", "-C", "/", "uninstall", "--all", "node"}) || configure.File == nil || configure.File.ExpectedHash == "" || !configure.File.Backup || !reflect.DeepEqual(configure.DependsOn, []string{uninstall.ID}) {
		t.Fatalf("uninstall=%#v configure=%#v", uninstall, configure)
	}
	if strings.Contains(string(configure.File.Content), "node =") || !strings.Contains(string(configure.File.Content), "python = '3.13'") {
		t.Fatalf("candidate = %q", configure.File.Content)
	}

	changed := profile.Packages{Mise: profile.MiseTools{"node": {"version": "22"}, "python": {"version": "3.13"}}}
	plan, err = (Provider{MiseGlobalConfig: config}).Plan(saved, changed, 13, "4.0", "4.1", PlanOptions{Exact: true})
	if err != nil || len(plan.Operations) != 0 || len(plan.Skipped) == 0 {
		t.Fatalf("changed declaration plan=%#v err=%v, want safe skip", plan, err)
	}
}

func TestExactMiseAdditionAndRemovalShareOnePreconditionedRewrite(t *testing.T) {
	config := filepath.Join(t.TempDir(), "mise", "config.toml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("[tools]\nnode = '24'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved := profile.Packages{
		Mise:   profile.MiseTools{"python": {"version": "3.13"}},
		Absent: []profile.PackageAbsence{{Ref: "mise:node", Mise: profile.MiseTool{"version": "24"}}},
	}
	current := profile.Packages{Mise: profile.MiseTools{"node": {"version": "24"}}}
	plan, err := (Provider{MiseGlobalConfig: config}).Plan(saved, current, 13, "4.0", "4.1", PlanOptions{Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	var writes, uninstalls, installs []model.Operation
	for _, op := range plan.Operations {
		switch {
		case op.File != nil:
			writes = append(writes, op)
		case op.Action == "remove":
			uninstalls = append(uninstalls, op)
		case op.Action == "install":
			installs = append(installs, op)
		}
	}
	if len(writes) != 1 || len(uninstalls) != 1 || len(installs) != 1 {
		t.Fatalf("Operations = %#v, want one uninstall, one shared rewrite, one install", plan.Operations)
	}
	if !strings.Contains(string(writes[0].File.Content), "python") || strings.Contains(string(writes[0].File.Content), "node") {
		t.Fatalf("shared candidate = %q", writes[0].File.Content)
	}
	if !reflect.DeepEqual(writes[0].DependsOn, []string{uninstalls[0].ID}) || !reflect.DeepEqual(installs[0].DependsOn, []string{writes[0].ID}) {
		t.Fatalf("uninstall=%#v rewrite=%#v install=%#v", uninstalls[0], writes[0], installs[0])
	}
}

func TestVerifyExactChecksOnlyActionablePackageTombstones(t *testing.T) {
	saved := profile.Packages{Absent: []profile.PackageAbsence{
		{Ref: "official:htop"},
		{Ref: "official:dependency"},
		{Ref: "mise:node", Mise: profile.MiseTool{"version": "24"}},
	}}
	current := profile.Packages{Official: []string{"htop"}, Installed: []string{"htop", "dependency"}, Mise: profile.MiseTools{"node": {"version": "24"}}}
	additive := Verify(saved, current, VerifyOptions{})
	if !additive.OK {
		t.Fatalf("additive verification = %#v", additive)
	}
	exact := Verify(saved, current, VerifyOptions{Exact: true})
	if exact.OK || !reflect.DeepEqual(exact.Missing, []string{"mise:node", "official:htop"}) {
		t.Fatalf("exact verification = %#v", exact)
	}
}
