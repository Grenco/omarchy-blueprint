package packages

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/graeme/omarchy-blueprint/internal/command"
	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
)

type queryRunner struct {
	output string
	err    error
}

func (r queryRunner) Run(context.Context, string, ...string) (string, error) {
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

func TestExcludeAndIncludeGenericPackageReference(t *testing.T) {
	original := profile.Packages{Official: []string{"git"}, AUR: []string{"dislocker-git"}}
	excluded, changed, err := Exclude(original, []string{"package:dislocker-git"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"aur:dislocker-git"}) || len(excluded.AUR) != 0 || !reflect.DeepEqual(excluded.Excluded, changed) {
		t.Fatalf("excluded=%#v changed=%#v", excluded, changed)
	}
	if changes := Diff(excluded, original); len(changes) != 0 {
		t.Fatalf("excluded package caused drift: %#v", changes)
	}
	plan := Plan(excluded, profile.Packages{Official: []string{"git"}}, 1, "4.0.0", "4.0.0")
	if len(plan.Operations) != 0 || len(plan.Skipped) != 1 || plan.Skipped[0].Reason != "excluded by profile" {
		t.Fatalf("plan = %#v", plan)
	}
	included, changed, err := Include(excluded, []string{"package:dislocker-git"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(changed, []string{"aur:dislocker-git"}) || !reflect.DeepEqual(included.AUR, []string{"dislocker-git"}) || len(included.Excluded) != 0 {
		t.Fatalf("included=%#v changed=%#v", included, changed)
	}
}

func TestExcludeRejectsUnknownAndAmbiguousReferencesAtomically(t *testing.T) {
	original := profile.Packages{Official: []string{"same"}, AUR: []string{"same"}}
	for _, ref := range []string{"package:missing", "package:same", "bad:same", "aur:has space"} {
		got, _, err := Exclude(original, []string{"official:same", ref})
		if err == nil {
			t.Fatalf("expected %q to fail", ref)
		}
		if !reflect.DeepEqual(got, original) {
			t.Fatalf("%q partially mutated profile: %#v", ref, got)
		}
	}
}

func TestValidateExclusionsRejectsInvalidAndOverlappingEntries(t *testing.T) {
	tests := []profile.Packages{
		{Excluded: []string{"package:git"}},
		{Excluded: []string{"aur:has space"}},
		{Official: []string{"git"}, Excluded: []string{"official:git"}},
	}
	for _, packages := range tests {
		if err := ValidateExclusions(packages); err == nil {
			t.Fatalf("expected %#v to fail", packages)
		}
	}
	if err := ValidateExclusions(profile.Packages{Excluded: []string{"official:git", "aur:dislocker-git"}}); err != nil {
		t.Fatal(err)
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
