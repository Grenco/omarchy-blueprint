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

func TestPlanInstallsNativeBeforeAURAndNeverRemoves(t *testing.T) {
	saved := profile.Packages{Official: []string{"git", "ripgrep", "zoxide"}, AUR: []string{"tool-bin"}}
	current := profile.Packages{Official: []string{"git", "extra"}}
	plan := Plan(saved, current, 1, "4.0.0", "4.1.0")
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if plan.Operations[0].Resource != "official:ripgrep,zoxide" || plan.Operations[1].Resource != "aur:tool-bin" {
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
	saved := profile.Packages{Official: []string{"git", "nvidia-open", "amd-ucode"}, AUR: []string{"nvidia-580xx-dkms"}}
	plan := Plan(saved, profile.Packages{Official: []string{"git"}}, 1, "4.0.0", "4.0.0")
	if len(plan.Operations) != 0 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	want := []string{"aur:nvidia-580xx-dkms", "official:amd-ucode", "official:nvidia-open"}
	var got []string
	for _, skipped := range plan.Skipped {
		got = append(got, skipped.Resource)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("skipped = %#v", got)
	}
}

func TestClassifyKeepsHardwareOutOfPortableDiff(t *testing.T) {
	saved := profile.Packages{Official: []string{"git", "nvidia-open"}}
	current := profile.Packages{Official: []string{"git", "nvidia-settings"}, AUR: []string{"nvidia-580xx-dkms"}}
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
