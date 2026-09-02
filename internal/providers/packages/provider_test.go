package packages

import (
	"os"
	"reflect"
	"testing"

	"github.com/graeme/omarchy-blueprint/internal/model"
	"github.com/graeme/omarchy-blueprint/internal/profile"
)

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
	saved := profile.Packages{Official: []string{"git", "zoxide"}, AUR: []string{"tool-bin"}}
	current := profile.Packages{Official: []string{"git", "extra"}}
	plan := Plan(saved, current, 1, "4.0.0", "4.1.0")
	if len(plan.Operations) != 2 {
		t.Fatalf("operations = %#v", plan.Operations)
	}
	if plan.Operations[0].Resource != "official:zoxide" || plan.Operations[1].Resource != "aur:tool-bin" {
		t.Fatalf("wrong order: %#v", plan.Operations)
	}
	for _, op := range plan.Operations {
		if op.Action != "install" || op.Risk != model.RiskLow || op.Reversible {
			t.Fatalf("unsafe operation: %#v", op)
		}
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
